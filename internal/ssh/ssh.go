package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var vlessCmdPattern = regexp.MustCompile(`vless://\S+`)

type Config struct {
	User            string
	Host            string
	Port            int
	IdentityFile    string
	KnownHostsFile  string
	StrictHostKey   bool
	ConnectTimeoutS int
}

// Runner executes SSH/SCP operations with optional dry-run logging.
type Runner struct {
	DryRun bool
	Logger *slog.Logger
}

type Result struct {
	Stdout string
	Stderr string
}

// Exec executes exec against remote hosts over SSH.
func (r *Runner) Exec(ctx context.Context, cfg Config, remoteCmd string) (Result, error) {
	args := sshArgs(cfg)
	args = append(args, target(cfg), remoteCmd)
	log := r.logger().With(
		"component", "ssh",
		"operation", "exec",
		"host", cfg.Host,
		"user", cfg.User,
	)
	// Remote commands may contain inline JSON payloads; sanitize before logging.
	log.Debug("running remote command", "command", sanitizeRemoteCmd(remoteCmd), "dry_run", r.DryRun)
	if r.DryRun {
		return Result{Stdout: fmt.Sprintf("DRY-RUN ssh %s", strings.Join(args, " "))}, nil
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	res := Result{Stdout: out.String(), Stderr: errBuf.String()}
	if err != nil {
		stderr := trimForError(res.Stderr)
		stdout := trimForError(res.Stdout)
		log.Error("remote command failed", "stderr", stderr, "stdout", stdout)
		return res, fmt.Errorf("ssh exec on %s failed: %w (stderr=%q)", cfg.Host, err, stderr)
	}
	log.Debug("remote command completed")
	return res, nil
}

// CopyFile combines input values to produce file.
func (r *Runner) CopyFile(ctx context.Context, cfg Config, localPath, remotePath string) error {
	args := scpArgs(cfg)
	args = append(args, localPath, fmt.Sprintf("%s:%s", target(cfg), remotePath))
	log := r.logger().With(
		"component", "ssh",
		"operation", "copy_file",
		"host", cfg.Host,
		"user", cfg.User,
		"local_path", localPath,
		"remote_path", remotePath,
	)
	log.Debug("copying file", "dry_run", r.DryRun)
	if r.DryRun {
		return nil
	}
	cmd := exec.CommandContext(ctx, "scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errOut := trimForError(stderr.String())
		log.Error("scp failed", "stderr", errOut)
		if strings.Contains(errOut, `stat local "22"`) {
			return fmt.Errorf("scp to %s failed: %w (stderr=%q); hint: this usually indicates an outdated ovpn binary with incorrect scp port handling, rebuild ovpn and retry", cfg.Host, err, errOut)
		}
		return fmt.Errorf("scp to %s failed: %w (stderr=%q)", cfg.Host, err, errOut)
	}
	log.Debug("file copied")
	return nil
}

// ExecStream executes exec stream against remote hosts over SSH.
func (r *Runner) ExecStream(ctx context.Context, cfg Config, remoteCmd string, stdout, stderr io.Writer) error {
	args := sshArgs(cfg)
	args = append(args, target(cfg), remoteCmd)
	log := r.logger().With(
		"component", "ssh",
		"operation", "exec_stream",
		"host", cfg.Host,
		"user", cfg.User,
	)
	log.Debug("starting streamed remote command", "command", sanitizeRemoteCmd(remoteCmd), "dry_run", r.DryRun)
	if r.DryRun {
		_, _ = io.WriteString(stdout, fmt.Sprintf("DRY-RUN ssh %s\n", strings.Join(args, " ")))
		return nil
	}

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		log.Error("streamed remote command failed", "error", err)
		return fmt.Errorf("ssh streamed exec on %s failed: %w", cfg.Host, err)
	}
	log.Debug("streamed remote command completed")
	return nil
}

// baseArgs returns base args.
func baseArgs(cfg Config, portFlag string) []string {
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	timeout := cfg.ConnectTimeoutS
	if timeout == 0 {
		timeout = 10
	}
	args := []string{portFlag, fmt.Sprint(port), "-o", fmt.Sprintf("ConnectTimeout=%d", timeout)}
	if cfg.IdentityFile != "" {
		args = append(args, "-i", cfg.IdentityFile)
	}
	if cfg.KnownHostsFile != "" {
		args = append(args, "-o", fmt.Sprintf("UserKnownHostsFile=%s", cfg.KnownHostsFile))
	}
	if cfg.StrictHostKey {
		args = append(args, "-o", "StrictHostKeyChecking=yes")
	} else {
		args = append(args, "-o", "StrictHostKeyChecking=no")
	}
	// BatchMode prevents hanging on interactive password prompts in automation/CI runs.
	args = append(args, "-o", "BatchMode=yes")
	return args
}

// sshArgs returns ssh args.
func sshArgs(cfg Config) []string {
	return baseArgs(cfg, "-p")
}

// scpArgs returns scp args.
func scpArgs(cfg Config) []string {
	// scp uses uppercase -P for port; lowercase -p means "preserve file attributes".
	return baseArgs(cfg, "-P")
}

// target returns target.
func target(cfg Config) string {
	if cfg.User == "" {
		return cfg.Host
	}
	return fmt.Sprintf("%s@%s", cfg.User, cfg.Host)
}

// TimeoutCtx returns timeout ctx.
func TimeoutCtx(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		d = 30 * time.Second
	}
	return context.WithTimeout(parent, d)
}

// logger returns logger.
func (r *Runner) logger() *slog.Logger {
	if r != nil && r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// trimForError normalizes for error and applies fallback defaults.
func trimForError(v string) string {
	v = strings.TrimSpace(v)
	if len(v) <= 400 {
		return v
	}
	const (
		head = 220
		tail = 160
	)
	// Keep both the beginning and end so we preserve contextual setup logs (head)
	// and the terminal failure reason that tools print last (tail).
	return v[:head] + "...(truncated)..." + v[len(v)-tail:]
}

// sanitizeRemoteCmd builds the Cobra command for sanitize remote.
func sanitizeRemoteCmd(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	// Redact heredoc payload commands because they often embed full config JSON.
	if strings.Contains(trimmed, "cat <<'JSON'") {
		return "JSON payload command (redacted)"
	}
	trimmed = vlessCmdPattern.ReplaceAllString(trimmed, "vless://[REDACTED]")
	if len(trimmed) > 500 {
		return trimmed[:500] + "...(truncated)"
	}
	return trimmed
}
