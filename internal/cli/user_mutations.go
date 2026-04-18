package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/telegrambot"
)

// addUserOnServer creates user in local state and applies runtime/persist updates for one server.
func (a *App) addUserOnServer(srv model.Server, user *model.User) (err error) {
	defer func() {
		status := "success"
		severity := "info"
		message := fmt.Sprintf("server=%s username=%s email=%s", srv.Name, user.Username, user.Email)
		if err != nil {
			status = "failure"
			severity = "error"
			message = err.Error()
		}
		a.sendTelegramNotifyEvent(srv, telegrambot.NotifyEvent{
			Event:    "user_add",
			Status:   status,
			Severity: severity,
			Source:   "ovpn-cli",
			Message:  message,
		})
	}()

	if err := a.store.AddUser(a.ctx, user); err != nil {
		return err
	}
	a.log().Info("user saved in local state", "server", srv.Name, "username", user.Username, "email", user.Email, "enabled", user.Enabled)

	if err := a.applyRuntimeUser(srv, *user, effectiveUserEnabled(*user)); err != nil {
		if errors.Is(err, errRuntimeQuotaBlocked) {
			a.log().Info("runtime add skipped because user is blocked by quota", "server", srv.Name, "username", user.Username, "email", user.Email)
			if err := a.persistAndSyncPolicies(srv); err != nil {
				return err
			}
			return nil
		}
		a.log().Warn("runtime add failed, running deploy fallback", "server", srv.Name, "username", user.Username, "error", err)
		if err := a.initOrDeployServer(srv, false); err != nil {
			return fmt.Errorf("fallback deploy failed: %w", err)
		}
		return nil
	}
	return a.persistAndSyncPolicies(srv)
}

// removeUserOnServer removes user from local state and applies runtime/persist updates for one server.
func (a *App) removeUserOnServer(srv model.Server, username string) (err error) {
	defer func() {
		status := "success"
		severity := "info"
		message := fmt.Sprintf("server=%s username=%s", srv.Name, username)
		if err != nil {
			status = "failure"
			severity = "error"
			message = err.Error()
		}
		a.sendTelegramNotifyEvent(srv, telegrambot.NotifyEvent{
			Event:    "user_remove",
			Status:   status,
			Severity: severity,
			Source:   "ovpn-cli",
			Message:  message,
		})
	}()

	u, err := a.store.GetUser(a.ctx, srv.ID, username)
	if err != nil {
		return err
	}
	if err := a.store.DeleteUser(a.ctx, srv.ID, username); err != nil {
		return err
	}
	a.log().Info("user removed from local state", "server", srv.Name, "username", u.Username, "email", u.Email)
	if err := a.applyRuntimeUser(srv, *u, false); err != nil {
		a.log().Warn("runtime remove failed, running deploy fallback", "server", srv.Name, "username", u.Username, "error", err)
		if err := a.initOrDeployServer(srv, false); err != nil {
			return fmt.Errorf("fallback deploy failed: %w", err)
		}
		return nil
	}
	return a.persistAndSyncPolicies(srv)
}

// setUserEnabledOnServer updates user enabled state on one server.
func (a *App) setUserEnabledOnServer(srv model.Server, username string, enable bool) error {
	u, err := a.store.GetUser(a.ctx, srv.ID, username)
	if err != nil {
		return err
	}
	u.Enabled = enable
	if err := a.store.UpdateUser(a.ctx, u); err != nil {
		return err
	}
	a.log().Info("user state updated", "server", srv.Name, "username", u.Username, "enabled", u.Enabled)

	if err := a.applyRuntimeUser(srv, *u, effectiveUserEnabled(*u)); err != nil {
		if errors.Is(err, errRuntimeQuotaBlocked) {
			a.log().Info("runtime enable skipped because user is blocked by quota", "server", srv.Name, "username", u.Username, "email", u.Email)
			return a.persistAndSyncPolicies(srv)
		}
		a.log().Warn("runtime update failed, running deploy fallback", "server", srv.Name, "username", u.Username, "enabled", u.Enabled, "error", err)
		if err := a.initOrDeployServer(srv, false); err != nil {
			return fmt.Errorf("fallback deploy failed: %w", err)
		}
		return nil
	}
	return a.persistAndSyncPolicies(srv)
}

