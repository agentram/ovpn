package local

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestServerUserBackupAndProxyCRUDCoverage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := mustAddStoreTestServer(t, ctx, store, storeCoverageServer("main", model.ServerRoleVPN))
	server.Host = "203.0.113.10"
	server.Enabled = false
	if err := store.UpdateServer(ctx, server); err != nil {
		t.Fatalf("update server: %v", err)
	}
	if err := store.SetServerLastDeploy(ctx, server.ID); err != nil {
		t.Fatalf("set last deploy: %v", err)
	}
	byID, err := store.GetServerByID(ctx, server.ID)
	if err != nil {
		t.Fatalf("get server by id: %v", err)
	}
	if byID.Host != "203.0.113.10" || byID.LastDeployAt == nil || byID.Enabled {
		t.Fatalf("unexpected updated server: %+v", byID)
	}
	servers, err := store.ListServers(ctx)
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(servers) != 1 || servers[0].Name != "main" {
		t.Fatalf("unexpected servers: %+v", servers)
	}

	quota := int64(100)
	expiry := time.Date(2099, 1, 2, 0, 0, 0, 0, time.UTC)
	user := &model.User{
		ServerID:         server.ID,
		Username:         "alice",
		UUID:             "11111111-1111-1111-1111-111111111111",
		Email:            "alice@example.com",
		Enabled:          true,
		ExpiryDate:       &expiry,
		TrafficLimitByte: &quota,
		QuotaEnabled:     true,
		QuotaBlocked:     true,
		QuotaBlockedAt:   &expiry,
		Notes:            "note",
		TagsCSV:          "alpha,beta",
	}
	if err := store.AddUser(ctx, user); err != nil {
		t.Fatalf("add user: %v", err)
	}
	got, err := store.GetUser(ctx, server.ID, "alice")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	got.Enabled = false
	got.TagsCSV = "beta,gamma"
	if err := store.UpdateUser(ctx, got); err != nil {
		t.Fatalf("update user: %v", err)
	}
	enabled, err := store.ListEnabledUsers(ctx, server.ID)
	if err != nil {
		t.Fatalf("list enabled users: %v", err)
	}
	if len(enabled) != 0 {
		t.Fatalf("expected no enabled users, got %+v", enabled)
	}
	if err := store.DeleteUser(ctx, server.ID, "alice"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := store.GetUser(ctx, server.ID, "alice"); err == nil {
		t.Fatalf("expected user not found after delete")
	}

	if err := store.AddDeployRevision(ctx, &model.DeployRevision{ServerID: server.ID, Revision: "r1", ConfigHash: "abc", AppliedBy: "tester", Status: "ok"}); err != nil {
		t.Fatalf("add deploy revision: %v", err)
	}
	globalBackup := &model.BackupRecord{Type: "global", Path: "/tmp/global.tgz", SHA256: "sha", CreatedBy: "tester"}
	serverBackup := &model.BackupRecord{ServerID: server.ID, Type: "server", Path: "/tmp/server.tgz", SHA256: "sha2", CreatedBy: "tester", RemotePath: "/remote"}
	if err := store.AddBackupRecord(ctx, globalBackup); err != nil {
		t.Fatalf("add global backup: %v", err)
	}
	if err := store.AddBackupRecord(ctx, serverBackup); err != nil {
		t.Fatalf("add server backup: %v", err)
	}
	backups, err := store.ListBackupRecords(ctx, server.ID)
	if err != nil {
		t.Fatalf("list backup records: %v", err)
	}
	if len(backups) != 2 || backups[0].ID == 0 || backups[1].ID == 0 {
		t.Fatalf("unexpected backups: %+v", backups)
	}

	proxy := mustAddStoreTestServer(t, ctx, store, storeCoverageServer("proxy", model.ServerRoleProxy))
	backend := mustAddStoreTestServer(t, ctx, store, storeCoverageServer("backend", model.ServerRoleVPN))
	mapping := &model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: backend.ID, Enabled: true, Priority: 50}
	if err := store.AddProxyBackend(ctx, mapping); err != nil {
		t.Fatalf("add proxy backend: %v", err)
	}
	if mapping.ID == 0 {
		t.Fatalf("expected proxy backend id")
	}
	if err := store.DeleteProxyBackend(ctx, proxy.ID, backend.ID); err != nil {
		t.Fatalf("delete proxy backend: %v", err)
	}
	hasProxy, err := store.BackendHasAttachedProxy(ctx, backend.ID)
	if err != nil {
		t.Fatalf("backend has proxy: %v", err)
	}
	if hasProxy {
		t.Fatalf("expected deleted backend mapping to be absent")
	}
}

func storeCoverageServer(name string, role string) *model.Server {
	return &model.Server{
		Name:              name,
		Role:              role,
		Host:              name + ".example.net",
		Domain:            name + ".example.com",
		SSHUser:           "root",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
}
