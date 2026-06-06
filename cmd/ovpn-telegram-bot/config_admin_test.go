package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingServiceOperator struct {
	restarts []string
	err      error
}

func (o *recordingServiceOperator) Restart(_ context.Context, service string) error {
	o.restarts = append(o.restarts, service)
	return o.err
}

func TestLoadConfigAndSecretFileBranches(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("token\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	linkPath := filepath.Join(t.TempDir(), "link.json")
	if err := os.WriteFile(linkPath, []byte(`{"address":"example.com","server_name":"www.microsoft.com","public_key":"pub","short_id":"abcd"}`), 0o600); err != nil {
		t.Fatalf("write link: %v", err)
	}
	resetFlags := replaceCommandLine(t, []string{
		"ovpn-telegram-bot",
		"--telegram-token-file", tokenPath,
		"--link-config-file", linkPath,
		"--notify-chat-ids", " 42,42,43 ",
		"--telegram-api-fallback-ips", "149.154.167.220,149.154.166.110",
		"--log-level", "debug",
	})
	defer resetFlags()
	t.Setenv("OVPN_TELEGRAM_POLL_INTERVAL", "2s")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.ownerUserID != 42 || cfg.pollInterval.String() != "2s" || cfg.logLevel != "debug" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if got, err := readSecretFile(tokenPath); err != nil || got != "token" {
		t.Fatalf("read secret got %q err=%v", got, err)
	}
	if got, err := readOptionalSecretFile(filepath.Join(t.TempDir(), "missing")); err != nil || got != "" {
		t.Fatalf("optional missing got %q err=%v", got, err)
	}
	emptyPath := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(emptyPath, []byte(" \n"), 0o600); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	if _, err := readSecretFile(emptyPath); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty token error, got %v", err)
	}
	if got := envOr("OVPN_DOES_NOT_EXIST_FOR_TEST", "fallback"); got != "fallback" {
		t.Fatalf("env fallback got %q", got)
	}
}

func TestLoadConfigRejectsBadPollIntervalAndFallbackIP(t *testing.T) {
	resetFlags := replaceCommandLine(t, []string{"ovpn-telegram-bot", "--telegram-api-fallback-ips", "bad"})
	defer resetFlags()
	t.Setenv("OVPN_TELEGRAM_POLL_INTERVAL", "500ms")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "poll interval") {
		t.Fatalf("expected short poll interval error, got %v", err)
	}

	resetFlags = replaceCommandLine(t, []string{"ovpn-telegram-bot", "--telegram-api-fallback-ips", "bad"})
	defer resetFlags()
	t.Setenv("OVPN_TELEGRAM_POLL_INTERVAL", "2s")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "not an IP") {
		t.Fatalf("expected invalid fallback IP error, got %v", err)
	}
}

func TestAdminActionsConfirmRestartHealAndFailures(t *testing.T) {
	t.Parallel()

	rec := &telegramRecorder{}
	b := newBotTestHarness(t, rec, false)
	op := &recordingServiceOperator{}
	b.adminToken = "enabled"
	b.operator = op

	if err := b.beginHealConfirm(context.Background(), 11); err != nil {
		t.Fatalf("begin heal: %v", err)
	}
	if st, ok := b.getConfirm(11); !ok || st.Kind != "heal" {
		t.Fatalf("expected heal confirm, got %+v ok=%v", st, ok)
	}
	if err := b.executeConfirm(context.Background(), 11); err != nil {
		t.Fatalf("execute heal: %v", err)
	}

	if err := b.beginRestartConfirm(context.Background(), 11, "xray"); err != nil {
		t.Fatalf("begin restart: %v", err)
	}
	if err := b.executeConfirm(context.Background(), 11); err != nil {
		t.Fatalf("execute restart: %v", err)
	}
	if len(op.restarts) == 0 || op.restarts[len(op.restarts)-1] != "xray" {
		t.Fatalf("expected xray restart, got %+v", op.restarts)
	}

	if err := b.beginRestartConfirm(context.Background(), 11, "bad"); err != nil {
		t.Fatalf("bad restart target should send message, got %v", err)
	}
	b.setConfirm(11, "unknown", nil)
	if err := b.executeConfirm(context.Background(), 11); err != nil {
		t.Fatalf("unknown confirm should send message, got %v", err)
	}
	b.setConfirm(11, "restart", nil)
	if err := b.executeConfirm(context.Background(), 11); err != nil {
		t.Fatalf("empty restart confirm should send message, got %v", err)
	}
	if got := restartOrderWeight("not-real"); got != 100 {
		t.Fatalf("unexpected unknown restart weight: %d", got)
	}
}

func replaceCommandLine(t *testing.T, args []string) func() {
	t.Helper()
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	os.Args = append([]string(nil), args...)
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	return func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	}
}
