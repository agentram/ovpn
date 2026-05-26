package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureRuntimeBinaryUsesOverride(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "ovpn-agent")
	if err := os.WriteFile(bin, []byte("agent"), 0o755); err != nil {
		t.Fatalf("write override binary: %v", err)
	}
	t.Setenv("OVPN_AGENT_BINARY", bin)
	t.Setenv("OVPN_AGENT_GOARCH", "bad-arch")

	got, err := (&App{repoRoot: t.TempDir()}).ensureAgentBinary()
	if err != nil {
		t.Fatalf("ensure agent binary: %v", err)
	}
	if got != bin {
		t.Fatalf("expected override path %q, got %q", bin, got)
	}
}

func TestEnsureRuntimeBinaryUsesTelegramBotOverride(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "ovpn-telegram-bot")
	if err := os.WriteFile(bin, []byte("bot"), 0o755); err != nil {
		t.Fatalf("write override binary: %v", err)
	}
	t.Setenv("OVPN_TELEGRAM_BOT_BINARY", bin)
	t.Setenv("OVPN_TELEGRAM_BOT_GOARCH", "bad-arch")

	got, err := (&App{repoRoot: t.TempDir()}).ensureTelegramBotBinary()
	if err != nil {
		t.Fatalf("ensure telegram bot binary: %v", err)
	}
	if got != bin {
		t.Fatalf("expected override path %q, got %q", bin, got)
	}
}

func TestEnsureRuntimeBinaryRejectsRelativeOverride(t *testing.T) {
	t.Setenv("OVPN_AGENT_BINARY", "relative/ovpn-agent")

	_, err := (&App{repoRoot: t.TempDir()}).ensureAgentBinary()
	if err == nil || !strings.Contains(err.Error(), "OVPN_AGENT_BINARY must be an absolute path") {
		t.Fatalf("expected absolute override error, got %v", err)
	}
}

func TestEnsureRuntimeBinaryRejectsUnsupportedArch(t *testing.T) {
	t.Setenv("OVPN_AGENT_GOARCH", "bad-arch")

	_, err := (&App{repoRoot: t.TempDir()}).ensureAgentBinary()
	if err == nil || !strings.Contains(err.Error(), `unsupported OVPN_AGENT_GOARCH: "bad-arch"`) {
		t.Fatalf("expected unsupported arch error, got %v", err)
	}
}

func TestEnsureRuntimeBinaryExplainsMissingEmbeddedAndSource(t *testing.T) {
	t.Setenv("OVPN_AGENT_GOARCH", "ppc64")

	_, err := (&App{repoRoot: t.TempDir()}).ensureAgentBinary()
	if err == nil {
		t.Fatal("expected missing runtime binary error")
	}
	for _, want := range []string{"no embedded asset", "OVPN_AGENT_BINARY", "OVPN_REPO_ROOT"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %v", want, err)
		}
	}
}

func TestEnsureRuntimeBinaryFallsBackToSourceBuild(t *testing.T) {
	root := testRepoRoot(t)
	fakeBin := t.TempDir()
	fakeGo := filepath.Join(fakeBin, "go")
	script := `#!/bin/sh
set -eu
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
  fi
  shift || true
done
if [ -z "$out" ]; then
  echo "missing -o" >&2
  exit 1
fi
printf 'runtime-binary' > "$out"
`
	if err := os.WriteFile(fakeGo, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake go: %v", err)
	}
	t.Setenv("PATH", fakeBin)
	t.Setenv("OVPN_AGENT_GOARCH", "riscv64")

	got, err := (&App{repoRoot: root}).ensureAgentBinary()
	if err != nil {
		t.Fatalf("ensure agent binary: %v", err)
	}
	raw, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read built binary: %v", err)
	}
	if string(raw) != "runtime-binary" {
		t.Fatalf("unexpected built binary content: %q", string(raw))
	}
}
