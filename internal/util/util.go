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

func HomeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func DefaultDataDir() string {
	return filepath.Join(HomeDir(), ".ovpn")
}

func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func NowUTC() time.Time {
	return time.Now().UTC()
}

func PrettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

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

func SHA256Bytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

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

func JoinCSV(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, ",")
}

func RequireNonEmpty(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

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
