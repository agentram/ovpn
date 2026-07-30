package deploy

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ovpn/internal/defaults"
	"ovpn/internal/model"
	"ovpn/internal/util"
	"ovpn/internal/xraycfg"
)

// Input describes everything needed to render a deployable bundle.
// Optional image/credential fields are defaulted in RenderBundle to keep deploys reproducible.
type Input struct {
	Server                       model.Server
	BackendServers               []model.Server
	Users                        []model.User
	ServiceUsers                 []xraycfg.ServiceUser
	ProxyRelay                   *xraycfg.ProxyRelay
	SecurityProfile              string
	ThreatDNSServers             []string
	RealityLimitFallbackUpload   *xraycfg.FallbackRateLimit
	RealityLimitFallbackDownload *xraycfg.FallbackRateLimit
	AgentBinaryPath              string
	TelegramBotBinaryPath        string
	XrayImage                    string
	AgentImage                   string
	TelegramBotImage             string
	WebImage                     string
	HAProxyImage                 string
	AgentLogLevel                string
	AgentToken                   string
	AgentHostPort                string
	TelegramBotHostPort          string
	AgentCertFile                string
	AgentCertHostPath            string
	XrayLogLevel                 string
	ConnectionDiagnostics        string
	XrayAccessLogPath            string
	XrayAccessLogMaxBytes        string
	TLSSelfSNICertDir            string
	TLSSelfSNISiteDir            string
	PrometheusImage              string
	AlertmanagerImage            string
	GrafanaImage                 string
	NodeExporterImage            string
	CAdvisorImage                string
	GrafanaAdminUser             string
	GrafanaAdminPassword         string
	GrafanaPort                  string
	TelegramNotifyChatIDs        string
	TelegramOwnerUserID          string
	TelegramClientsPDFPath       string
	TelegramClientsPDFSource     string
	TelegramClientsRUPDFPath     string
	TelegramClientsRUPDFSource   string
	TelegramAPIFallbackIPs       string
	TelegramAdminToken           string
	TelegramLinkAddress          string
	TelegramLinkServerName       string
	TelegramLinkPublicKey        string
	TelegramLinkShortID          string
	ProxyGeoSitePath             string
	ProxyGeoIPPath               string
	RenderedOverride             []byte
}

