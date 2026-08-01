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

func TestUserQRAcceptsQRFileAlias(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	qrPath := filepath.Join(t.TempDir(), "alice.png")
	cmd := app.newUserQRCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--qr-file", qrPath})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user qr --qr-file: %v", err)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "QR saved: "+qrPath) {
		t.Fatalf("expected saved status on stderr, got %q", stderr)
	}
	assertPNGQRCodeFile(t, qrPath)
}

func TestUserLinkCanSelectTransportProfile(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), model.TransportProfilePlainXHTTP)
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}

	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", "plain-xhttp", "--qr=false"})
	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user link --profile plain-xhttp: %v", err)
	}
	link := strings.TrimSpace(stdout)
	for _, want := range []string{":13179?", "type=xhttp", "path=%2F", "#ovpn-alice-vless-xhttp-plain"} {
		if !strings.Contains(link, want) {
			t.Fatalf("profile link missing %q: %s", want, link)
		}
	}
	for _, notWant := range []string{"fp=", "spx="} {
		if strings.Contains(link, notWant) {
			t.Fatalf("plain XHTTP link should not contain %q: %s", notWant, link)
		}
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestUserVLESSEncryptionXHTTPLinkAndQRHandleLongEncryptionValue(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	clientEncryption := "mlkem768x25519plus.native.0rtt." + strings.Repeat("A", 512)
	srv.VLESSClientEncryption = clientEncryption
	srv.VLESSServerDecryption = "mlkem768x25519plus.native.600s." + strings.Repeat("B", 512)
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), model.TransportProfileVLESSEncXHTTP)
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}

	linkCmd := app.newUserLinkCmd()
	linkCmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", model.TransportProfileVLESSEncXHTTP, "--qr=false"})
	stdout, stderr, err := captureStdoutStderr(t, linkCmd.Execute)
	if err != nil {
		t.Fatalf("user link vless encryption xhttp: %v", err)
	}
	link := strings.TrimSpace(stdout)
	for _, want := range []string{":13180?", "type=xhttp", "encryption=" + clientEncryption} {
		if !strings.Contains(link, want) {
			t.Fatalf("VLESS Encryption link missing %q: %s", want, link)
		}
	}
	if strings.Contains(link, srv.VLESSServerDecryption) {
		t.Fatalf("VLESS Encryption link leaked server decryption: %s", link)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}

	qrPath := filepath.Join(t.TempDir(), "alice-vlessenc.png")
	qrCmd := app.newUserQRCmd()
	qrCmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", model.TransportProfileVLESSEncXHTTP, "--out", qrPath})
	stdout, stderr, err = captureStdoutStderr(t, qrCmd.Execute)
	if err != nil {
		t.Fatalf("user qr vless encryption xhttp: %v", err)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "QR saved: "+qrPath) {
		t.Fatalf("expected saved status on stderr, got %q", stderr)
	}
	assertPNGQRCodeFile(t, qrPath)
}

