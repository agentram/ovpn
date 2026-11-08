package integrations

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ovpn/internal/clientcfg"
	"ovpn/internal/observation"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/transport/internet/splithttp"
)

// Opt-in contract smoke: synthetic loopback VPNs carry small verified HTTPS
// requests. Neither a production server nor its identities are used.
func TestInstalledToolsCarryHTTPSWithoutDirectFallback(t *testing.T) {
	dataDir := os.Getenv("OVPN_TEST_TOOL_STATE")
	if dataDir == "" {
		t.Skip("set OVPN_TEST_TOOL_STATE to an isolated workspace with installed tools")
	}
	st, err := observation.Open(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.List(context.Background(), "tools")
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]Tool{}
	for _, raw := range rows {
		var tool Tool
		if json.Unmarshal(raw, &tool) != nil {
			t.Fatal("invalid fixture tool state")
		}
		tools[tool.Engine] = tool
	}
	for _, engine := range []string{"checker", "knife"} {
		if _, err := Verify(context.Background(), tools[engine]); err != nil {
			t.Fatal(engine, err)
		}
	}
	for _, engine := range []string{"native", "checker", "knife-xray"} {
		t.Run(engine, func(t *testing.T) {
			network := "xhttp"
			if engine == "native" {
				network = "tcp"
			}
			link, cert := syntheticVPN(t, network)
			for _, valid := range []bool{true, false} {
				t.Run(fmt.Sprintf("valid=%v", valid), func(t *testing.T) {
					candidate := link
					if !valid {
						candidate = strings.Replace(candidate, "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", 1)
					}
					cfg, err := clientcfg.FromLink(candidate)
					if err != nil {
						t.Fatal(err)
					}
					cfg.Remarks = "opaque-fixture"
					if engine == "native" {
						cfg.Outbounds[0]["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"] = []any{map[string]any{"usage": "verify", "certificate": cert}}
					}
					tool := tools[engine]
					if engine == "knife-xray" {
						tool = tools["knife"]
					}
					result := (Runner{WorkDir: t.TempDir()}).Run(context.Background(), engine, tool, Input{Config: cfg, Link: candidate, Key: "opaque-fixture", Revision: "fixture"})
					if valid && (result.Outcome != "passed" || result.StatusCode != 204) {
						t.Fatalf("valid fixture failed: %+v", result)
					}
					if !valid && result.Outcome != "failed" {
						t.Fatalf("wrong identity was not a measured traffic refusal: %+v", result)
					}
				})
			}
		})
	}
}

func syntheticVPN(t *testing.T, network string) (string, []string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	const id = "11111111-1111-1111-1111-111111111111"
	stream := map[string]any{"network": network, "security": "none", "xhttpSettings": map[string]any{"path": "/observation"}}
	client := fmt.Sprintf("vless://%s@127.0.0.1:%d?type=xhttp&security=none&encryption=none&path=%%2Fobservation&mode=auto#opaque-fixture", id, port)
	var certificate []string
	flow := ""
	if network == "tcp" {
		certServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		defer certServer.Close()
		certificate = strings.Split(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certServer.Certificate().Raw})), "\n")
		key, err := x509.MarshalPKCS8PrivateKey(certServer.TLS.Certificates[0].PrivateKey)
		if err != nil {
			t.Fatal(err)
		}
		keyPEM := strings.Split(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})), "\n")
		stream = map[string]any{"network": "tcp", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificate": certificate, "key": keyPEM}}}}
		flow = "xtls-rprx-vision"
		client = fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=tls&encryption=none&sni=example.com&fp=firefox&flow=xtls-rprx-vision#opaque-fixture", id, port)
	}
	raw, err := json.Marshal(map[string]any{"log": map[string]any{"loglevel": "none", "access": "none", "error": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "vless", "settings": map[string]any{"clients": []any{map[string]any{"id": id, "flow": flow}}, "decryption": "none"}, "streamSettings": stream}}, "outbounds": []any{map[string]any{"protocol": "freedom"}}})
	if err != nil {
		t.Fatal(err)
	}
	var cfg conf.Config
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("invalid synthetic config")
	}
	built, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	server, err := core.New(built)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return client, certificate
}
