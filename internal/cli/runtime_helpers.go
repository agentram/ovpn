package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/defaults"
	"ovpn/internal/deploy"
	"ovpn/internal/model"
	"ovpn/internal/telegrambot"
	"ovpn/internal/util"
	"ovpn/internal/xraycfg"
)

// initOrDeployServer returns init or deploy server.
func (a *App) initOrDeployServer(srv model.Server, bootstrap bool) (err error) {
	event := "deploy"
	if bootstrap {
		event = "init"
	}
	defer func() {
		status := "success"
		severity := "info"
		message := fmt.Sprintf("server=%s host=%s bootstrap=%v", srv.Name, srv.Host, bootstrap)
		if err != nil {
			status = "failure"
			severity = "error"
			message = err.Error()
		}
		a.sendTelegramNotifyEvent(srv, telegrambot.NotifyEvent{
			Event:    event,
			Status:   status,
			Severity: severity,
			Source:   "ovpn-cli",
			Message:  message,
		})
	}()

	runner := a.newRunner("deploy")
	sshCfg := sshFromServer(srv)
	a.log().Info("starting deploy workflow", "server", srv.Name, "host", srv.Host, "bootstrap", bootstrap, "dry_run", a.dryRun)
	if bootstrap {
		if err = deploy.BootstrapRemote(a.ctx, runner, sshCfg); err != nil {
			return fmt.Errorf("bootstrap remote host %s for server %s: %w", srv.Host, srv.Name, err)
		}
	}
	if err := a.ensureRealityParity(); err != nil {
		return err
	}
	if err := a.materializeCanonicalUsersOnServer(srv); err != nil {
		return fmt.Errorf("materialize global users on %s: %w", srv.Name, err)
	}
	users, err := a.store.ListUsers(a.ctx, srv.ID)
	if err != nil {
		return err
	}
	users, err = a.usersForRuntimeConfig(srv, users)
	if err != nil {
		return err
	}
	a.log().Debug("loaded users for deploy", "server", srv.Name, "users", len(users))
	agentBinary, err := a.ensureAgentBinary()
	if err != nil {
		return err
	}
	telegramBotBinary, err := a.ensureTelegramBotBinary()
	if err != nil {
		return err
	}
	fallbackUpload, fallbackDownload, err := a.realityFallbackRateLimits()
	if err != nil {
		return err
	}
	deployInput, err := a.buildDeployInput(srv, users, agentBinary, telegramBotBinary, fallbackUpload, fallbackDownload)
	if err != nil {
		return err
	}
	bundle, err := deploy.RenderBundle(deployInput)
	if err != nil {
		return fmt.Errorf("render deploy bundle for server %s: %w", srv.Name, err)
	}
	defer deploy.CleanupBundle(bundle)
	a.log().Debug("bundle rendered", "server", srv.Name, "bundle_dir", bundle.Dir, "config_bytes", len(bundle.ConfigRaw))
	if err := deploy.UploadBundle(a.ctx, runner, sshCfg, bundle.Dir); err != nil {
		return fmt.Errorf("upload bundle to %s for server %s: %w", srv.Host, srv.Name, err)
	}
	if err := deploy.DeployRemote(a.ctx, runner, sshCfg); err != nil {
		return fmt.Errorf("deploy remote stack on %s for server %s: %w", srv.Host, srv.Name, err)
	}
	if err := a.syncUserPolicies(srv); err != nil {
		return fmt.Errorf("sync user policies on %s for server %s: %w", srv.Host, srv.Name, err)
	}
	if err := a.syncQuotaPolicy(srv); err != nil {
		return fmt.Errorf("sync quota policy on %s for server %s: %w", srv.Host, srv.Name, err)
	}
	if err := a.store.SetServerLastDeploy(a.ctx, srv.ID); err != nil {
		return err
	}
	rev := &model.DeployRevision{
		ServerID:   srv.ID,
		Revision:   time.Now().UTC().Format("20060102T150405"),
		ConfigHash: util.SHA256Bytes(bundle.ConfigRaw),
		AppliedBy:  os.Getenv("USER"),
		AppliedAt:  time.Now().UTC(),
		Status:     "ok",
	}
	if err := a.store.AddDeployRevision(a.ctx, rev); err != nil {
		return err
	}
	a.log().Info("deploy workflow completed", "server", srv.Name, "host", srv.Host, "config_hash", rev.ConfigHash)
	fmt.Println("deploy complete")
	return nil
}

