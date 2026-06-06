package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	runtime     stats.RuntimeManager
	metrics     *agentMetrics
	logger      *slog.Logger
	xrayAPI     string
	dbPath      string
	collectOnce func(context.Context) error
	refreshOnce func(context.Context)
}

func parseSinceParam(raw string, fallback time.Duration) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC().Add(-fallback), nil
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return time.Time{}, fmt.Errorf("since duration must be positive")
		}
		return time.Now().UTC().Add(-d), nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("since must be a duration such as 24h or an RFC3339 timestamp")
}

func buildUserDiagnosticsResponse(ctx context.Context, store *remote.Store, email string, since time.Time, until time.Time) (model.UserDiagnosticsResponse, error) {
	email = strings.TrimSpace(email)
	userStatus, err := store.UserStatus(ctx, until, stats.DefaultQuotaWindow, stats.DefaultWindow30DQuotaBytes, email)
	if err != nil {
		return model.UserDiagnosticsResponse{}, err
	}
	var user *model.UserAccessStatus
	username := ""
	if len(userStatus.Users) > 0 {
		u := userStatus.Users[0]
		user = &u
		username = u.Username
	}
	conn, err := store.ConnectionDiagnostics(ctx, email, since, until, 5)
	if err != nil {
		return model.UserDiagnosticsResponse{}, err
	}
	windows, err := buildTrafficWindows(ctx, store, email, until)
	if err != nil {
		return model.UserDiagnosticsResponse{}, err
	}
	resp := model.UserDiagnosticsResponse{
		Time:           until.UTC().Format(time.RFC3339),
		Email:          email,
		Username:       username,
		User:           user,
		TrafficWindows: windows,
		Connections:    conn,
		Hints:          diagnosticsHints(user, conn),
	}
	return resp, nil
}

func buildTrafficWindows(ctx context.Context, store *remote.Store, email string, until time.Time) ([]model.TrafficWindow, error) {
	specs := []struct {
		name     string
		duration time.Duration
	}{
		{name: "1h", duration: time.Hour},
		{name: "6h", duration: 6 * time.Hour},
		{name: "24h", duration: 24 * time.Hour},
	}
	out := make([]model.TrafficWindow, 0, len(specs)+1)
	for _, spec := range specs {
		start := until.Add(-spec.duration)
		traffic, err := store.UserTrafficBetween(ctx, email, start, until)
		if err != nil {
			return nil, err
		}
		out = append(out, model.TrafficWindow{
			Window:        spec.name,
			Start:         start.UTC().Format(time.RFC3339),
			End:           until.UTC().Format(time.RFC3339),
			UplinkBytes:   traffic.UplinkBytes,
			DownlinkBytes: traffic.DownlinkBytes,
			TotalBytes:    traffic.UplinkBytes + traffic.DownlinkBytes,
		})
	}
	quota, err := store.QuotaStatus(ctx, until, stats.DefaultQuotaWindow, stats.DefaultWindow30DQuotaBytes, email)
	if err != nil {
		return nil, err
	}
	total30d := int64(0)
	if len(quota.Users) > 0 {
		total30d = quota.Users[0].Window30DUsageByte
	}
	out = append(out, model.TrafficWindow{
		Window:     "30d",
		Start:      quota.Window30DStart,
		End:        quota.Window30DEnd,
		TotalBytes: total30d,
	})
	return out, nil
}

func diagnosticsHints(user *model.UserAccessStatus, conn model.UserConnectionDiagnostics) []string {
	hints := []string{"shared UUID; source networks are not exact device count"}
	if user != nil {
		if user.BlockedByQuota {
			hints = append(hints, "user is currently blocked by rolling quota")
		}
		if !user.EffectiveEnabled {
			hints = append(hints, "user is not effectively enabled")
		}
	}
	if conn.AcceptedCount == 0 && conn.RejectedCount == 0 {
		hints = append(hints, "no parsed access-log connections in selected window")
	}
	if conn.DestinationIPv6Count > 0 {
		hints = append(hints, "IPv6 destinations were observed; check IPv4-only egress symptoms if apps connect but do not load")
	}
	if conn.SourceNetworksOverflow > 0 {
		hints = append(hints, "source network cap was reached; approximate source count is lower than real distinct networks")
	}
	return hints
}

