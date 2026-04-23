package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

func TestResolveUserMutationServersDefaultsToAllEnabled(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	srv1 := addGlobalUsersTestServer(t, app.store, "main-1")
	_ = addGlobalUsersTestServer(t, app.store, "main-2")
	disabled := addGlobalUsersTestServer(t, app.store, "disabled")
	disabled.Enabled = false
	if err := app.store.UpdateServer(app.ctx, disabled); err != nil {
		t.Fatalf("disable server: %v", err)
	}

	targets, err := app.resolveUserMutationServers()
	if err != nil {
		t.Fatalf("resolve targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected 2 enabled targets, got %d", len(targets))
	}
	for _, target := range targets {
		if target.Name != srv1.Name && target.Name != "main-2" {
			t.Fatalf("unexpected target list: %+v", targets)
		}
	}
}

func TestCanonicalGlobalUsersDetectsConflicts(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	s1 := addGlobalUsersTestServer(t, app.store, "s1")
	s2 := addGlobalUsersTestServer(t, app.store, "s2")

	u1 := &model.User{ServerID: s1.ID, Username: "alice", UUID: "uuid-alice", Email: "alice@example.com", Enabled: true, QuotaEnabled: true}
	u2 := &model.User{ServerID: s2.ID, Username: "alice", UUID: "uuid-alice", Email: "alice+drift@example.com", Enabled: true, QuotaEnabled: true}
	if err := app.store.AddUser(app.ctx, u1); err != nil {
		t.Fatalf("add user s1: %v", err)
	}
	if err := app.store.AddUser(app.ctx, u2); err != nil {
		t.Fatalf("add user s2: %v", err)
	}

	_, err := app.canonicalGlobalUsers()
	if err == nil {
		t.Fatalf("expected conflict error")
	}
	if !strings.Contains(err.Error(), `user "alice"`) || !strings.Contains(err.Error(), "email") {
		t.Fatalf("unexpected conflict error: %v", err)
	}
}

func TestCanonicalGlobalUsersIgnoresDisabledServers(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	enabled := addGlobalUsersTestServer(t, app.store, "enabled")
	disabled := addGlobalUsersTestServer(t, app.store, "disabled")
	disabled.Enabled = false
	if err := app.store.UpdateServer(app.ctx, disabled); err != nil {
		t.Fatalf("disable server: %v", err)
	}

	if err := app.store.AddUser(app.ctx, &model.User{
		ServerID:     enabled.ID,
		Username:     "alice",
		UUID:         "uuid-alice",
		Email:        "alice@global",
		Enabled:      true,
		QuotaEnabled: true,
	}); err != nil {
		t.Fatalf("add enabled user: %v", err)
	}
	if err := app.store.AddUser(app.ctx, &model.User{
		ServerID:     disabled.ID,
		Username:     "alice",
		UUID:         "uuid-alice",
		Email:        "alice-drift@global",
		Enabled:      true,
		QuotaEnabled: true,
	}); err != nil {
		t.Fatalf("add disabled user: %v", err)
	}

	canonical, err := app.canonicalGlobalUsers()
	if err != nil {
		t.Fatalf("canonicalGlobalUsers should ignore disabled servers, got %v", err)
	}
	if got := canonical["alice"].Email; got != "alice@global" {
		t.Fatalf("unexpected canonical email: %q", got)
	}
}

func TestMaterializeCanonicalUsersOnServerSeedsMissingUsers(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	source := addGlobalUsersTestServer(t, app.store, "source")
	target := addGlobalUsersTestServer(t, app.store, "target")

	u := &model.User{
		ServerID:         source.ID,
		Username:         "alice",
		UUID:             "uuid-alice",
		Email:            "alice@example.com",
		Enabled:          true,
		QuotaEnabled:     true,
		TrafficLimitByte: int64Ptr(1024),
		Notes:            "note",
		TagsCSV:          "vip,team",
	}
	if err := app.store.AddUser(app.ctx, u); err != nil {
		t.Fatalf("add source user: %v", err)
	}
	if err := app.materializeCanonicalUsersOnServer(*target); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	got, err := app.store.GetUser(app.ctx, target.ID, "alice")
	if err != nil {
		t.Fatalf("get seeded user: %v", err)
	}
	if got.UUID != "uuid-alice" || got.Email != "alice@example.com" || !got.Enabled {
		t.Fatalf("unexpected seeded user: %+v", got)
	}
}

func TestMaterializePreservesExistingServerStyledEmail(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	source := addGlobalUsersTestServer(t, app.store, "germany-1")
	target := addGlobalUsersTestServer(t, app.store, "finland-1")

	u := &model.User{
		ServerID:     source.ID,
		Username:     "legacy-user",
		UUID:         "uuid-legacy",
		Email:        "legacy-user@node-a",
		Enabled:      true,
		QuotaEnabled: true,
	}
	if err := app.store.AddUser(app.ctx, u); err != nil {
		t.Fatalf("add source user: %v", err)
	}
	if err := app.materializeCanonicalUsersOnServer(*target); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	got, err := app.store.GetUser(app.ctx, target.ID, "legacy-user")
	if err != nil {
		t.Fatalf("get seeded user: %v", err)
	}
	if got.Email != "legacy-user@node-a" {
		t.Fatalf("expected preserved canonical email, got %q", got.Email)
	}
}

