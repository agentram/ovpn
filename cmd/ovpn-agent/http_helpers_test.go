package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ovpn/internal/model"
)

type fakeQuotaPolicyStore struct {
	policies []model.QuotaUserPolicy
	err      error
}

func (f *fakeQuotaPolicyStore) ListQuotaPolicies(_ context.Context) ([]model.QuotaUserPolicy, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]model.QuotaUserPolicy(nil), f.policies...), nil
}

func TestEnvInt64(t *testing.T) {
	const key = "OVPN_AGENT_TEST_INT64"
	t.Setenv(key, "")
	if got := envInt64(key, 7); got != 7 {
		t.Fatalf("envInt64 empty = %d, want 7", got)
	}
	t.Setenv(key, "15")
	if got := envInt64(key, 7); got != 15 {
		t.Fatalf("envInt64 parsed = %d, want 15", got)
	}
	t.Setenv(key, "bad")
	if got := envInt64(key, 7); got != 7 {
		t.Fatalf("envInt64 invalid = %d, want fallback 7", got)
	}
}

func TestHandleQuotaPoliciesMethodNotAllowedWithFakeStore(t *testing.T) {
	t.Parallel()

	h := handleQuotaPolicies(&fakeQuotaPolicyStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	req := httptest.NewRequest(http.MethodPost, "/quota/policies", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleQuotaPoliciesSuccessWithFakeStore(t *testing.T) {
	t.Parallel()

	store := &fakeQuotaPolicyStore{
		policies: []model.QuotaUserPolicy{
			{Email: "alice@test", UUID: "u1", InboundTag: "vless-reality"},
			{Email: "bob@test", UUID: "u2", InboundTag: "vless-reality"},
		},
	}
	h := handleQuotaPolicies(store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	req := httptest.NewRequest(http.MethodGet, "/quota/policies", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got []model.QuotaUserPolicy
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 || got[0].Email != "alice@test" || got[1].Email != "bob@test" {
		t.Fatalf("unexpected policies: %+v", got)
	}
}

func TestHandleQuotaPoliciesStoreError(t *testing.T) {
	t.Parallel()

	h := handleQuotaPolicies(&fakeQuotaPolicyStore{err: errors.New("db down")}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	req := httptest.NewRequest(http.MethodGet, "/quota/policies", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "db down") {
		t.Fatalf("expected error message in body, got %q", rec.Body.String())
	}
}

func TestWithRequestLoggingPreservesStatusCode(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := withRequestLogging(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
}
