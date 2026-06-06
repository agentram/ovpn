package remote

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestPolicyLookupMetaAndClearQuotaState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(123)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "alice@example.com",
		UUID:             "uuid-alice",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	policy, ok, err := store.GetQuotaPolicy(ctx, "alice@example.com")
	if err != nil || !ok || policy.UUID != "uuid-alice" {
		t.Fatalf("unexpected quota policy: policy=%+v ok=%v err=%v", policy, ok, err)
	}
	if _, ok, err := store.GetQuotaPolicy(ctx, "missing@example.com"); err != nil || ok {
		t.Fatalf("expected missing quota policy, ok=%v err=%v", ok, err)
	}
	blockedAt := time.Now().UTC()
	if err := store.SetQuotaBlocked(ctx, "alice@example.com", true, &blockedAt); err != nil {
		t.Fatalf("set quota blocked: %v", err)
	}
	if err := store.ClearQuotaState(ctx, "alice@example.com"); err != nil {
		t.Fatalf("clear quota state: %v", err)
	}
	if _, ok, err := store.GetQuotaState(ctx, "alice@example.com"); err != nil || ok {
		t.Fatalf("expected quota state cleared, ok=%v err=%v", ok, err)
	}

	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{{
		Username:   "alice",
		Email:      "alice@example.com",
		UUID:       "uuid-alice",
		Enabled:    true,
		InboundTag: "vless-reality",
	}}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}
	userPolicy, ok, err := store.GetUserPolicy(ctx, "alice@example.com")
	if err != nil || !ok || userPolicy.Username != "alice" {
		t.Fatalf("unexpected user policy: policy=%+v ok=%v err=%v", userPolicy, ok, err)
	}
	if _, ok, err := store.GetUserPolicy(ctx, "missing@example.com"); err != nil || ok {
		t.Fatalf("expected missing user policy, ok=%v err=%v", ok, err)
	}

	if err := store.SetMeta(ctx, "last_collect_at", "2099-01-02T03:04:05Z"); err != nil {
		t.Fatalf("set meta: %v", err)
	}
	value, ok, err := store.GetMeta(ctx, "last_collect_at")
	if err != nil || !ok || value != "2099-01-02T03:04:05Z" {
		t.Fatalf("unexpected meta value=%q ok=%v err=%v", value, ok, err)
	}
	if _, ok, err := store.GetMeta(ctx, "missing"); err != nil || ok {
		t.Fatalf("expected missing meta, ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetCounter(ctx, "missing"); err != nil || ok {
		t.Fatalf("expected missing counter, ok=%v err=%v", ok, err)
	}
}