// applyDefaults fills unset Input fields with their default images, ports, and paths.
func (in *Input) applyDefaults() {
	if in.XrayImage == "" {
		in.XrayImage = defaults.DefaultXrayImage(in.Server.XrayVersion)
	}
	if in.AgentImage == "" {
		in.AgentImage = defaults.DefaultAgentImage
	}
	in.TelegramBotImage = defaultString(in.TelegramBotImage, defaults.DefaultTelegramBotImage)
	in.WebImage = defaultString(in.WebImage, defaults.DefaultWebImage)
	in.HAProxyImage = defaultString(in.HAProxyImage, defaults.DefaultHAProxyImage)
	in.AgentLogLevel = defaultString(in.AgentLogLevel, "info")
	in.AgentHostPort = defaultString(in.AgentHostPort, "19000")
	in.TelegramBotHostPort = defaultString(in.TelegramBotHostPort, "19001")
	in.AgentCertFile = defaultString(in.AgentCertFile, "/tmp/ovpn-agent-cert.pem")
	in.AgentCertHostPath = defaultString(in.AgentCertHostPath, "/dev/null")
	in.ConnectionDiagnostics = defaultString(in.ConnectionDiagnostics, "basic")
	in.XrayAccessLogPath = defaultString(in.XrayAccessLogPath, "/var/log/ovpn/xray-access.log")
	in.XrayAccessLogMaxBytes = defaultString(in.XrayAccessLogMaxBytes, "31457280")
	in.TLSSelfSNICertDir = defaultString(in.TLSSelfSNICertDir, "/opt/ovpn/certs")
	in.TLSSelfSNISiteDir = defaultString(in.TLSSelfSNISiteDir, "/opt/ovpn/camouflage-site")
	in.PrometheusImage = defaultString(in.PrometheusImage, defaults.DefaultPrometheusImage)
	in.AlertmanagerImage = defaultString(in.AlertmanagerImage, defaults.DefaultAlertmanagerImage)
	in.GrafanaImage = defaultString(in.GrafanaImage, defaults.DefaultGrafanaImage)
	in.NodeExporterImage = defaultString(in.NodeExporterImage, defaults.DefaultNodeExporterImage)
	in.CAdvisorImage = defaultString(in.CAdvisorImage, defaults.DefaultCAdvisorImage)
	in.GrafanaAdminUser = defaultString(in.GrafanaAdminUser, "ovpn")
	in.GrafanaAdminPassword = defaultString(in.GrafanaAdminPassword, "change-me-now")
	in.GrafanaPort = defaultString(in.GrafanaPort, "3000")
	in.TelegramClientsPDFPath = defaultString(in.TelegramClientsPDFPath, "/opt/ovpn-telegram-bot/assets/clients.pdf")
	in.TelegramClientsPDFSource = defaultString(in.TelegramClientsPDFSource, "docs/clients.pdf")
	in.TelegramClientsRUPDFPath = defaultString(in.TelegramClientsRUPDFPath, "/opt/ovpn-telegram-bot/assets/clients-ru.pdf")
	in.TelegramClientsRUPDFSource = defaultString(in.TelegramClientsRUPDFSource, "docs/clients-ru.pdf")
	in.TelegramAPIFallbackIPs = defaultString(in.TelegramAPIFallbackIPs, "149.154.167.220")
	in.TelegramLinkAddress = defaultString(in.TelegramLinkAddress, firstNonEmpty(in.Server.Domain, in.Server.Host))
	in.TelegramLinkServerName = defaultString(in.TelegramLinkServerName, strings.TrimSpace(in.Server.RealityServerName))
	in.TelegramLinkPublicKey = defaultString(in.TelegramLinkPublicKey, strings.TrimSpace(in.Server.RealityPublicKey))
	in.TelegramLinkShortID = defaultString(in.TelegramLinkShortID, firstShortID(in.Server.RealityShortIDs))
}

