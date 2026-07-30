package cli

import (
	"strings"
	"testing"
)

func TestNormalizeRealityTargetDefaultsToTLSPort(t *testing.T) {
	target, host, err := normalizeRealityTarget("www.trip.com")
	if err != nil {
		t.Fatalf("normalize target: %v", err)
	}
	if target != "www.trip.com:443" || host != "www.trip.com" {
		t.Fatalf("unexpected target=%q host=%q", target, host)
	}
}

func TestNormalizeRealityTargetRejectsURLAndInvalidPort(t *testing.T) {
	for _, input := range []string{"https://www.trip.com", "www.trip.com:bad", "www.trip.com:70000", "www.trip.com/path"} {
		t.Run(input, func(t *testing.T) {
			if _, _, err := normalizeRealityTarget(input); err == nil {
				t.Fatalf("expected %q to be rejected", input)
			}
		})
	}
}

func TestSetRealityTargetUpdatesLocalStateAndExplainsRedeploy(t *testing.T) {
	app := newTestAppWithServer(t, false)
	cmd := app.newServerSetRealityTargetCmd()
	cmd.SetArgs([]string{"main", "www.trip.com"})
	stdout, stderr, err := captureStdoutStderr(t, cmd.Execute)
	if err != nil {
		t.Fatalf("set REALITY target: %v", err)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	for _, want := range []string{
		"REALITY target: www.microsoft.com:443 -> www.trip.com:443",
		"REALITY serverName: www.microsoft.com -> www.trip.com",
		"redeploy the server",
		"existing links use the previous target/SNI",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected output to contain %q, got %q", want, stdout)
		}
	}
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("reload server: %v", err)
	}
	if srv.RealityTarget != "www.trip.com:443" || srv.RealityServerName != "www.trip.com" {
		t.Fatalf("unexpected persisted REALITY settings: target=%q sni=%q", srv.RealityTarget, srv.RealityServerName)
	}
}

func TestSetRealityTargetRejectsIPServerName(t *testing.T) {
	app := newTestAppWithServer(t, false)
	cmd := app.newServerSetRealityTargetCmd()
	cmd.SetArgs([]string{"main", "www.trip.com", "--server-name", "203.0.113.10"})
	if _, _, err := captureStdoutStderr(t, cmd.Execute); err == nil || !strings.Contains(err.Error(), "not an IP address") {
		t.Fatalf("expected clear SNI validation error, got %v", err)
	}
}
