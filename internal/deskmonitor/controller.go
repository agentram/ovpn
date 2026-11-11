// Package deskmonitor owns automatic checks for an open desktop session.
package deskmonitor

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/observation"
	"ovpn/internal/observer"
)

const CheckInterval = 15 * time.Minute
const ProbeTimeout = 12 * time.Second
const PresenceQuietPeriod = 30 * time.Second

type Target struct {
	Target   dto.ObservationTarget
	Engine   string
	Revision string
}
type Source interface {
	Targets(context.Context) ([]Target, error)
	Measure(context.Context, Target) dto.Observation
	Telemetry(context.Context, bool) ([]dto.ServerLive, error)
}
type route struct {
	Target     Target
	View       dto.RouteLive
	DirtyUntil time.Time
}
type Controller struct {
	lifecycle     sync.Mutex
	mu            sync.Mutex
	source        Source
	workspace     string
	parent        context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	store         *observation.Store
	pulse         time.Time
	enabled       bool
	routes        map[string]*route
	live          map[string]dto.ServerLive
	active        map[string]bool
	activeNodes   map[string]bool
	now           func() time.Time
	interval      time.Duration
	lastFull      time.Time
	lastPresence  time.Time
	lastInventory time.Time
	collecting    bool
}

func New(ctx context.Context, workspace string, source Source) *Controller {
	return &Controller{source: source, workspace: workspace, parent: ctx, routes: map[string]*route{}, live: map[string]dto.ServerLive{}, active: map[string]bool{}, activeNodes: map[string]bool{}, now: time.Now, interval: CheckInterval}
}
func (c *Controller) Configure(enabled bool) error {
	c.mu.Lock()
	if c.store == nil {
		st, err := observation.Open(c.workspace, true)
		if err != nil {
			c.mu.Unlock()
			return err
		}
		c.store = st
	}
	if err := c.store.Set(c.parent, "desk.auto.enabled", map[bool]string{true: "true", false: "false"}[enabled]); err != nil {
		c.mu.Unlock()
		return err
	}
	c.enabled = enabled
	c.mu.Unlock()
	if !enabled {
		return nil
	}
	return c.Start()
}
func (c *Controller) Start() error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return nil
	}
	if c.store == nil {
		st, err := observation.Open(c.workspace, true)
		if err != nil {
			return err
		}
		c.store = st
		c.enabled = st.Setting(c.parent, "desk.auto.enabled") == "true"
	}
	c.pulse = c.now()
	ctx, cancel := context.WithCancel(c.parent)
	c.cancel = cancel
	c.done = make(chan struct{})
	go c.run(ctx, c.done)
	return nil
}
func (c *Controller) Stop() error {
	c.lifecycle.Lock()
	defer c.lifecycle.Unlock()
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return nil
}
func (c *Controller) Close() {
	_ = c.Stop()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.store != nil {
		_ = c.store.Close()
		c.store = nil
	}
}
func (c *Controller) Pulse() {
	_ = c.Start()
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	gap := now.UTC().Sub(c.pulse.UTC())
	if gap > 30*time.Second || gap < 0 {
		for _, r := range c.routes {
			r.View.State = "pending"
			r.View.NextAt = now
		}
	}
	c.pulse = now
}
func (c *Controller) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastInventory = time.Time{}
	c.lastFull = time.Time{}
	c.lastPresence = time.Time{}
}

