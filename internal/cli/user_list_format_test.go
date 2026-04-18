package cli

import (
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestBuildUserAuditRowsSortsByOperationalRisk(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	expiring, _ := model.ParseExpiryDate("2026-04-19")
	expired, _ := model.ParseExpiryDate("2026-04-17")

	users := []model.User{
		{Username: "ok", Email: "ok@test", Enabled: true, UUID: "aaaaaaaa-0000-0000-0000-000000000000"},
		{Username: "expiring", Email: "expiring@test", Enabled: true, UUID: "bbbbbbbb-0000-0000-0000-000000000000", ExpiryDate: expiring},
		{Username: "expired", Email: "expired@test", Enabled: true, UUID: "cccccccc-0000-0000-0000-000000000000", ExpiryDate: expired},
		{Username: "blocked", Email: "blocked@test", Enabled: true, UUID: "dddddddd-0000-0000-0000-000000000000"},
	}
	quota := map[string]model.QuotaUserStatus{
		"ok@test":       {Window30DUsageByte: 100, Window30DQuotaByte: 200},
		"expiring@test": {Window30DUsageByte: 150, Window30DQuotaByte: 200},
		"expired@test":  {Window30DUsageByte: 50, Window30DQuotaByte: 200},
		"blocked@test":  {Window30DUsageByte: 10, Window30DQuotaByte: 200, BlockedByQuota: true},
	}

	rows := buildUserAuditRows(users, quota, now)
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}
	if got := []string{rows[0].State, rows[1].State, rows[2].State, rows[3].State}; strings.Join(got, ",") != "blocked,expired,expiring,ok" {
		t.Fatalf("unexpected state order: %v", got)
	}
}

func TestRenderUserAuditTableIncludesCompactColumns(t *testing.T) {
	t.Parallel()

	out := renderUserAuditTable([]userAuditRow{{
		Username:   "alice",
		Email:      "alice@example.com",
		State:      "expiring",
		Expiry:     "2026-05-12",
		UsageQuota: "2.00GB / 200.00GB",
		QuotaPctUI: "1.0%",
		UUIDShort:  "fa1050e8",
	}})

	for _, want := range []string{"USER", "EMAIL", "STATE", "EXPIRY", "30D USAGE/QUOTA", "QUOTA %", "fa1050e8", "alice@example.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in table:\n%s", want, out)
		}
	}
}

func TestShortUUID(t *testing.T) {
	t.Parallel()

	if got := shortUUID("05fcf760-e185-434d-8d6b-bc74cc5ff34f"); got != "05fcf760" {
		t.Fatalf("shortUUID mismatch: %q", got)
	}
	if got := shortUUID("abcd"); got != "abcd" {
		t.Fatalf("shortUUID short input mismatch: %q", got)
	}
}
