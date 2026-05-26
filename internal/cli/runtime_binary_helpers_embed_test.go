//go:build runtimeassets

package cli

import (
	"os"
	"strings"
	"testing"
)

func TestEnsureRuntimeBinaryUsesEmbeddedAssetWithoutSource(t *testing.T) {
	t.Setenv("OVPN_AGENT_GOARCH", "amd64")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)

	got, err := (&App{repoRoot: t.TempDir()}).ensureAgentBinary()
	if err != nil {
		t.Fatalf("ensure embedded agent binary: %v", err)
	}
	if !strings.Contains(got, "runtime-assets") || !strings.Contains(got, "linux_amd64") {
		t.Fatalf("expected embedded runtime cache path, got %q", got)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat embedded agent binary: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("expected embedded agent file, got directory")
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected embedded agent file to be executable, got mode %o", info.Mode().Perm())
	}
}

func TestEnsureTelegramBotBinaryUsesEmbeddedAssetWithoutSource(t *testing.T) {
	t.Setenv("OVPN_TELEGRAM_BOT_GOARCH", "arm64")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home)

	got, err := (&App{repoRoot: t.TempDir()}).ensureTelegramBotBinary()
	if err != nil {
		t.Fatalf("ensure embedded telegram bot binary: %v", err)
	}
	if !strings.Contains(got, "runtime-assets") || !strings.Contains(got, "linux_arm64") {
		t.Fatalf("expected embedded runtime cache path, got %q", got)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat embedded telegram bot binary: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("expected embedded telegram bot file, got directory")
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected embedded telegram bot file to be executable, got mode %o", info.Mode().Perm())
	}
}
