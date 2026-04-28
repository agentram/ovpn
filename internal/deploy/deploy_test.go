package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/ssh"
	"ovpn/internal/xraycfg"
)

type fakeRunner struct {
	execCmds []string
	copyOps  [][2]string
	status   string
}

type failingRunner struct {
	fakeRunner
	failOn string
	err    error
}

type blockingRunner struct {
	fakeRunner
}

func (f *failingRunner) Exec(_ context.Context, _ ssh.Config, remoteCmd string) (ssh.Result, error) {
	f.execCmds = append(f.execCmds, remoteCmd)
	if f.failOn != "" && strings.Contains(remoteCmd, f.failOn) {
		return ssh.Result{Stderr: f.err.Error()}, f.err
	}
	if strings.Contains(remoteCmd, "docker compose") && strings.Contains(remoteCmd, " ps") {
		return ssh.Result{Stdout: f.status}, nil
	}
	return ssh.Result{}, nil
}

func (f *fakeRunner) Exec(_ context.Context, _ ssh.Config, remoteCmd string) (ssh.Result, error) {
	f.execCmds = append(f.execCmds, remoteCmd)
	if strings.Contains(remoteCmd, "docker compose") && strings.Contains(remoteCmd, " ps") {
		return ssh.Result{Stdout: f.status}, nil
	}
	return ssh.Result{}, nil
}

func (f *fakeRunner) CopyFile(_ context.Context, _ ssh.Config, localPath, remotePath string) error {
	if _, err := os.Stat(localPath); err != nil {
		return err
	}
	f.copyOps = append(f.copyOps, [2]string{localPath, remotePath})
	return nil
}

func (b *blockingRunner) Exec(ctx context.Context, _ ssh.Config, remoteCmd string) (ssh.Result, error) {
	b.execCmds = append(b.execCmds, remoteCmd)
	<-ctx.Done()
	return ssh.Result{}, ctx.Err()
}

func (b *blockingRunner) CopyFile(ctx context.Context, _ ssh.Config, localPath, remotePath string) error {
	if _, err := os.Stat(localPath); err != nil {
		return err
	}
	b.copyOps = append(b.copyOps, [2]string{localPath, remotePath})
	<-ctx.Done()
	return ctx.Err()
}

