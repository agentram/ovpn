package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestEnsureProxyGeodataAssetsRequiresPairedOverrides(t *testing.T) {
	app := newGlobalUsersTestApp(t)
	geositePath := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(geositePath, []byte("geosite"), 0o644); err != nil {
		t.Fatalf("write geosite: %v", err)
	}
	t.Setenv("OVPN_PROXY_GEOSITE_PATH", geositePath)
	t.Setenv("OVPN_PROXY_GEOIP_PATH", "")

	_, _, err := app.ensureProxyGeodataAssets()
	if err == nil || !strings.Contains(err.Error(), "OVPN_PROXY_GEOIP_PATH is required") {
		t.Fatalf("expected paired override error, got %v", err)
	}
}

func TestEnsureProxyGeodataAssetsUsesExplicitOverrides(t *testing.T) {
	app := newGlobalUsersTestApp(t)
	dir := t.TempDir()
	geositePath := filepath.Join(dir, "geosite.dat")
	geoipPath := filepath.Join(dir, "geoip.dat")
	if err := os.WriteFile(geositePath, []byte("geosite"), 0o644); err != nil {
		t.Fatalf("write geosite: %v", err)
	}
	if err := os.WriteFile(geoipPath, []byte("geoip"), 0o644); err != nil {
		t.Fatalf("write geoip: %v", err)
	}
	t.Setenv("OVPN_PROXY_GEOSITE_PATH", geositePath)
	t.Setenv("OVPN_PROXY_GEOIP_PATH", geoipPath)

	gotGeosite, gotGeoip, err := app.ensureProxyGeodataAssets()
	if err != nil {
		t.Fatalf("ensureProxyGeodataAssets: %v", err)
	}
	if gotGeosite != geositePath || gotGeoip != geoipPath {
		t.Fatalf("unexpected override paths: geosite=%q geoip=%q", gotGeosite, gotGeoip)
	}
}

func TestBuildXraySpecVPNOmitsProxyServiceUserWhenUnattached(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	backend := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "vpn-pub", "11111111-1111-1111-1111-111111111111")
	spec, err := app.buildXraySpec(*backend, nil)
	if err != nil {
		t.Fatalf("buildXraySpec: %v", err)
	}
	if len(spec.ServiceUsers) != 0 {
		t.Fatalf("expected no service users for unattached vpn backend, got %+v", spec.ServiceUsers)
	}
	if spec.ProxyRelay != nil {
		t.Fatalf("vpn spec should not include proxy relay")
	}
}

func TestBuildXraySpecVPNIncludesProxyServiceUserWhenAttached(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")
	backend := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "vpn-pub", "11111111-1111-1111-1111-111111111111")
	if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{
		ProxyServerID:   proxy.ID,
		BackendServerID: backend.ID,
		Enabled:         true,
		Priority:        10,
	}); err != nil {
		t.Fatalf("attach backend: %v", err)
	}

	spec, err := app.buildXraySpec(*backend, nil)
	if err != nil {
		t.Fatalf("buildXraySpec: %v", err)
	}
	if len(spec.ServiceUsers) != 1 {
		t.Fatalf("expected one service user, got %d", len(spec.ServiceUsers))
	}
	if spec.ServiceUsers[0].UUID != "11111111-1111-1111-1111-111111111111" || spec.ServiceUsers[0].Email != proxyServiceEmail() {
		t.Fatalf("unexpected service user: %+v", spec.ServiceUsers[0])
	}
}

func TestBuildXraySpecVPNFailsWhenAttachedBackendMissingProxyServiceUUID(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")
	backend := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "vpn-pub", "")
	if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{
		ProxyServerID:   proxy.ID,
		BackendServerID: backend.ID,
		Enabled:         true,
		Priority:        10,
	}); err != nil {
		t.Fatalf("attach backend: %v", err)
	}

	_, err := app.buildXraySpec(*backend, nil)
	if err == nil || !strings.Contains(err.Error(), "proxy_service_uuid is empty") {
		t.Fatalf("expected missing proxy service uuid error, got %v", err)
	}
}

func TestBuildXraySpecProxyFailsWithoutBackends(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")

	_, err := app.buildXraySpec(*proxy, nil)
	if err == nil || !strings.Contains(err.Error(), "no attached backends") {
		t.Fatalf("expected no-backends error, got %v", err)
	}
}