// buildDeployInput builds deploy input from the current inputs and defaults.
func (a *App) buildDeployInput(
	srv model.Server,
	users []model.User,
	agentBinary string,
	telegramBotBinary string,
	fallbackUpload, fallbackDownload *xraycfg.FallbackRateLimit,
) (deploy.Input, error) {
	securityProfile, err := parseSecurityProfileFromEnv()
	if err != nil {
		return deploy.Input{}, err
	}
	var threatDNSServers []string
	if securityProfile == xraycfg.SecurityProfileMinimal {
		threatDNSServers, err = parseThreatDNSServersFromEnv()
		if err != nil {
			return deploy.Input{}, err
		}
	}
	notifyChatIDs, ownerUserID, err := normalizeTelegramDeployIDs(
		envOr("OVPN_TELEGRAM_NOTIFY_CHAT_IDS", ""),
		envOr("OVPN_TELEGRAM_OWNER_USER_ID", ""),
	)
	if err != nil {
		return deploy.Input{}, err
	}
	if strings.TrimSpace(envOr("OVPN_TELEGRAM_OWNER_USER_ID", "")) == "" && ownerUserID != "" {
		a.log().Info("OVPN_TELEGRAM_OWNER_USER_ID is empty; using first notify chat id as owner fallback", "owner_user_id", ownerUserID)
	}
	return deploy.Input{
		Server:                       srv,
		Users:                        users,
		SecurityProfile:              securityProfile,
		ThreatDNSServers:             threatDNSServers,
		RealityLimitFallbackUpload:   fallbackUpload,
		RealityLimitFallbackDownload: fallbackDownload,
		AgentBinaryPath:              agentBinary,
		TelegramBotBinaryPath:        telegramBotBinary,
		XrayImage:                    defaults.DefaultXrayImage(normalizeXrayVersionTag(srv.XrayVersion)),
		AgentImage:                   defaults.DefaultAgentImage,
		TelegramBotImage:             envOr("OVPN_TELEGRAM_BOT_IMAGE", defaults.DefaultTelegramBotImage),
		AgentLogLevel:                a.agentLogLevel(),
		AgentHostPort:                a.agentHostPort(),
		TelegramBotHostPort:          a.telegramBotHostPort(),
		AgentCertFile:                envOr("OVPN_AGENT_CERT_FILE", "/tmp/ovpn-agent-cert.pem"),
		AgentCertHostPath:            envOr("OVPN_CERT_FULLCHAIN_PATH", "/dev/null"),
		XrayLogLevel:                 a.xrayLogLevel(),
		PrometheusImage:              envOr("OVPN_PROMETHEUS_IMAGE", defaults.DefaultPrometheusImage),
		AlertmanagerImage:            envOr("OVPN_ALERTMANAGER_IMAGE", defaults.DefaultAlertmanagerImage),
		GrafanaImage:                 envOr("OVPN_GRAFANA_IMAGE", defaults.DefaultGrafanaImage),
		NodeExporterImage:            envOr("OVPN_NODE_EXPORTER_IMAGE", defaults.DefaultNodeExporterImage),
		CAdvisorImage:                envOr("OVPN_CADVISOR_IMAGE", defaults.DefaultCAdvisorImage),
		GrafanaAdminUser:             envOr("OVPN_GRAFANA_ADMIN_USER", "ovpn"),
		GrafanaAdminPassword:         envOr("OVPN_GRAFANA_ADMIN_PASSWORD", "change-me-now"),
		GrafanaPort:                  envOr("OVPN_GRAFANA_PORT", "3000"),
		TelegramNotifyChatIDs:        notifyChatIDs,
		TelegramOwnerUserID:          ownerUserID,
		TelegramClientsPDFPath:       envOr("OVPN_TELEGRAM_CLIENTS_PDF_PATH", "/opt/ovpn-telegram-bot/assets/clients.pdf"),
		TelegramClientsPDFSource:     envOr("OVPN_TELEGRAM_CLIENTS_PDF_SOURCE", "docs/clients.pdf"),
		TelegramAPIFallbackIPs:       envOr("OVPN_TELEGRAM_API_FALLBACK_IPS", "149.154.167.220"),
		TelegramAdminToken:           envOr("OVPN_TELEGRAM_ADMIN_TOKEN", ""),
		TelegramLinkAddress:          firstNonEmpty(srv.Domain, srv.Host),
		TelegramLinkServerName:       strings.TrimSpace(srv.RealityServerName),
		TelegramLinkPublicKey:        strings.TrimSpace(srv.RealityPublicKey),
		TelegramLinkShortID:          firstShortID(srv.RealityShortIDs),
	}, nil
}

