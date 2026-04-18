package main

import (
	"context"
	"strings"
	"time"
)

func newBotHealth(pollInterval time.Duration, metrics *botMetrics) *botHealth {
	return &botHealth{
		startedAt:    time.Now().UTC(),
		pollInterval: pollInterval,
		metrics:      metrics,
	}
}

func (b *bot) ensureHealth() *botHealth {
	if b.health == nil {
		b.health = newBotHealth(b.cfg.pollInterval, b.metrics)
	}
	return b.health
}

func (h *botHealth) staleAfter() time.Duration {
	threshold := 3 * h.pollInterval
	if threshold < 2*time.Minute {
		threshold = 2 * time.Minute
	}
	return threshold
}

func (h *botHealth) onPollSuccess(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastPollSuccess = now.UTC()
	h.lastPollFailure = ""
	h.consecutivePollFailures = 0
	h.watchdogUnhealthy = false
	if h.metrics != nil {
		h.metrics.onPollSuccess(h.lastPollSuccess)
	}
}

func (h *botHealth) onPollFailure(now time.Time, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.consecutivePollFailures++
	if err != nil {
		h.lastPollFailure = strings.TrimSpace(err.Error())
	}
	if h.metrics != nil {
		h.metrics.onPollFailure(h.consecutivePollFailures)
	}
	_ = now
}

func (h *botHealth) onSendSuccess(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastSendSuccess = now.UTC()
	h.lastSendFailure = ""
	h.consecutiveSendFailures = 0
	if h.metrics != nil {
		h.metrics.onSendSuccess(h.lastSendSuccess)
	}
}

func (h *botHealth) onSendFailure(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.consecutiveSendFailures++
	if err != nil {
		h.lastSendFailure = strings.TrimSpace(err.Error())
	}
	if h.metrics != nil {
		h.metrics.onSendFailure(h.consecutiveSendFailures)
	}
}

func (h *botHealth) snapshot(now time.Time) botHealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	status := "healthy"
	ok := true
	staleAfter := h.staleAfter()
	switch {
	case h.lastPollSuccess.IsZero() && now.UTC().Sub(h.startedAt) > staleAfter:
		status = "unhealthy"
		ok = false
		h.watchdogUnhealthy = true
	case !h.lastPollSuccess.IsZero() && now.UTC().Sub(h.lastPollSuccess) > staleAfter:
		status = "unhealthy"
		ok = false
		h.watchdogUnhealthy = true
	case h.consecutiveSendFailures >= 3:
		status = "degraded"
		ok = true
		h.watchdogUnhealthy = false
	default:
		h.watchdogUnhealthy = false
	}
	if h.metrics != nil {
		h.metrics.setWatchdogUnhealthy(h.watchdogUnhealthy)
	}

	return botHealthSnapshot{
		Status:                  status,
		OK:                      ok,
		StartedAt:               h.startedAt,
		LastPollSuccess:         h.lastPollSuccess,
		LastSendSuccess:         h.lastSendSuccess,
		LastPollFailure:         h.lastPollFailure,
		LastSendFailure:         h.lastSendFailure,
		ConsecutivePollFailures: h.consecutivePollFailures,
		ConsecutiveSendFailures: h.consecutiveSendFailures,
		PollStaleAfter:          staleAfter.String(),
		WatchdogUnhealthy:       h.watchdogUnhealthy,
	}
}

func (b *bot) healthSnapshot() botHealthSnapshot {
	return b.ensureHealth().snapshot(time.Now().UTC())
}

func (b *bot) watchdogLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := b.healthSnapshot()
			if snap.Status != "unhealthy" {
				continue
			}
			b.logger.Error("telegram bot watchdog detected unhealthy polling state", "last_poll_success", snap.LastPollSuccess, "consecutive_poll_failures", snap.ConsecutivePollFailures, "last_poll_failure", snap.LastPollFailure)
			if b.exitFn != nil {
				b.exitFn(1)
				return
			}
		}
	}
}
