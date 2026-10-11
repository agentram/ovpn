package cli

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/defaults"
	"ovpn/internal/deploy"
	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

// This opt-in regression uses synthetic identities and an isolated Docker network.
// Only image downloads need Internet access; tunnel traffic stays in the fixture.
func TestClientCoreCompatibility(t *testing.T) {
	if os.Getenv("OVPN_TEST_CLIENT_COMPAT") != "1" {
		t.Skip("set OVPN_TEST_CLIENT_COMPAT=1 to test client cores with Docker")
	}
	images := map[string]string{
		"xray":     defaults.DefaultXrayImage(""),
		"sing-box": "ghcr.io/sagernet/sing-box:v1.14.3",
		"mihomo":   "metacubex/mihomo:v1.19.32",
	}
	for _, image := range []string{images["xray"], images["sing-box"], images["mihomo"], defaults.DefaultWebImage} {
		if _, err := compatibilityDocker("image", "inspect", image); err != nil {
			mustCompatibilityDocker(t, "pull", image)
		}
	}
	network := fmt.Sprintf("ovpn-client-compat-%d", time.Now().UnixNano())
	mustCompatibilityDocker(t, "network", "create", network)
	t.Cleanup(func() { _, _ = compatibilityDocker("network", "rm", network) })
	certDir, err := writeValidationTLSSelfSNICertificate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(certDir) })
	nginx := `server {
  listen 443 ssl;
  http2 on;
  ssl_protocols TLSv1.3;
  ssl_certificate /fixture/fullchain.pem;
  ssl_certificate_key /fixture/privkey.pem;
  location / { return 200 "ovpn-client-compatibility\n"; }
}`
	writeCompatibilityFile(t, filepath.Join(certDir, "nginx.conf"), []byte(nginx))
	fixture := network + "-fixture"
	startCompatibilityContainer(t, fixture, network, []string{
		"--network-alias", "ovpn-target.invalid", "--user", "0:0",
		"-v", certDir + ":/fixture:ro", "-v", filepath.Join(certDir, "nginx.conf") + ":/etc/nginx/conf.d/default.conf:ro",
		defaults.DefaultWebImage,
	})
	fixtureIP := strings.TrimSpace(mustCompatibilityDocker(t, "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", fixture))
	certificate, err := os.ReadFile(filepath.Join(certDir, "fullchain.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		t.Fatal("invalid fixture certificate")
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	app := newTestAppWithServer(t, true)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatal(err)
	}
	serverContainer := network + "-server"
	srv.Domain = "ovpn-validation.invalid"
	srv.RealityPrivateKey = base64.RawURLEncoding.EncodeToString(key.Bytes())
	srv.RealityPublicKey = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	srv.RealityServerName = "ovpn-validation.invalid"
	srv.RealityTarget = "ovpn-target.invalid:443"
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatal(err)
	}
	user := model.User{ServerID: srv.ID, Username: "alice", UUID: "11111111-1111-1111-1111-111111111111", Email: "alice@example.net", Enabled: true}
	if err := app.store.AddUser(app.ctx, &user); err != nil {
		t.Fatal(err)
	}
	serverDir := t.TempDir()
	configPath := filepath.Join(serverDir, "config.json")
	renderServer := func(version string, enabled bool) {
		t.Helper()
		cmd := app.serverCmd()
		cmd.SetArgs([]string{"set-reality-min-client-version", "main", version})
		if _, _, err := captureStdoutStderr(t, cmd.Execute); err != nil {
			t.Fatal(err)
		}
		updated, err := app.store.GetServerByName(app.ctx, "main")
		if err != nil {
			t.Fatal(err)
		}
		user.Enabled = enabled
		bundle, err := deploy.RenderBundle(deploy.Input{Server: *updated, Users: []model.User{user}, SecurityProfile: xraycfg.SecurityProfileOff, ConnectionDiagnostics: "off"})
		if err != nil {
			t.Fatal(err)
		}
		defer deploy.CleanupBundle(bundle)
		// Xray blocks private egress by default. Allow only this synthetic HTTPS
		// fixture; production rendering and its private-network protection stay intact.
		var cfg struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if err := json.Unmarshal(bundle.ConfigRaw, &cfg); err != nil {
			t.Fatal(err)
		}
		for _, outbound := range cfg.Outbounds {
			if outbound["tag"] == "direct" {
				outbound["settings"].(map[string]any)["finalRules"] = []any{map[string]any{"action": "allow", "ip": []string{fixtureIP + "/32"}}}
			}
		}
		var rendered map[string]any
		if err := json.Unmarshal(bundle.ConfigRaw, &rendered); err != nil {
			t.Fatal(err)
		}
		rendered["outbounds"] = cfg.Outbounds
		raw, err := json.Marshal(rendered)
		if err != nil {
			t.Fatal(err)
		}
		writeCompatibilityFile(t, configPath, raw)
	}
	renderServer("default", true)
	startCompatibilityContainer(t, serverContainer, network, []string{"--network-alias", srv.Domain, "--user", "0:0", "-v", configPath + ":/etc/xray/config.json:ro", "-v", certDir + ":/etc/xray/certs:ro", images["xray"], "run", "-config", "/etc/xray/config.json"})
	check := func(t *testing.T, core, fingerprint string, wantPass bool) {
		t.Helper()
		time.Sleep(300 * time.Millisecond)
		if strings.TrimSpace(mustCompatibilityDocker(t, "inspect", "--format", "{{.State.Running}}", serverContainer)) != "true" {
			logs, _ := compatibilityDocker("logs", serverContainer)
			t.Fatalf("server did not start: %s", logs)
		}
		current, err := app.store.GetServerByName(app.ctx, "main")
		if err != nil {
			t.Fatal(err)
		}
		link, err := buildUserProfileLink(*current, user, current.NormalizedPrimaryProfile(), userLinkOptions{fingerprint: fingerprint})
		if err != nil {
			t.Fatal(err)
		}
		config, command := compatibilityClientConfig(t, core, link)
		file := filepath.Join(t.TempDir(), "client.json")
		writeCompatibilityFile(t, file, config)
		name := fmt.Sprintf("%s-client-%d", network, time.Now().UnixNano())
		args := []string{"--user", "0:0", "-p", "127.0.0.1::1080", "-v", file + ":/config.json:ro", "-v", filepath.Join(certDir, "fullchain.pem") + ":/fixture-ca.pem:ro", "-e", "SSL_CERT_FILE=/fixture-ca.pem", images[core]}
		if core == "mihomo" {
			args = append([]string{"-p", "127.0.0.1::9090"}, args...)
		}
		startCompatibilityContainer(t, name, network, append(args, command...))
		defer func() { _, _ = compatibilityDocker("rm", "-f", name) }()
		port := strings.TrimSpace(mustCompatibilityDocker(t, "port", name, "1080"))
		deadline := time.Now().Add(5 * time.Second)
		for {
			conn, err := net.DialTimeout("tcp", port, 250*time.Millisecond)
			if err == nil {
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				_, err = conn.Write([]byte{5, 1, 0})
				var reply [2]byte
				if err == nil {
					_, err = io.ReadFull(conn, reply[:])
				}
				_ = conn.Close()
				if err == nil && reply == [2]byte{5, 0} {
					break
				}
			}
			if time.Now().After(deadline) {
				t.Fatal("client SOCKS listener did not start")
			}
			time.Sleep(100 * time.Millisecond)
		}
		if core == "mihomo" {
			// The SOCKS listener opens before Mihomo finishes loading providers.
			controller := strings.TrimSpace(mustCompatibilityDocker(t, "port", name, "9090"))
			readyClient := &http.Client{Timeout: time.Second}
			deadline := time.Now().Add(5 * time.Second)
			for {
				response, err := readyClient.Get("http://" + controller + "/proxies/vpn")
				if err == nil {
					_ = response.Body.Close()
					if response.StatusCode == http.StatusOK {
						break
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("Mihomo provider did not become ready")
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "socks5", Host: port}), TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "ovpn-validation.invalid", MinVersion: tls.VersionTLS13}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 8 * time.Second}
		response, requestErr := client.Get("https://" + fixtureIP + "/")
		passed := false
		if requestErr == nil {
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			passed = err == nil && response.StatusCode == http.StatusOK && string(body) == "ovpn-client-compatibility\n"
		}
		if !wantPass && core == "sing-box" {
			logs, _ := compatibilityDocker("logs", name)
			if !strings.Contains(logs, "REALITY verification failed") && !strings.Contains(logs, "reality verification failed") && !strings.Contains(logs, "x509: certificate signed by unknown authority") {
				t.Fatalf("expected REALITY verification refusal, got: %v; %s", requestErr, logs)
			}
		}
		if passed != wantPass {
			logs, _ := compatibilityDocker("logs", name)
			serverLogs, _ := compatibilityDocker("logs", serverContainer)
			t.Fatalf("%s/%s traffic passed=%v, want %v; error=%v; client log=%s; server log=%s", core, fingerprint, passed, wantPass, requestErr, logs, serverLogs)
		}
	}
	t.Run("xray-default-policy", func(t *testing.T) { check(t, "xray", "chrome", true) })
	t.Run("sing-box-default-policy-refused", func(t *testing.T) { check(t, "sing-box", "chrome", false) })
	renderServer("1.0.0", true)
	mustCompatibilityDocker(t, "restart", serverContainer)
	for _, core := range []string{"xray", "sing-box", "mihomo"} {
		for _, fingerprint := range []string{"chrome", "firefox"} {
			t.Run(core+"/"+fingerprint, func(t *testing.T) { check(t, core, fingerprint, true) })
		}
	}
	renderServer("1.0.0", false)
	mustCompatibilityDocker(t, "restart", serverContainer)
	t.Run("disabled-user-refused", func(t *testing.T) { check(t, "xray", "chrome", false) })
	renderServer("default", true)
	mustCompatibilityDocker(t, "restart", serverContainer)
	t.Run("reset-restores-default-policy", func(t *testing.T) { check(t, "sing-box", "chrome", false) })
	profileCmd := app.serverCmd()
	profileCmd.SetArgs([]string{"profile", "switch", "main", model.TransportProfileTLSSelfSNIWeb})
	if _, _, err := captureStdoutStderr(t, profileCmd.Execute); err != nil {
		t.Fatal(err)
	}
	renderServer("default", true)
	mustCompatibilityDocker(t, "restart", serverContainer)
	for _, core := range []string{"xray", "sing-box", "mihomo"} {
		for _, fingerprint := range []string{"chrome", "firefox"} {
			t.Run("tls-self-sni/"+core+"/"+fingerprint, func(t *testing.T) { check(t, core, fingerprint, true) })
		}
	}
}

