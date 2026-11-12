package deploy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/defaults"
	"ovpn/internal/haproxy"
	"ovpn/internal/model"
)

// This helper runs only inside the disposable backend containers.
func TestHAHTTPBackend(t *testing.T) {
	body := os.Getenv("OVPN_HA_BACKEND_RESPONSE")
	if body == "" {
		t.Skip("container endpoint helper")
	}
	server := &http.Server{Addr: ":443", ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })}
	if err := server.ListenAndServe(); err != nil {
		t.Fatal(err)
	}
}

func TestHAProxyFailover(t *testing.T) {
	if os.Getenv("OVPN_E2E_HA") != "1" {
		t.Skip("run make e2e-ha for disposable Docker failover acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	run := func(args ...string) string {
		t.Helper()
		command := exec.CommandContext(ctx, "docker", args...)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Docker %s failed: %v", args[0], err)
		}
		return strings.TrimSpace(string(out))
	}
	arch := run("version", "--format", "{{.Server.Arch}}")
	if arch != "amd64" && arch != "arm64" {
		t.Fatal("unsupported Docker test architecture")
	}
	run("pull", defaults.DefaultHAProxyImage)
	dir := t.TempDir()
	binary := filepath.Join(dir, "ha-endpoint")
	compile := exec.CommandContext(ctx, "go", "test", "-c", "-o", binary, ".")
	compile.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("build isolated endpoint: %v (%d output bytes)", err, len(out))
	}
	name := fmt.Sprintf("ovpn-ha-%d", time.Now().UnixNano())
	run("network", "create", name)
	t.Cleanup(func() { _ = exec.Command("docker", "network", "rm", name).Run() })
	start := func(label string, extra ...string) string {
		t.Helper()
		args := []string{"run", "-d", "--network", name, "--name", name + "-" + label, "--read-only", "--user", "0:0"}
		args = append(args, extra...)
		args = append(args, defaults.DefaultHAProxyImage)
		if label != "proxy" {
			args = append(args, "-test.run=^TestHAHTTPBackend$")
		}
		id := run(args...)
		t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
		return id
	}
	first := start("one", "--network-alias", "relay-a", "--entrypoint", "/ha-endpoint", "--mount", "type=bind,src="+binary+",dst=/ha-endpoint,readonly", "--env", "OVPN_HA_BACKEND_RESPONSE=first")
	second := start("two", "--network-alias", "relay-b", "--entrypoint", "/ha-endpoint", "--mount", "type=bind,src="+binary+",dst=/ha-endpoint,readonly", "--env", "OVPN_HA_BACKEND_RESPONSE=second")
	config := renderHAProxyConfig([]model.Server{{ID: 1, Host: "relay-a"}, {ID: 2, Host: "relay-b"}})
	path := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	proxy := start("proxy", "--publish", "127.0.0.1::8404", "--publish", "127.0.0.1::15443", "--mount", "type=bind,src="+path+",dst=/usr/local/etc/haproxy/haproxy.cfg,readonly")
	address := func(port string) string {
		mapping := run("port", proxy, port)
		return "http://" + strings.Split(mapping, "\n")[0]
	}
	metrics, traffic := address("8404"), address("15443")
	until := func(state string) {
		t.Helper()
		end := time.Now().Add(25 * time.Second)
		for time.Now().Before(end) {
			sample := haproxy.Read(ctx, metrics+"/metrics")
			if sample.State == state && len(sample.Backends) == 2 {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("HA pool did not reach %s", state)
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil}}
	defer client.CloseIdleConnections()
	request := func() (string, error) {
		response, err := client.Get(traffic)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1024))
		return string(raw), err
	}
	until("online")
	seen := map[string]bool{}
	for range 10 {
		body, err := request()
		if err != nil {
			t.Fatal("healthy pool rejected traffic")
		}
		seen[body] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Fatal("traffic did not reach both pool members")
	}
	run("stop", "--time", "1", first)
	until("degraded")
	for range 3 {
		body, err := request()
		if err != nil || body != "second" {
			t.Fatal("failed backend interrupted healthy routing")
		}
	}
	run("stop", "--time", "1", second)
	until("failed")
	if body, err := request(); err == nil && body != "" {
		t.Fatal("empty pool did not fail closed")
	}
	run("start", first)
	until("degraded")
	if body, err := request(); err != nil || body != "first" {
		t.Fatal("recovered backend did not restore traffic")
	}
	t.Log("PASS: healthy pool, backend loss, all-down fail-closed, and backend recovery")
}
