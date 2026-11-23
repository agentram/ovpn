package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/operations"
	"ovpn/internal/subscription"
)

func TestManagedSubscriptionUsesOnlyAppliedClientNodes(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	main, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	direct := addSubscriptionServer(t, app, "direct", true)
	addSubscriptionServer(t, app, "relay-only", true)
	alice, err := app.store.GetUser(app.ctx, main.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.SetSubscriptionNode(app.ctx, main.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SetSubscriptionNode(app.ctx, direct.ID, true); err != nil {
		t.Fatal(err)
	}
	enabled, err := app.store.SubscriptionNodes(app.ctx)
	if err != nil || len(enabled) != 0 {
		t.Fatal("pending deploy was published", err)
	}
	if err := app.store.SaveAppliedSubscriptionNode(app.ctx, *main); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveAppliedSubscriptionNode(app.ctx, direct); err != nil {
		t.Fatal(err)
	}
	originalDomain := direct.Domain
	direct.Domain = "pending.example.net"
	if err := app.store.UpdateServer(app.ctx, &direct); err != nil {
		t.Fatal(err)
	}
	enabled, err = app.store.SubscriptionNodes(app.ctx)
	if err != nil || len(enabled) != 2 {
		t.Fatal("wrong public node selection", err)
	}

	key := subscriptionIdentity(alice.UUID)
	verified := map[string]bool{"alice\x00" + key: true}
	nodes := []subscriptionNode{
		{server: enabled[0], known: true, users: verified},
		{server: enabled[1], known: true, users: verified},
	}
	bundle := subscription.Bundle{Owner: "alice", ManagedKey: key}
	profiles, err := app.managedEntries(bundle, nodes)
	if err != nil || len(profiles) != 2 || !strings.Contains(profiles[0].Link, originalDomain) {
		t.Fatal("failed to use the applied route for the new server", err)
	}
	nodes[0].known = false
	bundle.Entries = profiles
	saved, err := app.managedEntries(bundle, nodes)
	if err != nil || len(saved) != 2 || saved[0].Link != profiles[0].Link {
		t.Fatal("temporarily unreachable node did not keep its last route", err)
	}
	identity, err := app.store.GetUser(app.ctx, direct.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	identity.Enabled = false
	if err = app.store.UpdateUser(app.ctx, identity); err != nil {
		t.Fatal(err)
	}
	saved, err = app.managedEntries(bundle, nodes)
	if err != nil || len(saved) != 1 || saved[0].NodeID == direct.ID {
		t.Fatal("a local disable left the old route published", err)
	}
}

func TestSubscriptionPlanQueuesUserProvisioningBeforeDelivery(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	addSubscriptionServer(t, app, "second", true)
	control := deskControl{dataDir: filepath.Join(app.dataDir, "data")}
	plan, err := control.Prepare(context.Background(), operations.Request{
		Action: createSubscription, Values: map[string]string{"username": "pilot", "base-url": "https://subscriptions.example.net"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 3 || plan.Targets[2].ID != "workspace" || plan.Request.Values["uuid"] == "" {
		t.Fatal("publication must follow the reviewed node operations")
	}
}
