package remote

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/util"
)

type QuotaState struct {
	Email     string
	Blocked   bool
	BlockedAt *time.Time
}

// GetQuotaState reads quota state from the local database.
func (s *Store) GetQuotaState(ctx context.Context, email string) (QuotaState, bool, error) {
	var out QuotaState
	var blocked int
	var blockedMonth string
	var blockedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT email, blocked, blocked_month, blocked_at
		FROM quota_state
		WHERE email=?
	`, strings.TrimSpace(email)).Scan(&out.Email, &blocked, &blockedMonth, &blockedAt)
	if err == sql.ErrNoRows {
		return QuotaState{}, false, nil
	}
	if err != nil {
		return QuotaState{}, false, err
	}
	out.Blocked = blocked == 1
	if blockedAt.Valid {
		if t, err := time.Parse(time.RFC3339, blockedAt.String); err == nil {
			parsed := t
			out.BlockedAt = &parsed
		}
	}
	return out, true, nil
}

// ReplaceQuotaPolicies writes quota policies changes to the local database.
func (s *Store) ReplaceQuotaPolicies(ctx context.Context, users []model.QuotaUserPolicy) error {
	now := util.NowUTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM quota_policy`); err != nil {
		return err
	}
	emails := make([]string, 0, len(users))
	for _, u := range users {
		inboundTag := strings.TrimSpace(u.InboundTag)
		if inboundTag == "" {
			inboundTag = "vless-reality"
		}
		email := strings.TrimSpace(u.Email)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO quota_policy (email, uuid, inbound_tag, quota_enabled, monthly_quota_byte, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, email, strings.TrimSpace(u.UUID), inboundTag, boolToInt(u.QuotaEnabled), nullableInt64(u.MonthlyQuotaByte), now); err != nil {
			return err
		}
		emails = append(emails, email)
	}
	if len(emails) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM quota_state`); err != nil {
			return err
		}
	} else {
		holders := strings.Repeat("?,", len(emails))
		holders = strings.TrimSuffix(holders, ",")
		args := make([]any, 0, len(emails))
		for _, email := range emails {
			args = append(args, email)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM quota_state WHERE email NOT IN (`+holders+`)`, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListQuotaPolicies reads quota policies from the local database.
func (s *Store) ListQuotaPolicies(ctx context.Context) ([]model.QuotaUserPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT email, uuid, inbound_tag, quota_enabled, monthly_quota_byte
		FROM quota_policy
		ORDER BY email
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.QuotaUserPolicy
	for rows.Next() {
		var u model.QuotaUserPolicy
		var enabled int
		var quota sql.NullInt64
		if err := rows.Scan(&u.Email, &u.UUID, &u.InboundTag, &enabled, &quota); err != nil {
			return nil, err
		}
		u.QuotaEnabled = enabled == 1
		if quota.Valid {
			v := quota.Int64
			u.MonthlyQuotaByte = &v
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// GetQuotaPolicy reads quota policy from the local database.
func (s *Store) GetQuotaPolicy(ctx context.Context, email string) (model.QuotaUserPolicy, bool, error) {
	var u model.QuotaUserPolicy
	var enabled int
	var quota sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT email, uuid, inbound_tag, quota_enabled, monthly_quota_byte
		FROM quota_policy
		WHERE email=?
	`, strings.TrimSpace(email)).Scan(&u.Email, &u.UUID, &u.InboundTag, &enabled, &quota)
	if err == sql.ErrNoRows {
		return model.QuotaUserPolicy{}, false, nil
	}
	if err != nil {
		return model.QuotaUserPolicy{}, false, err
	}
	u.QuotaEnabled = enabled == 1
	if quota.Valid {
		v := quota.Int64
		u.MonthlyQuotaByte = &v
	}
	return u, true, nil
}

// SetQuotaBlocked writes quota blocked changes to the local database.
func (s *Store) SetQuotaBlocked(ctx context.Context, email string, blocked bool, blockedAt *time.Time) error {
	now := util.NowUTC().Format(time.RFC3339)
	if !blocked {
		blockedAt = nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO quota_state (email, blocked, blocked_month, blocked_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(email)
		DO UPDATE SET
			blocked=excluded.blocked,
			blocked_month=excluded.blocked_month,
			blocked_at=excluded.blocked_at,
			updated_at=excluded.updated_at
	`, strings.TrimSpace(email), boolToInt(blocked), "", nullableTime(blockedAt), now)
	return err
}

// ListQuotaStates reads quota states from the local database.
func (s *Store) ListQuotaStates(ctx context.Context) (map[string]QuotaState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT email, blocked, blocked_month, blocked_at
		FROM quota_state
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]QuotaState)
	for rows.Next() {
		var qs QuotaState
		var blocked int
		var blockedMonth string
		var blockedAt sql.NullString
		if err := rows.Scan(&qs.Email, &blocked, &blockedMonth, &blockedAt); err != nil {
			return nil, err
		}
		qs.Blocked = blocked == 1
		if blockedAt.Valid {
			if t, err := time.Parse(time.RFC3339, blockedAt.String); err == nil {
				parsed := t
				qs.BlockedAt = &parsed
			}
		}
		out[qs.Email] = qs
	}
	return out, rows.Err()
}

