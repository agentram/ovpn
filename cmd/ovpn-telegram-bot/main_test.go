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
	"testing"

	"ovpn/internal/model"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTelegramClientRedactsTokenFromHTTPError(t *testing.T) {
	t.Parallel()

	const token = "123456:abcdefghijklmnopqrstuvwxyzABCDE"
	c := &telegramClient{
		token: token,
		http: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("Post \"" + req.URL.String() + "\": test network error")
			}),
		},
	}

	var out telegramAPIResponse[map[string]any]
	err := c.callTelegram(context.Background(), "getUpdates", map[string]any{"timeout": 1}, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked in error: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected redacted token marker in error: %v", err)
	}
}

func TestTelegramClientDecodeResponse(t *testing.T) {
	t.Parallel()

	c := &telegramClient{
		token: "123456:abcdefghijklmnopqrstuvwxyzABCDE",
		http: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":[]}`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	var out telegramAPIResponse[[]telegramUpdate]
	if err := c.callTelegram(context.Background(), "getUpdates", map[string]any{"timeout": 1}, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.OK {
		t.Fatalf("expected ok response")
	}
}

func TestMenuActionFromText(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"Home":     "home",
		"status":   "status",
		" USERS ":  "users",
		"Doctor":   "doctor",
		"Services": "services",
		"unknown":  "",
	}
	for in, want := range cases {
		if got := menuActionFromText(in); got != want {
			t.Fatalf("menuActionFromText(%q)=%q want=%q", in, got, want)
		}
	}
}

func TestFindPolicyForQuery(t *testing.T) {
	t.Parallel()

	policies := []model.QuotaUserPolicy{{Email: "alice@test", UUID: "u1"}, {Email: "bob@test", UUID: "u2"}}

	p, username, err := findPolicyForQuery("alice", policies)
	if err != nil {
		t.Fatalf("find by username: %v", err)
	}
	if p.Email != "alice@test" || username != "alice" {
		t.Fatalf("unexpected result: %+v username=%s", p, username)
	}

	p, username, err = findPolicyForQuery("bob@test", policies)
	if err != nil {
		t.Fatalf("find by email: %v", err)
	}
	if p.Email != "bob@test" || username != "bob" {
		t.Fatalf("unexpected result: %+v username=%s", p, username)
	}

	if _, _, err := findPolicyForQuery("charlie", policies); err == nil {
		t.Fatalf("expected not found error")
	}
}

func TestParseOwnerUserID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    int64
		wantErr string
	}{
		{name: "empty allowed", raw: "", want: 0},
		{name: "single id", raw: "42", want: 42},
		{name: "invalid id", raw: "abc", wantErr: "invalid owner user id"},
		{name: "multiple ids", raw: "1,2", wantErr: "exactly one numeric telegram user id"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOwnerUserID(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("owner id = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestInferOwnerUserID(t *testing.T) {
	t.Parallel()

	if got := inferOwnerUserID(42, "1,2"); got != 42 {
		t.Fatalf("explicit owner should win, got %d", got)
	}
	if got := inferOwnerUserID(0, "123456789"); got != 123456789 {
		t.Fatalf("expected notify fallback owner, got %d", got)
	}
	if got := inferOwnerUserID(0, ""); got != 0 {
		t.Fatalf("expected empty fallback owner, got %d", got)
	}
}

func TestParseTelegramAPIFallbackIPs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "single", raw: "149.154.167.220", want: []string{"149.154.167.220"}},
		{name: "trim dedupe", raw: "149.154.167.220, 149.154.167.220 ,149.154.166.110", want: []string{"149.154.167.220", "149.154.166.110"}},
		{name: "invalid", raw: "not-ip", wantErr: "not an IP"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTelegramAPIFallbackIPs(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("fallback ips=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestCompactIPs(t *testing.T) {
	t.Parallel()
	got := compactIPs([]string{" 149.154.167.220 ", "", "149.154.167.220", "149.154.166.110"})
	want := []string{"149.154.167.220", "149.154.166.110"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("compactIPs=%v want=%v", got, want)
	}
}

func TestBuildUserLinkRequiresShortID(t *testing.T) {
	t.Parallel()

	b := &bot{
		cfg: config{
			linkAddress:    "example.com",
			linkServerName: "www.microsoft.com",
			linkPublicKey:  "publickey",
			linkShortID:    "",
		},
	}
	if _, err := b.buildUserLink(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "link settings are incomplete") {
		t.Fatalf("expected incomplete link settings error, got %v", err)
	}
}

func TestPromptInputIgnoresCommandsAndMenuButtons(t *testing.T) {
	t.Parallel()

	b := &bot{}
	if b.isPromptInput("/status") {
		t.Fatalf("expected command to be ignored")
	}
	if b.isPromptInput("Users") {
		t.Fatalf("expected menu button to be ignored")
	}
	if !b.isPromptInput("alice") {
		t.Fatalf("expected username to be prompt input")
	}
}

func TestDispatchCallbackUsersLinkOwnerSetsPrompt(t *testing.T) {
	t.Parallel()

	var payload map[string]any
	c := &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/bottoken/sendMessage" {
				t.Fatalf("unexpected telegram path: %s", req.URL.Path)
			}
			raw, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(raw, &payload)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
		})},
	}
	b := &bot{cfg: config{ownerUserID: 42}, tg: c, prompts: map[int64]promptState{}}

	if err := b.dispatchCallback(context.Background(), 11, 42, "users:link"); err != nil {
		t.Fatalf("dispatch callback: %v", err)
	}
	if _, ok := b.getPrompt(11); !ok {
		t.Fatalf("expected prompt state for chat")
	}
	text, _ := payload["text"].(string)
	if !strings.Contains(text, "Send username or email") {
		t.Fatalf("unexpected callback response text: %q", text)
	}
}

