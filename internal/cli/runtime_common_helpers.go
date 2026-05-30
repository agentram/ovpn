package cli

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"

	"ovpn/internal/model"
	"ovpn/internal/ssh"
	"ovpn/internal/util"
)

// sshFromServer derives an ssh.Config from a server record.
func sshFromServer(srv model.Server) ssh.Config {
	return ssh.Config{
		User:            srv.SSHUser,
		Host:            srv.Host,
		Port:            srv.SSHPort,
		IdentityFile:    srv.SSHIdentityFile,
		KnownHostsFile:  srv.SSHKnownHostsFile,
		StrictHostKey:   srv.SSHStrictHostKey,
		ConnectTimeoutS: 15,
	}
}

// generateX25519Pair returns generate x 25519 pair.
func generateX25519Pair() (string, string, error) {
	priv := make([]byte, 32)
	if _, err := io.ReadFull(cryptorand.Reader, priv); err != nil {
		return "", "", err
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64
	var pub [32]byte
	curve25519.ScalarBaseMult(&pub, (*[32]byte)(priv))
	// Xray REALITY keys are expected in raw URL-safe base64 form.
	return base64.RawURLEncoding.EncodeToString(priv), base64.RawURLEncoding.EncodeToString(pub[:]), nil
}

// randomShortID generates a random REALITY shortId hex string.
func randomShortID() string {
	b := make([]byte, 8)
	if _, err := io.ReadFull(cryptorand.Reader, b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// firstShortID returns the first shortId from a comma-separated list.
func firstShortID(csv string) string {
	items := util.ParseCSV(csv)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

// firstNonEmpty returns the first non-empty, trimmed value.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// normalizeXrayVersionTag strips a leading 'v' so the image tag matches the xray-core release naming.
func normalizeXrayVersionTag(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 1 && strings.HasPrefix(v, "v") {
		next := v[1]
		if next >= '0' && next <= '9' {
			return v[1:]
		}
	}
	return v
}

// newRunner builds an SSH runner tagged with operation and honoring the App's dry-run setting.
func (a *App) newRunner(operation string) *ssh.Runner {
	log := a.log()
	if log != nil {
		log = log.With("operation", operation)
	}
	return &ssh.Runner{
		DryRun: a.dryRun,
		Logger: log,
	}
}

// log returns the App logger, falling back to the default logger.
func (a *App) log() *slog.Logger {
	if a != nil && a.logger != nil {
		return a.logger
	}
	return slog.Default()
}

// xrayLogLevel returns the configured Xray log level, defaulting to a safe value.
func (a *App) xrayLogLevel() string {
	// Keep production noise low by default; elevate to info only for explicit debug sessions.
	if a.debug || a.verbose || strings.EqualFold(a.logLevel, "debug") {
		return "info"
	}
	return "warning"
}

// agentLogLevel returns the configured ovpn-agent log level, defaulting to info.
func (a *App) agentLogLevel() string {
	if a.debug || a.verbose || strings.EqualFold(a.logLevel, "debug") {
		return "debug"
	}
	return "info"
}

// validateComposeService normalizes and validates a restartable compose service name.
func validateComposeService(svc string) (string, error) {
	if strings.TrimSpace(svc) == "" {
		return "", nil
	}
	switch svc {
	case "xray", "haproxy", "ovpn-agent", "prometheus", "alertmanager", "grafana", "node-exporter", "cadvisor", "ovpn-telegram-bot":
		return " " + svc, nil
	default:
		return "", fmt.Errorf("unsupported --service %q (allowed: xray, haproxy, ovpn-agent, prometheus, alertmanager, grafana, node-exporter, cadvisor, ovpn-telegram-bot)", svc)
	}
}

// emptyAsAll returns "all" when v is empty, for display of unscoped operations.
func emptyAsAll(v string) string {
	if strings.TrimSpace(v) == "" {
		return "all"
	}
	return v
}
