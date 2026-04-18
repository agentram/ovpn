package cli

import (
	"fmt"
	"strings"
	"time"

	"ovpn/internal/deploy"
	"ovpn/internal/doctor"
	"ovpn/internal/ssh"
)

// checkSSH returns check ssh.
func (a *App) checkSSH(runner *ssh.Runner, cfg ssh.Config) (doctor.Check, bool) {
	cmd := strings.Join([]string{
		"set -u",
		"echo HOSTNAME=$(hostname 2>/dev/null || uname -n)",
		"echo REMOTE_USER=$(id -un 2>/dev/null || whoami)",
		"echo KERNEL=$(uname -srm 2>/dev/null || true)",
		"if [ -r /etc/os-release ]; then . /etc/os-release; echo OS=${PRETTY_NAME:-$ID}; fi",
	}, "; ")
	res, err := a.execRemote(runner, cfg, 20*time.Second, cmd)
	if err != nil {
		return doctor.Check{
			Name:    "SSH connectivity",
			Status:  doctor.StatusFail,
			Message: "SSH authentication or remote execution failed",
			Details: []string{err.Error()},
			Hint:    "Verify SSH key, user, port, firewall, and host key settings for this server.",
		}, false
	}
	kv := doctor.ParseKV(res.Stdout)
	details := []string{
		"hostname=" + kvOr(kv, "HOSTNAME", "unknown"),
		"remote_user=" + kvOr(kv, "REMOTE_USER", cfg.User),
		"kernel=" + kvOr(kv, "KERNEL", "unknown"),
	}
	if osName := strings.TrimSpace(kv["OS"]); osName != "" {
		details = append(details, "os="+osName)
	}
	return doctor.Check{
		Name:    "SSH connectivity",
		Status:  doctor.StatusPass,
		Message: "SSH auth and remote command execution are working",
		Details: details,
	}, true
}

// checkSudo returns check sudo.
func (a *App) checkSudo(runner *ssh.Runner, cfg ssh.Config) doctor.Check {
	cmd := strings.Join([]string{
		"set -u",
		"if sudo -n true >/dev/null 2>&1; then echo SUDO_NOPASS=1; else echo SUDO_NOPASS=0; fi",
		"if command -v docker >/dev/null 2>&1; then echo DOCKER_BIN=1; else echo DOCKER_BIN=0; fi",
		"if docker info >/dev/null 2>&1; then echo DOCKER_DIRECT=1; else echo DOCKER_DIRECT=0; fi",
		"if sudo -n docker info >/dev/null 2>&1; then echo DOCKER_SUDO=1; else echo DOCKER_SUDO=0; fi",
		"echo GROUPS=$(id -nG 2>/dev/null || true)",
	}, "; ")
	res, err := a.execRemote(runner, cfg, 20*time.Second, cmd)
	if err != nil {
		return doctor.Check{
			Name:    "Sudo and permissions",
			Status:  doctor.StatusFail,
			Message: "cannot verify sudo/docker permissions",
			Details: []string{err.Error()},
			Hint:    "Ensure the SSH user has passwordless sudo as required by ovpn.",
		}
	}
	kv := doctor.ParseKV(res.Stdout)
	details := []string{
		"sudonopass=" + kvOr(kv, "SUDO_NOPASS", "0"),
		"docker_bin=" + kvOr(kv, "DOCKER_BIN", "0"),
		"docker_direct=" + kvOr(kv, "DOCKER_DIRECT", "0"),
		"docker_sudo=" + kvOr(kv, "DOCKER_SUDO", "0"),
	}
	if groups := strings.TrimSpace(kv["GROUPS"]); groups != "" {
		details = append(details, "groups="+groups)
	}

	sudoOK := kv["SUDO_NOPASS"] == "1"
	dockerOK := kv["DOCKER_DIRECT"] == "1" || kv["DOCKER_SUDO"] == "1"

	check := doctor.Check{
		Name:    "Sudo and permissions",
		Status:  doctor.StatusPass,
		Message: "sudo and docker permissions are sufficient",
		Details: details,
	}
	if !sudoOK {
		check.Status = doctor.StatusFail
		check.Message = "passwordless sudo is not available"
		check.Hint = "Grant NOPASSWD sudo for the ovpn operator user."
		return check
	}
	if !dockerOK {
		check.Status = doctor.StatusFail
		check.Message = "docker commands are not usable for this user"
		check.Hint = "Fix docker daemon access or docker group membership, then retry."
	}
	return check
}

