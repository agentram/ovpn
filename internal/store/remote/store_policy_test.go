package remote

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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

func TestMigrateLegacySingleInboundPolicyTablesToCompositePrimaryKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	baseDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		t.Fatalf("mkdir base dir: %v", err)
	}
	db, err := sql.Open(sqliteDriver, filepath.Join(baseDir, "stats.db"))
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
	for _, stmt := range []string{
		`CREATE TABLE quota_policy (
			email TEXT PRIMARY KEY,
			uuid TEXT NOT NULL,
			inbound_tag TEXT NOT NULL,
			quota_enabled INTEGER NOT NULL DEFAULT 1,
			monthly_quota_byte INTEGER,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE user_policy (
			email TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			uuid TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			expiry_at TEXT,
			inbound_tag TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO quota_policy (email, uuid, inbound_tag, quota_enabled, monthly_quota_byte, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "alice@example.com", "uuid-alice", "vless-reality", 1, int64(123), now); err != nil {
		t.Fatalf("insert legacy quota policy: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO user_policy (email, username, uuid, enabled, expiry_at, inbound_tag, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "alice@example.com", "alice", "uuid-alice", 1, nil, "vless-reality", now); err != nil {
		t.Fatalf("insert legacy user policy: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy sqlite: %v", err)
	}

	store, err := Open(ctx, baseDir)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()

	if got := policyPrimaryKeyColumns(t, store, "quota_policy"); strings.Join(got, ",") != "email,inbound_tag" {
		t.Fatalf("quota_policy primary key = %v", got)
	}
	if got := policyPrimaryKeyColumns(t, store, "user_policy"); strings.Join(got, ",") != "email,inbound_tag" {
		t.Fatalf("user_policy primary key = %v", got)
	}
	quotaPolicies, err := store.ListQuotaPolicies(ctx)
	if err != nil {
		t.Fatalf("list migrated quota policies: %v", err)
	}
	if len(quotaPolicies) != 1 || quotaPolicies[0].Email != "alice@example.com" || quotaPolicies[0].InboundTag != "vless-reality" {
		t.Fatalf("unexpected migrated quota policies: %+v", quotaPolicies)
	}
	userPolicies, err := store.ListUserPolicies(ctx)
	if err != nil {
		t.Fatalf("list migrated user policies: %v", err)
	}
	if len(userPolicies) != 1 || userPolicies[0].Email != "alice@example.com" || userPolicies[0].InboundTag != "vless-reality" {
		t.Fatalf("unexpected migrated user policies: %+v", userPolicies)
	}
}

func policyPrimaryKeyColumns(t *testing.T, store *Store, table string) []string {
	t.Helper()

	rows, err := store.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("pragma %s: %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan pragma %s: %v", table, err)
		}
		if pk > 0 {
			out = append(out, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma %s: %v", table, err)
	}
	return out
}

func TestPolicyStatusAggregatesMultipleInboundTagsPerUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	quota := int64(500)
	quotaPolicies := []model.QuotaUserPolicy{
		{Email: "alice@example.com", UUID: "uuid-alice", InboundTag: "vless-reality", QuotaEnabled: true, MonthlyQuotaByte: &quota},
		{Email: "alice@example.com", UUID: "uuid-alice", InboundTag: "vless-reality-xhttp", QuotaEnabled: true, MonthlyQuotaByte: &quota},
	}
	if err := store.ReplaceQuotaPolicies(ctx, quotaPolicies); err != nil {
		t.Fatalf("replace quota policies: %v", err)
	}
	storedQuotaPolicies, err := store.ListQuotaPolicies(ctx)
	if err != nil {
		t.Fatalf("list quota policies: %v", err)
	}
	if len(storedQuotaPolicies) != 2 {
		t.Fatalf("expected both inbound quota policies, got %d: %+v", len(storedQuotaPolicies), storedQuotaPolicies)
	}

	userPolicies := []model.UserPolicy{
		{Username: "alice", Email: "alice@example.com", UUID: "uuid-alice", Enabled: true, InboundTag: "vless-reality"},
		{Username: "alice", Email: "alice@example.com", UUID: "uuid-alice", Enabled: true, InboundTag: "vless-reality-xhttp"},
	}
	if err := store.ReplaceUserPolicies(ctx, userPolicies); err != nil {
		t.Fatalf("replace user policies: %v", err)
	}
	storedUserPolicies, err := store.ListUserPolicies(ctx)
	if err != nil {
		t.Fatalf("list user policies: %v", err)
	}
	if len(storedUserPolicies) != 2 {
		t.Fatalf("expected both inbound user policies, got %d: %+v", len(storedUserPolicies), storedUserPolicies)
	}

	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	quotaStatus, err := store.QuotaStatus(ctx, now, 30*24*time.Hour, 1024, "")
	if err != nil {
		t.Fatalf("quota status: %v", err)
	}
	if len(quotaStatus.Users) != 1 {
		t.Fatalf("expected one logical quota user, got %d: %+v", len(quotaStatus.Users), quotaStatus.Users)
	}
	if !strings.Contains(quotaStatus.Users[0].InboundTag, "vless-reality") || !strings.Contains(quotaStatus.Users[0].InboundTag, "vless-reality-xhttp") {
		t.Fatalf("expected aggregate inbound tags, got %+v", quotaStatus.Users[0])
	}

	userStatus, err := store.UserStatus(ctx, now, 30*24*time.Hour, 1024, "")
	if err != nil {
		t.Fatalf("user status: %v", err)
	}
	if len(userStatus.Users) != 1 {
		t.Fatalf("expected one logical user, got %d: %+v", len(userStatus.Users), userStatus.Users)
	}
	if !strings.Contains(userStatus.Users[0].InboundTag, "vless-reality") || !strings.Contains(userStatus.Users[0].InboundTag, "vless-reality-xhttp") {
		t.Fatalf("expected aggregate inbound tags, got %+v", userStatus.Users[0])
	}
}
