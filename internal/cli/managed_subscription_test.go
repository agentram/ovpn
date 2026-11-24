package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
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
	profiles, err := app.managedEntries(bundle, nodes, nil)
	if err != nil || len(profiles) != 2 || !strings.Contains(profiles[0].Link, originalDomain) {
		t.Fatal("failed to use the applied route for the new server", err)
	}
	nodes[0].known = false
	bundle.Entries = profiles
	saved, err := app.managedEntries(bundle, nodes, nil)
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
	saved, err = app.managedEntries(bundle, nodes, nil)
	if err != nil || len(saved) != 1 || saved[0].NodeID == direct.ID {
		t.Fatal("a local disable left the old route published", err)
	}
}

func TestManagedSubscriptionLabelsPublishAndRefreshWithoutChangingRoutes(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	server, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	transports := []string{
		model.TransportProfileRealityTCPVision, model.TransportProfileTLSSelfSNIWeb, model.TransportProfilePlainXHTTP,
	}
	server.EnabledProfiles = model.EnabledProfilesCSV(server.PrimaryProfile, strings.Join(transports, ","))
	if err := app.store.UpdateServer(app.ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SetSubscriptionNode(app.ctx, server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveAppliedSubscriptionNode(app.ctx, *server); err != nil {
		t.Fatal(err)
	}
	user, err := app.store.GetUser(app.ctx, server.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		return json.Marshal(model.UserStatusResponse{
			Users: []model.UserAccessStatus{{
				Username: user.Username, UUID: user.UUID, EffectiveEnabled: true, HasRuntimeIdentity: true,
			}},
		})
	}
	if err := os.MkdirAll(app.managedBundleDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := subscription.Open(app.managedBundleDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	labelsPath := filepath.Join(app.dataDir, "delivery", subscriptionLabelsFile)
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(labelsPath, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fragments := func(entries []subscription.Entry) []string {
		t.Helper()
		var names []string
		for _, entry := range entries {
			u, err := url.Parse(entry.Link)
			if err != nil {
				t.Fatal("invalid published URL")
			}
			names = append(names, u.Fragment)
		}
		return names
	}
	owner := subscription.Bundle{Owner: user.Username, ManagedKey: subscriptionIdentity(user.UUID)}
	nodes, err := app.loadSubscriptionNodes()
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := app.readSubscriptionLabels()
	if err != nil || fallback != nil {
		t.Fatal("missing file should retain default labels", err)
	}
	defaults, err := app.managedEntries(owner, nodes, fallback)
	if err != nil {
		t.Fatal(err)
	}
	defaultNames := fragments(defaults)
	if len(defaultNames) != 3 {
		t.Fatalf("expected three fixture routes, got %d", len(defaultNames))
	}

	write(`{"main":{"name":"VPN","profiles":{
		"vless-xhttp-plain":"Резервный 2","vless-tcp-tls-selfsni-web":"Резервный","vless-reality-tcp-vision":"Основной"
	}}}`)
	labels, err := app.readSubscriptionLabels()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := app.managedEntries(owner, nodes, labels)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := store.PublishManaged("", owner.Owner, owner.ManagedKey, entries, false, false)
	if err != nil {
		t.Fatal(err)
	}
	read := func() subscription.Bundle {
		t.Helper()
		bundle, err := store.Read(id)
		if err != nil {
			t.Fatal(err)
		}
		return bundle
	}
	first := read()
	byDefault := map[string]string{
		"main · " + model.TransportProfileRealityTCPVision: "VPN · Основной",
		"main · " + model.TransportProfileTLSSelfSNIWeb:    "VPN · Резервный",
		"main · " + model.TransportProfilePlainXHTTP:       "VPN · Резервный 2",
	}
	assertPublication := func(bundle subscription.Bundle, names []string) {
		t.Helper()
		if !reflect.DeepEqual(fragments(bundle.Entries), names) || bundle.TokenHash != first.TokenHash {
			t.Fatalf("unexpected labels or changed delivery token")
		}
		for i, entry := range bundle.Entries {
			actual, _, _ := strings.Cut(entry.Link, "#")
			expected, _, _ := strings.Cut(defaults[i].Link, "#")
			if actual != expected || entry.NodeID != defaults[i].NodeID {
				t.Fatalf("renaming changed connection route at position %d", i)
			}
		}
	}
	wanted := []string{byDefault[defaultNames[0]], byDefault[defaultNames[1]], byDefault[defaultNames[2]]}
	assertPublication(first, wanted)
	if err := app.syncManagedSubscriptions(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, read()) {
		t.Fatal("unchanged sync rewrote the bundle")
	}

	write(`{"main":{"name":"VPN","profiles":{"vless-tcp-tls-selfsni-web":"Второй"}}}`)
	if err := app.syncManagedSubscriptions(); err != nil {
		t.Fatal(err)
	}
	wanted = nil
	for _, name := range defaultNames {
		if strings.HasSuffix(name, model.TransportProfileTLSSelfSNIWeb) {
			wanted = append(wanted, "VPN · Второй")
		} else {
			wanted = append(wanted, strings.Replace(name, "main · ", "VPN · ", 1))
		}
	}
	last := read()
	assertPublication(last, wanted)
	for _, invalid := range []string{
		`{"main":{"profiles":{"route-2":"Резервный"}}}`,
		`{"main":{"name":"two\nlines"}}`,
		`{"main":{"other":"VPN"}}`,
		`{}` + `{}`,
	} {
		write(invalid)
		if err := app.syncManagedSubscriptions(); !errors.Is(err, errSubscriptionLabels) {
			t.Fatalf("invalid labels did not fail sync: %v", err)
		}
		if !reflect.DeepEqual(last, read()) {
			t.Fatal("invalid labels changed the published bundle")
		}
	}
	if err := app.issueManagedSubscription(user.Username, user.UUID, "", "code", false); !errors.Is(err, errSubscriptionLabels) {
		t.Fatalf("invalid labels were not rejected before attempting issuance: %v", err)
	}
	if err := os.Remove(labelsPath); err != nil {
		t.Fatal(err)
	}
	if err := app.syncManagedSubscriptions(); err != nil {
		t.Fatal(err)
	}
	assertPublication(read(), defaultNames)
}

func TestSubscriptionPlanQueuesUserProvisioningBeforeDelivery(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	second := addSubscriptionServer(t, app, "second", true)
	control := deskControl{dataDir: filepath.Join(app.dataDir, "data")}
	request := operations.Request{Action: createSubscription,
		Values: map[string]string{"username": "pilot", "base-url": "https://subscriptions.example.net"}}
	if _, err := control.Prepare(context.Background(), request); err == nil || err.Error() != "SUBSCRIPTION_NO_READY_PROFILES" {
		t.Fatalf("plan without a published node = %v", err)
	}
	if err := app.store.SetSubscriptionNode(app.ctx, second.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Prepare(context.Background(), request); err == nil || err.Error() != "SUBSCRIPTION_NO_READY_PROFILES" {
		t.Fatalf("plan before deploy = %v", err)
	}
	if err := app.store.SaveAppliedSubscriptionNode(app.ctx, second); err != nil {
		t.Fatal(err)
	}
	plan, err := control.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 3 || plan.Targets[2].ID != "workspace" || plan.Request.Values["uuid"] == "" {
		t.Fatal("publication must follow the reviewed node operations")
	}
}

func TestCreateSubscriptionUnavailableDeliveryNeverProvisionsUser(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	server := addSubscriptionServer(t, app, "public", true)
	if err := app.store.SetSubscriptionNode(app.ctx, server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveAppliedSubscriptionNode(app.ctx, server); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	dir := filepath.Join(app.dataDir, "data")
	control := &deskControl{dataDir: dir, template: app}
	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		t.Error("contacted the fleet before confirming the HTTPS activation service")
		return nil, nil
	}
	coordinator := operations.New(ctx, dir, control)
	defer coordinator.Close()
	request := operations.Request{Action: createSubscription,
		Values: map[string]string{"username": "pilot", "base-url": "https://127.0.0.1:0"}}
	plan, err := coordinator.Plan(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	job, err := coordinator.Start(ctx, operations.Start{PlanID: plan.ID, Digest: plan.Digest,
		Key: strings.Repeat("a", 32), Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	for {
		job, err = coordinator.Job(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.FinishedAt != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("operation did not finish")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if job.Code != "DELIVERY_UNAVAILABLE" {
		t.Fatalf("operation finished with %s", job.Code)
	}
	for _, step := range job.Steps {
		if step.State != "queued" {
			t.Fatalf("operation touched target, status %s", step.State)
		}
	}
	if _, err := app.store.GetUser(ctx, server.ID, "pilot"); !isNotFoundErr(err) {
		t.Fatal("operation created a user before delivery validation")
	}
}

func TestCreateSubscriptionResumeAcceptsItsOwnPartialAssignment(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	server := addSubscriptionServer(t, app, "public", true)
	if err := app.store.SetSubscriptionNode(app.ctx, server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.store.SaveAppliedSubscriptionNode(app.ctx, server); err != nil {
		t.Fatal(err)
	}
	control := &deskControl{dataDir: filepath.Join(app.dataDir, "data")}
	request := operations.Request{Action: createSubscription,
		Values: map[string]string{"username": "pilot", "base-url": "https://delivery.example.test"}}
	plan, err := control.Prepare(app.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ServerID: server.ID, Username: "pilot", UUID: plan.Request.Values["uuid"],
		Email: "pilot@ovpn", Enabled: true}
	if err := app.store.AddUser(app.ctx, &user); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Prepare(app.ctx, request); err == nil || err.Error() != "USER_ASSIGNMENT_CONFLICT" {
		t.Fatalf("second create was not refused: %v", err)
	}
	if err := control.CanResume(app.ctx, plan); err != nil {
		t.Fatal("original operation cannot resume", err)
	}
	users, err := app.store.ListUsers(app.ctx, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	var matched int
	for _, user := range users {
		if user.Username == "pilot" && user.UUID == plan.Request.Values["uuid"] {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("expected exactly one checkpointed assignment, found %d", matched)
	}
}
