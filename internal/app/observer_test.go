package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"ovpn/internal/app/dto"
	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

func TestObservationUsesClientOnlyStateAndRejectsInactiveIdentities(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := local.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := model.Server{Name: "edge-a", Host: "node.example.net", Domain: "node.example.net", SSHUser: "private-operator", SSHPort: 22,
		SSHIdentityFile: "/private/ssh-key", XrayVersion: "26.3.27", RealityPrivateKey: "private-reality-key", RealityPublicKey: "public-key", RealityShortIDs: "abcd",
		RealityServerName: "example.com", RealityTarget: "example.com:443", Enabled: true}
	if err := st.AddServer(ctx, &srv); err != nil {
		t.Fatal(err)
	}
	u := model.User{ServerID: srv.ID, Username: "monitor", UUID: "11111111-1111-1111-1111-111111111111", Email: "monitor@example.net", Enabled: true}
	if err := st.AddUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	svc := ReadService{DataDir: dir}
	target := dto.ObservationTarget{ServerID: fmt.Sprintf("node-%d", srv.ID), Username: u.Username, Profile: model.TransportProfileRealityTCPVision}
	prepared, err := svc.prepareObservation(ctx, target, "native")
	if err != nil || prepared.Revision == "" || prepared.Link == "" {
		t.Fatal("client preparation failed", err)
	}
	raw, err := json.Marshal(prepared.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-operator", "/private/ssh-key", "private-reality-key", u.Username, u.Email} {
		if strings.Contains(string(raw), private) || strings.Contains(prepared.Link, private) {
			t.Fatal("management material or identity label reached a tool")
		}
	}
	u.UUID = "22222222-2222-2222-2222-222222222222"
	if err := st.UpdateUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	changed, err := svc.prepareObservation(ctx, target, "native")
	if err != nil || changed.Revision == prepared.Revision {
		t.Fatal("credential replacement did not change evidence revision", err)
	}
	u.Enabled = false
	if err := st.UpdateUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.prepareObservation(ctx, target, "native"); err == nil || err.Error() != "TARGET_UNAVAILABLE" {
		t.Fatal("inactive identity accepted", err)
	}
}
