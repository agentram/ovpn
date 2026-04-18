package ssh

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBaseArgs(t *testing.T) {
	t.Parallel()

	args := sshArgs(Config{
		Port:            2222,
		IdentityFile:    "/tmp/id",
		KnownHostsFile:  "/tmp/known_hosts",
		StrictHostKey:   true,
		ConnectTimeoutS: 15,
	})
	joined := strings.Join(args, " ")
	for _, part := range []string{"-p 2222", "ConnectTimeout=15", "-i /tmp/id", "UserKnownHostsFile=/tmp/known_hosts", "StrictHostKeyChecking=yes", "BatchMode=yes"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("expected %q in args: %q", part, joined)
		}
	}
}

func TestScpArgsUsesUppercasePortFlag(t *testing.T) {
	t.Parallel()

	args := scpArgs(Config{Port: 22, ConnectTimeoutS: 15})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-P 22") {
		t.Fatalf("expected scp args to contain '-P 22', got %q", joined)
	}
	if strings.Contains(joined, "-p 22") {
		t.Fatalf("did not expect ssh-style '-p 22' in scp args: %q", joined)
	}
}

func TestTarget(t *testing.T) {
	t.Parallel()

	if got := target(Config{User: "debian", Host: "1.2.3.4"}); got != "debian@1.2.3.4" {
		t.Fatalf("unexpected target: %q", got)
	}
	if got := target(Config{Host: "1.2.3.4"}); got != "1.2.3.4" {
		t.Fatalf("unexpected target without user: %q", got)
	}
}

func TestExecDryRun(t *testing.T) {
	t.Parallel()

	r := &Runner{DryRun: true}
	res, err := r.Exec(context.Background(), Config{User: "debian", Host: "127.0.0.1"}, "echo ok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Stdout, "DRY-RUN ssh") || !strings.Contains(res.Stdout, "echo ok") {
		t.Fatalf("unexpected dry-run output: %q", res.Stdout)
	}
}

func TestSanitizeRemoteCmdRedactsJSONHeredoc(t *testing.T) {
	t.Parallel()

	raw := "cat <<'JSON' | curl -fsS -d @- http://127.0.0.1:9090/runtime/user/add\n{\"email\":\"alice@example.com\"}\nJSON"
	if got := sanitizeRemoteCmd(raw); got != "JSON payload command (redacted)" {
		t.Fatalf("expected redacted marker, got %q", got)
	}
}

func TestSanitizeRemoteCmdTruncatesLongCommand(t *testing.T) {
	t.Parallel()

	raw := strings.Repeat("x", 800)
	got := sanitizeRemoteCmd(raw)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Fatalf("expected truncated suffix, got %q", got)
	}
	if len(got) <= 500 {
		t.Fatalf("expected truncated command to keep prefix + suffix, got len=%d", len(got))
	}
}

func TestSanitizeRemoteCmdRedactsVLESSURL(t *testing.T) {
	t.Parallel()

	raw := "echo vless://uuid@example.com:443?security=reality#alice"
	got := sanitizeRemoteCmd(raw)
	if strings.Contains(got, "uuid@example.com") {
		t.Fatalf("expected VLESS URL redacted, got %q", got)
	}
	if !strings.Contains(got, "vless://[REDACTED]") {
		t.Fatalf("expected redacted marker, got %q", got)
	}
}

func TestTrimForErrorKeepsHeadAndTail(t *testing.T) {
	t.Parallel()

	input := "HEAD-MARKER-" + strings.Repeat("a", 260) + strings.Repeat("b", 260) + "-TAIL-MARKER"
	got := trimForError(input)
	if !strings.Contains(got, "...(truncated)...") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if !strings.Contains(got, "HEAD-MARKER-") {
		t.Fatalf("expected head details to be preserved, got %q", got)
	}
	if !strings.Contains(got, "-TAIL-MARKER") {
		t.Fatalf("expected tail details to be preserved, got %q", got)
	}
	if len(got) >= len(strings.TrimSpace(input)) {
		t.Fatalf("expected output to be shorter than input")
	}
}

func TestTimeoutCtxUsesDefaultWhenDurationNonPositive(t *testing.T) {
	t.Parallel()

	ctx, cancel := TimeoutCtx(context.Background(), 0)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("expected deadline to be set")
	}
	remaining := time.Until(deadline)
	if remaining < 25*time.Second || remaining > 31*time.Second {
		t.Fatalf("expected default timeout around 30s, got %s", remaining)
	}
}

func TestTimeoutCtxUsesProvidedDuration(t *testing.T) {
	t.Parallel()

	ctx, cancel := TimeoutCtx(context.Background(), 5*time.Second)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("expected deadline to be set")
	}
	remaining := time.Until(deadline)
	if remaining < 4*time.Second || remaining > 6*time.Second {
		t.Fatalf("expected timeout around 5s, got %s", remaining)
	}
}
