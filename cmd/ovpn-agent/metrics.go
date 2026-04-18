package main

import (
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"ovpn/internal/model"
)

type agentMetrics struct {
	collectorRunsTotal          *prometheus.CounterVec
	collectorDurationSeconds    prometheus.Histogram
	collectorLastSuccessUnix    prometheus.Gauge
	collectorLastRunUnix        prometheus.Gauge
	collectorUsersSeen          prometheus.Gauge
	usersActive                 prometheus.Gauge
	userSpikeEventsTotal        prometheus.Counter
	collectorCounterResetsTotal prometheus.Counter
	dbWriteErrorsTotal          *prometheus.CounterVec
	runtimeOperationsTotal      *prometheus.CounterVec
	quotaEventsTotal            *prometheus.CounterVec
	quotaBlockedUsers           prometheus.Gauge
	quotaUsersOver80            prometheus.Gauge
	quotaUsersOver95            prometheus.Gauge
	userTrafficTotalBytes       *prometheus.GaugeVec
	userWindow30DUsageBytes     *prometheus.GaugeVec
	userWindow30DQuotaBytes     *prometheus.GaugeVec
	userQuotaPercent            *prometheus.GaugeVec
	userQuotaBlocked            *prometheus.GaugeVec
	userQuotaEnabled            *prometheus.GaugeVec
	userExpiryTimestamp         *prometheus.GaugeVec
	userExpired                 *prometheus.GaugeVec
	userEffectiveEnabled        *prometheus.GaugeVec
	userDaysUntilExpiry         *prometheus.GaugeVec
	usersExpiring2D             prometheus.Gauge
	usersExpired                prometheus.Gauge
	xrayAPIReachable            prometheus.Gauge
	healthChecksTotal           *prometheus.CounterVec
	certDaysUntilExpiry         prometheus.Gauge
	certNotAfterUnix            prometheus.Gauge
	certChecksTotal             *prometheus.CounterVec
}

