package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"ovpn/internal/model"
)

type serviceCheck struct {
	Key         string
	Label       string
	Healthy     bool
	Detail      string
	Critical    bool
	Restartable bool
}

type auditSnapshot struct {
	CheckedAt time.Time

	Health    agentHealth
	HealthErr error

	Quota    quotaSnapshot
	QuotaErr error

	UserStatus    model.UserStatusResponse
	UserStatusErr error

	Totals    trafficSnapshot
	TotalsErr error

	Services []serviceCheck

	CollectorState string
	CollectorAge   time.Duration

	ActiveAlerts int
	AlertsErr    error

	Overall string
}

type quotaSnapshot struct {
	Enabled    int
	Blocked    int
	Over80     int
	Over95     int
	Users      int
	Expiring2D int
	Expired    int
}

type trafficSnapshot struct {
	Users    int
	Active   int
	Total    int64
	Uplink   int64
	Downlink int64
}

var restartableServiceOrder = []string{
	"xray",
	"haproxy",
	"ovpn-agent",
	"prometheus",
	"alertmanager",
	"grafana",
	"node-exporter",
	"cadvisor",
}

var serviceAliases = map[string]string{
	"xray":          "xray",
	"haproxy":       "haproxy",
	"ovpn-agent":    "ovpn-agent",
	"agent":         "ovpn-agent",
	"prometheus":    "prometheus",
	"prom":          "prometheus",
	"alertmanager":  "alertmanager",
	"alert":         "alertmanager",
	"grafana":       "grafana",
	"node-exporter": "node-exporter",
	"node_exporter": "node-exporter",
	"node":          "node-exporter",
	"cadvisor":      "cadvisor",
}

func restartableServicesHelp(includeHAProxy bool) string {
	return strings.Join(enabledRestartableServices(includeHAProxy), ", ")
}

func enabledRestartableServices(includeHAProxy bool) []string {
	out := make([]string, 0, len(restartableServiceOrder))
	for _, svc := range restartableServiceOrder {
		if svc == "haproxy" && !includeHAProxy {
			continue
		}
		out = append(out, svc)
	}
	return out
}

func normalizeServiceName(raw string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "", false
	}
	n, ok := serviceAliases[v]
	return n, ok
}

func (b *bot) hasHAProxy() bool {
	return strings.TrimSpace(b.cfg.haproxyURL) != ""
}

func (b *bot) collectAuditSnapshot(ctx context.Context) auditSnapshot {
	out := auditSnapshot{CheckedAt: time.Now().UTC(), CollectorState: "unknown", Overall: "WARN"}

	healthCtx, cancelHealth := context.WithTimeout(ctx, 3*time.Second)
	health, err := b.fetchHealth(healthCtx)
	cancelHealth()
	if err != nil {
		out.HealthErr = err
	} else {
		out.Health = health
		if parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(health.LastCollectAt)); parseErr == nil {
			out.CollectorAge = out.CheckedAt.Sub(parsed)
			if out.CollectorAge > 5*time.Minute {
				out.CollectorState = "stale"
			} else {
				out.CollectorState = "fresh"
			}
		}
	}

	quotaCtx, cancelQuota := context.WithTimeout(ctx, 3*time.Second)
	quotaStatus, err := b.fetchQuotaStatus(quotaCtx)
	cancelQuota()
	if err != nil {
		out.QuotaErr = err
	} else {
		over80, over95 := quotaPressure(quotaStatus)
		out.Quota = quotaSnapshot{
			Enabled: quotaStatus.QuotaEnabledUsers,
			Blocked: quotaStatus.BlockedUsers,
			Over80:  over80,
			Over95:  over95,
			Users:   len(quotaStatus.Users),
		}
	}

	usersCtx, cancelUsers := context.WithTimeout(ctx, 3*time.Second)
	userStatus, err := b.fetchUserStatus(usersCtx)
	cancelUsers()
	if err != nil {
		out.UserStatusErr = err
	} else {
		out.UserStatus = userStatus
		out.Quota.Expiring2D = userStatus.Expiring2DUsers
		out.Quota.Expired = userStatus.ExpiredUsers
	}

	trafficCtx, cancelTraffic := context.WithTimeout(ctx, 3*time.Second)
	rows, err := b.fetchTotals(trafficCtx)
	cancelTraffic()
	if err != nil {
		out.TotalsErr = err
	} else {
		users, active, total := trafficSummary(rows)
		up := int64(0)
		down := int64(0)
		for _, row := range rows {
			up += row.UplinkBytes
			down += row.DownlinkBytes
		}
		out.Totals = trafficSnapshot{Users: users, Active: active, Total: total, Uplink: up, Downlink: down}
	}

	out.Services = b.collectServiceChecks(ctx, out)

	if svc, ok := findServiceCheck(out.Services, "alertmanager"); ok && svc.Healthy {
		alertsCtx, cancelAlerts := context.WithTimeout(ctx, 3*time.Second)
		count, err := b.fetchActiveAlerts(alertsCtx)
		cancelAlerts()
		if err != nil {
			out.AlertsErr = err
		} else {
			out.ActiveAlerts = count
		}
	}

	out.Overall = computeOverall(out)
	return out
}

