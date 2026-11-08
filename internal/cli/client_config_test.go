package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/clientcfg"
	"ovpn/internal/model"
)

func TestCheckerExportUsesPrivateFilesAndClientCredentialsOnly(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	srv.EnabledProfiles = model.EnabledProfilesCSV(model.TransportProfileRealityTCPVision, model.TransportProfilePlainXHTTP)
	srv.RealityPrivateKey = "server-private-key-must-not-be-exported"
	srv.VLESSClientEncryption = model.VLESSEncryptionClientPrefix + "client-only-key"
	srv.VLESSServerDecryption = model.VLESSEncryptionServerPrefix + "server-decryption-must-not-be-exported"
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	cmd := app.newUserExportCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--format", "xray-checker", "--all-profiles", "--out", out})
	stdout, _, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout, "vless://") || strings.Contains(stdout, "11111111-1111") {
		t.Fatal("export echoed client credentials")
	}
	files, err := os.ReadDir(out)
	if err != nil || len(files) != 2 {
		t.Fatalf("expected two profile configs: %v, %v", files, err)
	}
	for _, file := range files {
		path := filepath.Join(out, file.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var config clientcfg.Config
		if err := json.Unmarshal(raw, &config); err != nil || len(config.Outbounds) != 1 || filepath.Ext(path) != ".json" {
			t.Fatal("invalid checker configuration")
		}
		if strings.Contains(string(raw), "must-not-be-exported") || strings.Contains(string(raw), "inbounds") {
			t.Fatal("server material or listeners in client export")
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("credential file mode %o", info.Mode().Perm())
		}
	}
}

func TestPrivateClientConfigReplacesSymlinkAndTightensPermissions(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client.json")
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateClientConfig(path, []byte(`{"outbounds":[]}`)); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(other)
	info, _ := os.Lstat(path)
	if string(raw) != "unchanged" || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("symlink target changed or export permissions incorrect")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateClientConfig(path, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("existing file remained public")
	}
}

func TestClientChecksRejectInactiveUsersAndInvalidProfiles(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, _ := app.store.GetServerByName(app.ctx, "main")
	user, _ := app.store.GetUser(app.ctx, srv.ID, "alice")
	for _, mutate := range []func(*model.User){
		func(u *model.User) { u.Enabled = false },
		func(u *model.User) { expiry := time.Now().Add(-time.Hour); u.ExpiryDate = &expiry },
		func(u *model.User) { u.QuotaBlocked = true },
	} {
		copy := *user
		mutate(&copy)
		if _, err := clientConfigForUser(*srv, copy, "", userLinkOptions{}); err == nil {
			t.Fatal("inactive user accepted")
		}
	}
	if _, err := clientConfigForUser(*srv, *user, model.TransportProfilePlainXHTTP, userLinkOptions{}); err == nil {
		t.Fatal("disabled profile accepted")
	}

	srv.Enabled = false
	if _, err := clientConfigForUser(*srv, *user, "", userLinkOptions{}); err == nil {
		t.Fatal("disabled server accepted")
	}
}

func TestUserProbeRejectsDryRunAndUnprotectedURL(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	app.dryRun = true
	cmd := app.newUserProbeCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "dry-run") {
		t.Fatalf("expected dry-run rejection: %v", err)
	}
	app.dryRun = false
	cmd = app.newUserProbeCmd()
	cmd.SetArgs([]string{"--server", "main", "--username", "alice", "--url", "http://example.net/"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected URL rejection: %v", err)
	}
}