// defaultString returns v when non-blank, otherwise fallback.
func defaultString(v string, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func injectXrayProfilePorts(composeTpl []byte, profiles []string) []byte {
	var extra []string
	for _, profile := range profiles {
		meta, ok := model.LookupTransportProfile(profile)
		if !ok || meta.Port == 443 || meta.Port <= 0 {
			continue
		}
		extra = append(extra, fmt.Sprintf("      - \"%d:%d/tcp\"", meta.Port, meta.Port))
	}
	if len(extra) == 0 {
		return composeTpl
	}
	text := string(composeTpl)
	marker := "      # OVPN_XRAY_PROFILE_PORTS\n"
	if strings.Contains(text, marker) {
		return []byte(strings.Replace(text, marker, strings.Join(extra, "\n")+"\n"+marker, 1))
	}
	needle := "      - \"443:443/tcp\"\n"
	return []byte(strings.Replace(text, needle, needle+strings.Join(extra, "\n")+"\n", 1))
}

func usesTLSSelfSNIProfile(profiles []string) bool {
	for _, profile := range profiles {
		if model.NormalizeTransportProfile(profile) == model.TransportProfileTLSSelfSNIWeb {
			return true
		}
	}
	return false
}

func injectTLSSelfSNIWeb(composeTpl []byte, profiles []string) []byte {
	if !usesTLSSelfSNIProfile(profiles) {
		return composeTpl
	}
	text := string(composeTpl)
	volumeMarker := "      # OVPN_XRAY_TLS_SELFSNI_VOLUMES\n"
	if strings.Contains(text, volumeMarker) {
		volumes := strings.Join([]string{
			"      - ${OVPN_TLS_SELFSNI_CERT_DIR:-/opt/ovpn/certs}:/etc/xray/certs:ro",
			volumeMarker[:len(volumeMarker)-1],
		}, "\n") + "\n"
		text = strings.Replace(text, volumeMarker, volumes, 1)
	}
	dependsMarker := "    # OVPN_XRAY_TLS_SELFSNI_DEPENDS_ON\n"
	if strings.Contains(text, dependsMarker) {
		depends := strings.Join([]string{
			"    depends_on:",
			"      - ovpn-web",
			dependsMarker[:len(dependsMarker)-1],
		}, "\n") + "\n"
		text = strings.Replace(text, dependsMarker, depends, 1)
	}
	serviceMarker := "  # OVPN_CAMOUFLAGE_WEB_SERVICE\n"
	if strings.Contains(text, serviceMarker) {
		webService := `  ovpn-web:
    image: ${OVPN_WEB_IMAGE}
    container_name: ovpn-web
    restart: unless-stopped
    expose:
      - "8080"
    volumes:
      - ${OVPN_CAMOUFLAGE_SITE_DIR:-/opt/ovpn/camouflage-site}:/usr/share/nginx/html:ro
      - ./web/nginx.conf:/etc/nginx/conf.d/default.conf:ro
    logging:
      driver: json-file
      options:
        max-size: "5m"
        max-file: "3"
    networks:
      - ovpn-net

`
		text = strings.Replace(text, serviceMarker, webService+serviceMarker, 1)
	}
	return []byte(text)
}

type Bundle struct {
	Dir       string
	ConfigRaw []byte
}

// RenderBundle renders the full deployable bundle (config, compose files, env, monitoring assets) into a temp directory.
func RenderBundle(in Input) (*Bundle, error) {
	in.applyDefaults()

	spec := xraycfg.Spec{
		Role:                   in.Server.NormalizedRole(),
		ProxyPreset:            in.Server.NormalizedProxyPreset(),
		Domain:                 in.Server.Domain,
		RealityPrivateKey:      in.Server.RealityPrivateKey,
		RealityPublicKey:       in.Server.RealityPublicKey,
		RealityServerName:      in.Server.RealityServerName,
		RealityTarget:          in.Server.RealityTarget,
		ServiceUsers:           append([]xraycfg.ServiceUser(nil), in.ServiceUsers...),
		ProxyRelay:             in.ProxyRelay,
		SecurityProfile:        in.SecurityProfile,
		ThreatDNSServers:       append([]string(nil), in.ThreatDNSServers...),
		LimitFallbackUpload:    in.RealityLimitFallbackUpload,
		LimitFallbackDownload:  in.RealityLimitFallbackDownload,
		ShortIDs:               util.ParseCSV(in.Server.RealityShortIDs),
		EnabledProfiles:        in.Server.NormalizedEnabledProfiles(),
		APIListen:              "0.0.0.0",
		APIPort:                10085,
		LogLevel:               in.XrayLogLevel,
		AccessLogPath:          accessLogPathForDiagnostics(in.ConnectionDiagnostics, in.XrayAccessLogPath),
		TLSSelfSNICertFile:     xraycfg.DefaultTLSSelfSNICertFile,
		TLSSelfSNIKeyFile:      xraycfg.DefaultTLSSelfSNIKeyFile,
		TLSSelfSNIFallbackDest: xraycfg.DefaultTLSSelfSNIFallbackDest,
		Users:                  in.Users,
	}
	configRaw := in.RenderedOverride
	if len(configRaw) == 0 {
		var err error
		configRaw, err = xraycfg.RenderServerJSON(spec)
		if err != nil {
			return nil, err
		}
	}
	tmpDir, err := os.MkdirTemp("", "ovpn-bundle-")
	if err != nil {
		return nil, err
	}
	for _, sub := range []string{
		"xray",
		"agent",
		"logs",
		"web",
		"haproxy",
		"geodata",
		"monitoring/prometheus/rules",
		"monitoring/alertmanager",
		"monitoring/telegram-bot",
		"monitoring/telegram-bot/assets",
		"monitoring/grafana/provisioning/alerting",
		"monitoring/grafana/provisioning/datasources",
		"monitoring/grafana/provisioning/dashboards",
		"monitoring/grafana/provisioning/plugins",
		"monitoring/grafana/dashboards",
		"monitoring/secrets",
	} {
		if err := os.MkdirAll(filepath.Join(tmpDir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	// config.json carries the REALITY private key and every client UUID, so it must stay
	// owner-only rather than world-readable like the other rendered runtime files.
	if err := os.WriteFile(filepath.Join(tmpDir, "xray", "config.json"), configRaw, 0o600); err != nil {
		return nil, err
	}
	composeTpl, err := AssetFS.ReadFile("templates/docker-compose.yml.tmpl")
	if in.Server.IsProxy() {
		composeTpl, err = AssetFS.ReadFile("templates/docker-compose.proxy.yml.tmpl")
	}
	if err != nil {
		return nil, err
	}
	composeTpl = injectXrayProfilePorts(composeTpl, in.Server.NormalizedEnabledProfiles())
	composeTpl = injectTLSSelfSNIWeb(composeTpl, in.Server.NormalizedEnabledProfiles())
	if err := os.WriteFile(filepath.Join(tmpDir, "docker-compose.yml"), composeTpl, 0o644); err != nil {
		return nil, err
	}
	if usesTLSSelfSNIProfile(in.Server.NormalizedEnabledProfiles()) {
		nginxCfg, err := AssetFS.ReadFile("templates/web/nginx.conf")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "web", "nginx.conf"), nginxCfg, 0o644); err != nil {
			return nil, err
		}
	}
	monitoringComposeTpl, err := AssetFS.ReadFile("templates/docker-compose.monitoring.yml.tmpl")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "docker-compose.monitoring.yml"), monitoringComposeTpl, 0o644); err != nil {
		return nil, err
	}
	envContent := fmt.Sprintf(
		"XRAY_IMAGE=%s\nOVPN_AGENT_IMAGE=%s\nOVPN_TELEGRAM_BOT_IMAGE=%s\nOVPN_WEB_IMAGE=%s\nHAPROXY_IMAGE=%s\nOVPN_AGENT_LOG_LEVEL=%s\nOVPN_AGENT_TOKEN=%s\nOVPN_AGENT_HOST_PORT=%s\nOVPN_TELEGRAM_BOT_HOST_PORT=%s\nOVPN_AGENT_CERT_FILE=%s\nOVPN_CERT_FULLCHAIN_PATH=%s\nOVPN_CONNECTION_DIAGNOSTICS=%s\nOVPN_XRAY_ACCESS_LOG=%s\nOVPN_XRAY_ACCESS_LOG_MAX_BYTES=%s\nOVPN_TLS_SELFSNI_CERT_DIR=%s\nOVPN_CAMOUFLAGE_SITE_DIR=%s\nPROMETHEUS_IMAGE=%s\nALERTMANAGER_IMAGE=%s\nGRAFANA_IMAGE=%s\nNODE_EXPORTER_IMAGE=%s\nCADVISOR_IMAGE=%s\nGRAFANA_ADMIN_USER=%s\nGRAFANA_ADMIN_PASSWORD=%s\nGRAFANA_PORT=%s\nOVPN_TELEGRAM_NOTIFY_CHAT_IDS=%s\nOVPN_TELEGRAM_OWNER_USER_ID=%s\nOVPN_TELEGRAM_CLIENTS_PDF_PATH=%s\nOVPN_TELEGRAM_CLIENTS_RU_PDF_PATH=%s\nOVPN_TELEGRAM_API_FALLBACK_IPS=%s\nOVPN_TELEGRAM_HAPROXY_URL=%s\n",
		in.XrayImage,
		in.AgentImage,
		in.TelegramBotImage,
		in.WebImage,
		in.HAProxyImage,
		in.AgentLogLevel,
		in.AgentToken,
		in.AgentHostPort,
		in.TelegramBotHostPort,
		in.AgentCertFile,
		in.AgentCertHostPath,
		in.ConnectionDiagnostics,
		in.XrayAccessLogPath,
		in.XrayAccessLogMaxBytes,
		in.TLSSelfSNICertDir,
		in.TLSSelfSNISiteDir,
		in.PrometheusImage,
		in.AlertmanagerImage,
		in.GrafanaImage,
		in.NodeExporterImage,
		in.CAdvisorImage,
		in.GrafanaAdminUser,
		in.GrafanaAdminPassword,
		in.GrafanaPort,
		in.TelegramNotifyChatIDs,
		in.TelegramOwnerUserID,
		in.TelegramClientsPDFPath,
		in.TelegramClientsRUPDFPath,
		in.TelegramAPIFallbackIPs,
		proxyTelegramHAProxyURL(in.Server),
	)
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0o600); err != nil {
		return nil, err
	}
	linkConfig := fmt.Sprintf("{\n  \"address\": %q,\n  \"server_name\": %q,\n  \"public_key\": %q,\n  \"short_id\": %q\n}\n",
		in.TelegramLinkAddress,
		in.TelegramLinkServerName,
		in.TelegramLinkPublicKey,
		in.TelegramLinkShortID,
	)
	if err := os.WriteFile(filepath.Join(tmpDir, "monitoring", "telegram-bot", "link-config.json"), []byte(linkConfig), 0o600); err != nil {
		return nil, err
	}
	if in.Server.IsProxy() {
		haproxyCfg := renderHAProxyConfig(in.BackendServers)
		if err := os.WriteFile(filepath.Join(tmpDir, "haproxy", "haproxy.cfg"), []byte(haproxyCfg), 0o644); err != nil {
			return nil, err
		}
		if err := copyFile(in.ProxyGeoSitePath, filepath.Join(tmpDir, "geodata", "geosite.dat"), 0o644); err != nil {
			return nil, err
		}
		if err := copyFile(in.ProxyGeoIPPath, filepath.Join(tmpDir, "geodata", "geoip.dat"), 0o644); err != nil {
			return nil, err
		}
	}
	for _, f := range []struct {
		asset string
		dst   string
		mode  os.FileMode
	}{
		{asset: monitoringPrometheusAsset(in.Server), dst: "monitoring/prometheus/prometheus.yml", mode: 0o644},
		{asset: monitoringAlertsAsset(in.Server), dst: "monitoring/prometheus/rules/ovpn-alerts.yml", mode: 0o644},
		{asset: "templates/grafana-datasource.yml", dst: "monitoring/grafana/provisioning/datasources/prometheus.yml", mode: 0o644},
		{asset: "templates/grafana-dashboards.yml", dst: "monitoring/grafana/provisioning/dashboards/dashboards.yml", mode: 0o644},
		{asset: "templates/grafana-dashboard-host.json", dst: "monitoring/grafana/dashboards/ovpn-host.json", mode: 0o644},
		{asset: "templates/grafana-dashboard-containers.json", dst: "monitoring/grafana/dashboards/ovpn-containers.json", mode: 0o644},
		{asset: "templates/grafana-dashboard-agent.json", dst: "monitoring/grafana/dashboards/ovpn-agent.json", mode: 0o644},
		{asset: "templates/grafana-dashboard-users.json", dst: "monitoring/grafana/dashboards/ovpn-users.json", mode: 0o644},
		{asset: "templates/grafana-dashboard-proxy.json", dst: "monitoring/grafana/dashboards/ovpn-proxy.json", mode: 0o644},
	} {
		raw, err := AssetFS.ReadFile(f.asset)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(tmpDir, f.dst), raw, f.mode); err != nil {
			return nil, err
		}
	}
	alertmanagerTpl, err := AssetFS.ReadFile("templates/alertmanager.yml.tmpl")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "monitoring/alertmanager/alertmanager.yml"), alertmanagerTpl, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "monitoring/secrets/telegram_bot_token"), []byte(""), 0o600); err != nil {
		return nil, err
	}
	adminToken := strings.TrimSpace(in.TelegramAdminToken)
	if adminToken != "" {
		adminToken += "\n"
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "monitoring/secrets/telegram_admin_token"), []byte(adminToken), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "monitoring/secrets/README.txt"), []byte("Put Telegram bot token in telegram_bot_token before enabling monitoring alerts.\nOptional: put admin token in telegram_admin_token to enable Telegram restart/heal actions.\n"), 0o644); err != nil {
		return nil, err
	}
	if src := strings.TrimSpace(in.TelegramClientsPDFSource); src != "" {
		if st, err := os.Stat(src); err == nil && !st.IsDir() {
			if err := copyFile(src, filepath.Join(tmpDir, "monitoring", "telegram-bot", "assets", "clients.pdf"), 0o644); err != nil {
				return nil, err
			}
		}
	}
	if src := strings.TrimSpace(in.TelegramClientsRUPDFSource); src != "" {
		if st, err := os.Stat(src); err == nil && !st.IsDir() {
			if err := copyFile(src, filepath.Join(tmpDir, "monitoring", "telegram-bot", "assets", "clients-ru.pdf"), 0o644); err != nil {
				return nil, err
			}
		}
	}
	if in.AgentBinaryPath != "" {
		if err := copyFile(in.AgentBinaryPath, filepath.Join(tmpDir, "agent", "ovpn-agent"), 0o755); err != nil {
			return nil, err
		}
	}
	if in.TelegramBotBinaryPath != "" {
		if err := copyFile(in.TelegramBotBinaryPath, filepath.Join(tmpDir, "monitoring", "telegram-bot", "ovpn-telegram-bot"), 0o755); err != nil {
			return nil, err
		}
	}
	return &Bundle{Dir: tmpDir, ConfigRaw: configRaw}, nil
}

