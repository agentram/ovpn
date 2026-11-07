package cli

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/subscription"
)

func addSubscriptionServer(t *testing.T, app *App, name string, enabled bool) model.Server {
	t.Helper()
	base, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	srv := *base
	srv.ID, srv.Name, srv.Domain, srv.Enabled = 0, name, name+".example.net", enabled
	if err := app.store.AddServer(app.ctx, &srv); err != nil {
		t.Fatal(err)
	}
	user, err := app.store.GetUser(app.ctx, base.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	copy := *user
	copy.ID, copy.ServerID = 0, srv.ID
	if err := app.store.AddUser(app.ctx, &copy); err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestSubscriptionExportAggregatesEligibleNodesAndProfiles(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	main, _ := app.store.GetServerByName(app.ctx, "main")
	main.EnabledProfiles = model.EnabledProfilesCSV(model.TransportProfileRealityTCPVision, model.TransportProfilePlainXHTTP)
	main.RealityPrivateKey = "never-export-server-private-key"
	main.VLESSServerDecryption = ""
	if err := app.store.UpdateServer(app.ctx, main); err != nil {
		t.Fatal(err)
	}
	second := addSubscriptionServer(t, app, "second", true)
	second.PrimaryProfile = model.TransportProfileTLSSelfSNIWeb
	second.EnabledProfiles = model.TransportProfileTLSSelfSNIWeb
	if err := app.store.UpdateServer(app.ctx, &second); err != nil {
		t.Fatal(err)
	}
	addSubscriptionServer(t, app, "disabled", false)
	expired := addSubscriptionServer(t, app, "expired", true)
	user, _ := app.store.GetUser(app.ctx, expired.ID, "alice")
	cutoff := time.Now().Add(-time.Hour)
	user.ExpiryDate = &cutoff
	if err := app.store.UpdateUser(app.ctx, user); err != nil {
		t.Fatal(err)
	}
	blocked := addSubscriptionServer(t, app, "blocked", true)
	user, _ = app.store.GetUser(app.ctx, blocked.ID, "alice")
	user.QuotaBlocked = true
	if err := app.store.UpdateUser(app.ctx, user); err != nil {
		t.Fatal(err)
	}
	missing := addSubscriptionServer(t, app, "missing", true)
	if err := app.store.DeleteUser(app.ctx, missing.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	before, _ := app.store.ListUsers(app.ctx, main.ID)
	file := filepath.Join(t.TempDir(), "subscription.txt")
	cmd := app.newUserSubscriptionExportCmd()
	cmd.SetArgs([]string{"--username", "alice", "--all-profiles", "--out", file})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	links := strings.Fields(string(data))
	if len(links) != 3 || strings.Contains(string(data), "never-export") || strings.Contains(stdout.String(), "vless://") || strings.Contains(stdout.String(), "11111111") {
		t.Fatal("unexpected subscription count, server material or credentials in stdout")
	}
	labels := map[string]bool{}
	for _, link := range links {
		parsed, err := url.Parse(link)
		if err != nil || labels[parsed.Fragment] {
			t.Fatal("invalid or duplicate node label")
		}
		labels[parsed.Fragment] = true
		if parsed.Hostname() != main.Domain && parsed.Hostname() != second.Domain {
			t.Fatal("an ineligible node was included")
		}
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("subscription export is not private")
	}
	after, _ := app.store.ListUsers(app.ctx, main.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("subscription export changed user state")
	}
	entries, err := app.subscriptionEntries(subscriptionSelection{username: "alice"})
	if err != nil || len(entries) != 2 {
		t.Fatal("default selection must contain only one primary profile per eligible server")
	}
}

func TestSubscriptionPublishRefreshDisableAndDeliveryOnlyRevocation(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	dir := t.TempDir()
	publish := func(id string, rotate bool) (string, error) {
		cmd := app.newUserSubscriptionPublishCmd()
		args := []string{"--username", "alice", "--bundle-dir", dir, "--base-url", "https://subscriptions.example.net"}
		if id != "" {
			args = append(args, "--id", id)
		}
		if rotate {
			args = append(args, "--rotate-token")
		}
		cmd.SetArgs(args)
		var output bytes.Buffer
		cmd.SetOut(&output)
		err := cmd.Execute()
		return output.String(), err
	}
	output, err := publish("", false)
	if err != nil {
		t.Fatal(err)
	}
	var link string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "https://") {
			link = line
		}
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed.Host != "subscriptions.example.net" {
		t.Fatal("publish did not return the subscription URL")
	}
	id := strings.Split(parsed.Path, "/")[2]
	handler, err := subscription.NewHandler(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	fetch := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, parsed.String(), nil))
		return w
	}
	if fetch().Code != http.StatusOK {
		t.Fatal("new subscription was not available")
	}
	srv, _ := app.store.GetServerByName(app.ctx, "main")
	srv.Domain = "replacement.example.net"
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatal(err)
	}
	output, err = publish(id, false)
	if err != nil || strings.Contains(output, "https://") {
		t.Fatal("refresh rotated or echoed the delivery token")
	}
	data, _ := base64.StdEncoding.DecodeString(fetch().Body.String())
	if !strings.Contains(string(data), "replacement.example.net") {
		t.Fatal("refreshed URL did not receive the new endpoint")
	}
	user, _ := app.store.GetUser(app.ctx, srv.ID, "alice")
	user.Enabled = false
	if err := app.store.UpdateUser(app.ctx, user); err != nil {
		t.Fatal(err)
	}
	if _, err := publish(id, false); err != nil {
		t.Fatal(err)
	}
	if fetch().Code != http.StatusNotFound {
		t.Fatal("refresh retained a disabled user's credentials")
	}
	user.Enabled = true
	if err := app.store.UpdateUser(app.ctx, user); err != nil {
		t.Fatal(err)
	}
	if _, err := publish(id, false); err != nil || fetch().Code != http.StatusOK {
		t.Fatal("re-enabled user lost the stable subscription URL")
	}
	before, _ := app.store.GetUser(app.ctx, srv.ID, "alice")
	root := NewRootCmd()
	unusedState := filepath.Join(t.TempDir(), "must-not-be-created")
	root.SetArgs([]string{"--data-dir", unusedState, "subscription", "revoke", "--bundle-dir", dir, "--id", id})
	root.SetOut(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unusedState); !os.IsNotExist(err) {
		t.Fatal("delivery-only revocation opened local control state")
	}
	if fetch().Code != http.StatusNotFound {
		t.Fatal("revoked URL still delivers credentials")
	}
	after, _ := app.store.GetUser(app.ctx, srv.ID, "alice")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("delivery revocation altered the VPN user")
	}
}

