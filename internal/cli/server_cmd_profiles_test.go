package cli

import (
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestServerProfileListRendersTable(t *testing.T) {
	app := newTestAppWithServer(t, false)
	cmd := app.newServerProfileListCmd()
	cmd.SetArgs([]string{"main"})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("profile list: %v", err)
	}
	for _, want := range []string{"PROFILE", model.TransportProfileRealityTCPVision, model.TransportProfileXHTTPPlain, "ENABLED", "PRIMARY"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("profile list missing %q:\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestServerProfileEnableAndDisable(t *testing.T) {
	app := newTestAppWithServer(t, false)

	enable := app.newServerProfileEnableCmd()
	enable.SetArgs([]string{"main", model.TransportProfileXHTTPPlain, "--primary"})
	stdout, _, err := captureStdoutStderr(t, enable.Execute)
	if err != nil {
		t.Fatalf("profile enable: %v", err)
	}
	if !strings.Contains(stdout, "next: ovpn deploy main") {
		t.Fatalf("expected deploy hint, got:\n%s", stdout)
	}
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if !model.TransportProfileEnabled(srv.EnabledProfiles, model.TransportProfileXHTTPPlain) {
		t.Fatalf("xhttp profile not enabled: %s", srv.EnabledProfiles)
	}
	if srv.PrimaryProfile != model.TransportProfileXHTTPPlain {
		t.Fatalf("primary = %q, want xhttp", srv.PrimaryProfile)
	}

	disable := app.newServerProfileDisableCmd()
	disable.SetArgs([]string{"main", model.TransportProfileXHTTPPlain})
	stdout, _, err = captureStdoutStderr(t, disable.Execute)
	if err != nil {
		t.Fatalf("profile disable: %v", err)
	}
	if !strings.Contains(stdout, "primary profile: "+model.TransportProfileRealityTCPVision) {
		t.Fatalf("expected primary fallback, got:\n%s", stdout)
	}
	srv, err = app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server after disable: %v", err)
	}
	if model.TransportProfileEnabled(srv.EnabledProfiles, model.TransportProfileXHTTPPlain) {
		t.Fatalf("xhttp profile still enabled: %s", srv.EnabledProfiles)
	}
}

func TestServerProfileEnableRejectsUnrenderedProfile(t *testing.T) {
	app := newTestAppWithServer(t, false)
	cmd := app.newServerProfileEnableCmd()
	cmd.SetArgs([]string{"main", model.TransportProfileGRPCReality})

	_, _, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil || !strings.Contains(err.Error(), "not supported by deploy") {
		t.Fatalf("expected render support error, got %v", err)
	}
}
