package cli

import (
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestServerBackendListAndDetachFlows(t *testing.T) {
	app := newTestAppWithoutServers(t, false)
	ctx := app.ctx
	proxy := backendCommandServer("proxy", model.ServerRoleProxy)
	proxy.ProxyPreset = model.ProxyPresetRU
	backend := backendCommandServer("backend", model.ServerRoleVPN)
	if err := app.store.AddServer(ctx, &proxy); err != nil {
		t.Fatalf("add proxy: %v", err)
	}
	if err := app.store.AddServer(ctx, &backend); err != nil {
		t.Fatalf("add backend: %v", err)
	}

	stdout, _, err := captureStdoutStderr(t, func() error {
		cmd := app.serverCmd()
		cmd.SetArgs([]string{"backend", "list", "--proxy", "proxy"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("empty backend list: %v", err)
	}
	if !strings.Contains(stdout, "no backends") {
		t.Fatalf("unexpected empty backend list output: %s", stdout)
	}

	stdout, _, err = captureStdoutStderr(t, func() error {
		cmd := app.serverCmd()
		cmd.SetArgs([]string{"backend", "attach", "--proxy", "proxy", "--backend", "backend", "--priority", "7"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("backend attach: %v", err)
	}
	if !strings.Contains(stdout, "backend attached: proxy=proxy backend=backend priority=7") {
		t.Fatalf("unexpected attach output: %s", stdout)
	}

	stdout, _, err = captureStdoutStderr(t, func() error {
		cmd := app.serverCmd()
		cmd.SetArgs([]string{"backend", "list", "--proxy", "proxy"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("backend list: %v", err)
	}
	for _, want := range []string{"BACKEND", "PRIORITY", "STATE", "backend", "7", "enabled"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected %q in backend list output:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "\t") {
		t.Fatalf("backend list output should not use raw tabs:\n%s", stdout)
	}

	stdout, _, err = captureStdoutStderr(t, func() error {
		cmd := app.serverCmd()
		cmd.SetArgs([]string{"backend", "detach", "--proxy", "proxy", "--backend", "backend"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("backend detach: %v", err)
	}
	if !strings.Contains(stdout, "backend detached: proxy=proxy backend=backend") {
		t.Fatalf("unexpected detach output: %s", stdout)
	}
}

func TestServerBackendRejectsWrongRoles(t *testing.T) {
	app := newTestAppWithoutServers(t, false)
	ctx := app.ctx
	proxy := backendCommandServer("proxy", model.ServerRoleProxy)
	proxy.ProxyPreset = model.ProxyPresetRU
	vpn := backendCommandServer("vpn", model.ServerRoleVPN)
	if err := app.store.AddServer(ctx, &proxy); err != nil {
		t.Fatalf("add proxy: %v", err)
	}
	if err := app.store.AddServer(ctx, &vpn); err != nil {
		t.Fatalf("add vpn: %v", err)
	}
	cmd := app.serverCmd()
	cmd.SetArgs([]string{"backend", "attach", "--proxy", "vpn", "--backend", "proxy"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "expected proxy") {
		t.Fatalf("expected wrong proxy role error, got %v", err)
	}
	cmd = app.serverCmd()
	cmd.SetArgs([]string{"backend", "attach", "--proxy", "proxy", "--backend", "proxy"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "expected vpn backend") {
		t.Fatalf("expected wrong backend role error, got %v", err)
	}
}

func backendCommandServer(name string, role string) model.Server {
	return model.Server{
		Name:              name,
		Role:              role,
		Host:              name + ".internal",
		Domain:            name + ".example.com",
		SSHUser:           "root",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd1234",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
}
