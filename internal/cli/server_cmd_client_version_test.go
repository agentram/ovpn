package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/deploy"
	"ovpn/internal/doctor"
	"ovpn/internal/model"
	"ovpn/internal/store/local"
	"ovpn/internal/xraycfg"
)

func TestRealityClientVersionPersistsAcrossReopenDeployAndUserChanges(t *testing.T) {
	app := newTestAppWithServer(t, true)
	setRuntimeBinaryOverrides(t)
	app.remoteHTTPHook = successfulAgentHTTPHook(t)
	cmd := app.serverCmd()
	cmd.SetArgs([]string{"set-reality-min-client-version", "main", "1.0.0"})
	stdout, _, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "ovpn deploy main") || !strings.Contains(stdout, "outdated TLS fingerprints") {
		t.Fatalf("missing apply instructions or tradeoff: %s", stdout)
	}
	if err := app.store.Close(); err != nil {
		t.Fatal(err)
	}
	app.store, err = local.Open(app.ctx, filepath.Join(app.dataDir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.store.Close() })
	runUserCommand(t, app, "add", "--username", "alice", "--uuid", "11111111-1111-1111-1111-111111111111")
	runUserCommand(t, app, "disable", "--username", "alice")
	runUserCommand(t, app, "enable", "--username", "alice")
	runUserCommand(t, app, "quota-reset", "--username", "alice")
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	users, err := app.store.ListUsers(app.ctx, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := app.buildXraySpec(*srv, users)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := xraycfg.RenderServerJSON(spec)
	if err != nil {
		t.Fatal(err)
	}
	assertRenderedClientVersion(t, raw, "1.0.0")
	bundle, err := deploy.RenderBundle(deploy.Input{Server: *srv, Users: users})
	if err != nil {
		t.Fatal(err)
	}
	defer deploy.CleanupBundle(bundle)
	assertRenderedClientVersion(t, bundle.ConfigRaw, "1.0.0")
	if check := checkLocalConfig(*srv); check.Status != doctor.StatusWarn || !strings.Contains(strings.Join(check.Details, " "), "reality_min_client_version=1.0.0") {
		t.Fatal("doctor did not explain the custom compatibility policy")
	}
	if _, _, err := captureStdoutStderr(t, func() error { return app.initOrDeployServer(*srv, true) }); err != nil {
		t.Fatalf("ordinary init dry-run: %v", err)
	}
	clear := app.serverCmd()
	clear.SetArgs([]string{"set-reality-min-client-version", "main", "default"})
	if _, _, err := captureStdoutStderr(t, clear.Execute); err != nil {
		t.Fatal(err)
	}
	srv, err = app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	spec, err = app.buildXraySpec(*srv, users)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = xraycfg.RenderServerJSON(spec)
	if err != nil {
		t.Fatal(err)
	}
	assertRenderedClientVersion(t, raw, "")
}

func TestNewServerInheritsClientPolicyAndExistingUsers(t *testing.T) {
	for _, override := range []string{"inherit", "default", "1.8.1"} {
		t.Run(override, func(t *testing.T) {
			app := newTestAppWithLinkedUser(t)
			base, err := app.store.GetServerByName(app.ctx, "main")
			if err != nil {
				t.Fatal(err)
			}
			base.RealityMinClientVersion = "1.0.0"
			if err := app.store.UpdateServer(app.ctx, base); err != nil {
				t.Fatal(err)
			}
			original, err := app.store.GetUser(app.ctx, base.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"add", "--name", "second", "--host", "192.0.2.2", "--domain", "second.example.net"}
			want := "1.0.0"
			if override != "inherit" {
				args = append(args, "--reality-min-client-version", override)
				want = override
				if want == "default" {
					want = ""
				}
			}
			cmd := app.serverCmd()
			cmd.SetArgs(args)
			if _, _, err := captureStdoutStderr(t, cmd.Execute); err != nil {
				t.Fatal(err)
			}
			second, err := app.store.GetServerByName(app.ctx, "second")
			if err != nil {
				t.Fatal(err)
			}
			if second.RealityMinClientVersion != want {
				t.Fatalf("new server policy=%q, want %q", second.RealityMinClientVersion, want)
			}
			user, err := app.store.GetUser(app.ctx, second.ID, "alice")
			if err != nil {
				t.Fatal(err)
			}
			if user.UUID != original.UUID || user.Email != original.Email || user.Enabled != original.Enabled {
				t.Fatal("existing user identity changed on server add")
			}
			if err := app.ensureRealityParity(); err != nil {
				t.Fatalf("per-server client policies must not require equal values: %v", err)
			}
		})
	}
}

func TestInvalidClientVersionDoesNotMutateLocalState(t *testing.T) {
	app := newTestAppWithServer(t, false)
	for _, args := range [][]string{
		{"set-reality-min-client-version", "main", "256.0.0"},
		{"set-reality-min-client-version", "main", "1.0"},
		{"add", "--name", "invalid", "--host", "192.0.2.3", "--domain", "invalid.example.net", "--reality-min-client-version", "1.0.0.0"},
	} {
		cmd := app.serverCmd()
		cmd.SetArgs(args)
		if _, _, err := captureStdoutStderr(t, cmd.Execute); err == nil {
			t.Fatalf("invalid version accepted: %v", args)
		}
	}
	servers, err := app.store.ListServers(app.ctx)
	if err != nil || len(servers) != 1 || servers[0].RealityMinClientVersion != "" {
		t.Fatal("invalid command changed server state")
	}
}

func TestFreshServerCanSelectCompatibilityBeforeInit(t *testing.T) {
	app := newTestAppWithoutServers(t, true)
	cmd := app.serverCmd()
	cmd.SetArgs([]string{"add", "--name", "fresh", "--host", "192.0.2.4", "--domain", "fresh.example.net", "--reality-min-client-version", "1.0.0"})
	if _, _, err := captureStdoutStderr(t, cmd.Execute); err != nil {
		t.Fatal(err)
	}
	srv, err := app.store.GetServerByName(app.ctx, "fresh")
	if err != nil || srv.RealityMinClientVersion != "1.0.0" {
		t.Fatalf("fresh server compatibility setting: %v", err)
	}
	if err := model.ValidateRealityMinClientVersion(srv.RealityMinClientVersion); err != nil {
		t.Fatal(err)
	}
}

func assertRenderedClientVersion(t *testing.T, raw []byte, want string) {
	t.Helper()
	var cfg struct{ Inbounds []map[string]any }
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, inbound := range cfg.Inbounds {
		if inbound["tag"] == "vless-reality" {
			r := inbound["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
			got, present := r["minClientVer"]
			if present != (want != "") || (present && got != want) {
				t.Fatalf("rendered minClientVer=%v, present=%v, want %q", got, present, want)
			}
			return
		}
	}
	t.Fatal("REALITY inbound missing")
}
