package main

import (
	"fmt"
	"sort"

	"ovpn/internal/model"
)

// quotaPressure returns quota pressure.
func quotaPressure(status model.QuotaStatusResponse) (int, int) {
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
	return over80, over95
}

// trafficSummary returns traffic summary.
func trafficSummary(rows []model.UserTraffic) (users int, active int, total int64) {
	users = len(rows)
	for _, r := range rows {
		t := r.UplinkBytes + r.DownlinkBytes
		total += t
		if t > 0 {
			active++
		}
	}
	return users, active, total
}

// quotaPercent returns quota percent.
func quotaPercent(usage int64, quota int64) float64 {
	if usage <= 0 || quota <= 0 {
		return 0
	}
	return float64(usage) * 100 / float64(quota)
}

func blockedUsersCount(users []model.UserAccessStatus) int {
	count := 0
	for _, u := range users {
		if u.BlockedByQuota {
			count++
		}
	}
	return count
}

func renderUserState(u model.UserAccessStatus) string {
	switch {
	case u.BlockedByQuota:
		return "blocked"
	case u.Expired:
		return "expired"
	case u.DaysUntilExpiry != nil && *u.DaysUntilExpiry >= 0 && *u.DaysUntilExpiry <= 2:
		return "expiring"
	case u.EffectiveEnabled:
		return "ok"
	default:
		return "disabled"
	}
}

// formatBytes renders bytes into the format expected by callers.
func formatBytes(v int64) string {
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
		return fmt.Sprintf("%.2f TB", float64(v)/float64(tb))
	case v >= gb:
		return fmt.Sprintf("%.2f GB", float64(v)/float64(gb))
	case v >= mb:
		return fmt.Sprintf("%.2f MB", float64(v)/float64(mb))
	case v >= kb:
		return fmt.Sprintf("%.2f KB", float64(v)/float64(kb))
	default:
		return fmt.Sprintf("%d B", v)
	}
}

func sortUsersForAudit(users []model.UserAccessStatus) {
	sort.SliceStable(users, func(i, j int) bool {
		leftState := renderUserState(users[i])
		rightState := renderUserState(users[j])
		if userStateRank(leftState) != userStateRank(rightState) {
			return userStateRank(leftState) < userStateRank(rightState)
		}
		left := quotaPercent(users[i].Window30DUsageByte, users[i].Window30DQuotaByte)
		right := quotaPercent(users[j].Window30DUsageByte, users[j].Window30DQuotaByte)
		if left == right {
			leftName := usernameFromEmail(users[i].Email)
			rightName := usernameFromEmail(users[j].Email)
			if leftName == rightName {
				return users[i].Email < users[j].Email
			}
			return leftName < rightName
		}
		return left > right
	})
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
