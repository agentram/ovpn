package deploy

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ovpn/internal/ssh"
)

const (
	// RemoteDir is the canonical remote working directory managed by ovpn.
	RemoteDir = "/opt/ovpn"
	// RemoteBackupDir stores pre-deploy snapshots used for manual rollback/forensics.
	RemoteBackupDir = "/opt/ovpn-backups"
	// RemoteStageDir keeps uploaded candidate bundles isolated until validation passes.
	RemoteStageDir = RemoteDir + "/.incoming"
	// SnapshotRetentionCount caps retained pre-deploy ovpn-* snapshots in remote backup dir.
	SnapshotRetentionCount = 7
	// XrayRuntimeGID is the GID the pinned ghcr.io/xtls/xray-core image runs as (distroless
	// "nonroot"). config.json embeds the REALITY private key and every client UUID, so it is
	// delivered as 0640 root:XrayRuntimeGID: readable by the xray container but not by other
	// local users on a shared host.
	XrayRuntimeGID = 65532
)

var (
	uploadCopyTimeout            = 2 * time.Minute
	uploadExtractTimeout         = 30 * time.Second
	deployBackupTimeout          = 30 * time.Second
	deployComposeValidateTimeout = 30 * time.Second
	deployXrayValidateTimeout    = 60 * time.Second
	deployApplyTimeout           = 30 * time.Second
	deployUpTimeout              = 5 * time.Minute
	deployStatusTimeout          = 30 * time.Second
)

// Runner is the minimal remote transport used by deploy operations.
// CLI code provides an SSH-backed implementation; tests use fakes.
type Runner interface {
	Exec(ctx context.Context, cfg ssh.Config, remoteCmd string) (ssh.Result, error)
	CopyFile(ctx context.Context, cfg ssh.Config, localPath, remotePath string) error
}

type CleanupOptions struct {
	IncludeMonitoring bool
	RemoveVolumes     bool
	RemoveBackups     bool
}

// ValidateConfigWithDocker runs `xray -test` against a config file using the given image, with no extra mounts.
func ValidateConfigWithDocker(ctx context.Context, xrayImage string, configPath string) error {
	return ValidateConfigWithDockerAndMounts(ctx, xrayImage, configPath, nil)
}

