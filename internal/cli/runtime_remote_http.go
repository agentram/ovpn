package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/deploy"
	"ovpn/internal/model"
	"ovpn/internal/telegrambot"
	"ovpn/internal/util"
)

// fetchRemoteAgent calls an ovpn-agent endpoint on a server and returns the response body.
func (a *App) fetchRemoteAgent(srv model.Server, method, url string, payload any) ([]byte, error) {
	return a.fetchRemoteHTTP(srv, method, url, payload)
}

// fetchRemoteHTTP runs curl on the host over SSH to reach the loopback-bound agent, returning the response body or an error for non-2xx status.
func (a *App) fetchRemoteHTTP(srv model.Server, method, url string, payload any) ([]byte, error) {
	if a.remoteHTTPHook != nil {
		return a.remoteHTTPHook(srv, method, url, payload)
	}
	runner := a.newRunner("agent_http")
	cfg := sshFromServer(srv)
	cmd := buildAgentHTTPCommand(method, url, payload)
	a.log().Debug("calling remote agent endpoint", "server", srv.Name, "host", srv.Host, "method", method, "url", url, "has_payload", payload != nil)
	res, err := runner.Exec(a.ctx, cfg, cmd)
	if err != nil {
		return nil, fmt.Errorf("remote http call %s %s on %s failed: %w", method, url, srv.Host, err)
	}
	body, status, err := parseRemoteHTTPResponse(res.Stdout)
	if err != nil {
		return nil, fmt.Errorf("remote http call %s %s on %s returned invalid response: %w", method, url, srv.Host, err)
	}
	if status < 200 || status >= 300 {
		msg := strings.TrimSpace(body)
		if msg == "" {
			msg = httpStatusText(status)
		}
		if status == http.StatusUnauthorized {
			// A 401 is always an agent auth-token mismatch, not a transient runtime error. Spell out
			// the likely cause so the operator fixes the token/version skew instead of assuming a flake.
			return nil, fmt.Errorf("remote http call %s %s on %s returned 401: %s; the ovpn-agent rejected the auth token — ensure this CLI and the deployed agent are the same ovpn version and run `ovpn deploy %s` to re-sync OVPN_AGENT_TOKEN", method, url, srv.Host, msg, srv.Name)
		}
		return nil, fmt.Errorf("remote http call %s %s on %s returned %d: %s", method, url, srv.Host, status, msg)
	}
	return []byte(strings.TrimSpace(body)), nil
}

// buildAgentHTTPCommand builds the remote shell command that calls an ovpn-agent endpoint.
//
// The agent bearer token is read from the host-side .env that started the agent container, so the
// operator never has to keep it in local state and both sides always agree on the value. An empty
// token (older deploys, or auth disabled) sends a harmless empty bearer. JSON payloads are streamed
// via stdin to avoid shell-escaping bugs and to keep large bodies out of logs.
func buildAgentHTTPCommand(method, url string, payload any) string {
	tokenPrelude := fmt.Sprintf("OVPN_AGENT_TOKEN=$(sed -n 's/^OVPN_AGENT_TOKEN=//p' %s/.env 2>/dev/null | head -n1); ", deploy.RemoteDir)
	if payload == nil {
		return tokenPrelude + fmt.Sprintf("curl --max-time 10 -sS -H \"Authorization: Bearer ${OVPN_AGENT_TOKEN}\" -w '\\nOVPN_HTTP_STATUS:%%{http_code}' -X %s '%s'", method, url)
	}
	b, _ := json.Marshal(payload)
	return tokenPrelude + fmt.Sprintf("cat <<'JSON' | curl --max-time 10 -sS -H \"Authorization: Bearer ${OVPN_AGENT_TOKEN}\" -w '\\nOVPN_HTTP_STATUS:%%{http_code}' -X %s -H 'Content-Type: application/json' -d @- '%s'\n%s\nJSON", method, url, string(b))
}

func parseRemoteHTTPResponse(raw string) (string, int, error) {
	const marker = "\nOVPN_HTTP_STATUS:"
	idx := strings.LastIndex(raw, marker)
	if idx < 0 {
		return "", 0, fmt.Errorf("missing HTTP status marker")
	}
	statusText := strings.TrimSpace(raw[idx+len(marker):])
	status, err := strconv.Atoi(statusText)
	if err != nil {
		return "", 0, fmt.Errorf("invalid HTTP status %q", statusText)
	}
	if status <= 0 {
		return "", 0, fmt.Errorf("invalid HTTP status %d", status)
	}
	return raw[:idx], status, nil
}

func httpStatusText(status int) string {
	if text := strings.TrimSpace(http.StatusText(status)); text != "" {
		return text
	}
	return "HTTP error"
}

// agentHostPort returns the validated host port that maps to the agent, defaulting to 19000.
func (a *App) agentHostPort() string {
	raw := strings.TrimSpace(envOr("OVPN_AGENT_HOST_PORT", "19000"))
	p, err := strconv.Atoi(raw)
	if err != nil || p <= 0 || p > 65535 {
		a.log().Warn("invalid OVPN_AGENT_HOST_PORT, falling back to default 19000", "value", raw)
		return "19000"
	}
	return strconv.Itoa(p)
}

