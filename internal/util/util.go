package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HomeDir returns the current user's home directory, falling back to the working directory.
func HomeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// DefaultDataDir returns the default ovpn local state directory (~/.ovpn).
func DefaultDataDir() string {
	return filepath.Join(HomeDir(), ".ovpn")
}

// EnsureDir creates path (and parents) if it does not already exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// NowUTC returns the current time in UTC.
func NowUTC() time.Time {
	return time.Now().UTC()
}

// PrettyJSON renders v as indented JSON, or an error placeholder on failure.
func PrettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// SHA256File returns the hex-encoded SHA-256 of a file's contents.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SHA256Bytes returns the hex-encoded SHA-256 of b.
func SHA256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ParseCSV splits a comma-separated string into trimmed, non-empty values.
func ParseCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// JoinCSV joins items into a comma-separated string.
func JoinCSV(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, ",")
}

// RequireNonEmpty returns an error when value is blank, naming the field.
func RequireNonEmpty(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

// CombineErrors joins the non-nil errors into a single error, or returns nil when there are none.
func CombineErrors(errs ...error) error {
	var filtered []string
	for _, err := range errs {
		if err != nil {
			filtered = append(filtered, err.Error())
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return errors.New(strings.Join(filtered, "; "))
}