func compatibilityClientConfig(t *testing.T, core, link string) ([]byte, []string) {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	var config any
	var command []string
	switch core {
	case "xray":
		config = map[string]any{
			"log":      map[string]any{"loglevel": "warning"},
			"inbounds": []any{map[string]any{"listen": "0.0.0.0", "port": 1080, "protocol": "socks", "settings": map[string]any{"auth": "noauth"}}},
			"outbounds": []any{map[string]any{
				"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": 443, "users": []any{map[string]any{"id": u.User.Username(), "encryption": "none", "flow": q.Get("flow")}}}}},
				"streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": q.Get("sni"), "password": q.Get("pbk"), "shortId": q.Get("sid"), "fingerprint": q.Get("fp"), "spiderX": q.Get("spx")}},
			}},
		}
		command = []string{"run", "-config", "/config.json"}
	case "sing-box":
		config = map[string]any{
			"log":       map[string]any{"level": "warn"},
			"inbounds":  []any{map[string]any{"type": "socks", "listen": "0.0.0.0", "listen_port": 1080}},
			"outbounds": []any{map[string]any{"type": "vless", "server": u.Hostname(), "server_port": 443, "uuid": u.User.Username(), "flow": q.Get("flow"), "tls": map[string]any{"enabled": true, "server_name": q.Get("sni"), "utls": map[string]any{"enabled": true, "fingerprint": q.Get("fp")}, "reality": map[string]any{"enabled": true, "public_key": q.Get("pbk"), "short_id": q.Get("sid")}}}},
		}
		command = []string{"run", "-c", "/config.json"}
	case "mihomo":
		config = map[string]any{
			"external-controller": "0.0.0.0:9090", "socks-port": 1080, "allow-lan": true, "bind-address": "*", "mode": "rule", "log-level": "warning",
			"proxies": []any{map[string]any{"name": "vpn", "type": "vless", "server": u.Hostname(), "port": 443, "uuid": u.User.Username(), "flow": q.Get("flow"), "tls": true, "servername": q.Get("sni"), "client-fingerprint": q.Get("fp"), "reality-opts": map[string]any{"public-key": q.Get("pbk"), "short-id": q.Get("sid")}}},
			"rules":   []string{"MATCH,vpn"},
		}
		command = []string{"-f", "/config.json"}
	default:
		t.Fatalf("unknown client core %q", core)
	}
	if q.Get("security") == "tls" {
		cfg := config.(map[string]any)
		alpn := strings.Split(q.Get("alpn"), ",")
		switch core {
		case "xray":
			stream := cfg["outbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)
			delete(stream, "realitySettings")
			stream["security"] = "tls"
			stream["tlsSettings"] = map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "alpn": alpn}
		case "sing-box":
			tlsSettings := cfg["outbounds"].([]any)[0].(map[string]any)["tls"].(map[string]any)
			delete(tlsSettings, "reality")
			tlsSettings["alpn"] = alpn
		case "mihomo":
			proxy := cfg["proxies"].([]any)[0].(map[string]any)
			delete(proxy, "reality-opts")
			proxy["alpn"] = alpn
		}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return raw, command
}

func writeCompatibilityFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func startCompatibilityContainer(t *testing.T, name, network string, args []string) {
	t.Helper()
	mustCompatibilityDocker(t, append([]string{"run", "-d", "--name", name, "--network", network}, args...)...)
	t.Cleanup(func() { _, _ = compatibilityDocker("rm", "-f", name) })
}

func compatibilityDocker(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	return string(output), err
}

func mustCompatibilityDocker(t *testing.T, args ...string) string {
	t.Helper()
	output, err := compatibilityDocker(args...)
	if err != nil {
		t.Fatalf("docker %s failed: %v: %s", args[0], err, output)
	}
	return output
}