// newAgentMetrics initializes agent metrics with the required dependencies.
func newAgentMetrics(reg prometheus.Registerer) *agentMetrics {
	// Keep labels low-cardinality and operationally bounded.
	// Per-user traffic stays in SQLite/HTTP endpoints, not in Prometheus labels.
	m := &agentMetrics{
		collectorRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ovpn_agent_collector_runs_total",
			Help: "Total number of collector runs grouped by result.",
		}, []string{"result"}),
		collectorDurationSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "ovpn_agent_collector_duration_seconds",
			Help:    "Collector run duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}),
		collectorLastSuccessUnix: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_collector_last_success_timestamp_seconds",
			Help: "Unix timestamp of the last successful collector run.",
		}),
		collectorLastRunUnix: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_collector_last_run_timestamp_seconds",
			Help: "Unix timestamp of the latest collector run start.",
		}),
		collectorUsersSeen: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_collector_users_seen",
			Help: "Number of user counters seen in the latest collector run.",
		}),
		usersActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_users_active",
			Help: "Number of users with non-zero traffic delta in the latest collector run.",
		}),
		userSpikeEventsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ovpn_agent_user_spike_events_total",
			Help: "Count of per-user traffic spike events detected per collection cycle.",
		}),
		collectorCounterResetsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "ovpn_agent_collector_counter_resets_total",
			Help: "Number of counter reset events detected during collection.",
		}),
		dbWriteErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ovpn_agent_db_write_errors_total",
			Help: "Total number of remote DB write/read errors grouped by operation.",
		}, []string{"operation"}),
		runtimeOperationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ovpn_agent_runtime_operations_total",
			Help: "Runtime add/remove user operations grouped by operation and result.",
		}, []string{"operation", "result"}),
		quotaEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ovpn_agent_quota_events_total",
			Help: "Quota block/unblock operations grouped by action and result.",
		}, []string{"action", "result"}),
		quotaBlockedUsers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_quota_blocked_users",
			Help: "Number of users currently blocked by rolling 30d quota.",
		}),
		quotaUsersOver80: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_quota_users_over_80",
			Help: "Number of quota-enabled users with rolling 30d usage at or above 80%.",
		}),
		quotaUsersOver95: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_quota_users_over_95",
			Help: "Number of quota-enabled users with rolling 30d usage at or above 95%.",
		}),
		userTrafficTotalBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_total_traffic_bytes",
			Help: "Per-user persisted traffic totals from remote store, grouped by direction (uplink/downlink/total).",
		}, []string{"email", "direction"}),
		userWindow30DUsageBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_window_30d_usage_bytes",
			Help: "Per-user usage in bytes for the current rolling 30d window.",
		}, []string{"email"}),
		userWindow30DQuotaBytes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_window_30d_quota_bytes",
			Help: "Per-user quota in bytes for the current rolling 30d window.",
		}, []string{"email"}),
		userQuotaPercent: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_quota_percent",
			Help: "Per-user rolling 30d quota usage percentage for quota-enabled users.",
		}, []string{"email"}),
		userQuotaBlocked: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_quota_blocked",
			Help: "Per-user quota blocked state (1=blocked, 0=not blocked).",
		}, []string{"email"}),
		userQuotaEnabled: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_quota_enabled",
			Help: "Per-user quota enabled state (1=enabled, 0=disabled).",
		}, []string{"email"}),
		userExpiryTimestamp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_expiry_timestamp_seconds",
			Help: "Per-user exclusive expiry cutoff unix timestamp; 0 means no expiry.",
		}, []string{"email", "expiry_date"}),
		userExpired: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_expired",
			Help: "Per-user expiry state after UTC end-of-day evaluation (1=expired, 0=active).",
		}, []string{"email", "expiry_date"}),
		userEffectiveEnabled: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_effective_enabled",
			Help: "Per-user effective enabled state after applying expiry semantics.",
		}, []string{"email", "expiry_date"}),
		userDaysUntilExpiry: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "ovpn_agent_user_days_until_expiry",
			Help: "Per-user days until exclusive expiry cutoff; negative means already expired, -10000 means no expiry.",
		}, []string{"email", "expiry_date"}),
		usersExpiring2D: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_users_expiring_2d",
			Help: "Number of effectively enabled users expiring within the next two days.",
		}),
		usersExpired: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_users_expired",
			Help: "Number of users whose expiry cutoff has passed.",
		}),
		xrayAPIReachable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_xray_api_reachable",
			Help: "Whether Xray API is reachable from ovpn-agent (1=yes, 0=no).",
		}),
		healthChecksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ovpn_agent_health_checks_total",
			Help: "Health endpoint checks grouped by xray reachability result.",
		}, []string{"result"}),
		certDaysUntilExpiry: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_cert_days_until_expiry",
			Help: "Certificate validity remaining in days for configured cert file. -10000 means unknown/not configured.",
		}),
		certNotAfterUnix: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "ovpn_agent_cert_not_after_timestamp_seconds",
			Help: "Configured certificate not_after unix timestamp. 0 means unknown.",
		}),
		certChecksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "ovpn_agent_cert_checks_total",
			Help: "Certificate check results grouped by result.",
		}, []string{"result"}),
	}
	reg.MustRegister(
		m.collectorRunsTotal,
		m.collectorDurationSeconds,
		m.collectorLastSuccessUnix,
		m.collectorLastRunUnix,
		m.collectorUsersSeen,
		m.usersActive,
		m.userSpikeEventsTotal,
		m.collectorCounterResetsTotal,
		m.dbWriteErrorsTotal,
		m.runtimeOperationsTotal,
		m.quotaEventsTotal,
		m.quotaBlockedUsers,
		m.quotaUsersOver80,
		m.quotaUsersOver95,
		m.userTrafficTotalBytes,
		m.userWindow30DUsageBytes,
		m.userWindow30DQuotaBytes,
		m.userQuotaPercent,
		m.userQuotaBlocked,
		m.userQuotaEnabled,
		m.userExpiryTimestamp,
		m.userExpired,
		m.userEffectiveEnabled,
		m.userDaysUntilExpiry,
		m.usersExpiring2D,
		m.usersExpired,
		m.xrayAPIReachable,
		m.healthChecksTotal,
		m.certDaysUntilExpiry,
		m.certNotAfterUnix,
		m.certChecksTotal,
	)
	m.certDaysUntilExpiry.Set(-10000)
	m.certNotAfterUnix.Set(0)
	return m
}