// ValidateConfigWithDockerAndMounts executes config validation with additional bind mounts.
func ValidateConfigWithDockerAndMounts(ctx context.Context, xrayImage string, configPath string, extraMounts []string) error {
	if xrayImage == "" {
		return fmt.Errorf("xray image is required")
	}
	// ghcr.io/xtls/xray-core images use /usr/local/bin/xray as ENTRYPOINT, so the command
	// passed to `docker run` must not include a second leading `xray` token.
	args := []string{"run", "--rm", "-v", fmt.Sprintf("%s:/etc/xray/config.json:ro", configPath)}
	args = append(args, extraMounts...)
	args = append(args, xrayImage, "run", "-test", "-config", "/etc/xray/config.json")
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if isLikelyXrayGeositeResourceError(string(out)) {
			return fmt.Errorf("xray config validation failed: %w: %s; hint: set OVPN_SECURITY_PROFILE=off to bypass BT/tracker geosite rules when this image lacks geosite resources", err, strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("xray config validation failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// buildBootstrapCommand renders the script that installs Docker/Compose and prepares the runtime directories.
func buildBootstrapCommand() string {
	return strings.Join([]string{
		"set -e",
		"if ! command -v docker >/dev/null 2>&1; then sudo apt-get update -y && sudo apt-get install -y ca-certificates curl gnupg; fi",
		"if ! command -v docker >/dev/null 2>&1; then curl -fsSL https://get.docker.com | sh; fi",
		"if ! sudo docker compose version >/dev/null 2>&1; then sudo apt-get update -y && sudo apt-get install -y docker-compose-plugin; fi",
		"sudo mkdir -p " + RemoteDir + " " + RemoteBackupDir,
		"sudo chown -R $USER:$USER " + RemoteDir + " " + RemoteBackupDir,
	}, " && ")
}

// BootstrapRemote installs Docker/Compose and prepares the runtime directories on a fresh host.
func BootstrapRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	cmd := buildBootstrapCommand()
	_, err := runner.Exec(ctx, cfg, cmd)
	if err != nil {
		return fmt.Errorf("bootstrap remote host %s: %w", cfg.Host, err)
	}
	return nil
}

// buildExtractCommand renders the script that unpacks the uploaded bundle into the staging directory and locks down its secret files.
func buildExtractCommand(remoteTar string) string {
	// --no-same-owner makes extracted files owned by the deploying account instead of the
	// operator's local UID/GID baked into the tar header (which maps to a phantom user on the host).
	// .env is left owned by that account at 0600 so deploy/doctor/CLI steps that read it as the SSH
	// user (config validation, the agent-token lookup) work for non-root deployers; root still reads
	// it via sudo for `docker compose`. config.json instead targets the xray runtime group because
	// only the xray container needs to read it.
	return fmt.Sprintf("set -e; mkdir -p %[1]s; find %[1]s -mindepth 1 -maxdepth 1 -exec rm -rf {} +; tar --no-same-owner -xzf %[2]s -C %[1]s; rm -f %[2]s; mkdir -p %[1]s/logs; sudo chown %[3]d:%[3]d %[1]s/logs; sudo chmod 770 %[1]s/logs; if [ -f %[1]s/.env ]; then chmod 600 %[1]s/.env; fi; if [ -f %[1]s/xray/config.json ]; then sudo chown 0:%[3]d %[1]s/xray/config.json; sudo chmod 640 %[1]s/xray/config.json; fi", RemoteStageDir, remoteTar, XrayRuntimeGID)
}

func shellQuote(v string) string {
	v = strings.ReplaceAll(v, `'`, `'"'"'`)
	return "'" + v + "'"
}

func withRemoteTimeout(timeout time.Duration, cmd string) string {
	seconds := int(math.Ceil(timeout.Seconds()))
	if seconds <= 0 {
		seconds = 1
	}
	quoted := shellQuote(cmd)
	return fmt.Sprintf("if command -v timeout >/dev/null 2>&1; then timeout %d sh -c %s; else sh -c %s; fi", seconds, quoted, quoted)
}

// UploadBundle archives the local bundle, copies it to the host, and extracts it into the staging directory.
func UploadBundle(ctx context.Context, runner Runner, cfg ssh.Config, bundleDir string) error {
	tarPath := filepath.Join(os.TempDir(), fmt.Sprintf("ovpn-%d.tar.gz", time.Now().UnixNano()))
	defer os.Remove(tarPath)
	if err := createTarGz(tarPath, bundleDir); err != nil {
		return fmt.Errorf("create bundle archive: %w", err)
	}
	remoteTar := filepath.Join("/tmp", filepath.Base(tarPath))
	copyCtx, cancelCopy := ssh.TimeoutCtx(ctx, uploadCopyTimeout)
	defer cancelCopy()
	if err := runner.CopyFile(copyCtx, cfg, tarPath, remoteTar); err != nil {
		return fmt.Errorf("copy bundle to %s:%s: %w", cfg.Host, remoteTar, err)
	}
	extractCmd := buildExtractCommand(remoteTar)
	extractCtx, cancelExtract := ssh.TimeoutCtx(ctx, uploadExtractTimeout)
	defer cancelExtract()
	_, err := runner.Exec(extractCtx, cfg, withRemoteTimeout(uploadExtractTimeout, extractCmd))
	if err != nil {
		return fmt.Errorf("extract bundle on %s: %w", cfg.Host, err)
	}
	return nil
}

// buildDeployBackupCommand renders the script that snapshots the live runtime dir and prunes old snapshots.
func buildDeployBackupCommand(backupStamp string) string {
	return fmt.Sprintf("set -e; if [ -d %[1]s ]; then sudo cp -a %[1]s %[2]s/ovpn-%[3]s; fi; old_snapshots=$(find %[2]s -mindepth 1 -maxdepth 1 -name 'ovpn-*' -printf '%%T@ %%p\\n' | sort -nr | awk 'NR>%[4]d {print $2}'); if [ -n \"$old_snapshots\" ]; then printf '%%s\\n' \"$old_snapshots\" | xargs -r sudo rm -rf; fi", RemoteDir, RemoteBackupDir, backupStamp, SnapshotRetentionCount)
}

// buildDeployComposeValidateCommand renders the `docker compose config` validation for a staged bundle.
func buildDeployComposeValidateCommand(dir string) string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml config -q", dir)
}

// buildDeployXrayTestCommand renders the `xray -test` validation of a staged config inside the target image.
func buildDeployXrayTestCommand(dir string) string {
	// Validate config in the target image before compose up to catch incompatible syntax early.
	return fmt.Sprintf("set -e; cd %[1]s; . ./.env; sudo mkdir -p %[1]s/logs; sudo chown %[2]d:%[2]d %[1]s/logs; sudo chmod 770 %[1]s/logs; extra_mounts=''; if [ -f %[1]s/geodata/geosite.dat ]; then extra_mounts=\"$extra_mounts -v %[1]s/geodata/geosite.dat:/usr/local/share/xray/geosite.dat:ro\"; fi; if [ -f %[1]s/geodata/geoip.dat ]; then extra_mounts=\"$extra_mounts -v %[1]s/geodata/geoip.dat:/usr/local/share/xray/geoip.dat:ro\"; fi; eval sudo docker run --rm -v %[1]s/xray/config.json:/etc/xray/config.json:ro -v %[1]s/logs:/var/log/ovpn $extra_mounts $XRAY_IMAGE run -test -config /etc/xray/config.json", dir, XrayRuntimeGID)
}

// isLikelyXrayVersionTagError reports whether likely xray version tag error.
func isLikelyXrayVersionTagError(errText string) bool {
	return strings.Contains(errText, "xray-core:v") && strings.Contains(errText, "not found")
}

// isLikelyXrayGeositeResourceError reports whether likely xray geosite resource error.
func isLikelyXrayGeositeResourceError(errText string) bool {
	text := strings.ToLower(strings.TrimSpace(errText))
	if text == "" {
		return false
	}
	return (strings.Contains(text, "geosite") || strings.Contains(text, "category-public-tracker")) &&
		(strings.Contains(text, "no such file") || strings.Contains(text, "failed") || strings.Contains(text, "not found"))
}

// buildDeployApplyCommand renders the script that swaps the validated bundle into the live runtime directory, preserving existing secret files.
func buildDeployApplyCommand() string {
	// When ovpn-agent is running, truncating /opt/ovpn/agent/ovpn-agent in-place can fail with
	// ETXTBSY ("Text file busy"). Same applies to ovpn-telegram-bot binary when monitoring is up.
	// Unlink first, then copy staged files.
	return fmt.Sprintf(
		"set -e; token_file=%[1]s/monitoring/secrets/telegram_bot_token; token_backup=/tmp/ovpn-telegram-bot-token-prev; stage_token=%[2]s/monitoring/secrets/telegram_bot_token; admin_file=%[1]s/monitoring/secrets/telegram_admin_token; admin_backup=/tmp/ovpn-telegram-admin-token-prev; stage_admin=%[2]s/monitoring/secrets/telegram_admin_token; rm -f \"$token_backup\" \"$admin_backup\"; if [ -s \"$token_file\" ]; then cp -f \"$token_file\" \"$token_backup\"; fi; if [ -s \"$admin_file\" ]; then cp -f \"$admin_file\" \"$admin_backup\"; fi; mkdir -p %[1]s/agent %[1]s/monitoring/telegram-bot; rm -f %[1]s/agent/ovpn-agent %[1]s/monitoring/telegram-bot/ovpn-telegram-bot; sudo cp -a %[2]s/. %[1]s/; mkdir -p %[1]s/monitoring/secrets; sudo mkdir -p %[1]s/logs; sudo chown %[3]d:%[3]d %[1]s/logs; sudo chmod 770 %[1]s/logs; if [ ! -s \"$stage_token\" ] && [ -s \"$token_backup\" ]; then mv -f \"$token_backup\" \"$token_file\"; fi; if [ ! -s \"$stage_admin\" ] && [ -s \"$admin_backup\" ]; then mv -f \"$admin_backup\" \"$admin_file\"; fi; if [ -f %[1]s/.env ]; then chmod 600 %[1]s/.env; fi; if [ -f %[1]s/xray/config.json ]; then sudo chown 0:%[3]d %[1]s/xray/config.json; sudo chmod 640 %[1]s/xray/config.json; fi; if [ -f \"$token_file\" ]; then chmod 600 \"$token_file\"; fi; if [ -f \"$admin_file\" ]; then chmod 600 \"$admin_file\"; fi; rm -f \"$token_backup\" \"$admin_backup\"",
		RemoteDir,
		RemoteStageDir,
		XrayRuntimeGID,
	)
}

// buildDeployUpCommand renders the `docker compose up` that force-recreates services, preserving an already-enabled monitoring stack.
func buildDeployUpCommand() string {
	// Force recreate so updated binaries/config mounts are guaranteed to be picked up
	// by running containers on every deploy. Preserve an already-enabled monitoring stack
	// by including the monitoring compose file only when monitoring containers already exist.
	return fmt.Sprintf(
		"set -e; cd %[1]s; monitor_files=''; monitor_services='ovpn-prometheus|ovpn-alertmanager|ovpn-grafana|ovpn-node-exporter|ovpn-cadvisor|ovpn-telegram-bot'; if [ -f docker-compose.monitoring.yml ] && sudo docker ps -a --format '{{.Names}}' | grep -Eq \"^($monitor_services)$\"; then monitor_files='-f docker-compose.monitoring.yml --profile monitoring'; fi; if [ -n \"$monitor_files\" ] && [ ! -s monitoring/secrets/telegram_bot_token ]; then eval sudo docker compose --env-file .env -f docker-compose.yml $monitor_files up -d --force-recreate --remove-orphans --scale ovpn-telegram-bot=0; else eval sudo docker compose --env-file .env -f docker-compose.yml $monitor_files up -d --force-recreate --remove-orphans; fi",
		RemoteDir,
	)
}

// buildDeployStatusCommand renders the `docker compose ps` status command.
func buildDeployStatusCommand() string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose ps", RemoteDir)
}

// buildMonitoringUpCommand renders the command that starts the monitoring stack, skipping the bot when no token is present.
func buildMonitoringUpCommand() string {
	return fmt.Sprintf("set -e; cd %s; if [ -s monitoring/secrets/telegram_bot_token ]; then sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring up -d --remove-orphans; else echo 'telegram token file is empty: starting monitoring without ovpn-telegram-bot' >&2; sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring up -d --remove-orphans --scale ovpn-telegram-bot=0; fi", RemoteDir)
}

// buildMonitoringDownCommand renders the command that stops and removes the monitoring services.
func buildMonitoringDownCommand() string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml stop prometheus alertmanager grafana node-exporter cadvisor ovpn-telegram-bot || true; sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml rm -f prometheus alertmanager grafana node-exporter cadvisor ovpn-telegram-bot || true", RemoteDir)
}

