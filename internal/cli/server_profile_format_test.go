package cli

import (
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestRenderServerProfileTableUsesReadableColumns(t *testing.T) {
	srv := model.Server{
		Name:           "vpn-a",
		PrimaryProfile: model.TransportProfilePlainXHTTP,
		// Simulate old local state. Removed profiles must be normalized away before display.
		EnabledProfiles: model.TransportProfilePlainXHTTP + ",vless-reality-xhttp",
	}
	enabled := map[string]bool{}
	for _, profile := range srv.NormalizedEnabledProfiles() {
		enabled[profile] = true
	}

	out := renderServerProfileTable(srv, enabled)

	for _, want := range []string{"PROFILE", "STATUS", "PORT", "ENABLED", "PRIMARY", "DESCRIPTION", model.TransportProfilePlainXHTTP, "fallback", "13179"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in profile table:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "│ yes     │ yes") {
		t.Fatalf("expected primary enabled profile to be rendered with yes markers:\n%s", out)
	}
	if strings.Contains(out, "\t") {
		t.Fatalf("profile table should not use raw tabs:\n%s", out)
	}
}
