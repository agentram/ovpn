package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ovpn/internal/model"
)

// sendMainMenu returns send main menu.
func (b *bot) sendMainMenu(ctx context.Context, chatID int64) error {
	mode := "read-only audit"
	if b.adminActionsEnabled() {
		mode = "audit + owner recovery"
	}
	msg := strings.Join([]string{
		"Main menu",
		"Use buttons below for " + mode + ".",
		"For sensitive link lookup use Users -> User link (owner only).",
	}, "\n")
	return b.sendPlainMessage(ctx, chatID, msg, mainReplyKeyboard())
}

// sendHelp returns send help.
func (b *bot) sendHelp(ctx context.Context, chatID int64) error {
	mutating := "disabled"
	if b.adminActionsEnabled() {
		mutating = "enabled (owner-only)"
	}
	msg := strings.Join([]string{
		"ovpn Telegram bot",
		"Mode: audit-first, admin actions " + mutating,
		"Users are globally mirrored across servers; traffic/quota are local to this server.",
		"Expiry uses UTC end-of-day semantics and is shown in /users and /doctor.",
		"User link: " + b.linkFeatureStatus(),
		"Commands:",
		"/menu /start - open menu",
		"/status - compact status overview",
		"/services - service checks and drilldowns",
		"/doctor - full diagnostic report",
		"/users - user quota + expiry audit table",
		"/traffic - traffic totals",
		"/quota - quota pressure summary",
		"/restart <service> - owner restart with confirmation",
		"/heal - owner auto-fix unhealthy services",
		"/guide - send VPN client PDF guide",
		"/cancel - cancel active prompt or pending confirmation",
		"Restart services: " + restartableServicesHelp(),
	}, "\n")
	return b.sendPlainMessage(ctx, chatID, msg, mainReplyKeyboard())
}

// sendGuide returns send guide.
func (b *bot) sendGuide(ctx context.Context, chatID int64) error {
	path := strings.TrimSpace(b.cfg.clientsPDFPath)
	if path == "" {
		return b.sendPlainMessage(ctx, chatID, "Guide PDF path is not configured.", mainReplyKeyboard())
	}
	f, err := os.Open(path)
	if err != nil {
		return b.sendPlainMessage(ctx, chatID, "Guide PDF is unavailable on server. Ask operator to generate `docs/clients.pdf` with `make docs-pdf` and redeploy.", mainReplyKeyboard())
	}
	defer f.Close()
	if err := b.sendDocument(ctx, chatID, filepath.Base(path), f, "VPN client guide"); err != nil {
		return err
	}
	return nil
}

// renderStatusSummary renders compact status from unified diagnostics.
func (b *bot) renderStatusSummary(ctx context.Context) string {
	s := b.collectAuditSnapshot(ctx)
	healthy, total := s.serviceHealthCount()
	lines := []string{
		"Status",
		fmt.Sprintf("Overall: %s", s.Overall),
		fmt.Sprintf("Services: %d/%d healthy", healthy, total),
	}
	if botSvc, ok := findServiceCheck(s.Services, "ovpn-telegram-bot"); ok {
		botState := "unhealthy"
		if botSvc.Healthy {
			botState = "healthy"
		}
		lines = append(lines, fmt.Sprintf("Bot: %s", defaultText(botSvc.Detail, botState)))
	}

	switch s.CollectorState {
	case "fresh":
		lines = append(lines, fmt.Sprintf("Collector: fresh (%s ago)", s.CollectorAge.Round(time.Second)))
	case "stale":
		lines = append(lines, fmt.Sprintf("Collector: stale (%s ago)", s.CollectorAge.Round(time.Second)))
	default:
		lines = append(lines, "Collector: unknown")
	}

	if s.QuotaErr != nil {
		lines = append(lines, "Quota: unavailable")
	} else {
		lines = append(lines, fmt.Sprintf("Quota: enabled=%d blocked=%d over95=%d", s.Quota.Enabled, s.Quota.Blocked, s.Quota.Over95))
	}
	if s.UserStatusErr != nil {
		lines = append(lines, "Expiry: unavailable")
	} else {
		lines = append(lines, fmt.Sprintf("Expiry: expiring_2d=%d expired=%d", s.Quota.Expiring2D, s.Quota.Expired))
	}

	if s.TotalsErr != nil {
		lines = append(lines, "Traffic: unavailable")
	} else {
		lines = append(lines, fmt.Sprintf("Traffic: users=%d active=%d total=%s", s.Totals.Users, s.Totals.Active, formatBytes(s.Totals.Total)))
	}

	if s.AlertsErr != nil {
		lines = append(lines, "Alerts: unavailable")
	} else {
		lines = append(lines, fmt.Sprintf("Alerts: active=%d", s.ActiveAlerts))
	}
	return strings.Join(lines, "\n")
}