// buildMonitoringStatusCommand renders the monitoring-stack status command.
func buildMonitoringStatusCommand() string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml ps prometheus alertmanager grafana node-exporter cadvisor ovpn-telegram-bot", RemoteDir)
}

// buildCleanupMonitoringCommand renders the command that tears down the monitoring stack during cleanup.
func buildCleanupMonitoringCommand() string {
	return fmt.Sprintf("set -e; if [ ! -d %s ]; then exit 0; fi; cd %s; if [ -f docker-compose.yml ] && [ -f docker-compose.monitoring.yml ]; then sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring down --remove-orphans || true; fi", RemoteDir, RemoteDir)
}

// buildCleanupRuntimeDownCommand renders the command that stops the core runtime stack during cleanup.
func buildCleanupRuntimeDownCommand() string {
	return fmt.Sprintf("set -e; if [ ! -d %s ]; then exit 0; fi; cd %s; if [ -f docker-compose.yml ]; then sudo docker compose --env-file .env -f docker-compose.yml down --remove-orphans || true; fi", RemoteDir, RemoteDir)
}

// buildCleanupRemoveRuntimeDirCommand renders the command that removes the runtime directory.
func buildCleanupRemoveRuntimeDirCommand() string {
	return fmt.Sprintf("set -e; sudo rm -rf %s", RemoteDir)
}

