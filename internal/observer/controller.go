package observer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"ovpn/internal/app/dto"
	"ovpn/internal/integrations"
	"ovpn/internal/observation"
)

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }
func fail(code string) error   { return &Error{Code: code} }

type Prepare func(context.Context, dto.ObservationTarget, string) (integrations.Input, error)
type execution struct {
	job    dto.ObservationJob
	cancel context.CancelFunc
	done   chan struct{}
}

type Controller struct {
	mu         sync.Mutex
	parent     context.Context
	dataDir    string
	prepare    Prepare
	runner     integrations.Runner
	measure    func(context.Context, string, integrations.Tool, integrations.Input) dto.Observation
	now        func() time.Time
	store      *observation.Store
	unlock     func()
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	session    string
	monitoring bool
	closing    bool
	policies   map[string]dto.ObservationPolicy
	tools      map[string]integrations.Tool
	next       map[string]time.Time
	incidents  map[string]dto.ObservationIncident
	active     *execution
}

func New(ctx context.Context, dataDir string, prepare Prepare) *Controller {
	return &Controller{parent: ctx, dataDir: dataDir, prepare: prepare, now: time.Now, runner: integrations.Runner{WorkDir: filepath.Join(observation.Directory(dataDir), "work")}}
}

func TargetKey(target dto.ObservationTarget) string {
	raw, _ := json.Marshal(target)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

func validTarget(t dto.ObservationTarget) bool {
	return strings.HasPrefix(t.ServerID, "node-") && len(t.ServerID) <= 64 && t.Username != "" && len(t.Username) <= 128 && utf8.ValidString(t.Username) && strings.TrimSpace(t.Username) == t.Username && strings.IndexFunc(t.Username, unicode.IsControl) < 0 && len(t.Profile) > 0 && len(t.Profile) <= 128
}

func validatePolicy(p dto.ObservationPolicy) error {
	if p.Engine != "native" && p.Engine != "checker" || len(p.Targets) < 1 || len(p.Targets) > 10 || p.IntervalSeconds < 60 || p.IntervalSeconds > 3600 || p.MaxChecksPerDay < 1 || p.MaxChecksPerDay > 2000 || len(p.Name) > 64 || !utf8.ValidString(p.Name) || strings.IndexFunc(p.Name, unicode.IsControl) >= 0 {
		return fail("INVALID_PARAMS")
	}
	if p.Enabled && !p.TrafficConfirmed {
		return fail("TRAFFIC_CONFIRMATION_REQUIRED")
	}
	seen := map[string]bool{}
	for _, target := range p.Targets {
		key := TargetKey(target)
		if !validTarget(target) || seen[key] {
			return fail("INVALID_PARAMS")
		}
		seen[key] = true
	}
	return nil
}

func (c *Controller) ensureLocked() error {
	if c.closing {
		return fail("OBSERVER_STOPPING")
	}
	if c.store != nil {
		return nil
	}
	unlock, err := observation.Lock(c.dataDir)
	if err != nil {
		return fail("OBSERVER_IN_USE")
	}
	st, err := observation.Open(c.dataDir, true)
	if err != nil {
		unlock()
		return fail("OBSERVATION_UNAVAILABLE")
	}
	c.store, c.unlock = st, unlock
	c.policies = map[string]dto.ObservationPolicy{}
	c.tools = map[string]integrations.Tool{}
	c.next = map[string]time.Time{}
	c.incidents = map[string]dto.ObservationIncident{}
	load := func(table string, fn func(json.RawMessage) error) error {
		rows, err := st.List(c.parent, table)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if err := fn(row); err != nil {
				return err
			}
		}
		return nil
	}
	err = load("policies", func(raw json.RawMessage) error {
		var p dto.ObservationPolicy
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if err := validatePolicy(p); err != nil {
			return err
		}
		c.policies[p.ID] = p
		return nil
	})
	if err == nil {
		err = load("tools", func(raw json.RawMessage) error {
			var t integrations.Tool
			if err := json.Unmarshal(raw, &t); err != nil {
				return err
			}
			c.tools[t.Engine] = t
			return nil
		})
	}
	if err == nil {
		err = load("incidents", func(raw json.RawMessage) error {
			var i dto.ObservationIncident
			if err := json.Unmarshal(raw, &i); err != nil {
				return err
			}
			i.Failures = 0
			i.Successes = 0
			c.incidents[i.Source+"/"+TargetKey(i.Target)+"/"+i.Revision+"/"+i.Class] = i
			return nil
		})
	}
	if err == nil {
		err = load("jobs", func(raw json.RawMessage) error {
			var j dto.ObservationJob
			if err := json.Unmarshal(raw, &j); err != nil {
				return err
			}
			if j.State == "running" {
				j.State = "interrupted"
				j.Code = "PREVIOUS_PROCESS_STOPPED"
				j.FinishedAt = c.now().UTC().Format(time.RFC3339)
				return st.Save(context.Background(), "jobs", j.ID, j)
			}
			return nil
		})
	}
	if err != nil {
		_ = st.Close()
		unlock()
		c.store = nil
		return fail("OBSERVATION_UNAVAILABLE")
	}
	// Exclusive ownership makes leftover private job files safe to remove after a crash.
	if err := observation.PrivateDir(c.runner.WorkDir); err != nil {
		_ = st.Close()
		unlock()
		c.store = nil
		return fail("OBSERVATION_UNAVAILABLE")
	}
	entries, _ := os.ReadDir(c.runner.WorkDir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "job-") {
			_ = os.RemoveAll(filepath.Join(c.runner.WorkDir, entry.Name()))
		}
	}
	c.session = integrations.Token()
	c.ctx, c.cancel = context.WithCancel(c.parent)
	c.done = make(chan struct{})
	c.monitoring = false
	_ = st.Set(c.parent, "previous_heartbeat", st.Setting(c.parent, "heartbeat"))
	_ = st.Set(c.parent, "session", c.session)
	_ = st.Set(c.parent, "heartbeat", c.now().UTC().Format(time.RFC3339))
	_ = st.Set(c.parent, "monitoring", "false")
	go c.loop(c.ctx, c.done)
	return nil
}

