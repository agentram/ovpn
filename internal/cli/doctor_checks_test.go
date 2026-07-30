package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"ovpn/internal/deploy"
	"ovpn/internal/doctor"
	"ovpn/internal/model"
	"ovpn/internal/ssh"
)

func TestRunDoctorHappyPathWithFakeRemote(t *testing.T) {
	app := newTestAppWithServer(t, false)
	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		return []byte(`{"ok":true,"service":"ovpn-agent","xray_api":"127.0.0.1:10085","xray_api_reachable":true,"db_path":"/var/lib/ovpn-agent/stats.db","last_collect_at":"2099-01-02T03:04:05Z","last_reset_at":"2099-01-01T00:00:00Z"}`), nil
	}
	app.remoteExecHook = successfulDoctorExecHook

	report, err := app.runDoctor("main", doctorOptions{includeLogs: true, tail: 5})
	if err != nil {
		t.Fatalf("run doctor: %v", err)
	}
	if report.OverallStatus != doctor.StatusPass {
		t.Fatalf("expected PASS report, got %+v", report)
	}
	if len(report.Logs) == 0 || !strings.Contains(report.Logs["xray"], "log line") {
		t.Fatalf("expected collected logs, got %+v", report.Logs)
	}

	stdout, _, err := captureStdoutStderr(t, func() error {
		return printDoctorReport(report, true, false)
	})
	if err != nil {
		t.Fatalf("print json report: %v", err)
	}
	if !strings.Contains(stdout, `"overall_status": "pass"`) {
		t.Fatalf("unexpected JSON report:\n%s", stdout)
	}
}

func TestDoctorBranchesForRemoteFailuresAndWarnings(t *testing.T) {
	app := newTestAppWithServer(t, false)
	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{}, errors.New("ssh denied")
	}
	report, err := app.runDoctor("main", doctorOptions{})
	if err != nil {
		t.Fatalf("run doctor with ssh failure should return report without error, got %v", err)
	}
	if report.OverallStatus != doctor.StatusFail {
		t.Fatalf("expected FAIL report, got %+v", report)
	}

	missing, err := app.runDoctor("missing", doctorOptions{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing server error, got %v", err)
	}
	if missing.OverallStatus != doctor.StatusFail {
		t.Fatalf("expected missing-server fail report, got %+v", missing)
	}

	warnSrv := model.Server{
		Name:              "warn",
		Role:              model.ServerRoleVPN,
		Host:              "example.com",
		Domain:            "example.com",
		SSHUser:           "root",
		SSHPort:           22,
		XrayVersion:       "latest",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "example.com",
		RealityTarget:     "example.com:8443",
		Enabled:           true,
	}
	if got := checkLocalConfig(warnSrv); got.Status != doctor.StatusWarn {
		t.Fatalf("expected local warning, got %+v", got)
	}
	warnSrv.RealityTarget = "cdn.example.net:443"
	warnSrv.RealityServerName = "www.microsoft.com"
	if got := checkLocalConfig(warnSrv); got.Status != doctor.StatusWarn || !strings.Contains(strings.Join(got.Details, "\n"), "reality_server_name differs") {
		t.Fatalf("expected target/serverName mismatch warning, got %+v", got)
	}
	warnSrv.SSHPort = 0
	if got := checkLocalConfig(warnSrv); got.Status != doctor.StatusFail {
		t.Fatalf("expected local validation failure, got %+v", got)
	}

	if got := buildDoctorDiskCommand(); !strings.Contains(got, "AGENT_DB_PATH") || !strings.Contains(got, deploy.RemoteBackupDir) || !strings.Contains(got, "sudo -n test -f") {
		t.Fatalf("unexpected disk command: %s", got)
	}
	if got := sanitizeKey("/opt/ovpn/xray/config.json"); got != "opt_ovpn_xray_config_json" {
		t.Fatalf("sanitizeKey got %q", got)
	}
	if got := extractOwnerMode("A=1\nOVPN_OWNER=root:root OVPN_MODE=755\n"); got != "root:root OVPN_MODE=755" {
		t.Fatalf("extract owner mode got %q", got)
	}
	if got := trimmedLines(" a \n\n b "); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("trimmed lines got %+v", got)
	}
}