func TestRenderBundleWithOverride(t *testing.T) {
	t.Parallel()

	tmpAgent := filepath.Join(t.TempDir(), "ovpn-agent")
	if err := os.WriteFile(tmpAgent, []byte("fake-agent"), 0o755); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	tmpBot := filepath.Join(t.TempDir(), "ovpn-telegram-bot")
	if err := os.WriteFile(tmpBot, []byte("fake-bot"), 0o755); err != nil {
		t.Fatalf("write bot: %v", err)
	}

	override := []byte(`{"inbounds":[{"tag":"vless-reality","port":8443}]}`)
	bundle, err := RenderBundle(Input{
		Server: model.Server{
			XrayVersion:       "26.3.27",
			Domain:            "example.com",
			RealityPrivateKey: "priv",
			RealityServerName: "www.microsoft.com",
			RealityTarget:     "www.microsoft.com:443",
			RealityShortIDs:   "abcd",
		},
		AgentBinaryPath:       tmpAgent,
		TelegramBotBinaryPath: tmpBot,
		RenderedOverride:      override,
		XrayImage:             "ghcr.io/xtls/xray-core:26.3.27",
		AgentImage:            "alpine:3.23.4",
	})
	if err != nil {
		t.Fatalf("render bundle: %v", err)
	}
	defer CleanupBundle(bundle)

	gotCfg, err := os.ReadFile(filepath.Join(bundle.Dir, "xray", "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(gotCfg) != string(override) {
		t.Fatalf("config override mismatch")
	}
	cfgInfo, err := os.Stat(filepath.Join(bundle.Dir, "xray", "config.json"))
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := cfgInfo.Mode().Perm(); got != 0o644 {
		t.Fatalf("config mode = %o, want 644", got)
	}
	gotEnv, err := os.ReadFile(filepath.Join(bundle.Dir, ".env"))
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	envInfo, err := os.Stat(filepath.Join(bundle.Dir, ".env"))
	if err != nil {
		t.Fatalf("stat env: %v", err)
	}
	if got := envInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf(".env mode = %o, want 600", got)
	}
	if !strings.Contains(string(gotEnv), "XRAY_IMAGE=ghcr.io/xtls/xray-core:26.3.27") {
		t.Fatalf("missing xray image in env: %q", string(gotEnv))
	}
	if !strings.Contains(string(gotEnv), "OVPN_AGENT_LOG_LEVEL=info") {
		t.Fatalf("missing agent log level in env: %q", string(gotEnv))
	}
	if !strings.Contains(string(gotEnv), "OVPN_AGENT_HOST_PORT=19000") {
		t.Fatalf("missing default agent host port in env: %q", string(gotEnv))
	}
	if !strings.Contains(string(gotEnv), "OVPN_AGENT_CERT_FILE=/tmp/ovpn-agent-cert.pem") {
		t.Fatalf("missing agent cert file in env: %q", string(gotEnv))
	}
	if !strings.Contains(string(gotEnv), "OVPN_CERT_FULLCHAIN_PATH=/dev/null") {
		t.Fatalf("missing cert host path in env: %q", string(gotEnv))
	}
	gotCompose, err := os.ReadFile(filepath.Join(bundle.Dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	if !strings.Contains(string(gotCompose), `command: ["run", "-config", "/etc/xray/config.json"]`) {
		t.Fatalf("expected xray service to use entrypoint-aware command, got:\n%s", string(gotCompose))
	}
	if strings.Contains(string(gotCompose), `command: ["xray", "run", "-config", "/etc/xray/config.json"]`) {
		t.Fatalf("unexpected duplicate xray command in compose")
	}
	if !strings.Contains(string(gotCompose), `- "127.0.0.1:${OVPN_AGENT_HOST_PORT:-19000}:9090"`) {
		t.Fatalf("expected ovpn-agent host port mapping variable for host-local control plane access, got:\n%s", string(gotCompose))
	}
	if !strings.Contains(string(gotEnv), "PROMETHEUS_IMAGE=prom/prometheus:v3.11.2") {
		t.Fatalf("missing prometheus image in env: %q", string(gotEnv))
	}
	for _, p := range []string{
		"docker-compose.monitoring.yml",
		"monitoring/prometheus/prometheus.yml",
		"monitoring/prometheus/rules/ovpn-alerts.yml",
		"monitoring/alertmanager/alertmanager.yml",
		"monitoring/telegram-bot",
		"monitoring/grafana/provisioning/alerting",
		"monitoring/grafana/provisioning/datasources/prometheus.yml",
		"monitoring/grafana/provisioning/dashboards/dashboards.yml",
		"monitoring/grafana/provisioning/plugins",
		"monitoring/grafana/dashboards/ovpn-host.json",
		"monitoring/grafana/dashboards/ovpn-containers.json",
		"monitoring/grafana/dashboards/ovpn-agent.json",
		"monitoring/grafana/dashboards/ovpn-users.json",
		"monitoring/grafana/dashboards/ovpn-proxy.json",
		"monitoring/secrets/telegram_bot_token",
		"monitoring/secrets/telegram_admin_token",
	} {
		if _, err := os.Stat(filepath.Join(bundle.Dir, p)); err != nil {
			t.Fatalf("expected monitoring asset %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(bundle.Dir, "monitoring/telegram-bot/ovpn-telegram-bot")); err != nil {
		t.Fatalf("expected bundled telegram bot binary: %v", err)
	}
	alertCfg, err := os.ReadFile(filepath.Join(bundle.Dir, "monitoring/alertmanager/alertmanager.yml"))
	if err != nil {
		t.Fatalf("read alertmanager config: %v", err)
	}
	if !strings.Contains(string(alertCfg), "http://ovpn-telegram-bot:8080/alertmanager") {
		t.Fatalf("expected alertmanager webhook receiver, got:\n%s", string(alertCfg))
	}
	monitoringCompose, err := os.ReadFile(filepath.Join(bundle.Dir, "docker-compose.monitoring.yml"))
	if err != nil {
		t.Fatalf("read monitoring compose: %v", err)
	}
	for _, want := range []string{"--data.retention=168h", "/run/udev:/run/udev:ro", "/dev/kmsg:/dev/kmsg:ro"} {
		if !strings.Contains(string(monitoringCompose), want) {
			t.Fatalf("expected monitoring compose to contain %q, got:\n%s", want, string(monitoringCompose))
		}
	}
}

func TestRenderBundleAppliesMonitoringAndTelegramDefaults(t *testing.T) {
	t.Parallel()

	bundle, err := RenderBundle(Input{
		Server: model.Server{
			XrayVersion:       "26.3.27",
			Domain:            "example.com",
			RealityPrivateKey: "priv",
			RealityServerName: "www.microsoft.com",
			RealityTarget:     "www.microsoft.com:443",
			RealityShortIDs:   "abcd",
		},
	})
	if err != nil {
		t.Fatalf("render bundle: %v", err)
	}
	defer CleanupBundle(bundle)

	envRaw, err := os.ReadFile(filepath.Join(bundle.Dir, ".env"))
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	env := string(envRaw)
	for _, want := range []string{
		"PROMETHEUS_IMAGE=prom/prometheus:v3.11.2",
		"ALERTMANAGER_IMAGE=prom/alertmanager:v0.32.0",
		"GRAFANA_IMAGE=grafana/grafana:12.4.3",
		"OVPN_AGENT_HOST_PORT=19000",
		"OVPN_TELEGRAM_BOT_IMAGE=alpine:3.23.4",
		"OVPN_TELEGRAM_BOT_HOST_PORT=19001",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("missing expected default in .env: %q", want)
		}
	}

	secretInfo, err := os.Stat(filepath.Join(bundle.Dir, "monitoring/secrets/telegram_bot_token"))
	if err != nil {
		t.Fatalf("stat telegram_bot_token: %v", err)
	}
	if secretInfo.Mode().Perm() != 0o600 {
		t.Fatalf("telegram_bot_token mode: got %o want 600", secretInfo.Mode().Perm())
	}
	adminSecretInfo, err := os.Stat(filepath.Join(bundle.Dir, "monitoring/secrets/telegram_admin_token"))
	if err != nil {
		t.Fatalf("stat telegram_admin_token: %v", err)
	}
	if adminSecretInfo.Mode().Perm() != 0o600 {
		t.Fatalf("telegram_admin_token mode: got %o want 600", adminSecretInfo.Mode().Perm())
	}
}

func TestRenderBundleProxyIncludesHAProxyAndGeodata(t *testing.T) {
	t.Parallel()

	geositePath := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(geositePath, []byte("geosite"), 0o644); err != nil {
		t.Fatalf("write geosite: %v", err)
	}
	geoipPath := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(geoipPath, []byte("geoip"), 0o644); err != nil {
		t.Fatalf("write geoip: %v", err)
	}

	bundle, err := RenderBundle(Input{
		Server: model.Server{
			Role:              model.ServerRoleProxy,
			XrayVersion:       "26.3.27",
			Domain:            "proxy.example.com",
			RealityPrivateKey: "priv",
			RealityPublicKey:  "proxy-pub",
			RealityServerName: "www.microsoft.com",
			RealityTarget:     "www.microsoft.com:443",
			RealityShortIDs:   "abcd1234",
		},
		BackendServers: []model.Server{
			{Host: "198.51.100.10"},
			{Host: "203.0.113.20"},
		},
		ProxyRelay: &xraycfg.ProxyRelay{
			Address:     "haproxy",
			Port:        15443,
			ServiceUUID: "22222222-2222-2222-2222-222222222222",
			ServerName:  "backend.example.com",
			PublicKey:   "backend-pub",
			ShortID:     "deadbeef",
		},
		ProxyGeoSitePath: geositePath,
		ProxyGeoIPPath:   geoipPath,
	})
	if err != nil {
		t.Fatalf("render proxy bundle: %v", err)
	}
	defer CleanupBundle(bundle)

	gotCompose, err := os.ReadFile(filepath.Join(bundle.Dir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read compose: %v", err)
	}
	compose := string(gotCompose)
	for _, want := range []string{
		"haproxy:",
		"./haproxy/haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro",
		"./geodata/geosite.dat:/usr/local/share/xray/geosite.dat:ro",
		"./geodata/geoip.dat:/usr/local/share/xray/geoip.dat:ro",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("expected proxy compose to contain %q, got:\n%s", want, compose)
		}
	}

	gotEnv, err := os.ReadFile(filepath.Join(bundle.Dir, ".env"))
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if !strings.Contains(string(gotEnv), "HAPROXY_IMAGE=haproxy:3.2.15-alpine3.23") {
		t.Fatalf("expected haproxy image in env, got:\n%s", string(gotEnv))
	}
	if !strings.Contains(string(gotEnv), "OVPN_TELEGRAM_HAPROXY_URL=http://haproxy:8404/metrics") {
		t.Fatalf("expected telegram haproxy url in env, got:\n%s", string(gotEnv))
	}

	gotHAProxy, err := os.ReadFile(filepath.Join(bundle.Dir, "haproxy", "haproxy.cfg"))
	if err != nil {
		t.Fatalf("read haproxy config: %v", err)
	}
	haproxyCfg := string(gotHAProxy)
	for _, want := range []string{"bind 0.0.0.0:15443", "198.51.100.10:443", "203.0.113.20:443", "prometheus-exporter"} {
		if !strings.Contains(haproxyCfg, want) {
			t.Fatalf("expected haproxy config to contain %q, got:\n%s", want, haproxyCfg)
		}
	}

	gotProm, err := os.ReadFile(filepath.Join(bundle.Dir, "monitoring", "prometheus", "prometheus.yml"))
	if err != nil {
		t.Fatalf("read proxy prometheus config: %v", err)
	}
	if !strings.Contains(string(gotProm), "job_name: haproxy") || !strings.Contains(string(gotProm), "haproxy:8404") {
		t.Fatalf("expected proxy prometheus scrape config for haproxy, got:\n%s", string(gotProm))
	}

	gotRules, err := os.ReadFile(filepath.Join(bundle.Dir, "monitoring", "prometheus", "rules", "ovpn-alerts.yml"))
	if err != nil {
		t.Fatalf("read proxy alert rules: %v", err)
	}
	for _, want := range []string{"OVPNHAProxyMetricsDown", "OVPNForeignBackendPoolDown", "OVPNHAProxyContainerMissing"} {
		if !strings.Contains(string(gotRules), want) {
			t.Fatalf("expected proxy alert rule %q, got:\n%s", want, string(gotRules))
		}
	}
	if _, err := os.Stat(filepath.Join(bundle.Dir, "monitoring", "grafana", "dashboards", "ovpn-proxy.json")); err != nil {
		t.Fatalf("expected proxy dashboard asset in proxy bundle: %v", err)
	}

	for path, want := range map[string]string{
		filepath.Join(bundle.Dir, "geodata", "geosite.dat"): "geosite",
		filepath.Join(bundle.Dir, "geodata", "geoip.dat"):   "geoip",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read copied geodata %s: %v", path, err)
		}
		if string(raw) != want {
			t.Fatalf("unexpected copied geodata content for %s: %q", path, string(raw))
		}
	}
}

func TestRenderBundleSecurityProfileOffDisablesMinimalRules(t *testing.T) {
	t.Parallel()

	bundle, err := RenderBundle(Input{
		Server: model.Server{
			XrayVersion:       "26.3.27",
			Domain:            "example.com",
			RealityPrivateKey: "priv",
			RealityServerName: "www.microsoft.com",
			RealityTarget:     "www.microsoft.com:443",
			RealityShortIDs:   "abcd",
		},
		SecurityProfile: "off",
	})
	if err != nil {
		t.Fatalf("render bundle: %v", err)
	}
	defer CleanupBundle(bundle)

	cfgRaw, err := os.ReadFile(filepath.Join(bundle.Dir, "xray", "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg := string(cfgRaw)
	if strings.Contains(cfg, "category-public-tracker") {
		t.Fatalf("security profile off should not include tracker block rule")
	}
	if strings.Contains(cfg, "\"dns\"") {
		t.Fatalf("security profile off should not include dns section")
	}
}

func TestRenderBundleSecurityProfileMinimalUsesThreatDNS(t *testing.T) {
	t.Parallel()

	bundle, err := RenderBundle(Input{
		Server: model.Server{
			XrayVersion:       "26.3.27",
			Domain:            "example.com",
			RealityPrivateKey: "priv",
			RealityServerName: "www.microsoft.com",
			RealityTarget:     "www.microsoft.com:443",
			RealityShortIDs:   "abcd",
		},
		SecurityProfile:  "minimal",
		ThreatDNSServers: []string{"1.1.1.2", "9.9.9.9"},
	})
	if err != nil {
		t.Fatalf("render bundle: %v", err)
	}
	defer CleanupBundle(bundle)

	cfgRaw, err := os.ReadFile(filepath.Join(bundle.Dir, "xray", "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg := string(cfgRaw)
	if !strings.Contains(cfg, "\"servers\": [\n      \"1.1.1.2\",\n      \"9.9.9.9\"\n    ]") {
		t.Fatalf("expected custom threat DNS servers in config, got:\n%s", cfg)
	}
	if !strings.Contains(cfg, "category-public-tracker") || !strings.Contains(cfg, "\"bittorrent\"") {
		t.Fatalf("security profile minimal should include BT/tracker block rules")
	}
}

func TestBootstrapRemoteExecutesExpectedCommand(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	if err := BootstrapRemote(context.Background(), r, ssh.Config{}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(r.execCmds) != 1 {
		t.Fatalf("expected exactly one command, got %d", len(r.execCmds))
	}
	cmd := r.execCmds[0]
	for _, want := range []string{"get.docker.com", "docker-compose-plugin", "/opt/ovpn"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected bootstrap command to contain %q, got %q", want, cmd)
		}
	}
}

func TestDeployRemoteCommandSequence(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	if err := DeployRemote(context.Background(), r, ssh.Config{}); err != nil {
		t.Fatalf("deploy remote: %v", err)
	}
	if len(r.execCmds) != 6 {
		t.Fatalf("expected 6 commands, got %d", len(r.execCmds))
	}
	if !strings.Contains(r.execCmds[0], "/opt/ovpn-backups/ovpn-") {
		t.Fatalf("first command should backup stack, got %q", r.execCmds[0])
	}
	if !strings.Contains(r.execCmds[0], "timeout 30 sh -c") {
		t.Fatalf("first command should use bounded remote timeout, got %q", r.execCmds[0])
	}
	if !strings.Contains(r.execCmds[0], "find /opt/ovpn-backups -mindepth 1 -maxdepth 1") || !strings.Contains(r.execCmds[0], "ovpn-*") {
		t.Fatalf("first command should prune old pre-deploy snapshots, got %q", r.execCmds[0])
	}
	if !strings.Contains(r.execCmds[0], "NR>7") {
		t.Fatalf("first command should keep only latest snapshots, got %q", r.execCmds[0])
	}
	if !strings.Contains(r.execCmds[1], "/opt/ovpn/.incoming") {
		t.Fatalf("second command should validate staged bundle, got %q", r.execCmds[1])
	}
	if !strings.Contains(r.execCmds[3], "rm -f /opt/ovpn/agent/ovpn-agent") {
		t.Fatalf("fourth command should unlink agent binary before copy, got %q", r.execCmds[3])
	}
	if !strings.Contains(r.execCmds[3], "rm -f /opt/ovpn/agent/ovpn-agent /opt/ovpn/monitoring/telegram-bot/ovpn-telegram-bot") {
		t.Fatalf("fourth command should unlink agent+telegram bot binaries before copy, got %q", r.execCmds[3])
	}
	if !strings.Contains(r.execCmds[3], "cp -a /opt/ovpn/.incoming/. /opt/ovpn/") {
		t.Fatalf("fourth command should apply validated staged bundle, got %q", r.execCmds[3])
	}
	if !strings.Contains(r.execCmds[3], "sudo chown root:root /opt/ovpn/.env") || !strings.Contains(r.execCmds[3], "sudo chmod 600 /opt/ovpn/.env") {
		t.Fatalf("fourth command should lock down .env after copy, got %q", r.execCmds[3])
	}
	if !strings.Contains(r.execCmds[3], "sudo chown root:root /opt/ovpn/xray/config.json") || !strings.Contains(r.execCmds[3], "sudo chmod 644 /opt/ovpn/xray/config.json") {
		t.Fatalf("fourth command should keep xray config portable across container runtime users, got %q", r.execCmds[3])
	}
	if !strings.Contains(r.execCmds[4], "docker compose --env-file .env -f docker-compose.yml") || !strings.Contains(r.execCmds[4], "up -d --force-recreate --remove-orphans") {
		t.Fatalf("expected compose up command, got %q", r.execCmds[4])
	}
	if !strings.Contains(r.execCmds[4], "timeout 300 sh -c") {
		t.Fatalf("expected compose up command to use bounded remote timeout, got %q", r.execCmds[4])
	}
	if !strings.Contains(r.execCmds[4], "docker ps -a --format") || !strings.Contains(r.execCmds[4], "docker-compose.monitoring.yml") || !strings.Contains(r.execCmds[4], "--profile monitoring") {
		t.Fatalf("expected compose up command to preserve active monitoring stack, got %q", r.execCmds[4])
	}
	if !strings.Contains(r.execCmds[4], "--scale ovpn-telegram-bot=0") {
		t.Fatalf("expected compose up command to guard empty telegram token when monitoring is active, got %q", r.execCmds[4])
	}
	if !strings.Contains(r.execCmds[4], "--remove-orphans") {
		t.Fatalf("expected remove-orphans in deploy command, got %q", r.execCmds[4])
	}
	if !strings.Contains(r.execCmds[4], "--force-recreate") {
		t.Fatalf("expected force-recreate in deploy command, got %q", r.execCmds[4])
	}
	if !strings.Contains(r.execCmds[5], "docker compose ps") {
		t.Fatalf("expected final status command, got %q", r.execCmds[5])
	}
	if !strings.Contains(r.execCmds[2], "$XRAY_IMAGE run -test -config /etc/xray/config.json") {
		t.Fatalf("third command should validate xray config with image entrypoint-aware syntax, got %q", r.execCmds[2])
	}
	if strings.Contains(r.execCmds[2], "$XRAY_IMAGE xray run -test") {
		t.Fatalf("xray test command should not include duplicate leading 'xray': %q", r.execCmds[2])
	}
}

func TestBuildDeployBackupCommandIncludesRetentionPrune(t *testing.T) {
	t.Parallel()

	cmd := buildDeployBackupCommand("20260412T010203")
	for _, want := range []string{
		"cp -a /opt/ovpn /opt/ovpn-backups/ovpn-20260412T010203",
		"find /opt/ovpn-backups -mindepth 1 -maxdepth 1 -name 'ovpn-*'",
		"NR>7",
		"xargs -r sudo rm -rf",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("backup command missing %q: %q", want, cmd)
		}
	}
}

func TestDeployRemoteReturnsXrayVersionHintForMissingImageTag(t *testing.T) {
	t.Parallel()

	r := &failingRunner{
		failOn: "run -test -config /etc/xray/config.json",
		err:    errors.New("manifest for ghcr.io/xtls/xray-core:v26.3.27 not found"),
	}
	err := DeployRemote(context.Background(), r, ssh.Config{Host: "example-host"})
	if err == nil {
		t.Fatalf("expected deploy error")
	}
	if !strings.Contains(err.Error(), "without 'v' prefix") {
		t.Fatalf("expected xray version hint, got %v", err)
	}
}

func TestDeployRemoteOmitsVersionHintForGenericXrayValidationError(t *testing.T) {
	t.Parallel()

	r := &failingRunner{
		failOn: "run -test -config /etc/xray/config.json",
		err:    errors.New("docker run failed with exit code 125"),
	}
	err := DeployRemote(context.Background(), r, ssh.Config{Host: "example-host"})
	if err == nil {
		t.Fatalf("expected deploy error")
	}
	if !strings.Contains(err.Error(), "validate xray config in container") {
		t.Fatalf("expected validation context, got %v", err)
	}
	if strings.Contains(err.Error(), "without 'v' prefix") {
		t.Fatalf("did not expect xray-version hint for generic error, got %v", err)
	}
}

func TestUploadBundleCopiesAndExtracts(t *testing.T) {
	t.Parallel()

	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	r := &fakeRunner{}
	if err := UploadBundle(context.Background(), r, ssh.Config{}, bundleDir); err != nil {
		t.Fatalf("upload bundle: %v", err)
	}
	if len(r.copyOps) != 1 {
		t.Fatalf("expected one scp call, got %d", len(r.copyOps))
	}
	if len(r.execCmds) != 1 || !strings.Contains(r.execCmds[0], "tar -xzf") {
		t.Fatalf("expected extract command, got %#v", r.execCmds)
	}
	if !strings.Contains(r.execCmds[0], "timeout 30 sh -c") {
		t.Fatalf("expected extract command to use bounded remote timeout, got %#v", r.execCmds)
	}
	if !strings.Contains(r.execCmds[0], "sudo chown root:root /opt/ovpn/.incoming/.env") || !strings.Contains(r.execCmds[0], "sudo chmod 600 /opt/ovpn/.incoming/.env") {
		t.Fatalf("extract command should lock down staged .env before validation, got %#v", r.execCmds)
	}
	if !strings.Contains(r.execCmds[0], "sudo chown root:root /opt/ovpn/.incoming/xray/config.json") || !strings.Contains(r.execCmds[0], "sudo chmod 644 /opt/ovpn/.incoming/xray/config.json") {
		t.Fatalf("extract command should keep staged xray config portable across container runtime users before validation, got %#v", r.execCmds)
	}
}

func TestDeployRemoteReturnsTimeoutContextWhenRemoteStepHangs(t *testing.T) {
	prev := deployBackupTimeout
	deployBackupTimeout = 10 * time.Millisecond
	t.Cleanup(func() { deployBackupTimeout = prev })

	r := &blockingRunner{}
	err := DeployRemote(context.Background(), r, ssh.Config{Host: "example-host"})
	if err == nil {
		t.Fatalf("expected deploy timeout error")
	}
	if !strings.Contains(err.Error(), "create pre-deploy backup") {
		t.Fatalf("expected backup timeout context, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestUploadBundleReturnsTimeoutContextWhenCopyHangs(t *testing.T) {
	prev := uploadCopyTimeout
	uploadCopyTimeout = 10 * time.Millisecond
	t.Cleanup(func() { uploadCopyTimeout = prev })

	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	r := &blockingRunner{}
	err := UploadBundle(context.Background(), r, ssh.Config{Host: "example-host"}, bundleDir)
	if err == nil {
		t.Fatalf("expected upload timeout error")
	}
	if !strings.Contains(err.Error(), "copy bundle") {
		t.Fatalf("expected copy timeout context, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestRestartAndStatusCommands(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{status: "ok"}
	if err := RestartRemote(context.Background(), r, ssh.Config{}); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if !strings.Contains(r.execCmds[0], "restart xray ovpn-agent") {
		t.Fatalf("unexpected restart command: %q", r.execCmds[0])
	}
	out, err := RemoteStatus(context.Background(), r, ssh.Config{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if out != "ok" {
		t.Fatalf("unexpected status output: %q", out)
	}
}

func TestMonitoringCommands(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{status: "monitoring-ok"}
	if err := MonitoringUpRemote(context.Background(), r, ssh.Config{}); err != nil {
		t.Fatalf("monitoring up: %v", err)
	}
	if !strings.Contains(r.execCmds[0], "-f docker-compose.monitoring.yml --profile monitoring up -d") {
		t.Fatalf("unexpected monitoring up command: %q", r.execCmds[0])
	}
	if !strings.Contains(r.execCmds[0], "--scale ovpn-telegram-bot=0") {
		t.Fatalf("monitoring up command should include empty-token bot scale-down guard: %q", r.execCmds[0])
	}
	if err := MonitoringDownRemote(context.Background(), r, ssh.Config{}); err != nil {
		t.Fatalf("monitoring down: %v", err)
	}
	if !strings.Contains(r.execCmds[1], "stop prometheus alertmanager grafana node-exporter cadvisor ovpn-telegram-bot") {
		t.Fatalf("unexpected monitoring down command: %q", r.execCmds[1])
	}
	out, err := MonitoringStatusRemote(context.Background(), r, ssh.Config{})
	if err != nil {
		t.Fatalf("monitoring status: %v", err)
	}
	if out != "monitoring-ok" {
		t.Fatalf("unexpected monitoring status output: %q", out)
	}
}

func TestCleanupRemoteDefaultSequence(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	err := CleanupRemote(context.Background(), r, ssh.Config{}, CleanupOptions{
		IncludeMonitoring: true,
		RemoveVolumes:     true,
		RemoveBackups:     false,
	})
	if err != nil {
		t.Fatalf("cleanup remote: %v", err)
	}
	if len(r.execCmds) != 4 {
		t.Fatalf("expected 4 cleanup commands, got %d", len(r.execCmds))
	}
	if !strings.Contains(r.execCmds[0], "docker-compose.monitoring.yml") || !strings.Contains(r.execCmds[0], "--profile monitoring down --remove-orphans") {
		t.Fatalf("unexpected monitoring cleanup command: %q", r.execCmds[0])
	}
	if !strings.Contains(r.execCmds[1], "docker compose --env-file .env -f docker-compose.yml down --remove-orphans") {
		t.Fatalf("unexpected runtime down command: %q", r.execCmds[1])
	}
	if !strings.Contains(r.execCmds[2], "rm -rf /opt/ovpn") {
		t.Fatalf("unexpected runtime dir cleanup command: %q", r.execCmds[2])
	}
	if !strings.Contains(r.execCmds[3], "docker volume ls -q --filter label=com.docker.compose.project=ovpn") {
		t.Fatalf("unexpected volume cleanup command: %q", r.execCmds[3])
	}
}

func TestCleanupRemoteOptions(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	err := CleanupRemote(context.Background(), r, ssh.Config{}, CleanupOptions{
		IncludeMonitoring: false,
		RemoveVolumes:     false,
		RemoveBackups:     true,
	})
	if err != nil {
		t.Fatalf("cleanup remote: %v", err)
	}
	if len(r.execCmds) != 3 {
		t.Fatalf("expected 3 cleanup commands, got %d", len(r.execCmds))
	}
	if strings.Contains(r.execCmds[0], "monitoring") {
		t.Fatalf("monitoring cleanup should be skipped when disabled: %q", r.execCmds[0])
	}
	if strings.Contains(strings.Join(r.execCmds, "\n"), "label=com.docker.compose.project=ovpn") {
		t.Fatalf("volume cleanup should be skipped when disabled: %#v", r.execCmds)
	}
	if !strings.Contains(r.execCmds[2], "rm -rf /opt/ovpn-backups") {
		t.Fatalf("expected remote backup cleanup command, got %q", r.execCmds[2])
	}
}
