package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ovpn/internal/telegrambot"
)

func TestBotHTTPHealthAlertmanagerAndNotify(t *testing.T) {
	t.Parallel()

	rec := &telegramRecorder{}
	b := newBotTestHarness(t, rec, false)
	b.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	b.notifyChats = []int64{101, 102}

	rr := httptest.NewRecorder()
	b.handleHealth(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "ovpn-telegram-bot") {
		t.Fatalf("unexpected health response status=%d body=%s", rr.Code, rr.Body.String())
	}

	alertPayload := `{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"HighTraffic","instance":"vpn-1"},"annotations":{"summary":"traffic spike"}}]}`
	rr = httptest.NewRecorder()
	b.handleAlertmanagerWebhook(rr, httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(alertPayload)))
	if rr.Code != http.StatusOK {
		t.Fatalf("alertmanager status=%d body=%s", rr.Code, rr.Body.String())
	}

	notifyPayload := `{"event":"quota_block","status":"success","severity":"warning","source":"ovpn-agent","message":"quota block applied"}`
	rr = httptest.NewRecorder()
	b.handleNotifyEvent(rr, httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(notifyPayload)))
	if rr.Code != http.StatusOK {
		t.Fatalf("notify status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(rec.texts) < 4 {
		t.Fatalf("expected messages to both notify chats, got %+v", rec.texts)
	}
}

func TestBotHTTPRejectsBadRequestsAndTelegramFailures(t *testing.T) {
	t.Parallel()

	rec := &telegramRecorder{}
	b := newBotTestHarness(t, rec, false)
	b.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	b.notifyChats = nil

	cases := []struct {
		name       string
		handler    func(http.ResponseWriter, *http.Request)
		req        *http.Request
		wantStatus int
		wantBody   string
	}{
		{
			name:       "alert method",
			handler:    b.handleAlertmanagerWebhook,
			req:        httptest.NewRequest(http.MethodGet, "/alertmanager", nil),
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed",
		},
		{
			name:       "alert invalid",
			handler:    b.handleAlertmanagerWebhook,
			req:        httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader("{")),
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid payload",
		},
		{
			name:       "alert no chats",
			handler:    b.handleAlertmanagerWebhook,
			req:        httptest.NewRequest(http.MethodPost, "/alertmanager", strings.NewReader(`{"status":"resolved"}`)),
			wantStatus: http.StatusBadGateway,
			wantBody:   "telegram send failed",
		},
		{
			name:       "notify method",
			handler:    b.handleNotifyEvent,
			req:        httptest.NewRequest(http.MethodGet, "/notify", nil),
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed",
		},
		{
			name:       "notify invalid",
			handler:    b.handleNotifyEvent,
			req:        httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader("{")),
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid payload",
		},
		{
			name:       "notify no chats",
			handler:    b.handleNotifyEvent,
			req:        httptest.NewRequest(http.MethodPost, "/notify", strings.NewReader(`{"event":"deploy"}`)),
			wantStatus: http.StatusBadGateway,
			wantBody:   "telegram send failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			tc.handler(rr, tc.req)
			if rr.Code != tc.wantStatus || !strings.Contains(rr.Body.String(), tc.wantBody) {
				t.Fatalf("status=%d body=%s want status=%d containing %q", rr.Code, rr.Body.String(), tc.wantStatus, tc.wantBody)
			}
		})
	}

	if err := b.sendToNotifyChats(context.Background(), telegrambot.RenderNotifyMessage(telegrambot.NotifyEvent{Event: "deploy"})); err == nil {
		t.Fatalf("expected missing notify chats error")
	}
}

func TestBotSendToNotifyChatsReturnsFirstSendError(t *testing.T) {
	t.Parallel()

	b := newBotTestHarness(t, &telegramRecorder{}, false)
	b.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	b.notifyChats = []int64{101}
	b.tg = &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("telegram unavailable")
		})},
	}
	if err := b.sendToNotifyChats(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "telegram unavailable") {
		t.Fatalf("expected telegram failure, got %v", err)
	}
}
