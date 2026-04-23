package cli

import (
	"context"
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestServerBackendAttachValidatesExistingPool(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")
	backendA := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "vpn-pub", "svc-shared")
	backendB := addServerBackendTestServer(t, app, "backend-b", model.ServerRoleVPN, "vpn-pub", "svc-other")

	cmd := app.serverCmd()
	cmd.SetArgs([]string{"backend", "attach", "--proxy", proxy.Name, "--backend", backendA.Name})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("attach backend-a: %v", err)
	}

	cmd = app.serverCmd()
	cmd.SetArgs([]string{"backend", "attach", "--proxy", proxy.Name, "--backend", backendB.Name})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected parity error when attaching mismatched backend")
	}
	if !strings.Contains(err.Error(), "REALITY parity check failed") || !strings.Contains(err.Error(), "proxy_service_uuid") {
		t.Fatalf("unexpected attach error: %v", err)
	}

	mappings, err := app.store.ListProxyBackends(app.ctx, proxy.ID)
	if err != nil {
		t.Fatalf("list proxy backends: %v", err)
	}
	if len(mappings) != 1 || mappings[0].BackendServer == nil || mappings[0].BackendServer.Name != backendA.Name {
		t.Fatalf("unexpected backend mappings: %+v", mappings)
	}
}

func TestServerBackendAttachAssignsSharedProxyServiceUUID(t *testing.T) {
	t.Parallel()

	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-ru", model.ServerRoleProxy, "proxy-pub", "")
	backendA := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "vpn-pub", "")
	backendB := addServerBackendTestServer(t, app, "backend-b", model.ServerRoleVPN, "vpn-pub", "")

	cmd := app.serverCmd()
	cmd.SetArgs([]string{"backend", "attach", "--proxy", proxy.Name, "--backend", backendA.Name})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("attach backend-a: %v", err)
	}

	cmd = app.serverCmd()
	cmd.SetArgs([]string{"backend", "attach", "--proxy", proxy.Name, "--backend", backendB.Name})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("attach backend-b: %v", err)
	}

	reloadedA, err := app.store.GetServerByName(app.ctx, backendA.Name)
	if err != nil {
		t.Fatalf("reload backend-a: %v", err)
	}
	reloadedB, err := app.store.GetServerByName(app.ctx, backendB.Name)
	if err != nil {
		t.Fatalf("reload backend-b: %v", err)
	}
	if strings.TrimSpace(reloadedA.ProxyServiceUUID) == "" {
		t.Fatalf("expected backend-a to receive a proxy service uuid")
	}
	if reloadedA.ProxyServiceUUID != reloadedB.ProxyServiceUUID {
		t.Fatalf("expected attached backends to share proxy service uuid, got %q vs %q", reloadedA.ProxyServiceUUID, reloadedB.ProxyServiceUUID)
	}
}

func addServerBackendTestServer(t *testing.T, app *App, name, role, publicKey, proxyServiceUUID string) *model.Server {
	t.Helper()

	srv := &model.Server{
		Name:              name,
		Role:              role,
		Host:              "127.0.0.1",
		Domain:            name + ".example.com",
		SSHUser:           "root",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  publicKey,
		RealityShortIDs:   "abcd1234",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ProxyServiceUUID:  proxyServiceUUID,
		Enabled:           true,
	}
	if err := app.store.AddServer(context.Background(), srv); err != nil {
		t.Fatalf("add server %s: %v", name, err)
	}
	return srv
}
