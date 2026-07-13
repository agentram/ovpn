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
const DefaultWindow30DQuotaBytes int64 = 300 * 1024 * 1024 * 1024

// RuntimeManager adds and removes users from the live Xray inbound.
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

// Enforce blocks users whose rolling-window usage meets their quota and unblocks those that have dropped back below it.
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

	blockedEmails := map[string]bool{}
	over80Emails := map[string]bool{}
	over95Emails := map[string]bool{}
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
				blockedEmails[p.Email] = true
				continue
			}
			if err := q.Store.SetQuotaBlocked(ctx, p.Email, false, nil); err != nil {
				firstErr = combineFirst(firstErr, fmt.Errorf("clear quota block for %s: %w", p.Email, err))
				blockedEmails[p.Email] = true
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
			over80Emails[p.Email] = true
		}
		if usageRatio >= 0.95 {
			over95Emails[p.Email] = true
		}

		if isBlocked {
			if usage >= quota {
				blockedEmails[p.Email] = true
				continue
			}
			if err := q.runtimeAdd(ctx, p.InboundTag, p.Email, p.UUID); err != nil {
				firstErr = combineFirst(firstErr, fmt.Errorf("unblock %s: %w", p.Email, err))
				blockedEmails[p.Email] = true
				continue
			}
			if err := q.Store.SetQuotaBlocked(ctx, p.Email, false, nil); err != nil {
				firstErr = combineFirst(firstErr, fmt.Errorf("clear quota block for %s: %w", p.Email, err))
				blockedEmails[p.Email] = true
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
		blockedEmails[p.Email] = true
		q.recordEvent("block", "success")
		q.notify("quota_block", fmt.Sprintf("quota block applied for %s in rolling 30d window", p.Email))
		q.logger().Warn("quota block applied", "email", p.Email, "window_start", windowStart.Format(time.RFC3339), "window_end", windowEnd.Format(time.RFC3339), "window_usage_bytes", usage, "window_quota_bytes", quota)
	}

	if q.OnBlockedUsers != nil {
		q.OnBlockedUsers(len(blockedEmails))
	}
	if q.OnUsageBands != nil {
		q.OnUsageBands(len(over80Emails), len(over95Emails))
	}
	return firstErr
}

// runtimeAdd re-adds a user to the live inbound, validating that the runtime identity is complete.
func (q *QuotaEnforcer) runtimeAdd(ctx context.Context, inboundTag, email, uuid string) error {
	if q.Runtime == nil {
		return fmt.Errorf("runtime manager is not configured")
	}
	if strings.TrimSpace(inboundTag) == "" || strings.TrimSpace(email) == "" || strings.TrimSpace(uuid) == "" {
		return fmt.Errorf("runtime identity is incomplete")
	}
	if err := q.Runtime.AddUser(ctx, inboundTag, email, uuid); err != nil {
		if isRuntimeAlreadyPresentError(err) {
			return nil
		}
		q.recordEvent("unblock", "error")
		return err
	}
	return nil
}

// runtimeRemove removes a user from the live inbound, validating that the runtime identity is complete.
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

// logger returns the enforcer's logger, falling back to the default logger.
func (q *QuotaEnforcer) logger() *slog.Logger {
	if q != nil && q.Logger != nil {
		return q.Logger
	}
	return slog.Default()
}

// recordEvent reports a quota action and result to the optional observer.
func (q *QuotaEnforcer) recordEvent(action, result string) {
	if q != nil && q.OnEvent != nil {
		q.OnEvent(action, result)
	}
}

// notify forwards a quota event to the optional notification callback.
func (q *QuotaEnforcer) notify(event, message string) {
	if q != nil && q.OnNotify != nil {
		q.OnNotify(event, message)
	}
}

// combineFirst keeps the first non-nil error, used to surface the earliest failure in a batch.
func combineFirst(current error, next error) error {
	if current != nil {
		return current
	}
	return next
}

func isRuntimeAlreadyPresentError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "already exists") ||
		strings.Contains(text, "already exist")
}
