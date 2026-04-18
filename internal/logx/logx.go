package logx

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var vlessURLPattern = regexp.MustCompile(`vless://\S+`)
var kvSecretPattern = regexp.MustCompile(`(?i)\b((?:smtp_)?password|token|secret|private_key)\s*=\s*([^\s,;]+)`)
var jsonSecretPattern = regexp.MustCompile(`(?i)"((?:smtp_)?password|token|secret|private_key)"\s*:\s*"[^"]*"`)

// ParseLevel parses level and returns normalized values.
func ParseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q (allowed: debug, info, warn, error)", raw)
	}
}

// NewTextLogger initializes text logger with the required dependencies.
func NewTextLogger(level slog.Level) *slog.Logger {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceSensitiveAttr,
	})
	return slog.New(handler)
}

// replaceSensitiveAttr applies sensitive attr and returns an error on failure.
func replaceSensitiveAttr(_ []string, attr slog.Attr) slog.Attr {
	key := strings.ToLower(strings.TrimSpace(attr.Key))
	if isSensitiveKey(key) {
		return slog.String(attr.Key, "[REDACTED]")
	}
	if attr.Value.Kind() == slog.KindString {
		v := sanitizeSensitiveString(attr.Value.String())
		if v != attr.Value.String() {
			attr.Value = slog.StringValue(v)
		}
	}
	return attr
}

// isSensitiveKey reports whether sensitive key.
func isSensitiveKey(key string) bool {
	if key == "" {
		return false
	}
	for _, marker := range []string{
		"reality_private_key",
		"private_key",
		"password",
		"smtp_password",
		"secret",
		"token",
		"authorization",
	} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

// sanitizeSensitiveString returns sanitize sensitive string.
func sanitizeSensitiveString(v string) string {
	if strings.TrimSpace(v) == "" {
		return v
	}
	v = vlessURLPattern.ReplaceAllString(v, "vless://[REDACTED]")
	v = kvSecretPattern.ReplaceAllString(v, "${1}=[REDACTED]")
	v = jsonSecretPattern.ReplaceAllString(v, "\"$1\":\"[REDACTED]\"")
	return v
}
