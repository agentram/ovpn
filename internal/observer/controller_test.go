package observer

import (
	"context"
	"errors"
	"os"
	"ovpn/internal/app/dto"
	"ovpn/internal/integrations"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Controller, *atomic.Int64) {
	t.Helper()
	revision := &atomic.Int64{}
	revision.Store(1)
	c := New(context.Background(), t.TempDir(), func(_ context.Context, target dto.ObservationTarget, _ string) (integrations.Input, error) {
		return integrations.Input{Key: TargetKey(target), Revision: string(rune('a' + revision.Load()))}, nil
	})
	c.measure = func(_ context.Context, engine string, _ integrations.Tool, input integrations.Input) dto.Observation {
		return dto.Observation{Source: engine, Revision: input.Revision, Outcome: "passed", StatusCode: 204}
	}
	t.Cleanup(func() { _ = c.Stop() })
	return c, revision
}
func target() dto.ObservationTarget {
	return dto.ObservationTarget{ServerID: "node-1", Username: "monitor", Profile: "vless-reality-tcp-vision"}
}
func policy() dto.ObservationPolicy {
	return dto.ObservationPolicy{Name: "test", Engine: "native", Targets: []dto.ObservationTarget{target()}, IntervalSeconds: 60, MaxChecksPerDay: 1, TrafficConfirmed: true}
}
func awaitJob(t *testing.T, c *Controller, id string) dto.ObserverSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, e := c.Status(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		for _, job := range s.Jobs {
			if job.ID == id && job.State != "running" {
				return s
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return dto.ObserverSnapshot{}
}

func TestOptInOwnershipFreshnessAndPersistentBudget(t *testing.T) {
	ctx := context.Background()
	c, revision := fixture(t)
	p := policy()
	p.Enabled = true
	p.TrafficConfirmed = false
	if _, err := c.Configure(ctx, p); err == nil {
		t.Fatal("scheduled traffic accepted without consent")
	}
	if _, err := os.Stat(filepath.Join(c.dataDir, "observations")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected consent created storage")
	}
	p.Enabled = false
	p.TrafficConfirmed = true
	p, err := c.Configure(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	other := New(ctx, c.dataDir, c.prepare)
	defer other.Stop()
	if _, err := other.Configure(ctx, p); err == nil {
		t.Fatal("second writer accepted")
	}
	job, err := c.RunPolicy(ctx, dto.PolicyRunRequest{PolicyID: p.ID, TrafficConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := awaitJob(t, c, job.ID)
	if len(snapshot.Latest) != 1 || !snapshot.Latest[0].Fresh || snapshot.Latest[0].Outcome != "passed" {
		t.Fatal("fresh sample missing")
	}
	revision.Store(2)
	snapshot, err = c.Status(ctx)
	if err != nil || snapshot.Latest[0].Fresh {
		t.Fatal("changed credentials kept green")
	}
	job, err = c.RunPolicy(ctx, dto.PolicyRunRequest{PolicyID: p.ID, TrafficConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	snapshot = awaitJob(t, c, job.ID)
	if snapshot.Latest[0].Code != "DAILY_ATTEMPT_LIMIT" || snapshot.Latest[0].Outcome != "skipped" {
		t.Fatal("budget exhaustion called target failed")
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = other.Status(ctx)
	if err != nil || snapshot.Running || snapshot.Latest[0].Fresh {
		t.Fatal("stopped observer retained a fresh sample")
	}
}

func TestScheduleRunsWithoutQuerySideEffectsAndCancelsOwnedWork(t *testing.T) {
	ctx := context.Background()
	c, _ := fixture(t)
	var started atomic.Int64
	working := make(chan struct{})
	var once sync.Once
	c.measure = func(ctx context.Context, engine string, _ integrations.Tool, input integrations.Input) dto.Observation {
		started.Add(1)
		once.Do(func() { close(working) })
		<-ctx.Done()
		return dto.Observation{Source: engine, Revision: input.Revision, Outcome: "observer_error"}
	}
	if _, err := c.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if started.Load() != 0 {
		t.Fatal("status triggered traffic")
	}
	p := policy()
	p.Enabled = true
	if _, err := c.Configure(ctx, p); err != nil {
		t.Fatal(err)
	}
	select {
	case <-working:
	case <-time.After(4 * time.Second):
		t.Fatal("automatic policy never ran")
	}
	if _, err := c.Diagnose(ctx, dto.DiagnosticRequest{Engine: "knife-xray", Targets: []dto.ObservationTarget{target()}, TrafficConfirmed: true}); err == nil {
		t.Fatal("overlapping job accepted")
	}
	start := time.Now()
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("stop did not cancel active work")
	}
	snapshot, err := c.Status(ctx)
	if err != nil || len(snapshot.Jobs) != 1 || snapshot.Jobs[0].State != "canceled" {
		t.Fatal("cancellation not journaled")
	}
}

func TestIncidentThresholdsKeepObserverAndTrafficSeparate(t *testing.T) {
	c, _ := fixture(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		t.Fatal(err)
	}
	sample := dto.Observation{Target: target(), Source: "native", Outcome: "failed", MeasuredAt: time.Now().UTC().Format(time.RFC3339)}
	key := "native/" + TargetKey(target()) + "//traffic"
	c.recordIncident(sample)
	if c.incidents[key].State != "suspect" {
		t.Fatal("first failure immediately opened incident")
	}
	c.recordIncident(sample)
	c.recordIncident(sample)
	if c.incidents[key].State != "open" {
		t.Fatal("threshold did not open incident")
	}
	sample.Outcome = "observer_error"
	c.recordIncident(sample)
	if c.incidents[key].Failures != 0 || c.incidents[key].State != "open" {
		t.Fatal("observer failure changed traffic verdict")
	}
	sample.Outcome = "passed"
	c.recordIncident(sample)
	if c.incidents[key].State != "recovering" {
		t.Fatal("one sample resolved incident")
	}
	c.recordIncident(sample)
	if c.incidents[key].State != "resolved" {
		t.Fatal("recovery threshold did not resolve")
	}
}