func (c *Controller) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := c.now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			now := c.now()
			if now.UTC().Before(last.UTC()) || now.UTC().Sub(last.UTC()) > 15*time.Second {
				c.session = integrations.Token()
				_ = c.store.Set(context.Background(), "session", c.session)
				for id := range c.next {
					c.next[id] = now.Add(time.Second)
				}
				for key, incident := range c.incidents {
					incident.Failures = 0
					incident.Successes = 0
					c.incidents[key] = incident
				}
			}
			last = now
			_ = c.store.Set(context.Background(), "heartbeat", now.UTC().Format(time.RFC3339))
			if c.monitoring && c.active == nil {
				var due *dto.ObservationPolicy
				var earliest time.Time
				for id, p := range c.policies {
					if p.Enabled && !now.Before(c.next[id]) && (due == nil || c.next[id].Before(earliest)) {
						copy := p
						due = &copy
						earliest = c.next[id]
					}
				}
				if due != nil {
					_, _ = c.startCheckLocked(*due, "scheduled")
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *Controller) Configure(ctx context.Context, p dto.ObservationPolicy) (dto.ObservationPolicy, error) {
	p.Targets = append([]dto.ObservationTarget(nil), p.Targets...)
	if err := validatePolicy(p); err != nil {
		return p, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return p, err
	}
	if p.ID == "" {
		if len(c.policies) >= 10 {
			return p, fail("POLICY_LIMIT")
		}
		p.ID = integrations.Token()
	} else if _, ok := c.policies[p.ID]; !ok {
		return p, fail("POLICY_NOT_FOUND")
	}
	if p.Enabled {
		if p.Engine == "checker" && c.tools["checker"].Path == "" {
			return p, fail("TOOL_UNAVAILABLE")
		}
		for _, target := range p.Targets {
			if _, err := c.prepare(ctx, target, p.Engine); err != nil {
				return p, fail("TARGET_UNAVAILABLE")
			}
			for id, other := range c.policies {
				if id == p.ID || !other.Enabled || other.Engine != p.Engine {
					continue
				}
				for _, ref := range other.Targets {
					if TargetKey(ref) == TargetKey(target) {
						return p, fail("DUPLICATE_SCHEDULE")
					}
				}
			}
		}
	}
	if c.active != nil && c.active.job.PolicyID == p.ID {
		if p.Enabled {
			return p, fail("JOB_BUSY")
		}
		c.active.cancel()
	}
	if err := c.store.Save(ctx, "policies", p.ID, p); err != nil {
		return p, fail("OBSERVATION_UNAVAILABLE")
	}
	c.policies[p.ID] = p
	c.next[p.ID] = c.now().Add(time.Second)
	if p.Enabled {
		c.monitoring = true
		_ = c.store.Set(ctx, "monitoring", "true")
	}
	return p, nil
}

func (c *Controller) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return err
	}
	enabled := false
	for id, p := range c.policies {
		if p.Enabled {
			enabled = true
			c.next[id] = c.now().Add(time.Second)
		}
	}
	if !enabled {
		return fail("NO_ENABLED_POLICIES")
	}
	c.monitoring = true
	return c.store.Set(ctx, "monitoring", "true")
}