// normalizeTelegramDeployIDs validates and normalizes telegram IDs used in runtime env.
func normalizeTelegramDeployIDs(notifyChatIDsRaw, ownerRaw string) (string, string, error) {
	notifyChatIDs := strings.TrimSpace(notifyChatIDsRaw)
	owner := strings.TrimSpace(ownerRaw)

	if notifyChatIDs != "" {
		ids, err := parseTelegramIDsOrdered(notifyChatIDs)
		if err != nil {
			return "", "", fmt.Errorf("invalid OVPN_TELEGRAM_NOTIFY_CHAT_IDS: %w", err)
		}
		normalized := make([]string, 0, len(ids))
		for _, id := range ids {
			normalized = append(normalized, strconv.FormatInt(id, 10))
		}
		notifyChatIDs = strings.Join(normalized, ",")
	}

	if owner != "" {
		ids, err := parseTelegramIDsOrdered(owner)
		if err != nil {
			return "", "", fmt.Errorf("invalid OVPN_TELEGRAM_OWNER_USER_ID: %w", err)
		}
		if len(ids) != 1 {
			return "", "", errors.New("invalid OVPN_TELEGRAM_OWNER_USER_ID: provide exactly one numeric Telegram user ID")
		}
		owner = strconv.FormatInt(ids[0], 10)
	}

	return notifyChatIDs, inferTelegramOwnerUserID(owner, notifyChatIDs), nil
}

// inferTelegramOwnerUserID returns explicit owner id or fallback from notify-chat ids.
func inferTelegramOwnerUserID(ownerRaw string, notifyChatIDsRaw string) string {
	owner := strings.TrimSpace(ownerRaw)
	if owner != "" {
		return owner
	}
	ids, err := parseTelegramIDsOrdered(strings.TrimSpace(notifyChatIDsRaw))
	if err != nil || len(ids) == 0 {
		return ""
	}
	return strconv.FormatInt(ids[0], 10)
}