func TestDispatchCallbackUsersLinkNonOwnerDenied(t *testing.T) {
	t.Parallel()

	c := &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
		})},
	}
	b := &bot{cfg: config{ownerUserID: 42}, tg: c, prompts: map[int64]promptState{}}

	if err := b.dispatchCallback(context.Background(), 11, 7, "users:link"); err != nil {
		t.Fatalf("dispatch callback: %v", err)
	}
	if _, ok := b.getPrompt(11); ok {
		t.Fatalf("did not expect prompt for non-owner")
	}
}

func TestSendGuideMissingFileReturnsMessage(t *testing.T) {
	t.Parallel()

	c := &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/bottoken/sendMessage" {
				t.Fatalf("unexpected path for missing-file flow: %s", req.URL.Path)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
		})},
	}
	b := &bot{cfg: config{clientsPDFPath: "/no/such/file.pdf"}, tg: c}

	if err := b.sendGuide(context.Background(), 10); err != nil {
		t.Fatalf("sendGuide should degrade to user message, got error: %v", err)
	}
}

func TestTelegramClientSendDocument(t *testing.T) {
	t.Parallel()

	called := false
	c := &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			if req.URL.Path != "/bottoken/sendDocument" {
				t.Fatalf("unexpected telegram method path: %s", req.URL.Path)
			}
			if !strings.Contains(req.Header.Get("Content-Type"), "multipart/form-data") {
				t.Fatalf("expected multipart content-type, got %q", req.Header.Get("Content-Type"))
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
		})},
	}

	err := c.sendDocument(context.Background(), 1, "clients.pdf", strings.NewReader("pdf"), "guide")
	if err != nil {
		t.Fatalf("sendDocument error: %v", err)
	}
	if !called {
		t.Fatalf("expected telegram API call")
	}
}

func TestLoadLinkConfigSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "link-config.json")
	raw := `{"address":"example.com","server_name":"www.microsoft.com","public_key":"pub","short_id":"abcd"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write link config: %v", err)
	}
	cfg := config{linkConfigFile: path}
	if err := cfg.loadLinkConfig(); err != nil {
		t.Fatalf("load link config: %v", err)
	}
	if cfg.linkAddress != "example.com" || cfg.linkServerName != "www.microsoft.com" || cfg.linkPublicKey != "pub" || cfg.linkShortID != "abcd" {
		t.Fatalf("unexpected parsed link config: %+v", cfg)
	}
}

func TestLoadLinkConfigRejectsIncomplete(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "link-config.json")
	raw := `{"address":"example.com","server_name":"","public_key":"pub","short_id":"abcd"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write link config: %v", err)
	}
	cfg := config{linkConfigFile: path}
	if err := cfg.loadLinkConfig(); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("expected incomplete config error, got %v", err)
	}
}

func TestBotAccessOwnerOnlyAndAutoNotifyChat(t *testing.T) {
	t.Parallel()

	b := &bot{
		cfg: config{
			ownerUserID: 42,
		},
	}
	if b.isAllowed(100, 7) {
		t.Fatalf("non-owner should be denied")
	}
	if !b.isAllowed(100, 42) {
		t.Fatalf("owner should be allowed")
	}
	b.ensureOwnerNotifyChat(1001, 42)
	b.ensureOwnerNotifyChat(1001, 42)
	if len(b.notifyChats) != 1 || b.notifyChats[0] != 1001 {
		t.Fatalf("expected deduplicated owner notify chat, got %+v", b.notifyChats)
	}
}
