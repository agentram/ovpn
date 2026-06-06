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
			Name:        "germany-1",
			Role:        model.ServerRoleVPN,
			Host:        "92.51.37.146",
			Domain:      "germany-1.masterandmargarita.webtm.ru",
			XrayVersion: "26.3.27",
			Enabled:     true,
		},
		{
			ID:          5,
			Name:        "italy-1",
			Role:        model.ServerRoleVPN,
			Host:        "188.213.171.179",
			Domain:      "olproject.com.ru",
			XrayVersion: "26.3.27",
			Enabled:     true,
		},
	})

	for _, want := range []string{"ID", "NAME", "ROLE", "HOST", "DOMAIN", "XRAY", "STATE", "italy-1", "olproject.com.ru", "enabled"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in server table:\n%s", want, out)
		}
	}
}
