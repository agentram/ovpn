package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ovpn/internal/model"
)

type telegramRecorder struct {
	mu        sync.Mutex
	paths     []string
	texts     []string
	callbacks []string
}

func (r *telegramRecorder) client() *telegramClient {
	return &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.paths = append(r.paths, req.URL.Path)
			switch req.URL.Path {
			case "/bottoken/sendMessage":
				var payload map[string]any
				raw, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(raw, &payload)
				text, _ := payload["text"].(string)
				r.texts = append(r.texts, text)
			case "/bottoken/answerCallbackQuery":
				var payload map[string]any
				raw, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(raw, &payload)
				text, _ := payload["text"].(string)
				r.callbacks = append(r.callbacks, text)
			case "/bottoken/sendDocument", "/bottoken/sendPhoto":
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{"ok":false,"description":"bad method"}`)), Header: make(http.Header)}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
		})},
	}
}

func TestRenderersAndDiagnosticsWithHealthyEndpoints(t *testing.T) {
	t.Parallel()

	rec := &telegramRecorder{}
	b := newCoverageBot(t, rec, false)
	ctx := context.Background()
	snapshot := b.collectAuditSnapshot(ctx)

	renderChecks := []struct {
		name string
		text string
		want string
	}{
		{name: "status", text: b.renderStatusSummary(ctx), want: "Overall: OK"},
		{name: "services", text: b.renderServicesOverview(ctx), want: "Healthy:"},
		{name: "service detail", text: b.renderSingleService(ctx, "ovpn-agent"), want: "Service Details"},
		{name: "doctor", text: b.renderDoctorReport(ctx), want: "Service Matrix"},
	}
	for _, check := range renderChecks {
		if !strings.Contains(check.text, check.want) {
			t.Fatalf("%s expected %q in:\n%s\nservices=%+v", check.name, check.want, check.text, snapshot.Services)
		}
	}

	top, err := b.renderTopUsers(ctx, 1)
	if err != nil {
		t.Fatalf("top users: %v", err)
	}
	if !strings.Contains(top, "@bob") {
		t.Fatalf("expected highest traffic user bob, got:\n%s", top)
	}
	totals, err := b.renderTrafficTotals(ctx)
	if err != nil {
		t.Fatalf("traffic totals: %v", err)
	}
	if !strings.Contains(totals, "Active users: 2") {
		t.Fatalf("unexpected totals:\n%s", totals)
	}
	today, err := b.renderTrafficToday(ctx)
	if err != nil {
		t.Fatalf("traffic today: %v", err)
	}
	if !strings.Contains(today, "Users with entries: 2") {
		t.Fatalf("unexpected today:\n%s", today)
	}
	quota, err := b.renderQuotaSummary(ctx)
	if err != nil {
		t.Fatalf("quota summary: %v", err)
	}
	if !strings.Contains(quota, "Users over 95%: 1") {
		t.Fatalf("unexpected quota summary:\n%s", quota)
	}
	over80, err := b.renderQuotaThreshold(ctx, 0.80, "Users over 80% quota")
	if err != nil {
		t.Fatalf("quota threshold: %v", err)
	}
	if !strings.Contains(over80, "@bob") || !strings.Contains(over80, "@alice") {
		t.Fatalf("unexpected over80:\n%s", over80)
	}
	blocked, err := b.renderQuotaBlocked(ctx)
	if err != nil {
		t.Fatalf("quota blocked: %v", err)
	}
	if !strings.Contains(blocked, "@bob") {
		t.Fatalf("unexpected blocked users:\n%s", blocked)
	}

	if snapshot.Overall != "OK" {
		t.Fatalf("expected healthy snapshot, got %+v", snapshot)
	}
	if got := serviceLabelForKey("custom"); got != "custom" {
		t.Fatalf("unexpected custom label: %q", got)
	}
	if line := renderServiceLine(serviceCheck{Label: "svc"}); !strings.Contains(line, "FAIL") || !strings.Contains(line, "n/a") {
		t.Fatalf("unexpected service line: %q", line)
	}
	if _, ok := findServiceCheck(snapshot.Services, "missing"); ok {
		t.Fatalf("missing service should not be found")
	}
}

func TestDispatchCommandMenuCallbackAndMessageFlows(t *testing.T) {
	t.Parallel()

	rec := &telegramRecorder{}
	b := newCoverageBot(t, rec, false)
	ctx := context.Background()
	pdfPath := filepath.Join(t.TempDir(), "clients.pdf")
	if err := os.WriteFile(pdfPath, []byte("pdf"), 0o600); err != nil {
		t.Fatalf("write guide: %v", err)
	}
	b.cfg.clientsPDFPath = pdfPath

	commands := [][]string{
		{"/start"},
		{"/help"},
		{"/guide"},
		{"/status"},
		{"/services"},
		{"/doctor"},
		{"/users"},
		{"/traffic"},
		{"/quota"},
		{"/restart"},
		{"/restart", "xray"},
		{"/heal"},
		{"/unknown"},
	}
	for _, args := range commands {
		if err := b.dispatchCommand(ctx, 11, 42, args[0], args[1:]); err != nil {
			t.Fatalf("dispatch command %v: %v", args, err)
		}
	}

	for _, action := range []string{"home", "status", "doctor", "services", "users", "traffic", "quota", "help", "bad"} {
		if err := b.dispatchMenuAction(ctx, 11, 42, action); err != nil {
			t.Fatalf("dispatch menu %q: %v", action, err)
		}
	}

	callbacks := []string{
		"users:refresh",
		"users:top",
		"users:link",
		"users:back",
		"traffic:totals",
		"traffic:top10",
		"traffic:today",
		"traffic:back",
		"quota:summary",
		"quota:over80",
		"quota:over95",
		"quota:blocked",
		"quota:back",
		"services:overview",
		"services:doctor",
		"services:heal",
		"services:detail:xray-via-agent",
		"services:restart:xray",
		"confirm:no",
		"unknown",
	}
	for _, data := range callbacks {
		if err := b.dispatchCallback(ctx, 11, 42, data); err != nil {
			t.Fatalf("dispatch callback %q: %v", data, err)
		}
	}

	b.handleMessage(ctx, nil)
	b.handleMessage(ctx, &telegramMessage{Chat: telegramChat{ID: 11}, From: &telegramUser{ID: 7}, Text: "/status"})
	b.handleMessage(ctx, &telegramMessage{Chat: telegramChat{ID: 11}, From: &telegramUser{ID: 42}, Text: ""})
	b.setPrompt(11, promptUserLink)
	b.handleMessage(ctx, &telegramMessage{Chat: telegramChat{ID: 11}, From: &telegramUser{ID: 42}, Text: "/cancel"})
	b.handleMessage(ctx, &telegramMessage{Chat: telegramChat{ID: 11}, From: &telegramUser{ID: 42}, Text: "/status"})
	b.handleMessage(ctx, &telegramMessage{Chat: telegramChat{ID: 11}, From: &telegramUser{ID: 42}, Text: "Status"})
	b.handleMessage(ctx, &telegramMessage{Chat: telegramChat{ID: 11}, From: &telegramUser{ID: 42}, Text: "nonsense"})
	b.handleCallback(ctx, nil)
	b.handleCallback(ctx, &telegramCallbackQuery{ID: "cb-denied", From: &telegramUser{ID: 7}, Message: &telegramMessage{Chat: telegramChat{ID: 11}}, Data: "status"})
	b.handleCallback(ctx, &telegramCallbackQuery{ID: "cb-ok", From: &telegramUser{ID: 42}, Message: &telegramMessage{Chat: telegramChat{ID: 11}}, Data: "services:overview"})

	if len(rec.paths) == 0 {
		t.Fatalf("expected telegram calls")
	}
	if got := formatFriendlyError(nil); got != "Request failed." {
		t.Fatalf("unexpected nil friendly error: %q", got)
	}
	if got := formatFriendlyError(errors.New(" boom ")); got != "Request failed: boom" {
		t.Fatalf("unexpected friendly error: %q", got)
	}
}

func TestDiagnosticsFailureAndEmptyRendererBranches(t *testing.T) {
	t.Parallel()

	rec := &telegramRecorder{}
	b := newCoverageBot(t, rec, true)
	ctx := context.Background()

	status := b.renderStatusSummary(ctx)
	if !strings.Contains(status, "Overall: FAIL") || !strings.Contains(status, "Quota: unavailable") {
		t.Fatalf("expected degraded status, got:\n%s", status)
	}
	services := b.renderServicesOverview(ctx)
	if !strings.Contains(services, "FAIL") {
		t.Fatalf("expected failed service overview, got:\n%s", services)
	}
	if got := b.renderSingleService(ctx, "missing"); got != "Service check not found." {
		t.Fatalf("unexpected missing service detail: %q", got)
	}

	b.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/stats/total", "/stats/daily":
			return jsonResponse([]model.UserTraffic{})
		case "/quota/status":
			return jsonResponse(model.QuotaStatusResponse{})
		default:
			return jsonResponse(map[string]any{"ok": true})
		}
	})}
	if top, err := b.renderTopUsers(ctx, 0); err != nil || !strings.Contains(top, "No traffic data yet") {
		t.Fatalf("unexpected empty top users: text=%q err=%v", top, err)
	}
	if today, err := b.renderTrafficToday(ctx); err != nil || !strings.Contains(today, "No traffic data") {
		t.Fatalf("unexpected empty traffic today: text=%q err=%v", today, err)
	}
	if over, err := b.renderQuotaThreshold(ctx, 0.95, "over95"); err != nil || !strings.Contains(over, "No users") {
		t.Fatalf("unexpected empty quota threshold: text=%q err=%v", over, err)
	}
	if blocked, err := b.renderQuotaBlocked(ctx); err != nil || !strings.Contains(blocked, "No blocked") {
		t.Fatalf("unexpected empty blocked quota: text=%q err=%v", blocked, err)
	}
}

func newCoverageBot(t *testing.T, rec *telegramRecorder, broken bool) *bot {
	t.Helper()
	now := time.Now().UTC()
	health := agentHealth{
		OK:               true,
		Service:          "ovpn-agent",
		XrayAPIReachable: true,
		LastCollectAt:    now.Format(time.RFC3339),
		LastResetAt:      now.Add(-time.Hour).Format(time.RFC3339),
		Time:             now.Format(time.RFC3339),
	}
	quota := model.QuotaStatusResponse{
		Window30DStart:    now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
		Window30DEnd:      now.Format(time.RFC3339),
		QuotaEnabledUsers: 2,
		BlockedUsers:      1,
		Users: []model.QuotaUserStatus{
			{Email: "alice@test", QuotaEnabled: true, Window30DUsageByte: 90, Window30DQuotaByte: 100},
			{Email: "bob@test", QuotaEnabled: true, Window30DUsageByte: 120, Window30DQuotaByte: 100, BlockedByQuota: true},
		},
	}
	days := 1.0
	userStatus := model.UserStatusResponse{
		EffectiveEnabledUsers: 1,
		Expiring2DUsers:       1,
		ExpiredUsers:          1,
		Users: []model.UserAccessStatus{
			{Username: "alice", Email: "alice@test", EffectiveEnabled: true, ExpiryDate: "2099-01-02", DaysUntilExpiry: &days, QuotaEnabled: true, Window30DUsageByte: 90, Window30DQuotaByte: 100},
			{Username: "bob", Email: "bob@test", Expired: true, BlockedByQuota: true, QuotaEnabled: true, Window30DUsageByte: 120, Window30DQuotaByte: 100},
		},
	}
	totals := []model.UserTraffic{
		{Email: "alice@test", UplinkBytes: 10, DownlinkBytes: 20},
		{Email: "bob@test", UplinkBytes: 100, DownlinkBytes: 200},
	}
	policies := []model.QuotaUserPolicy{
		{Email: "alice@test", UUID: "11111111-1111-1111-1111-111111111111", InboundTag: "vless-reality", QuotaEnabled: true},
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if broken {
			switch req.URL.Path {
			case "/health", "/quota/status", "/users/status", "/stats/total", "/api/v2/alerts":
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(`{"error":"boom"}`)), Header: make(http.Header)}, nil
			case "/-/healthy", "/api/health", "/metrics", "/healthz", "/":
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(`fail`)), Header: make(http.Header)}, nil
			default:
				return jsonResponse(map[string]any{"ok": false})
			}
		}
		switch req.URL.Path {
		case "/health":
			if req.URL.Host == "self" {
				return jsonResponse(selfHealthResponse{OK: true, Status: "ok", LinkFeature: "enabled", Health: botHealthSnapshot{ConsecutiveSendFailures: 3}})
			}
			return jsonResponse(health)
		case "/quota/status":
			return jsonResponse(quota)
		case "/users/status":
			return jsonResponse(userStatus)
		case "/stats/total", "/stats/daily":
			return jsonResponse(totals)
		case "/quota/policies":
			return jsonResponse(policies)
		case "/api/v2/alerts":
			return jsonResponse([]map[string]any{{"status": "firing"}, {"status": "firing"}})
		case "", "/-/healthy", "/api/health", "/metrics", "/healthz", "/":
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`ok`)), Header: make(http.Header)}, nil
		default:
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`not found`)), Header: make(http.Header)}, nil
		}
	})}
	return &bot{
		cfg: config{
			ownerUserID:     42,
			agentURL:        "http://agent",
			selfURL:         "http://self/health",
			alertmanagerURL: "http://alertmanager",
			prometheusURL:   "http://prometheus",
			grafanaURL:      "http://grafana",
			nodeExporterURL: "http://node",
			cadvisorURL:     "http://cadvisor",
			haproxyURL:      "http://haproxy",
			linkAddress:     "example.com",
			linkServerName:  "www.microsoft.com",
			linkPublicKey:   "publickey",
			linkShortID:     "abcd1234",
		},
		httpClient:  client,
		tg:          rec.client(),
		prompts:     map[int64]promptState{},
		confirms:    map[int64]confirmState{},
		notifyChats: []int64{},
	}
}

func jsonResponse(v any) (*http.Response, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
}
