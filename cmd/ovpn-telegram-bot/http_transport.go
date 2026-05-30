package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

const telegramAPIHost = "api.telegram.org"

// newTelegramHTTPClient builds an HTTP client whose dialer falls back to fixed Telegram API IPs when DNS resolution fails.
func newTelegramHTTPClient(logger *slog.Logger, fallbackIPs []string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	cleanFallback := compactIPs(fallbackIPs)
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialTelegramWithFallback(ctx, logger, dialer, network, addr, cleanFallback)
	}
	return &http.Client{
		Timeout:   45 * time.Second,
		Transport: transport,
	}
}

// dialTelegramWithFallback dials addr normally and, on failure, retries against the configured fallback IPs.
func dialTelegramWithFallback(ctx context.Context, logger *slog.Logger, dialer *net.Dialer, network, addr string, fallbackIPs []string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || !strings.EqualFold(strings.TrimSpace(host), telegramAPIHost) || len(fallbackIPs) == 0 {
		return dialer.DialContext(ctx, network, addr)
	}

	conn, err := dialer.DialContext(ctx, network, addr)
	if err == nil {
		return conn, nil
	}
	firstErr := err

	for _, ip := range fallbackIPs {
		target := net.JoinHostPort(ip, port)
		conn, err = dialer.DialContext(ctx, network, target)
		if err == nil {
			if logger != nil {
				logger.Warn("telegram api fallback ip used", "target", target)
			}
			return conn, nil
		}
	}
	return nil, fmt.Errorf("telegram connect to %s failed: %w", addr, firstErr)
}

// compactIPs trims, de-duplicates, and drops empty entries from a list of IPs.
func compactIPs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, ip := range in {
		trimmed := strings.TrimSpace(ip)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