func TestUserVLESSEncryptionXHTTPLinkRejectsInvalidClientEncryption(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.VLESSClientEncryption = "invalid-client-value"
	srv.VLESSServerDecryption = "mlkem768x25519plus.native.600s.server-value"
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), model.TransportProfileVLESSEncXHTTP)
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}

	_, err = buildUserProfileLink(*srv, model.User{Username: "alice", UUID: "11111111-1111-1111-1111-111111111111"}, model.TransportProfileVLESSEncXHTTP)
	if err == nil || !strings.Contains(err.Error(), "no valid client encryption value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserLinkRejectsRemovedTransportProfile(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), "vless-reality-xhttp")
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}

	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", "vless-reality-xhttp", "--qr=false"})
	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil {
		t.Fatalf("expected removed profile error")
	}
	for _, want := range []string{"vless-reality-xhttp", "unsupported transport profile"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %v", want, err)
		}
	}
	if stdout != "" {
		t.Fatalf("expected no link output for removed profile, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestPlainXHTTPProfileLinkDoesNotRequireRealityShortID(t *testing.T) {
	link, err := buildUserProfileLink(model.Server{
		Name:            "main",
		Host:            "203.0.113.10",
		Domain:          "example.org",
		PrimaryProfile:  model.TransportProfilePlainXHTTP,
		EnabledProfiles: model.TransportProfilePlainXHTTP,
	}, model.User{
		Username: "alice",
		UUID:     "11111111-1111-1111-1111-111111111111",
	}, model.TransportProfilePlainXHTTP)
	if err != nil {
		t.Fatalf("plain XHTTP link should not need REALITY short-id: %v", err)
	}
	for _, want := range []string{"vless://11111111-1111-1111-1111-111111111111@example.org:13179", "security=none", "type=xhttp", "path=%2F"} {
		if !strings.Contains(link, want) {
			t.Fatalf("plain XHTTP link missing %q: %s", want, link)
		}
	}
	for _, forbidden := range []string{"pbk=", "sid=", "sni="} {
		if strings.Contains(link, forbidden) {
			t.Fatalf("plain XHTTP link should not contain %s: %s", forbidden, link)
		}
	}
}

func TestTLSSelfSNIProfileLinkUsesServerDomain(t *testing.T) {
	link, err := buildUserProfileLink(model.Server{
		Name:            "main",
		Host:            "203.0.113.10",
		Domain:          "example.org",
		PrimaryProfile:  model.TransportProfileTLSSelfSNIWeb,
		EnabledProfiles: model.TransportProfileTLSSelfSNIWeb,
	}, model.User{
		Username: "alice",
		UUID:     "11111111-1111-1111-1111-111111111111",
	}, model.TransportProfileTLSSelfSNIWeb)
	if err != nil {
		t.Fatalf("TLS self-SNI link should not need REALITY material: %v", err)
	}
	for _, want := range []string{
		"vless://11111111-1111-1111-1111-111111111111@example.org:443",
		"security=tls",
		"type=tcp",
		"flow=xtls-rprx-vision",
		"sni=example.org",
		"alpn=http%2F1.1",
		"fp=firefox",
		"headerType=none",
	} {
		if !strings.Contains(link, want) {
			t.Fatalf("TLS self-SNI link missing %q: %s", want, link)
		}
	}
	for _, forbidden := range []string{"security=reality", "pbk=", "sid="} {
		if strings.Contains(link, forbidden) {
			t.Fatalf("TLS self-SNI link should not contain %s: %s", forbidden, link)
		}
	}
}

func TestUserLinkCanOverrideFingerprintAndSpiderX(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{
		"--server", "main",
		"--username", "alice",
		"--fingerprint", "qq",
		"--spider-x", "/news/app.js",
		"--qr=false",
	})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user link with fingerprint/spiderX overrides: %v", err)
	}
	link := strings.TrimSpace(stdout)
	for _, want := range []string{"fp=qq", "spx=%2Fnews%2Fapp.js"} {
		if !strings.Contains(link, want) {
			t.Fatalf("override link missing %q: %s", want, link)
		}
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestUserLinkCanGenerateLegacyRealityParams(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{
		"--server", "main",
		"--username", "alice",
		"--profile", model.TransportProfileRealityTCPVision,
		"--legacy-reality",
		"--qr=false",
	})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user link --legacy-reality: %v", err)
	}
	link := strings.TrimSpace(stdout)
	if !strings.Contains(link, "fp=chrome") {
		t.Fatalf("legacy link should use chrome fingerprint: %s", link)
	}
	if strings.Contains(link, "spx=") {
		t.Fatalf("legacy link should omit spiderX: %s", link)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestUserLinkCanOmitSpiderX(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{
		"--server", "main",
		"--username", "alice",
		"--fingerprint", "chrome",
		"--no-spider-x",
		"--qr=false",
	})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user link --no-spider-x: %v", err)
	}
	link := strings.TrimSpace(stdout)
	if !strings.Contains(link, "fp=chrome") {
		t.Fatalf("link should keep explicit fingerprint: %s", link)
	}
	if strings.Contains(link, "spx=") {
		t.Fatalf("link should omit spiderX: %s", link)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestUserLinkRejectsBadFingerprintAndSpiderX(t *testing.T) {
	app := newTestAppWithLinkedUser(t)

	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--fingerprint", "netscape", "--qr=false"})
	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "unsupported --fingerprint") || !strings.Contains(err.Error(), "firefox") {
		t.Fatalf("expected unsupported fingerprint error, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad fingerprint should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}

	cmd = app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--spider-x", "missing-slash", "--qr=false"})
	stdout, stderr, err = captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "--spider-x must start with /") {
		t.Fatalf("expected bad spider-x error, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad spider-x should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}

	cmd = app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--spider-x", "/ok", "--no-spider-x", "--qr=false"})
	stdout, stderr, err = captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "use only one of --spider-x or --no-spider-x") {
		t.Fatalf("expected mutually exclusive spider-x error, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad spider-x combination should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}

	cmd = app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--legacy-reality", "--fingerprint", "firefox", "--qr=false"})
	stdout, stderr, err = captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "--legacy-reality already sets --fingerprint chrome") {
		t.Fatalf("expected legacy-reality combination error, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad legacy combination should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestUserLinkRejectsFingerprintAndSpiderXForPlainXHTTP(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), model.TransportProfilePlainXHTTP)
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}

	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", model.TransportProfilePlainXHTTP, "--fingerprint", "firefox", "--qr=false"})
	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "--fingerprint is only supported") {
		t.Fatalf("expected unsupported fingerprint for plain profile, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad profile option should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}

	cmd = app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", model.TransportProfilePlainXHTTP, "--spider-x", "/ok", "--qr=false"})
	stdout, stderr, err = captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "--spider-x is only supported") {
		t.Fatalf("expected unsupported spider-x for plain profile, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad profile option should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}

	cmd = app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", model.TransportProfilePlainXHTTP, "--no-spider-x", "--qr=false"})
	stdout, stderr, err = captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "--no-spider-x is only supported") {
		t.Fatalf("expected unsupported no-spider-x for plain profile, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad profile option should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}

	cmd = app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", model.TransportProfilePlainXHTTP, "--legacy-reality", "--qr=false"})
	stdout, stderr, err = captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "--legacy-reality is only supported for REALITY profiles") {
		t.Fatalf("expected unsupported legacy-reality for plain profile, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad profile option should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestUserLinkRejectsSpiderXForTLSSelfSNI(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.Domain = "example.org"
	srv.PrimaryProfile = model.TransportProfileTLSSelfSNIWeb
	srv.EnabledProfiles = model.TransportProfileTLSSelfSNIWeb
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}

	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", model.TransportProfileTLSSelfSNIWeb, "--spider-x", "/assets/test.js", "--qr=false"})
	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "--spider-x is only supported for REALITY profiles") || !strings.Contains(err.Error(), model.TransportProfileTLSSelfSNIWeb) {
		t.Fatalf("expected unsupported spider-x for TLS self-SNI profile, err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("bad profile option should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestUserLinkRejectsUnknownExplicitTransportProfile(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", "typo", "--qr=false"})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), `unsupported transport profile "typo"`) {
		t.Fatalf("expected explicit profile validation error, got err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if !strings.Contains(err.Error(), model.TransportProfilePlainXHTTP) {
		t.Fatalf("expected supported-profile hint, got %v", err)
	}
	if stdout != "" {
		t.Fatalf("invalid profile should not print secrets, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestUserLinkRejectsDisabledTransportProfileWithTip(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	cmd := app.newUserLinkCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--profile", model.TransportProfilePlainXHTTP, "--qr=false"})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil {
		t.Fatalf("expected disabled profile error")
	}
	for _, want := range []string{
		"profile " + model.TransportProfilePlainXHTTP + " is not enabled on server main",
		"ovpn server profile enable main " + model.TransportProfilePlainXHTTP,
		"ovpn deploy main",
		"ovpn server profile list main",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %v", want, err)
		}
	}
	if stdout != "" {
		t.Fatalf("disabled profile should not print link or QR output, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestUserExportAllProfilesWritesLinksAndQRs(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), strings.Join([]string{
		model.TransportProfilePlainXHTTP,
	}, ","))
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}
	out := t.TempDir()
	cmd := app.newUserExportCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--all-profiles", "--out", out})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user export: %v", err)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if strings.Count(stdout, "exported ") != 2 {
		t.Fatalf("expected two deployable exported profiles, got:\n%s", stdout)
	}
	for _, profile := range []string{
		model.TransportProfileRealityTCPVision,
		model.TransportProfilePlainXHTTP,
	} {
		base := filepath.Join(out, "main-alice-"+profile)
		raw, err := os.ReadFile(base + ".txt")
		if err != nil {
			t.Fatalf("read exported link for %s: %v", profile, err)
		}
		if !strings.Contains(string(raw), "#ovpn-alice-"+profile) {
			t.Fatalf("exported link for %s missing profile label: %s", profile, string(raw))
		}
		assertPNGQRCodeFile(t, base+".png")
	}
}

func TestUserExportWritesFingerprintVariants(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), model.TransportProfileRealityTCPVision)
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}
	out := t.TempDir()
	cmd := app.newUserExportCmd()
	cmd.SetArgs([]string{
		"--server", "main",
		"--username", "alice",
		"--profile", model.TransportProfileRealityTCPVision,
		"--fingerprints", "firefox,qq,chrome",
		"--out", out,
	})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("user export --fingerprints: %v", err)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if strings.Count(stdout, "exported ") != 3 {
		t.Fatalf("expected three exported variants, got:\n%s", stdout)
	}
	for _, fp := range []string{"firefox", "qq", "chrome"} {
		base := filepath.Join(out, "main-alice-"+model.TransportProfileRealityTCPVision+"-fp-"+fp)
		raw, err := os.ReadFile(base + ".txt")
		if err != nil {
			t.Fatalf("read exported %s link: %v", fp, err)
		}
		link := string(raw)
		if !strings.Contains(link, "fp="+fp) || !strings.Contains(link, "spx=%2Fassets%2F08ac542059c2.js") {
			t.Fatalf("exported %s link missing hardened params: %s", fp, link)
		}
		assertPNGQRCodeFile(t, base+".png")
	}
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

func TestTerminalQRCodeKeepsVerticalQuietZone(t *testing.T) {
	t.Parallel()

	qrText, err := renderTerminalQRCode(testAliceVLESSLink)
	if err != nil {
		t.Fatalf("renderTerminalQRCode: %v", err)
	}
	lines := strings.Split(strings.TrimRight(qrText, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected QR rows, got %d", len(lines))
	}
	if !isUniformTerminalQRLine(lines[0]) {
		t.Fatalf("top QR quiet zone was clipped: %q", lines[0])
	}
	if !isUniformTerminalQRLine(lines[len(lines)-1]) {
		t.Fatalf("bottom QR quiet zone was clipped: %q", lines[len(lines)-1])
	}
}

func isUniformTerminalQRLine(line string) bool {
	runes := []rune(line)
	if len(runes) == 0 {
		return false
	}
	first := runes[0]
	for _, r := range runes[1:] {
		if r != first {
			return false
		}
	}
	return true
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

const testAliceVLESSLink = "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=reality&encryption=none&pbk=pub&fp=firefox&type=tcp&flow=xtls-rprx-vision&sni=www.microsoft.com&sid=abcd1234&spx=%2Fassets%2F08ac542059c2.js#ovpn-alice"
