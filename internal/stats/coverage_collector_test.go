package stats

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/store/remote"
)

type fakeXrayStatsClient struct {
	stats map[string]int64
	err   error
}

func (f fakeXrayStatsClient) Close() error { return nil }

func (f fakeXrayStatsClient) QueryStats(context.Context, string, bool) (map[string]int64, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.stats, nil
}

type collectorObserver struct {
	starts      int
	finishes    int
	users       int
	active      int
	resets      int
	dbErrors    int
	reachable   []bool
	spikeDeltas []int64
}

func (o *collectorObserver) OnCollectStart() { o.starts++ }
func (o *collectorObserver) OnCollectFinish(_ time.Duration, users int, _ error) {
	o.finishes++
	o.users = users
}
func (o *collectorObserver) OnCounterReset()           { o.resets++ }
func (o *collectorObserver) OnDBWriteError(string)     { o.dbErrors++ }
func (o *collectorObserver) OnXrayAPIReachable(v bool) { o.reachable = append(o.reachable, v) }
func (o *collectorObserver) OnUsersActive(count int)   { o.active = count }
func (o *collectorObserver) OnUserSpike(deltaBytes int64) {
	o.spikeDeltas = append(o.spikeDeltas, deltaBytes)
}

func TestCollectorCollectOnceSuccessWithObserverAndSpike(t *testing.T) {
	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open remote store: %v", err)
	}
	defer store.Close()

	restore := replaceXrayStatsClient(func(context.Context, string) (xrayStatsClient, error) {
		return fakeXrayStatsClient{stats: map[string]int64{
			"user>>>alice@test>>>traffic>>>uplink":   100,
			"user>>>alice@test>>>traffic>>>downlink": 50,
			"user>>>bob@test>>>traffic>>>uplink":     0,
			"user>>>bob@test>>>traffic>>>downlink":   0,
		}}, nil
	})
	defer restore()

	obs := &collectorObserver{}
	c := &Collector{Store: store, APIAddr: "fake", Observer: obs, SpikeDeltaBytes: 120}
	if err := c.CollectOnce(ctx); err != nil {
		t.Fatalf("collect once: %v", err)
	}
	if obs.starts != 1 || obs.finishes != 1 || obs.users != 2 || obs.active != 1 {
		t.Fatalf("unexpected observer state: %+v", obs)
	}
	if len(obs.reachable) != 1 || !obs.reachable[0] {
		t.Fatalf("expected xray reachable observer, got %+v", obs.reachable)
	}
	if len(obs.spikeDeltas) != 1 || obs.spikeDeltas[0] != 150 {
		t.Fatalf("expected one spike delta 150, got %+v", obs.spikeDeltas)
	}
	if _, ok, err := store.GetMeta(ctx, "last_collect_at"); err != nil || !ok {
		t.Fatalf("expected last_collect_at meta, ok=%v err=%v", ok, err)
	}
	totals, err := store.ListTotals(ctx)
	if err != nil {
		t.Fatalf("list totals: %v", err)
	}
	if len(totals) != 1 || totals[0].Email != "alice@test" || totals[0].UplinkBytes != 100 || totals[0].DownlinkBytes != 50 {
		t.Fatalf("unexpected totals: %+v", totals)
	}
}

func TestCollectorCollectOnceClientAndQueryErrors(t *testing.T) {
	ctx := context.Background()
	store, err := remote.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open remote store: %v", err)
	}
	defer store.Close()

	obs := &collectorObserver{}
	restore := replaceXrayStatsClient(func(context.Context, string) (xrayStatsClient, error) {
		return nil, errors.New("dial failed")
	})
	err = (&Collector{Store: store, APIAddr: "fake", Observer: obs}).CollectOnce(ctx)
	restore()
	if err == nil || !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("expected dial failure, got %v", err)
	}
	if len(obs.reachable) != 1 || obs.reachable[0] {
		t.Fatalf("expected unreachable observer, got %+v", obs.reachable)
	}

	obs = &collectorObserver{}
	restore = replaceXrayStatsClient(func(context.Context, string) (xrayStatsClient, error) {
		return fakeXrayStatsClient{err: errors.New("query failed")}, nil
	})
	err = (&Collector{Store: store, APIAddr: "fake", Observer: obs}).CollectOnce(ctx)
	restore()
	if err == nil || !strings.Contains(err.Error(), "query failed") {
		t.Fatalf("expected query failure, got %v", err)
	}
	if len(obs.reachable) != 1 || obs.reachable[0] {
		t.Fatalf("expected query unreachable observer, got %+v", obs.reachable)
	}
}

func TestCollectorRunStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store, err := remote.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open remote store: %v", err)
	}
	defer store.Close()

	restore := replaceXrayStatsClient(func(context.Context, string) (xrayStatsClient, error) {
		return fakeXrayStatsClient{stats: map[string]int64{}}, nil
	})
	defer restore()

	err = (&Collector{Store: store, APIAddr: "fake", Interval: time.Millisecond}).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func replaceXrayStatsClient(fn func(context.Context, string) (xrayStatsClient, error)) func() {
	previous := newXrayStatsClient
	newXrayStatsClient = fn
	return func() { newXrayStatsClient = previous }
}
