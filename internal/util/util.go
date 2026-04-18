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

// HomeDir returns home dir.
func HomeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// DefaultDataDir normalizes data dir and applies fallback defaults.
func DefaultDataDir() string {
	return filepath.Join(HomeDir(), ".ovpn")
}

// EnsureDir executes dir flow and returns the first error.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// NowUTC returns now utc.
func NowUTC() time.Time {
	return time.Now().UTC()
}

// PrettyJSON returns pretty json.
func PrettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// SHA256File returns sha 256 file.
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

// SHA256Bytes returns sha 256 bytes.
func SHA256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ParseCSV parses csv and returns normalized values.
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

// JoinCSV returns join csv.
func JoinCSV(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, ",")
}

// RequireNonEmpty returns require non empty.
func RequireNonEmpty(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

// CombineErrors combines input values to produce errors.
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