// parseTelegramIDsOrdered parses CSV Telegram ids, preserving first-seen order.
func parseTelegramIDsOrdered(raw string) ([]int64, error) {
	items := util.ParseCSV(strings.TrimSpace(raw))
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]int64, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for _, item := range items {
		id, err := strconv.ParseInt(item, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", item, err)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// persistAndSyncPolicies persists config and syncs mirrored user+quota state to ovpn-agent.
func (a *App) persistAndSyncPolicies(srv model.Server) error {
	if err := a.persistConfigOnly(srv); err != nil {
		return err
	}
	if err := a.syncUserPolicies(srv); err != nil {
		return err
	}
	return a.syncQuotaPolicy(srv)
}

// persistConfigOnly executes persist config only against remote hosts over SSH.
func (a *App) persistConfigOnly(srv model.Server) error {
	// Runtime API changes are ephemeral across Xray restarts unless config.json is updated too.
	// This path persists the reconciled config without forcing an immediate compose restart.
	users, err := a.store.ListUsers(a.ctx, srv.ID)
	if err != nil {
		return err
	}
	users, err = a.usersForRuntimeConfig(srv, users)
	if err != nil {
		return err
	}
	spec, err := a.buildXraySpec(srv, users)
	if err != nil {
		return err
	}
	jsonRaw, err := xraycfg.RenderServerJSON(spec)
	if err != nil {
		return err
	}
	tmpFile := filepath.Join(os.TempDir(), "ovpn-config-persist.json")
	if err := os.WriteFile(tmpFile, jsonRaw, 0o644); err != nil {
		return err
	}
	defer os.Remove(tmpFile)
	runner := a.newRunner("persist_config")
	cfg := sshFromServer(srv)
	a.log().Debug("persisting config only", "server", srv.Name, "host", srv.Host, "config_bytes", len(jsonRaw))
	if err := runner.CopyFile(a.ctx, cfg, tmpFile, "/tmp/ovpn-config.json"); err != nil {
		return err
	}
	_, err = runner.Exec(a.ctx, cfg, "set -e; mkdir -p /opt/ovpn/xray; mv /tmp/ovpn-config.json /opt/ovpn/xray/config.json")
	return err
}

// syncQuotaPolicy executes quota policy flow and returns the first error.
func (a *App) syncQuotaPolicy(srv model.Server) error {
	// Only effectively enabled users are synced to quota policy so expired/manual-disabled users
	// stay disabled even after automatic rolling-window quota unblocks.
	users, err := a.store.ListUsers(a.ctx, srv.ID)
	if err != nil {
		return err
	}
	policies := make([]model.QuotaUserPolicy, 0, len(users))
	for _, u := range users {
		if !effectiveUserEnabled(u) {
			continue
		}
		policies = append(policies, model.QuotaUserPolicy{
			Email:            u.Email,
			UUID:             u.UUID,
			InboundTag:       "vless-reality",
			QuotaEnabled:     u.QuotaEnabled,
			MonthlyQuotaByte: u.TrafficLimitByte,
		})
	}
	var lastErr error
	for attempt := 1; attempt <= 15; attempt++ {
		if _, err := a.fetchRemoteAgent(srv, "POST", a.agentURL("/quota/sync"), map[string]any{"users": policies}); err != nil {
			lastErr = err
			time.Sleep(3 * time.Second)
			continue
		}
		a.log().Info("quota policy synced", "server", srv.Name, "enabled_users", len(policies), "attempt", attempt)
		return nil
	}
	if lastErr == nil {
		return errors.New("quota sync failed after retries")
	}
	agentBase := a.agentBaseURL()
	if strings.Contains(lastErr.Error(), "127.0.0.1 port") {
		return fmt.Errorf("quota sync failed after retries: %w; hint: ovpn-agent is not reachable on %s, check `ovpn server status %s` and `ovpn server logs %s --service ovpn-agent --tail 200`", lastErr, agentBase, srv.Name, srv.Name)
	}
	return fmt.Errorf("quota sync failed after retries: %w", lastErr)
}

// syncUserPolicies mirrors full user state to ovpn-agent for expiry enforcement and bot views.
func (a *App) syncUserPolicies(srv model.Server) error {
	users, err := a.store.ListUsers(a.ctx, srv.ID)
	if err != nil {
		return err
	}
	policies := make([]model.UserPolicy, 0, len(users))
	for _, u := range users {
		policies = append(policies, model.UserPolicy{
			Username:   u.Username,
			Email:      u.Email,
			UUID:       u.UUID,
			Enabled:    u.Enabled,
			ExpiryAt:   cloneTimePtr(u.ExpiryDate),
			InboundTag: "vless-reality",
		})
	}
	var lastErr error
	for attempt := 1; attempt <= 15; attempt++ {
		if _, err := a.fetchRemoteAgent(srv, "POST", a.agentURL("/users/sync"), map[string]any{"users": policies}); err != nil {
			lastErr = err
			time.Sleep(3 * time.Second)
			continue
		}
		a.log().Info("user policy synced", "server", srv.Name, "users", len(policies), "attempt", attempt)
		return nil
	}
	if lastErr == nil {
		return errors.New("user policy sync failed after retries")
	}
	return fmt.Errorf("user policy sync failed after retries: %w", lastErr)
}

// fetchQuotaStatus returns quota status for callers.
func (a *App) fetchQuotaStatus(srv model.Server, email string) (model.QuotaStatusResponse, error) {
	url := a.agentURL("/quota/status")
	if strings.TrimSpace(email) != "" {
		url += "?email=" + neturl.QueryEscape(email)
	}
	body, err := a.fetchRemoteAgent(srv, "GET", url, nil)
	if err != nil {
		return model.QuotaStatusResponse{}, err
	}
	var status model.QuotaStatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return model.QuotaStatusResponse{}, err
	}
	return status, nil
}

// isUserBlockedByQuota reports whether user blocked by quota.
func (a *App) isUserBlockedByQuota(srv model.Server, email string) (bool, error) {
	status, err := a.fetchQuotaStatus(srv, email)
	if err != nil {
		return false, fmt.Errorf("fetch quota status for %s on %s: %w", email, srv.Host, err)
	}
	if len(status.Users) == 0 {
		return false, nil
	}
	return status.Users[0].BlockedByQuota, nil
}

// usersForRuntimeConfig returns users for runtime config.
func (a *App) usersForRuntimeConfig(srv model.Server, users []model.User) ([]model.User, error) {
	// First deploy has no remote quota_state yet. Use local desired state as-is.
	if srv.LastDeployAt == nil {
		filtered := make([]model.User, 0, len(users))
		for _, u := range users {
			if !effectiveUserEnabled(u) {
				u.Enabled = false
			}
			filtered = append(filtered, u)
		}
		return filtered, nil
	}
	status, err := a.fetchQuotaStatus(srv, "")
	if err != nil {
		return nil, fmt.Errorf("fetch remote quota status before rendering config on %s: %w", srv.Host, err)
	}
	blocked := make(map[string]model.QuotaUserStatus, len(status.Users))
	for _, row := range status.Users {
		if row.BlockedByQuota {
			blocked[row.Email] = row
		}
	}
	if len(blocked) == 0 {
		return users, nil
	}
	filtered := make([]model.User, 0, len(users))
	for _, u := range users {
		if !effectiveUserEnabled(u) {
			u.Enabled = false
		}
		if row, ok := blocked[u.Email]; ok {
			a.log().Info("excluding quota-blocked user from rendered runtime config", "server", srv.Name, "username", u.Username, "email", u.Email)
			u.Enabled = false
			u.QuotaBlocked = true
			u.QuotaBlockedAt = row.BlockedAt
		}
		filtered = append(filtered, u)
	}
	return filtered, nil
}

func effectiveUserEnabled(u model.User) bool {
	return model.IsEffectivelyEnabled(u.Enabled, u.ExpiryDate, time.Now().UTC())
}

// applyRuntimeUser returns apply runtime user.
func (a *App) applyRuntimeUser(srv model.Server, user model.User, enable bool) error {
	path := "/runtime/user/remove"
	if enable {
		blockedByQuota, err := a.isUserBlockedByQuota(srv, user.Email)
		if err != nil {
			return err
		}
		if blockedByQuota {
			return errRuntimeQuotaBlocked
		}
		path = "/runtime/user/add"
	}
	payload := map[string]string{
		"inbound_tag": "vless-reality",
		"email":       user.Email,
		"uuid":        user.UUID,
	}
	a.log().Debug("applying runtime user operation", "server", srv.Name, "host", srv.Host, "username", user.Username, "email", user.Email, "operation", path)
	// Runtime operations are best-effort fast path. Callers fall back to full deploy when this fails.
	_, err := a.fetchRemoteAgent(srv, "POST", a.agentURL(path), payload)
	if err == nil {
		a.log().Info("runtime user operation applied", "server", srv.Name, "username", user.Username, "email", user.Email, "operation", path)
	}
	return err
}
