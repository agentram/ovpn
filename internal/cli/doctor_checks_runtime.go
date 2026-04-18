package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/deploy"
	"ovpn/internal/doctor"
	"ovpn/internal/model"
	"ovpn/internal/ssh"
)

// checkAgentHealth returns check agent health.
func (a *App) checkAgentHealth(runner *ssh.Runner, cfg ssh.Config, srv model.Server) doctor.Check {
	agentBaseURL := a.agentBaseURL()
	check := doctor.Check{
		Name:    "Xray API and ovpn-agent health",
		Status:  doctor.StatusPass,
		Message: "ovpn-agent is reachable and API checks passed",
	}
	healthBody, err := a.fetchRemoteAgent(srv, "GET", a.agentURL("/health"), nil)
	if err != nil {
		check.Status = doctor.StatusFail
		check.Message = "ovpn-agent health endpoint is not reachable"
		check.Details = []string{err.Error()}
		check.Hint = fmt.Sprintf("Check `ovpn-agent` container logs and ensure the service is running on %s.", agentBaseURL)
		return check
	}
	var health agentHealth
	if err := json.Unmarshal(healthBody, &health); err != nil {
		check.Status = doctor.StatusWarn
		check.Message = "health endpoint returned non-JSON response"
		check.Details = []string{string(healthBody)}
		check.Hint = "Ensure ovpn-agent is up-to-date and healthy."
		return check
	}
	check.Details = append(check.Details, "xray_api="+health.XrayAPI)
	if health.DBPath != "" {
		check.Details = append(check.Details, "db_path="+health.DBPath)
	}
	if health.LastCollectAt != "" {
		check.Details = append(check.Details, "last_collect_at="+health.LastCollectAt)
	}
	if health.LastResetAt != "" {
		check.Details = append(check.Details, "last_reset_at="+health.LastResetAt)
	}
	if health.XrayAPIReachable != nil && !*health.XrayAPIReachable {
		check.Status = doctor.StatusFail
		check.Message = "ovpn-agent is up, but xray API is unreachable"
		check.Hint = "Check xray container health and API config binding."
	}

	statsBody, statsErr := a.fetchRemoteAgent(srv, "GET", a.agentURL("/stats/total"), nil)
	if statsErr != nil {
		if check.Status != doctor.StatusFail {
			check.Status = doctor.StatusWarn
			check.Message = "agent health is reachable, but stats endpoint failed"
		}
		check.Details = append(check.Details, "stats_error="+statsErr.Error())
		if check.Hint == "" {
			check.Hint = "Verify agent DB permissions and xray stats collection."
		}
	} else {
		check.Details = append(check.Details, fmt.Sprintf("stats_total_bytes=%d", len(statsBody)))
	}

	runtimeProbeCmd := fmt.Sprintf("curl -sS -o /dev/null -w '%%{http_code}' '%s/runtime/user/add'", agentBaseURL)
	runtimeRes, runtimeErr := a.execRemote(runner, cfg, 10*time.Second, runtimeProbeCmd)
	if runtimeErr != nil {
		if check.Status == doctor.StatusPass {
			check.Status = doctor.StatusWarn
			check.Message = "runtime route probe failed"
			check.Hint = "Check ovpn-agent runtime handlers."
		}
		check.Details = append(check.Details, "runtime_probe_error="+runtimeErr.Error())
	} else {
		code := strings.TrimSpace(runtimeRes.Stdout)
		check.Details = append(check.Details, "runtime_probe="+code)
		if code != "405" && code != "200" && code != "204" {
			if check.Status == doctor.StatusPass {
				check.Status = doctor.StatusWarn
				check.Message = "runtime endpoint returned unexpected status"
				check.Hint = "Check ovpn-agent runtime route behavior and logs."
			}
		}
	}

	return check
}

