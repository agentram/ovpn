package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestRuntimeHelpersQuotaBlockedFilteringAndApply(t *testing.T) {
	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	now := time.Now().UTC()
	srv.LastDeployAt = &now
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}
	expired := now.Add(-24 * time.Hour)
	users := []model.User{
		{Username: "alice", Email: "alice@global", UUID: "11111111-1111-1111-1111-111111111111", Enabled: true},
		{Username: "bob", Email: "bob@global", UUID: "22222222-2222-2222-2222-222222222222", Enabled: true},
		{Username: "old", Email: "old@global", UUID: "33333333-3333-3333-3333-333333333333", Enabled: true, ExpiryDate: &expired},
	}
	app.remoteHTTPHook = func(_ model.Server, method, url string, payload any) ([]byte, error) {
		switch {
		case method == "GET" && strings.Contains(url, "/quota/status"):
			if strings.Contains(url, "alice%40global") {
				return []byte(`{"users":[{"email":"alice@global","blocked_by_quota":true}]}`), nil
			}
			return []byte(`{"users":[{"email":"bob@global","blocked_by_quota":true,"blocked_at":"2026-05-29T00:00:00Z"}]}`), nil
		case method == "POST" && strings.Contains(url, "/runtime/user/remove"):
			return []byte(`{"ok":true}`), nil
		default:
			t.Fatalf("unexpected hook call method=%s url=%s payload=%v", method, url, payload)
			return nil, nil
		}
	}

	filtered, err := app.usersForRuntimeConfig(*srv, users)
	if err != nil {
		t.Fatalf("usersForRuntimeConfig: %v", err)
	}
	if !filtered[0].Enabled || filtered[1].Enabled || !filtered[1].QuotaBlocked || filtered[2].Enabled {
		t.Fatalf("unexpected filtered users: %+v", filtered)
	}
	if err := app.applyRuntimeUser(*srv, users[0], true); !errors.Is(err, errRuntimeQuotaBlocked) {
		t.Fatalf("expected quota blocked runtime add, got %v", err)
	}
	if err := app.applyRuntimeUser(*srv, users[0], false); err != nil {
		t.Fatalf("runtime remove should succeed: %v", err)
	}
}

func TestApplyRuntimeUserUsesAllEnabledTransportInbounds(t *testing.T) {
	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), strings.Join([]string{
		model.TransportProfileRealityXHTTP,
		model.TransportProfilePlainXHTTP,
	}, ","))
	if err := app.store.UpdateServer(app.ctx, srv); err != nil {
		t.Fatalf("update server: %v", err)
	}
	user := model.User{
		Username: "alice",
		Email:    "alice@global",
		UUID:     "11111111-1111-1111-1111-111111111111",
		Enabled:  true,
	}
	var calls []string
	app.remoteHTTPHook = func(_ model.Server, method, url string, payload any) ([]byte, error) {
		if method == "GET" && strings.Contains(url, "/quota/status") {
			return []byte(`{"users":[{"email":"alice@global","blocked_by_quota":false}]}`), nil
		}
		body, _ := payload.(map[string]string)
		calls = append(calls, method+" "+url+" "+body["inbound_tag"])
		return []byte(`{"ok":true}`), nil
	}

	if err := app.applyRuntimeUser(*srv, user, true); err != nil {
		t.Fatalf("runtime add: %v", err)
	}
	if err := app.applyRuntimeUser(*srv, user, false); err != nil {
		t.Fatalf("runtime remove: %v", err)
	}
	got := strings.Join(calls, "\n")
	for _, want := range []string{
		"POST http://127.0.0.1:19000/runtime/user/add vless-reality",
		"POST http://127.0.0.1:19000/runtime/user/add vless-reality-xhttp",
		"POST http://127.0.0.1:19000/runtime/user/add vless-xhttp-plain",
		"POST http://127.0.0.1:19000/runtime/user/remove vless-reality",
		"POST http://127.0.0.1:19000/runtime/user/remove vless-reality-xhttp",
		"POST http://127.0.0.1:19000/runtime/user/remove vless-xhttp-plain",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing runtime call %q in:\n%s", want, got)
		}
	}
}

func TestRuntimePolicySyncRetriesReturnActionableErrors(t *testing.T) {
	app := newTestAppWithServer(t, false)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	var sleepCalls int
	app.sleepHook = func(time.Duration) { sleepCalls++ }
	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		return nil, errors.New("dial tcp 127.0.0.1 port refused")
	}
	if err := app.syncQuotaPolicy(*srv); err == nil || !strings.Contains(err.Error(), "ovpn-agent is not reachable") {
		t.Fatalf("expected actionable quota sync error, got %v", err)
	}
	if sleepCalls != 15 {
		t.Fatalf("expected 15 quota retry sleeps, got %d", sleepCalls)
	}

	sleepCalls = 0
	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		return nil, errors.New("agent down")
	}
	if err := app.syncUserPolicies(*srv); err == nil || !strings.Contains(err.Error(), "user policy sync failed after retries") {
		t.Fatalf("expected user sync retry error, got %v", err)
	}
	if sleepCalls != 15 {
		t.Fatalf("expected 15 user retry sleeps, got %d", sleepCalls)
	}
}

func TestRemoteHTTPAndPortHelpersBranches(t *testing.T) {
	app := newTestAppWithServer(t, true)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if _, err := app.fetchRemoteHTTP(*srv, "GET", app.agentURL("/health"), nil); err == nil || !strings.Contains(err.Error(), "missing HTTP status marker") {
		t.Fatalf("expected dry-run invalid response error, got %v", err)
	}

	if _, _, err := parseRemoteHTTPResponse("body\nOVPN_HTTP_STATUS:0"); err == nil || !strings.Contains(err.Error(), "invalid HTTP status") {
		t.Fatalf("expected invalid zero status, got %v", err)
	}
	if got := httpStatusText(599); got != "HTTP error" {
		t.Fatalf("unexpected unknown status text: %q", got)
	}

	t.Setenv("OVPN_AGENT_HOST_PORT", "bad")
	t.Setenv("OVPN_TELEGRAM_BOT_HOST_PORT", "70000")
	if got := app.agentHostPort(); got != "19000" {
		t.Fatalf("agent host port fallback=%q", got)
	}
	if got := app.telegramBotHostPort(); got != "19001" {
		t.Fatalf("telegram host port fallback=%q", got)
	}
	t.Setenv("OVPN_AGENT_HOST_PORT", "19002")
	t.Setenv("OVPN_TELEGRAM_BOT_HOST_PORT", "19003")
	if got := app.agentURL("health"); got != "http://127.0.0.1:19002/health" {
		t.Fatalf("agent url=%q", got)
	}
	if got := app.telegramNotifyURL(); got != "http://127.0.0.1:19003/notify" {
		t.Fatalf("telegram notify URL=%q", got)
	}
	t.Setenv("OVPN_TELEGRAM_NOTIFY_URL", "http://notify.local/hook")
	if got := app.telegramNotifyURL(); got != "http://notify.local/hook" {
		t.Fatalf("telegram notify override=%q", got)
	}
}

func TestUploadTelegramBotTokenDryRunAndValidation(t *testing.T) {
	app := newTestAppWithServer(t, true)
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if err := app.uploadTelegramBotToken(*srv, " "); err == nil || !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("expected token validation error, got %v", err)
	}
	if err := app.uploadTelegramBotToken(*srv, "123:abc"); err != nil {
		t.Fatalf("upload telegram token dry-run: %v", err)
	}
}
