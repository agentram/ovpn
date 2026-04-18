package main

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"ovpn/internal/model"
)

func TestSetUserExpiryStatusExportsExpiryGauges(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := newAgentMetrics(reg)
	expiry := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	days := 1.5

	m.setUserExpiryStatus(model.UserStatusResponse{
		Expiring2DUsers: 1,
		ExpiredUsers:    1,
		Users: []model.UserAccessStatus{
			{
				Email:            "alice@example.com",
				ExpiryAt:         &expiry,
				ExpiryDate:       "2026-04-18",
				Expired:          false,
				EffectiveEnabled: true,
				DaysUntilExpiry:  &days,
			},
			{
				Email:            "bob@example.com",
				Expired:          true,
				EffectiveEnabled: false,
			},
		},
	})

	if got := testutil.ToFloat64(m.usersExpiring2D); got != 1 {
		t.Fatalf("unexpected expiring_2d gauge: %v", got)
	}
	if got := testutil.ToFloat64(m.usersExpired); got != 1 {
		t.Fatalf("unexpected expired gauge: %v", got)
	}
	if got := testutil.ToFloat64(m.userEffectiveEnabled.WithLabelValues("alice@example.com", "2026-04-18")); got != 1 {
		t.Fatalf("unexpected alice effective_enabled gauge: %v", got)
	}
	if got := testutil.ToFloat64(m.userDaysUntilExpiry.WithLabelValues("alice@example.com", "2026-04-18")); got != 1.5 {
		t.Fatalf("unexpected alice days_until_expiry gauge: %v", got)
	}
	if got := testutil.ToFloat64(m.userExpired.WithLabelValues("bob@example.com", "none")); got != 1 {
		t.Fatalf("unexpected bob expired gauge: %v", got)
	}
	if got := testutil.ToFloat64(m.userDaysUntilExpiry.WithLabelValues("bob@example.com", "none")); got != -10000 {
		t.Fatalf("unexpected bob no-expiry sentinel: %v", got)
	}
}
