package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"ovpn/internal/haproxy"
	"path/filepath"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/operations"
)

func TestHAPlanOrdersReviewedBackendsBeforeProxyAndRejectsIncompatibleChanges(t *testing.T) {
	a := newTestAppWithLinkedUser(t)
	proxy := addServerBackendTestServer(t, a, "entry-fixture", model.ServerRoleProxy, "entry-public", "")
	first := addServerBackendTestServer(t, a, "relay-a", model.ServerRoleVPN, "pool-public", "shared-relay")
	second := addServerBackendTestServer(t, a, "relay-b", model.ServerRoleVPN, "pool-public", "shared-relay")
	for _, item := range []struct {
		server   *model.Server
		priority int
	}{{first, 20}, {second, 10}} {
		if err := a.store.AddProxyBackend(a.ctx, &model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: item.server.ID, Enabled: true, Priority: item.priority}); err != nil {
			t.Fatal(err)
		}
	}
	d := deskControl{dataDir: filepath.Join(a.dataDir, "data")}
	request := operations.Request{Action: "server.ha-apply", Arguments: []string{proxy.Name}}
	plan, err := d.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.StopOnFailure || !plan.Mutation || len(plan.Targets) != 3 || plan.Targets[0].Name != second.Name || plan.Targets[1].Name != first.Name || plan.Targets[2].Name != proxy.Name {
		t.Fatalf("unsafe HA order: %+v", plan.Targets)
	}
	second.RealityPublicKey = "different"
	if err := a.store.UpdateServer(a.ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Prepare(a.ctx, request); err == nil || err.Error() != "HA_BACKENDS_INCOMPATIBLE" {
		t.Fatal("incompatible pool reached remote apply", err)
	}
	request.Arguments = []string{first.Name}
	if _, err := d.Prepare(a.ctx, request); err == nil {
		t.Fatal("ordinary server accepted as HA entry")
	}
}

func TestHAApplyVerificationRejectsMissingWrongAndFailedPoolEvidence(t *testing.T) {
	for _, kind := range []string{"healthy", "missing", "wrong-node", "down"} {
		t.Run(kind, func(t *testing.T) {
			a := newTestAppWithLinkedUser(t)
			proxy := addServerBackendTestServer(t, a, "entry-check", model.ServerRoleProxy, "entry-public", "")
			backend := addServerBackendTestServer(t, a, "relay-check", model.ServerRoleVPN, "pool-public", "shared-relay")
			if err := a.store.AddProxyBackend(a.ctx, &model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: backend.ID, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			a.operationTarget = proxy.ID
			ctx, cancel := context.WithTimeout(a.ctx, time.Second)
			defer cancel()
			a.ctx = ctx
			node := fmt.Sprintf("node-%d", backend.ID)
			if kind == "wrong-node" {
				node = "node-999"
			}
			status := haproxy.Status{Supported: kind != "missing", State: "online", CheckedAt: time.Now().UTC().Format(time.RFC3339), Backends: []haproxy.Backend{{ServerID: node, Available: new(bool)}}}
			*status.Backends[0].Available = kind != "down"
			a.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) { return json.Marshal(status) }
			err := a.verifyHAOperation()
			if (err == nil) != (kind == "healthy") {
				t.Fatal("HA apply accepted unverified pool evidence", err)
			}
		})
	}
}
