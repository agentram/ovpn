package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

func TestRenderProxyBundleSelfSNIKeepsHAProxyAndGeodata(t *testing.T) {
	t.Parallel()
	for _, profile := range []string{model.TransportProfileTLSSelfSNIWeb, model.TransportProfileRealityTCPVision} {
		t.Run(profile, func(t *testing.T) {
			geodata := filepath.Join(t.TempDir(), "geodata.dat")
			if err := os.WriteFile(geodata, []byte("synthetic geodata"), 0o600); err != nil {
				t.Fatal(err)
			}
			bundle, err := RenderBundle(Input{
				Server: model.Server{
					Name: "proxy-a", Role: model.ServerRoleProxy, Domain: "proxy.example.net",
					PrimaryProfile: profile, EnabledProfiles: profile + "," + model.TransportProfilePlainXHTTP,
					RealityPrivateKey: "private-key", RealityServerName: "target.example.net",
					RealityTarget: "target.example.net:443", RealityShortIDs: "abcd1234",
				},
				BackendServers: []model.Server{{Host: "192.0.2.10"}},
				ProxyRelay: &xraycfg.ProxyRelay{
					Address: "haproxy", Port: 15443, ServiceUUID: "22222222-2222-2222-2222-222222222222",
					ServerName: "backend.example.net", PublicKey: "backend-public-key", ShortID: "abcd1234",
				},
				ProxyGeoSitePath: geodata, ProxyGeoIPPath: geodata,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer CleanupBundle(bundle)
			raw, err := os.ReadFile(filepath.Join(bundle.Dir, "docker-compose.yml"))
			if err != nil {
				t.Fatal(err)
			}
			compose := string(raw)
			for _, want := range []string{"      - haproxy\n", "  haproxy:\n", "./geodata/geosite.dat:", "./geodata/geoip.dat:", `"13179:13179/tcp"`} {
				if !strings.Contains(compose, want) {
					t.Fatalf("proxy lost required runtime entry %q", want)
				}
			}
			selfSNI := profile == model.TransportProfileTLSSelfSNIWeb
			for _, entry := range []string{"  ovpn-web:\n", "      - ovpn-web\n", ":/etc/xray/certs:ro", ":/usr/share/nginx/html:ro", "./web/nginx.conf:"} {
				if strings.Contains(compose, entry) != selfSNI {
					t.Fatalf("unexpected self-SNI dependency %q for %s", entry, profile)
				}
			}
			xraySection := strings.Split(compose, "\n  # OVPN_CAMOUFLAGE_WEB_SERVICE")[0]
			if strings.Count(xraySection, "    depends_on:\n") != 1 {
				t.Fatal("Xray must have one dependencies block")
			}
			if _, err := os.Stat(filepath.Join(bundle.Dir, "web", "nginx.conf")); (err == nil) != selfSNI {
				t.Fatalf("unexpected website config for %s: %v", profile, err)
			}
			haproxy, err := os.ReadFile(filepath.Join(bundle.Dir, "haproxy", "haproxy.cfg"))
			if err != nil || !strings.Contains(string(haproxy), "192.0.2.10:443 check") {
				t.Fatalf("proxy lost its backend health check: %v", err)
			}
		})
	}
}
