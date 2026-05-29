package ssh

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRunnerDryRunExecCopyAndStream(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		DryRun: true,
		Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	}
	cfg := Config{
		User:            "root",
		Host:            "example.com",
		Port:            2222,
		IdentityFile:    "/tmp/id_ed25519",
		KnownHostsFile:  "/tmp/known_hosts",
		StrictHostKey:   true,
		ConnectTimeoutS: 3,
	}

	res, err := runner.Exec(context.Background(), cfg, "echo vless://secret@example")
	if err != nil {
		t.Fatalf("dry-run exec: %v", err)
	}
	for _, want := range []string{"DRY-RUN ssh", "-p 2222", "-i /tmp/id_ed25519", "root@example.com"} {
		if !strings.Contains(res.Stdout, want) {
			t.Fatalf("dry-run exec output missing %q: %s", want, res.Stdout)
		}
	}
	if err := runner.CopyFile(context.Background(), cfg, "/tmp/local", "/tmp/remote"); err != nil {
		t.Fatalf("dry-run copy: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := runner.ExecStream(context.Background(), cfg, "uptime", &stdout, &stderr); err != nil {
		t.Fatalf("dry-run stream: %v", err)
	}
	if !strings.Contains(stdout.String(), "DRY-RUN ssh") || stderr.Len() != 0 {
		t.Fatalf("unexpected dry-run stream stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestHelpersBuildDefaultsAndSanitizeErrors(t *testing.T) {
	t.Parallel()

	cfg := Config{Host: "host"}
	if got := strings.Join(sshArgs(cfg), " "); !strings.Contains(got, "-p 22") || !strings.Contains(got, "ConnectTimeout=10") || !strings.Contains(got, "StrictHostKeyChecking=no") {
		t.Fatalf("unexpected default ssh args: %s", got)
	}
	if got := strings.Join(scpArgs(cfg), " "); !strings.Contains(got, "-P 22") {
		t.Fatalf("unexpected default scp args: %s", got)
	}
	if got := target(Config{Host: "host"}); got != "host" {
		t.Fatalf("target without user = %q", got)
	}

	ctx, cancel := TimeoutCtx(context.Background(), 0)
	defer cancel()
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 {
		t.Fatalf("expected default timeout deadline")
	}

	long := strings.Repeat("a", 500)
	trimmed := trimForError(long)
	if len(trimmed) >= len(long) || !strings.Contains(trimmed, "truncated") {
		t.Fatalf("expected trimmed long error, got len=%d", len(trimmed))
	}
	if got := sanitizeRemoteCmd("curl vless://secret@example.com/path"); strings.Contains(got, "secret@example") || !strings.Contains(got, "vless://[REDACTED]") {
		t.Fatalf("unexpected redacted command: %q", got)
	}
	if got := sanitizeRemoteCmd("cat <<'JSON'\n{\"secret\":true}\nJSON"); got != "JSON payload command (redacted)" {
		t.Fatalf("unexpected heredoc redaction: %q", got)
	}
	if got := sanitizeRemoteCmd(strings.Repeat("x", 600)); len(got) > 515 || !strings.Contains(got, "truncated") {
		t.Fatalf("expected long command truncation, got len=%d", len(got))
	}
}