func TestDoctorCheckCoreFailureBranches(t *testing.T) {
	app := newTestAppWithServer(t, false)
	runner := &ssh.Runner{DryRun: true}
	cfg := ssh.Config{Host: "example.com", User: "root"}
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}

	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "SUDO_NOPASS=0\nDOCKER_DIRECT=0\nDOCKER_SUDO=0\n"}, nil
	}
	if got := app.checkSudo(runner, cfg); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "sudo") {
		t.Fatalf("expected sudo failure, got %+v", got)
	}

	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "DOCKER_VERSION=\nDOCKER_DAEMON=0\nCOMPOSE_OK=0\n"}, nil
	}
	if got := app.checkDocker(runner, cfg); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "docker is not installed") {
		t.Fatalf("expected docker missing failure, got %+v", got)
	}

	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "DOCKER_VERSION=26.1.0\nDOCKER_DAEMON=0\nCOMPOSE_OK=0\n"}, nil
	}
	if got := app.checkDocker(runner, cfg); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "daemon") {
		t.Fatalf("expected docker daemon failure, got %+v", got)
	}

	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "DOCKER_VERSION=26.1.0\nDOCKER_DAEMON=1\nCOMPOSE_OK=0\n"}, nil
	}
	if got := app.checkDocker(runner, cfg); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "compose") {
		t.Fatalf("expected compose failure, got %+v", got)
	}

	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "BACKUP_DIR=0\n"}, nil
	}
	if got := app.checkDeployFiles(runner, cfg, *srv); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "missing") {
		t.Fatalf("expected deploy file failure, got %+v", got)
	}

	proxySrv := *srv
	proxySrv.Role = model.ServerRoleVPN
	proxySrv.ProxyServiceUUID = "proxy-uuid"
	app.remoteExecHook = func(_ ssh.Config, _ time.Duration, cmd string) (ssh.Result, error) {
		if !strings.Contains(cmd, "sudo -n grep -q") {
			t.Fatalf("proxy identity check must read root-owned config via sudo, got %s", cmd)
		}
		return ssh.Result{Stdout: "PROXY_SERVICE_IDENTITY=0\n"}, nil
	}
	if got := app.checkProxyServiceRuntimeIdentity(runner, cfg, proxySrv); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "missing") {
		t.Fatalf("expected proxy identity failure, got %+v", got)
	}
}

func TestDoctorXrayConfigMountsAccessLogDir(t *testing.T) {
	app := newTestAppWithServer(t, false)
	runner := &ssh.Runner{DryRun: true}
	cfg := ssh.Config{Host: "example.com", User: "root"}

	var gotCmd string
	app.remoteExecHook = func(_ ssh.Config, _ time.Duration, cmd string) (ssh.Result, error) {
		gotCmd = cmd
		return ssh.Result{Stdout: "Configuration OK\n"}, nil
	}
	check := app.checkXrayConfig(runner, cfg)
	if check.Status != doctor.StatusPass {
		t.Fatalf("expected pass, got %+v", check)
	}
	for _, want := range []string{
		"XRAY_IMAGE=$(sudo -n sed -n 's/^XRAY_IMAGE=//p' ./.env",
		"sudo mkdir -p /opt/ovpn/logs",
		"sudo chown 65532:65532 /opt/ovpn/logs",
		"sudo chmod 770 /opt/ovpn/logs",
		"-v /opt/ovpn/logs:/var/log/ovpn",
		"tls_selfsni_cert_dir=${OVPN_TLS_SELFSNI_CERT_DIR:-/opt/ovpn/certs}",
		":/etc/xray/certs:ro",
	} {
		if !strings.Contains(gotCmd, want) {
			t.Fatalf("xray config check command missing %q: %s", want, gotCmd)
		}
	}
}

func TestDoctorRuntimeCheckAgentHealthBranches(t *testing.T) {
	app := newTestAppWithServer(t, false)
	runner := &ssh.Runner{DryRun: true}
	cfg := ssh.Config{Host: "example.com", User: "root"}
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}

	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		return nil, errors.New("agent unreachable")
	}
	if got := app.checkAgentHealth(runner, cfg, *srv); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "not reachable") {
		t.Fatalf("expected agent health failure, got %+v", got)
	}

	app.remoteHTTPHook = func(_ model.Server, _ string, url string, _ any) ([]byte, error) {
		if strings.Contains(url, "/health") {
			return []byte(`not-json`), nil
		}
		return []byte(`[]`), nil
	}
	if got := app.checkAgentHealth(runner, cfg, *srv); got.Status != doctor.StatusWarn || !strings.Contains(got.Message, "non-JSON") {
		t.Fatalf("expected non-json warning, got %+v", got)
	}

	reachable := false
	app.remoteHTTPHook = func(_ model.Server, _ string, url string, _ any) ([]byte, error) {
		if strings.Contains(url, "/health") {
			return []byte(`{"ok":true,"service":"ovpn-agent","xray_api":"xray:10085","xray_api_reachable":false}`), nil
		}
		return nil, errors.New("stats failed")
	}
	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "500"}, nil
	}
	if got := app.checkAgentHealth(runner, cfg, *srv); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "xray API") {
		t.Fatalf("expected xray API failure, got %+v reachable=%v", got, reachable)
	}
}

