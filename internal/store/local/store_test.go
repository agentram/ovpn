package local

import (
	"context"
	"database/sql"
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
	if server.ProxyServiceUUID != "" {
		t.Fatalf("expected plain vpn server to keep empty proxy service uuid, got %q", server.ProxyServiceUUID)
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

func TestAddProxyServerDefaultsProxyPresetRU(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := &model.Server{
		Name:              "proxy-ru",
		Role:              model.ServerRoleProxy,
		Host:              "10.0.0.10",
		Domain:            "proxy.example.com",
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
		t.Fatalf("add proxy server: %v", err)
	}
	if server.NormalizedProxyPreset() != model.ProxyPresetRU {
		t.Fatalf("expected proxy preset %q, got %q", model.ProxyPresetRU, server.NormalizedProxyPreset())
	}
}

func TestAddVPNServerRejectsProxyPreset(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := &model.Server{
		Name:              "main",
		Role:              model.ServerRoleVPN,
		ProxyPreset:       model.ProxyPresetRU,
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
		if !strings.Contains(err.Error(), "proxy_preset is only supported for proxy role") {
			t.Fatalf("unexpected add vpn server error: %v", err)
		}
		return
	}
	t.Fatalf("expected vpn server proxy_preset validation error")
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

func TestListAttachedBackendServers(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	proxy := mustAddStoreTestServer(t, ctx, store, &model.Server{
		Name:              "proxy-ru",
		Role:              model.ServerRoleProxy,
		Host:              "10.0.0.10",
		Domain:            "proxy-ru.example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "proxy-pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	})
	backendA := mustAddStoreTestServer(t, ctx, store, &model.Server{
		Name:              "backend-a",
		Role:              model.ServerRoleVPN,
		Host:              "198.51.100.10",
		Domain:            "backend-a.example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "vpn-pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ProxyServiceUUID:  "svc-shared",
		Enabled:           true,
	})
	backendB := mustAddStoreTestServer(t, ctx, store, &model.Server{
		Name:              "backend-b",
		Role:              model.ServerRoleVPN,
		Host:              "198.51.100.11",
		Domain:            "backend-b.example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "vpn-pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ProxyServiceUUID:  "svc-shared",
		Enabled:           true,
	})

	if err := store.UpsertProxyBackend(ctx, &model.ProxyBackend{
		ProxyServerID:   proxy.ID,
		BackendServerID: backendB.ID,
		Enabled:         false,
		Priority:        20,
	}); err != nil {
		t.Fatalf("attach backend-b: %v", err)
	}
	if err := store.UpsertProxyBackend(ctx, &model.ProxyBackend{
		ProxyServerID:   proxy.ID,
		BackendServerID: backendA.ID,
		Enabled:         true,
		Priority:        10,
	}); err != nil {
		t.Fatalf("attach backend-a: %v", err)
	}

	mappings, err := store.ListProxyBackends(ctx, proxy.ID)
	if err != nil {
		t.Fatalf("list proxy backends: %v", err)
	}
	if len(mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(mappings))
	}
	if mappings[0].BackendServer == nil || mappings[0].BackendServer.Name != backendA.Name {
		t.Fatalf("expected backend-a first by priority, got %+v", mappings[0])
	}

	attached, err := store.ListAttachedBackendServers(ctx, proxy.ID)
	if err != nil {
		t.Fatalf("list attached backend servers: %v", err)
	}
	if len(attached) != 1 || attached[0].Name != backendA.Name {
		t.Fatalf("expected only enabled backend-a to be returned, got %+v", attached)
	}
	if proxy.NormalizedProxyPreset() != model.ProxyPresetRU {
		t.Fatalf("expected proxy preset %q, got %q", model.ProxyPresetRU, proxy.NormalizedProxyPreset())
	}
	hasProxy, err := store.BackendHasAttachedProxy(ctx, backendA.ID)
	if err != nil {
		t.Fatalf("backend has attached proxy for backend-a: %v", err)
	}
	if !hasProxy {
		t.Fatalf("expected backend-a to report attached proxy")
	}
	hasProxy, err = store.BackendHasAttachedProxy(ctx, backendB.ID)
	if err != nil {
		t.Fatalf("backend has attached proxy for backend-b: %v", err)
	}
	if hasProxy {
		t.Fatalf("expected disabled backend-b mapping to be ignored")
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

func mustAddStoreTestServer(t *testing.T, ctx context.Context, store *Store, srv *model.Server) *model.Server {
	t.Helper()
	if err := store.AddServer(ctx, srv); err != nil {
		t.Fatalf("add server %s: %v", srv.Name, err)
	}
	return srv
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

func TestMigrateBackfillsProxyPresetForLegacyProxyRows(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "ovpn.db")

	db, err := sql.Open(sqliteDriver, dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	stmts := []string{
		`CREATE TABLE servers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			role TEXT NOT NULL DEFAULT 'vpn',
			host TEXT NOT NULL,
			domain TEXT NOT NULL,
			ssh_user TEXT NOT NULL,
			ssh_port INTEGER NOT NULL DEFAULT 22,
			ssh_identity_file TEXT NOT NULL DEFAULT '',
			ssh_known_hosts_file TEXT NOT NULL DEFAULT '',
			ssh_strict_host_key INTEGER NOT NULL DEFAULT 1,
			xray_version TEXT NOT NULL,
			reality_private_key TEXT NOT NULL,
			reality_public_key TEXT NOT NULL,
			reality_short_ids TEXT NOT NULL,
			reality_server_name TEXT NOT NULL,
			reality_target TEXT NOT NULL,
			proxy_service_uuid TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_deploy_at TEXT
		);`,
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			uuid TEXT NOT NULL,
			email TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			expiry_date TEXT,
			traffic_limit_byte INTEGER,
			quota_enabled INTEGER NOT NULL DEFAULT 1,
			quota_blocked INTEGER NOT NULL DEFAULT 0,
			quota_blocked_at TEXT,
			notes TEXT NOT NULL DEFAULT '',
			tags_csv TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE user_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			tag TEXT NOT NULL
		);`,
		`CREATE TABLE deploy_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			revision TEXT NOT NULL,
			config_hash TEXT NOT NULL,
			applied_by TEXT NOT NULL,
			applied_at TEXT NOT NULL,
			status TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE backup_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER,
			type TEXT NOT NULL,
			path TEXT NOT NULL,
			sha256 TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL,
			remote_path TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE stats_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			email TEXT NOT NULL,
			window_type TEXT NOT NULL,
			window_start TEXT NOT NULL,
			uplink_bytes INTEGER NOT NULL,
			downlink_bytes INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("exec schema stmt: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO servers (
			name, role, host, domain, ssh_user, ssh_port, ssh_identity_file, ssh_known_hosts_file,
			ssh_strict_host_key, xray_version, reality_private_key, reality_public_key,
			reality_short_ids, reality_server_name, reality_target, proxy_service_uuid, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "legacy-proxy", model.ServerRoleProxy, "10.0.0.10", "proxy.example.com", "root", 22, "", "", 1, "26.3.27", "priv", "pub", "abcd", "www.microsoft.com", "www.microsoft.com:443", "", 1, now, now); err != nil {
		t.Fatalf("insert legacy proxy row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	store, err := Open(ctx, dataDir)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()

	srv, err := store.GetServerByName(ctx, "legacy-proxy")
	if err != nil {
		t.Fatalf("get migrated proxy: %v", err)
	}
	if srv.NormalizedProxyPreset() != model.ProxyPresetRU {
		t.Fatalf("expected migrated proxy preset %q, got %q", model.ProxyPresetRU, srv.NormalizedProxyPreset())
	}

	var preset string
	if err := store.db.QueryRowContext(ctx, `SELECT proxy_preset FROM servers WHERE name=?`, "legacy-proxy").Scan(&preset); err != nil {
		t.Fatalf("query migrated proxy preset: %v", err)
	}
	if preset != model.ProxyPresetRU {
		t.Fatalf("expected persisted proxy_preset %q, got %q", model.ProxyPresetRU, preset)
	}
}
