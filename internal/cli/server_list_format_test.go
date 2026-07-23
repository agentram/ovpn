package cli

import (
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestRenderServerListTableIncludesHeaders(t *testing.T) {
	out := renderServerListTable([]model.Server{
		{
			ID:          2,
			Name:        "vpn-a",
			Role:        model.ServerRoleVPN,
			Host:        "203.0.113.10",
			Domain:      "vpn-a.example.net",
			XrayVersion: "26.3.27",
			Enabled:     true,
		},
		{
			ID:          5,
			Name:        "vpn-b",
			Role:        model.ServerRoleVPN,
			Host:        "203.0.113.11",
			Domain:      "vpn-b.example.net",
			XrayVersion: "26.3.27",
			Enabled:     true,
		},
	})

	for _, want := range []string{"ID", "NAME", "ROLE", "HOST", "DOMAIN", "XRAY", "STATE", "vpn-b", "vpn-b.example.net", "enabled"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in server table:\n%s", want, out)
		}
	}
}