// setUserExpiryOnServer updates user expiry state on one server.
func (a *App) setUserExpiryOnServer(srv model.Server, username string, expiry *time.Time) error {
	u, err := a.store.GetUser(a.ctx, srv.ID, username)
	if err != nil {
		return err
	}
	updated, effectiveChanged := userAfterExpiryUpdate(*u, expiry, time.Now().UTC())
	*u = updated
	if err := a.store.UpdateUser(a.ctx, u); err != nil {
		return err
	}
	if !effectiveChanged {
		return a.persistAndSyncPolicies(srv)
	}
	if err := a.applyRuntimeUser(srv, *u, effectiveUserEnabled(*u)); err != nil {
		if errors.Is(err, errRuntimeQuotaBlocked) {
			a.log().Info("runtime enable skipped because user is blocked by quota", "server", srv.Name, "username", u.Username, "email", u.Email)
			return a.persistAndSyncPolicies(srv)
		}
		a.log().Warn("runtime update failed, running deploy fallback", "server", srv.Name, "username", u.Username, "error", err)
		if err := a.initOrDeployServer(srv, false); err != nil {
			return fmt.Errorf("fallback deploy failed: %w", err)
		}
		return nil
	}
	return a.persistAndSyncPolicies(srv)
}

func userAfterExpiryUpdate(u model.User, expiry *time.Time, now time.Time) (model.User, bool) {
	previousEffective := model.IsEffectivelyEnabled(u.Enabled, u.ExpiryDate, now)
	u.ExpiryDate = cloneTimePtr(expiry)
	if expiry == nil || expiry.UTC().After(now.UTC()) {
		u.Enabled = true
	}
	currentEffective := model.IsEffectivelyEnabled(u.Enabled, u.ExpiryDate, now)
	return u, previousEffective != currentEffective
}

// setUserQuotaOnServer updates user quota policy on one server.
func (a *App) setUserQuotaOnServer(srv model.Server, username string, monthlyByte int64, enabled bool) error {
	u, err := a.store.GetUser(a.ctx, srv.ID, username)
	if err != nil {
		return err
	}
	u.QuotaEnabled = enabled
	if monthlyByte > 0 {
		v := monthlyByte
		u.TrafficLimitByte = &v
	} else {
		u.TrafficLimitByte = nil
	}
	if !u.QuotaEnabled {
		u.QuotaBlocked = false
		u.QuotaBlockedAt = nil
	}
	if err := a.store.UpdateUser(a.ctx, u); err != nil {
		return err
	}
	if err := a.syncQuotaPolicy(srv); err != nil {
		return err
	}
	blockedByQuota, err := a.isUserBlockedByQuota(srv, u.Email)
	if err != nil {
		return err
	}
	if !u.QuotaEnabled || blockedByQuota {
		if _, err := a.fetchRemoteAgent(srv, "POST", a.agentURL("/quota/reset"), map[string]string{"email": u.Email}); err != nil {
			return err
		}
	}
	return nil
}

// resetUserQuotaOnServer clears quota block for one user on one server.
func (a *App) resetUserQuotaOnServer(srv model.Server, username string) error {
	u, err := a.store.GetUser(a.ctx, srv.ID, username)
	if err != nil {
		return err
	}
	if _, err := a.fetchRemoteAgent(srv, "POST", a.agentURL("/quota/reset"), map[string]string{"email": u.Email}); err != nil {
		return err
	}
	u.QuotaBlocked = false
	u.QuotaBlockedAt = nil
	if err := a.store.UpdateUser(a.ctx, u); err != nil {
		return err
	}
	return nil
}

// isNotFoundErr reports local-store "not found" shape.
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
