package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"ovpn/internal/model"
	"ovpn/internal/store/remote"
)

func TestAgentRoutesExerciseQuotaStatsHealthAndRuntime(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{}
	store, mux := newTestAgentMuxWithRuntime(t, runtime)
	ctx := context.Background()
	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "alice@global",
		UUID:             "11111111-1111-1111-1111-111111111111",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@global",
		UUID:       "11111111-1111-1111-1111-111111111111",
		Enabled:    true,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}
	if err := store.AddDelta(ctx, "alice@global", 25, 50, time.Now().UTC()); err != nil {
		t.Fatalf("add delta: %v", err)
	}

	assertAgentStatus(t, mux, http.MethodGet, "/health", nil, http.StatusOK, "ovpn-agent")
	assertAgentStatus(t, mux, http.MethodGet, "/stats/total", nil, http.StatusOK, "alice@global")
	assertAgentStatus(t, mux, http.MethodGet, "/stats/daily?date=bad", nil, http.StatusOK, "alice@global")
	assertAgentStatus(t, mux, http.MethodGet, "/quota/status?email=alice%40global", nil, http.StatusOK, "alice@global")
	assertAgentStatus(t, mux, http.MethodGet, "/users/status?email=alice%40global", nil, http.StatusOK, "alice")
	assertAgentStatus(t, mux, http.MethodGet, "/quota/policies", nil, http.StatusOK, "alice@global")
	assertAgentStatus(t, mux, http.MethodPost, "/runtime/user/add", `{"email":"alice@global","uuid":"11111111-1111-1111-1111-111111111111","inbound_tag":"vless-reality"}`, http.StatusOK, `"ok":true`)
	assertAgentStatus(t, mux, http.MethodPost, "/runtime/user/remove", `{"email":"alice@global","inbound_tag":"vless-reality"}`, http.StatusOK, `"ok":true`)

	if len(runtime.adds) != 1 || runtime.adds[0] != "alice@global" {
		t.Fatalf("unexpected runtime adds: %+v", runtime.adds)
	}
	if len(runtime.removes) != 1 || runtime.removes[0] != "alice@global" {
		t.Fatalf("unexpected runtime removes: %+v", runtime.removes)
	}
}

func TestAgentRoutesQuotaSyncAndResetReadd(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{}
	store, mux := newTestAgentMuxWithRuntime(t, runtime)
	ctx := context.Background()
	quota := int64(100)
	syncBody := `{"users":[{"email":"alice@global","uuid":"11111111-1111-1111-1111-111111111111","inbound_tag":"vless-reality","quota_enabled":true,"monthly_quota_byte":100}]}`
	assertAgentStatus(t, mux, http.MethodPost, "/quota/sync", syncBody, http.StatusOK, `"users":1`)
	assertAgentStatus(t, mux, http.MethodPost, "/users/sync", `{"users":[{"username":"alice","email":"alice@global","uuid":"11111111-1111-1111-1111-111111111111","enabled":true,"inbound_tag":"vless-reality"}]}`, http.StatusOK, `"users":1`)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "alice@global",
		UUID:             "11111111-1111-1111-1111-111111111111",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	blockedAt := time.Now().UTC()
	if err := store.SetQuotaBlocked(ctx, "alice@global", true, &blockedAt); err != nil {
		t.Fatalf("set quota blocked: %v", err)
	}
	assertAgentStatus(t, mux, http.MethodPost, "/quota/reset", `{"email":" alice@global "}`, http.StatusOK, `"runtime_readd":true`)
	if len(runtime.adds) == 0 {
		t.Fatalf("expected runtime re-add on quota reset")
	}
}

func TestAgentRuntimeAddQuotaBranchesAndBadInputs(t *testing.T) {
	t.Parallel()

	store, mux := newTestAgentMux(t)
	ctx := context.Background()
	blockedAt := time.Now().UTC()
	if err := store.SetQuotaBlocked(ctx, "blocked@global", true, &blockedAt); err != nil {
		t.Fatalf("set quota blocked: %v", err)
	}
	assertAgentStatus(t, mux, http.MethodGet, "/runtime/user/add", nil, http.StatusMethodNotAllowed, "method not allowed")
	assertAgentStatus(t, mux, http.MethodPost, "/runtime/user/add", `{`, http.StatusBadRequest, "invalid request")
	assertAgentStatus(t, mux, http.MethodPost, "/runtime/user/add", `{"email":"blocked@global","uuid":"11111111-1111-1111-1111-111111111111","inbound_tag":"vless-reality"}`, http.StatusOK, `"ok":true`)

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "still-blocked@global",
		UUID:             "22222222-2222-2222-2222-222222222222",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	if err := store.SetQuotaBlocked(ctx, "still-blocked@global", true, &blockedAt); err != nil {
		t.Fatalf("set quota blocked: %v", err)
	}
	assertAgentStatus(t, mux, http.MethodPost, "/runtime/user/add", `{"email":"still-blocked@global","uuid":"22222222-2222-2222-2222-222222222222","inbound_tag":"vless-reality"}`, http.StatusConflict, "blocked by rolling 30d quota")
}

