package logx

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var vlessURLPattern = regexp.MustCompile(`vless://\S+`)
var kvSecretPattern = regexp.MustCompile(`(?i)\b([a-z0-9_]*(?:password|token|secret|private_?key|uuid|authorization|decryption|encryption))\s*=\s*([^\s,;]+)`)
var jsonSecretPattern = regexp.MustCompile(`(?i)"([a-z0-9_]*(?:password|token|secret|private_?key|uuid|authorization|decryption|encryption))"\s*:\s*"[^"]*"`)

// ParseLevel maps debug|info|warn|error to an slog.Level, defaulting to info when empty.
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

// NewTextLogger builds a text slog.Logger that redacts sensitive attribute values.
func NewTextLogger(level slog.Level) *slog.Logger {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceSensitiveAttr,
	})
	return slog.New(handler)
}

// replaceSensitiveAttr redacts attribute values that look like secrets before they are logged.
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

// sanitizeSensitiveString masks secret-looking substrings (such as VLESS links) within a value.
func sanitizeSensitiveString(v string) string {
	if strings.TrimSpace(v) == "" {
		return v
	}
	v = vlessURLPattern.ReplaceAllString(v, "vless://[REDACTED]")
	v = kvSecretPattern.ReplaceAllString(v, "${1}=[REDACTED]")
	v = jsonSecretPattern.ReplaceAllString(v, "\"$1\":\"[REDACTED]\"")
	return v
}

// Sanitize removes credentials from human diagnostic output.
var diagnosticUUID = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
var diagnosticEmail = regexp.MustCompile(`[^\s"<>@,:{}]+@[^\s"<>@,:{}]+`)
var diagnosticIPv6 = regexp.MustCompile(`\[[0-9a-fA-F:]+\]`)
var diagnosticIP = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)

func Sanitize(value string) string {
	value = sanitizeSensitiveString(value)
	value = diagnosticUUID.ReplaceAllString(value, "[identity]")
	value = diagnosticEmail.ReplaceAllString(value, "[identity]")
	value = diagnosticIPv6.ReplaceAllString(value, "[address]")
	return diagnosticIP.ReplaceAllString(value, "[address]")
}
