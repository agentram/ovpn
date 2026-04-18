package model

import (
	"testing"
	"time"
)

func TestParseExpiryDateStoresExclusiveUTCCutoff(t *testing.T) {
	t.Parallel()

	got, err := ParseExpiryDate("2026-04-17")
	if err != nil {
		t.Fatalf("ParseExpiryDate: %v", err)
	}
	if got == nil {
		t.Fatalf("expected expiry cutoff")
	}
	want := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected cutoff: got %s want %s", got.UTC().Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestParseExpiryDateEmptyAndInvalid(t *testing.T) {
	t.Parallel()

	got, err := ParseExpiryDate("   ")
	if err != nil {
		t.Fatalf("ParseExpiryDate empty: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil expiry for empty input, got %v", got)
	}

	if _, err := ParseExpiryDate("2026/04/17"); err == nil {
		t.Fatalf("expected parse error for invalid format")
	}
}

func TestExpiryDateStringFormatsOperatorDate(t *testing.T) {
	t.Parallel()

	expiry := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	if got := ExpiryDateString(&expiry); got != "2026-04-17" {
		t.Fatalf("unexpected expiry date string: %q", got)
	}
	if got := ExpiryDateString(nil); got != "" {
		t.Fatalf("unexpected expiry date string for nil: %q", got)
	}
}

func TestIsExpiredAtUsesExclusiveCutoff(t *testing.T) {
	t.Parallel()

	expiry := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	if IsExpiredAt(&expiry, time.Date(2026, 4, 17, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("user should still be active before cutoff")
	}
	if !IsExpiredAt(&expiry, time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("user should expire at cutoff")
	}
}

func TestIsEffectivelyEnabled(t *testing.T) {
	t.Parallel()

	expiry := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	if !IsEffectivelyEnabled(true, &expiry, now) {
		t.Fatalf("expected enabled user before expiry")
	}
	if IsEffectivelyEnabled(false, &expiry, now) {
		t.Fatalf("manual disable should win")
	}
	if IsEffectivelyEnabled(true, &expiry, expiry) {
		t.Fatalf("expiry should disable user at cutoff")
	}
}

func TestDaysUntilExpiry(t *testing.T) {
	t.Parallel()

	expiry := time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	got, ok := DaysUntilExpiry(&expiry, now)
	if !ok {
		t.Fatalf("expected expiry interval to be present")
	}
	if got != 0.5 {
		t.Fatalf("unexpected days until expiry: got %v want 0.5", got)
	}
	if got, ok := DaysUntilExpiry(nil, now); ok || got != 0 {
		t.Fatalf("expected nil expiry to return zero,false, got %v,%v", got, ok)
	}
}
