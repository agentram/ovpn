package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"

	"ovpn/internal/model"
)

type userAuditRow struct {
	Username   string
	Email      string
	State      string
	Expiry     string
	UsageQuota string
	QuotaPct   float64
	QuotaPctUI string
	UUIDShort  string
}

func buildUserAuditRows(users []model.User, quotaByEmail map[string]model.QuotaUserStatus, now time.Time) []userAuditRow {
	rows := make([]userAuditRow, 0, len(users))
	for _, u := range users {
		quota := quotaByEmail[strings.TrimSpace(u.Email)]
		expired := model.IsExpiredAt(u.ExpiryDate, now)
		effectiveEnabled := model.IsEffectivelyEnabled(u.Enabled, u.ExpiryDate, now)
		daysUntil, hasExpiry := model.DaysUntilExpiry(u.ExpiryDate, now)
		state := userOperationalState(quota.BlockedByQuota, expired, effectiveEnabled, hasExpiry, daysUntil)

		expiry := model.ExpiryDateString(u.ExpiryDate)
		if expiry == "" {
			expiry = "none"
		}

		usageQuota := "n/a"
		pct := 0.0
		if quota.Window30DQuotaByte > 0 {
			usageQuota = fmt.Sprintf("%s / %s", formatTrafficBytes(quota.Window30DUsageByte), formatTrafficBytes(quota.Window30DQuotaByte))
			pct = quotaPercent(quota.Window30DUsageByte, quota.Window30DQuotaByte)
		}

		rows = append(rows, userAuditRow{
			Username:   u.Username,
			Email:      u.Email,
			State:      state,
			Expiry:     expiry,
			UsageQuota: usageQuota,
			QuotaPct:   pct,
			QuotaPctUI: fmt.Sprintf("%.1f%%", pct),
			UUIDShort:  shortUUID(u.UUID),
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		leftRank := userStateRank(rows[i].State)
		rightRank := userStateRank(rows[j].State)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if rows[i].QuotaPct != rows[j].QuotaPct {
			return rows[i].QuotaPct > rows[j].QuotaPct
		}
		if rows[i].Username != rows[j].Username {
			return rows[i].Username < rows[j].Username
		}
		return rows[i].Email < rows[j].Email
	})
	return rows
}

func renderUserAuditTable(rows []userAuditRow) string {
	tw := table.NewWriter()
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"#", "User", "Email", "State", "Expiry", "30d Usage/Quota", "Quota %", "UUID"})
	for i, row := range rows {
		tw.AppendRow(table.Row{i + 1, row.Username, row.Email, row.State, row.Expiry, row.UsageQuota, row.QuotaPctUI, row.UUIDShort})
	}
	if len(rows) == 0 {
		tw.AppendRow(table.Row{"-", "-", "-", "-", "-", "-", "-", "-"})
	}
	return tw.Render()
}

func shortUUID(v string) string {
	v = strings.TrimSpace(v)
	if len(v) <= 8 {
		return v
	}
	return v[:8]
}

func formatTrafficBytes(v int64) string {
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
		return fmt.Sprintf("%.2fTB", float64(v)/float64(tb))
	case v >= gb:
		return fmt.Sprintf("%.2fGB", float64(v)/float64(gb))
	case v >= mb:
		return fmt.Sprintf("%.2fMB", float64(v)/float64(mb))
	case v >= kb:
		return fmt.Sprintf("%.2fKB", float64(v)/float64(kb))
	default:
		return fmt.Sprintf("%dB", v)
	}
}

func userOperationalState(blockedByQuota, expired, effectiveEnabled, hasExpiry bool, daysUntil float64) string {
	switch {
	case blockedByQuota:
		return "blocked"
	case expired:
		return "expired"
	case hasExpiry && effectiveEnabled && daysUntil >= 0 && daysUntil <= 2:
		return "expiring"
	case !effectiveEnabled:
		return "disabled"
	default:
		return "ok"
	}
}

func userStateRank(state string) int {
	switch state {
	case "blocked":
		return 0
	case "expired":
		return 1
	case "expiring":
		return 2
	case "disabled":
		return 3
	default:
		return 4
	}
}
