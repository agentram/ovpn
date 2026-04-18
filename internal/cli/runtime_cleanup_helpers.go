package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ovpn/internal/model"
)

// cleanupRemoveBackupsEffective returns cleanup remove backups effective.
func cleanupRemoveBackupsEffective(keepBackups, removeBackups bool) bool {
	if removeBackups {
		return true
	}
	return !keepBackups
}

// validateCleanupConfirm executes cleanup confirm flow and returns the first error.
func validateCleanupConfirm(confirm string, dryRun bool) error {
	confirm = strings.TrimSpace(confirm)
	if confirm == "CLEANUP" {
		return nil
	}
	if dryRun {
		fmt.Println("dry-run: --confirm CLEANUP is not required for preview mode")
		return nil
	}
	return errors.New("refusing destructive cleanup without explicit confirmation; pass --confirm CLEANUP")
}

// latestServerBackupRecord returns latest server backup record.
func (a *App) latestServerBackupRecord(serverID int64) (*model.BackupRecord, error) {
	records, err := a.store.ListBackupRecords(a.ctx, serverID)
	if err != nil {
		return nil, err
	}
	var latest *model.BackupRecord
	for i := range records {
		rec := records[i]
		if rec.ServerID != serverID || rec.Type != "server" {
			continue
		}
		if latest == nil || rec.CreatedAt.After(latest.CreatedAt) {
			copyRec := rec
			latest = &copyRec
		}
	}
	if latest == nil {
		return nil, errors.New("no server backup records found")
	}
	return latest, nil
}

// ensureRecentBackupForCleanup executes recent backup for cleanup flow and returns the first error.
func (a *App) ensureRecentBackupForCleanup(srv model.Server, maxAge time.Duration) (*model.BackupRecord, time.Duration, error) {
	latest, err := a.latestServerBackupRecord(srv.ID)
	if err != nil {
		return nil, 0, fmt.Errorf("backup safety check failed: no backup record found for server %s; run `ovpn server backup %s` first or pass --skip-backup-check", srv.Name, srv.Name)
	}
	age := time.Since(latest.CreatedAt)
	if age < 0 {
		age = 0
	}
	if age > maxAge {
		return latest, age, fmt.Errorf("backup safety check failed: latest backup for server %s is too old (%s, path=%s, created_at=%s); run `ovpn server backup %s` first or pass --skip-backup-check", srv.Name, roundDuration(age), latest.Path, latest.CreatedAt.UTC().Format(time.RFC3339), srv.Name)
	}
	return latest, age, nil
}

// roundDuration returns round duration.
func roundDuration(d time.Duration) time.Duration {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return d.Round(time.Second)
	}
	return d.Round(time.Minute)
}

type quotaSummaryOut struct {
	Window30DUsageByte int64   `json:"window_30d_usage_byte"`
	Window30DQuotaByte int64   `json:"window_30d_quota_byte"`
	QuotaPercent       float64 `json:"quota_percent"`
	RemainingByte      int64   `json:"remaining_byte"`
	BlockedByQuota     bool    `json:"blocked_by_quota"`
}

// quotaSummary returns quota summary.
func quotaSummary(status model.QuotaUserStatus) quotaSummaryOut {
	remaining := status.Window30DQuotaByte - status.Window30DUsageByte
	if remaining < 0 {
		remaining = 0
	}
	return quotaSummaryOut{
		Window30DUsageByte: status.Window30DUsageByte,
		Window30DQuotaByte: status.Window30DQuotaByte,
		QuotaPercent:       quotaPercent(status.Window30DUsageByte, status.Window30DQuotaByte),
		RemainingByte:      remaining,
		BlockedByQuota:     status.BlockedByQuota,
	}
}

// quotaPercent returns quota percent.
func quotaPercent(usage int64, quota int64) float64 {
	if usage <= 0 || quota <= 0 {
		return 0
	}
	pct := (float64(usage) * 100) / float64(quota)
	if pct < 0 {
		return 0
	}
	return pct
}

type userTopRow struct {
	Rank           int
	Username       string
	Email          string
	TotalBytes     int64
	UplinkBytes    int64
	DownlinkBytes  int64
	QuotaPercent   *float64
	BlockedByQuota bool
}

// buildUserTopRows builds user top rows from the current inputs and defaults.
func buildUserTopRows(totals []model.UserTraffic, users []model.User, quotaByEmail map[string]model.QuotaUserStatus, limit int) []userTopRow {
	usernames := make(map[string]string, len(users))
	for _, u := range users {
		usernames[u.Email] = u.Username
	}
	sort.Slice(totals, func(i, j int) bool {
		left := totals[i].UplinkBytes + totals[i].DownlinkBytes
		right := totals[j].UplinkBytes + totals[j].DownlinkBytes
		if left == right {
			return totals[i].Email < totals[j].Email
		}
		return left > right
	})
	if limit <= 0 {
		limit = 10
	}
	if limit > len(totals) {
		limit = len(totals)
	}
	rows := make([]userTopRow, 0, limit)
	for i := 0; i < limit; i++ {
		t := totals[i]
		total := t.UplinkBytes + t.DownlinkBytes
		username := usernames[t.Email]
		if username == "" {
			username = "-"
		}
		var quotaPct *float64
		blocked := false
		if q, ok := quotaByEmail[t.Email]; ok {
			pct := quotaPercent(q.Window30DUsageByte, q.Window30DQuotaByte)
			quotaPct = &pct
			blocked = q.BlockedByQuota
		}
		rows = append(rows, userTopRow{
			Rank:           i + 1,
			Username:       username,
			Email:          t.Email,
			TotalBytes:     total,
			UplinkBytes:    t.UplinkBytes,
			DownlinkBytes:  t.DownlinkBytes,
			QuotaPercent:   quotaPct,
			BlockedByQuota: blocked,
		})
	}
	return rows
}
