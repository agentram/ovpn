package main

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBotHealthTransitionsAndMetrics(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	metrics := newBotMetrics(reg)
	health := newBotHealth(10*time.Second, metrics)
	now := time.Now().UTC()

	if got := health.staleAfter(); got != 2*time.Minute {
		t.Fatalf("short poll interval staleAfter=%v", got)
	}
	health.onPollSuccess(now)
	health.onSendSuccess(now)
	snap := health.snapshot(now.Add(time.Minute))
	if !snap.OK || snap.Status != "healthy" || snap.WatchdogUnhealthy {
		t.Fatalf("expected healthy snapshot, got %+v", snap)
	}
	if got := testutil.ToFloat64(metrics.pollFailuresConsec); got != 0 {
		t.Fatalf("unexpected poll failures metric: %v", got)
	}

	health.onPollFailure(now, errors.New("poll failed"))
	health.onSendFailure(errors.New("send failed"))
	health.onSendFailure(errors.New("send failed again"))
	health.onSendFailure(nil)
	snap = health.snapshot(now.Add(time.Minute))
	if snap.Status != "degraded" || snap.ConsecutiveSendFailures != 3 || snap.LastPollFailure != "poll failed" {
		t.Fatalf("expected degraded send-failure snapshot, got %+v", snap)
	}
	if got := testutil.ToFloat64(metrics.sendFailuresConsec); got != 3 {
		t.Fatalf("unexpected send failures metric: %v", got)
	}

	health.lastPollSuccess = now.Add(-5 * time.Minute)
	snap = health.snapshot(now)
	if snap.Status != "degraded" || !snap.WatchdogUnhealthy {
		t.Fatalf("expected stale polling snapshot, got %+v", snap)
	}
}

func TestBotEnsureHealthCreatesMissingState(t *testing.T) {
	t.Parallel()

	b := newBotTestHarness(t, &telegramRecorder{}, false)
	b.health = nil
	if got := b.ensureHealth(); got == nil {
		t.Fatalf("expected health object")
	}
	if snap := b.healthSnapshot(); !snap.OK {
		t.Fatalf("unexpected unhealthy snapshot: %+v", snap)
	}

	health := newBotHealth(time.Minute, nil)
	if got := health.staleAfter(); got != 3*time.Minute {
		t.Fatalf("long poll interval staleAfter=%v", got)
	}
	health.startedAt = time.Now().UTC().Add(-10 * time.Minute)
	snap := health.snapshot(time.Now().UTC())
	if snap.Status != "degraded" || !snap.WatchdogUnhealthy {
		t.Fatalf("expected never-polled stale snapshot, got %+v", snap)
	}
}
