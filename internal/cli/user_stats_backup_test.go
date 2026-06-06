package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestUserTopCommandPrintsQuotaAwareRowsAndEmptyState(t *testing.T) {
	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if err := app.store.AddUser(app.ctx, &model.User{
		ServerID: srv.ID,
		Username: "alice",
		Email:    "alice@global",
		UUID:     "11111111-1111-1111-1111-111111111111",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("add user: %v", err)
	}
	app.remoteHTTPHook = func(_ model.Server, method, url string, payload any) ([]byte, error) {
		if method != "GET" || payload != nil {
			t.Fatalf("unexpected hook call method=%s payload=%v", method, payload)
		}
		switch {
		case strings.Contains(url, "/stats/total"):
			return []byte(`[{"email":"alice@global","uplink_bytes":10,"downlink_bytes":20}]`), nil
		case strings.Contains(url, "/quota/status"):
			return []byte(`{"users":[{"email":"alice@global","quota_enabled":true,"window_30d_usage_byte":30,"window_30d_quota_byte":60,"blocked_by_quota":true}]}`), nil
		default:
			t.Fatalf("unexpected url %s", url)
			return nil, nil
		}
	}

	stdout, _, err := captureStdoutStderr(t, func() error {
		cmd := app.userCmd()
		cmd.SetArgs([]string{"top", "--server", "main", "--limit", "1"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("user top: %v", err)
	}
	for _, want := range []string{"rank\tusername\temail", "1\talice\talice@global\t30", "50.0\ttrue"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in user top output:\n%s", want, stdout)
		}
	}

	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		return []byte(`[]`), nil
	}
	stdout, _, err = captureStdoutStderr(t, func() error {
		cmd := app.userCmd()
		cmd.SetArgs([]string{"top", "--server", "main"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("empty user top: %v", err)
	}
	if !strings.Contains(stdout, "no traffic rows") {
		t.Fatalf("unexpected empty top output: %s", stdout)
	}
}

func TestUserListAndShowIncludeQuotaAndRuntimeErrors(t *testing.T) {
	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if err := app.store.AddUser(app.ctx, &model.User{
		ServerID:     srv.ID,
		Username:     "alice",
		Email:        "alice@global",
		UUID:         "11111111-1111-1111-1111-111111111111",
		Enabled:      true,
		QuotaEnabled: true,
		Notes:        "paid",
		TagsCSV:      "team-a",
	}); err != nil {
		t.Fatalf("add user: %v", err)
	}
	app.remoteHTTPHook = func(_ model.Server, _ string, url string, _ any) ([]byte, error) {
		switch {
		case strings.Contains(url, "alice%40global"):
			return []byte(`{"users":[{"email":"alice@global","quota_enabled":true,"window_30d_usage_byte":30,"window_30d_quota_byte":60,"blocked_by_quota":false}]}`), nil
		case strings.Contains(url, "/quota/status"):
			return nil, os.ErrPermission
		default:
			return []byte(`{}`), nil
		}
	}

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		cmd := app.userCmd()
		cmd.SetArgs([]string{"list", "--server", "main"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("user list: %v", err)
	}
	if !strings.Contains(stdout, "alice") || !strings.Contains(stderr, "quota runtime unavailable") {
		t.Fatalf("unexpected list stdout=%q stderr=%q", stdout, stderr)
	}

	stdout, _, err = captureStdoutStderr(t, func() error {
		cmd := app.userCmd()
		cmd.SetArgs([]string{"show", "--server", "main", "--username", "alice"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("user show: %v", err)
	}
	for _, want := range []string{`"username": "alice"`, `"quota_summary"`, `"notes": "paid"`, `"tags": "team-a"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in user show output:\n%s", want, stdout)
		}
	}
}

func TestUserDiagnoseAndDebugCommands(t *testing.T) {
	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if err := app.store.AddUser(app.ctx, &model.User{
		ServerID: srv.ID,
		Username: "alice",
		Email:    "alice@global",
		UUID:     "11111111-1111-1111-1111-111111111111",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("add user: %v", err)
	}
	app.remoteHTTPHook = func(_ model.Server, method, url string, payload any) ([]byte, error) {
		switch {
		case method == "GET" && strings.Contains(url, "/diagnostics/user"):
			if !strings.Contains(url, "alice%40global") || !strings.Contains(url, "since=24h") {
				t.Fatalf("unexpected diagnose url %s", url)
			}
			return []byte(`{
				"email":"alice@global",
				"username":"alice",
				"user":{"username":"alice","email":"alice@global","enabled":true,"effective_enabled":true,"window_30d_usage_byte":1073741824,"window_30d_quota_byte":2147483648},
				"traffic_windows":[{"window":"24h","uplink_bytes":10,"downlink_bytes":20,"total_bytes":30}],
				"connections":{"email":"alice@global","accepted_count":2,"rejected_count":1,"approx_source_networks":2,"destination_ipv6_count":1,"top_ports":[{"port":443,"count":2}]},
				"hints":["shared UUID; source networks are not exact device count"]
			}`), nil
		case method == "POST" && strings.Contains(url, "/diagnostics/debug/start"):
			body := payload.(map[string]string)
			if body["email"] != "alice@global" || body["duration"] != "15m" {
				t.Fatalf("unexpected start payload: %+v", payload)
			}
			return []byte(`{"ok":true,"session":{"email":"alice@global","started_at":"2026-06-05T12:00:00Z","expires_at":"2026-06-05T12:15:00Z"}}`), nil
		case method == "GET" && strings.Contains(url, "/diagnostics/debug/sessions"):
			return []byte(`{"time":"2026-06-05T12:01:00Z","sessions":[{"email":"alice@global","started_at":"2026-06-05T12:00:00Z","expires_at":"2026-06-05T12:15:00Z"}]}`), nil
		case method == "GET" && strings.Contains(url, "/diagnostics/debug/events"):
			return []byte(`{"email":"alice@global","events":[{"timestamp":"2026-06-05T12:01:00Z","result":"accepted","source_network":"198.51.100.0/24","destination":"api.telegram.org","destination_port":443,"destination_family":"domain"}]}`), nil
		case method == "POST" && strings.Contains(url, "/diagnostics/debug/stop"):
			return []byte(`{"ok":true}`), nil
		default:
			t.Fatalf("unexpected hook call method=%s url=%s payload=%v", method, url, payload)
			return nil, nil
		}
	}

	stdout, _, err := captureStdoutStderr(t, func() error {
		cmd := app.userCmd()
		cmd.SetArgs([]string{"diagnose", "--server", "main", "--username", "alice"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("user diagnose: %v", err)
	}
	for _, want := range []string{"user: alice", "quota: 50.0%", "accepted=2 rejected=1", "top_ports=443=2", "shared UUID"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in diagnose output:\n%s", want, stdout)
		}
	}
	for _, args := range [][]string{
		{"debug", "start", "--server", "main", "--username", "alice"},
		{"debug", "list", "--server", "main"},
		{"debug", "show", "--server", "main", "--username", "alice"},
		{"debug", "stop", "--server", "main", "--username", "alice"},
	} {
		if _, _, err := captureStdoutStderr(t, func() error {
			cmd := app.userCmd()
			cmd.SetArgs(args)
			return cmd.Execute()
		}); err != nil {
			t.Fatalf("user %v: %v", args, err)
		}
	}
}

func TestServerBackupRestoreCommandsUseDryRunRunner(t *testing.T) {
	app := newTestAppWithServer(t, true)
	app.dataDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(app.dataDir, "state.txt"), []byte("state"), 0o600); err != nil {
		t.Fatalf("seed data dir: %v", err)
	}

	stdout, _, err := captureStdoutStderr(t, func() error {
		cmd := app.serverCmd()
		cmd.SetArgs([]string{"backup", "main"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("server backup: %v", err)
	}
	if !strings.Contains(stdout, "remote backup: /opt/ovpn-backups/main-") || !strings.Contains(stdout, "local backup:") {
		t.Fatalf("unexpected backup output:\n%s", stdout)
	}
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	records, err := app.store.ListBackupRecords(app.ctx, srv.ID)
	if err != nil {
		t.Fatalf("list backup records: %v", err)
	}
	if len(records) != 1 || records[0].RemotePath == "" || records[0].SHA256 == "" {
		t.Fatalf("unexpected backup records: %+v", records)
	}

	stdout, _, err = captureStdoutStderr(t, func() error {
		cmd := app.serverCmd()
		cmd.SetArgs([]string{"restore", "main", "--remote-path", records[0].RemotePath})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("server restore: %v", err)
	}
	if !strings.Contains(stdout, "restore complete") {
		t.Fatalf("unexpected restore output:\n%s", stdout)
	}

	cmd := app.serverCmd()
	cmd.SetArgs([]string{"restore", "main"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--remote-path is required") {
		t.Fatalf("expected missing remote path error, got %v", err)
	}
}

func TestRuntimeBinaryOverrideAndArchValidationBranches(t *testing.T) {
	app := &App{}
	spec := runtimeBinarySpec{Name: "ovpn-agent", Override: "OVPN_AGENT_BINARY", ArchEnv: "OVPN_AGENT_GOARCH"}
	if _, err := app.validateRuntimeBinaryOverride(spec, "relative/path"); err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("expected relative override error, got %v", err)
	}
	if _, err := app.validateRuntimeBinaryOverride(spec, filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "missing file") {
		t.Fatalf("expected missing override error, got %v", err)
	}
	dir := t.TempDir()
	if _, err := app.validateRuntimeBinaryOverride(spec, dir); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected directory override error, got %v", err)
	}
	bin := filepath.Join(t.TempDir(), "ovpn-agent")
	if err := os.WriteFile(bin, []byte("bin"), 0o755); err != nil {
		t.Fatalf("write override bin: %v", err)
	}
	if got, err := app.validateRuntimeBinaryOverride(spec, bin); err != nil || got != bin {
		t.Fatalf("expected valid override path %q, got %q err=%v", bin, got, err)
	}
	if got, err := normalizedRuntimeGOARCH("", "OVPN_AGENT_GOARCH"); err != nil || got != "amd64" {
		t.Fatalf("unexpected default arch %q err=%v", got, err)
	}
	if _, err := normalizedRuntimeGOARCH("badarch", "OVPN_AGENT_GOARCH"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported arch error, got %v", err)
	}
	if got := runtimeBinaryCacheDir("arm64"); !strings.Contains(got, "linux_arm64") {
		t.Fatalf("unexpected runtime cache dir: %s", got)
	}
}
