package stats

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"ovpn/internal/store/remote"
)

const DefaultQuotaWindow = 30 * 24 * time.Hour
const DefaultWindow30DQuotaBytes int64 = 200 * 1024 * 1024 * 1024

// DefaultMonthlyQuotaBytes is kept as a compatibility alias for older callsites.
const DefaultMonthlyQuotaBytes int64 = DefaultWindow30DQuotaBytes

type RuntimeManager interface {
	AddUser(ctx context.Context, inboundTag, email, uuid string) error
	RemoveUser(ctx context.Context, inboundTag, email string) error
}

// QuotaEnforcer applies hard rolling-window caps by removing users from the live Xray inbound when
// they exceed their quota, then adding them back automatically when usage drops below the cap.
type QuotaEnforcer struct {
	Store                     *remote.Store
	Runtime                   RuntimeManager
	DefaultWindow30DQuotaByte int64
	Window                    time.Duration
	Logger                    *slog.Logger
	OnEvent                   func(action, result string)
	OnBlockedUsers            func(blocked int)
	OnUsageBands              func(over80 int, over95 int)
	OnNotify                  func(event, message string)
}

// Enforce returns enforce.
func (q *QuotaEnforcer) Enforce(ctx context.Context, now time.Time) error {
	if q == nil || q.Store == nil {
		return nil
	}

	window := q.Window
	if window <= 0 {
		window = DefaultQuotaWindow
	}
	windowEnd := now.UTC()
	windowStart := windowEnd.Add(-window)

	defaultQuota := q.DefaultWindow30DQuotaByte
	if defaultQuota <= 0 {
		defaultQuota = DefaultWindow30DQuotaBytes
	}

	policies, err := q.Store.ListQuotaPolicies(ctx)
	if err != nil {
		return err
	}
	states, err := q.Store.ListQuotaStates(ctx)
	if err != nil {
		return err
	}
	usageByEmail, err := q.Store.ListUsageBetween(ctx, windowStart, windowEnd)
	if err != nil {
		return err
	}

	var blockedUsers int
	var over80Users int
	var over95Users int
	var firstErr error

	for _, p := range policies {
		quota := defaultQuota
		if p.MonthlyQuotaByte != nil && *p.MonthlyQuotaByte > 0 {
			quota = *p.MonthlyQuotaByte
		}

		state := states[p.Email]
		isBlocked := state.Blocked
		usage := usageByEmail[p.Email]

		if !p.QuotaEnabled {
			if !isBlocked {
				continue
			}
			if err := q.runtimeAdd(ctx, p.InboundTag, p.Email, p.UUID); err != nil {
				firstErr = combineFirst(firstErr, fmt.Errorf("unblock %s after quota disabled: %w", p.Email, err))
				blockedUsers++
				continue
			}
			if err := q.Store.SetQuotaBlocked(ctx, p.Email, false, nil); err != nil {
				firstErr = combineFirst(firstErr, fmt.Errorf("clear quota block for %s: %w", p.Email, err))
				blockedUsers++
				continue
			}
			q.recordEvent("unblock", "success")
			q.notify("quota_unblock", fmt.Sprintf("quota unblock applied for %s because enforcement is disabled", p.Email))
			q.logger().Info("quota block cleared because enforcement disabled", "email", p.Email)
			continue
		}

		if quota <= 0 {
			continue
		}

		usageRatio := float64(usage) / float64(quota)
		if usageRatio >= 0.80 {
			over80Users++
		}
		if usageRatio >= 0.95 {
			over95Users++
		}

		if isBlocked {
			if usage >= quota {
				blockedUsers++
				continue
			}
			if err := q.runtimeAdd(ctx, p.InboundTag, p.Email, p.UUID); err != nil {
				firstErr = combineFirst(firstErr, fmt.Errorf("unblock %s: %w", p.Email, err))
				blockedUsers++
				continue
			}
			if err := q.Store.SetQuotaBlocked(ctx, p.Email, false, nil); err != nil {
				firstErr = combineFirst(firstErr, fmt.Errorf("clear quota block for %s: %w", p.Email, err))
				blockedUsers++
				continue
			}
			q.recordEvent("unblock", "success")
			q.notify("quota_unblock", fmt.Sprintf("quota unblock applied for %s when usage dropped below rolling 30d quota", p.Email))
			q.logger().Info("quota unblock applied in rolling window", "email", p.Email, "window_start", windowStart.Format(time.RFC3339), "window_end", windowEnd.Format(time.RFC3339), "window_usage_bytes", usage, "window_quota_bytes", quota)
			continue
		}

		if usage < quota {
			continue
		}

		if err := q.runtimeRemove(ctx, p.InboundTag, p.Email); err != nil {
			firstErr = combineFirst(firstErr, fmt.Errorf("block %s: %w", p.Email, err))
			continue
		}
		blockedAt := now.UTC()
		if err := q.Store.SetQuotaBlocked(ctx, p.Email, true, &blockedAt); err != nil {
			firstErr = combineFirst(firstErr, fmt.Errorf("persist quota block for %s: %w", p.Email, err))
			continue
		}
		blockedUsers++
		q.recordEvent("block", "success")
		q.notify("quota_block", fmt.Sprintf("quota block applied for %s in rolling 30d window", p.Email))
		q.logger().Warn("quota block applied", "email", p.Email, "window_start", windowStart.Format(time.RFC3339), "window_end", windowEnd.Format(time.RFC3339), "window_usage_bytes", usage, "window_quota_bytes", quota)
	}

	if q.OnBlockedUsers != nil {
		q.OnBlockedUsers(blockedUsers)
	}
	if q.OnUsageBands != nil {
		q.OnUsageBands(over80Users, over95Users)
	}
	return firstErr
}

// runtimeAdd executes runtime add flow and returns the first error.
func (q *QuotaEnforcer) runtimeAdd(ctx context.Context, inboundTag, email, uuid string) error {
	if q.Runtime == nil {
		return fmt.Errorf("runtime manager is not configured")
	}
	if strings.TrimSpace(inboundTag) == "" || strings.TrimSpace(email) == "" || strings.TrimSpace(uuid) == "" {
		return fmt.Errorf("runtime identity is incomplete")
	}
	if err := q.Runtime.AddUser(ctx, inboundTag, email, uuid); err != nil {
		q.recordEvent("unblock", "error")
		return err
	}
	return nil
}

// runtimeRemove executes runtime remove flow and returns the first error.
func (q *QuotaEnforcer) runtimeRemove(ctx context.Context, inboundTag, email string) error {
	if q.Runtime == nil {
		return fmt.Errorf("runtime manager is not configured")
	}
	if strings.TrimSpace(inboundTag) == "" || strings.TrimSpace(email) == "" {
		return fmt.Errorf("runtime identity is incomplete")
	}
	if err := q.Runtime.RemoveUser(ctx, inboundTag, email); err != nil {
		q.recordEvent("block", "error")
		return err
	}
	return nil
}

// logger returns logger.
func (q *QuotaEnforcer) logger() *slog.Logger {
	if q != nil && q.Logger != nil {
		return q.Logger
	}
	return slog.Default()
}

// recordEvent returns record event.
func (q *QuotaEnforcer) recordEvent(action, result string) {
	if q != nil && q.OnEvent != nil {
		q.OnEvent(action, result)
	}
}

// notify returns notify.
func (q *QuotaEnforcer) notify(event, message string) {
	if q != nil && q.OnNotify != nil {
		q.OnNotify(event, message)
	}
}

// combineFirst combines input values to produce first.
func combineFirst(current error, next error) error {
	if current != nil {
		return current
	}
	return next
}
