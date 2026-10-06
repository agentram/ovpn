package cli

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestWriteValidationTLSSelfSNICertificate(t *testing.T) {
	dir, err := writeValidationTLSSelfSNICertificate()
	if err != nil {
		t.Fatalf("write validation certificate: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	certificate := filepath.Join(dir, "fullchain.pem")
	key := filepath.Join(dir, "privkey.pem")
	if _, err := tls.LoadX509KeyPair(certificate, key); err != nil {
		t.Fatalf("load generated validation certificate: %v", err)
	}
	info, err := os.Stat(key)
	if err != nil {
		t.Fatalf("stat generated validation key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected private key mode 0600, got %o", info.Mode().Perm())
	}
}

func TestConfigValidatePrivateInputsAndCleanup(t *testing.T) {
	for _, exitCode := range []string{"0", "23"} {
		t.Run("docker_exit_"+exitCode, func(t *testing.T) {
			app := newGlobalUsersTestApp(t)
			proxy := addServerBackendTestServer(t, app, "proxy-test", model.ServerRoleProxy, "proxy-pub", "")
			backend := addServerBackendTestServer(t, app, "backend-test", model.ServerRoleVPN, "vpn-pub", "11111111-1111-1111-1111-111111111111")
			if err := app.store.UpsertProxyBackend(app.ctx, &model.ProxyBackend{ProxyServerID: proxy.ID, BackendServerID: backend.ID, Enabled: true, Priority: 10}); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			site, ip := filepath.Join(dir, "geosite.dat"), filepath.Join(dir, "geoip.dat")
			for _, path := range []string{site, ip} {
				if err := os.WriteFile(path, []byte("private geodata fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("OVPN_PROXY_GEOSITE_PATH", site)
			t.Setenv("OVPN_PROXY_GEOIP_PATH", ip)
			argsPath := filepath.Join(dir, "args")
			// Capture the path while it exists; verify its mode inside the fake command.
			modeFlag := "-c '%a'"
			if runtime.GOOS == "darwin" {
				modeFlag = "-f '%Lp'"
			}
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$OVPN_TEST_CONFIG_ARGS\"\nfor arg do\ncase \"$arg\" in\n*:/etc/xray/config.json:ro) path=${arg%:/etc/xray/config.json:ro}; test -s \"$path\" || exit 90; mode=$(stat " + modeFlag + " \"$path\"); test \"$mode\" = 600 || exit 91;;\nesac\ndone\nexit \"$OVPN_TEST_CONFIG_EXIT\"\n"
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("OVPN_TEST_CONFIG_ARGS", argsPath)
			t.Setenv("OVPN_TEST_CONFIG_EXIT", exitCode)
			cmd := app.configCmd()
			cmd.SetArgs([]string{"validate", "--server", proxy.Name})
			err := cmd.Execute()
			if exitCode == "0" && err != nil {
				t.Fatal(err)
			}
			if exitCode == "23" && (err == nil || !strings.Contains(err.Error(), "exit status 23")) {
				t.Fatalf("expected docker failure, got %v", err)
			}
			raw, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if !strings.Contains(string(raw), "--user\n"+fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())) {
				t.Fatalf("missing CLI identity: %s", raw)
			}
			for _, path := range []string{site, ip} {
				if !strings.Contains(string(raw), path+":/usr/local/share/xray/"+filepath.Base(path)+":ro") {
					t.Fatalf("missing geodata mount: %s", raw)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("source permissions changed: %v", err)
				}
			}
			var configPath string
			for _, arg := range args {
				if strings.HasSuffix(arg, ":/etc/xray/config.json:ro") {
					configPath = strings.TrimSuffix(arg, ":/etc/xray/config.json:ro")
				}
			}
			if configPath == "" {
				t.Fatal("config mount missing")
			}
			if _, err := os.Stat(filepath.Dir(configPath)); !os.IsNotExist(err) {
				t.Fatalf("temporary validation inputs remain after command: %v", err)
			}
		})
	}
}