// buildCleanupRemoveVolumesCommand renders the command that removes the project's Docker volumes.
func buildCleanupRemoveVolumesCommand() string {
	return "set -e; sudo docker volume ls -q --filter label=com.docker.compose.project=ovpn | xargs -r sudo docker volume rm"
}

// buildCleanupRemoveBackupsCommand renders the command that removes the remote backup directory.
func buildCleanupRemoveBackupsCommand() string {
	return fmt.Sprintf("set -e; sudo rm -rf %s", RemoteBackupDir)
}

// DeployRemote applies a staged bundle conservatively: snapshot, validate, apply, compose up, then status.
func DeployRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	// Keep ordering conservative: snapshot -> validate staged bundle -> apply -> compose up.
	// This makes deploy failures easier to recover from and avoids replacing a healthy stack
	// with a syntactically broken configuration.
	backupStamp := time.Now().UTC().Format("20060102T150405")
	backupCmd := buildDeployBackupCommand(backupStamp)
	backupCtx, cancelBackup := ssh.TimeoutCtx(ctx, deployBackupTimeout)
	defer cancelBackup()
	if _, err := runner.Exec(backupCtx, cfg, withRemoteTimeout(deployBackupTimeout, backupCmd)); err != nil {
		return fmt.Errorf("create pre-deploy backup on %s: %w", cfg.Host, err)
	}
	validateCmd := buildDeployComposeValidateCommand(RemoteStageDir)
	validateCtx, cancelValidate := ssh.TimeoutCtx(ctx, deployComposeValidateTimeout)
	defer cancelValidate()
	if _, err := runner.Exec(validateCtx, cfg, withRemoteTimeout(deployComposeValidateTimeout, validateCmd)); err != nil {
		return fmt.Errorf("validate compose config on %s: %w", cfg.Host, err)
	}
	xrayTestCmd := buildDeployXrayTestCommand(RemoteStageDir)
	xrayCtx, cancelXray := ssh.TimeoutCtx(ctx, deployXrayValidateTimeout)
	defer cancelXray()
	if _, err := runner.Exec(xrayCtx, cfg, withRemoteTimeout(deployXrayValidateTimeout, xrayTestCmd)); err != nil {
		if isLikelyXrayVersionTagError(err.Error()) {
			return fmt.Errorf("validate xray config in container on %s: %w; hint: use xray version without 'v' prefix (example: 26.3.27)", cfg.Host, err)
		}
		if isLikelyXrayGeositeResourceError(err.Error()) {
			return fmt.Errorf("validate xray config in container on %s: %w; hint: set OVPN_SECURITY_PROFILE=off and redeploy if this Xray image lacks geosite resources", cfg.Host, err)
		}
		return fmt.Errorf("validate xray config in container on %s: %w", cfg.Host, err)
	}
	applyCmd := buildDeployApplyCommand()
	applyCtx, cancelApply := ssh.TimeoutCtx(ctx, deployApplyTimeout)
	defer cancelApply()
	if _, err := runner.Exec(applyCtx, cfg, withRemoteTimeout(deployApplyTimeout, applyCmd)); err != nil {
		return fmt.Errorf("apply validated bundle on %s: %w", cfg.Host, err)
	}
	upCmd := buildDeployUpCommand()
	upCtx, cancelUp := ssh.TimeoutCtx(ctx, deployUpTimeout)
	defer cancelUp()
	if _, err := runner.Exec(upCtx, cfg, withRemoteTimeout(deployUpTimeout, upCmd)); err != nil {
		return fmt.Errorf("compose up on %s: %w", cfg.Host, err)
	}
	statusCmd := buildDeployStatusCommand()
	statusCtx, cancelStatus := ssh.TimeoutCtx(ctx, deployStatusTimeout)
	defer cancelStatus()
	_, err := runner.Exec(statusCtx, cfg, withRemoteTimeout(deployStatusTimeout, statusCmd))
	if err != nil {
		return fmt.Errorf("read post-deploy status on %s: %w", cfg.Host, err)
	}
	return nil
}

