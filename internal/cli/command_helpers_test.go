package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestInitOrDeployServerDryRunCompletesWorkflow(t *testing.T) {
	app := newTestAppWithServer(t, true)
	app.logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		return nil, errors.New("dry-run deploy must not call remote agent HTTP")
	}
	setRuntimeBinaryOverrides(t)

	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	now := time.Now().UTC()
	srv.LastDeployAt = &now
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("mark server deployed: %v", err)
	}
	if err := app.store.AddUser(app.ctx, &model.User{
		ServerID:     srv.ID,
		Username:     "alice",
		UUID:         "11111111-1111-1111-1111-111111111111",
		Email:        "alice@global",
		Enabled:      true,
		QuotaEnabled: true,
	}); err != nil {
		t.Fatalf("add user: %v", err)
	}

	if err := app.initOrDeployServer(*srv, false); err != nil {
		t.Fatalf("deploy dry-run: %v", err)
	}
	updated, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get updated server: %v", err)
	}
	if updated.LastDeployAt == nil {
		t.Fatalf("expected last deploy timestamp")
	}
}

func TestUserMutationCommandsExerciseRuntimeAndPolicySync(t *testing.T) {
	app := newTestAppWithServer(t, true)
	app.remoteHTTPHook = successfulAgentHTTPHook(t)

	runUserCommand(t, app, "add", "--username", "alice", "--uuid", "11111111-1111-1111-1111-111111111111", "--expiry", "2099-01-02", "--quota-bytes", "12345", "--notes", "primary", "--tags", "team-a, paid")
	runUserCommand(t, app, "disable", "--username", "alice")
	runUserCommand(t, app, "enable", "--username", "alice")
	runUserCommand(t, app, "expiry-set", "--username", "alice", "--date", "2099-02-03")
	runUserCommand(t, app, "expiry-clear", "--username", "alice")
	runUserCommand(t, app, "quota-set", "--username", "alice", "--monthly-gb", "400", "--enabled=false")
	runUserCommand(t, app, "quota-reset", "--username", "alice")
	runUserCommand(t, app, "rm", "--username", "alice")

	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if _, err := app.store.GetUser(app.ctx, srv.ID, "alice"); err == nil || !isNotFoundErr(err) {
		t.Fatalf("expected alice to be removed, got %v", err)
	}
}