// renderServicesOverview renders service matrix from unified diagnostics.
func (b *bot) renderServicesOverview(ctx context.Context) string {
	s := b.collectAuditSnapshot(ctx)
	healthy, total := s.serviceHealthCount()
	lines := []string{
		"Services",
		fmt.Sprintf("Overall: %s", s.Overall),
		fmt.Sprintf("Healthy: %d/%d", healthy, total),
		"",
	}
	for _, svc := range s.Services {
		lines = append(lines, renderServiceLine(svc))
	}
	if !b.adminActionsEnabled() {
		lines = append(lines, "", "Admin actions: disabled")
	}
	return strings.Join(lines, "\n")
}

// renderSingleService renders one service from diagnostics.
func (b *bot) renderSingleService(ctx context.Context, key string) string {
	s := b.collectAuditSnapshot(ctx)
	svc, ok := findServiceCheck(s.Services, key)
	if !ok {
		return "Service check not found."
	}
	status := "OK"
	if !svc.Healthy {
		status = "FAIL"
	}
	lines := []string{
		"Service Details",
		fmt.Sprintf("Name: %s", svc.Label),
		fmt.Sprintf("Status: %s", status),
		"Detail: " + defaultText(strings.TrimSpace(svc.Detail), "n/a"),
	}
	if svc.Restartable {
		lines = append(lines, "Restartable: yes")
	} else {
		lines = append(lines, "Restartable: no")
	}
	return strings.Join(lines, "\n")
}

// renderDoctorReport renders full diagnostic report.
func (b *bot) renderDoctorReport(ctx context.Context) string {
	s := b.collectAuditSnapshot(ctx)
	healthy, total := s.serviceHealthCount()
	lines := []string{
		"Doctor Report",
		fmt.Sprintf("Time: %s", s.CheckedAt.Format(time.RFC3339)),
		fmt.Sprintf("Overall: %s", s.Overall),
		fmt.Sprintf("Services: %d/%d healthy", healthy, total),
		"",
		"Service Matrix:",
	}
	for _, svc := range s.Services {
		lines = append(lines, renderServiceLine(svc))
	}

	lines = append(lines, "", "Collector:")
	switch s.CollectorState {
	case "fresh":
		lines = append(lines, fmt.Sprintf("- state: fresh (%s ago)", s.CollectorAge.Round(time.Second)))
	case "stale":
		lines = append(lines, fmt.Sprintf("- state: stale (%s ago)", s.CollectorAge.Round(time.Second)))
	default:
		lines = append(lines, "- state: unknown")
	}

	lines = append(lines, "", "Quota:")
	if s.QuotaErr != nil {
		lines = append(lines, "- unavailable: "+strings.TrimSpace(s.QuotaErr.Error()))
	} else {
		lines = append(lines,
			fmt.Sprintf("- users: %d", s.Quota.Users),
			fmt.Sprintf("- enabled: %d", s.Quota.Enabled),
			fmt.Sprintf("- blocked: %d", s.Quota.Blocked),
			fmt.Sprintf("- over80: %d", s.Quota.Over80),
			fmt.Sprintf("- over95: %d", s.Quota.Over95),
			fmt.Sprintf("- expiring_2d: %d", s.Quota.Expiring2D),
			fmt.Sprintf("- expired: %d", s.Quota.Expired),
		)
	}

	lines = append(lines, "", "Traffic:")
	if s.TotalsErr != nil {
		lines = append(lines, "- unavailable: "+strings.TrimSpace(s.TotalsErr.Error()))
	} else {
		lines = append(lines,
			fmt.Sprintf("- users: %d", s.Totals.Users),
			fmt.Sprintf("- active: %d", s.Totals.Active),
			fmt.Sprintf("- uplink: %s", formatBytes(s.Totals.Uplink)),
			fmt.Sprintf("- downlink: %s", formatBytes(s.Totals.Downlink)),
			fmt.Sprintf("- total: %s", formatBytes(s.Totals.Total)),
		)
	}

	lines = append(lines, "", "Alerts:")
	if s.AlertsErr != nil {
		lines = append(lines, "- unavailable: "+strings.TrimSpace(s.AlertsErr.Error()))
	} else {
		lines = append(lines, fmt.Sprintf("- active: %d", s.ActiveAlerts))
	}
	return strings.Join(lines, "\n")
}

