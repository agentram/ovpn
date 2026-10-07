package cli

import (
	"slices"
	"strings"
	"testing"

	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

func TestProxyProfileSwitchToSelfSNIRendersWithRealityBackend(t *testing.T) {
	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-a", model.ServerRoleProxy, "proxy-pub", "")
	backend := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "backend-pub", "service-uuid")
	if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: backend.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	cmd := app.newServerProfileSwitchCmd()
	cmd.SetArgs([]string{proxy.Name, model.TransportProfileTLSSelfSNIWeb})
	if _, _, err := captureStdoutStderr(t, cmd.Execute); err != nil {
		t.Fatal(err)
	}
	updated, err := app.store.GetServerByName(app.ctx, proxy.Name)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NormalizedPrimaryProfile() != model.TransportProfileTLSSelfSNIWeb || updated.IsTransportProfileEnabled(model.TransportProfileRealityTCPVision) {
		t.Fatalf("unexpected proxy profiles: %s", updated.EnabledProfiles)
	}
	spec, err := app.buildXraySpec(*updated, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xraycfg.RenderServerJSON(spec); err != nil {
		t.Fatalf("selected proxy profile must render: %v", err)
	}
	link, err := buildUserProfileLink(*updated, model.User{Username: "alice", UUID: "11111111-1111-1111-1111-111111111111"}, model.TransportProfileTLSSelfSNIWeb)
	if err != nil || !strings.Contains(link, "@proxy-a.example.com:443") || !strings.Contains(link, "security=tls") || strings.Contains(link, "pbk=") {
		t.Fatalf("client link must use proxy TLS rather than backend REALITY: %s, %v", link, err)
	}
	unchanged, err := app.store.GetServerByName(app.ctx, backend.Name)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.IsTransportProfileEnabled(model.TransportProfileRealityTCPVision) || unchanged.ProxyServiceUUID != backend.ProxyServiceUUID {
		t.Fatal("changing proxy ingress must preserve backend REALITY and service credentials")
	}
}

func TestAttachedBackendCannotLoseRealityProfile(t *testing.T) {
	for _, command := range []string{"switch", "disable"} {
		t.Run(command, func(t *testing.T) {
			app := newGlobalUsersTestApp(t)
			proxy := addServerBackendTestServer(t, app, "proxy-a", model.ServerRoleProxy, "proxy-pub", "")
			backend := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "backend-pub", "service-uuid")
			backend.PrimaryProfile = model.TransportProfilePlainXHTTP
			backend.EnabledProfiles = model.TransportProfilePlainXHTTP + "," + model.TransportProfileRealityTCPVision
			if err := app.store.UpdateServer(app.ctx, backend); err != nil {
				t.Fatal(err)
			}
			if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: backend.ID, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			profile := model.TransportProfileTLSSelfSNIWeb
			if command == "disable" {
				profile = model.TransportProfileRealityTCPVision
			}
			cmd := app.serverCmd()
			cmd.SetArgs([]string{"profile", command, backend.Name, profile})
			_, _, err := captureStdoutStderr(t, cmd.Execute)
			if err == nil || !strings.Contains(err.Error(), "attached to a proxy") {
				t.Fatalf("expected backend relay protection: %v", err)
			}
			unchanged, err := app.store.GetServerByName(app.ctx, backend.Name)
			if err != nil {
				t.Fatal(err)
			}
			if unchanged.PrimaryProfile != backend.PrimaryProfile || unchanged.EnabledProfiles != backend.EnabledProfiles {
				t.Fatal("rejected profile change mutated the backend")
			}
			if err := app.store.DeleteProxyBackend(app.ctx, proxy.ID, backend.ID); err != nil {
				t.Fatal(err)
			}
			cmd = app.serverCmd()
			cmd.SetArgs([]string{"profile", command, backend.Name, profile})
			if _, _, err := captureStdoutStderr(t, cmd.Execute); err != nil {
				t.Fatalf("detached backend must allow the profile change: %v", err)
			}
		})
	}
}

func TestProxyRejectsBackendWithoutRealityListener(t *testing.T) {
	app := newGlobalUsersTestApp(t)
	proxy := addServerBackendTestServer(t, app, "proxy-a", model.ServerRoleProxy, "proxy-pub", "")
	backend := addServerBackendTestServer(t, app, "backend-a", model.ServerRoleVPN, "backend-pub", "")
	backend.PrimaryProfile = model.TransportProfileTLSSelfSNIWeb
	backend.EnabledProfiles = model.TransportProfileTLSSelfSNIWeb
	if err := app.store.UpdateServer(app.ctx, backend); err != nil {
		t.Fatal(err)
	}
	cmd := app.serverCmd()
	cmd.SetArgs([]string{"backend", "attach", "--proxy", proxy.Name, "--backend", backend.Name})
	_, _, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "requires "+model.TransportProfileRealityTCPVision) {
		t.Fatalf("expected incompatible listener error: %v", err)
	}
	backends, err := app.store.ListProxyBackends(app.ctx, proxy.ID)
	if err != nil || len(backends) != 0 {
		t.Fatalf("rejected attachment must not be saved: %+v, %v", backends, err)
	}
	unchanged, err := app.store.GetServerByName(app.ctx, backend.Name)
	if err != nil || unchanged.ProxyServiceUUID != "" {
		t.Fatalf("rejected attachment must not generate service credentials: %v", err)
	}
	if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: backend.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.buildXraySpec(*proxy, nil); err == nil || !strings.Contains(err.Error(), "requires "+model.TransportProfileRealityTCPVision) {
		t.Fatalf("existing invalid topology must fail before deploy: %v", err)
	}
}

func TestProxySelfSNISyncsUserAndQuotaPolicies(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	srv.Role = model.ServerRoleProxy
	srv.PrimaryProfile = model.TransportProfileTLSSelfSNIWeb
	srv.EnabledProfiles = model.TransportProfileTLSSelfSNIWeb + "," + model.TransportProfilePlainXHTTP
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatal(err)
	}
	var quotaTags, userTags []string
	app.remoteHTTPHook = func(_ model.Server, method, url string, payload any) ([]byte, error) {
		body := payload.(map[string]any)
		switch {
		case method == "POST" && strings.HasSuffix(url, "/quota/sync"):
			for _, policy := range body["users"].([]model.QuotaUserPolicy) {
				if !policy.QuotaEnabled || policy.Email != "alice@example.com" {
					t.Fatalf("incorrect quota policy: %+v", policy)
				}
				quotaTags = append(quotaTags, policy.InboundTag)
			}
		case method == "POST" && strings.HasSuffix(url, "/users/sync"):
			for _, policy := range body["users"].([]model.UserPolicy) {
				if !policy.Enabled || policy.Username != "alice" {
					t.Fatalf("incorrect user policy: %+v", policy)
				}
				userTags = append(userTags, policy.InboundTag)
			}
		default:
			t.Fatalf("unexpected policy call: %s %s", method, url)
		}
		return []byte(`{"ok":true}`), nil
	}
	if err := app.syncQuotaPolicy(*srv); err != nil {
		t.Fatal(err)
	}
	if err := app.syncUserPolicies(*srv); err != nil {
		t.Fatal(err)
	}
	want := model.TransportProfileTLSSelfSNIWeb + "," + model.TransportProfilePlainXHTTP
	slices.Sort(quotaTags)
	slices.Sort(userTags)
	if strings.Join(quotaTags, ",") != want || strings.Join(userTags, ",") != want {
		t.Fatalf("policies must cover both proxy client inbounds: quota=%v users=%v", quotaTags, userTags)
	}
}