// agentBaseURL returns the loopback base URL of the agent on the host.
func (a *App) agentBaseURL() string {
	return "http://127.0.0.1:" + a.agentHostPort()
}

// agentURL joins path onto the agent base URL.
func (a *App) agentURL(path string) string {
	return strings.TrimRight(a.agentBaseURL(), "/") + "/" + strings.TrimLeft(path, "/")
}

// telegramBotHostPort returns the validated host port that maps to the Telegram bot, defaulting to 19001.
func (a *App) telegramBotHostPort() string {
	raw := strings.TrimSpace(envOr("OVPN_TELEGRAM_BOT_HOST_PORT", "19001"))
	p, err := strconv.Atoi(raw)
	if err != nil || p <= 0 || p > 65535 {
		a.log().Warn("invalid OVPN_TELEGRAM_BOT_HOST_PORT, falling back to default 19001", "value", raw)
		return "19001"
	}
	return strconv.Itoa(p)
}

// telegramNotifyURL returns the bot notify endpoint URL on the host.
func (a *App) telegramNotifyURL() string {
	return envOr("OVPN_TELEGRAM_NOTIFY_URL", "http://127.0.0.1:"+a.telegramBotHostPort()+"/notify")
}

// sendTelegramNotifyEvent makes a best-effort delivery of a notify event when Telegram targets are configured.
func (a *App) sendTelegramNotifyEvent(srv model.Server, ev telegrambot.NotifyEvent) {
	if a.dryRun {
		return
	}
	// Telegram notifications are optional. Try best-effort delivery when either owner
	// or notify chat IDs are configured for this CLI process.
	if len(util.ParseCSV(envOr("OVPN_TELEGRAM_NOTIFY_CHAT_IDS", ""))) == 0 && strings.TrimSpace(envOr("OVPN_TELEGRAM_OWNER_USER_ID", "")) == "" {
		return
	}
	if strings.TrimSpace(ev.Event) == "" {
		return
	}
	if strings.TrimSpace(ev.Source) == "" {
		ev.Source = "ovpn-cli"
	}
	if strings.TrimSpace(ev.Status) == "" {
		ev.Status = "info"
	}
	if strings.TrimSpace(ev.Severity) == "" {
		ev.Severity = "info"
	}
	if err := a.postRemoteNotifyBestEffort(srv, a.telegramNotifyURL(), ev); err != nil {
		a.log().Debug("telegram notify delivery skipped", "server", srv.Name, "event", ev.Event, "error", err)
	}
}

// postRemoteNotifyBestEffort POSTs a notify payload on the host via a temp file, ignoring delivery failures.
func (a *App) postRemoteNotifyBestEffort(srv model.Server, endpoint string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	remoteCmd := fmt.Sprintf(
		"set -e; payload_file=/tmp/ovpn-notify.json; printf '%%s' %q | base64 -d > \"$payload_file\"; curl -fsS -X POST -H 'Content-Type: application/json' --data @\"$payload_file\" %q >/dev/null 2>&1 || true; rm -f \"$payload_file\"",
		encoded,
		endpoint,
	)
	runner := a.newRunner("notify_http")
	_, err = runner.Exec(a.ctx, sshFromServer(srv), remoteCmd)
	return err
}

// uploadTelegramBotToken copies the Telegram bot token to the host and installs it with restrictive permissions.
func (a *App) uploadTelegramBotToken(srv model.Server, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("telegram bot token is required")
	}
	tmp, err := os.CreateTemp("", "ovpn-telegram-token-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	if err := os.WriteFile(tmpPath, []byte(token+"\n"), 0o600); err != nil {
		return err
	}

	runner := a.newRunner("server.monitor.telegram_setup")
	cfg := sshFromServer(srv)
	remoteTmp := "/tmp/ovpn-telegram-bot-token"
	if err := runner.CopyFile(a.ctx, cfg, tmpPath, remoteTmp); err != nil {
		return fmt.Errorf("upload telegram bot token to %s: %w", srv.Host, err)
	}
	remoteSecret := deploy.RemoteDir + "/monitoring/secrets/telegram_bot_token"
	remoteCmd := fmt.Sprintf(
		"set -e; sudo install -m 700 -d %s/monitoring/secrets; sudo mv %s %s; sudo chmod 600 %s",
		deploy.RemoteDir, remoteTmp, remoteSecret, remoteSecret,
	)
	if _, err := runner.Exec(a.ctx, cfg, remoteCmd); err != nil {
		return fmt.Errorf("install telegram bot token on %s: %w", srv.Host, err)
	}
	return nil
}

// waitForRemoteHTTPReady polls url until it responds successfully or the timeout elapses.
func (a *App) waitForRemoteHTTPReady(srv model.Server, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		_, err := a.fetchRemoteHTTP(srv, "GET", url, nil)
		if err == nil {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		a.sleep(2 * time.Second)
	}
	if lastErr == nil {
		lastErr = errors.New("service did not become ready before timeout")
	}
	return lastErr
}

func (a *App) sleep(d time.Duration) {
	if a.sleepHook != nil {
		a.sleepHook(d)
		return
	}
	time.Sleep(d)
}
