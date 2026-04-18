package deploy

import (
	"context"
	"fmt"
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

// ValidateConfigWithDocker executes config with docker flow and returns the first error.
func ValidateConfigWithDocker(ctx context.Context, xrayImage string, configPath string) error {
	if xrayImage == "" {
		return fmt.Errorf("xray image is required")
	}
	// ghcr.io/xtls/xray-core images use /usr/local/bin/xray as ENTRYPOINT, so the command
	// passed to `docker run` must not include a second leading `xray` token.
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", fmt.Sprintf("%s:/etc/xray/config.json:ro", configPath), xrayImage, "run", "-test", "-config", "/etc/xray/config.json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if isLikelyXrayGeositeResourceError(string(out)) {
			return fmt.Errorf("xray config validation failed: %w: %s; hint: set OVPN_SECURITY_PROFILE=off to bypass BT/tracker geosite rules when this image lacks geosite resources", err, strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("xray config validation failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// buildBootstrapCommand builds bootstrap command from the current inputs and defaults.
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

// BootstrapRemote executes remote on the remote host in a fixed order.
func BootstrapRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	cmd := buildBootstrapCommand()
	_, err := runner.Exec(ctx, cfg, cmd)
	if err != nil {
		return fmt.Errorf("bootstrap remote host %s: %w", cfg.Host, err)
	}
	return nil
}

// buildExtractCommand builds extract command from the current inputs and defaults.
func buildExtractCommand(remoteTar string) string {
	return fmt.Sprintf("set -e; mkdir -p %[1]s; find %[1]s -mindepth 1 -maxdepth 1 -exec rm -rf {} +; tar -xzf %[2]s -C %[1]s; rm -f %[2]s", RemoteStageDir, remoteTar)
}

// UploadBundle executes bundle on the remote host in a fixed order.
func UploadBundle(ctx context.Context, runner Runner, cfg ssh.Config, bundleDir string) error {
	tarPath := filepath.Join(os.TempDir(), fmt.Sprintf("ovpn-%d.tar.gz", time.Now().UnixNano()))
	defer os.Remove(tarPath)
	if err := createTarGz(tarPath, bundleDir); err != nil {
		return fmt.Errorf("create bundle archive: %w", err)
	}
	remoteTar := filepath.Join("/tmp", filepath.Base(tarPath))
	if err := runner.CopyFile(ctx, cfg, tarPath, remoteTar); err != nil {
		return fmt.Errorf("copy bundle to %s:%s: %w", cfg.Host, remoteTar, err)
	}
	extractCmd := buildExtractCommand(remoteTar)
	_, err := runner.Exec(ctx, cfg, extractCmd)
	if err != nil {
		return fmt.Errorf("extract bundle on %s: %w", cfg.Host, err)
	}
	return nil
}

// buildDeployBackupCommand builds deploy backup command from the current inputs and defaults.
func buildDeployBackupCommand(backupStamp string) string {
	return fmt.Sprintf("set -e; if [ -d %[1]s ]; then cp -a %[1]s %[2]s/ovpn-%[3]s; fi; old_snapshots=$(find %[2]s -mindepth 1 -maxdepth 1 -name 'ovpn-*' -printf '%%T@ %%p\\n' | sort -nr | awk 'NR>%[4]d {print $2}'); if [ -n \"$old_snapshots\" ]; then printf '%%s\\n' \"$old_snapshots\" | xargs -r sudo rm -rf; fi", RemoteDir, RemoteBackupDir, backupStamp, SnapshotRetentionCount)
}

// buildDeployComposeValidateCommand builds deploy compose validate command from the current inputs and defaults.
func buildDeployComposeValidateCommand(dir string) string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml config -q", dir)
}

// buildDeployXrayTestCommand builds deploy xray test command from the current inputs and defaults.
func buildDeployXrayTestCommand(dir string) string {
	// Validate config in the target image before compose up to catch incompatible syntax early.
	return fmt.Sprintf("set -e; cd %s; . ./.env; sudo docker run --rm -v %s/xray/config.json:/etc/xray/config.json:ro $XRAY_IMAGE run -test -config /etc/xray/config.json", dir, dir)
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

// buildDeployApplyCommand builds deploy apply command from the current inputs and defaults.
func buildDeployApplyCommand() string {
	// When ovpn-agent is running, truncating /opt/ovpn/agent/ovpn-agent in-place can fail with
	// ETXTBSY ("Text file busy"). Same applies to ovpn-telegram-bot binary when monitoring is up.
	// Unlink first, then copy staged files.
	return fmt.Sprintf(
		"set -e; token_file=%[1]s/monitoring/secrets/telegram_bot_token; token_backup=/tmp/ovpn-telegram-bot-token-prev; stage_token=%[2]s/monitoring/secrets/telegram_bot_token; admin_file=%[1]s/monitoring/secrets/telegram_admin_token; admin_backup=/tmp/ovpn-telegram-admin-token-prev; stage_admin=%[2]s/monitoring/secrets/telegram_admin_token; rm -f \"$token_backup\" \"$admin_backup\"; if [ -s \"$token_file\" ]; then cp -f \"$token_file\" \"$token_backup\"; fi; if [ -s \"$admin_file\" ]; then cp -f \"$admin_file\" \"$admin_backup\"; fi; mkdir -p %[1]s/agent %[1]s/monitoring/telegram-bot; rm -f %[1]s/agent/ovpn-agent %[1]s/monitoring/telegram-bot/ovpn-telegram-bot; cp -a %[2]s/. %[1]s/; mkdir -p %[1]s/monitoring/secrets; if [ ! -s \"$stage_token\" ] && [ -s \"$token_backup\" ]; then mv -f \"$token_backup\" \"$token_file\"; fi; if [ ! -s \"$stage_admin\" ] && [ -s \"$admin_backup\" ]; then mv -f \"$admin_backup\" \"$admin_file\"; fi; if [ -f \"$token_file\" ]; then chmod 600 \"$token_file\"; fi; if [ -f \"$admin_file\" ]; then chmod 600 \"$admin_file\"; fi; rm -f \"$token_backup\" \"$admin_backup\"",
		RemoteDir,
		RemoteStageDir,
	)
}

// buildDeployUpCommand builds deploy up command from the current inputs and defaults.
func buildDeployUpCommand() string {
	// Force recreate so updated binaries/config mounts are guaranteed to be picked up
	// by running containers on every deploy.
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml up -d --force-recreate --remove-orphans", RemoteDir)
}

// buildDeployStatusCommand builds deploy status command from the current inputs and defaults.
func buildDeployStatusCommand() string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose ps", RemoteDir)
}

// buildMonitoringUpCommand builds monitoring up command from the current inputs and defaults.
func buildMonitoringUpCommand() string {
	return fmt.Sprintf("set -e; cd %s; if [ -s monitoring/secrets/telegram_bot_token ]; then sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring up -d --remove-orphans; else echo 'telegram token file is empty: starting monitoring without ovpn-telegram-bot' >&2; sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring up -d --remove-orphans --scale ovpn-telegram-bot=0; fi", RemoteDir)
}

// buildMonitoringDownCommand builds monitoring down command from the current inputs and defaults.
func buildMonitoringDownCommand() string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml stop prometheus alertmanager grafana node-exporter cadvisor ovpn-telegram-bot || true; sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml rm -f prometheus alertmanager grafana node-exporter cadvisor ovpn-telegram-bot || true", RemoteDir)
}

// buildMonitoringStatusCommand builds monitoring status command from the current inputs and defaults.
func buildMonitoringStatusCommand() string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml ps prometheus alertmanager grafana node-exporter cadvisor ovpn-telegram-bot", RemoteDir)
}

// buildCleanupMonitoringCommand builds cleanup monitoring command from the current inputs and defaults.
func buildCleanupMonitoringCommand() string {
	return fmt.Sprintf("set -e; if [ ! -d %s ]; then exit 0; fi; cd %s; if [ -f docker-compose.yml ] && [ -f docker-compose.monitoring.yml ]; then sudo docker compose --env-file .env -f docker-compose.yml -f docker-compose.monitoring.yml --profile monitoring down --remove-orphans || true; fi", RemoteDir, RemoteDir)
}

// buildCleanupRuntimeDownCommand builds cleanup runtime down command from the current inputs and defaults.
func buildCleanupRuntimeDownCommand() string {
	return fmt.Sprintf("set -e; if [ ! -d %s ]; then exit 0; fi; cd %s; if [ -f docker-compose.yml ]; then sudo docker compose --env-file .env -f docker-compose.yml down --remove-orphans || true; fi", RemoteDir, RemoteDir)
}

// buildCleanupRemoveRuntimeDirCommand builds cleanup remove runtime dir command from the current inputs and defaults.
func buildCleanupRemoveRuntimeDirCommand() string {
	return fmt.Sprintf("set -e; sudo rm -rf %s", RemoteDir)
}

// buildCleanupRemoveVolumesCommand builds cleanup remove volumes command from the current inputs and defaults.
func buildCleanupRemoveVolumesCommand() string {
	return "set -e; sudo docker volume ls -q --filter label=com.docker.compose.project=ovpn | xargs -r sudo docker volume rm"
}

// buildCleanupRemoveBackupsCommand builds cleanup remove backups command from the current inputs and defaults.
func buildCleanupRemoveBackupsCommand() string {
	return fmt.Sprintf("set -e; sudo rm -rf %s", RemoteBackupDir)
}

// DeployRemote executes remote on the remote host in a fixed order.
func DeployRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	// Keep ordering conservative: snapshot -> validate staged bundle -> apply -> compose up.
	// This makes deploy failures easier to recover from and avoids replacing a healthy stack
	// with a syntactically broken configuration.
	backupStamp := time.Now().UTC().Format("20060102T150405")
	backupCmd := buildDeployBackupCommand(backupStamp)
	if _, err := runner.Exec(ctx, cfg, backupCmd); err != nil {
		return fmt.Errorf("create pre-deploy backup on %s: %w", cfg.Host, err)
	}
	validateCmd := buildDeployComposeValidateCommand(RemoteStageDir)
	if _, err := runner.Exec(ctx, cfg, validateCmd); err != nil {
		return fmt.Errorf("validate compose config on %s: %w", cfg.Host, err)
	}
	xrayTestCmd := buildDeployXrayTestCommand(RemoteStageDir)
	if _, err := runner.Exec(ctx, cfg, xrayTestCmd); err != nil {
		if isLikelyXrayVersionTagError(err.Error()) {
			return fmt.Errorf("validate xray config in container on %s: %w; hint: use xray version without 'v' prefix (example: 26.3.27)", cfg.Host, err)
		}
		if isLikelyXrayGeositeResourceError(err.Error()) {
			return fmt.Errorf("validate xray config in container on %s: %w; hint: set OVPN_SECURITY_PROFILE=off and redeploy if this Xray image lacks geosite resources", cfg.Host, err)
		}
		return fmt.Errorf("validate xray config in container on %s: %w", cfg.Host, err)
	}
	applyCmd := buildDeployApplyCommand()
	if _, err := runner.Exec(ctx, cfg, applyCmd); err != nil {
		return fmt.Errorf("apply validated bundle on %s: %w", cfg.Host, err)
	}
	upCmd := buildDeployUpCommand()
	if _, err := runner.Exec(ctx, cfg, upCmd); err != nil {
		return fmt.Errorf("compose up on %s: %w", cfg.Host, err)
	}
	statusCmd := buildDeployStatusCommand()
	_, err := runner.Exec(ctx, cfg, statusCmd)
	if err != nil {
		return fmt.Errorf("read post-deploy status on %s: %w", cfg.Host, err)
	}
	return nil
}

