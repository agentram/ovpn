package stats

import (
	"context"
	"os"
	"testing"
	"time"

	"ovpn/internal/store/remote"
)

func TestConsumeCounterDetectsReset(t *testing.T) {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "ovpn-stats-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	s, err := remote.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c := &Collector{Store: s}
	now := time.Now().UTC()
	if _, err := c.consumeCounter(ctx, "user>>>u@example.com>>>traffic>>>uplink", "u@example.com", 100, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.consumeCounter(ctx, "user>>>u@example.com>>>traffic>>>uplink", "u@example.com", 10, true, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	totals, err := s.ListTotals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(totals) != 1 {
		t.Fatalf("expected 1 total row, got %d", len(totals))
	}
	if totals[0].UplinkBytes != 110 {
		t.Fatalf("expected 110 uplink bytes, got %d", totals[0].UplinkBytes)
	}
	lastReset, ok, err := s.GetMeta(ctx, "last_reset_at")
	if err != nil {
		t.Fatalf("get last_reset_at: %v", err)
	}
	if !ok || lastReset == "" {
		t.Fatalf("expected last_reset_at to be set after counter reset")
	}
}

func TestCollectorSpikeThresholdDefault(t *testing.T) {
	t.Parallel()

	c := &Collector{}
	if got := c.spikeThresholdBytes(); got != DefaultUserSpikeDeltaBytes {
		t.Fatalf("unexpected default threshold: %d", got)
	}
}

func TestCollectorSpikeThresholdCustom(t *testing.T) {
	t.Parallel()

	c := &Collector{SpikeDeltaBytes: 12345}
	if got := c.spikeThresholdBytes(); got != 12345 {
		t.Fatalf("unexpected custom threshold: %d", got)
	}
}
