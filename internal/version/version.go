package version

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var (
	pinnedVersion = "dev"
	semverRE      = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	changelogRE   = regexp.MustCompile(`(?m)^## ([0-9]+\.[0-9]+\.[0-9]+)$`)
)

// Current returns the pinned application version.
func Current() string {
	if v := strings.TrimSpace(pinnedVersion); v != "" && v != "dev" {
		return v
	}
	if v, err := ReadVersionFile(""); err == nil {
		return v
	}
	return "dev"
}

// Validate validates plain semantic versions without a v prefix.
func Validate(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return errors.New("version is empty")
	}
	if !semverRE.MatchString(v) {
		return fmt.Errorf("invalid version %q: expected plain semver like 1.1.0", v)
	}
	return nil
}

// ReadVersionFile returns the normalized VERSION value from the repository root.
func ReadVersionFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		root, err := repoRoot()
		if err != nil {
			return "", err
		}
		path = filepath.Join(root, "VERSION")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(raw))
	if err := Validate(v); err != nil {
		return "", err
	}
	return v, nil
}

// TopChangelogVersion extracts the first plain-semver release heading from CHANGELOG.md.
func TopChangelogVersion(raw string) (string, error) {
	m := changelogRE.FindStringSubmatch(raw)
	if len(m) != 2 {
		return "", errors.New("top changelog version not found")
	}
	if err := Validate(m[1]); err != nil {
		return "", err
	}
	return m[1], nil
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}