func TestAgentRoutesManualCollectAndRuntimeErrorBranches(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{addErr: errors.New("add failed"), removeErr: errors.New("remove failed")}
	store, mux := newTestAgentMuxWithRuntime(t, runtime)
	ctx := context.Background()
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@global",
		UUID:       "11111111-1111-1111-1111-111111111111",
		Enabled:    true,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}

	assertAgentStatus(t, mux, http.MethodPost, "/collect", nil, http.StatusInternalServerError, "collector is not configured")
	assertAgentStatus(t, mux, http.MethodGet, "/quota/sync", nil, http.StatusMethodNotAllowed, "method not allowed")
	assertAgentStatus(t, mux, http.MethodPost, "/quota/sync", `{`, http.StatusBadRequest, "invalid request")
	assertAgentStatus(t, mux, http.MethodGet, "/users/sync", nil, http.StatusMethodNotAllowed, "method not allowed")
	assertAgentStatus(t, mux, http.MethodPost, "/users/sync", `{`, http.StatusBadRequest, "invalid request")
	assertAgentStatus(t, mux, http.MethodGet, "/quota/reset", nil, http.StatusMethodNotAllowed, "method not allowed")
	assertAgentStatus(t, mux, http.MethodPost, "/quota/reset", `{`, http.StatusBadRequest, "invalid request")
	assertAgentStatus(t, mux, http.MethodPost, "/quota/reset", `{"email":" "}`, http.StatusBadRequest, "email is required")
	assertAgentStatus(t, mux, http.MethodPost, "/runtime/user/add", `{"email":"alice@global","uuid":"11111111-1111-1111-1111-111111111111"}`, http.StatusInternalServerError, "add failed")
	assertAgentStatus(t, mux, http.MethodGet, "/runtime/user/remove", nil, http.StatusMethodNotAllowed, "method not allowed")
	assertAgentStatus(t, mux, http.MethodPost, "/runtime/user/remove", `{`, http.StatusBadRequest, "invalid request")
	assertAgentStatus(t, mux, http.MethodPost, "/runtime/user/remove", `{"email":"alice@global"}`, http.StatusInternalServerError, "remove failed")
}

func TestAgentManualCollectRouteUsesInjectedCollector(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	metrics := newAgentMetrics(prometheus.NewRegistry())
	calls := 0
	mux := http.NewServeMux()
	registerHTTPRoutes(ctx, mux, routeDeps{
		store:       store,
		runtime:     &testRuntime{},
		metrics:     metrics,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		xrayAPI:     "127.0.0.1:0",
		dbPath:      ":memory:",
		collectOnce: func(context.Context) error { calls++; return nil },
		refreshOnce: func(context.Context) {},
	})
	assertAgentStatus(t, mux, http.MethodPost, "/collect", nil, http.StatusOK, `"ok":true`)
	if calls != 1 {
		t.Fatalf("expected collectOnce call, got %d", calls)
	}
}

func TestAgentMetricsCallbacksAndCertExpiry(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	metrics := newAgentMetrics(reg)
	metrics.OnCollectStart()
	metrics.OnCollectFinish(25*time.Millisecond, 3, nil)
	metrics.OnCollectFinish(time.Millisecond, 1, os.ErrNotExist)
	metrics.OnCounterReset()
	metrics.OnUsersActive(-5)
	metrics.OnUserSpike(10)
	metrics.OnDBWriteError("")
	metrics.OnXrayAPIReachable(false)
	metrics.observeRuntime("", "")
	metrics.observeQuotaEvent("", "")
	metrics.setQuotaBlockedUsers(2)
	metrics.setQuotaUsageBands(-1, 4)
	metrics.setUserTrafficTotals([]model.UserTraffic{{Email: "alice@global", UplinkBytes: 10, DownlinkBytes: 20}})
	metrics.setUserQuotaStatus(model.QuotaStatusResponse{Users: []model.QuotaUserStatus{{
		Email: "alice@global", QuotaEnabled: true, Window30DUsageByte: 50, Window30DQuotaByte: 100, BlockedByQuota: true,
	}}})
	days := 1.5
	expiryAt := time.Now().UTC().Add(36 * time.Hour)
	metrics.setUserExpiryStatus(model.UserStatusResponse{Expiring2DUsers: 1, ExpiredUsers: 2, Users: []model.UserAccessStatus{{
		Email: "alice@global", ExpiryDate: "2099-01-02", ExpiryAt: &expiryAt, Expired: false, EffectiveEnabled: true, DaysUntilExpiry: &days,
	}}})
	metrics.observeCertExpiry("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	metrics.observeCertExpiry(filepath.Join(t.TempDir(), "missing.pem"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	certFile := writeTestCertificate(t)
	metrics.observeCertExpiry(certFile, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if got := testutil.ToFloat64(metrics.quotaBlockedUsers); got != 2 {
		t.Fatalf("quotaBlockedUsers=%v want=2", got)
	}
	if got := testutil.ToFloat64(metrics.usersExpired); got != 2 {
		t.Fatalf("usersExpired=%v want=2", got)
	}
}

func assertAgentStatus(t *testing.T, mux *http.ServeMux, method, path string, body any, wantStatus int, wantBody string) {
	t.Helper()
	var src *bytes.Reader
	switch v := body.(type) {
	case nil:
		src = bytes.NewReader(nil)
	case string:
		src = bytes.NewReader([]byte(v))
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		src = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, src)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	if wantBody != "" && !strings.Contains(rec.Body.String(), wantBody) {
		t.Fatalf("%s %s expected body containing %q, got %s", method, path, wantBody, rec.Body.String())
	}
}

func writeTestCertificate(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().UTC().Add(-time.Hour),
		NotAfter:     time.Now().UTC().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	path := filepath.Join(t.TempDir(), "cert.pem")
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("encode cert: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	return path
}
