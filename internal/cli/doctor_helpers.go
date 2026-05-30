package cli

import (
	"fmt"
	"strings"
	"time"

	"ovpn/internal/ssh"
)

func (a *App) execRemote(runner *ssh.Runner, cfg ssh.Config, timeout time.Duration, cmd string) (ssh.Result, error) {
	if a.remoteExecHook != nil {
		return a.remoteExecHook(cfg, timeout, cmd)
	}
	ctx, cancel := ssh.TimeoutCtx(a.ctx, timeout)
	defer cancel()
	return runner.Exec(ctx, cfg, cmd)
}

func kvOr(kv map[string]string, key, fallback string) string {
	if v := strings.TrimSpace(kv[key]); v != "" {
		return v
	}
	return fallback
}

func sanitizeKey(path string) string {
	replacer := strings.NewReplacer("/", "_", "-", "_", ".", "_")
	return replacer.Replace(strings.Trim(path, "_/"))
}

func extractOwnerMode(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "OVPN_OWNER=") {
			return strings.TrimPrefix(line, "OVPN_OWNER=")
		}
	}
	return ""
}

func trimState(v string) string {
	return strings.TrimSpace(strings.Trim(v, "\""))
}

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