func accessLogPathForDiagnostics(mode string, path string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "off") {
		return ""
	}
	return strings.TrimSpace(path)
}

func renderHAProxyConfig(backends []model.Server) string {
	var b strings.Builder
	b.WriteString(`global
  log stdout format raw local0
  maxconn 2048

defaults
  log global
  mode tcp
  timeout connect 5s
  timeout client 2m
  timeout server 2m
  option tcp-check

frontend foreign_in
  bind 0.0.0.0:15443
  default_backend foreign_backends

frontend stats
  bind 0.0.0.0:8404
  mode http
  http-request use-service prometheus-exporter if { path /metrics }
  stats enable
  stats uri /stats

backend foreign_backends
  balance roundrobin
  default-server inter 5s fall 2 rise 1 observe layer4 error-limit 10 on-error mark-down
`)
	for idx, backend := range backends {
		fmt.Fprintf(&b, "  server backend_%d %s:443 check\n", idx+1, strings.TrimSpace(backend.Host))
	}
	return b.String()
}

// CleanupBundle removes the temporary bundle directory.
func CleanupBundle(b *Bundle) {
	if b == nil || b.Dir == "" {
		return
	}
	_ = os.RemoveAll(b.Dir)
}

// createTarGz prepares create tar gz files and filesystem state.
func createTarGz(tarPath, srcDir string) error {
	f, err := os.Create(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(tw, file)
		return err
	})
}

// copyFile copies src to dst with the given file mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// firstShortID returns the first non-empty short id entry.
func firstShortID(raw string) string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			return v
		}
	}
	return ""
}

// firstNonEmpty returns the first non-empty string in order.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func monitoringPrometheusAsset(srv model.Server) string {
	if srv.IsProxy() {
		return "templates/prometheus.proxy.yml"
	}
	return "templates/prometheus.yml"
}

func monitoringAlertsAsset(srv model.Server) string {
	if srv.IsProxy() {
		return "templates/ovpn-alerts.proxy.yml"
	}
	return "templates/ovpn-alerts.yml"
}

func proxyTelegramHAProxyURL(srv model.Server) string {
	if srv.IsProxy() {
		return "http://haproxy:8404/metrics"
	}
	return ""
}
