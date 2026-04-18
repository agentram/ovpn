package main

import (
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestSetPromptSkipsEmptyKind(t *testing.T) {
	t.Parallel()

	b := &bot{prompts: map[int64]promptState{}}
	b.setPrompt(11, " ")
	if len(b.prompts) != 0 {
		t.Fatalf("expected no prompt for empty kind")
	}
}

func TestGetPromptExpiresAndClears(t *testing.T) {
	t.Parallel()

	b := &bot{prompts: map[int64]promptState{
		11: {Kind: promptUserLink, ExpiresAt: time.Now().UTC().Add(-time.Second)},
	}}
	if _, ok := b.getPrompt(11); ok {
		t.Fatalf("expected expired prompt to be unavailable")
	}
	if _, exists := b.prompts[11]; exists {
		t.Fatalf("expected expired prompt to be removed")
	}
}

func TestQuotaPressureBoundaries(t *testing.T) {
	t.Parallel()

	status := model.QuotaStatusResponse{
		Users: []model.QuotaUserStatus{
			{Email: "u1", QuotaEnabled: true, Window30DUsageByte: 80, Window30DQuotaByte: 100},
			{Email: "u2", QuotaEnabled: true, Window30DUsageByte: 95, Window30DQuotaByte: 100},
			{Email: "u3", QuotaEnabled: true, Window30DUsageByte: 79, Window30DQuotaByte: 100},
			{Email: "u4", QuotaEnabled: false, Window30DUsageByte: 99, Window30DQuotaByte: 100},
		},
	}
	over80, over95 := quotaPressure(status)
	if over80 != 2 || over95 != 1 {
		t.Fatalf("unexpected pressure counts: over80=%d over95=%d", over80, over95)
	}
}

func TestTrafficSummaryActiveAndTotal(t *testing.T) {
	t.Parallel()

	rows := []model.UserTraffic{
		{Email: "a", UplinkBytes: 100, DownlinkBytes: 200},
		{Email: "b", UplinkBytes: 0, DownlinkBytes: 0},
		{Email: "c", UplinkBytes: 50, DownlinkBytes: 0},
	}
	users, active, total := trafficSummary(rows)
	if users != 3 || active != 2 || total != 350 {
		t.Fatalf("unexpected summary: users=%d active=%d total=%d", users, active, total)
	}
}

func TestQuotaPercentAndFormatBytes(t *testing.T) {
	t.Parallel()

	if quotaPercent(0, 100) != 0 {
		t.Fatalf("expected zero percent for zero usage")
	}
	if got := quotaPercent(50, 200); got != 25 {
		t.Fatalf("expected 25%%, got %.2f", got)
	}
	if got := formatBytes(1024); got != "1.00 KB" {
		t.Fatalf("unexpected format for 1KB: %s", got)
	}
	if got := formatBytes(-10); got != "0 B" {
		t.Fatalf("unexpected format for negative bytes: %s", got)
	}
}
