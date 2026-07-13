package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"ovpn/internal/model"
	"ovpn/internal/stats"
	"ovpn/internal/store/remote"
)

type testRuntime struct {
	addErr    error
	removeErr error
	adds      []string
	removes   []string
}

func (r *testRuntime) AddUser(_ context.Context, _ string, email string, _ string) error {
	r.adds = append(r.adds, email)
	return r.addErr
}

func (r *testRuntime) RemoveUser(_ context.Context, _ string, email string) error {
	r.removes = append(r.removes, email)
	return r.removeErr
}

func newTestAgentMux(t *testing.T) (*remote.Store, *http.ServeMux) {
	return newTestAgentMuxWithRuntime(t, nil)
}

func newTestAgentMuxWithRuntime(t *testing.T, runtime *testRuntime) (*remote.Store, *http.ServeMux) {
	t.Helper()

	ctx := context.Background()
	store, err := remote.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if runtime == nil {
		runtime = &testRuntime{}
	}

	mux := http.NewServeMux()
	metrics := newAgentMetrics(prometheus.NewRegistry())
	registerHTTPRoutes(ctx, mux, routeDeps{
		store:       store,
		quota:       &stats.QuotaEnforcer{Store: store, Runtime: runtime},
		expiry:      &stats.ExpiryEnforcer{Store: store, Runtime: runtime},
		runtime:     runtime,
		metrics:     metrics,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		xrayAPI:     "127.0.0.1:0",
		dbPath:      ":memory:",
		refreshOnce: func(context.Context) {},
	})
	return store, mux
}