func TestEnsureRealityParityForServers(t *testing.T) {
	t.Parallel()

	base := model.Server{
		Name:              "a",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "id",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ProxyServiceUUID:  "svc-a",
	}
	other := base
	other.Name = "b"
	other.ProxyServiceUUID = "svc-b"
	if err := ensureRealityParityForServers([]model.Server{base, other}); err != nil {
		t.Fatalf("expected parity success, got %v", err)
	}
	other.RealityPublicKey = "pub-drift"
	if err := ensureRealityParityForServers([]model.Server{base, other}); err == nil {
		t.Fatalf("expected parity error")
	}
}

func TestEnsureRealityParityIgnoresDisabledServers(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	base := addGlobalUsersTestServer(t, app.store, "base")
	disabled := addGlobalUsersTestServer(t, app.store, "disabled")
	disabled.Enabled = false
	disabled.RealityPublicKey = "drift"
	if err := app.store.UpdateServer(app.ctx, disabled); err != nil {
		t.Fatalf("update disabled server: %v", err)
	}
	if err := app.ensureRealityParity(); err != nil {
		t.Fatalf("ensureRealityParity should ignore disabled servers; base=%+v disabled=%+v err=%v", base, disabled, err)
	}
}

func TestServerAddInheritsRealityBaselineWhenNotProvided(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	base := addGlobalUsersTestServer(t, app.store, "base")

	cmd := app.serverCmd()
	cmd.SetArgs([]string{"add", "--name", "new", "--host", "10.0.0.2", "--domain", "new.example.com"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("server add failed: %v", err)
	}
	got, err := app.store.GetServerByName(app.ctx, "new")
	if err != nil {
		t.Fatalf("load new server: %v", err)
	}
	if got.RealityPrivateKey != base.RealityPrivateKey ||
		got.RealityPublicKey != base.RealityPublicKey ||
		got.RealityShortIDs != base.RealityShortIDs ||
		got.RealityServerName != base.RealityServerName ||
		got.RealityTarget != base.RealityTarget {
		t.Fatalf("expected inherited reality settings, got %+v", got)
	}
}

func TestServerAddRejectsRealityMismatchAgainstBaseline(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	_ = addGlobalUsersTestServer(t, app.store, "base")

	cmd := app.serverCmd()
	cmd.SetArgs([]string{
		"add",
		"--name", "bad",
		"--host", "10.0.0.3",
		"--domain", "bad.example.com",
		"--reality-public-key", "different",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected mismatch error")
	}
	if !strings.Contains(err.Error(), "reality-public-key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServerAddIgnoresDisabledServersAsBaseline(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	disabled := addGlobalUsersTestServer(t, app.store, "old-disabled")
	disabled.Enabled = false
	disabled.RealityPublicKey = "old-pub"
	disabled.RealityPrivateKey = "old-priv"
	if err := app.store.UpdateServer(app.ctx, disabled); err != nil {
		t.Fatalf("update disabled server: %v", err)
	}

	cmd := app.serverCmd()
	cmd.SetArgs([]string{
		"add",
		"--name", "new-active",
		"--host", "10.0.0.9",
		"--domain", "new-active.example.com",
		"--reality-private-key", "new-priv",
		"--reality-public-key", "new-pub",
		"--reality-short-id", "abcd1234",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("server add should ignore disabled baseline, got: %v", err)
	}
	got, err := app.store.GetServerByName(app.ctx, "new-active")
	if err != nil {
		t.Fatalf("load new server: %v", err)
	}
	if got.RealityPublicKey != "new-pub" || got.RealityPrivateKey != "new-priv" {
		t.Fatalf("unexpected reality keys on new server: %+v", got)
	}
}

func TestServerAddDefaultsProxyPresetForProxyRole(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	_ = addGlobalUsersTestServer(t, app.store, "base")

	cmd := app.serverCmd()
	cmd.SetArgs([]string{
		"add",
		"--name", "proxy-ru",
		"--role", model.ServerRoleProxy,
		"--host", "10.0.0.20",
		"--domain", "proxy.example.com",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("server add proxy failed: %v", err)
	}

	got, err := app.store.GetServerByName(app.ctx, "proxy-ru")
	if err != nil {
		t.Fatalf("load proxy server: %v", err)
	}
	if got.NormalizedProxyPreset() != model.ProxyPresetRU {
		t.Fatalf("expected proxy preset %q, got %q", model.ProxyPresetRU, got.NormalizedProxyPreset())
	}
}

func TestServerAddRejectsProxyPresetForVPNRole(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	_ = addGlobalUsersTestServer(t, app.store, "base")

	cmd := app.serverCmd()
	cmd.SetArgs([]string{
		"add",
		"--name", "bad-vpn",
		"--host", "10.0.0.21",
		"--domain", "bad-vpn.example.com",
		"--proxy-preset", model.ProxyPresetRU,
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected proxy-preset validation error for vpn role")
	}
	if !strings.Contains(err.Error(), "proxy_preset is only supported for proxy role") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newGlobalUsersTestApp(t *testing.T) *App {
	t.Helper()
	ctx := context.Background()
	st, err := local.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &App{ctx: ctx, store: st, dryRun: true}
}

func addGlobalUsersTestServer(t *testing.T, st *local.Store, name string) *model.Server {
	t.Helper()
	s := &model.Server{
		Name:              name,
		Host:              "127.0.0.1",
		Domain:            name + ".example.com",
		SSHUser:           "root",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd1234",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ProxyServiceUUID:  "11111111-1111-1111-1111-111111111111",
		Enabled:           true,
	}
	if err := st.AddServer(context.Background(), s); err != nil {
		t.Fatalf("add server %s: %v", name, err)
	}
	return s
}

func int64Ptr(v int64) *int64 { return &v }