func (c *Controller) Stop() error {
	c.mu.Lock()
	if c.store == nil {
		c.mu.Unlock()
		return nil
	}
	cancel, done := c.cancel, c.done
	c.closing = true
	active := c.active
	cancel()
	if active != nil {
		active.cancel()
	}
	c.mu.Unlock()
	<-done
	if active != nil {
		<-active.done
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store == nil {
		return nil
	}
	_ = c.store.Set(context.Background(), "monitoring", "false")
	_ = c.store.Set(context.Background(), "heartbeat", "")
	err := c.store.Close()
	c.unlock()
	c.store = nil
	c.active = nil
	c.monitoring = false
	c.closing = false
	return err
}

func (c *Controller) RemovePolicy(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return err
	}
	if c.active != nil && c.active.job.PolicyID == id {
		c.active.cancel()
	}
	if _, ok := c.policies[id]; !ok {
		return fail("POLICY_NOT_FOUND")
	}
	if c.store.RemovePolicy(ctx, id) != nil {
		return fail("OBSERVATION_UNAVAILABLE")
	}
	delete(c.policies, id)
	delete(c.next, id)
	return nil
}

func (c *Controller) RunPolicy(_ context.Context, request dto.PolicyRunRequest) (dto.ObservationJob, error) {
	if !request.TrafficConfirmed {
		return dto.ObservationJob{}, fail("TRAFFIC_CONFIRMATION_REQUIRED")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return dto.ObservationJob{}, err
	}
	p, ok := c.policies[request.PolicyID]
	if !ok {
		return dto.ObservationJob{}, fail("POLICY_NOT_FOUND")
	}
	return c.startCheckLocked(p, "manual")
}

