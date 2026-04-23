package cli

import (
	"fmt"
	"strings"
	"time"

	"ovpn/internal/ssh"
)

// execRemote executes exec remote against remote hosts over SSH.
func (a *App) execRemote(runner *ssh.Runner, cfg ssh.Config, timeout time.Duration, cmd string) (ssh.Result, error) {
	ctx, cancel := ssh.TimeoutCtx(a.ctx, timeout)
	defer cancel()
	return runner.Exec(ctx, cfg, cmd)
}

// kvOr returns kv or.
func kvOr(kv map[string]string, key, fallback string) string {
	if v := strings.TrimSpace(kv[key]); v != "" {
		return v
	}
	return fallback
}

// sanitizeKey returns sanitize key.
func sanitizeKey(path string) string {
	replacer := strings.NewReplacer("/", "_", "-", "_", ".", "_")
	return replacer.Replace(strings.Trim(path, "_/"))
}

// extractOwnerMode returns extract owner mode.
func extractOwnerMode(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "OVPN_OWNER=") {
			return strings.TrimPrefix(line, "OVPN_OWNER=")
		}
	}
	return ""
}

// trimState normalizes state and applies fallback defaults.
func trimState(v string) string {
	return strings.TrimSpace(strings.Trim(v, "\""))
}

// trimmedLines normalizes trimmed lines and applies fallback defaults.
func trimmedLines(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// shellQuote returns shell quote.
func shellQuote(v string) string {
	v = strings.ReplaceAll(v, `'`, `'"'"'`)
	return "'" + v + "'"
}

// withRemoteTimeout wraps a remote shell snippet with the `timeout` utility when available.
func withRemoteTimeout(seconds int, cmd string) string {
	if seconds <= 0 {
		seconds = 10
	}
	quoted := shellQuote(cmd)
	return fmt.Sprintf("if command -v timeout >/dev/null 2>&1; then timeout %d sh -c %s; else sh -c %s; fi", seconds, quoted, quoted)
}
