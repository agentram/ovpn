package operations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"ovpn/internal/observation"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Service struct {
	OnFinish  func(Plan)
	workspace string
	backend   Backend
	parent    context.Context
	mu        sync.Mutex
	running   map[string]context.CancelFunc
	failures  map[string]error
	results   map[string]Result
	wg        sync.WaitGroup
}

func New(ctx context.Context, workspace string, backend Backend) *Service {
	return &Service{workspace: workspace, backend: backend, parent: ctx, running: map[string]context.CancelFunc{}, results: map[string]Result{}, failures: map[string]error{}}
}
func token() string {
	var v [16]byte
	if _, err := rand.Read(v[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(v[:])
}
func validID(s string) bool {
	v, e := hex.DecodeString(s)
	return e == nil && len(v) == 16 && s == hex.EncodeToString(v)
}
func (s *Service) Catalog() []Action { return s.backend.Catalog() }

func (s *Service) Plan(ctx context.Context, request Request) (Plan, error) {
	unlock, err := observation.Lock(filepath.Join(directory(s.workspace), "metadata"))
	if err != nil {
		return Plan{}, ErrBusy
	}
	defer unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.backend.Prepare(ctx, request)
	if err != nil {
		return Plan{}, err
	}
	p.ID, p.CreatedAt = token(), time.Now().UTC()
	if p.Request.Action == "" {
		p.Request = request
	}
	raw, err := json.Marshal(struct {
		Request  Request
		Revision string
		Targets  []Target
	}{p.Request, p.Revision, p.Targets})
	if err != nil {
		return Plan{}, err
	}
	digest := sha256.Sum256(raw)
	p.Digest = hex.EncodeToString(digest[:])
	root, err := privateRoot(s.workspace)
	if err != nil {
		return Plan{}, ErrUnavailable
	}
	defer root.Close()
	r := record{Plan: p, Request: p.Request, Revision: p.Revision, Snapshot: p.Snapshot}
	if save(root, r) != nil {
		return Plan{}, ErrUnavailable
	}
	return p, nil
}

func (s *Service) Start(ctx context.Context, request Start) (Job, error) {
	unlock, err := observation.Lock(filepath.Join(directory(s.workspace), "metadata"))
	if err != nil {
		return Job{}, ErrBusy
	}
	defer unlock()
	if !validID(request.PlanID) || !validID(request.Key) || !request.Confirmed {
		return Job{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := privateRoot(s.workspace)
	if err != nil {
		return Job{}, ErrUnavailable
	}
	defer root.Close()
	r, err := read(root, request.PlanID)
	if err != nil {
		return Job{}, err
	}
	if r.Plan.Digest != request.Digest {
		return Job{}, ErrStale
	}
	if r.Job != nil {
		if r.Key != request.Key {
			return Job{}, ErrInvalid
		}
		return *r.Job, nil
	}
	if time.Since(r.Plan.CreatedAt) > 15*time.Minute {
		return Job{}, ErrStale
	}
	all, err := records(root)
	if err != nil {
		return Job{}, ErrUnavailable
	}
	for _, other := range all {
		if other.Key == request.Key {
			return Job{}, ErrInvalid
		}
	}
	j := Job{ID: r.Plan.ID, PlanID: r.Plan.ID, Action: r.Plan.Action, Title: r.Plan.Title, State: "queued", StartedAt: time.Now().UTC(), Steps: []Step{}}
	for _, t := range r.Plan.Targets {
		j.Steps = append(j.Steps, Step{Target: t, State: "queued"})
	}
	r.Job, r.Key = &j, request.Key
	if err := s.launch(r, false); err != nil {
		return Job{}, err
	}
	return j, nil
}

func (s *Service) launch(r record, recovering bool) error {
	owner, err := observation.Lock(filepath.Join(directory(s.workspace), "job-"+r.Plan.ID))
	if err != nil {
		return ErrBusy
	}
	root, err := privateRoot(s.workspace)
	if err != nil {
		owner()
		return ErrUnavailable
	}
	err = save(root, r)
	root.Close()
	if err != nil {
		owner()
		return ErrUnavailable
	}
	if r.Job != nil {
		copy := *r.Job
		copy.Steps = append([]Step(nil), r.Job.Steps...)
		r.Job = &copy
	}
	ctx, cancel := context.WithCancel(s.parent)
	s.running[r.Plan.ID] = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer owner()
		defer cancel()
		s.execute(ctx, r, recovering)
		s.mu.Lock()
		delete(s.running, r.Plan.ID)
		s.mu.Unlock()
	}()
	return nil
}

func (s *Service) execute(ctx context.Context, r record, recovering bool) {
	defer func() {
		if s.OnFinish != nil && r.Plan.Mutation {
			s.OnFinish(r.Plan)
		}
	}()
	run := func() error {
		root, err := privateRoot(s.workspace)
		if err != nil {
			return ErrUnavailable
		}
		defer root.Close()
		persist := func() error { s.mu.Lock(); defer s.mu.Unlock(); return save(root, r) }
		if recovering {
			if guard, ok := s.backend.(ResumeGuard); ok {
				if e := guard.CanResume(ctx, r.Plan); e != nil {
					return e
				}
			}
		}
		if !recovering {
			revision, err := s.backend.Revision(ctx)
			if err != nil {
				return err
			}
			if revision != r.Plan.Revision {
				return ErrStale
			}
		}
		r.Job.State = "running"
		if err := persist(); err != nil {
			return err
		}
		for i := range r.Job.Steps {
			step := &r.Job.Steps[i]
			if step.State == "succeeded" {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			executeStep := func() error {
				stepCtx := ctx
				if locker, ok := s.backend.(TargetLocker); ok && r.Plan.Mutation {
					lease, release, e := locker.Lock(ctx, r.Plan, step.Target)
					if e != nil {
						step.State, step.Code = "failed", "SERVER_BUSY"
						return persist()
					}
					defer release()
					stepCtx = lease
				}
				step.State, step.Code = "backing_up", ""
				if err := persist(); err != nil {
					return err
				}
				if err := s.backend.Backup(stepCtx, r.Plan, step.Target); err != nil {
					step.State, step.Code = "failed", "BACKUP_FAILED"
					if err := persist(); err != nil {
						return err
					}
					return nil
				}
				step.State = "applying"
				if err := persist(); err != nil {
					return err
				}
				result, err := s.backend.Apply(stepCtx, r.Plan, step.Target, recovering)
				if err != nil {
					step.State, step.Code = "recovery_required", safeCode(err)
					s.mu.Lock()
					s.failures[r.Plan.ID] = err
					s.mu.Unlock()
				} else {
					step.State = "succeeded"
					s.mu.Lock()
					s.results[r.Plan.ID] = result
					s.mu.Unlock()
				}
				if err := persist(); err != nil {
					return err
				}
				return nil
			}
			if err := executeStep(); err != nil {
				return err
			}

			if r.Plan.StopOnFailure && step.State != "succeeded" {
				for j := i + 1; j < len(r.Job.Steps); j++ {
					r.Job.Steps[j].State, r.Job.Steps[j].Code = "blocked", "DEPENDENCY_FAILED"
				}
				if err := persist(); err != nil {
					return err
				}
				break
			}
		}
		return nil
	}
	var err error
	if r.Plan.Mutation {
		err = WithWriter(s.workspace, run)
	} else {
		err = run()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	root, openErr := privateRoot(s.workspace)
	if openErr != nil {
		return
	}
	defer root.Close()
	// Reload when another process owns the journal and preserve its durable state.
	if errors.Is(err, ErrBusy) {
		r.Job.State, r.Job.Code = "blocked", "WORKSPACE_BUSY"
	} else if errors.Is(err, ErrStale) {
		r.Job.State, r.Job.Code = "failed", "STALE_PLAN"
	} else if err != nil {
		r.Job.State, r.Job.Code = "recovery_required", safeCode(err)
	} else {
		successes := 0
		for _, step := range r.Job.Steps {
			if step.State == "succeeded" {
				successes++
			}
		}
		r.Job.State = "succeeded"
		if successes != len(r.Job.Steps) {
			r.Job.State = "partial"
			r.Job.Code = "APPLY_UNVERIFIED"
			if successes == 0 {
				r.Job.State = "failed"
			}
		}
	}
	now := time.Now().UTC()
	r.Job.FinishedAt = &now
	_ = save(root, r)
}

func safeCode(err error) string {
	switch err.Error() {
	case "NO_ENABLED_SERVERS", "IDENTITY_CONFLICT", "USER_ASSIGNMENT_CONFLICT", "SSH_HOST_UNREACHABLE", "SSH_ACCESS_FAILED", "SSH_HOST_KEY_CHANGED", "SERVER_BUSY", "BACKUP_FAILED", "TOOL_UNAVAILABLE", "TRAFFIC_CONFIRMATION_REQUIRED", "DELIVERY_UNAVAILABLE":
		return err.Error()
	}

	for _, e := range []error{ErrBusy, ErrStale, ErrInvalid} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "INTERRUPTED"
	}
	return "APPLY_UNVERIFIED"
}

func (s *Service) List() ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, err := privateRoot(s.workspace)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer root.Close()
	rows, err := records(root)
	if err != nil {
		return nil, ErrUnavailable
	}
	jobs := []Job{}
	for _, r := range rows {
		if r.Job == nil {
			continue
		}
		j := *r.Job
		if _, running := s.running[j.ID]; running && j.State != "queued" && j.State != "running" {
			j.State = "running"
		}
		if _, running := s.running[j.ID]; !running && (j.State == "running" || j.State == "queued") {
			if release, err := observation.Lock(filepath.Join(directory(s.workspace), "job-"+j.ID)); err == nil {
				release()
				j.State = "recovery_required"
				j.Code = "INTERRUPTED"
			}
		}
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].StartedAt.After(jobs[j].StartedAt) })
	if len(jobs) > 100 {
		jobs = jobs[:100]
	}
	return jobs, nil
}

func (s *Service) Resume(id string) (Job, error) {
	unlock, err := observation.Lock(filepath.Join(directory(s.workspace), "metadata"))
	if err != nil {
		return Job{}, ErrBusy
	}
	defer unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.running[id]; ok {
		return Job{}, ErrBusy
	}
	root, err := privateRoot(s.workspace)
	if err != nil {
		return Job{}, ErrUnavailable
	}
	defer root.Close()
	r, err := read(root, id)
	if err != nil || r.Job == nil {
		return Job{}, ErrInvalid
	}
	if r.Job.State == "succeeded" {
		return *r.Job, nil
	}
	r.Job.State = "queued"
	r.Job.FinishedAt = nil
	if r.Job.Code == "STALE_PLAN" {
		return Job{}, ErrStale
	}
	if err := s.launch(r, true); err != nil {
		return Job{}, err
	}
	return *r.Job, nil
}
func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cancel, ok := s.running[id]
	if !ok {
		return ErrInvalid
	}
	cancel()
	return nil
}
func (s *Service) Result(id string) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.results[id]
	if !ok {
		return Result{}, ErrUnavailable
	}
	return r, nil
}
func (s *Service) Close() {
	s.mu.Lock()
	for _, cancel := range s.running {
		cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// Failure is private to the in-process CLI adapter; raw errors never cross the worker protocol.
func (s *Service) Failure(id string) error { s.mu.Lock(); defer s.mu.Unlock(); return s.failures[id] }