func TestSubscriptionRejectsIdentityConflictsAndUnsafeOrigins(t *testing.T) {
	app := newTestAppWithLinkedUser(t)
	second := addSubscriptionServer(t, app, "second", true)
	user, _ := app.store.GetUser(app.ctx, second.ID, "alice")
	user.UUID = "22222222-2222-2222-2222-222222222222"
	if err := app.store.UpdateUser(app.ctx, user); err != nil {
		t.Fatal(err)
	}
	if _, err := app.subscriptionEntries(subscriptionSelection{username: "alice"}); err == nil || strings.Contains(err.Error(), user.UUID) {
		t.Fatal("conflicting user identities were aggregated or disclosed")
	}
	if entries, err := app.subscriptionEntries(subscriptionSelection{username: "alice", server: "main"}); err != nil || len(entries) != 1 {
		t.Fatal("server-scoped selection was rejected")
	}
	dir := t.TempDir()
	cmd := app.newUserSubscriptionPublishCmd()
	cmd.SetArgs([]string{"--username", "alice", "--server", "main", "--bundle-dir", dir, "--base-url", "http://subscriptions.example.net"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("unsafe public origin was accepted")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("failed validation wrote a delivery bundle")
	}
}

func TestSubscriptionServeSkipsControlStateAndRequiresTLS(t *testing.T) {
	root := NewRootCmd()
	invalidState := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidState, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	root.SetArgs([]string{"--data-dir", invalidState, "subscription", "serve", "--bundle-dir", t.TempDir(), "--listen", "0.0.0.0:8088"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "TLS is required") {
		t.Fatal("serve accessed control state or accepted public plaintext")
	}
	raw, _ := os.ReadFile(invalidState)
	if string(raw) != "preserve" {
		t.Fatal("delivery touched control state")
	}
}