// checkDocker returns check docker.
func (a *App) checkDocker(runner *ssh.Runner, cfg ssh.Config) doctor.Check {
	cmd := strings.Join([]string{
		"set -u",
		"if command -v docker >/dev/null 2>&1; then echo DOCKER_VERSION=$(docker --version 2>/dev/null | tr -s ' '); else echo DOCKER_VERSION=; fi",
		"if sudo -n docker info >/dev/null 2>&1; then echo DOCKER_DAEMON=1; else echo DOCKER_DAEMON=0; fi",
		"if sudo -n docker compose version >/dev/null 2>&1; then echo COMPOSE_OK=1; echo COMPOSE_VERSION=$(sudo -n docker compose version 2>/dev/null | head -n1); else echo COMPOSE_OK=0; fi",
	}, "; ")
	res, err := a.execRemote(runner, cfg, 25*time.Second, cmd)
	if err != nil {
		return doctor.Check{
			Name:    "Docker and Compose availability",
			Status:  doctor.StatusFail,
			Message: "docker/compose checks failed to execute",
			Details: []string{err.Error()},
			Hint:    "Install Docker Engine and docker compose plugin, then run `ovpn server init <server>`.",
		}
	}
	kv := doctor.ParseKV(res.Stdout)
	check := doctor.Check{
		Name:    "Docker and Compose availability",
		Status:  doctor.StatusPass,
		Message: "docker engine and compose plugin are available",
		Details: []string{
			"docker_version=" + kvOr(kv, "DOCKER_VERSION", "unknown"),
			"compose_version=" + kvOr(kv, "COMPOSE_VERSION", "unknown"),
			"docker_daemon=" + kvOr(kv, "DOCKER_DAEMON", "0"),
		},
	}
	if strings.TrimSpace(kv["DOCKER_VERSION"]) == "" {
		check.Status = doctor.StatusFail
		check.Message = "docker is not installed"
		check.Hint = "Install Docker on the host (`ovpn server init <server>` handles this)."
		return check
	}
	if kv["DOCKER_DAEMON"] != "1" {
		check.Status = doctor.StatusFail
		check.Message = "docker daemon is not reachable"
		check.Hint = "Start Docker service and verify `sudo docker info` succeeds."
		return check
	}
	if kv["COMPOSE_OK"] != "1" {
		check.Status = doctor.StatusFail
		check.Message = "docker compose plugin is missing or broken"
		check.Hint = "Install docker compose plugin and rerun diagnostics."
	}
	return check
}

// checkDeployFiles returns check deploy files.
func (a *App) checkDeployFiles(runner *ssh.Runner, cfg ssh.Config) doctor.Check {
	paths := []string{
		deploy.RemoteDir,
		deploy.RemoteDir + "/docker-compose.yml",
		deploy.RemoteDir + "/.env",
		deploy.RemoteDir + "/xray/config.json",
		deploy.RemoteDir + "/agent/ovpn-agent",
	}
	cmdParts := []string{"set -u"}
	for _, p := range paths {
		cmdParts = append(cmdParts, fmt.Sprintf("if [ -e %s ]; then echo EXISTS_%s=1; else echo EXISTS_%s=0; fi", shellQuote(p), sanitizeKey(p), sanitizeKey(p)))
	}
	cmdParts = append(cmdParts, fmt.Sprintf("if [ -d %s ]; then stat -c 'OVPN_OWNER=%%U:%%G OVPN_MODE=%%a' %s; fi", shellQuote(deploy.RemoteDir), shellQuote(deploy.RemoteDir)))
	cmdParts = append(cmdParts, fmt.Sprintf("if [ -d %s ]; then echo BACKUP_DIR=1; else echo BACKUP_DIR=0; fi", shellQuote(deploy.RemoteBackupDir)))
	res, err := a.execRemote(runner, cfg, 20*time.Second, strings.Join(cmdParts, "; "))
	if err != nil {
		return doctor.Check{
			Name:    "Deploy root and files",
			Status:  doctor.StatusFail,
			Message: "cannot inspect remote ovpn files",
			Details: []string{err.Error()},
			Hint:    "Run `ovpn server init <server>` and `ovpn deploy <server>`.",
		}
	}
	kv := doctor.ParseKV(res.Stdout)
	var missing []string
	for _, p := range paths {
		if kv["EXISTS_"+sanitizeKey(p)] != "1" {
			missing = append(missing, p)
		}
	}
	check := doctor.Check{
		Name:    "Deploy root and files",
		Status:  doctor.StatusPass,
		Message: "required ovpn files are present",
		Details: []string{
			"backup_dir=" + kvOr(kv, "BACKUP_DIR", "0"),
			"owner_mode=" + strings.TrimSpace(extractOwnerMode(res.Stdout)),
		},
	}
	if len(missing) > 0 {
		check.Status = doctor.StatusFail
		check.Message = "required ovpn files are missing"
		check.Details = append(check.Details, "missing="+strings.Join(missing, ", "))
		check.Hint = "Run `ovpn deploy <server>` to re-upload compose/config files."
		return check
	}
	if kv["BACKUP_DIR"] != "1" {
		check.Status = doctor.StatusWarn
		check.Message = "deploy files are present, but backup dir is missing"
		check.Hint = "Create backup directory or run `ovpn server init <server>` again."
	}
	return check
}

