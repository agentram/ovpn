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

// sshFromServer returns ssh from server.
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

// randomShortID returns random short id.
func randomShortID() string {
	b := make([]byte, 8)
	if _, err := io.ReadFull(cryptorand.Reader, b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// firstShortID normalizes short id and applies fallback defaults.
func firstShortID(csv string) string {
	items := util.ParseCSV(csv)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

// firstNonEmpty normalizes non empty and applies fallback defaults.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// normalizeXrayVersionTag normalizes xray version tag and applies fallback defaults.
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

// newRunner initializes runner with the required dependencies.
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

// log returns log.
func (a *App) log() *slog.Logger {
	if a != nil && a.logger != nil {
		return a.logger
	}
	return slog.Default()
}

// xrayLogLevel returns xray log level.
func (a *App) xrayLogLevel() string {
	// Keep production noise low by default; elevate to info only for explicit debug sessions.
	if a.debug || a.verbose || strings.EqualFold(a.logLevel, "debug") {
		return "info"
	}
	return "warning"
}

// agentLogLevel returns agent log level.
func (a *App) agentLogLevel() string {
	if a.debug || a.verbose || strings.EqualFold(a.logLevel, "debug") {
		return "debug"
	}
	return "info"
}

// validateComposeService executes compose service flow and returns the first error.
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

// emptyAsAll returns empty as all.
func emptyAsAll(v string) string {
	if strings.TrimSpace(v) == "" {
		return "all"
	}
	return v
}
