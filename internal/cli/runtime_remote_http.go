package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/deploy"
	"ovpn/internal/model"
	"ovpn/internal/telegrambot"
	"ovpn/internal/util"
)

// fetchRemoteAgent returns remote agent for callers.
func (a *App) fetchRemoteAgent(srv model.Server, method, url string, payload any) ([]byte, error) {
	return a.fetchRemoteHTTP(srv, method, url, payload)
}

// fetchRemoteHTTP returns remote http for callers.
func (a *App) fetchRemoteHTTP(srv model.Server, method, url string, payload any) ([]byte, error) {
	runner := a.newRunner("agent_http")
	cfg := sshFromServer(srv)
	var cmd string
	if payload == nil {
		cmd = fmt.Sprintf("curl --max-time 10 -fsS -X %s '%s'", method, url)
	} else {
		b, _ := json.Marshal(payload)
		// Send payload through stdin to avoid shell escaping bugs and leaking large JSON in logs.
		cmd = fmt.Sprintf("cat <<'JSON' | curl --max-time 10 -fsS -X %s -H 'Content-Type: application/json' -d @- '%s'\n%s\nJSON", method, url, string(b))
	}
	a.log().Debug("calling remote agent endpoint", "server", srv.Name, "host", srv.Host, "method", method, "url", url, "has_payload", payload != nil)
	res, err := runner.Exec(a.ctx, cfg, cmd)
	if err != nil {
		return nil, fmt.Errorf("remote http call %s %s on %s failed: %w", method, url, srv.Host, err)
	}
	return []byte(strings.TrimSpace(res.Stdout)), nil
}

// agentHostPort returns agent host port.
func (a *App) agentHostPort() string {
	raw := strings.TrimSpace(envOr("OVPN_AGENT_HOST_PORT", "19000"))
	p, err := strconv.Atoi(raw)
	if err != nil || p <= 0 || p > 65535 {
		a.log().Warn("invalid OVPN_AGENT_HOST_PORT, falling back to default 19000", "value", raw)
		return "19000"
	}
	return strconv.Itoa(p)
}

// agentBaseURL returns agent base url.
func (a *App) agentBaseURL() string {
	return "http://127.0.0.1:" + a.agentHostPort()
}

// agentURL returns agent url.
func (a *App) agentURL(path string) string {
	return strings.TrimRight(a.agentBaseURL(), "/") + "/" + strings.TrimLeft(path, "/")
}

// telegramBotHostPort returns telegram bot host port.
func (a *App) telegramBotHostPort() string {
	raw := strings.TrimSpace(envOr("OVPN_TELEGRAM_BOT_HOST_PORT", "19001"))
	p, err := strconv.Atoi(raw)
	if err != nil || p <= 0 || p > 65535 {
		a.log().Warn("invalid OVPN_TELEGRAM_BOT_HOST_PORT, falling back to default 19001", "value", raw)
		return "19001"
	}
	return strconv.Itoa(p)
}

// telegramNotifyURL returns telegram notify url.
func (a *App) telegramNotifyURL() string {
	return envOr("OVPN_TELEGRAM_NOTIFY_URL", "http://127.0.0.1:"+a.telegramBotHostPort()+"/notify")
}

// sendTelegramNotifyEvent returns send telegram notify event.
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

// postRemoteNotifyBestEffort executes post remote notify best effort against remote hosts over SSH.
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

// uploadTelegramBotToken executes telegram bot token on the remote host in a fixed order.
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

// waitForRemoteHTTPReady runs for remote http ready loop until context cancellation or error.
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
		time.Sleep(2 * time.Second)
	}
	if lastErr == nil {
		lastErr = errors.New("service did not become ready before timeout")
	}
	return lastErr
}
