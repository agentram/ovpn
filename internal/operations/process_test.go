package operations

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type fileBackend struct {
	dir   string
	pause bool
}

func (b fileBackend) Catalog() []Action { return nil }
func (b fileBackend) Prepare(_ context.Context, r Request) (Plan, error) {
	return Plan{Action: r.Action, Mutation: true, Revision: "one", Targets: []Target{{ID: "one", Name: "First"}, {ID: "two", Name: "Second"}}}, nil
}
func (b fileBackend) Revision(context.Context) (string, error)   { return "one", nil }
func (b fileBackend) Backup(context.Context, Plan, Target) error { return nil }
func (b fileBackend) Apply(ctx context.Context, _ Plan, target Target, recovery bool) (Result, error) {
	marker := filepath.Join(b.dir, "applied-"+target.ID)
	if recovery {
		if _, e := os.Stat(marker); e == nil {
			return Result{}, nil
		}
	}
	f, err := os.OpenFile(marker, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return Result{}, err
	}
	_, err = f.WriteString("applied\n")
	_ = f.Close()
	if err != nil {
		return Result{}, err
	}
	if b.pause && target.ID == "one" {
		<-ctx.Done()
		return Result{}, ctx.Err()
	}
	return Result{}, nil
}

// The helper is a real second process so ownership cannot be inferred from an
// in-memory map, and killing it skips every Go defer and checkpoint.
func TestOperationProcessHelper(t *testing.T) {
	mode := os.Getenv("OVPN_OPERATION_HELPER")
	if mode == "" {
		return
	}
	dir := os.Getenv("OVPN_OPERATION_WORKSPACE")
	if mode == "lock" {
		if e := WithWriter(dir, func() error {
			if e := os.WriteFile(filepath.Join(dir, "entered"), nil, 0o600); e != nil {
				return e
			}
			for {
				time.Sleep(time.Second)
			}
		}); e != nil {
			os.Exit(2)
		}
		return
	}
	s := New(context.Background(), dir, fileBackend{dir: dir, pause: true})
	p, e := s.Plan(context.Background(), Request{Action: "user.add"})
	if e != nil {
		os.Exit(2)
	}
	if e = os.WriteFile(filepath.Join(dir, "plan-id"), []byte(p.ID), 0o600); e != nil {
		os.Exit(2)
	}
	if _, e = s.Start(context.Background(), Start{PlanID: p.ID, Digest: p.Digest, Key: token(), Confirmed: true}); e != nil {
		os.Exit(2)
	}
	for {
		time.Sleep(time.Second)
	}
}
func helper(t *testing.T, dir, mode, marker string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOperationProcessHelper$")
	cmd.Env = append(os.Environ(), "OVPN_OPERATION_HELPER="+mode, "OVPN_OPERATION_WORKSPACE="+dir)
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, marker)); e == nil {
			return cmd
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("helper did not reach the durable checkpoint")
	return nil
}
func TestWriterLockExcludesIndependentProcesses(t *testing.T) {
	dir := t.TempDir()
	process := helper(t, dir, "lock", "entered")
	if e := WithWriter(dir, func() error { t.Fatal("parallel process entered a locked workspace"); return nil }); e != ErrBusy {
		t.Fatal(e)
	}
	_ = process.Process.Kill()
	_ = process.Wait()
	if e := WithWriter(dir, func() error { return nil }); e != nil {
		t.Fatal("dead process retained the writer lock", e)
	}
}
func TestCrashAfterRemoteWriteReconcilesWithoutRepeatingIt(t *testing.T) {
	dir := t.TempDir()
	process := helper(t, dir, "crash", "applied-one")
	_ = process.Process.Kill()
	_ = process.Wait()
	raw, e := os.ReadFile(filepath.Join(dir, "plan-id"))
	if e != nil {
		t.Fatal(e)
	}
	service := New(context.Background(), dir, fileBackend{dir: dir})
	defer service.Close()
	jobs, e := service.List()
	if e != nil || len(jobs) != 1 || jobs[0].State != "recovery_required" {
		t.Fatal("lost interrupted write", e)
	}
	if _, e = service.Resume(string(raw)); e != nil {
		t.Fatal(e)
	}
	if job := finished(t, service, string(raw)); job.State != "succeeded" {
		t.Fatal(job)
	}
	for _, node := range []string{"one", "two"} {
		value, e := os.ReadFile(filepath.Join(dir, "applied-"+node))
		if e != nil || string(value) != "applied\n" {
			t.Fatal("write was repeated or skipped", e)
		}
	}
}
