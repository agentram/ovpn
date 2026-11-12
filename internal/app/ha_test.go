package app

import (
	"context"
	"encoding/json"
	"fmt"
	"ovpn/internal/model"
	"strings"
	"testing"
)

func TestHATopologyIsRedactedAndInvalidatesAccessChecksAfterPoolChanges(t *testing.T) {
	ctx := context.Background()
	svc, st, servers := planFixture(t, 2)
	servers[0].Role = model.ServerRoleProxy
	servers[0].ProxyPreset = model.ProxyPresetCN
	if err := st.UpdateServer(ctx, &servers[0]); err != nil {
		t.Fatal(err)
	}
	user := model.User{ServerID: servers[0].ID, Username: "fixture-user", UUID: "11111111-1111-1111-1111-111111111111", Email: "fixture@example.test", Enabled: true}
	if err := st.AddUser(ctx, &user); err != nil {
		t.Fatal(err)
	}
	source := deskSource{service: &DesktopService{DataDir: svc.DataDir}}
	before, err := source.Targets(ctx)
	if err != nil || len(before) == 0 {
		t.Fatal(err)
	}
	if err := st.AddProxyBackend(ctx, &model.ProxyBackend{ProxyServerID: servers[0].ID, BackendServerID: servers[1].ID, Enabled: true, Priority: 10}); err != nil {
		t.Fatal(err)
	}
	after, err := source.Targets(ctx)
	if err != nil || after[0].Revision == before[0].Revision {
		t.Fatal("pool edit retained successful old access evidence", err)
	}
	page, err := svc.Servers(ctx, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].ProxyPreset != model.ProxyPresetCN || len(page.Items[0].Backends) != 1 || page.Items[0].Backends[0].ServerID != fmt.Sprintf("node-%d", servers[1].ID) {
		t.Fatal("topology missing from inventory")
	}
	raw, _ := json.Marshal(page)
	for _, private := range []string{"private-host", "private-key", "fixture@example", "11111111"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private data escaped the topology")
		}
	}
}
