package cli

import (
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestServerProfileDisableRemovesNonPrimaryProfile(t *testing.T) {
	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.PrimaryProfile = model.TransportProfilePlainXHTTP
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.PrimaryProfile, strings.Join([]string{
		model.TransportProfileRealityTCPVision,
		model.TransportProfileRealityXHTTP,
		model.TransportProfilePlainXHTTP,
	}, ","))
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}

	cmd := app.newServerProfileDisableCmd()
	cmd.SetArgs([]string{"main", model.TransportProfileRealityXHTTP})
	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("profile disable: %v", err)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	for _, want := range []string{
		"disabled profile " + model.TransportProfileRealityXHTTP + " on main",
		"redeploy the server",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected stdout to contain %q, got %q", want, stdout)
		}
	}
	updated, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("reload server: %v", err)
	}
	if updated.IsTransportProfileEnabled(model.TransportProfileRealityXHTTP) {
		t.Fatalf("expected %s to be disabled, enabled=%v", model.TransportProfileRealityXHTTP, updated.NormalizedEnabledProfiles())
	}
	if !updated.IsTransportProfileEnabled(model.TransportProfilePlainXHTTP) {
		t.Fatalf("expected primary profile to remain enabled, enabled=%v", updated.NormalizedEnabledProfiles())
	}
}

func TestServerProfileDisableRejectsPrimaryProfile(t *testing.T) {
	app := newTestAppWithServer(t, false)
	cmd := app.newServerProfileDisableCmd()
	cmd.SetArgs([]string{"main", model.TransportProfileRealityTCPVision})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err == nil {
		t.Fatalf("expected primary-profile disable error")
	}
	for _, want := range []string{
		"cannot disable primary profile " + model.TransportProfileRealityTCPVision + " on main",
		"ovpn server profile switch main <other-profile>",
		"then redeploy",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %v", want, err)
		}
	}
	if stdout != "" {
		t.Fatalf("primary profile error should not print stdout, stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestServerProfileDisableAlreadyDisabledIsNoop(t *testing.T) {
	app := newTestAppWithServer(t, false)
	cmd := app.newServerProfileDisableCmd()
	cmd.SetArgs([]string{"main", model.TransportProfilePlainXHTTP})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("profile disable already disabled: %v", err)
	}
	if !strings.Contains(stdout, "profile "+model.TransportProfilePlainXHTTP+" is already disabled on main") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestServerProfileSwitchAllowsFallbackProfile(t *testing.T) {
	app := newTestAppWithServer(t, false)
	cmd := app.newServerProfileSwitchCmd()
	cmd.SetArgs([]string{"main", model.TransportProfilePlainXHTTP})

	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("profile switch fallback: %v", err)
	}
	if !strings.Contains(stdout, "primary profile for main: "+model.TransportProfilePlainXHTTP) {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	updated, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("reload server: %v", err)
	}
	if updated.NormalizedPrimaryProfile() != model.TransportProfilePlainXHTTP {
		t.Fatalf("expected plain XHTTP primary, got %s", updated.NormalizedPrimaryProfile())
	}
	if !updated.IsTransportProfileEnabled(model.TransportProfilePlainXHTTP) {
		t.Fatalf("expected primary fallback profile to be enabled, enabled=%v", updated.NormalizedEnabledProfiles())
	}
}