// renderUsersList renders users in a compact mobile-friendly audit format.
func (b *bot) renderUsersList(ctx context.Context) ([]string, error) {
	status, err := b.fetchUserStatus(ctx)
	if err != nil {
		return nil, err
	}
	users := append([]model.UserAccessStatus(nil), status.Users...)
	sortUsersForAudit(users)
	lines := make([]string, 0, len(users)+2)
	header := padText("#", 2) + " " + padText("user", 10) + " " + padText("state", 8) + " " + padText("expiry", 10) + " " + padText("usage/quota", 15) + " " + padText("%", 5)
	for i, u := range users {
		name := usernameFromEmail(u.Email)
		pct := quotaPercent(u.Window30DUsageByte, u.Window30DQuotaByte)
		state := renderUserState(u)
		usageQuota := formatBytesCompact(u.Window30DUsageByte) + "/" + formatBytesCompact(u.Window30DQuotaByte)
		lines = append(lines, escapeTelegramHTML(
			padText(fmt.Sprintf("%d", i+1), 2)+" "+
				padText(name, 10)+" "+
				padText(state, 8)+" "+
				padText(defaultText(u.ExpiryDate, "none"), 10)+" "+
				padText(usageQuota, 15)+" "+
				padText(fmt.Sprintf("%.1f", pct), 5),
		))
	}
	if len(users) == 0 {
		lines = append(lines, escapeTelegramHTML("(no users)"))
	}
	totals := fmt.Sprintf("Tot %d  Eff %d  Blk %d  Exp2d %d  Expd %d", len(users), status.EffectiveEnabledUsers, blockedUsersCount(users), status.Expiring2DUsers, status.ExpiredUsers)
	return buildPreformattedMessages("Users Audit", escapeTelegramHTML(totals), escapeTelegramHTML(header), lines, telegramMessageLimit), nil
}

// renderTopUsers renders top users into the format expected by callers.
func (b *bot) renderTopUsers(ctx context.Context, limit int) (string, error) {
	rows, err := b.fetchTotals(ctx)
	if err != nil {
		return "", err
	}
	sort.Slice(rows, func(i, j int) bool {
		left := rows[i].UplinkBytes + rows[i].DownlinkBytes
		right := rows[j].UplinkBytes + rows[j].DownlinkBytes
		if left == right {
			return rows[i].Email < rows[j].Email
		}
		return left > right
	})
	if len(rows) == 0 {
		return "Top Users\nNo traffic data yet.", nil
	}
	if limit <= 0 {
		limit = 10
	}
	if len(rows) < limit {
		limit = len(rows)
	}
	lines := []string{"Top Users by Traffic"}
	for i := 0; i < limit; i++ {
		total := rows[i].UplinkBytes + rows[i].DownlinkBytes
		lines = append(lines, fmt.Sprintf("%d) @%s - %s", i+1, usernameFromEmail(rows[i].Email), formatBytes(total)))
	}
	return strings.Join(lines, "\n"), nil
}