func TestDoctorDiskBranches(t *testing.T) {
	app := newTestAppWithServer(t, false)
	runner := &ssh.Runner{DryRun: true}
	cfg := ssh.Config{Host: "example.com", User: "root"}

	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{}, errors.New("df failed")
	}
	if got := app.checkDisk(runner, cfg); got.Status != doctor.StatusWarn || !strings.Contains(got.Message, "could not be completed") {
		t.Fatalf("expected disk command warning, got %+v", got)
	}

	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "DF=/,100,96,4,96%\nBACKUP_DIR=1\nAGENT_DB_PATH=/data/stats.db\nAGENT_DB_EXISTS=1\n"}, nil
	}
	if got := app.checkDisk(runner, cfg); got.Status != doctor.StatusFail || !strings.Contains(got.Message, "critical") {
		t.Fatalf("expected critical disk failure, got %+v", got)
	}

	app.remoteExecHook = func(ssh.Config, time.Duration, string) (ssh.Result, error) {
		return ssh.Result{Stdout: "DF=/,100,80,20,80%\nAGENT_DB_PATH=/data/stats.db\nAGENT_DB_EXISTS=0\nBACKUP_DIR=0\n"}, nil
	}
	if got := app.checkDisk(runner, cfg); got.Status != doctor.StatusWarn || !strings.Contains(strings.Join(got.Details, "\n"), "agent_db=/data/stats.db") {
		t.Fatalf("expected missing db/backup warning, got %+v", got)
	}
}

func successfulDoctorExecHook(_ ssh.Config, _ time.Duration, cmd string) (ssh.Result, error) {
	switch {
	case strings.Contains(cmd, "HOSTNAME="):
		return ssh.Result{Stdout: "HOSTNAME=vpn-1\nREMOTE_USER=root\nKERNEL=Linux 6.1 x86_64\nOS=Debian GNU/Linux 12\n"}, nil
	case strings.Contains(cmd, "SUDO_NOPASS"):
		return ssh.Result{Stdout: "SUDO_NOPASS=1\nDOCKER_BIN=1\nDOCKER_DIRECT=0\nDOCKER_SUDO=1\nGROUPS=root docker\n"}, nil
	case strings.Contains(cmd, "DOCKER_VERSION"):
		return ssh.Result{Stdout: "DOCKER_VERSION=26.1.0/26.1.0\nDOCKER_DAEMON=1\nCOMPOSE_OK=1\nCOMPOSE_VERSION=Docker Compose version v2.27.0\n"}, nil
	case strings.Contains(cmd, "EXISTS_"):
		keys := []string{
			deploy.RemoteDir,
			deploy.RemoteDir + "/docker-compose.yml",
			deploy.RemoteDir + "/.env",
			deploy.RemoteDir + "/xray/config.json",
			deploy.RemoteDir + "/agent/ovpn-agent",
		}
		var b strings.Builder
		for _, key := range keys {
			b.WriteString("EXISTS_" + sanitizeKey(key) + "=1\n")
		}
		b.WriteString("OVPN_OWNER=root:root OVPN_MODE=755\nBACKUP_DIR=1\n")
		return ssh.Result{Stdout: b.String()}, nil
	case strings.Contains(cmd, "config -q"):
		return ssh.Result{}, nil
	case strings.Contains(cmd, "ps --all --format json"):
		return ssh.Result{Stdout: `[{"Service":"xray","State":"running","Status":"Up 1m"},{"Service":"ovpn-agent","State":"running","Status":"Up 1m"}]`}, nil
	case strings.Contains(cmd, "run -test"):
		return ssh.Result{Stdout: "Configuration OK\n"}, nil
	case strings.Contains(cmd, "runtime/user/add"):
		return ssh.Result{Stdout: "405"}, nil
	case strings.Contains(cmd, "df -Pk"):
		return ssh.Result{Stdout: "DF=/,100000,10000,90000,10%\nDF=/opt/ovpn,100000,20000,80000,20%\nBACKUP_DIR=1\nAGENT_DB_PATH=/var/lib/docker/volumes/ovpn-agent/stats.db\nAGENT_DB_EXISTS=1\n"}, nil
	case strings.Contains(cmd, "logs --tail"):
		return ssh.Result{Stdout: "log line\n"}, nil
	default:
		return ssh.Result{}, nil
	}
}
