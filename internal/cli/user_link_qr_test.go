package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/skip2/go-qrcode"

	"ovpn/internal/model"
)

func TestUserLinkDefaultPrintsTerminalQR(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice"})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user link: %v", err)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) < 8 {
		t.Fatalf("expected link plus terminal QR, got %d lines:\n%s", len(lines), stdout)
	}
	if lines[0] != testAliceVLESSLink {
		t.Fatalf("expected link as first line, got %q", lines[0])
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestUserLinkCanDisableTerminalQR(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--qr=false"})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user link --qr=false: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != testAliceVLESSLink {
		t.Fatalf("unexpected link output:\n%s", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestUserLinkWritesQRFile(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	qrPath := filepath.Join(t.TempDir(), "alice.png")
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--qr=false", "--qr-file", qrPath})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user link --qr-file: %v", err)
	}
	if got := strings.TrimSpace(stdout); got != testAliceVLESSLink {
		t.Fatalf("unexpected link output:\n%s", stdout)
	}
	if !strings.Contains(stderr, "QR saved: "+qrPath) {
		t.Fatalf("expected saved status on stderr, got %q", stderr)
	}
	assertPNGQRCodeFile(t, qrPath)
}

func TestUserLinkQRFileRequiresExistingParent(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	qrPath := filepath.Join(t.TempDir(), "missing", "alice.png")
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--qr=false", "--qr-file", qrPath})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "qr file parent directory") {
		t.Fatalf("expected missing parent error, got %v", err)
	}
	if got := strings.TrimSpace(stdout); got != testAliceVLESSLink {
		t.Fatalf("link should still be first output before QR file error, got %q", got)
	}
	if strings.Contains(stderr, "QR saved:") {
		t.Fatalf("failed QR save should not report success, got %q", stderr)
	}
	if _, statErr := os.Stat(qrPath); !os.IsNotExist(statErr) {
		t.Fatalf("QR file should not exist, stat err=%v", statErr)
	}
}

func TestUserLinkPrintsAndWritesQR(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	qrPath := filepath.Join(t.TempDir(), "alice.png")
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--qr-file", qrPath})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user link --qr --qr-file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) < 8 {
		t.Fatalf("expected link plus terminal QR, got %d lines:\n%s", len(lines), stdout)
	}
	if lines[0] != testAliceVLESSLink {
		t.Fatalf("expected link as first line, got %q", lines[0])
	}
	if !strings.Contains(stderr, "QR saved: "+qrPath) {
		t.Fatalf("expected saved status on stderr, got %q", stderr)
	}
	assertPNGQRCodeFile(t, qrPath)
}

func TestTerminalQRCodeTrimsQuietZone(t *testing.T) {
	t.Parallel()

	qr, err := qrcode.New(testAliceVLESSLink, qrcode.Low)
	if err != nil {
		t.Fatalf("build QR: %v", err)
	}
	full := qr.ToSmallString(false)
	trimmed, err := renderTerminalQRCode(testAliceVLESSLink)
	if err != nil {
		t.Fatalf("renderTerminalQRCode: %v", err)
	}
	if len(trimmed) >= len(full) {
		t.Fatalf("expected trimmed terminal QR to be smaller")
	}
	if len(strings.Split(strings.TrimRight(trimmed, "\n"), "\n")) >= len(strings.Split(strings.TrimRight(full, "\n"), "\n")) {
		t.Fatalf("expected trimmed terminal QR to use fewer rows")
	}
	if !utf8.ValidString(trimmed) {
		t.Fatalf("terminal QR must be valid UTF-8")
	}
	if strings.ContainsRune(trimmed, '\uFFFD') {
		t.Fatalf("terminal QR must not contain replacement characters")
	}
}

func TestTrimTerminalQRCodeQuietZoneIsRuneSafe(t *testing.T) {
	t.Parallel()

	got := trimTerminalQRCodeQuietZone("▀▀▀\n▀█▀\n▀▀▀\n", 1)
	if got != "█\n" {
		t.Fatalf("unexpected trimmed QR: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("trimmed QR must be valid UTF-8")
	}
}

func TestTerminalQRCodeStaysCompactForVLESSLinks(t *testing.T) {
	t.Parallel()

	qrText, err := renderTerminalQRCode(testAliceVLESSLink)
	if err != nil {
		t.Fatalf("renderTerminalQRCode: %v", err)
	}
	lines := strings.Split(strings.TrimRight(qrText, "\n"), "\n")
	if len(lines) > 34 {
		t.Fatalf("expected compact terminal QR, got %d rows", len(lines))
	}
}

func assertPNGQRCodeFile(t *testing.T, path string) {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read QR file: %v", err)
	}
	if len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("expected PNG header, got %q", raw[:8])
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat QR file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected QR file mode 0600, got %o", got)
	}
}

func newTestAppWithLinkedUser(t *testing.T) *App {
	t.Helper()

	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	user := &model.User{
		ServerID:     srv.ID,
		Username:     "alice",
		UUID:         "11111111-1111-1111-1111-111111111111",
		Email:        "alice@example.com",
		Enabled:      true,
		QuotaEnabled: true,
	}
	if err := app.store.AddUser(app.ctx, user); err != nil {
		t.Fatalf("add user: %v", err)
	}
	return app
}

func captureStdoutStderr(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(outR)
		outCh <- string(raw)
	}()
	go func() {
		raw, _ := io.ReadAll(errR)
		errCh <- string(raw)
	}()

	os.Stdout = outW
	os.Stderr = errW
	runErr := fn()
	_ = outW.Close()
	_ = errW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	stdout := <-outCh
	stderr := <-errCh
	_ = outR.Close()
	_ = errR.Close()
	return stdout, stderr, runErr
}

const testAliceVLESSLink = "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=reality&encryption=none&pbk=pub&fp=chrome&type=tcp&flow=xtls-rprx-vision&sni=www.microsoft.com&sid=abcd1234#ovpn-alice"
