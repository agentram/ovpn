package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"ovpn/internal/model"
	"ovpn/internal/store/remote"
)

func TestMetricsRefreshFuncReadsTrafficQuotaAndExpiryState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	quota := int64(100)
	now := time.Now().UTC()
	expiry := now.Add(24 * time.Hour)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email: "alice@global", UUID: "uuid-1", InboundTag: "vless-reality", QuotaEnabled: true, MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username: "alice", Email: "alice@global", UUID: "uuid-1", Enabled: true, ExpiryAt: &expiry, InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}
	if err := store.AddDelta(ctx, "alice@global", 90, 5, now); err != nil {
		t.Fatalf("add delta: %v", err)
	}
	blockedAt := now
	if err := store.SetQuotaBlocked(ctx, "alice@global", true, &blockedAt); err != nil {
		t.Fatalf("set blocked: %v", err)
	}

	metrics := newAgentMetrics(prometheus.NewRegistry())
	refresh := newMetricsRefreshFunc(store, slog.New(slog.NewTextHandler(io.Discard, nil)), metrics)
	refresh(ctx)
	if got := testutil.ToFloat64(metrics.quotaBlockedUsers); got != 1 {
		t.Fatalf("quota blocked metric=%v", got)
	}
	if got := testutil.ToFloat64(metrics.quotaUsersOver80); got != 1 {
		t.Fatalf("quota over80 metric=%v", got)
	}
	if got := testutil.ToFloat64(metrics.quotaUsersOver95); got != 1 {
		t.Fatalf("quota over95 metric=%v", got)
	}
	if got := testutil.ToFloat64(metrics.usersExpiring2D); got != 1 {
		t.Fatalf("users expiring metric=%v", got)
	}
}

func TestHTTPHelpersEnvAbsentAndRequestLoggingBranches(t *testing.T) {
	t.Setenv("OVPN_AGENT_SPIKE_DELTA_BYTES", "")
	if got := envInt64("OVPN_AGENT_SPIKE_DELTA_BYTES", 123); got != 123 {
		t.Fatalf("empty envInt64=%d", got)
	}
	t.Setenv("OVPN_AGENT_SPIKE_DELTA_BYTES", "bad")
	if got := envInt64("OVPN_AGENT_SPIKE_DELTA_BYTES", 123); got != 123 {
		t.Fatalf("bad envInt64=%d", got)
	}
	t.Setenv("OVPN_AGENT_SPIKE_DELTA_BYTES", "456")
	if got := envInt64("OVPN_AGENT_SPIKE_DELTA_BYTES", 123); got != 456 {
		t.Fatalf("valid envInt64=%d", got)
	}
	for _, errText := range []string{"user not found", "user does not exist", "failed to remove inbound user"} {
		if !isRuntimeUserAbsentError(errors.New(errText)) {
			t.Fatalf("expected absent error match for %q", errText)
		}
	}
	if isRuntimeUserAbsentError(nil) || isRuntimeUserAbsentError(errors.New("other")) {
		t.Fatalf("unexpected absent error match")
	}
	for _, errText := range []string{"user already exists", "proxy/vless: User alice@global already exists."} {
		if !isRuntimeUserAlreadyPresentError(errors.New(errText)) {
			t.Fatalf("expected already-present error match for %q", errText)
		}
	}
	if isRuntimeUserAlreadyPresentError(nil) || isRuntimeUserAlreadyPresentError(errors.New("other")) {
		t.Fatalf("unexpected already-present error match")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		handler := withRequestLogging(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, status, map[string]any{"status": status})
		}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", nil))
		if rec.Code != status {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, status, rec.Body.String())
		}
	}
}

type stringErrorReader struct {
	text string
}

func (r stringErrorReader) Error() string { return r.text }

func TestRuntimeAbsentErrorReaderHelper(t *testing.T) {
	t.Parallel()
	if !isRuntimeUserAbsentError(stringErrorReader{text: "failed to remove user"}) {
		t.Fatalf("expected custom error to match")
	}
}
