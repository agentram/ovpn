package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ovpn/internal/operations"
)

func TestRecoveryRefusesUnrelatedChangesAndPreservesTrafficPolicy(t *testing.T) {
	a := newTestAppWithLinkedUser(t)
	d := deskControl{dataDir: filepath.Join(a.dataDir, "data")}
	p, err := d.Prepare(context.Background(), operations.Request{Action: "user.disable", Values: map[string]string{"username": "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	// Resume permits the intended changed field, without resetting accumulated stats.
	server, err := a.store.GetServerByName(a.ctx, "main")
	if err != nil {
		servers, _ := a.store.ListServers(a.ctx)
		server = &servers[0]
	}
	user, err := a.store.GetUser(a.ctx, server.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	user.Enabled = false
	if err = a.store.UpdateUser(a.ctx, user); err != nil {
		t.Fatal(err)
	}
	if err = d.validateUserRecovery(a.ctx, p); err != nil {
		t.Fatal("own applied checkpoint rejected", err)
	}
	deployed := time.Now().UTC()
	server.LastDeployAt = &deployed
	if err = a.store.UpdateServer(a.ctx, server); err != nil {
		t.Fatal(err)
	}
	if err = d.validateUserRecovery(a.ctx, p); err != nil {
		t.Fatal("own fallback deployment prevented reconciliation", err)
	}
	user.Notes = "another operator changed this"
	if err = a.store.UpdateUser(a.ctx, user); err != nil {
		t.Fatal(err)
	}
	if err = d.validateUserRecovery(a.ctx, p); err != operations.ErrStale {
		t.Fatal("unrelated change was overwritten", err)
	}
}
func TestInternalInvocationRetainsItsExplicitWorkspace(t *testing.T) {
	a := &App{ctx: context.Background(), internal: true, dataDir: t.TempDir()}
	expected := a.dataDir
	if _, err := prepareInvocation(a, operations.Request{Action: "version"}); err != nil {
		t.Fatal(err)
	}
	if a.dataDir != expected {
		t.Fatal("internal invocation replaced its selected workspace")
	}
}