// checkDisk returns check disk.
func (a *App) checkDisk(runner *ssh.Runner, cfg ssh.Config) doctor.Check {
	cmd := buildDoctorDiskCommand()
	res, err := a.execRemote(runner, cfg, 25*time.Second, cmd)
	if err != nil {
		return doctor.Check{
			Name:    "Disk and filesystem health",
			Status:  doctor.StatusWarn,
			Message: "disk checks could not be completed",
			Details: []string{err.Error()},
			Hint:    "Check free space manually (`df -h`) and verify docker storage.",
		}
	}
	lines := strings.Split(res.Stdout, "\n")
	kv := doctor.ParseKV(res.Stdout)
	status := doctor.StatusPass
	message := "disk usage is within safe thresholds"
	var details []string
	hint := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "DF=") {
			continue
		}
		val := strings.TrimPrefix(line, "DF=")
		parts := strings.Split(val, ",")
		if len(parts) != 5 {
			continue
		}
		path := parts[0]
		usedPctRaw := strings.TrimSuffix(parts[4], "%")
		usedPct, _ := strconv.Atoi(usedPctRaw)
		details = append(details, fmt.Sprintf("%s used=%d%% avail_kb=%s", path, usedPct, parts[3]))
		switch {
		case usedPct >= 95:
			status = doctor.StatusFail
			message = "critical disk pressure detected"
			hint = "Free disk space under /var/lib/docker and /opt/ovpn before restarting services."
		case usedPct >= 85:
			if status != doctor.StatusFail {
				status = doctor.StatusWarn
				message = "disk usage is high"
			}
			if hint == "" {
				hint = "Plan cleanup of Docker images/logs and old backups."
			}
		}
	}
	if dbPath := strings.TrimSpace(kv["AGENT_DB_PATH"]); dbPath != "" {
		details = append(details, "agent_db="+dbPath)
		if kv["AGENT_DB_EXISTS"] != "1" {
			if status == doctor.StatusPass {
				status = doctor.StatusWarn
				message = "agent stats database is missing"
			}
			if hint == "" {
				hint = "Check ovpn-agent volume mount and permissions."
			}
		}
	}
	if kv["BACKUP_DIR"] != "1" {
		if status == doctor.StatusPass {
			status = doctor.StatusWarn
			message = "backup directory is missing"
		}
		if hint == "" {
			hint = "Create /opt/ovpn-backups to keep remote backup flow operational."
		}
	}
	if len(details) == 0 {
		details = []string{"no filesystem metrics returned by remote host"}
	}
	return doctor.Check{
		Name:    "Disk and filesystem health",
		Status:  status,
		Message: message,
		Details: details,
		Hint:    hint,
	}
}

// buildDoctorDiskCommand builds doctor disk command from the current inputs and defaults.
func buildDoctorDiskCommand() string {
	cmd := strings.Join([]string{
		"set -u",
		"for p in / /var /opt/ovpn /var/lib/docker; do",
		"  if [ -d \"$p\" ]; then",
		"    df -Pk \"$p\" | awk -v path=\"$p\" 'NR==2{printf \"DF=%s,%s,%s,%s,%s\\n\", path,$2,$3,$4,$5}'",
		"  fi",
		"done",
		fmt.Sprintf("if [ -d %s ]; then echo BACKUP_DIR=1; else echo BACKUP_DIR=0; fi", shellQuote(deploy.RemoteBackupDir)),
		"if sudo -n docker inspect ovpn-agent >/dev/null 2>&1; then",
		"  SRC=$(sudo -n docker inspect ovpn-agent --format '{{range .Mounts}}{{if eq .Destination \"/var/lib/ovpn-agent\"}}{{.Source}}{{end}}{{end}}' 2>/dev/null || true)",
		"  if [ -n \"$SRC\" ]; then",
		"    echo AGENT_DB_PATH=$SRC/stats.db",
		"    if [ -f \"$SRC/stats.db\" ]; then echo AGENT_DB_EXISTS=1; else echo AGENT_DB_EXISTS=0; fi",
		"  fi",
		"fi",
	}, "\n")
	return cmd
}

// collectDoctorLogs executes doctor logs flow and returns the first error.
func (a *App) collectDoctorLogs(srv model.Server, tail int) map[string]string {
	services := []string{"xray", "ovpn-agent"}
	out := map[string]string{}
	runner := a.newRunner("doctor.logs")
	cfg := sshFromServer(srv)
	for _, svc := range services {
		serviceArg, err := validateComposeService(svc)
		if err != nil {
			continue
		}
		cmd := fmt.Sprintf("set -e; cd %s; sudo -n docker compose --env-file .env -f docker-compose.yml logs --tail %d%s 2>&1", deploy.RemoteDir, tail, serviceArg)
		res, runErr := a.execRemote(runner, cfg, 30*time.Second, cmd)
		if runErr != nil {
			out[svc] = "failed to fetch logs: " + runErr.Error()
			continue
		}
		text := strings.TrimSpace(res.Stdout)
		if text == "" {
			text = "(no log lines)"
		}
		out[svc] = text
	}
	return out
}

// addRemoteSkips applies remote skips and returns an error on failure.
func addRemoteSkips(report *doctor.Report, includeSSH bool) {
	names := []string{
		"Sudo and permissions",
		"Docker and Compose availability",
		"Deploy root and files",
		"Compose stack state",
		"Xray config validation",
		"Xray API and ovpn-agent health",
		"Disk and filesystem health",
	}
	if includeSSH {
		names = append([]string{"SSH connectivity"}, names...)
	}
	for _, name := range names {
		report.Add(doctor.Check{Name: name, Status: doctor.StatusSkip, Message: "skipped due previous failure"})
	}
}
