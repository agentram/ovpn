package stats

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/store/remote"
)

// ExpiryEnforcer applies mirrored user expiry state to live runtime membership.
type ExpiryEnforcer struct {
	Store   *remote.Store
	Runtime RuntimeManager
	Logger  *slog.Logger
}

// Enforce reconciles runtime membership against mirrored user expiry state.
func (e *ExpiryEnforcer) Enforce(ctx context.Context, now time.Time) error {
	if e == nil || e.Store == nil {
		return nil
	}
	policies, err := e.Store.ListUserPolicies(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, p := range policies {
		effectiveEnabled := model.IsEffectivelyEnabled(p.Enabled, p.ExpiryAt, now)
		if !effectiveEnabled {
			if err := e.runtimeRemove(ctx, p.InboundTag, p.Email); err != nil {
				firstErr = combineFirst(firstErr, fmt.Errorf("remove expired/disabled %s: %w", p.Email, err))
			}
		}
	}
	return firstErr
}

func (e *ExpiryEnforcer) runtimeRemove(ctx context.Context, inboundTag, email string) error {
	if e == nil || e.Runtime == nil {
		return fmt.Errorf("runtime manager is not configured")
	}
	if strings.TrimSpace(inboundTag) == "" || strings.TrimSpace(email) == "" {
		return fmt.Errorf("runtime identity is incomplete")
	}
	if err := e.Runtime.RemoveUser(ctx, inboundTag, email); err != nil && !isRuntimeMissingError(err) {
		return err
	}
	return nil
}

func isRuntimeMissingError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "not found") || strings.Contains(text, "not exist") || strings.Contains(text, "failed to remove") && strings.Contains(text, "user")
}
