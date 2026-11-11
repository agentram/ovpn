package operations

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testBackend struct {
	mu       sync.Mutex
	revision string
	calls    map[string]int
	fail     string
	block    chan struct{}
	entered  chan struct{}
}

func (b *testBackend) Catalog() []Action { return []Action{{ID: "user.add", Mutation: true}} }
func (b *testBackend) Prepare(_ context.Context, r Request) (Plan, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Plan{Action: r.Action, Title: "Add user", Mutation: true, Revision: b.revision, Targets: []Target{{ID: "one", Name: "First"}, {ID: "two", Name: "Second"}}, Snapshot: json.RawMessage(`{"secret":"private-fixture-value"}`)}, nil
}
func (b *testBackend) Revision(context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.revision, nil
}
func (b *testBackend) Backup(context.Context, Plan, Target) error { return nil }
func (b *testBackend) Apply(ctx context.Context, _ Plan, target Target, recovering bool) (Result, error) {
	if b.entered != nil {
		select {
		case b.entered <- struct{}{}:
		default:
		}
	}
	if b.block != nil {
		select {
		case <-b.block:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail == target.ID {
		return Result{}, errors.New("remote fixture failure containing private-fixture-value")
	}
	if !recovering || b.calls[target.ID] == 0 {
		b.calls[target.ID]++
	}
	return Result{Content: "private-fixture-value"}, nil
}
func fixtureService(t *testing.T) (*Service, *testBackend) {
	t.Helper()
	b := &testBackend{revision: "first", calls: map[string]int{}}
	s := New(context.Background(), t.TempDir(), b)
	t.Cleanup(s.Close)
	return s, b
}
func prepareStart(t *testing.T, s *Service) (Plan, Start) {
	t.Helper()
	p, e := s.Plan(context.Background(), Request{Action: "user.add", Values: map[string]string{"password": "private-fixture-value"}})
	if e != nil {
		t.Fatal(e)
	}
	return p, Start{PlanID: p.ID, Digest: p.Digest, Key: token(), Confirmed: true}
}
func finished(t *testing.T, s *Service, id string) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rows, e := s.List()
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range rows {
			if r.ID == id && r.State != "queued" && r.State != "running" {
				return r
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not terminate")
	return Job{}
}

func TestRepeatedSubmissionKeepsOneOperationAndPrivateJournal(t *testing.T) {
	s, b := fixtureService(t)
	p, r := prepareStart(t, s)
	first, e := s.Start(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Start(context.Background(), r)
	if e != nil || first.ID != second.ID {
		t.Fatal("duplicate submission was not idempotent", e)
	}
	if j := finished(t, s, first.ID); j.State != "succeeded" {
		t.Fatal(j)
	}
	b.mu.Lock()
	if b.calls["one"] != 1 || b.calls["two"] != 1 {
		t.Fatal("a duplicate write occurred")
	}
	b.mu.Unlock()
	root, e := os.OpenRoot(directory(s.workspace))
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	raw, e := root.ReadFile(p.ID + ".json.enc")
	if e != nil || strings.Contains(string(raw), "private-fixture-value") {
		t.Fatal("plaintext intent in the journal", e)
	}
	info, e := os.Stat(filepath.Join(directory(s.workspace), p.ID+".json.enc"))
	if e != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("journal permissions", e)
	}
	encoded, _ := json.Marshal(p)
	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "private-fixture-value") {
		t.Fatal("private intent in plan projection")
	}
}
func TestChangedSnapshotAndMissingConfirmationDoNotWrite(t *testing.T) {
	s, b := fixtureService(t)
	_, r := prepareStart(t, s)
	r.Confirmed = false
	if _, e := s.Start(context.Background(), r); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	r.Confirmed = true
	b.mu.Lock()
	b.revision = "changed"
	b.mu.Unlock()
	job, e := s.Start(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if j := finished(t, s, job.ID); j.Code != "STALE_PLAN" {
		t.Fatal(j)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.calls) != 0 {
		t.Fatal("a stale plan wrote state")
	}
}
func TestPartialApplyResumesOnlyUnfinishedTargets(t *testing.T) {
	s, b := fixtureService(t)
	b.fail = "two"
	_, r := prepareStart(t, s)
	j, e := s.Start(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	first := finished(t, s, j.ID)
	if first.State != "partial" || first.Steps[1].State != "recovery_required" {
		t.Fatal(first)
	}
	b.mu.Lock()
	b.fail = ""
	b.mu.Unlock()
	if _, e = s.Resume(j.ID); e != nil {
		t.Fatal(e)
	}
	// Resume must make the durable state running before it returns.
	end := finished(t, s, j.ID)
	if end.State != "succeeded" {
		t.Fatal(end)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.calls["one"] != 1 || b.calls["two"] != 1 {
		t.Fatal("resumed a completed node", b.calls)
	}
}
func TestWriterLockBlocksAnotherEntryPoint(t *testing.T) {
	s, b := fixtureService(t)
	b.block = make(chan struct{})
	b.entered = make(chan struct{}, 1)
	_, r := prepareStart(t, s)
	job, e := s.Start(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not enter")
	}
	if e := WithWriter(s.workspace, func() error { t.Fatal("parallel writer entered"); return nil }); !errors.Is(e, ErrBusy) {
		t.Fatal(e)
	}
	close(b.block)
	if j := finished(t, s, job.ID); j.State != "succeeded" {
		t.Fatal(j)
	}
}
func TestRestartKeepsResultAndDoesNotRepeatWrites(t *testing.T) {
	s, b := fixtureService(t)
	_, r := prepareStart(t, s)
	j, e := s.Start(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	finished(t, s, j.ID)
	s.Close()
	next := New(context.Background(), s.workspace, b)
	defer next.Close()
	rows, e := next.List()
	if e != nil || len(rows) != 1 || rows[0].State != "succeeded" {
		t.Fatal(rows, e)
	}
	if _, e = next.Resume(j.ID); e != nil {
		t.Fatal(e)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.calls["one"] != 1 || b.calls["two"] != 1 {
		t.Fatal("restart repeated a write")
	}
	if _, e = next.Result(j.ID); e == nil {
		t.Fatal("private delivery result survived restart")
	}
}
