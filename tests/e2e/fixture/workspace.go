package main

import (
	"context"
	"fmt"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

// workspace creates a two-node inventory with different eligible identities and
// enough users to exercise a real page boundary. Endpoints use an isolated
// fixture address and the workspace has no deployment credentials.
func workspace(ctx context.Context, dir string) error {
	store, err := local.Open(ctx, dir)
	if err != nil {
		return err
	}
	defer store.Close()
	for i, name := range []string{"Primary fixture", "Backup fixture"} {
		server := model.Server{
			Name: name, Role: model.ServerRoleVPN, Enabled: true,
			Host: "127.0.0.1", Domain: "fixture.invalid", SSHUser: "fixture", SSHPort: 22,
			XrayVersion: "fixture", RealityPrivateKey: "fixture-private-marker",
			RealityPublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			RealityShortIDs:  "abcd", RealityServerName: "fixture.invalid", RealityTarget: "fixture.invalid:443",
			PrimaryProfile: model.TransportProfileTLSSelfSNIWeb,
		}
		if i == 1 {
			server.PrimaryProfile = model.TransportProfileRealityTCPVision
		}
		if err := store.AddServer(ctx, &server); err != nil {
			return err
		}
		name := []string{"alice", "bob"}[i]
		if err := store.AddUser(ctx, &model.User{
			ServerID: server.ID, Username: name, Email: name + "@example.test",
			UUID: fmt.Sprintf("11111111-1111-4111-8111-%012d", i+1), Enabled: true, QuotaEnabled: true,
		}); err != nil {
			return err
		}
		if i == 0 {
			expired := time.Now().Add(-time.Hour).UTC()
			for n := range 52 {
				username := fmt.Sprintf("expired-%02d", n)
				if err := store.AddUser(ctx, &model.User{
					ServerID: server.ID, Username: username, Email: username + "@example.test",
					UUID: fmt.Sprintf("22222222-2222-4222-8222-%012d", n), Enabled: true, ExpiryDate: &expired,
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func disableAlice(ctx context.Context, dir string) error {
	store, err := local.Open(ctx, dir)
	if err != nil {
		return err
	}
	defer store.Close()
	server, err := store.GetServerByName(ctx, "Primary fixture")
	if err != nil {
		return err
	}
	user, err := store.GetUser(ctx, server.ID, "alice")
	if err != nil {
		return err
	}
	user.Enabled = false
	return store.UpdateUser(ctx, user)
}

func haWorkspace(ctx context.Context, dir string) error {
	st, err := local.Open(ctx, dir)
	if err != nil {
		return err
	}
	defer st.Close()
	first, err := st.GetServerByName(ctx, "Primary fixture")
	if err != nil {
		return err
	}
	second, err := st.GetServerByName(ctx, "Backup fixture")
	if err != nil {
		return err
	}
	first.EnabledProfiles = model.TransportProfileRealityTCPVision + "," + model.TransportProfileTLSSelfSNIWeb
	for _, server := range []*model.Server{first, second} {
		server.ProxyServiceUUID = "33333333-3333-4333-8333-333333333333"
		if err := st.UpdateServer(ctx, server); err != nil {
			return err
		}
	}
	proxy := *first
	proxy.ID = 0
	proxy.Name = "Entry fixture"
	proxy.Role = model.ServerRoleProxy
	proxy.ProxyPreset = model.ProxyPresetCN
	if err := st.AddServer(ctx, &proxy); err != nil {
		return err
	}
	for i, server := range []*model.Server{first, second} {
		if err := st.AddProxyBackend(ctx, &model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: server.ID, Enabled: true, Priority: i + 1}); err != nil {
			return err
		}
	}
	return nil
}
