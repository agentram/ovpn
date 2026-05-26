package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ovpn/internal/runtimeassets"
	"ovpn/internal/version"
)

// ensureAgentBinary executes agent binary flow and returns the first error.
func (a *App) ensureAgentBinary() (string, error) {
	return a.ensureRuntimeBinary(runtimeBinarySpec{
		Name:       "ovpn-agent",
		Override:   "OVPN_AGENT_BINARY",
		ArchEnv:    "OVPN_AGENT_GOARCH",
		SourcePath: "./cmd/ovpn-agent",
	})
}

// ensureTelegramBotBinary executes telegram bot binary flow and returns the first error.
func (a *App) ensureTelegramBotBinary() (string, error) {
	return a.ensureRuntimeBinary(runtimeBinarySpec{
		Name:       "ovpn-telegram-bot",
		Override:   "OVPN_TELEGRAM_BOT_BINARY",
		ArchEnv:    "OVPN_TELEGRAM_BOT_GOARCH",
		SourcePath: "./cmd/ovpn-telegram-bot",
	})
}

type runtimeBinarySpec struct {
	Name       string
	Override   string
	ArchEnv    string
	SourcePath string
}

func (a *App) ensureRuntimeBinary(spec runtimeBinarySpec) (string, error) {
	if p := strings.TrimSpace(os.Getenv(spec.Override)); p != "" {
		return a.validateRuntimeBinaryOverride(spec, p)
	}
	goarch, err := normalizedRuntimeGOARCH(strings.TrimSpace(os.Getenv(spec.ArchEnv)), spec.ArchEnv)
	if err != nil {
		return "", err
	}
	if path, err := a.materializeEmbeddedRuntimeBinary(spec, goarch); err == nil {
		return path, nil
	} else if !errors.Is(err, runtimeassets.ErrNotEmbedded) && !errors.Is(err, runtimeassets.ErrUnsupported) {
		return "", err
	} else {
		a.log().Debug("embedded runtime binary unavailable, falling back to source build", "binary", spec.Name, "goarch", goarch, "error", err)
	}
	if isRepoRoot(a.repoRoot) {
		return a.buildRuntimeBinaryFromSource(spec, goarch)
	}
	return "", fmt.Errorf("resolve %s runtime binary for linux/%s: no embedded asset is available and no source checkout is available; use an ovpn release built with runtime assets, set %s=/absolute/path/to/%s, or run from the ovpn checkout with Go installed (OVPN_REPO_ROOT=/absolute/path/to/ovpn)", spec.Name, goarch, spec.Override, spec.Name)
}

func (a *App) validateRuntimeBinaryOverride(spec runtimeBinarySpec, p string) (string, error) {
	cleanPath := filepath.Clean(p)
	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("%s must be an absolute path", spec.Override)
	}
	// #nosec G304,G703 -- operator-provided local override path, validated as absolute and checked below.
	info, err := os.Stat(cleanPath)
	if err != nil {
		return "", fmt.Errorf("%s points to missing file: %w", spec.Override, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s must point to a file, not a directory", spec.Override)
	}
	a.log().Debug("using external runtime binary", "binary", spec.Name, "path", cleanPath)
	return cleanPath, nil
}

func (a *App) materializeEmbeddedRuntimeBinary(spec runtimeBinarySpec, goarch string) (string, error) {
	src, asset, err := runtimeassets.Open(spec.Name, goarch)
	if err != nil {
		return "", err
	}
	defer src.Close()

	dir := runtimeBinaryCacheDir(goarch)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create runtime binary cache: %w", err)
	}
	out := filepath.Join(dir, spec.Name)
	tmp, err := os.CreateTemp(dir, "."+spec.Name+"-*")
	if err != nil {
		return "", fmt.Errorf("create cached %s: %w", spec.Name, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write cached %s: %w", spec.Name, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("chmod cached %s: %w", spec.Name, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close cached %s: %w", spec.Name, err)
	}
	if err := os.Rename(tmpPath, out); err != nil {
		return "", fmt.Errorf("install cached %s: %w", spec.Name, err)
	}
	cleanup = false
	a.log().Debug("materialized embedded runtime binary", "binary", spec.Name, "path", out, "asset", asset.Path, "goarch", goarch)
	return out, nil
}

func runtimeBinaryCacheDir(goarch string) string {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "ovpn", "runtime-assets", version.Current(), runtimeassets.LinuxGOOS+"_"+goarch)
}

func (a *App) buildRuntimeBinaryFromSource(spec runtimeBinarySpec, goarch string) (string, error) {
	goos := runtimeassets.LinuxGOOS
	out := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s-%s", spec.Name, goos, goarch))
	a.log().Debug("building runtime binary from source", "binary", spec.Name, "output", out, "goos", goos, "goarch", goarch)
	cmd := exec.Command("go", "build", "-o", out, spec.SourcePath)
	cmd.Dir = a.repoRoot
	cmd.Env = append(os.Environ(),
		"GOOS="+goos,
		"GOARCH="+goarch,
		"CGO_ENABLED=0",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return "", fmt.Errorf("build %s from source: Go toolchain is required for source fallback; use an ovpn release built with runtime assets or set %s=/absolute/path/to/%s", spec.Name, spec.Override, spec.Name)
		}
		return "", fmt.Errorf("build %s from source: %w: %s", spec.Name, err, string(output))
	}
	a.log().Debug("runtime binary built from source", "binary", spec.Name, "output", out)
	return out, nil
}

func normalizedRuntimeGOARCH(raw string, envName string) (string, error) {
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
		return "", fmt.Errorf("unsupported %s: %q", envName, raw)
	}
	return arch, nil
}
