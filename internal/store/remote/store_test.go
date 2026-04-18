package remote

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestCounterAndAggregates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.UpsertCounter(ctx, "user>>>alice>>>traffic>>>uplink", 10); err != nil {
		t.Fatalf("upsert counter: %v", err)
	}
	c, ok, err := store.GetCounter(ctx, "user>>>alice>>>traffic>>>uplink")
	if err != nil {
		t.Fatalf("get counter: %v", err)
	}
	if !ok || c.Value != 10 {
		t.Fatalf("unexpected counter: ok=%v value=%d", ok, c.Value)
	}

	now := time.Date(2026, 4, 5, 12, 1, 0, 0, time.UTC)
	if err := store.AddDelta(ctx, "alice@example.com", 100, 200, now); err != nil {
		t.Fatalf("add delta #1: %v", err)
	}
	if err := store.AddDelta(ctx, "alice@example.com", 50, 30, now.Add(10*time.Minute)); err != nil {
		t.Fatalf("add delta #2: %v", err)
	}

	totals, err := store.ListTotals(ctx)
	if err != nil {
		t.Fatalf("list totals: %v", err)
	}
	if len(totals) != 1 || totals[0].UplinkBytes != 150 || totals[0].DownlinkBytes != 230 {
		t.Fatalf("unexpected totals: %+v", totals)
	}

	daily, err := store.ListDaily(ctx, now)
	if err != nil {
		t.Fatalf("list daily: %v", err)
	}
	if len(daily) != 1 || daily[0].UplinkBytes != 150 || daily[0].DownlinkBytes != 230 {
		t.Fatalf("unexpected daily rows: %+v", daily)
	}
}

func TestQuotaPolicyAndStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	customQuota := int64(10)
	users := []model.QuotaUserPolicy{
		{
			Email:            "alice@example.com",
			UUID:             "uuid-alice",
			InboundTag:       "vless-reality",
			QuotaEnabled:     true,
			MonthlyQuotaByte: &customQuota,
		},
		{
			Email:        "bob@example.com",
			UUID:         "uuid-bob",
			InboundTag:   "vless-reality",
			QuotaEnabled: true,
		},
	}
	if err := store.ReplaceQuotaPolicies(ctx, users); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	if err := store.AddDelta(ctx, "alice@example.com", 3, 4, time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add delta alice: %v", err)
	}
	blockedAt := time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)
	if err := store.SetQuotaBlocked(ctx, "alice@example.com", true, &blockedAt); err != nil {
		t.Fatalf("set blocked: %v", err)
	}

	now := time.Date(2026, 4, 6, 1, 0, 0, 0, time.UTC)
	status, err := store.QuotaStatus(ctx, now, 30*24*time.Hour, 300, "")
	if err != nil {
		t.Fatalf("quota status: %v", err)
	}
	if status.Window30DStart == "" || status.Window30DEnd == "" {
		t.Fatalf("expected rolling window bounds, got %+v", status)
	}
	if len(status.Users) != 2 {
		t.Fatalf("unexpected users count: %d", len(status.Users))
	}
	if status.BlockedUsers != 1 {
		t.Fatalf("unexpected blocked users: %d", status.BlockedUsers)
	}
	if status.Users[0].Email != "alice@example.com" {
		t.Fatalf("expected sorted users, got %+v", status.Users)
	}
	if status.Users[0].Window30DUsageByte != 7 {
		t.Fatalf("unexpected alice usage: %d", status.Users[0].Window30DUsageByte)
	}
	if status.Users[0].Window30DQuotaByte != 10 {
		t.Fatalf("unexpected alice quota: %d", status.Users[0].Window30DQuotaByte)
	}
	if !status.Users[0].BlockedByQuota {
		t.Fatalf("expected alice blocked")
	}
	if status.Users[1].Window30DQuotaByte != 300 {
		t.Fatalf("expected default quota for bob, got %d", status.Users[1].Window30DQuotaByte)
	}

	state, ok, err := store.GetQuotaState(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("get quota state: %v", err)
	}
	if !ok {
		t.Fatalf("expected quota state for alice")
	}
	if !state.Blocked {
		t.Fatalf("unexpected quota state: %+v", state)
	}
}

func TestListUsageBetweenRollingWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.AddDelta(ctx, "alice@example.com", 100, 0, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add old delta: %v", err)
	}
	if err := store.AddDelta(ctx, "alice@example.com", 10, 20, time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add current delta: %v", err)
	}

	end := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	start := end.Add(-30 * 24 * time.Hour)
	usage, err := store.ListUsageBetween(ctx, start, end)
	if err != nil {
		t.Fatalf("ListUsageBetween: %v", err)
	}
	if usage["alice@example.com"] != 30 {
		t.Fatalf("expected rolling usage 30, got %d", usage["alice@example.com"])
	}
}

func TestUserPoliciesAndStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	expiry := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{
		{
			Username:   "alice",
			Email:      "alice@example.com",
			UUID:       "uuid-alice",
			Enabled:    true,
			ExpiryAt:   &expiry,
			InboundTag: "vless-reality",
		},
		{
			Username:   "bob",
			Email:      "bob@example.com",
			UUID:       "uuid-bob",
			Enabled:    false,
			InboundTag: "vless-reality",
		},
	}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}
	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{
		{Email: "alice@example.com", UUID: "uuid-alice", InboundTag: "vless-reality", QuotaEnabled: true, MonthlyQuotaByte: &quota},
	}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	if err := store.AddDelta(ctx, "alice@example.com", 30, 20, time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("add delta: %v", err)
	}

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	status, err := store.UserStatus(ctx, now, 30*24*time.Hour, 200, "")
	if err != nil {
		t.Fatalf("user status: %v", err)
	}
	if len(status.Users) != 2 {
		t.Fatalf("unexpected user count: %d", len(status.Users))
	}
	if status.EffectiveEnabledUsers != 1 {
		t.Fatalf("unexpected effective enabled count: %d", status.EffectiveEnabledUsers)
	}
	if status.Expiring2DUsers != 1 {
		t.Fatalf("unexpected expiring_2d count: %d", status.Expiring2DUsers)
	}
	if status.Users[0].ExpiryDate != "2026-04-17" {
		t.Fatalf("unexpected expiry date: %+v", status.Users[0])
	}
	if !status.Users[0].EffectiveEnabled || status.Users[0].Expired {
		t.Fatalf("unexpected alice state: %+v", status.Users[0])
	}
}

func TestUserStatusBoundaryCasesAndFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	expiringAt, err := model.ParseExpiryDate("2026-04-18")
	if err != nil {
		t.Fatalf("parse expiring date: %v", err)
	}
	farFutureAt, err := model.ParseExpiryDate("2026-04-25")
	if err != nil {
		t.Fatalf("parse far future date: %v", err)
	}
	expiredAt := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)

	if err := store.ReplaceUserPolicies(ctx, []model.UserPolicy{
		{
			Username:   "alice",
			Email:      "alice@example.com",
			UUID:       "uuid-alice",
			Enabled:    true,
			ExpiryAt:   expiringAt,
			InboundTag: "vless-reality",
		},
		{
			Username:   "bob",
			Email:      "bob@example.com",
			UUID:       "uuid-bob",
			Enabled:    true,
			InboundTag: "vless-reality",
		},
		{
			Username:   "carol",
			Email:      "carol@example.com",
			UUID:       "uuid-carol",
			Enabled:    true,
			ExpiryAt:   &expiredAt,
			InboundTag: "vless-reality",
		},
		{
			Username:   "dave",
			Email:      "dave@example.com",
			UUID:       "uuid-dave",
			Enabled:    false,
			ExpiryAt:   farFutureAt,
			InboundTag: "vless-reality",
		},
	}); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}

	status, err := store.UserStatus(ctx, now, 30*24*time.Hour, 200, "")
	if err != nil {
		t.Fatalf("user status: %v", err)
	}
	if got := len(status.Users); got != 4 {
		t.Fatalf("unexpected user count: %d", got)
	}
	if status.EffectiveEnabledUsers != 2 {
		t.Fatalf("unexpected effective enabled count: %d", status.EffectiveEnabledUsers)
	}
	if status.Expiring2DUsers != 1 {
		t.Fatalf("unexpected expiring_2d count: %d", status.Expiring2DUsers)
	}
	if status.ExpiredUsers != 1 {
		t.Fatalf("unexpected expired count: %d", status.ExpiredUsers)
	}
	if status.Users[1].Email != "bob@example.com" || status.Users[1].DaysUntilExpiry != nil || status.Users[1].ExpiryDate != "" {
		t.Fatalf("unexpected no-expiry user row: %+v", status.Users[1])
	}
	if !status.Users[2].Expired || status.Users[2].EffectiveEnabled {
		t.Fatalf("unexpected expired user row: %+v", status.Users[2])
	}
	if status.Users[3].EffectiveEnabled {
		t.Fatalf("expected disabled user to stay ineffective: %+v", status.Users[3])
	}

	filtered, err := store.UserStatus(ctx, now, 30*24*time.Hour, 200, "bob@example.com")
	if err != nil {
		t.Fatalf("filtered user status: %v", err)
	}
	if len(filtered.Users) != 1 || filtered.Users[0].Email != "bob@example.com" {
		t.Fatalf("unexpected filtered users: %+v", filtered.Users)
	}
	if filtered.EffectiveEnabledUsers != 1 || filtered.Expiring2DUsers != 0 || filtered.ExpiredUsers != 0 {
		t.Fatalf("unexpected filtered counts: %+v", filtered)
	}
}
