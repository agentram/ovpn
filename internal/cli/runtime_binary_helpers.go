package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ensureAgentBinary executes agent binary flow and returns the first error.
func (a *App) ensureAgentBinary() (string, error) {
	if p := strings.TrimSpace(os.Getenv("OVPN_AGENT_BINARY")); p != "" {
		cleanPath := filepath.Clean(p)
		if !filepath.IsAbs(cleanPath) {
			return "", errors.New("OVPN_AGENT_BINARY must be an absolute path")
		}
		// #nosec G304,G703 -- operator-provided local override path, validated as absolute and checked below.
		info, err := os.Stat(cleanPath)
		if err != nil {
			return "", fmt.Errorf("OVPN_AGENT_BINARY points to missing file: %w", err)
		}
		if info.IsDir() {
			return "", errors.New("OVPN_AGENT_BINARY must point to a file, not a directory")
		}
		a.log().Debug("using external ovpn-agent binary", "path", cleanPath)
		return cleanPath, nil
	}
	goos := "linux"
	goarch, err := normalizedAgentGOARCH(strings.TrimSpace(os.Getenv("OVPN_AGENT_GOARCH")))
	if err != nil {
		return "", err
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("ovpn-agent-%s-%s", goos, goarch))
	a.log().Debug("building ovpn-agent binary", "output", out, "goos", goos, "goarch", goarch)
	cmd := exec.Command("go", "build", "-o", out, "./cmd/ovpn-agent")
	cmd.Dir = a.repoRoot
	cmd.Env = append(os.Environ(),
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED=0",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build ovpn-agent: %w: %s", err, string(output))
	}
	a.log().Debug("ovpn-agent binary built", "output", out)
	return out, nil
}

// ensureTelegramBotBinary executes telegram bot binary flow and returns the first error.
func (a *App) ensureTelegramBotBinary() (string, error) {
	if p := strings.TrimSpace(os.Getenv("OVPN_TELEGRAM_BOT_BINARY")); p != "" {
		cleanPath := filepath.Clean(p)
		if !filepath.IsAbs(cleanPath) {
			return "", errors.New("OVPN_TELEGRAM_BOT_BINARY must be an absolute path")
		}
		// #nosec G304,G703 -- operator-provided local override path, validated as absolute and checked below.
		info, err := os.Stat(cleanPath)
		if err != nil {
			return "", fmt.Errorf("OVPN_TELEGRAM_BOT_BINARY points to missing file: %w", err)
		}
		if info.IsDir() {
			return "", errors.New("OVPN_TELEGRAM_BOT_BINARY must point to a file, not a directory")
		}
		a.log().Debug("using external ovpn-telegram-bot binary", "path", cleanPath)
		return cleanPath, nil
	}
	goos := "linux"
	goarch, err := normalizedAgentGOARCH(strings.TrimSpace(os.Getenv("OVPN_TELEGRAM_BOT_GOARCH")))
	if err != nil {
		return "", err
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("ovpn-telegram-bot-%s-%s", goos, goarch))
	a.log().Debug("building ovpn-telegram-bot binary", "output", out, "goos", goos, "goarch", goarch)
	cmd := exec.Command("go", "build", "-o", out, "./cmd/ovpn-telegram-bot")
	cmd.Dir = a.repoRoot
	cmd.Env = append(os.Environ(),
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED=0",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build ovpn-telegram-bot: %w: %s", err, string(output))
	}
	a.log().Debug("ovpn-telegram-bot binary built", "output", out)
	return out, nil
}

// normalizedAgentGOARCH normalizes normalized agent goarch and applies fallback defaults.
func normalizedAgentGOARCH(raw string) (string, error) {
	allowed := map[string]string{
		"":         "amd64",
		"386":      "386",
		"amd64":    "amd64",
		"arm":      "arm",
		"arm64":    "arm64",
		"loong64":  "loong64",
		"mips":     "mips",
		"mips64":   "mips64",
		"mips64le": "mips64le",
		"mipsle":   "mipsle",
		"ppc64":    "ppc64",
		"ppc64le":  "ppc64le",
		"riscv64":  "riscv64",
		"s390x":    "s390x",
		"wasm":     "wasm",
	}
	arch, ok := allowed[raw]
	if !ok {
		return "", fmt.Errorf("unsupported OVPN_AGENT_GOARCH: %q", raw)
	}
	return arch, nil
}
