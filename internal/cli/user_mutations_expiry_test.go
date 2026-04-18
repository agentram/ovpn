package cli

import (
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestUserAfterExpiryUpdateKeepsEffectiveStateWhenSettingFutureExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	user := model.User{
		Username:     "alice",
		Email:        "alice@example.com",
		Enabled:      true,
		QuotaEnabled: true,
	}
	expiry, err := model.ParseExpiryDate("2026-05-12")
	if err != nil {
		t.Fatalf("parse expiry: %v", err)
	}

	updated, changed := userAfterExpiryUpdate(user, expiry, now)
	if changed {
		t.Fatalf("expected effective state to remain unchanged")
	}
	if !updated.Enabled {
		t.Fatalf("expected user to stay enabled")
	}
	if got := model.ExpiryDateString(updated.ExpiryDate); got != "2026-05-12" {
		t.Fatalf("unexpected expiry date: %q", got)
	}
}

func TestUserAfterExpiryUpdateFutureDateAutoEnablesUser(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	user := model.User{
		Username: "bob",
		Email:    "bob@example.com",
		Enabled:  false,
	}
	expiry, err := model.ParseExpiryDate("2026-06-01")
	if err != nil {
		t.Fatalf("parse expiry: %v", err)
	}

	updated, changed := userAfterExpiryUpdate(user, expiry, now)
	if !changed {
		t.Fatalf("expected effective state to change")
	}
	if !updated.Enabled {
		t.Fatalf("expected future expiry update to enable user")
	}
	if got := model.ExpiryDateString(updated.ExpiryDate); got != "2026-06-01" {
		t.Fatalf("unexpected expiry date: %q", got)
	}
}

func TestUserAfterExpiryUpdateClearAutoEnablesUser(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	future := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	user := model.User{
		Username:   "carol",
		Email:      "carol@example.com",
		Enabled:    false,
		ExpiryDate: &future,
	}

	updated, changed := userAfterExpiryUpdate(user, nil, now)
	if !changed {
		t.Fatalf("expected effective state to change")
	}
	if !updated.Enabled {
		t.Fatalf("expected clear expiry to enable user")
	}
	if updated.ExpiryDate != nil {
		t.Fatalf("expected expiry to be cleared, got %v", updated.ExpiryDate)
	}
}

func TestUserAfterExpiryUpdatePastDateDisablesEffectiveAccessOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	past := time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)
	user := model.User{
		Username: "dave",
		Email:    "dave@example.com",
		Enabled:  true,
	}

	updated, changed := userAfterExpiryUpdate(user, &past, now)
	if !changed {
		t.Fatalf("expected effective state to change")
	}
	if !updated.Enabled {
		t.Fatalf("expected manual enabled flag to remain true")
	}
	if !model.IsExpiredAt(updated.ExpiryDate, now) {
		t.Fatalf("expected user to be expired")
	}
	if model.IsEffectivelyEnabled(updated.Enabled, updated.ExpiryDate, now) {
		t.Fatalf("expected user to be effectively disabled")
	}
}