func (b *bot) collectServiceChecks(ctx context.Context, snapshot auditSnapshot) []serviceCheck {
	checks := make([]serviceCheck, 0, 9)
	checks = append(checks, b.serviceCheckFromSelfHealth(ctx))

	if snapshot.HealthErr != nil {
		checks = append(checks, serviceCheck{Key: "ovpn-agent", Label: "ovpn-agent", Healthy: false, Detail: snapshot.HealthErr.Error(), Critical: true, Restartable: true})
		checks = append(checks, serviceCheck{Key: "xray-via-agent", Label: "xray-via-agent", Healthy: false, Detail: "ovpn-agent health unavailable", Critical: true, Restartable: false})
	} else {
		agentDetail := "health endpoint reachable"
		if !snapshot.Health.OK {
			agentDetail = "health payload reports warning"
		}
		checks = append(checks, serviceCheck{Key: "ovpn-agent", Label: "ovpn-agent", Healthy: snapshot.Health.OK, Detail: agentDetail, Critical: true, Restartable: true})
		xrayHealthy := snapshot.Health.XrayAPIReachable
		xrayDetail := "xray API reachable"
		if !xrayHealthy {
			xrayDetail = strings.TrimSpace(snapshot.Health.XrayAPIError)
			if xrayDetail == "" {
				xrayDetail = "xray API not reachable from ovpn-agent"
			}
		}
		checks = append(checks, serviceCheck{Key: "xray-via-agent", Label: "xray-via-agent", Healthy: xrayHealthy, Detail: xrayDetail, Critical: true, Restartable: false})
	}

	checks = append(checks,
		b.serviceCheckFromProbe(ctx, serviceCheck{Key: "prometheus", Label: "prometheus", Critical: true, Restartable: true}, strings.TrimRight(b.cfg.prometheusURL, "/")+"/-/healthy"),
		b.serviceCheckFromProbe(ctx, serviceCheck{Key: "alertmanager", Label: "alertmanager", Critical: true, Restartable: true}, strings.TrimRight(b.cfg.alertmanagerURL, "/")+"/-/healthy"),
		b.serviceCheckFromProbe(ctx, serviceCheck{Key: "grafana", Label: "grafana", Critical: false, Restartable: true}, strings.TrimRight(b.cfg.grafanaURL, "/")+"/api/health"),
		b.serviceCheckFromProbe(ctx, serviceCheck{Key: "node-exporter", Label: "node-exporter", Critical: false, Restartable: true}, strings.TrimRight(b.cfg.nodeExporterURL, "/")+"/metrics"),
		b.serviceCheckFromProbe(ctx, serviceCheck{Key: "cadvisor", Label: "cadvisor", Critical: false, Restartable: true}, strings.TrimRight(b.cfg.cadvisorURL, "/")+"/healthz"),
	)
	if b.hasHAProxy() {
		checks = append(checks, b.serviceCheckFromProbe(ctx, serviceCheck{Key: "haproxy", Label: "haproxy", Critical: true, Restartable: true}, strings.TrimRight(b.cfg.haproxyURL, "/")))
	}

	sort.SliceStable(checks, func(i, j int) bool {
		if checks[i].Key == "ovpn-agent" {
			return true
		}
		if checks[j].Key == "ovpn-agent" {
			return false
		}
		return checks[i].Label < checks[j].Label
	})
	return checks
}