func TestUserReconcileDryRunAndApply(t *testing.T) {
	app := newTestAppWithServer(t, false)
	ctx := app.ctx

	target := model.Server{
		Name:              "target",
		Host:              "127.0.0.2",
		Domain:            "target.example.com",
		SSHUser:           "root",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd1234",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
	if err := app.store.AddServer(ctx, &target); err != nil {
		t.Fatalf("add target server: %v", err)
	}
	disabled := target
	disabled.ID = 0
	disabled.Name = "disabled"
	disabled.Host = "127.0.0.3"
	disabled.Enabled = false
	if err := app.store.AddServer(ctx, &disabled); err != nil {
		t.Fatalf("add disabled server: %v", err)
	}
	source, err := app.store.GetServerByName(ctx, "main")
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	quota := int64(1024)
	expiry := time.Date(2099, 1, 2, 0, 0, 0, 0, time.UTC)
	if err := app.store.AddUser(ctx, &model.User{
		ServerID:         source.ID,
		Username:         "alice",
		UUID:             "11111111-1111-1111-1111-111111111111",
		Email:            "alice@global",
		Enabled:          true,
		ExpiryDate:       &expiry,
		TrafficLimitByte: &quota,
		QuotaEnabled:     true,
		QuotaBlocked:     true,
		QuotaBlockedAt:   &expiry,
		Notes:            "source note",
		TagsCSV:          "b,a",
	}); err != nil {
		t.Fatalf("add source user: %v", err)
	}
	if err := app.store.AddUser(ctx, &model.User{
		ServerID:     target.ID,
		Username:     "alice",
		UUID:         "22222222-2222-2222-2222-222222222222",
		Email:        "old@global",
		Enabled:      false,
		QuotaEnabled: false,
		Notes:        "old",
		TagsCSV:      "c",
	}); err != nil {
		t.Fatalf("add target alice: %v", err)
	}
	if err := app.store.AddUser(ctx, &model.User{
		ServerID: target.ID,
		Username: "orphan",
		UUID:     "33333333-3333-3333-3333-333333333333",
		Email:    "orphan@global",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("add target orphan: %v", err)
	}

	dryRunCmd := app.userCmd()
	dryRunCmd.SetArgs([]string{"reconcile", "--from-server", "main", "--to-server", "target"})
	stdout, _, err := captureStdoutStderr(t, dryRunCmd.Execute)
	if err != nil {
		t.Fatalf("reconcile dry-run: %v", err)
	}
	for _, want := range []string{"reconcile plan", "- update alice on target", "- delete orphan on target", "dry-run mode"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in dry-run output:\n%s", want, stdout)
		}
	}

	applyCmd := app.userCmd()
	applyCmd.SetArgs([]string{"reconcile", "--from-server", "main", "--to-server", "target", "--apply"})
	if err := applyCmd.Execute(); err != nil {
		t.Fatalf("reconcile apply: %v", err)
	}
	got, err := app.store.GetUser(ctx, target.ID, "alice")
	if err != nil {
		t.Fatalf("get reconciled alice: %v", err)
	}
	if got.UUID != "11111111-1111-1111-1111-111111111111" || got.Email != "alice@global" || !got.QuotaEnabled || !got.QuotaBlocked {
		t.Fatalf("unexpected reconciled user: %+v", got)
	}
	if _, err := app.store.GetUser(ctx, target.ID, "orphan"); err == nil || !isNotFoundErr(err) {
		t.Fatalf("expected orphan removal, got %v", err)
	}

	targets, err := app.reconcileTargets(*source, "", false)
	if err != nil {
		t.Fatalf("reconcile enabled targets: %v", err)
	}
	if len(targets) != 1 || targets[0].Name != "target" {
		t.Fatalf("expected only enabled target, got %+v", targets)
	}
	targets, err = app.reconcileTargets(*source, "", true)
	if err != nil {
		t.Fatalf("reconcile all targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected enabled+disabled targets with --all, got %+v", targets)
	}
}

func TestServerAndConfigCommandsUseLocalStateAndDryRunSSH(t *testing.T) {
	app := newTestAppWithServer(t, true)
	runServerCommand(t, app, "list")
	runServerCommand(t, app, "set-xray-version", "main", "v26.4.1")
	runServerCommand(t, app, "status", "main")
	runServerCommand(t, app, "logs", "main", "--service", "xray", "--tail", "5")
	runServerCommand(t, app, "monitor", "up", "main")
	runServerCommand(t, app, "monitor", "down", "main")
	runServerCommand(t, app, "monitor", "status", "main")

	renderCmd := app.configCmd()
	renderCmd.SetArgs([]string{"render", "--server", "main"})
	if err := renderCmd.Execute(); err != nil {
		t.Fatalf("config render: %v", err)
	}

	t.Setenv("PATH", t.TempDir())
	validateCmd := app.configCmd()
	validateCmd.SetArgs([]string{"validate", "--server", "main"})
	if err := validateCmd.Execute(); err != nil {
		t.Fatalf("config validate: %v", err)
	}
}

func TestRemoteHTTPHookAndReadyLoop(t *testing.T) {
	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	attempts := 0
	var slept []time.Duration
	app.sleepHook = func(d time.Duration) { slept = append(slept, d) }
	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		attempts++
		if attempts < 2 {
			return nil, errors.New("not ready")
		}
		return []byte(`{"ok":true}`), nil
	}
	if err := app.waitForRemoteHTTPReady(*srv, app.agentURL("/health"), time.Second); err != nil {
		t.Fatalf("wait for ready: %v", err)
	}
	if attempts != 2 || len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("unexpected attempts=%d slept=%v", attempts, slept)
	}
}

func runUserCommand(t *testing.T, app *App, args ...string) {
	t.Helper()
	cmd := app.userCmd()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("user %s: %v", strings.Join(args, " "), err)
	}
}

func runServerCommand(t *testing.T, app *App, args ...string) {
	t.Helper()
	cmd := app.serverCmd()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("server %s: %v", strings.Join(args, " "), err)
	}
}

func setRuntimeBinaryOverrides(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, spec := range []struct {
		env  string
		name string
	}{
		{env: "OVPN_AGENT_BINARY", name: "ovpn-agent"},
		{env: "OVPN_TELEGRAM_BOT_BINARY", name: "ovpn-telegram-bot"},
	} {
		path := filepath.Join(dir, spec.name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write runtime override: %v", err)
		}
		t.Setenv(spec.env, path)
	}
}

func successfulAgentHTTPHook(t *testing.T) func(model.Server, string, string, any) ([]byte, error) {
	t.Helper()
	return func(_ model.Server, method, url string, payload any) ([]byte, error) {
		if strings.TrimSpace(method) == "" {
			return nil, fmt.Errorf("method is required")
		}
		switch {
		case strings.Contains(url, "/quota/status"):
			return []byte(`{"users":[]}`), nil
		case strings.Contains(url, "/stats/total"):
			return []byte(`[{"email":"alice@global","uplink_bytes":10,"downlink_bytes":20}]`), nil
		case strings.Contains(url, "/runtime/user/"), strings.Contains(url, "/quota/reset"), strings.Contains(url, "/users/sync"), strings.Contains(url, "/quota/sync"):
			if payload == nil && !strings.Contains(url, "/runtime/user/remove") {
				return nil, fmt.Errorf("expected payload for %s", url)
			}
			if _, err := json.Marshal(payload); err != nil {
				return nil, err
			}
			return []byte(`{"ok":true}`), nil
		default:
			return []byte(`{"ok":true}`), nil
		}
	}
}
