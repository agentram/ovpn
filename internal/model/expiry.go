package model

import (
	"strings"
	"time"
)

const ExpiryDateLayout = "2006-01-02"

// ParseExpiryDate parses YYYY-MM-DD and returns the exclusive UTC cutoff at the
// next midnight so the user stays active through the whole calendar day.
func ParseExpiryDate(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	day, err := time.ParseInLocation(ExpiryDateLayout, raw, time.UTC)
	if err != nil {
		return nil, err
	}
	cutoff := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, time.UTC)
	return &cutoff, nil
}

// ExpiryDateString formats an exclusive expiry cutoff back to operator-facing
// YYYY-MM-DD semantics.
func ExpiryDateString(expiry *time.Time) string {
	if expiry == nil {
		return ""
	}
	return expiry.UTC().Add(-time.Nanosecond).Format(ExpiryDateLayout)
}

// IsExpiredAt reports whether the exclusive expiry cutoff has passed.
func IsExpiredAt(expiry *time.Time, now time.Time) bool {
	if expiry == nil {
		return false
	}
	return !now.UTC().Before(expiry.UTC())
}

// IsEffectivelyEnabled reports whether manual enabled state remains active after
// applying expiry semantics.
func IsEffectivelyEnabled(enabled bool, expiry *time.Time, now time.Time) bool {
	return enabled && !IsExpiredAt(expiry, now)
}

// DaysUntilExpiry returns remaining days until the exclusive expiry cutoff.
func DaysUntilExpiry(expiry *time.Time, now time.Time) (float64, bool) {
	if expiry == nil {
		return 0, false
	}
	return expiry.UTC().Sub(now.UTC()).Hours() / 24, true
}