// buildRestartCommand renders the command that restarts xray and ovpn-agent.
func buildRestartCommand() string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml restart xray ovpn-agent", RemoteDir)
}

// RestartRemote restarts xray and ovpn-agent via docker compose.
func RestartRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	cmd := buildRestartCommand()
	_, err := runner.Exec(ctx, cfg, cmd)
	if err != nil {
		return fmt.Errorf("restart services on %s: %w", cfg.Host, err)
	}
	return nil
}

// RemoteStatus returns the remote `docker compose ps` output.
func RemoteStatus(ctx context.Context, runner Runner, cfg ssh.Config) (string, error) {
	res, err := runner.Exec(ctx, cfg, buildDeployStatusCommand())
	if err != nil {
		return "", fmt.Errorf("get compose status on %s: %w", cfg.Host, err)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// MonitoringUpRemote starts the optional monitoring stack on the host.
func MonitoringUpRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	if _, err := runner.Exec(ctx, cfg, buildMonitoringUpCommand()); err != nil {
		return fmt.Errorf("bring up monitoring stack on %s: %w", cfg.Host, err)
	}
	return nil
}

// MonitoringDownRemote stops the optional monitoring stack on the host.
func MonitoringDownRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	if _, err := runner.Exec(ctx, cfg, buildMonitoringDownCommand()); err != nil {
		return fmt.Errorf("stop monitoring stack on %s: %w", cfg.Host, err)
	}
	return nil
}

