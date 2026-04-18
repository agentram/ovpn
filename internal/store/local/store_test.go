package local

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestAddServerAndUserValidation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	badServer := &model.Server{Name: "bad"}
	if err := store.AddServer(ctx, badServer); err == nil {
		t.Fatalf("expected server validation error")
	}

	server := &model.Server{
		Name:              "main",
		Host:              "1.2.3.4",
		Domain:            "example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
	if err := store.AddServer(ctx, server); err != nil {
		t.Fatalf("add server: %v", err)
	}

	user := &model.User{
		ServerID: server.ID,
		Username: "alice",
		UUID:     "11111111-1111-1111-1111-111111111111",
		Email:    "alice@example.com",
		Enabled:  true,
	}
	if err := store.AddUser(ctx, user); err != nil {
		t.Fatalf("add user: %v", err)
	}
	users, err := store.ListUsers(ctx, server.ID)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected one user, got %d", len(users))
	}
	if !users[0].QuotaEnabled {
		t.Fatalf("expected quota enabled by default")
	}
}

func TestUpsertStatsCache(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := &model.Server{
		Name:              "main",
		Host:              "1.2.3.4",
		Domain:            "example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
	if err := store.AddServer(ctx, server); err != nil {
		t.Fatalf("add server: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Hour)
	row := model.UserTraffic{
		ServerID:      server.ID,
		Email:         "alice@example.com",
		WindowType:    "total",
		WindowStart:   now,
		UplinkBytes:   100,
		DownlinkBytes: 200,
	}
	if err := store.UpsertStatsCache(ctx, row); err != nil {
		t.Fatalf("upsert stats cache: %v", err)
	}
	row.UplinkBytes = 300
	row.DownlinkBytes = 400
	if err := store.UpsertStatsCache(ctx, row); err != nil {
		t.Fatalf("update stats cache: %v", err)
	}
	out, err := store.ListStatsCache(ctx, server.ID, "total")
	if err != nil {
		t.Fatalf("list stats cache: %v", err)
	}
	if len(out) != 1 || out[0].UplinkBytes != 300 || out[0].DownlinkBytes != 400 {
		t.Fatalf("unexpected cache rows: %+v", out)
	}
}

func TestDeleteServerByNameRemovesDependentState(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := &model.Server{
		Name:              "main",
		Host:              "1.2.3.4",
		Domain:            "example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
	if err := store.AddServer(ctx, server); err != nil {
		t.Fatalf("add server: %v", err)
	}

	user := &model.User{
		ServerID: server.ID,
		Username: "alice",
		UUID:     "11111111-1111-1111-1111-111111111111",
		Email:    "alice@example.com",
		Enabled:  true,
	}
	if err := store.AddUser(ctx, user); err != nil {
		t.Fatalf("add user: %v", err)
	}

	if err := store.AddBackupRecord(ctx, &model.BackupRecord{
		ServerID:   server.ID,
		Type:       "server",
		Path:       "/tmp/backup.tgz",
		SHA256:     "abc",
		CreatedBy:  "tester",
		RemotePath: "/opt/ovpn-backups/backup.tgz",
	}); err != nil {
		t.Fatalf("add backup record: %v", err)
	}

	if err := store.DeleteServerByName(ctx, server.Name); err != nil {
		t.Fatalf("delete server: %v", err)
	}

	if _, err := store.GetServerByName(ctx, server.Name); err == nil {
		t.Fatalf("expected server to be deleted")
	}
	users, err := store.ListUsers(ctx, server.ID)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected dependent users to be deleted, got %d", len(users))
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_records WHERE server_id=?`, server.ID).Scan(&count); err != nil {
		t.Fatalf("count backup records: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected backup records for deleted server to be removed, got %d", count)
	}
}

func TestRealityPrivateKeyStoredEncryptedWhenSecretKeyConfigured(t *testing.T) {
	resetSecretKeyCacheForTests()
	t.Setenv("HOME", t.TempDir())

	key := strings.Repeat("k", 32)
	t.Setenv("OVPN_SECRET_KEY", base64.RawStdEncoding.EncodeToString([]byte(key)))

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := &model.Server{
		Name:              "main",
		Host:              "1.2.3.4",
		Domain:            "example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "secret-private-key",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
	if err := store.AddServer(ctx, server); err != nil {
		t.Fatalf("add server: %v", err)
	}

	var stored string
	if err := store.db.QueryRowContext(ctx, `SELECT reality_private_key FROM servers WHERE id=?`, server.ID).Scan(&stored); err != nil {
		t.Fatalf("query raw private key: %v", err)
	}
	if stored == server.RealityPrivateKey {
		t.Fatalf("expected encrypted private key at rest, got plaintext")
	}
	if !strings.HasPrefix(stored, encryptedFieldPrefix) {
		t.Fatalf("expected encrypted prefix, got %q", stored)
	}

	got, err := store.GetServerByName(ctx, server.Name)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if got.RealityPrivateKey != "secret-private-key" {
		t.Fatalf("unexpected decrypted private key: %q", got.RealityPrivateKey)
	}
}

func TestEncryptedPrivateKeyRequiresKeyOnRead(t *testing.T) {
	resetSecretKeyCacheForTests()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// First write with key configured.
	t.Setenv("OVPN_SECRET_KEY", strings.Repeat("k", 32))
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "data")
	store, err := Open(ctx, dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	server := &model.Server{
		Name:              "main",
		Host:              "1.2.3.4",
		Domain:            "example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "secret-private-key",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
	if err := store.AddServer(ctx, server); err != nil {
		t.Fatalf("add server: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Then clear key and ensure read fails with explicit error.
	t.Setenv("OVPN_SECRET_KEY", "")
	resetSecretKeyCacheForTests()
	storeNoKey, err := Open(ctx, dataDir)
	if err != nil {
		t.Fatalf("re-open store: %v", err)
	}
	defer storeNoKey.Close()
	_, err = storeNoKey.GetServerByName(ctx, "main")
	if err == nil {
		t.Fatalf("expected decryption error without secret key")
	}
	if !strings.Contains(err.Error(), "no key is configured") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ensure no fallback key file exists in HOME that would hide failure.
	if _, statErr := os.Stat(filepath.Join(homeDir, ".ovpn", "secret.key")); statErr == nil {
		t.Fatalf("unexpected secret key file in test home")
	}
}
