package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/telegrambot"
)

// loadConfig returns load config.
func loadConfig() (config, error) {
	cfg := config{}
	ownerUserIDRaw := envOr("OVPN_TELEGRAM_OWNER_USER_ID", "")
	telegramAPIFallbackIPsRaw := envOr("OVPN_TELEGRAM_API_FALLBACK_IPS", "149.154.167.220")

	flag.StringVar(&cfg.listenAddr, "listen", envOr("OVPN_TELEGRAM_BOT_LISTEN", ":8080"), "HTTP listen address")
	flag.StringVar(&cfg.agentURL, "agent-url", envOr("OVPN_TELEGRAM_AGENT_URL", "http://ovpn-agent:9090"), "ovpn-agent base URL")
	flag.StringVar(&cfg.prometheusURL, "prometheus-url", envOr("OVPN_TELEGRAM_PROMETHEUS_URL", "http://prometheus:9090"), "Prometheus base URL")
	flag.StringVar(&cfg.alertmanagerURL, "alertmanager-url", envOr("OVPN_TELEGRAM_ALERTMANAGER_URL", "http://alertmanager:9093"), "Alertmanager base URL")
	flag.StringVar(&cfg.grafanaURL, "grafana-url", envOr("OVPN_TELEGRAM_GRAFANA_URL", "http://grafana:3000"), "Grafana base URL")
	flag.StringVar(&cfg.nodeExporterURL, "node-exporter-url", envOr("OVPN_TELEGRAM_NODE_EXPORTER_URL", "http://node-exporter:9100"), "node_exporter base URL")
	flag.StringVar(&cfg.cadvisorURL, "cadvisor-url", envOr("OVPN_TELEGRAM_CADVISOR_URL", "http://cadvisor:8080"), "cAdvisor base URL")
	flag.StringVar(&cfg.haproxyURL, "haproxy-url", envOr("OVPN_TELEGRAM_HAPROXY_URL", ""), "Optional HAProxy metrics URL for proxy nodes")
	flag.StringVar(&cfg.selfURL, "self-url", envOr("OVPN_TELEGRAM_SELF_URL", "http://127.0.0.1:8080/health"), "ovpn-telegram-bot self health URL")
	flag.StringVar(&cfg.tokenFile, "telegram-token-file", envOr("OVPN_TELEGRAM_BOT_TOKEN_FILE", "/run/secrets/telegram_bot_token"), "Telegram token file path")
	flag.StringVar(&cfg.adminTokenFile, "admin-token-file", envOr("OVPN_TELEGRAM_ADMIN_TOKEN_FILE", "/run/secrets/telegram_admin_token"), "Optional admin token file path for mutating owner actions")
	flag.StringVar(&cfg.notifyChatIDs, "notify-chat-ids", envOr("OVPN_TELEGRAM_NOTIFY_CHAT_IDS", ""), "Comma-separated Telegram chat IDs for outbound alerts/events")
	flag.StringVar(&ownerUserIDRaw, "owner-user-id", ownerUserIDRaw, "Owner Telegram user ID for sensitive read-only actions")
	flag.StringVar(&cfg.clientsPDFPath, "clients-pdf-path", envOr("OVPN_TELEGRAM_CLIENTS_PDF_PATH", "/opt/ovpn-telegram-bot/assets/clients.pdf"), "Path to clients PDF guide")
	flag.StringVar(&cfg.linkConfigFile, "link-config-file", "/opt/ovpn-telegram-bot/link-config.json", "Path to generated link config file")
	flag.StringVar(&telegramAPIFallbackIPsRaw, "telegram-api-fallback-ips", telegramAPIFallbackIPsRaw, "Comma-separated fallback IPs for api.telegram.org")
	flag.StringVar(&cfg.logLevel, "log-level", strings.TrimSpace(os.Getenv("OVPN_TELEGRAM_BOT_LOG_LEVEL")), "Log level: debug|info|warn|error")
	pollRaw := envOr("OVPN_TELEGRAM_POLL_INTERVAL", "3s")
	flag.Parse()

	if strings.TrimSpace(cfg.logLevel) == "" {
		cfg.logLevel = "info"
	}
	interval, err := time.ParseDuration(strings.TrimSpace(pollRaw))
	if err != nil {
		return cfg, fmt.Errorf("invalid OVPN_TELEGRAM_POLL_INTERVAL: %w", err)
	}
	if interval < time.Second {
		return cfg, errors.New("poll interval must be >= 1s")
	}
	cfg.pollInterval = interval
	ownerUserID, err := parseOwnerUserID(ownerUserIDRaw)
	if err != nil {
		return cfg, err
	}
	cfg.ownerUserID = inferOwnerUserID(ownerUserID, cfg.notifyChatIDs)
	cfg.telegramAPIFallbackIPs, err = parseTelegramAPIFallbackIPs(telegramAPIFallbackIPsRaw)
	if err != nil {
		return cfg, err
	}
	if err := cfg.loadLinkConfig(); err != nil {
		cfg.linkConfigErr = err.Error()
	}
	return cfg, nil
}

