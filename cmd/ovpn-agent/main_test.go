package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"ovpn/internal/model"
	"ovpn/internal/store/remote"
)

type quotaPolicyListerStub struct {
	policies []model.QuotaUserPolicy
	err      error
}

func (s quotaPolicyListerStub) ListQuotaPolicies(ctx context.Context) ([]model.QuotaUserPolicy, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.policies, nil
}

func TestHandleQuotaPoliciesMethodNotAllowed(t *testing.T) {
	t.Parallel()

	h := handleQuotaPolicies(quotaPolicyListerStub{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/quota/policies", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleQuotaPoliciesSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "alice@test",
		UUID:             "uuid-1",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}

	h := handleQuotaPolicies(store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/quota/policies", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if rr.Body.String() == "" || rr.Body.String() == "null\n" {
		t.Fatalf("expected non-empty body")
	}
}

func TestHandleQuotaPoliciesError(t *testing.T) {
	t.Parallel()

	h := handleQuotaPolicies(quotaPolicyListerStub{err: errors.New("boom")}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/quota/policies", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusInternalServerError)
	}
}
