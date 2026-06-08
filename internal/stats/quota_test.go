package stats

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/store/remote"
)

type fakeRuntimeManager struct {
	adds      []string
	removes   []string
	addErr    error
	removeErr error
}

func (f *fakeRuntimeManager) AddUser(_ context.Context, _ string, email, _ string) error {
	f.adds = append(f.adds, email)
	return f.addErr
}

func (f *fakeRuntimeManager) RemoveUser(_ context.Context, _ string, email string) error {
	f.removes = append(f.removes, email)
	return f.removeErr
}

func TestQuotaEnforcerBlocksAtExactQuotaBoundaryRollingWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "bob@example.com",
		UUID:             "uuid-bob",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	now := time.Date(2026, 4, 2, 1, 0, 0, 0, time.UTC)
	if err := store.AddDelta(ctx, "bob@example.com", 60, 40, now); err != nil {
		t.Fatalf("add delta: %v", err)
	}

	rt := &fakeRuntimeManager{}
	enforcer := &QuotaEnforcer{Store: store, Runtime: rt}
	if err := enforcer.Enforce(ctx, now); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if len(rt.removes) != 1 || rt.removes[0] != "bob@example.com" {
		t.Fatalf("expected one remove for bob, got %+v", rt.removes)
	}

	status, err := store.QuotaStatus(ctx, now, DefaultQuotaWindow, DefaultWindow30DQuotaBytes, "bob@example.com")
	if err != nil {
		t.Fatalf("quota status: %v", err)
	}
	if len(status.Users) != 1 || !status.Users[0].BlockedByQuota {
		t.Fatalf("expected bob blocked at quota boundary, got %+v", status.Users)
	}
}

func TestQuotaEnforcerDoesNotBlockWhenUsageIsBelowQuotaRollingWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "below@example.com",
		UUID:             "uuid-below",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	now := time.Date(2026, 4, 2, 1, 0, 0, 0, time.UTC)
	if err := store.AddDelta(ctx, "below@example.com", 70, 29, now); err != nil {
		t.Fatalf("add delta: %v", err)
	}

	rt := &fakeRuntimeManager{}
	enforcer := &QuotaEnforcer{Store: store, Runtime: rt}
	if err := enforcer.Enforce(ctx, now); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if len(rt.removes) != 0 {
		t.Fatalf("expected no runtime remove below quota, got %+v", rt.removes)
	}

	status, err := store.QuotaStatus(ctx, now, DefaultQuotaWindow, DefaultWindow30DQuotaBytes, "below@example.com")
	if err != nil {
		t.Fatalf("quota status: %v", err)
	}
	if len(status.Users) != 1 || status.Users[0].BlockedByQuota {
		t.Fatalf("expected user to remain unblocked below quota, got %+v", status.Users)
	}
}

func TestQuotaEnforcerUnblocksWhenUsageFallsOutsideRollingWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "alice@example.com",
		UUID:             "uuid-alice",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	blockAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if err := store.AddDelta(ctx, "alice@example.com", 60, 50, blockAt); err != nil {
		t.Fatalf("add delta: %v", err)
	}

	rt := &fakeRuntimeManager{}
	enforcer := &QuotaEnforcer{Store: store, Runtime: rt}
	if err := enforcer.Enforce(ctx, blockAt); err != nil {
		t.Fatalf("enforce block: %v", err)
	}
	if len(rt.removes) != 1 || rt.removes[0] != "alice@example.com" {
		t.Fatalf("expected one remove for alice, got %+v", rt.removes)
	}

	unblockAt := blockAt.Add(35 * 24 * time.Hour)
	if err := enforcer.Enforce(ctx, unblockAt); err != nil {
		t.Fatalf("enforce unblock: %v", err)
	}
	if len(rt.adds) != 1 || rt.adds[0] != "alice@example.com" {
		t.Fatalf("expected one add for alice after usage window moved, got %+v", rt.adds)
	}

	status, err := store.QuotaStatus(ctx, unblockAt, DefaultQuotaWindow, DefaultWindow30DQuotaBytes, "alice@example.com")
	if err != nil {
		t.Fatalf("quota status: %v", err)
	}
	if len(status.Users) != 1 || status.Users[0].BlockedByQuota {
		t.Fatalf("expected alice unblocked after rolling window moved, got %+v", status.Users)
	}
}

func TestQuotaEnforcerClearsBlockWhenQuotaIsDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "disabled@example.com",
		UUID:             "uuid-disabled",
		InboundTag:       "vless-reality",
		QuotaEnabled:     false,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	blockedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := store.SetQuotaBlocked(ctx, "disabled@example.com", true, &blockedAt); err != nil {
		t.Fatalf("set blocked: %v", err)
	}

	rt := &fakeRuntimeManager{}
	enforcer := &QuotaEnforcer{Store: store, Runtime: rt}
	now := time.Date(2026, 4, 10, 12, 5, 0, 0, time.UTC)
	if err := enforcer.Enforce(ctx, now); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if len(rt.adds) != 1 || rt.adds[0] != "disabled@example.com" {
		t.Fatalf("expected runtime add to clear disabled-policy block, got %+v", rt.adds)
	}
	if len(rt.removes) != 0 {
		t.Fatalf("expected no runtime remove while quota disabled, got %+v", rt.removes)
	}

	state, ok, err := store.GetQuotaState(ctx, "disabled@example.com")
	if err != nil {
		t.Fatalf("get quota state: %v", err)
	}
	if !ok || state.Blocked {
		t.Fatalf("expected block cleared when quota disabled, state=%+v ok=%v", state, ok)
	}
}