func (c *Controller) Diagnose(_ context.Context, request dto.DiagnosticRequest) (dto.ObservationJob, error) {
	if request.Engine != "knife-xray" && request.Engine != "knife-singbox" || len(request.Targets) == 0 || len(request.Targets) > 10 {
		return dto.ObservationJob{}, fail("INVALID_PARAMS")
	}
	if !request.TrafficConfirmed {
		return dto.ObservationJob{}, fail("TRAFFIC_CONFIRMATION_REQUIRED")
	}
	for _, t := range request.Targets {
		if !validTarget(t) {
			return dto.ObservationJob{}, fail("INVALID_PARAMS")
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return dto.ObservationJob{}, err
	}
	return c.startCheckLocked(dto.ObservationPolicy{Engine: request.Engine, Targets: request.Targets, MaxChecksPerDay: 20, IntervalSeconds: 300}, "diagnostic")
}

func (c *Controller) startCheckLocked(p dto.ObservationPolicy, kind string) (dto.ObservationJob, error) {
	if c.active != nil {
		return dto.ObservationJob{}, fail("JOB_BUSY")
	}
	tool := c.tools[p.Engine]
	if strings.HasPrefix(p.Engine, "knife-") {
		tool = c.tools["knife"]
	}
	if p.Engine != "native" && tool.Path == "" {
		return dto.ObservationJob{}, fail("TOOL_UNAVAILABLE")
	}
	ctx, cancel := context.WithCancel(c.ctx)
	j := dto.ObservationJob{ID: integrations.Token(), Kind: kind, PolicyID: p.ID, Engine: p.Engine, State: "running", Total: len(p.Targets), StartedAt: c.now().UTC().Format(time.RFC3339)}
	if c.store.Save(context.Background(), "jobs", j.ID, j) != nil {
		cancel()
		return j, fail("OBSERVATION_UNAVAILABLE")
	}
	x := &execution{job: j, cancel: cancel, done: make(chan struct{})}
	c.active = x
	session := c.session
	go c.execute(ctx, x, p, tool, session)
	return j, nil
}

func (c *Controller) execute(ctx context.Context, x *execution, p dto.ObservationPolicy, tool integrations.Tool, session string) {
	defer close(x.done)
	defer x.cancel()
	for _, target := range p.Targets {
		if ctx.Err() != nil {
			break
		}
		input, err := c.prepare(ctx, target, p.Engine)
		sample := dto.Observation{Source: p.Engine, EngineVersion: integrations.Version(p.Engine), Outcome: "skipped", FailureClass: "policy", Code: "TARGET_UNAVAILABLE", Bytes: -1, Unverified: []string{"runtime", "udp", "dns", "ipv6", "gui-import", "wire-bytes"}}
		if err == nil {
			c.mu.Lock()
			reserved, reserveErr := c.store.Reserve(context.Background(), p.Engine+"/"+TargetKey(target), c.now(), p.MaxChecksPerDay)
			c.mu.Unlock()
			if reserveErr != nil {
				sample.Outcome = "observer_error"
				sample.FailureClass = "observer"
				sample.Code = "OBSERVATION_UNAVAILABLE"
			} else if !reserved {
				sample.FailureClass = "budget"
				sample.Code = "DAILY_ATTEMPT_LIMIT"
			} else {
				if c.measure != nil {
					sample = c.measure(ctx, p.Engine, tool, input)
				} else {
					sample = c.runner.Run(ctx, p.Engine, tool, input)
				}
			}
		}
		c.mu.Lock()
		sample.Target = target
		sample.JobID = x.job.ID
		sample.SessionID = session
		sample.MeasuredAt = c.now().UTC().Format(time.RFC3339)
		sample.FreshForSeconds = p.IntervalSeconds * 2
		if ctx.Err() == nil {
			if c.store.Add(context.Background(), p.Engine+"/"+TargetKey(target), sample) != nil {
				x.job.Code = "OBSERVATION_UNAVAILABLE"
			}
			c.recordIncident(sample)
			x.job.Completed++
			_ = c.store.Save(context.Background(), "jobs", x.job.ID, x.job)
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	x.job.State = "completed"
	if x.job.Code != "" {
		x.job.State = "failed"
	}
	if ctx.Err() != nil {
		x.job.State = "canceled"
	}
	x.job.FinishedAt = c.now().UTC().Format(time.RFC3339)
	_ = c.store.Save(context.Background(), "jobs", x.job.ID, x.job)
	if c.active == x {
		c.active = nil
	}
	if p.ID != "" {
		sum := sha256.Sum256([]byte(p.ID + c.now().UTC().Format(time.RFC3339)))
		jitter := time.Duration(sum[0]%16) * time.Second
		c.next[p.ID] = c.now().Add(time.Duration(p.IntervalSeconds)*time.Second + jitter)
	}
}

func (c *Controller) Cancel(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active == nil || c.active.job.ID != id {
		return fail("JOB_NOT_RUNNING")
	}
	c.active.cancel()
	return nil
}

func (c *Controller) recordIncident(sample dto.Observation) {
	for _, class := range []string{"traffic", "observer"} {
		key := sample.Source + "/" + TargetKey(sample.Target) + "/" + sample.Revision + "/" + class
		i := c.incidents[key]
		bad := sample.Outcome == "failed" && class == "traffic" || sample.Outcome == "observer_error" && class == "observer"
		good := sample.Outcome == "passed" || class == "observer" && sample.Outcome == "failed"
		if !bad && !good {
			i.Failures = 0
			i.Successes = 0
			if i.Source != "" {
				c.incidents[key] = i
			}
			continue
		}
		if !bad && i.Source == "" {
			continue
		}
		i.Target = sample.Target
		i.Revision = sample.Revision
		i.Source = sample.Source
		i.Class = class
		i.LastSeen = sample.MeasuredAt
		if bad {
			if i.FirstSeen == "" || i.State == "resolved" {
				i.FirstSeen = sample.MeasuredAt
			}
			i.Failures++
			i.Successes = 0
			if i.State != "open" && i.State != "recovering" {
				i.State = "suspect"
			} else {
				i.State = "open"
			}
			if i.Failures >= 3 {
				i.State = "open"
			}
		} else {
			i.Successes++
			i.Failures = 0
			i.State = "recovering"
			if i.Successes >= 2 {
				i.State = "resolved"
			}
		}
		c.incidents[key] = i
		_ = c.store.Save(context.Background(), "incidents", key, i)
	}
}

func (c *Controller) Install(_ context.Context, engine string) (dto.ObservationJob, error) {
	if engine != "checker" && engine != "knife" {
		return dto.ObservationJob{}, fail("INVALID_PARAMS")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return dto.ObservationJob{}, err
	}
	if c.active != nil {
		return dto.ObservationJob{}, fail("JOB_BUSY")
	}
	ctx, cancel := context.WithCancel(c.ctx)
	j := dto.ObservationJob{ID: integrations.Token(), Kind: "install", Engine: engine, State: "running", Total: 1, StartedAt: c.now().UTC().Format(time.RFC3339)}
	if c.store.Save(context.Background(), "jobs", j.ID, j) != nil {
		cancel()
		return j, fail("OBSERVATION_UNAVAILABLE")
	}
	x := &execution{job: j, cancel: cancel, done: make(chan struct{})}
	c.active = x
	go func() {
		defer close(x.done)
		defer cancel()
		tool, err := integrations.Install(ctx, c.dataDir, engine)
		c.mu.Lock()
		defer c.mu.Unlock()
		x.job.State = "completed"
		if err != nil {
			x.job.State = "failed"
			x.job.Code = err.Error()
		} else if c.store.Save(context.Background(), "tools", engine, tool) != nil {
			x.job.State = "failed"
			x.job.Code = "OBSERVATION_UNAVAILABLE"
		} else {
			c.tools[engine] = tool
			x.job.Completed = 1
		}
		if ctx.Err() != nil {
			x.job.State = "canceled"
		}
		x.job.FinishedAt = c.now().UTC().Format(time.RFC3339)
		_ = c.store.Save(context.Background(), "jobs", j.ID, x.job)
		if c.active == x {
			c.active = nil
		}
	}()
	return j, nil
}

func (c *Controller) Setup(ctx context.Context, request dto.ToolSetup) (dto.ObservationTool, error) {
	tool, err := integrations.Verify(ctx, integrations.Tool{Engine: request.Engine, Path: request.Path, SHA256: request.SHA256})
	if err != nil {
		return dto.ObservationTool{}, fail(err.Error())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureLocked(); err != nil {
		return dto.ObservationTool{}, err
	}
	if c.active != nil {
		return dto.ObservationTool{}, fail("JOB_BUSY")
	}
	if c.store.Save(ctx, "tools", tool.Engine, tool) != nil {
		return dto.ObservationTool{}, fail("OBSERVATION_UNAVAILABLE")
	}
	c.tools[tool.Engine] = tool
	return dto.ObservationTool{Engine: tool.Engine, Version: tool.Version, Configured: true}, nil
}

func (c *Controller) Status(ctx context.Context) (dto.ObserverSnapshot, error) {
	result := dto.ObserverSnapshot{Owner: "stopped", Policies: []dto.ObservationPolicy{}, Tools: []dto.ObservationTool{}, Jobs: []dto.ObservationJob{}, Latest: []dto.Observation{}, History: []dto.Observation{}, Incidents: []dto.ObservationIncident{}, HistoryLimit: observation.HistoryLimit}
	st, err := observation.Open(c.dataDir, false)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fail("OBSERVATION_UNAVAILABLE")
	}
	defer st.Close()
	for _, table := range []string{"policies", "tools", "jobs", "incidents"} {
		rows, err := st.List(ctx, table)
		if err != nil {
			return result, fail("OBSERVATION_UNAVAILABLE")
		}
		for _, raw := range rows {
			switch table {
			case "policies":
				var p dto.ObservationPolicy
				if json.Unmarshal(raw, &p) != nil {
					return result, fail("OBSERVATION_UNAVAILABLE")
				}
				result.Policies = append(result.Policies, p)
			case "tools":
				var t integrations.Tool
				if json.Unmarshal(raw, &t) != nil {
					return result, fail("OBSERVATION_UNAVAILABLE")
				}
				result.Tools = append(result.Tools, dto.ObservationTool{Engine: t.Engine, Version: t.Version, Configured: true})
			case "jobs":
				var j dto.ObservationJob
				if json.Unmarshal(raw, &j) != nil {
					return result, fail("OBSERVATION_UNAVAILABLE")
				}
				result.Jobs = append(result.Jobs, j)
			case "incidents":
				var i dto.ObservationIncident
				if json.Unmarshal(raw, &i) != nil {
					return result, fail("OBSERVATION_UNAVAILABLE")
				}
				result.Incidents = append(result.Incidents, i)
			}
		}
	}
	result.SessionID = st.Setting(ctx, "session")
	result.Heartbeat = st.Setting(ctx, "heartbeat")
	result.PreviousHeartbeat = st.Setting(ctx, "previous_heartbeat")
	now := c.now()
	heartbeat, err := time.Parse(time.RFC3339, result.Heartbeat)
	alive := err == nil && !now.Before(heartbeat) && now.Sub(heartbeat) < 10*time.Second
	result.Running = alive && st.Setting(ctx, "monitoring") == "true"
	if !alive {
		for i := range result.Jobs {
			if result.Jobs[i].State == "running" {
				result.Jobs[i].State = "unconfirmed"
				result.Jobs[i].Code = "OWNER_HEARTBEAT_STALE"
			}
		}
	}
	if alive {
		result.Owner = "other_process"
	}
	c.mu.Lock()
	if c.store != nil && c.session == result.SessionID {
		result.Owner = "this_process"
	}
	c.mu.Unlock()
	if result.Latest, err = st.Samples(ctx, true); err != nil {
		return result, fail("OBSERVATION_UNAVAILABLE")
	}
	if result.History, err = st.Samples(ctx, false); err != nil {
		return result, fail("OBSERVATION_UNAVAILABLE")
	}
	if result.RetainedSamples, err = st.Count(ctx); err != nil {
		return result, fail("OBSERVATION_UNAVAILABLE")
	}
	for i := range result.Latest {
		sample := &result.Latest[i]
		measured, err := time.Parse(time.RFC3339, sample.MeasuredAt)
		if !alive || err != nil || sample.SessionID != result.SessionID || now.Before(measured) || now.Sub(measured) >= time.Duration(sample.FreshForSeconds)*time.Second {
			continue
		}
		current, err := c.prepare(ctx, sample.Target, sample.Source)
		sample.Fresh = err == nil && current.Revision == sample.Revision
	}
	sort.Slice(result.Policies, func(i, j int) bool { return result.Policies[i].Name < result.Policies[j].Name })
	return result, nil
}
