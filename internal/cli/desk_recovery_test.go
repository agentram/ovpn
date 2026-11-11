package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"ovpn/internal/model"
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

func TestDisabledUserVerificationDoesNotRequireAnActiveQuotaPolicy(t *testing.T) {
	a := newTestAppWithLinkedUser(t)
	server, err := a.store.GetServerByName(a.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	user, err := a.store.GetUser(a.ctx, server.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	user.Enabled = false
	if err = a.store.UpdateUser(a.ctx, user); err != nil {
		t.Fatal(err)
	}
	a.operationTarget = server.ID
	a.remoteHTTPHook = func(_ model.Server, _ string, _ string, _ any) ([]byte, error) {
		return json.Marshal(model.UserStatusResponse{Users: []model.UserAccessStatus{{Username: user.Username, UUID: user.UUID, Email: user.Email, Enabled: false, EffectiveEnabled: false, QuotaPolicyKnown: false, QuotaEnabled: false}}})
	}
	if err = a.verifyUserOperation(operations.Plan{Action: "user.disable", Request: operations.Request{Values: map[string]string{"username": "alice"}}}); err != nil {
		t.Fatal("disabled access was mistaken for a quota-application failure", err)
	}
}

func TestLocalRepairPlansItsTargetsWithoutSSHAndPreservesThePreview(t *testing.T) {
	a := newTestAppWithLinkedUser(t)
	source, err := a.store.GetServerByName(a.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	target := *source
	target.ID = 0
	target.Name = "repair-target"
	target.Domain = "repair.example.test"
	if err := a.store.AddServer(a.ctx, &target); err != nil {
		t.Fatal(err)
	}
	d := deskControl{dataDir: filepath.Join(a.dataDir, "data")}
	request := operations.Request{Action: "user.reconcile", Values: map[string]string{"from-server": "main", "to-server": target.Name}}
	preview, err := d.Prepare(a.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Mutation || len(preview.Targets) != 1 || preview.Targets[0].Name != target.Name || remoteMutation("user.reconcile") {
		t.Fatal("local preview acquired a remote write scope")
	}
	if _, err = d.Apply(a.ctx, preview, preview.Targets[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err = a.store.GetUser(a.ctx, target.ID, "alice"); err == nil {
		t.Fatal("preview wrote a user")
	}
	request.Values["apply"] = "1"
	plan, err := d.Prepare(a.ctx, request)
	if err != nil || !plan.Mutation || !plan.Destructive {
		t.Fatal("apply bypassed the reviewed write/confirmation", err)
	}
	if _, err = d.Apply(a.ctx, plan, plan.Targets[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err = a.store.GetUser(a.ctx, target.ID, "alice"); err != nil {
		t.Fatal("reviewed local repair did not copy the account", err)
	}
}

func TestReviewedRevisionAndUserRecoveryIncludeBackendAssignments(t *testing.T) {
	a := newTestAppWithLinkedUser(t)
	source, err := a.store.GetServerByName(a.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	proxy := *source
	proxy.ID = 0
	proxy.Name = "proxy-fixture"
	proxy.Role = model.ServerRoleProxy
	proxy.Enabled = false
	if err := a.store.AddServer(a.ctx, &proxy); err != nil {
		t.Fatal(err)
	}
	d := deskControl{dataDir: filepath.Join(a.dataDir, "data")}
	plan, err := d.Prepare(a.ctx, operations.Request{Action: "user.disable", Values: map[string]string{"username": "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	attachment := model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: source.ID, Enabled: true, Priority: 10}
	if err := a.store.AddProxyBackend(a.ctx, &attachment); err != nil {
		t.Fatal(err)
	}
	revision, err := d.Revision(a.ctx)
	if err != nil || revision == plan.Revision {
		t.Fatal("backend edit did not invalidate the reviewed topology", err)
	}
	if err := d.validateUserRecovery(a.ctx, plan); err != operations.ErrStale {
		t.Fatal("recovery accepted unrelated backend edits", err)
	}
}
