package cli

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// envOr returns the value of key (including any _FILE override), or fallback when unset.
func envOr(key, fallback string) string {
	if v, ok := envWithFileOverride(key); ok {
		return v
	}
	return fallback
}

// envWithFileOverride resolves key from a <key>_FILE path first, then the plain env var.
func envWithFileOverride(key string) (string, bool) {
	fileKey := key + "_FILE"
	if filePath := strings.TrimSpace(os.Getenv(fileKey)); filePath != "" {
		cleanPath := filepath.Clean(filePath)
		// #nosec G304 -- operator-controlled local path override for secret file loading.
		b, err := os.ReadFile(cleanPath)
		if err != nil {
			slog.Warn("failed to read *_FILE override; falling back", "key", fileKey, "path", cleanPath, "error", err)
		} else if v := strings.TrimSpace(string(b)); v != "" {
			return v, true
		}
	}
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v, true
	}
	return "", false
}

// setEnvOverrides applies env var overrides and returns a function that restores the previous values.
func setEnvOverrides(overrides map[string]string) (restore func()) {
	type previous struct {
		value string
		ok    bool
	}
	prev := make(map[string]previous, len(overrides))
	for key, value := range overrides {
		old, ok := os.LookupEnv(key)
		prev[key] = previous{value: old, ok: ok}
		_ = os.Setenv(key, value)
	}
	return func() {
		for key, old := range prev {
			if old.ok {
				_ = os.Setenv(key, old.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}
}