func (b *bot) serviceCheckFromSelfHealth(ctx context.Context) serviceCheck {
	base := serviceCheck{Key: "ovpn-telegram-bot", Label: "ovpn-telegram-bot", Critical: true, Restartable: false}
	health, err := b.fetchSelfHealth(ctx)
	if err != nil {
		base.Healthy = false
		base.Detail = err.Error()
		return base
	}
	base.Healthy = health.OK
	base.Detail = "status=" + defaultText(health.Status, "unknown")
	if strings.TrimSpace(health.LinkFeature) != "" {
		base.Detail += ", link=" + health.LinkFeature
	}
	if health.Health.ConsecutiveSendFailures >= 3 {
		base.Detail += fmt.Sprintf(", send_failures=%d", health.Health.ConsecutiveSendFailures)
	}
	return base
}

func (b *bot) serviceCheckFromProbe(ctx context.Context, base serviceCheck, targetURL string) serviceCheck {
	result := b.probe(ctx, base.Key, targetURL)
	base.Healthy = result.Healthy
	if result.Healthy {
		base.Detail = "reachable"
	} else {
		base.Detail = result.Detail
	}
	return base
}

func findServiceCheck(in []serviceCheck, key string) (serviceCheck, bool) {
	for _, row := range in {
		if row.Key == key {
			return row, true
		}
	}
	return serviceCheck{}, false
}

func computeOverall(s auditSnapshot) string {
	criticalFail := false
	warn := false
	for _, svc := range s.Services {
		if svc.Healthy {
			continue
		}
		if svc.Critical {
			criticalFail = true
		} else {
			warn = true
		}
	}
	if s.HealthErr != nil || s.QuotaErr != nil || s.UserStatusErr != nil || s.TotalsErr != nil || s.AlertsErr != nil {
		warn = true
	}
	if s.CollectorState == "stale" {
		warn = true
	}
	if criticalFail {
		return "FAIL"
	}
	if warn {
		return "WARN"
	}
	return "OK"
}

func (s auditSnapshot) serviceHealthCount() (healthy int, total int) {
	total = len(s.Services)
	for _, svc := range s.Services {
		if svc.Healthy {
			healthy++
		}
	}
	return healthy, total
}

func (s auditSnapshot) unhealthyRestartables() []string {
	out := make([]string, 0)
	for _, svc := range s.Services {
		if svc.Healthy || !svc.Restartable {
			continue
		}
		out = append(out, svc.Key)
	}
	sort.Strings(out)
	return out
}

func serviceLabelForKey(key string) string {
	switch key {
	case "xray":
		return "xray"
	case "ovpn-agent":
		return "ovpn-agent"
	case "prometheus":
		return "prometheus"
	case "alertmanager":
		return "alertmanager"
	case "grafana":
		return "grafana"
	case "node-exporter":
		return "node-exporter"
	case "cadvisor":
		return "cadvisor"
	default:
		return key
	}
}

func renderServiceLine(s serviceCheck) string {
	status := "OK"
	if !s.Healthy {
		status = "FAIL"
	}
	detail := strings.TrimSpace(s.Detail)
	if detail == "" {
		detail = "n/a"
	}
	return fmt.Sprintf("- %s: %s (%s)", s.Label, status, detail)
}