func TestQuotaEnforcerUsageBandsRollingWindow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{
		{Email: "u80@example.com", UUID: "uuid-80", InboundTag: "vless-reality", QuotaEnabled: true, MonthlyQuotaByte: &quota},
		{Email: "u95@example.com", UUID: "uuid-95", InboundTag: "vless-reality", QuotaEnabled: true, MonthlyQuotaByte: &quota},
	}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := store.AddDelta(ctx, "u80@example.com", 40, 40, ts); err != nil {
		t.Fatalf("add delta u80: %v", err)
	}
	if err := store.AddDelta(ctx, "u95@example.com", 60, 35, ts); err != nil {
		t.Fatalf("add delta u95: %v", err)
	}

	var got80, got95 int
	enforcer := &QuotaEnforcer{
		Store: store,
		OnUsageBands: func(over80 int, over95 int) {
			got80 = over80
			got95 = over95
		},
	}
	if err := enforcer.Enforce(ctx, ts); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if got80 != 2 || got95 != 1 {
		t.Fatalf("unexpected usage bands, got over80=%d over95=%d", got80, got95)
	}
}

func TestQuotaEnforcerMetricsDeduplicateProfileRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{
		{Email: "shared@example.com", UUID: "uuid-shared", InboundTag: "vless-reality", QuotaEnabled: true, MonthlyQuotaByte: &quota},
		{Email: "shared@example.com", UUID: "uuid-shared", InboundTag: "vless-xhttp-plain", QuotaEnabled: true, MonthlyQuotaByte: &quota},
	}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := store.AddDelta(ctx, "shared@example.com", 100, 0, now); err != nil {
		t.Fatalf("add delta: %v", err)
	}

	var blockedUsers, got80, got95 int
	rt := &fakeRuntimeManager{}
	enforcer := &QuotaEnforcer{
		Store:          store,
		Runtime:        rt,
		OnBlockedUsers: func(blocked int) { blockedUsers = blocked },
		OnUsageBands: func(over80 int, over95 int) {
			got80 = over80
			got95 = over95
		},
	}
	if err := enforcer.Enforce(ctx, now); err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if len(rt.removes) != 2 {
		t.Fatalf("expected runtime remove for both profile inbounds, got %+v", rt.removes)
	}
	if blockedUsers != 1 || got80 != 1 || got95 != 1 {
		t.Fatalf("expected per-user metrics, got blocked=%d over80=%d over95=%d", blockedUsers, got80, got95)
	}
}

func TestQuotaEnforcerRuntimeErrorAndCallbackBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(100)
	if err := store.ReplaceQuotaPolicies(ctx, []model.QuotaUserPolicy{{
		Email:            "broken@example.com",
		UUID:             "uuid-broken",
		InboundTag:       "vless-reality",
		QuotaEnabled:     true,
		MonthlyQuotaByte: &quota,
	}}); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	if err := store.AddDelta(ctx, "broken@example.com", 100, 0, now); err != nil {
		t.Fatalf("add delta: %v", err)
	}

	var events []string
	var blockedUsers int
	var notifications []string
	rt := &fakeRuntimeManager{removeErr: errors.New("xray remove failed")}
	enforcer := &QuotaEnforcer{
		Store:          store,
		Runtime:        rt,
		OnEvent:        func(action, result string) { events = append(events, action+":"+result) },
		OnBlockedUsers: func(blocked int) { blockedUsers = blocked },
		OnNotify:       func(event, message string) { notifications = append(notifications, event+":"+message) },
	}
	err = enforcer.Enforce(ctx, now)
	if err == nil || !strings.Contains(err.Error(), "xray remove failed") {
		t.Fatalf("expected runtime remove error, got %v", err)
	}
	if len(rt.removes) != 1 || rt.removes[0] != "broken@example.com" {
		t.Fatalf("expected remove attempt, got %+v", rt.removes)
	}
	if len(events) != 1 || events[0] != "block:error" {
		t.Fatalf("unexpected events: %+v", events)
	}
	if blockedUsers != 0 || len(notifications) != 0 {
		t.Fatalf("unexpected blockedUsers=%d notifications=%+v", blockedUsers, notifications)
	}
}

func TestQuotaEnforcerNilAndIncompleteRuntimeIdentity(t *testing.T) {
	t.Parallel()

	if err := (*QuotaEnforcer)(nil).Enforce(context.Background(), time.Now()); err != nil {
		t.Fatalf("nil quota enforcer should be no-op: %v", err)
	}
	q := &QuotaEnforcer{}
	if err := q.runtimeAdd(context.Background(), "", "email", "uuid"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected missing runtime error, got %v", err)
	}
	q.Runtime = &fakeRuntimeManager{}
	if err := q.runtimeAdd(context.Background(), "", "email", "uuid"); err == nil || !strings.Contains(err.Error(), "identity is incomplete") {
		t.Fatalf("expected incomplete add identity error, got %v", err)
	}
	if err := q.runtimeRemove(context.Background(), "", "email"); err == nil || !strings.Contains(err.Error(), "identity is incomplete") {
		t.Fatalf("expected incomplete remove identity error, got %v", err)
	}
	if err := combineFirst(errors.New("first"), errors.New("second")); err == nil || err.Error() != "first" {
		t.Fatalf("unexpected combine first result: %v", err)
	}
	if err := combineFirst(nil, errors.New("second")); err == nil || err.Error() != "second" {
		t.Fatalf("unexpected combine first nil result: %v", err)
	}
}