// buildRestartCommand builds restart command from the current inputs and defaults.
func buildRestartCommand() string {
	return fmt.Sprintf("set -e; cd %s; sudo docker compose --env-file .env -f docker-compose.yml restart xray ovpn-agent", RemoteDir)
}

// RestartRemote executes remote on the remote host in a fixed order.
func RestartRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	cmd := buildRestartCommand()
	_, err := runner.Exec(ctx, cfg, cmd)
	if err != nil {
		return fmt.Errorf("restart services on %s: %w", cfg.Host, err)
	}
	return nil
}

// RemoteStatus executes remote status against remote hosts over SSH.
func RemoteStatus(ctx context.Context, runner Runner, cfg ssh.Config) (string, error) {
	res, err := runner.Exec(ctx, cfg, buildDeployStatusCommand())
	if err != nil {
		return "", fmt.Errorf("get compose status on %s: %w", cfg.Host, err)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// MonitoringUpRemote executes monitoring up remote against remote hosts over SSH.
func MonitoringUpRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	if _, err := runner.Exec(ctx, cfg, buildMonitoringUpCommand()); err != nil {
		return fmt.Errorf("bring up monitoring stack on %s: %w", cfg.Host, err)
	}
	return nil
}

// MonitoringDownRemote executes monitoring down remote against remote hosts over SSH.
func MonitoringDownRemote(ctx context.Context, runner Runner, cfg ssh.Config) error {
	if _, err := runner.Exec(ctx, cfg, buildMonitoringDownCommand()); err != nil {
		return fmt.Errorf("stop monitoring stack on %s: %w", cfg.Host, err)
	}
	return nil
}

// MonitoringStatusRemote executes monitoring status remote against remote hosts over SSH.
func MonitoringStatusRemote(ctx context.Context, runner Runner, cfg ssh.Config) (string, error) {
	res, err := runner.Exec(ctx, cfg, buildMonitoringStatusCommand())
	if err != nil {
		return "", fmt.Errorf("get monitoring stack status on %s: %w", cfg.Host, err)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// CleanupRemote executes cleanup remote against remote hosts over SSH.
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
