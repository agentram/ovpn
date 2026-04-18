package stats

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"ovpn/internal/store/remote"
	"ovpn/internal/xrayapi"
)

type Observer interface {
	OnCollectStart()
	OnCollectFinish(duration time.Duration, users int, err error)
	OnCounterReset()
	OnDBWriteError(operation string)
	OnXrayAPIReachable(reachable bool)
	OnUsersActive(count int)
	OnUserSpike(deltaBytes int64)
}

const DefaultUserSpikeDeltaBytes int64 = 1 * 1024 * 1024 * 1024

// Collector periodically reads cumulative counters from Xray and stores deltas in remote SQLite.
// It is intentionally single-flight (mu lock in CollectOnce) so manual and ticker-triggered runs
// cannot race and double-count traffic.
type Collector struct {
	Store      *remote.Store
	APIAddr    string
	Interval   time.Duration
	InboundTag string
	// SpikeDeltaBytes is the per-user total (uplink+downlink) delta threshold for one
	// collection cycle that marks unusual traffic burst activity.
	SpikeDeltaBytes int64
	Logger          *slog.Logger
	Observer        Observer
	Quota           *QuotaEnforcer
	Expiry          *ExpiryEnforcer

	mu sync.Mutex
}

// Run runs run loop until context cancellation or error.
func (c *Collector) Run(ctx context.Context) error {
	if c.Interval <= 0 {
		c.Interval = 30 * time.Second
	}
	log := c.logger().With("component", "stats-collector", "api_addr", c.APIAddr, "interval", c.Interval.String())
	log.Info("collector started")
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	if err := c.CollectOnce(ctx); err != nil {
		log.Warn("initial collection failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			log.Info("collector stopped", "reason", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			if err := c.CollectOnce(ctx); err != nil {
				log.Warn("collection tick failed", "error", err)
			}
		}
	}
}

// CollectOnce executes once flow and returns the first error.
func (c *Collector) CollectOnce(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Observer != nil {
		c.Observer.OnCollectStart()
	}
	start := time.Now()
	usersSeen := 0
	usersActive := 0
	var retErr error
	defer func() {
		if c.Observer != nil {
			c.Observer.OnCollectFinish(time.Since(start), usersSeen, retErr)
			c.Observer.OnUsersActive(usersActive)
		}
	}()

	api, err := xrayapi.New(ctx, c.APIAddr)
	if err != nil {
		if c.Observer != nil {
			c.Observer.OnXrayAPIReachable(false)
		}
		retErr = err
		return retErr
	}
	defer api.Close()

	statsMap, err := api.QueryStats(ctx, "user>>>", false)
	if err != nil {
		if c.Observer != nil {
			c.Observer.OnXrayAPIReachable(false)
		}
		retErr = err
		return retErr
	}
	if c.Observer != nil {
		c.Observer.OnXrayAPIReachable(true)
	}
	users := xrayapi.ParseUserCounters(statsMap)
	usersSeen = len(users)
	now := time.Now().UTC()
	c.logger().Debug("stats collected", "users", len(users), "timestamp", now.Format(time.RFC3339))

	for _, u := range users {
		upDelta, err := c.consumeCounter(ctx, "user>>>"+u.Email+">>>traffic>>>uplink", u.Email, u.Uplink, true, now)
		if err != nil {
			retErr = err
			return retErr
		}
		downDelta, err := c.consumeCounter(ctx, "user>>>"+u.Email+">>>traffic>>>downlink", u.Email, u.Downlink, false, now)
		if err != nil {
			retErr = err
			return retErr
		}
		totalDelta := upDelta + downDelta
		if totalDelta > 0 {
			usersActive++
		}
		if c.Observer != nil && totalDelta >= c.spikeThresholdBytes() {
			c.Observer.OnUserSpike(totalDelta)
		}
	}
	if err := c.Store.SetMeta(ctx, "last_collect_at", now.Format(time.RFC3339)); err != nil {
		if c.Observer != nil {
			c.Observer.OnDBWriteError("set_meta_last_collect_at")
		}
		retErr = err
		return retErr
	}
	if c.Quota != nil {
		if err := c.Quota.Enforce(ctx, now); err != nil {
			retErr = fmt.Errorf("enforce quota: %w", err)
			return retErr
		}
	}
	if c.Expiry != nil {
		if err := c.Expiry.Enforce(ctx, now); err != nil {
			retErr = fmt.Errorf("enforce expiry: %w", err)
			return retErr
		}
	}
	return retErr
}

// consumeCounter returns consume counter.
func (c *Collector) consumeCounter(ctx context.Context, counterKey, email string, current int64, uplink bool, now time.Time) (int64, error) {
	prev, ok, err := c.Store.GetCounter(ctx, counterKey)
	if err != nil {
		if c.Observer != nil {
			c.Observer.OnDBWriteError("get_counter")
		}
		return 0, err
	}
	// Counters are cumulative. When current < previous we treat it as a reset and use the new
	// value as delta to avoid dropping post-reset traffic.
	delta, reset := computeDelta(prev.Value, ok, current)
	if reset {
		c.logger().Warn("counter reset detected", "email", email, "counter", counterKey)
		if c.Observer != nil {
			c.Observer.OnCounterReset()
		}
		if err := c.Store.SetMeta(ctx, "last_reset_at", now.Format(time.RFC3339)); err != nil {
			if c.Observer != nil {
				c.Observer.OnDBWriteError("set_meta_last_reset_at")
			}
			return 0, err
		}
	}
	if delta > 0 {
		var upDelta, downDelta int64
		if uplink {
			upDelta = delta
		} else {
			downDelta = delta
		}
		if err := c.Store.AddDelta(ctx, email, upDelta, downDelta, now); err != nil {
			if c.Observer != nil {
				c.Observer.OnDBWriteError("add_delta")
			}
			return 0, fmt.Errorf("add delta for %s: %w", email, err)
		}
		direction := "downlink"
		if uplink {
			direction = "uplink"
		}
		c.logger().Debug("delta persisted", "email", email, "direction", direction, "delta_bytes", delta)
	}
	if err := c.Store.UpsertCounter(ctx, counterKey, current); err != nil {
		if c.Observer != nil {
			c.Observer.OnDBWriteError("upsert_counter")
		}
		return 0, err
	}
	return delta, nil
}

// spikeThresholdBytes returns spike threshold bytes.
func (c *Collector) spikeThresholdBytes() int64 {
	if c != nil && c.SpikeDeltaBytes > 0 {
		return c.SpikeDeltaBytes
	}
	return DefaultUserSpikeDeltaBytes
}

// logger returns logger.
func (c *Collector) logger() *slog.Logger {
	if c != nil && c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}