func TestUsersSyncPersistsPolicies(t *testing.T) {
	t.Parallel()

	store, mux := newTestAgentMux(t)
	expiry, err := model.ParseExpiryDate("2099-04-18")
	if err != nil {
		t.Fatalf("parse expiry: %v", err)
	}

	body, err := json.Marshal(usersSyncReq{
		Users: []model.UserPolicy{{
			Username:   "alice",
			Email:      "alice@global",
			UUID:       "uuid-1",
			Enabled:    true,
			ExpiryAt:   expiry,
			InboundTag: "vless-reality",
		}},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/users/sync", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	policies, err := store.ListUserPolicies(context.Background())
	if err != nil {
		t.Fatalf("list policies: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("unexpected policies len=%d", len(policies))
	}
	if policies[0].Email != "alice@global" || model.ExpiryDateString(policies[0].ExpiryAt) != "2099-04-18" {
		t.Fatalf("unexpected policy: %+v", policies[0])
	}
}

func TestUsersStatusReturnsExpiryFields(t *testing.T) {
	t.Parallel()

	store, mux := newTestAgentMux(t)
	ctx := context.Background()
	expiry, err := model.ParseExpiryDate("2099-04-18")
	if err != nil {
		t.Fatalf("parse expiry: %v", err)
	}
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@global",
		UUID:       "uuid-1",
		Enabled:    true,
		ExpiryAt:   expiry,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/status?email=alice@global", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp model.UserStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Users) != 1 {
		t.Fatalf("unexpected user count: %d", len(resp.Users))
	}
	if resp.Users[0].ExpiryDate != "2099-04-18" || !resp.Users[0].EffectiveEnabled || resp.Users[0].Expired {
		t.Fatalf("unexpected user status: %+v", resp.Users[0])
	}
}

func TestRuntimeAddRejectsExpiredUser(t *testing.T) {
	t.Parallel()

	store, mux := newTestAgentMux(t)
	ctx := context.Background()
	expiredAt := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@global",
		UUID:       "uuid-1",
		Enabled:    true,
		ExpiryAt:   &expiredAt,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}

	body := `{"email":"alice@global","uuid":"uuid-1","inbound_tag":"vless-reality"}`
	req := httptest.NewRequest(http.MethodPost, "/runtime/user/add", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("disabled or expired")) {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestRuntimeAddRejectsDisabledUser(t *testing.T) {
	t.Parallel()

	store, mux := newTestAgentMux(t)
	ctx := context.Background()
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@global",
		UUID:       "uuid-1",
		Enabled:    false,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}

	body := `{"email":"alice@global","uuid":"uuid-1","inbound_tag":"vless-reality"}`
	req := httptest.NewRequest(http.MethodPost, "/runtime/user/add", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("disabled or expired")) {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestRuntimeRemoveTreatsMissingUserAsSuccess(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{removeErr: errors.New("failed to remove user: not found")}
	_, mux := newTestAgentMuxWithRuntime(t, runtime)

	body := `{"email":" alice@global ","inbound_tag":"vless-reality"}`
	req := httptest.NewRequest(http.MethodPost, "/runtime/user/remove", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(runtime.removes) != 1 || runtime.removes[0] != "alice@global" {
		t.Fatalf("unexpected runtime removes: %+v", runtime.removes)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"absent":true`)) {
		t.Fatalf("expected absent=true response, got %s", rec.Body.String())
	}
}

func TestRuntimeRemoveRequiresEmail(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{}
	_, mux := newTestAgentMuxWithRuntime(t, runtime)

	req := httptest.NewRequest(http.MethodPost, "/runtime/user/remove", bytes.NewBufferString(`{"email":" "}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(runtime.removes) != 0 {
		t.Fatalf("expected no runtime remove, got %+v", runtime.removes)
	}
}

func TestRuntimeRemoveReturnsErrorForUnexpectedRuntimeFailure(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{removeErr: errors.New("xray api unavailable")}
	_, mux := newTestAgentMuxWithRuntime(t, runtime)

	req := httptest.NewRequest(http.MethodPost, "/runtime/user/remove", bytes.NewBufferString(`{"email":"alice@global"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("xray api unavailable")) {
		t.Fatalf("unexpected response body: %s", rec.Body.String())
	}
}

func TestRuntimeUserAbsentErrorMatching(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want bool
	}{
		{err: nil, want: false},
		{err: errors.New("user not found"), want: true},
		{err: errors.New("user does not exist"), want: true},
		{err: errors.New("failed to remove user: xray returned 500"), want: true},
		{err: errors.New("xray api unavailable"), want: false},
	}
	for _, tc := range cases {
		if got := isRuntimeUserAbsentError(tc.err); got != tc.want {
			t.Fatalf("isRuntimeUserAbsentError(%v)=%v want %v", tc.err, got, tc.want)
		}
	}
}

func TestQuotaResetDoesNotReaddExpiredUser(t *testing.T) {
	t.Parallel()

	store, mux := newTestAgentMux(t)
	ctx := context.Background()
	quota := int64(200)
	expiredAt := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)

	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "alice@global",
		UUID:             "uuid-1",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@global",
		UUID:       "uuid-1",
		Enabled:    true,
		ExpiryAt:   &expiredAt,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SetQuotaBlocked(ctx, "alice@global", true, &now); err != nil {
		t.Fatalf("set quota blocked: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/quota/reset", bytes.NewBufferString(`{"email":"alice@global"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := resp["runtime_readd"].(bool); got {
		t.Fatalf("expected runtime_readd=false, got %+v", resp)
	}

	state, found, err := store.GetQuotaState(ctx, "alice@global")
	if err != nil {
		t.Fatalf("get quota state: %v", err)
	}
	if !found || state.Blocked {
		t.Fatalf("expected quota state to be unblocked, got found=%v state=%+v", found, state)
	}
}

func TestQuotaResetTreatsAlreadyExistsAsSuccess(t *testing.T) {
	t.Parallel()

	runtime := &testRuntime{addErr: errors.New("rpc error: code = Unknown desc = proxy/vless: User alice@global already exists.")}
	store, mux := newTestAgentMuxWithRuntime(t, runtime)
	ctx := context.Background()
	quota := int64(200)

	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "alice@global",
		UUID:             "uuid-1",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@global",
		UUID:       "uuid-1",
		Enabled:    true,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}
	blockedAt := time.Now().UTC()
	if err := store.SetQuotaBlocked(ctx, "alice@global", true, &blockedAt); err != nil {
		t.Fatalf("set quota blocked: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/quota/reset", bytes.NewBufferString(`{"email":"alice@global"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"runtime_readd":true`)) {
		t.Fatalf("expected runtime_readd=true, body=%s", rec.Body.String())
	}

	state, found, err := store.GetQuotaState(ctx, "alice@global")
	if err != nil {
		t.Fatalf("get quota state: %v", err)
	}
	if !found || state.Blocked {
		t.Fatalf("expected quota state to be unblocked, got found=%v state=%+v", found, state)
	}
}
