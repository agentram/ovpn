package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"ovpn/internal/model"
	"ovpn/internal/store/remote"
)

func newTestAgentMux(t *testing.T) (*remote.Store, *http.ServeMux) {
	t.Helper()

	ctx := context.Background()
	store, err := remote.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	mux := http.NewServeMux()
	registerHTTPRoutes(ctx, mux, routeDeps{
		store:       store,
		metrics:     newAgentMetrics(prometheus.NewRegistry()),
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
	expiry, err := model.ParseExpiryDate("2026-04-18")
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
	if policies[0].Email != "alice@global" || model.ExpiryDateString(policies[0].ExpiryAt) != "2026-04-18" {
		t.Fatalf("unexpected policy: %+v", policies[0])
	}
}

func TestUsersStatusReturnsExpiryFields(t *testing.T) {
	t.Parallel()

	store, mux := newTestAgentMux(t)
	ctx := context.Background()
	expiry, err := model.ParseExpiryDate("2026-04-18")
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
	if resp.Users[0].ExpiryDate != "2026-04-18" || !resp.Users[0].EffectiveEnabled || resp.Users[0].Expired {
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