func (c *Controller) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		c.tick(ctx, &workers)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (c *Controller) tick(ctx context.Context, workers *sync.WaitGroup) {
	c.mu.Lock()
	now := c.now()
	awake := now.UTC().Sub(c.pulse.UTC()) >= 0 && now.UTC().Sub(c.pulse.UTC()) < 30*time.Second
	inventoryDue := now.Sub(c.lastInventory) >= 30*time.Second
	c.mu.Unlock()
	if !awake {
		return
	}
	if inventoryDue {
		c.refreshTargets(ctx)
	}
	c.mu.Lock()
	if !c.collecting && now.Sub(c.lastPresence) >= 10*time.Second {
		c.lastPresence = now
		c.collecting = true
		full := now.Sub(c.lastFull) >= 30*time.Second
		if full {
			c.lastFull = now
		}
		workers.Add(1)
		go func() { defer workers.Done(); c.collect(ctx, !full) }()
	}
	// Presence refreshes are requested on the ten-second boundary.
	var selected []*route
	for c.enabled && len(c.active)+len(selected) < 4 {
		var next *route
		for key, r := range c.routes {
			if c.active[key] || c.activeNodes[r.Target.Target.ServerID] || r.View.NextAt.After(now) {
				continue
			}
			chosen := false
			for _, candidate := range selected {
				if candidate == r || candidate.Target.Target.ServerID == r.Target.Target.ServerID {
					chosen = true
					break
				}
			}
			if chosen {
				continue
			}
			if next == nil || r.View.NextAt.Before(next.View.NextAt) {
				next = r
			}
		}
		if next == nil {
			break
		}
		selected = append(selected, next)
	}
	for _, r := range selected {
		key := observer.TargetKey(r.Target.Target)
		c.active[key] = true
		c.activeNodes[r.Target.Target.ServerID] = true
		r.View.State = "checking"
		r.DirtyUntil = now.Add(ProbeTimeout + PresenceQuietPeriod)
		copy := r.Target
		workers.Add(1)
		go func(target Target) { defer workers.Done(); c.measure(ctx, target) }(copy)
	}
	c.mu.Unlock()
}
func (c *Controller) refreshTargets(ctx context.Context) {
	targets, err := c.source.Targets(ctx)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.lastInventory = now
	wanted := map[string]bool{}
	for _, target := range targets {
		key := observer.TargetKey(target.Target)
		wanted[key] = true
		existing, ok := c.routes[key]
		if ok && existing.Target.Revision == target.Revision {
			continue
		}
		r := &route{DirtyUntil: now.Add(PresenceQuietPeriod), Target: target, View: dto.RouteLive{Target: target.Target, State: "pending", NextAt: now}}
		if c.store != nil {
			var saved struct {
				View     dto.RouteLive
				Revision string
			}
			if json.Unmarshal([]byte(c.store.Setting(ctx, "desk.route."+key)), &saved) == nil && saved.Revision == target.Revision {
				r.View = saved.View
			}
		}
		// A restarted worker collects a new sample before presenting fresh evidence.
		r.View.State = "pending"
		r.View.NextAt = now
		c.routes[key] = r
	}
	for key := range c.routes {
		if !wanted[key] {
			delete(c.routes, key)
		}
	}
}
func (c *Controller) collect(ctx context.Context, presenceOnly bool) {
	wait, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	rows, err := c.source.Telemetry(wait, presenceOnly)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collecting = false
	if err != nil {
		return
	}
	now := c.now()
	for _, row := range rows {
		if row.ServerID == "" {
			continue
		}
		row.Users = append([]dto.UserLive(nil), row.Users...)
		previous := c.live[row.ServerID]
		if presenceOnly {
			row.State, row.Code, row.CheckedAt = previous.State, previous.Code, previous.CheckedAt
			for i := range row.Users {
				for _, old := range previous.Users {
					if old.Username == row.Users[i].Username {
						row.Users[i].UsedBytes = old.UsedBytes
						row.Users[i].QuotaBytes = old.QuotaBytes
						row.Users[i].RemainingBytes = old.RemainingBytes
						row.Users[i].QuotaEnabled = old.QuotaEnabled
						row.Users[i].BlockedByQuota = old.BlockedByQuota
						row.Users[i].RuntimeApplied = old.RuntimeApplied
						row.Users[i].TrafficAt = old.TrafficAt
						break
					}
				}
			}
		}
		for i := range row.Users {
			u := &row.Users[i]
			for _, r := range c.routes {
				if r.Target.Target.ServerID == row.ServerID && r.Target.Target.Username == u.Username && now.Before(r.DirtyUntil) {
					u.Connected = nil
					u.PresenceCode = "CHECK_IN_PROGRESS"
					break
				}
			}
		}
		c.live[row.ServerID] = row
	}
}
func (c *Controller) measure(ctx context.Context, target Target) {
	key := observer.TargetKey(target.Target)
	checkCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	c.mu.Lock()
	store := c.store
	now := c.now()
	c.mu.Unlock()
	var sample dto.Observation
	reserved, err := store.Reserve(checkCtx, "desk/"+target.Engine+"/"+key, now, 128)
	if err != nil {
		sample = dto.Observation{Outcome: "observer_error", Code: "OBSERVATION_UNAVAILABLE"}
	} else if !reserved {
		sample = dto.Observation{Outcome: "skipped", Code: "DAILY_ATTEMPT_LIMIT"}
	} else {
		sample = c.source.Measure(checkCtx, target)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.active, key)
	delete(c.activeNodes, target.Target.ServerID)
	r, ok := c.routes[key]
	if !ok || r.Target.Revision != target.Revision {
		return
	}
	sample.Target = target.Target
	sample.Source = target.Engine
	sample.Revision = target.Revision
	if sample.MeasuredAt == "" {
		sample.MeasuredAt = c.now().UTC().Format(time.RFC3339Nano)
	}
	sample.FreshForSeconds = int(c.interval.Seconds())
	sample.Fresh = true
	r.View.Sample = &sample
	r.View.State = sample.Outcome
	r.View.NextAt = c.now().Add(c.interval)
	if sample.Code == "TOOL_UNAVAILABLE" {
		r.View.NextAt = c.now().Add(30 * time.Second)
	}
	r.DirtyUntil = c.now().Add(PresenceQuietPeriod)
	raw, _ := json.Marshal(struct {
		View     dto.RouteLive
		Revision string
	}{r.View, target.Revision})
	_ = store.Set(c.parent, "desk.route."+key, string(raw))
	_ = store.Add(c.parent, "desk/"+target.Engine+"/"+key, sample)
}
func (c *Controller) Snapshot() dto.DeskSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	result := dto.DeskSnapshot{Enabled: c.enabled, Running: c.cancel != nil && !now.Before(c.pulse) && now.UTC().Sub(c.pulse.UTC()) < 30*time.Second, Servers: []dto.ServerLive{}, Routes: []dto.RouteLive{}, Total: len(c.routes), Active: len(c.active)}
	for _, row := range c.live {
		users := append([]dto.UserLive(nil), row.Users...)
		row.Users = users
		for i := range row.Users {
			u := &row.Users[i]
			u.RoutesTotal, u.RoutesPassed, u.RoutesFailed = 0, 0, 0
			u.AccessAt = ""
			u.NextCheckAt = ""
			for _, r := range c.routes {
				if r.Target.Target.ServerID == row.ServerID && r.Target.Target.Username == u.Username {
					u.RoutesTotal++
					if r.View.Sample != nil && (u.AccessAt == "" || r.View.Sample.MeasuredAt < u.AccessAt) {
						u.AccessAt = r.View.Sample.MeasuredAt
					}
					next := r.View.NextAt.UTC().Format(time.RFC3339Nano)
					if u.NextCheckAt == "" || next < u.NextCheckAt {
						u.NextCheckAt = next
					}
					if result.Running && r.View.Sample != nil && freshSample(r.View.Sample, now, c.interval) && r.View.NextAt.After(now) && r.Target.Revision == r.View.Sample.Revision {
						switch r.View.State {
						case "passed":
							u.RoutesPassed++
						case "failed":
							u.RoutesFailed++
						}
					}
				}
			}
			presenceTime, _ := time.Parse(time.RFC3339, u.PresenceAt)
			if !result.Running || now.UTC().Sub(presenceTime) > 30*time.Second || now.Before(presenceTime) {
				u.Connected = nil
			}
		}
		result.Servers = append(result.Servers, row)
	}
	for _, r := range c.routes {
		if r.View.Sample != nil && freshSample(r.View.Sample, now, c.interval) && result.Running && r.View.State != "pending" && r.View.NextAt.After(now) {
			result.Checked++
		}
	}
	return result
}
func (c *Controller) Routes(server, username string) []dto.RouteLive {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := []dto.RouteLive{}
	now := c.now()
	for _, r := range c.routes {
		if (server == "" || r.Target.Target.ServerID == server) && (username == "" || r.Target.Target.Username == username) {
			view := r.View
			if view.Sample != nil {
				copy := *view.Sample
				copy.Fresh = c.cancel != nil && !now.Before(c.pulse) && now.UTC().Sub(c.pulse.UTC()) < 30*time.Second && view.State != "pending" && freshSample(&copy, now, c.interval) && view.NextAt.After(now)
				view.Sample = &copy
			}
			result = append(result, view)
		}
	}
	return result
}

func freshSample(sample *dto.Observation, now time.Time, interval time.Duration) bool {
	measured, err := time.Parse(time.RFC3339Nano, sample.MeasuredAt)
	return err == nil && !now.Before(measured) && now.Sub(measured) < interval
}

// InvalidateRoutes requests new evidence after a runtime-only repair, without
// rescheduling unrelated users or turning inventory reads into traffic checks.
func (c *Controller) InvalidateRoutes(nodes map[string]bool, username string) {
	c.Invalidate()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, route := range c.routes {
		if nodes[route.Target.Target.ServerID] && (username == "" || route.Target.Target.Username == username) {
			route.View.NextAt = c.now()
		}
	}
}