// checkComposeState returns check compose state.
func (a *App) checkComposeState(runner *ssh.Runner, cfg ssh.Config) doctor.Check {
	validateCmd := fmt.Sprintf("set -e; cd %s; sudo -n docker compose --env-file .env -f docker-compose.yml config -q", shellQuote(deploy.RemoteDir))
	if _, err := a.execRemote(runner, cfg, 25*time.Second, validateCmd); err != nil {
		return doctor.Check{
			Name:    "Compose stack state",
			Status:  doctor.StatusFail,
			Message: "docker compose config validation failed",
			Details: []string{err.Error()},
			Hint:    "Run `ovpn config validate --server <server>` and redeploy.",
		}
	}

	psCmd := fmt.Sprintf("set -e; cd %s; sudo -n docker compose --env-file .env -f docker-compose.yml ps --all --format json", shellQuote(deploy.RemoteDir))
	psRes, err := a.execRemote(runner, cfg, 25*time.Second, psCmd)
	if err != nil {
		return doctor.Check{
			Name:    "Compose stack state",
			Status:  doctor.StatusFail,
			Message: "cannot read compose service state",
			Details: []string{err.Error()},
			Hint:    "Check docker compose health and run `ovpn server logs <server>`.",
		}
	}
	states, err := doctor.ParseComposePS(psRes.Stdout)
	if err != nil {
		return doctor.Check{
			Name:    "Compose stack state",
			Status:  doctor.StatusWarn,
			Message: "compose config is valid, but service state output was not parseable",
			Details: []string{strings.TrimSpace(psRes.Stdout)},
			Hint:    "Run `ovpn server status <server>` for raw status output.",
		}
	}

	byService := map[string]doctor.ServiceState{}
	for _, st := range states {
		byService[st.Service] = st
	}
	required := []string{"xray", "ovpn-agent"}
	details := make([]string, 0, len(states))
	status := doctor.StatusPass
	message := "required compose services are running"
	hint := ""
	for _, svc := range required {
		st, ok := byService[svc]
		if !ok {
			status = doctor.StatusFail
			message = "required compose services are missing"
			details = append(details, svc+"=missing")
			hint = "Run `ovpn deploy <server>` to create missing services."
			continue
		}
		details = append(details, fmt.Sprintf("%s=%s %s", svc, trimState(st.State), trimState(st.Status)))
		lower := strings.ToLower(st.State + " " + st.Status)
		switch {
		case strings.Contains(lower, "running"):
		case strings.Contains(lower, "restarting"):
			if status != doctor.StatusFail {
				status = doctor.StatusWarn
				message = "one or more services are restarting"
			}
			if hint == "" {
				hint = "Inspect service logs with `ovpn server logs <server> --service " + svc + " --tail 200`."
			}
		default:
			status = doctor.StatusFail
			message = "one or more required services are not running"
			hint = "Inspect logs and restart/deploy: `ovpn server logs <server> --service " + svc + " --tail 200`."
		}
	}
	return doctor.Check{
		Name:    "Compose stack state",
		Status:  status,
		Message: message,
		Details: details,
		Hint:    hint,
	}
}

// checkXrayConfig returns check xray config.
func (a *App) checkXrayConfig(runner *ssh.Runner, cfg ssh.Config) doctor.Check {
	cmd := strings.Join([]string{
		"set -e",
		"cd " + shellQuote(deploy.RemoteDir),
		". ./.env",
		fmt.Sprintf("sudo -n docker run --rm -v %s/xray/config.json:/etc/xray/config.json:ro $XRAY_IMAGE run -test -config /etc/xray/config.json", deploy.RemoteDir),
	}, "; ")
	res, err := a.execRemote(runner, cfg, 40*time.Second, cmd)
	if err != nil {
		return doctor.Check{
			Name:    "Xray config validation",
			Status:  doctor.StatusFail,
			Message: "xray config test failed",
			Details: []string{err.Error()},
			Hint:    "Run `ovpn config validate --server <server>` and fix xray config fields.",
		}
	}
	details := trimmedLines(res.Stdout)
	if len(details) == 0 {
		details = []string{"xray run -test completed without errors"}
	}
	return doctor.Check{
		Name:    "Xray config validation",
		Status:  doctor.StatusPass,
		Message: "xray config test passed",
		Details: details,
	}
}
