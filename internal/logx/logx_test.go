package logx

import (
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	t.Parallel()

	cases := []string{"debug", "info", "warn", "warning", "error", ""}
	for _, c := range cases {
		if _, err := ParseLevel(c); err != nil {
			t.Fatalf("expected valid level for %q: %v", c, err)
		}
	}
	if _, err := ParseLevel("invalid"); err == nil {
		t.Fatal("expected invalid level error")
	}
}

func TestReplaceSensitiveAttrRedactsSensitiveKeys(t *testing.T) {
	t.Parallel()

	attr := replaceSensitiveAttr(nil, slog.String("reality_private_key", "private-value"))
	if got := attr.Value.String(); got != "[REDACTED]" {
		t.Fatalf("expected redacted value, got %q", got)
	}
}

func TestReplaceSensitiveAttrRedactsVLESSURLs(t *testing.T) {
	t.Parallel()

	raw := "client link vless://uuid@example.com:443?security=reality#alice"
	attr := replaceSensitiveAttr(nil, slog.String("message", raw))
	got := attr.Value.String()
	if strings.Contains(got, "uuid@example.com") {
		t.Fatalf("expected vless payload redacted, got %q", got)
	}
	if !strings.Contains(got, "vless://[REDACTED]") {
		t.Fatalf("expected redacted vless marker, got %q", got)
	}
}

func TestReplaceSensitiveAttrRedactsSMTPPasswordFields(t *testing.T) {
	t.Parallel()

	attr := replaceSensitiveAttr(nil, slog.String("smtp_password", "super-secret"))
	if got := attr.Value.String(); got != "[REDACTED]" {
		t.Fatalf("expected smtp_password to be redacted, got %q", got)
	}
	attr = replaceSensitiveAttr(nil, slog.String("smtp_auth_password", "super-secret"))
	if got := attr.Value.String(); got != "[REDACTED]" {
		t.Fatalf("expected smtp_auth_password to be redacted, got %q", got)
	}
}

func TestReplaceSensitiveAttrRedactsInlineKeyValueSecrets(t *testing.T) {
	t.Parallel()

	raw := "connect smtp_password=super-secret token=abc123 private_key=xyz"
	attr := replaceSensitiveAttr(nil, slog.String("message", raw))
	got := attr.Value.String()
	for _, leaked := range []string{"super-secret", "abc123", "xyz"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("expected inline secret %q to be redacted, got %q", leaked, got)
		}
	}
	if !strings.Contains(got, "smtp_password=[REDACTED]") {
		t.Fatalf("expected smtp_password key-value redaction, got %q", got)
	}
}

func TestReplaceSensitiveAttrRedactsInlineJSONSecrets(t *testing.T) {
	t.Parallel()

	raw := `payload {"smtp_password":"super-secret","token":"abc123"}`
	attr := replaceSensitiveAttr(nil, slog.String("message", raw))
	got := attr.Value.String()
	if strings.Contains(got, "super-secret") || strings.Contains(got, "abc123") {
		t.Fatalf("expected JSON secret values redacted, got %q", got)
	}
	if !strings.Contains(got, `"smtp_password":"[REDACTED]"`) {
		t.Fatalf("expected smtp_password JSON redaction, got %q", got)
	}
}
