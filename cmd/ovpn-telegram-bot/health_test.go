package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBotHealthSnapshotTransitions(t *testing.T) {
	t.Parallel()

	h := newBotHealth(3*time.Second, nil)
	h.startedAt = time.Now().UTC().Add(-5 * time.Minute)

	snap := h.snapshot(time.Now().UTC())
	if snap.Status != "unhealthy" || snap.OK {
		t.Fatalf("expected unhealthy startup snapshot, got %+v", snap)
	}

	h.onPollSuccess(time.Now().UTC())
	h.onSendFailure(assertErr("send failed"))
	h.onSendFailure(assertErr("send failed"))
	h.onSendFailure(assertErr("send failed"))
	snap = h.snapshot(time.Now().UTC())
	if snap.Status != "degraded" || !snap.OK {
		t.Fatalf("expected degraded snapshot after send failures, got %+v", snap)
	}
}

func TestBuildPreformattedMessagesSplitsLongContent(t *testing.T) {
	t.Parallel()

	rows := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		rows = append(rows, "row "+strings.Repeat("x", 40))
	}
	chunks := buildPreformattedMessages("Users Audit", "Tot 120", "# hdr", rows, 300)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if !strings.Contains(chunk, "<pre>") || !strings.Contains(chunk, "</pre>") {
			t.Fatalf("expected preformatted chunk, got %q", chunk)
		}
	}
}

func TestHandleHealthReturnsServiceUnavailableWhenPollingStale(t *testing.T) {
	t.Parallel()

	b := &bot{
		cfg:     config{pollInterval: 3 * time.Second},
		metrics: newBotMetrics(prometheus.NewRegistry()),
	}
	b.health = newBotHealth(3*time.Second, b.metrics)
	b.health.startedAt = time.Now().UTC().Add(-5 * time.Minute)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	b.handleHealth(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"unhealthy"`) {
		t.Fatalf("expected unhealthy payload, got %s", rec.Body.String())
	}
}

func TestBotMetricsTrackPollAndSendState(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := newBotMetrics(reg)
	now := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	m.onPollSuccess(now)
	m.onPollFailure(2)
	m.onSendSuccess(now)
	m.onSendFailure(3)
	m.setWatchdogUnhealthy(true)

	if got := testutil.ToFloat64(m.pollLastSuccessUnix); got != float64(now.Unix()) {
		t.Fatalf("unexpected poll success gauge: %v", got)
	}
	if got := testutil.ToFloat64(m.pollFailuresConsec); got != 2 {
		t.Fatalf("unexpected poll consecutive failures: %v", got)
	}
	if got := testutil.ToFloat64(m.sendFailuresConsec); got != 3 {
		t.Fatalf("unexpected send consecutive failures: %v", got)
	}
	if got := testutil.ToFloat64(m.watchdogUnhealthyState); got != 1 {
		t.Fatalf("unexpected watchdog state: %v", got)
	}
}

func TestLinkFeatureStatusDisabledWhenConfigBroken(t *testing.T) {
	t.Parallel()

	b := &bot{cfg: config{linkConfigErr: "parse failed"}}
	if got := b.linkFeatureStatus(); got != "disabled" {
		t.Fatalf("linkFeatureStatus = %q, want disabled", got)
	}
}

func TestSendHTMLChunksAppliesReplyMarkupOnlyOnLastChunk(t *testing.T) {
	t.Parallel()

	var payloads []string
	c := &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			payloads = append(payloads, string(body))
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
		})},
	}
	b := &bot{
		tg:     c,
		cfg:    config{pollInterval: 3 * time.Second},
		health: newBotHealth(3*time.Second, nil),
	}

	err := b.sendHTMLChunks(context.Background(), 1, []string{"one", "two"}, map[string]any{"inline_keyboard": []any{}})
	if err != nil {
		t.Fatalf("sendHTMLChunks: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("expected 2 telegram calls, got %d", len(payloads))
	}
	if strings.Contains(payloads[0], "reply_markup") {
		t.Fatalf("first chunk should not carry reply_markup: %s", payloads[0])
	}
	if !strings.Contains(payloads[1], "reply_markup") {
		t.Fatalf("last chunk should carry reply_markup: %s", payloads[1])
	}
}

func assertErr(msg string) error {
	return &staticErr{msg: msg}
}

type staticErr struct {
	msg string
}

func (e *staticErr) Error() string { return e.msg }
