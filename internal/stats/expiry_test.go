package stats

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/store/remote"
)

func TestExpiryEnforcerRemovesExpiredUsers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	expiry := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@example.com",
		UUID:       "uuid-alice",
		Enabled:    true,
		ExpiryAt:   &expiry,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}

	rt := &fakeRuntimeManager{}
	enforcer := &ExpiryEnforcer{Store: store, Runtime: rt}
	now := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	if err := enforcer.Enforce(ctx, now); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if len(rt.removes) != 1 || rt.removes[0] != "alice@example.com" {
		t.Fatalf("expected expired user removal, got %+v", rt.removes)
	}
}

func TestExpiryEnforcerDoesNotReAddActiveUsers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	expiry := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@example.com",
		UUID:       "uuid-alice",
		Enabled:    true,
		ExpiryAt:   &expiry,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}

	rt := &fakeRuntimeManager{}
	enforcer := &ExpiryEnforcer{Store: store, Runtime: rt}
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	if err := enforcer.Enforce(ctx, now); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if len(rt.adds) != 0 {
		t.Fatalf("expected no runtime add for active user, got %+v", rt.adds)
	}
}

func TestExpiryEnforcerSkipsQuotaBlockedActiveUsers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	expiry := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@example.com",
		UUID:       "uuid-alice",
		Enabled:    true,
		ExpiryAt:   &expiry,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}
	blockedAt := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	if err := store.SetQuotaBlocked(ctx, "alice@example.com", true, &blockedAt); err != nil {
		t.Fatalf("set quota blocked: %v", err)
	}

	rt := &fakeRuntimeManager{}
	enforcer := &ExpiryEnforcer{Store: store, Runtime: rt}
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	if err := enforcer.Enforce(ctx, now); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if len(rt.adds) != 0 {
		t.Fatalf("expected no add while quota blocked, got %+v", rt.adds)
	}
}