// OnCollectStart returns on collect start.
func (m *agentMetrics) OnCollectStart() {
	m.collectorLastRunUnix.Set(float64(time.Now().UTC().Unix()))
}

// OnCollectFinish returns on collect finish.
func (m *agentMetrics) OnCollectFinish(duration time.Duration, users int, err error) {
	m.collectorDurationSeconds.Observe(duration.Seconds())
	m.collectorUsersSeen.Set(float64(users))
	if err != nil {
		m.collectorRunsTotal.WithLabelValues("error").Inc()
		return
	}
	m.collectorRunsTotal.WithLabelValues("success").Inc()
	m.collectorLastSuccessUnix.Set(float64(time.Now().UTC().Unix()))
}

// OnCounterReset returns on counter reset.
func (m *agentMetrics) OnCounterReset() {
	m.collectorCounterResetsTotal.Inc()
}

// OnUsersActive returns on users active.
func (m *agentMetrics) OnUsersActive(count int) {
	if count < 0 {
		count = 0
	}
	m.usersActive.Set(float64(count))
}

// OnUserSpike returns on user spike.
func (m *agentMetrics) OnUserSpike(_ int64) {
	m.userSpikeEventsTotal.Inc()
}

// OnDBWriteError returns on db write error.
func (m *agentMetrics) OnDBWriteError(operation string) {
	if strings.TrimSpace(operation) == "" {
		operation = "unknown"
	}
	m.dbWriteErrorsTotal.WithLabelValues(operation).Inc()
}

// OnXrayAPIReachable returns on xray api reachable.
func (m *agentMetrics) OnXrayAPIReachable(reachable bool) {
	if reachable {
		m.xrayAPIReachable.Set(1)
		return
	}
	m.xrayAPIReachable.Set(0)
}

// observeRuntime returns observe runtime.
func (m *agentMetrics) observeRuntime(operation, result string) {
	if strings.TrimSpace(operation) == "" {
		operation = "unknown"
	}
	if strings.TrimSpace(result) == "" {
		result = "unknown"
	}
	m.runtimeOperationsTotal.WithLabelValues(operation, result).Inc()
}

// observeQuotaEvent returns observe quota event.
func (m *agentMetrics) observeQuotaEvent(action, result string) {
	if strings.TrimSpace(action) == "" {
		action = "unknown"
	}
	if strings.TrimSpace(result) == "" {
		result = "unknown"
	}
	m.quotaEventsTotal.WithLabelValues(action, result).Inc()
}

// setQuotaBlockedUsers applies quota blocked users and returns an error on failure.
func (m *agentMetrics) setQuotaBlockedUsers(blocked int) {
	m.quotaBlockedUsers.Set(float64(blocked))
}

// setQuotaUsageBands applies quota usage bands and returns an error on failure.
func (m *agentMetrics) setQuotaUsageBands(over80 int, over95 int) {
	if over80 < 0 {
		over80 = 0
	}
	if over95 < 0 {
		over95 = 0
	}
	m.quotaUsersOver80.Set(float64(over80))
	m.quotaUsersOver95.Set(float64(over95))
}

// setUserTrafficTotals applies user traffic totals and returns an error on failure.
func (m *agentMetrics) setUserTrafficTotals(rows []model.UserTraffic) {
	m.userTrafficTotalBytes.Reset()
	for _, row := range rows {
		email := strings.TrimSpace(row.Email)
		if email == "" {
			continue
		}
		uplink := row.UplinkBytes
		downlink := row.DownlinkBytes
		total := uplink + downlink
		m.userTrafficTotalBytes.WithLabelValues(email, "uplink").Set(float64(uplink))
		m.userTrafficTotalBytes.WithLabelValues(email, "downlink").Set(float64(downlink))
		m.userTrafficTotalBytes.WithLabelValues(email, "total").Set(float64(total))
	}
}