// renderTrafficTotals renders traffic totals into the format expected by callers.
func (b *bot) renderTrafficTotals(ctx context.Context) (string, error) {
	rows, err := b.fetchTotals(ctx)
	if err != nil {
		return "", err
	}
	users, active, totalBytes := trafficSummary(rows)
	up, down := int64(0), int64(0)
	for _, r := range rows {
		up += r.UplinkBytes
		down += r.DownlinkBytes
	}
	return strings.Join([]string{
		"Traffic Totals",
		fmt.Sprintf("Users: %d", users),
		fmt.Sprintf("Active users: %d", active),
		"Uplink: " + formatBytes(up),
		"Downlink: " + formatBytes(down),
		"Total: " + formatBytes(totalBytes),
	}, "\n"), nil
}

// renderTrafficToday renders traffic today into the format expected by callers.
func (b *bot) renderTrafficToday(ctx context.Context) (string, error) {
	rows, err := b.fetchDaily(ctx, time.Now().UTC())
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "Traffic Today\nNo traffic data for today.", nil
	}
	up, down := int64(0), int64(0)
	for _, r := range rows {
		up += r.UplinkBytes
		down += r.DownlinkBytes
	}
	return strings.Join([]string{
		"Traffic Today",
		fmt.Sprintf("Users with entries: %d", len(rows)),
		"Uplink: " + formatBytes(up),
		"Downlink: " + formatBytes(down),
		"Total: " + formatBytes(up+down),
	}, "\n"), nil
}

// renderQuotaSummary renders quota summary into the format expected by callers.
func (b *bot) renderQuotaSummary(ctx context.Context) (string, error) {
	status, err := b.fetchQuotaStatus(ctx)
	if err != nil {
		return "", err
	}
	over80, over95 := quotaPressure(status)
	return strings.Join([]string{
		"Quota Summary (rolling 30d)",
		"Window start: " + defaultText(status.Window30DStart, "n/a"),
		"Window end: " + defaultText(status.Window30DEnd, "n/a"),
		fmt.Sprintf("Quota enabled users: %d", status.QuotaEnabledUsers),
		fmt.Sprintf("Blocked users: %d", status.BlockedUsers),
		fmt.Sprintf("Users over 80%%: %d", over80),
		fmt.Sprintf("Users over 95%%: %d", over95),
	}, "\n"), nil
}

// renderQuotaThreshold renders quota threshold into the format expected by callers.
func (b *bot) renderQuotaThreshold(ctx context.Context, threshold float64, title string) (string, error) {
	status, err := b.fetchQuotaStatus(ctx)
	if err != nil {
		return "", err
	}
	lines := []string{title}
	count := 0
	for _, u := range status.Users {
		if !u.QuotaEnabled || u.Window30DQuotaByte <= 0 {
			continue
		}
		pct := quotaPercent(u.Window30DUsageByte, u.Window30DQuotaByte)
		if pct < threshold*100 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d) @%s - %.1f%%", count+1, usernameFromEmail(u.Email), pct))
		count++
		if count >= 20 {
			break
		}
	}
	if count == 0 {
		lines = append(lines, "No users in this range.")
	}
	return strings.Join(lines, "\n"), nil
}

// renderQuotaBlocked renders quota blocked into the format expected by callers.
func (b *bot) renderQuotaBlocked(ctx context.Context) (string, error) {
	status, err := b.fetchQuotaStatus(ctx)
	if err != nil {
		return "", err
	}
	lines := []string{"Blocked by Quota"}
	count := 0
	for _, u := range status.Users {
		if !u.BlockedByQuota {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d) @%s", count+1, usernameFromEmail(u.Email)))
		count++
	}
	if count == 0 {
		lines = append(lines, "No blocked users.")
	}
	return strings.Join(lines, "\n"), nil
}

// formatBytesCompact keeps user lines short for Telegram mobile screens.
func formatBytesCompact(v int64) string {
	if v < 0 {
		v = 0
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
		tb = 1024 * gb
	)
	switch {
	case v >= tb:
		return fmt.Sprintf("%.1fTB", float64(v)/float64(tb))
	case v >= gb:
		return fmt.Sprintf("%.1fGB", float64(v)/float64(gb))
	case v >= mb:
		return fmt.Sprintf("%.0fMB", float64(v)/float64(mb))
	case v >= kb:
		return fmt.Sprintf("%.0fKB", float64(v)/float64(kb))
	default:
		return fmt.Sprintf("%dB", v)
	}
}
