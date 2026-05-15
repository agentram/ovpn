package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func resolveRepoRoot(wd string) (string, error) {
	if raw := strings.TrimSpace(os.Getenv("OVPN_REPO_ROOT")); raw != "" {
		root := filepath.Clean(raw)
		if !filepath.IsAbs(root) {
			return "", errors.New("OVPN_REPO_ROOT must be an absolute path")
		}
		if !isRepoRoot(root) {
			return "", fmt.Errorf("OVPN_REPO_ROOT %q is not an ovpn source root", root)
		}
		return root, nil
	}

	if root, ok := findRepoRoot(wd); ok {
		return root, nil
	}
	if root, ok := compiledRepoRoot(); ok {
		return root, nil
	}
	if strings.TrimSpace(wd) == "" {
		return "", errors.New("cannot determine current directory")
	}
	return filepath.Clean(wd), nil
}

func findRepoRoot(start string) (string, bool) {
	if strings.TrimSpace(start) == "" {
		return "", false
	}
	dir := filepath.Clean(start)
	for {
		if isRepoRoot(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func compiledRepoRoot() (string, bool) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", false
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return root, isRepoRoot(root)
}

func isRepoRoot(root string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return false
	}
	if !hasModulePath(string(mod), "ovpn") {
		return false
	}
	for _, rel := range []string{
		filepath.Join("cmd", "ovpn-agent"),
		filepath.Join("cmd", "ovpn-telegram-bot"),
	} {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

func hasModulePath(raw, want string) bool {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1] == want
		}
	}
	return false
}