// setUserQuotaStatus applies user quota status and returns an error on failure.
func (m *agentMetrics) setUserQuotaStatus(status model.QuotaStatusResponse) {
	m.userWindow30DUsageBytes.Reset()
	m.userWindow30DQuotaBytes.Reset()
	m.userQuotaPercent.Reset()
	m.userQuotaBlocked.Reset()
	m.userQuotaEnabled.Reset()
	for _, user := range status.Users {
		email := strings.TrimSpace(user.Email)
		if email == "" {
			continue
		}
		enabled := 0.0
		if user.QuotaEnabled {
			enabled = 1
		}
		blocked := 0.0
		if user.BlockedByQuota {
			blocked = 1
		}
		quotaPercent := 0.0
		if user.Window30DQuotaByte > 0 && user.Window30DUsageByte > 0 {
			quotaPercent = (float64(user.Window30DUsageByte) * 100) / float64(user.Window30DQuotaByte)
		}
		m.userWindow30DUsageBytes.WithLabelValues(email).Set(float64(user.Window30DUsageByte))
		m.userWindow30DQuotaBytes.WithLabelValues(email).Set(float64(user.Window30DQuotaByte))
		m.userQuotaPercent.WithLabelValues(email).Set(quotaPercent)
		m.userQuotaBlocked.WithLabelValues(email).Set(blocked)
		m.userQuotaEnabled.WithLabelValues(email).Set(enabled)
	}
}

// setUserExpiryStatus applies user expiry state and aggregate counts.
func (m *agentMetrics) setUserExpiryStatus(status model.UserStatusResponse) {
	m.userExpiryTimestamp.Reset()
	m.userExpired.Reset()
	m.userEffectiveEnabled.Reset()
	m.userDaysUntilExpiry.Reset()
	m.usersExpiring2D.Set(float64(status.Expiring2DUsers))
	m.usersExpired.Set(float64(status.ExpiredUsers))
	for _, user := range status.Users {
		email := strings.TrimSpace(user.Email)
		if email == "" {
			continue
		}
		expiryDate := strings.TrimSpace(user.ExpiryDate)
		if expiryDate == "" {
			expiryDate = "none"
		}
		expired := 0.0
		if user.Expired {
			expired = 1
		}
		effectiveEnabled := 0.0
		if user.EffectiveEnabled {
			effectiveEnabled = 1
		}
		expiryTS := 0.0
		if user.ExpiryAt != nil {
			expiryTS = float64(user.ExpiryAt.UTC().Unix())
		}
		daysUntil := -10000.0
		if user.DaysUntilExpiry != nil {
			daysUntil = *user.DaysUntilExpiry
		}
		m.userExpiryTimestamp.WithLabelValues(email, expiryDate).Set(expiryTS)
		m.userExpired.WithLabelValues(email, expiryDate).Set(expired)
		m.userEffectiveEnabled.WithLabelValues(email, expiryDate).Set(effectiveEnabled)
		m.userDaysUntilExpiry.WithLabelValues(email, expiryDate).Set(daysUntil)
	}
}

// observeCertExpiry returns observe cert expiry.
func (m *agentMetrics) observeCertExpiry(certFile string, logger *slog.Logger) {
	certFile = strings.TrimSpace(certFile)
	if certFile == "" {
		m.certChecksTotal.WithLabelValues("skipped").Inc()
		return
	}
	raw, err := os.ReadFile(certFile)
	if err != nil {
		m.certChecksTotal.WithLabelValues("error").Inc()
		logger.Warn("read cert file failed", "cert_file", certFile, "error", err)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		m.certChecksTotal.WithLabelValues("skipped").Inc()
		return
	}
	// Parse the first PEM block; fullchain files put the leaf cert first which is enough for expiry alerts.
	block, _ := pem.Decode(raw)
	if block == nil {
		m.certChecksTotal.WithLabelValues("error").Inc()
		logger.Warn("no pem certificate found in cert file", "cert_file", certFile)
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		m.certChecksTotal.WithLabelValues("error").Inc()
		logger.Warn("parse certificate failed", "cert_file", certFile, "error", err)
		return
	}
	notAfter := cert.NotAfter.UTC()
	days := notAfter.Sub(time.Now().UTC()).Hours() / 24
	m.certNotAfterUnix.Set(float64(notAfter.Unix()))
	m.certDaysUntilExpiry.Set(days)
	m.certChecksTotal.WithLabelValues("success").Inc()
}
