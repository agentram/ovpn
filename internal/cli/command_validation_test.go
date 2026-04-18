package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

func TestDeployCommandRequiresServerArg(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).deployCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts 1 arg(s)") {
		t.Fatalf("expected missing arg error, got %v", err)
	}
}

func TestRestartCommandRequiresServerArg(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).restartCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts 1 arg(s)") {
		t.Fatalf("expected missing arg error, got %v", err)
	}
}

func TestServerRestoreRequiresRemotePathFlag(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"restore", "main"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--remote-path is required") {
		t.Fatalf("expected remote-path validation error, got %v", err)
	}
}

func TestServerLogsRejectsInvalidTailBeforeStoreLookup(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"logs", "main", "--tail", "0"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--tail must be > 0") {
		t.Fatalf("expected tail validation error, got %v", err)
	}
}

func TestServerLogsRejectsUnsupportedServiceBeforeStoreLookup(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"logs", "main", "--service", "bad"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unsupported --service") {
		t.Fatalf("expected service validation error, got %v", err)
	}
}

func TestServerMonitorUpRequiresServerArg(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"monitor", "up"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts 1 arg(s)") {
		t.Fatalf("expected missing arg error, got %v", err)
	}
}

func TestServerMonitorTelegramSetupRequiresTokenBeforeStoreLookup(t *testing.T) {
	clearTelegramSetupEnv(t)

	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"monitor", "telegram-setup", "main", "--owner-user-id", "123"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "telegram token is required") {
		t.Fatalf("expected missing token validation error, got %v", err)
	}
}

func TestServerMonitorTelegramSetupRequiresOwnerBeforeStoreLookup(t *testing.T) {
	clearTelegramSetupEnv(t)
	t.Setenv("OVPN_TELEGRAM_BOT_TOKEN", "token")
	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"monitor", "telegram-setup", "main"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "owner user id is required when notify chat ids are empty") {
		t.Fatalf("expected owner required error, got %v", err)
	}
}

func TestServerMonitorTelegramSetupUsesNotifyChatIDsAsOwnerFallback(t *testing.T) {
	clearTelegramSetupEnv(t)
	t.Setenv("OVPN_TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("OVPN_TELEGRAM_NOTIFY_CHAT_IDS", "123")
	app := newTestAppWithoutServers(t, true)
	cmd := app.serverCmd()
	cmd.SetArgs([]string{"monitor", "telegram-setup", "main"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("expected server lookup error after owner fallback, got %v", err)
	}
}

func TestServerMonitorTelegramSetupRejectsInvalidNotifyChatIDsBeforeStoreLookup(t *testing.T) {
	clearTelegramSetupEnv(t)
	t.Setenv("OVPN_TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("OVPN_TELEGRAM_OWNER_USER_ID", "123")
	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"monitor", "telegram-setup", "main", "--notify-chat-ids", "bad"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid notify chat ids") {
		t.Fatalf("expected invalid notify chat ids error, got %v", err)
	}
}

func TestServerMonitorTelegramSetupRejectsInvalidOwnerIDBeforeStoreLookup(t *testing.T) {
	clearTelegramSetupEnv(t)
	t.Setenv("OVPN_TELEGRAM_BOT_TOKEN", "token")
	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"monitor", "telegram-setup", "main", "--owner-user-id", "abc"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid owner user id") {
		t.Fatalf("expected invalid owner id error, got %v", err)
	}
}

func TestServerMonitorTelegramSetupRejectsMultipleOwnerIDsBeforeStoreLookup(t *testing.T) {
	clearTelegramSetupEnv(t)
	t.Setenv("OVPN_TELEGRAM_BOT_TOKEN", "token")
	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"monitor", "telegram-setup", "main", "--owner-user-id", "1,2"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "exactly one numeric telegram user id") {
		t.Fatalf("expected single owner id validation error, got %v", err)
	}
}

func TestServerCleanupRequiresConfirmationOutsideDryRun(t *testing.T) {
	t.Parallel()

	app := newTestAppWithServer(t, false)
	cmd := app.serverCmd()
	cmd.SetArgs([]string{"cleanup", "main", "--skip-backup-check"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "pass --confirm CLEANUP") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
}

func TestServerCleanupDryRunAllowsMissingConfirmAndKeepsLocalMetadata(t *testing.T) {
	t.Parallel()

	app := newTestAppWithServer(t, true)
	cmd := app.serverCmd()
	cmd.SetArgs([]string{"cleanup", "main", "--remove-local", "--skip-backup-check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup dry-run: %v", err)
	}
	if _, err := app.store.GetServerByName(app.ctx, "main"); err != nil {
		t.Fatalf("server should remain in local state during dry-run: %v", err)
	}
}

func TestFinalizeCleanupLocalStateDisablesServerWhenKeepingMetadata(t *testing.T) {
	t.Parallel()

	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if err := app.finalizeCleanupLocalState(srv, false); err != nil {
		t.Fatalf("finalize cleanup local state: %v", err)
	}
	updated, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("load updated server: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("expected server to be disabled after cleanup keep-local path")
	}
}

func TestFinalizeCleanupLocalStateRemovesServerWhenRequested(t *testing.T) {
	t.Parallel()

	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if err := app.finalizeCleanupLocalState(srv, true); err != nil {
		t.Fatalf("finalize cleanup local remove: %v", err)
	}
	if _, err := app.store.GetServerByName(app.ctx, "main"); err == nil {
		t.Fatalf("expected server to be removed")
	}
}

func newTestAppWithServer(t *testing.T, dryRun bool) *App {
	t.Helper()

	ctx := context.Background()
	st, err := local.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open local store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	srv := &model.Server{
		Name:              "main",
		Host:              "127.0.0.1",
		Domain:            "example.com",
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
	if err := st.AddServer(ctx, srv); err != nil {
		t.Fatalf("add server: %v", err)
	}
	return &App{
		ctx:    ctx,
		store:  st,
		dryRun: dryRun,
	}
}

func newTestAppWithoutServers(t *testing.T, dryRun bool) *App {
	t.Helper()

	ctx := context.Background()
	st, err := local.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open local store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return &App{
		ctx:    ctx,
		store:  st,
		dryRun: dryRun,
	}
}

func clearTelegramSetupEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OVPN_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("OVPN_TELEGRAM_BOT_TOKEN_FILE", "")
	t.Setenv("OVPN_TELEGRAM_NOTIFY_CHAT_IDS", "")
	t.Setenv("OVPN_TELEGRAM_OWNER_USER_ID", "")
}
