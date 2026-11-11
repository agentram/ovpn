package deskmonitor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/observation"
	"ovpn/internal/observer"
)

type sourceFixture struct {
	mu      sync.Mutex
	targets []Target
	entered chan Target
	release chan struct{}
	live    []dto.ServerLive
	calls   int
}

func (s *sourceFixture) Targets(context.Context) ([]Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Target(nil), s.targets...), nil
}
func (s *sourceFixture) Telemetry(context.Context, bool) ([]dto.ServerLive, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live, nil
}
func (s *sourceFixture) Measure(ctx context.Context, t Target) dto.Observation {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.entered != nil {
		s.entered <- t
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return dto.Observation{Outcome: "failed", Code: "TIMEOUT"}
		}
	}
	return dto.Observation{Outcome: "passed"}
}
func testController(t *testing.T, s *sourceFixture) (*Controller, *time.Time) {
	t.Helper()
	c := New(context.Background(), t.TempDir(), s)
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	c.pulse = clock
	c.enabled = true
	var err error
	c.store, err = observation.Open(c.workspace, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, &clock
}
func testTarget(node, user string) Target {
	return Target{Target: dto.ObservationTarget{ServerID: node, Username: user, Profile: "tls-self-sni-web"}, Engine: "native", Revision: "1"}
}

func TestQueueIncludesEveryAssignmentWithoutBlockingOtherServers(t *testing.T) {
	s := &sourceFixture{entered: make(chan Target, 100), release: make(chan struct{})}
	for n := 0; n < 10; n++ {
		for u := 0; u < 100; u++ {
			s.targets = append(s.targets, testTarget(fmt.Sprint(n), fmt.Sprint(u)))
		}
	}
	c, clock := testController(t, s)
	var wg sync.WaitGroup
	c.tick(context.Background(), &wg)
	if got := c.Snapshot(); got.Total != 1000 || got.Active != 4 {
		t.Fatalf("incomplete coverage or concurrency: %+v", got)
	}
	nodes := map[string]bool{}
	for range 4 {
		select {
		case r := <-s.entered:
			if nodes[r.Target.ServerID] {
				t.Fatal("concurrent probes on the same server")
			}
			nodes[r.Target.ServerID] = true
		case <-time.After(time.Second):
			t.Fatal("queue stalled")
		}
	}
	// The current four remain blocked: another tick must not start a fifth.
	c.tick(context.Background(), &wg)
	select {
	case <-s.entered:
		t.Fatal("exceeded global limit")
	default:
	}
	close(s.release)
	wg.Wait()
	*clock = clock.Add(time.Second)
	c.pulse = *clock
	c.tick(context.Background(), &wg)
	wg.Wait()
	if len(s.entered) != 4 {
		t.Fatal("unfinished nodes did not continue independently")
	}
}
func TestProbeDoesNotCountAsConnectedAndMissingTrafficStaysUnknown(t *testing.T) {
	yes := true
	s := &sourceFixture{targets: []Target{testTarget("node-1", "alice")}, live: []dto.ServerLive{{ServerID: "node-1", State: "online", Users: []dto.UserLive{{Username: "alice", Connected: &yes, PresenceAt: "2026-01-01T12:00:00Z"}}}}}
	c, clock := testController(t, s)
	c.cancel = func() {}
	c.done = make(chan struct{})
	close(c.done)
	c.refreshTargets(context.Background())
	c.collect(context.Background(), false)
	row := c.Snapshot().Servers[0].Users[0]
	if row.Connected != nil || row.PresenceCode != "CHECK_IN_PROGRESS" || row.UsedBytes != nil {
		t.Fatal("probe activity presented as human presence or absent traffic as zero")
	}
	*clock = clock.Add(PresenceQuietPeriod + time.Second)
	c.pulse = *clock
	s.live[0].Users[0].PresenceAt = clock.Format(time.RFC3339)
	c.collect(context.Background(), false)
	row = c.Snapshot().Servers[0].Users[0]
	if row.Connected == nil || !*row.Connected {
		t.Fatal("independent presence never became visible")
	}
	// No UI heartbeat means previously green evidence becomes unknown.
	*clock = clock.Add(time.Minute)
	if row = c.Snapshot().Servers[0].Users[0]; row.Connected != nil {
		t.Fatal("sleep retained fresh connected state")
	}
	c.cancel = nil // no run goroutine was created in this deterministic test.
}
func TestNewAssignmentsAndChangedProfilesAreCheckedImmediately(t *testing.T) {
	s := &sourceFixture{targets: []Target{testTarget("node-1", "alice")}}
	c, clock := testController(t, s)
	var wg sync.WaitGroup
	c.tick(context.Background(), &wg)
	wg.Wait()
	original := c.Routes("", "")[0]
	if original.State != "passed" || original.NextAt.Sub(*clock) != CheckInterval {
		t.Fatal("incorrect check cadence")
	}
	*clock = clock.Add(time.Second)
	c.pulse = *clock
	c.tick(context.Background(), &wg)
	wg.Wait()
	if s.calls != 1 {
		t.Fatal("repeated a fresh successful check")
	}
	c.Invalidate()
	c.tick(context.Background(), &wg)
	wg.Wait()
	if s.calls != 1 {
		t.Fatal("inventory refresh repeated unrelated fresh traffic checks")
	}
	s.targets = append(s.targets, testTarget("node-2", "bob"))
	s.targets[0].Revision = "changed"
	c.Invalidate()
	c.tick(context.Background(), &wg)
	wg.Wait()
	if s.calls != 3 || c.Snapshot().Total != 2 {
		t.Fatal("new or changed access was not checked immediately")
	}
	delete(c.routes, observer.TargetKey(s.targets[1].Target))
	s.targets = s.targets[:1]
	c.Invalidate()
	c.tick(context.Background(), &wg)
	wg.Wait()
	if c.Snapshot().Total != 1 {
		t.Fatal("removed assignment remains in coverage")
	}
}
func TestLifecycleCanRestartAndBackwardClockInvalidatesEvidence(t *testing.T) {
	s := &sourceFixture{}
	c := New(context.Background(), t.TempDir(), s)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	c.Close()
	now := time.Now().UTC()
	sample := &dto.Observation{MeasuredAt: now.Format(time.RFC3339Nano)}
	if freshSample(sample, now.Add(-time.Second), CheckInterval) || freshSample(sample, now.Add(CheckInterval), CheckInterval) {
		t.Fatal("invalid clock or expired sample marked fresh")
	}
}
