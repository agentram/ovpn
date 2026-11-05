package probe

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"

	"ovpn/internal/clientcfg"
)

func TestTLSTunnelCarriesHTTPSAndRejectsWrongIdentity(t *testing.T) {
	var hits atomic.Int64
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "synthetic-traffic")
	}))
	defer https.Close()
	roots := x509.NewCertPool()
	roots.AddCert(https.Certificate())
	cert := strings.Split(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw})), "\n")
	keyDER, err := x509.MarshalPKCS8PrivateKey(https.TLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Split(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})), "\n")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	const id = "11111111-1111-1111-1111-111111111111"
	stream, err := json.Marshal(map[string]any{"network": "tcp", "security": "tls", "tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificate": cert, "key": key}}}})
	if err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{
 "log":{"loglevel":"none","access":"none","error":"none"},
 "inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"vless","settings":{"clients":[{"id":"%s","flow":"xtls-rprx-vision"}],"decryption":"none"},"streamSettings":%s}],
 "outbounds":[{"protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.0/8"]}]}}]
 }`, port, id, stream)
	var config conf.Config
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	built, err := config.Build()
	if err != nil {
		t.Fatal(err)
	}
	server, err := core.New(built)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{id, "22222222-2222-2222-2222-222222222222"} {
		cfg, err := clientcfg.FromLink(fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=tls&encryption=none&sni=example.com&fp=firefox&flow=xtls-rprx-vision", userID, port))
		if err != nil {
			t.Fatal(err)
		}
		tls := cfg.Outbounds[0]["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
		tls["certificates"] = []any{map[string]any{"usage": "verify", "certificate": cert}}
		tunnel, err := Open(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		result, checkErr := Check(context.Background(), Options{URL: https.URL, ExpectedStatus: 200, Timeout: 2 * time.Second, MaxBodyBytes: 100, RootCAs: roots}, tunnel.DialContext)
		_ = tunnel.Close()
		if userID == id {
			if checkErr != nil || !result.Success || result.Bytes != int64(len("synthetic-traffic")) || hits.Load() != 1 {
				t.Fatalf("HTTPS did not cross the tunnel: %+v, %v; hits %d", result, checkErr, hits.Load())
			}
		} else if checkErr == nil || result.Success || hits.Load() != 1 {
			t.Fatalf("wrong identity reached HTTPS: %+v, %v; hits %d", result, checkErr, hits.Load())
		}
	}
}

func TestTunnelRejectsXHTTPBeforeStartingCore(t *testing.T) {
	cfg, err := clientcfg.FromLink("vless://11111111-1111-1111-1111-111111111111@127.0.0.1:1?type=xhttp&security=none&encryption=none&path=%2F&mode=auto")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "Xray Checker") {
		t.Fatalf("unsupported probe transport accepted: %v", err)
	}
}