// readSecretFile returns secret file for callers.
func readSecretFile(path string) (string, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return "", errors.New("token file path is empty")
	}
	// #nosec G304 -- controlled by deployment config and mounted secret file.
	b, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", errors.New("token file is empty")
	}
	return token, nil
}

// readOptionalSecretFile reads optional secret file and returns empty value when file is missing/empty.
func readOptionalSecretFile(path string) (string, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return "", nil
	}
	// #nosec G304 -- controlled by deployment config and mounted secret file.
	b, err := os.ReadFile(cleanPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// envOr normalizes env or and applies fallback defaults.
func envOr(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

// parseOwnerUserID parses owner user id and returns normalized values.
func parseOwnerUserID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	ids, err := telegrambot.ParseIDSliceCSV(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid owner user id: %w", err)
	}
	if len(ids) != 1 {
		return 0, errors.New("invalid owner user id: provide exactly one numeric telegram user id")
	}
	return ids[0], nil
}

// inferOwnerUserID returns explicit owner id or fallback from first notify chat id.
func inferOwnerUserID(ownerUserID int64, notifyChatIDsRaw string) int64 {
	if ownerUserID > 0 {
		return ownerUserID
	}
	ids, err := parseTelegramIDsOrdered(strings.TrimSpace(notifyChatIDsRaw))
	if err != nil || len(ids) == 0 {
		return 0
	}
	return ids[0]
}

// parseTelegramIDsOrdered parses CSV Telegram ids, preserving first-seen order.
func parseTelegramIDsOrdered(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		id, err := strconv.ParseInt(item, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", item, err)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// loadLinkConfig loads generated link parameters used by owner-only user link rendering.
func (c *config) loadLinkConfig() error {
	path := strings.TrimSpace(c.linkConfigFile)
	if path == "" {
		return errors.New("link config file path is empty")
	}
	type linkConfig struct {
		Address    string `json:"address"`
		ServerName string `json:"server_name"`
		PublicKey  string `json:"public_key"`
		ShortID    string `json:"short_id"`
	}
	// #nosec G304 -- path is fixed by deployment and mounted from runtime bundle.
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read link config file %s: %w", path, err)
	}
	var cfg linkConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("parse link config file %s: %w", path, err)
	}
	c.linkAddress = strings.TrimSpace(cfg.Address)
	c.linkServerName = strings.TrimSpace(cfg.ServerName)
	c.linkPublicKey = strings.TrimSpace(cfg.PublicKey)
	c.linkShortID = strings.TrimSpace(cfg.ShortID)
	if c.linkAddress == "" || c.linkServerName == "" || c.linkPublicKey == "" || c.linkShortID == "" {
		return fmt.Errorf("link config file %s is incomplete", path)
	}
	return nil
}

// parseTelegramAPIFallbackIPs parses telegram api fallback ips and returns normalized values.
func parseTelegramAPIFallbackIPs(raw string) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		ip := strings.TrimSpace(p)
		if ip == "" {
			continue
		}
		if parsed := net.ParseIP(ip); parsed == nil {
			return nil, fmt.Errorf("invalid OVPN_TELEGRAM_API_FALLBACK_IPS value: %q is not an IP", ip)
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	return out, nil
}

// defaultText normalizes text and applies fallback defaults.
func defaultText(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}
