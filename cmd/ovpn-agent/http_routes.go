package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"ovpn/internal/model"
	"ovpn/internal/stats"
	"ovpn/internal/store/remote"
	"ovpn/internal/xrayapi"
)

type routeDeps struct {
	store       *remote.Store
	collector   *stats.Collector
	quota       *stats.QuotaEnforcer
	expiry      *stats.ExpiryEnforcer
	runtime     *runtimeGateway
	metrics     *agentMetrics
	logger      *slog.Logger
	xrayAPI     string
	dbPath      string
	refreshOnce func(context.Context)
}

// newMetricsRefreshFunc initializes metrics refresh func with the required dependencies.
func newMetricsRefreshFunc(store *remote.Store, logger *slog.Logger, metrics *agentMetrics) func(context.Context) {
	return func(rctx context.Context) {
		totals, err := store.ListTotals(rctx)
		if err != nil {
			logger.Warn("list totals failed for metrics refresh", "error", err)
			metrics.OnDBWriteError("list_totals")
		} else {
			metrics.setUserTrafficTotals(totals)
		}

		now := time.Now().UTC()
		status, err := store.QuotaStatus(rctx, now, stats.DefaultQuotaWindow, stats.DefaultWindow30DQuotaBytes, "")
		if err != nil {
			logger.Warn("read quota status failed", "error", err)
			metrics.OnDBWriteError("quota_status")
			return
		}
		metrics.setUserQuotaStatus(status)
		metrics.setQuotaBlockedUsers(status.BlockedUsers)
		over80 := 0
		over95 := 0
		for _, u := range status.Users {
			if !u.QuotaEnabled || u.Window30DQuotaByte <= 0 {
				continue
			}
			ratio := float64(u.Window30DUsageByte) / float64(u.Window30DQuotaByte)
			if ratio >= 0.80 {
				over80++
			}
			if ratio >= 0.95 {
				over95++
			}
		}
		metrics.setQuotaUsageBands(over80, over95)

		userStatus, err := store.UserStatus(rctx, now, stats.DefaultQuotaWindow, stats.DefaultWindow30DQuotaBytes, "")
		if err != nil {
			logger.Warn("read user status failed", "error", err)
			metrics.OnDBWriteError("user_status")
			return
		}
		metrics.setUserExpiryStatus(userStatus)
	}
}