// newMetricsRefreshFunc returns a closure that refreshes the Prometheus gauges (traffic totals, quota status, usage bands, and expiry) from the store.
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

		connRows, err := store.ListConnectionMetricSnapshots(rctx, now.Add(-24*time.Hour), now)
		if err != nil {
			logger.Warn("read connection diagnostics metrics failed", "error", err)
			metrics.OnDBWriteError("connection_metrics")
			return
		}
		metrics.setConnectionDiagnostics(connRows, "24h")
	}
}

// registerHTTPRoutes wires the agent's HTTP handlers (health, stats, quota, user sync, and runtime user add/remove) onto mux.
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
	mux.HandleFunc("/collect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		collectOnce := d.collectOnce
		if collectOnce == nil && d.collector != nil {
			collectOnce = d.collector.CollectOnce
		}
		if collectOnce == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "collector is not configured"})
			return
		}
		if err := collectOnce(ctx); err != nil {
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
	mux.HandleFunc("/diagnostics/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		if email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email is required"})
			return
		}
		since, err := parseSinceParam(r.URL.Query().Get("since"), 24*time.Hour)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		resp, err := buildUserDiagnosticsResponse(r.Context(), d.store, email, since, time.Now().UTC())
		if err != nil {
			d.logger.Warn("user diagnostics failed", "email", email, "error", err)
			d.metrics.OnDBWriteError("connection_diagnostics")
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc("/diagnostics/debug/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var req debugStartReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
			return
		}
		req.Email = strings.TrimSpace(req.Email)
		if req.Email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email is required"})
			return
		}
		duration := 15 * time.Minute
		if strings.TrimSpace(req.Duration) != "" {
			parsed, err := time.ParseDuration(strings.TrimSpace(req.Duration))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "duration must be a Go duration such as 15m"})
				return
			}
			duration = parsed
		}
		session, err := d.store.StartDebugSession(r.Context(), req.Email, duration, time.Now().UTC())
		if err != nil {
			d.logger.Warn("start user debug failed", "email", req.Email, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session": session})
	})
	mux.HandleFunc("/diagnostics/debug/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		now := time.Now().UTC()
		sessions, err := d.store.ListDebugSessions(r.Context(), now)
		if err != nil {
			d.logger.Warn("list user debug sessions failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, model.ConnectionDebugSessionsResponse{
			Time:     now.Format(time.RFC3339),
			Sessions: sessions,
		})
	})
	mux.HandleFunc("/diagnostics/debug/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		email := strings.TrimSpace(r.URL.Query().Get("email"))
		if email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email is required"})
			return
		}
		since, err := parseSinceParam(r.URL.Query().Get("since"), 15*time.Minute)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		until := time.Now().UTC()
		events, err := d.store.ListDebugEvents(r.Context(), email, since, until, 1000)
		if err != nil {
			d.logger.Warn("list user debug events failed", "email", email, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, model.ConnectionDebugEventsResponse{
			Email:  email,
			Since:  since.UTC().Format(time.RFC3339),
			Until:  until.Format(time.RFC3339),
			Events: events,
		})
	})
	mux.HandleFunc("/diagnostics/debug/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var req debugStopReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request"})
			return
		}
		req.Email = strings.TrimSpace(req.Email)
		if req.Email == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email is required"})
			return
		}
		if err := d.store.StopDebugSession(r.Context(), req.Email); err != nil {
			d.logger.Warn("stop user debug failed", "email", req.Email, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": req.Email})
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
		req.Email = strings.TrimSpace(req.Email)
		req.InboundTag = strings.TrimSpace(req.InboundTag)
		if req.Email == "" {
			d.metrics.observeRuntime("remove", "bad_request")
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "email is required"})
			return
		}
		if err := d.runtime.RemoveUser(r.Context(), req.InboundTag, req.Email); err != nil {
			if isRuntimeUserAbsentError(err) {
				d.metrics.observeRuntime("remove", "already_absent")
				d.metrics.OnXrayAPIReachable(true)
				d.logger.Info("runtime user already absent", "email", req.Email, "inbound_tag", req.InboundTag)
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "absent": true})
				return
			}
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