// ListUsageBetween reads usage between from the local database.
func (s *Store) ListUsageBetween(ctx context.Context, start time.Time, end time.Time) (map[string]int64, error) {
	startUTC := start.UTC()
	endUTC := end.UTC()
	rows, err := s.db.QueryContext(ctx, `
		SELECT email, COALESCE(SUM(uplink_bytes + downlink_bytes), 0)
		FROM user_traffic_daily
		WHERE window_start >= ? AND window_start < ?
		GROUP BY email
	`, startUTC.Format(time.RFC3339), endUTC.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var email string
		var usage int64
		if err := rows.Scan(&email, &usage); err != nil {
			return nil, err
		}
		out[email] = usage
	}
	return out, rows.Err()
}

// QuotaStatus returns quota status.
func (s *Store) QuotaStatus(ctx context.Context, now time.Time, window time.Duration, defaultQuotaByte int64, email string) (model.QuotaStatusResponse, error) {
	policies, err := s.ListQuotaPolicies(ctx)
	if err != nil {
		return model.QuotaStatusResponse{}, err
	}
	states, err := s.ListQuotaStates(ctx)
	if err != nil {
		return model.QuotaStatusResponse{}, err
	}
	end := now.UTC()
	if window <= 0 {
		window = 30 * 24 * time.Hour
	}
	start := end.Add(-window)
	usage, err := s.ListUsageBetween(ctx, start, end)
	if err != nil {
		return model.QuotaStatusResponse{}, err
	}
	filter := strings.TrimSpace(email)
	resp := model.QuotaStatusResponse{
		Window30DStart:   start.Format(time.RFC3339),
		Window30DEnd:     end.Format(time.RFC3339),
		DefaultQuotaByte: defaultQuotaByte,
	}
	for _, p := range policies {
		if filter != "" && p.Email != filter {
			continue
		}
		quota := defaultQuotaByte
		if p.MonthlyQuotaByte != nil && *p.MonthlyQuotaByte > 0 {
			quota = *p.MonthlyQuotaByte
		}
		st := states[p.Email]
		row := model.QuotaUserStatus{
			Email:              p.Email,
			QuotaEnabled:       p.QuotaEnabled,
			Window30DQuotaByte: quota,
			Window30DUsageByte: usage[p.Email],
			BlockedByQuota:     st.Blocked,
			BlockedAt:          st.BlockedAt,
			InboundTag:         p.InboundTag,
			HasRuntimeIdentity: strings.TrimSpace(p.UUID) != "" && strings.TrimSpace(p.InboundTag) != "",
		}
		if row.QuotaEnabled {
			resp.QuotaEnabledUsers++
		}
		if row.BlockedByQuota {
			resp.BlockedUsers++
		}
		resp.Users = append(resp.Users, row)
	}
	sort.Slice(resp.Users, func(i, j int) bool { return resp.Users[i].Email < resp.Users[j].Email })
	return resp, nil
}

// boolToInt returns bool to int.
func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// nullableTime returns nullable time.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// nullableInt64 returns nullable int 64.
func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// ClearQuotaState writes quota state changes to the local database.
func (s *Store) ClearQuotaState(ctx context.Context, email string) error {
	if strings.TrimSpace(email) == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM quota_state`)
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM quota_state WHERE email=?`, strings.TrimSpace(email))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("quota state not found for %s", strings.TrimSpace(email))
	}
	return nil
}