// registerHTTPRoutes handles register http routes HTTP behavior for this service.
func registerHTTPRoutes(ctx context.Context, mux *http.ServeMux, d routeDeps) {
	// Serialize runtime add/remove calls to avoid concurrent AlterInbound races against one Xray process.
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		hctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		apiErr := xrayapi.EnsureAPIReachable(hctx, d.xrayAPI)
		xrayReachable := apiErr == nil
		d.metrics.OnXrayAPIReachable(xrayReachable)
		if xrayReachable {
			d.metrics.healthChecksTotal.WithLabelValues("success").Inc()
		} else {
			d.metrics.healthChecksTotal.WithLabelValues("error").Inc()
		}
		lastCollect, _, collectErr := d.store.GetMeta(hctx, "last_collect_at")
		if collectErr != nil {
			d.logger.Warn("read collector meta failed", "key", "last_collect_at", "error", collectErr)
			d.metrics.OnDBWriteError("get_meta_last_collect_at")
		}
		lastReset, _, resetErr := d.store.GetMeta(hctx, "last_reset_at")
		if resetErr != nil {
			d.logger.Warn("read collector meta failed", "key", "last_reset_at", "error", resetErr)
			d.metrics.OnDBWriteError("get_meta_last_reset_at")
		}
		payload := map[string]any{
			"ok":                 true,
			"service":            "ovpn-agent",
			"xray_api":           d.xrayAPI,
			"xray_api_reachable": xrayReachable,
			"db_path":            d.dbPath,
			"last_collect_at":    lastCollect,
			"last_reset_at":      lastReset,
			"time":               time.Now().UTC().Format(time.RFC3339),
		}
		if apiErr != nil {
			payload["xray_api_error"] = apiErr.Error()
		}
		writeJSON(w, http.StatusOK, payload)
	})
	mux.HandleFunc("/collect", func(w http.ResponseWriter, _ *http.Request) {
		if err := d.collector.CollectOnce(ctx); err != nil {
			d.logger.Warn("manual collect failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		d.logger.Info("manual collect completed")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/stats/total", func(w http.ResponseWriter, _ *http.Request) {
		rows, err := d.store.ListTotals(ctx)
		if err != nil {
			d.logger.Warn("list total stats failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
	mux.HandleFunc("/stats/daily", func(w http.ResponseWriter, r *http.Request) {
		day := time.Now().UTC()
		if q := strings.TrimSpace(r.URL.Query().Get("date")); q != "" {
			if parsed, err := time.Parse("2006-01-02", q); err == nil {
				day = parsed
			}
		}
		rows, err := d.store.ListDaily(ctx, day)
		if err != nil {
			d.logger.Warn("list daily stats failed", "date", day.Format("2006-01-02"), "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
	mux.HandleFunc("/quota/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var req quotaSyncReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
			return
		}
		if err := d.store.ReplaceQuotaPolicies(r.Context(), req.Users); err != nil {
			d.logger.Warn("quota sync failed", "error", err)
			d.metrics.OnDBWriteError("replace_quota_policy")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if err := d.quota.Enforce(r.Context(), time.Now().UTC()); err != nil {
			d.logger.Warn("quota enforcement after sync failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		d.refreshOnce(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "users": len(req.Users)})
	})
	mux.HandleFunc("/users/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var req usersSyncReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
			return
		}
		if err := d.store.ReplaceUserPolicies(r.Context(), req.Users); err != nil {
			d.logger.Warn("user sync failed", "error", err)
			d.metrics.OnDBWriteError("replace_user_policy")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if d.expiry != nil {
			if err := d.expiry.Enforce(r.Context(), time.Now().UTC()); err != nil {
				d.logger.Warn("expiry enforcement after sync failed", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
		}
		d.refreshOnce(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "users": len(req.Users)})
	})
	mux.HandleFunc("/quota/status", func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		status, err := d.store.QuotaStatus(r.Context(), time.Now().UTC(), stats.DefaultQuotaWindow, stats.DefaultWindow30DQuotaBytes, email)
		if err != nil {
			d.logger.Warn("quota status failed", "email", email, "error", err)
			d.metrics.OnDBWriteError("quota_status")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/users/status", func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		status, err := d.store.UserStatus(r.Context(), time.Now().UTC(), stats.DefaultQuotaWindow, stats.DefaultWindow30DQuotaBytes, email)
		if err != nil {
			d.logger.Warn("user status failed", "email", email, "error", err)
			d.metrics.OnDBWriteError("user_status")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("/quota/policies", handleQuotaPolicies(d.store, d.logger, d.metrics))
	mux.HandleFunc("/quota/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var req quotaResetReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
			return
		}
		req.Email = strings.TrimSpace(req.Email)
		if req.Email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email is required"})
			return
		}
		if err := d.store.SetQuotaBlocked(r.Context(), req.Email, false, nil); err != nil {
			d.logger.Warn("quota reset persist failed", "email", req.Email, "error", err)
			d.metrics.OnDBWriteError("quota_reset")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		resp := map[string]any{"ok": true, "email": req.Email, "runtime_readd": false}
		policy, ok, err := d.store.GetQuotaPolicy(r.Context(), req.Email)
		if err != nil {
			d.logger.Warn("quota reset policy lookup failed", "email", req.Email, "error", err)
			d.metrics.OnDBWriteError("quota_get_policy")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		userPolicy, userPolicyFound, err := d.store.GetUserPolicy(r.Context(), req.Email)
		if err != nil {
			d.logger.Warn("quota reset user-policy lookup failed", "email", req.Email, "error", err)
			d.metrics.OnDBWriteError("user_get_policy")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if ok && strings.TrimSpace(policy.UUID) != "" && userPolicyFound && model.IsEffectivelyEnabled(userPolicy.Enabled, userPolicy.ExpiryAt, time.Now().UTC()) {
			if err := d.runtime.AddUser(r.Context(), policy.InboundTag, policy.Email, policy.UUID); err != nil {
				d.logger.Warn("quota reset runtime add failed", "email", req.Email, "error", err)
				d.metrics.observeQuotaEvent("manual_reset", "error")
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			d.metrics.observeQuotaEvent("manual_reset", "success")
			resp["runtime_readd"] = true
		}
		d.refreshOnce(r.Context())
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc("/runtime/user/add", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			d.metrics.observeRuntime("add", "method_not_allowed")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var req runtimeReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			d.metrics.observeRuntime("add", "bad_request")
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
			return
		}
		req.Email = strings.TrimSpace(req.Email)
		req.UUID = strings.TrimSpace(req.UUID)
		req.InboundTag = strings.TrimSpace(req.InboundTag)
		if req.Email == "" || req.UUID == "" {
			d.metrics.observeRuntime("add", "bad_request")
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email and uuid are required"})
			return
		}
		// Quota state is authoritative for runtime add decisions. This prevents deploy/runtime sync
		// from re-adding a user that is still blocked by the rolling 30d hard cap.
		qs, found, err := d.store.GetQuotaState(r.Context(), req.Email)
		if err != nil {
			d.logger.Warn("runtime add quota-state lookup failed", "email", req.Email, "error", err)
			d.metrics.OnDBWriteError("quota_get_state")
			d.metrics.observeRuntime("add", "error")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if found && qs.Blocked {
			policy, policyFound, err := d.store.GetQuotaPolicy(r.Context(), req.Email)
			if err != nil {
				d.logger.Warn("runtime add quota-policy lookup failed", "email", req.Email, "error", err)
				d.metrics.OnDBWriteError("quota_get_policy")
				d.metrics.observeRuntime("add", "error")
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
				return
			}
			quotaDisabled := !policyFound || !policy.QuotaEnabled
			if quotaDisabled {
				if err := d.store.SetQuotaBlocked(r.Context(), req.Email, false, nil); err != nil {
					d.logger.Warn("runtime add quota-state cleanup failed", "email", req.Email, "error", err)
					d.metrics.OnDBWriteError("quota_set_blocked")
					d.metrics.observeRuntime("add", "error")
					writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
					return
				}
				d.refreshOnce(r.Context())
			} else {
				d.metrics.observeRuntime("add", "blocked_by_quota")
				writeJSON(w, http.StatusConflict, map[string]any{
					"error": "user is blocked by rolling 30d quota",
					"email": req.Email,
				})
				return
			}
		}
		userPolicy, ok, err := d.store.GetUserPolicy(r.Context(), req.Email)
		if err != nil {
			d.logger.Warn("runtime add user-policy lookup failed", "email", req.Email, "error", err)
			d.metrics.OnDBWriteError("user_get_policy")
			d.metrics.observeRuntime("add", "error")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if ok && !model.IsEffectivelyEnabled(userPolicy.Enabled, userPolicy.ExpiryAt, time.Now().UTC()) {
			d.metrics.observeRuntime("add", "blocked_by_expiry")
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "user is disabled or expired",
				"email": req.Email,
			})
			return
		}
		if err := d.runtime.AddUser(r.Context(), req.InboundTag, req.Email, req.UUID); err != nil {
			d.logger.Warn("runtime add user failed", "email", req.Email, "inbound_tag", req.InboundTag, "error", err)
			d.metrics.observeRuntime("add", "error")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		d.metrics.observeRuntime("add", "success")
		d.metrics.OnXrayAPIReachable(true)
		d.logger.Info("runtime user added", "email", req.Email, "inbound_tag", req.InboundTag)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("/runtime/user/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			d.metrics.observeRuntime("remove", "method_not_allowed")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var req runtimeReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			d.metrics.observeRuntime("remove", "bad_request")
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
			return
		}
		if err := d.runtime.RemoveUser(r.Context(), req.InboundTag, req.Email); err != nil {
			d.logger.Warn("runtime remove user failed", "email", req.Email, "inbound_tag", req.InboundTag, "error", err)
			d.metrics.observeRuntime("remove", "error")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		d.metrics.observeRuntime("remove", "success")
		d.metrics.OnXrayAPIReachable(true)
		d.logger.Info("runtime user removed", "email", req.Email, "inbound_tag", req.InboundTag)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}