// MonitoringStatusRemote returns the status of the monitoring services.
func MonitoringStatusRemote(ctx context.Context, runner Runner, cfg ssh.Config) (string, error) {
	res, err := runner.Exec(ctx, cfg, buildMonitoringStatusCommand())
	if err != nil {
		return "", fmt.Errorf("get monitoring stack status on %s: %w", cfg.Host, err)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// CleanupRemote tears down the runtime (and optionally monitoring, volumes, and backups) per opts.
func CleanupRemote(ctx context.Context, runner Runner, cfg ssh.Config, opts CleanupOptions) error {
	if opts.IncludeMonitoring {
		if _, err := runner.Exec(ctx, cfg, buildCleanupMonitoringCommand()); err != nil {
			return fmt.Errorf("stop monitoring stack on %s: %w", cfg.Host, err)
		}
	}
	if _, err := runner.Exec(ctx, cfg, buildCleanupRuntimeDownCommand()); err != nil {
		return fmt.Errorf("stop runtime stack on %s: %w", cfg.Host, err)
	}
	if _, err := runner.Exec(ctx, cfg, buildCleanupRemoveRuntimeDirCommand()); err != nil {
		return fmt.Errorf("remove runtime directory on %s: %w", cfg.Host, err)
	}
	if opts.RemoveVolumes {
		if _, err := runner.Exec(ctx, cfg, buildCleanupRemoveVolumesCommand()); err != nil {
			return fmt.Errorf("remove ovpn volumes on %s: %w", cfg.Host, err)
		}
	}
	if opts.RemoveBackups {
		if _, err := runner.Exec(ctx, cfg, buildCleanupRemoveBackupsCommand()); err != nil {
			return fmt.Errorf("remove remote backups on %s: %w", cfg.Host, err)
		}
	}
	return nil
}