func TestBuildXraySpecProxyIncludesRelayFromAttachedBackends(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")
	backend := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "vpn-pub", "22222222-2222-2222-2222-222222222222")
	if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{
		ProxyServerID:   proxy.ID,
		BackendServerID: backend.ID,
		Enabled:         true,
		Priority:        10,
	}); err != nil {
		t.Fatalf("attach backend: %v", err)
	}

	spec, err := app.buildXraySpec(*proxy, nil)
	if err != nil {
		t.Fatalf("buildXraySpec: %v", err)
	}
	if spec.ProxyRelay == nil {
		t.Fatalf("expected proxy relay in proxy spec")
	}
	if spec.ProxyRelay.Address != proxyHAProxyAddress || spec.ProxyRelay.Port != proxyHAProxyListenPort {
		t.Fatalf("unexpected proxy relay address: %+v", spec.ProxyRelay)
	}
	if spec.ProxyRelay.ServiceUUID != backend.ProxyServiceUUID {
		t.Fatalf("unexpected proxy relay service uuid: %+v", spec.ProxyRelay)
	}
	if spec.ProxyRelay.PublicKey != backend.RealityPublicKey || spec.ProxyRelay.ShortID != "abcd1234" {
		t.Fatalf("unexpected proxy relay reality settings: %+v", spec.ProxyRelay)
	}
	if len(spec.ServiceUsers) != 0 {
		t.Fatalf("proxy spec should not expose backend service users: %+v", spec.ServiceUsers)
	}
}

func TestCheckProxyTopologyFailsWithoutBackends(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")

	check := app.checkProxyTopology(*proxy)
	if check.Status != "fail" {
		t.Fatalf("expected fail status, got %+v", check)
	}
	if !strings.Contains(strings.ToLower(check.Message), "no attached backends") {
		t.Fatalf("unexpected check message: %+v", check)
	}
}

func TestCheckProxyTopologyFailsForIncompatibleBackends(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")
	backendA := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "vpn-pub", "svc-a")
	backendB := addServerBackendTestServer(t, app, "backend-b", model.ServerRoleVPN, "vpn-pub", "svc-b")
	for idx, backend := range []*model.Server{backendA, backendB} {
		if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{
			ProxyServerID:   proxy.ID,
			BackendServerID: backend.ID,
			Enabled:         true,
			Priority:        10 + idx,
		}); err != nil {
			t.Fatalf("attach backend %s: %v", backend.Name, err)
		}
	}

	check := app.checkProxyTopology(*proxy)
	if check.Status != "fail" {
		t.Fatalf("expected fail status, got %+v", check)
	}
	if !strings.Contains(check.Message, "not parity-compatible") || len(check.Details) == 0 || !strings.Contains(check.Details[0], "proxy_service_uuid") {
		t.Fatalf("unexpected proxy topology check: %+v", check)
	}
}

func TestCheckProxyTopologyWarnsWhenGeodataIsStale(t *testing.T) {
	dir := t.TempDir()
	geositePath := filepath.Join(dir, "geosite.dat")
	geoipPath := filepath.Join(dir, "geoip.dat")
	for path, content := range map[string]string{
		geositePath: "geosite",
		geoipPath:   "geoip",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write geodata asset %s: %v", path, err)
		}
		old := time.Now().Add(-proxyGeodataWarnAfter - 2*time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	app := newGlobalUsersTestApp(t)
	t.Setenv("OVPN_PROXY_GEOSITE_PATH", geositePath)
	t.Setenv("OVPN_PROXY_GEOIP_PATH", geoipPath)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")
	backend := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "vpn-pub", "svc-a")
	if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{
		ProxyServerID:   proxy.ID,
		BackendServerID: backend.ID,
		Enabled:         true,
		Priority:        10,
	}); err != nil {
		t.Fatalf("attach backend: %v", err)
	}

	check := app.checkProxyTopology(*proxy)
	if check.Status != "warn" {
		t.Fatalf("expected warn status for stale geodata, got %+v", check)
	}
	if !strings.Contains(check.Message, "geodata assets are stale") {
		t.Fatalf("unexpected proxy topology warning: %+v", check)
	}
}
