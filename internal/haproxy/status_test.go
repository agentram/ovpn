package haproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const healthyMetrics = `# TYPE haproxy_server_status gauge
haproxy_server_status{proxy="foreign_backends",server="ovpn_node_4",state="UP"} 1
haproxy_server_status{proxy="foreign_backends",server="ovpn_node_4",state="DOWN"} 0
haproxy_server_status{proxy="foreign_backends",server="ovpn_node_7",state="UP"} 1
`

func TestHealthDistinguishesPoolFailureAndIgnoresUnrelatedLabels(t *testing.T) {
	for _, tc := range []struct {
		name, body, state, code string
		count                   int
	}{
		{"all up", healthyMetrics, "online", "", 2},
		{"one down", strings.Replace(healthyMetrics, `server="ovpn_node_7",state="UP"`, `server="ovpn_node_7",state="DOWN"`, 1), "degraded", "", 2},
		{"all down", strings.ReplaceAll(strings.ReplaceAll(healthyMetrics, `state="DOWN"} 0`, `state="UP"} 0`), `state="UP"} 1`, `state="DOWN"} 1`), "failed", "", 2},
		{"old labels", "# TYPE haproxy_server_status gauge\nhaproxy_server_status{proxy=\"foreign_backends\",server=\"backend_1\",state=\"UP\"} 1\n", "unknown", "HA_CONFIG_UPDATE_REQUIRED", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, err := Parse(strings.NewReader(tc.body))
			if err != nil || status.State != tc.state || status.Code != tc.code || len(status.Backends) != tc.count {
				t.Fatalf("unexpected health: %+v %v", status, err)
			}
		})
	}
	ambiguous := healthyMetrics + "haproxy_server_status{proxy=\"foreign_backends\",server=\"ovpn_node_4\",state=\"DOWN\"} 1\n"
	if _, err := Parse(strings.NewReader(ambiguous)); err == nil {
		t.Fatal("conflicting evidence accepted")
	}
	if _, err := Parse(strings.NewReader("not prometheus")); err == nil {
		t.Fatal("invalid metrics accepted")
	}
}
func TestReaderRejectsRedirectsOversizedAndUnavailableSources(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; _, _ = w.Write([]byte(healthyMetrics)) }))
	defer target.Close()
	for _, mode := range []string{"ok", "redirect", "oversized", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "ok":
					_, _ = w.Write([]byte(healthyMetrics))
				case "redirect":
					http.Redirect(w, r, target.URL, http.StatusFound)
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
				default:
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer source.Close()
			status := Read(context.Background(), source.URL)
			if mode == "ok" {
				if status.State != "online" {
					t.Fatal(status)
				}
			} else if status.State != "unknown" || len(status.Backends) != 0 {
				t.Fatal("failed collection asserted availability")
			}
		})
	}
	if reached {
		t.Fatal("followed an untrusted redirect")
	}
	if Read(context.Background(), "").Supported {
		t.Fatal("ordinary VPN reported HA support")
	}
}
