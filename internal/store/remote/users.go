package remote

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/util"
)

// ReplaceUserPolicies writes mirrored user state to the remote database.
func (s *Store) ReplaceUserPolicies(ctx context.Context, users []model.UserPolicy) error {
	now := util.NowUTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_policy`); err != nil {
		return err
	}
	for _, u := range users {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_policy (email, username, uuid, enabled, expiry_at, inbound_tag, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, strings.TrimSpace(u.Email), strings.TrimSpace(u.Username), strings.TrimSpace(u.UUID), boolToInt(u.Enabled), nullableTime(u.ExpiryAt), strings.TrimSpace(u.InboundTag), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListUserPolicies reads mirrored user state from the remote database.
func (s *Store) ListUserPolicies(ctx context.Context) ([]model.UserPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT username, email, uuid, enabled, expiry_at, inbound_tag
		FROM user_policy
		ORDER BY email
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UserPolicy
	for rows.Next() {
		var row model.UserPolicy
		var enabled int
		var expiry sql.NullString
		if err := rows.Scan(&row.Username, &row.Email, &row.UUID, &enabled, &expiry, &row.InboundTag); err != nil {
			return nil, err
		}
		row.Enabled = enabled == 1
		if expiry.Valid {
			if parsed, err := time.Parse(time.RFC3339, expiry.String); err == nil {
				t := parsed.UTC()
				row.ExpiryAt = &t
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetUserPolicy reads one mirrored user state row by email.
func (s *Store) GetUserPolicy(ctx context.Context, email string) (model.UserPolicy, bool, error) {
	var row model.UserPolicy
	var enabled int
	var expiry sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT username, email, uuid, enabled, expiry_at, inbound_tag
		FROM user_policy
		WHERE email=?
	`, strings.TrimSpace(email)).Scan(&row.Username, &row.Email, &row.UUID, &enabled, &expiry, &row.InboundTag)
	if err == sql.ErrNoRows {
		return model.UserPolicy{}, false, nil
	}
	if err != nil {
		return model.UserPolicy{}, false, err
	}
	row.Enabled = enabled == 1
	if expiry.Valid {
		if parsed, err := time.Parse(time.RFC3339, expiry.String); err == nil {
			t := parsed.UTC()
			row.ExpiryAt = &t
		}
	}
	return row, true, nil
}

// UserStatus returns merged user state with expiry and quota data.
func (s *Store) UserStatus(ctx context.Context, now time.Time, window time.Duration, defaultQuotaByte int64, email string) (model.UserStatusResponse, error) {
	policies, err := s.ListUserPolicies(ctx)
	if err != nil {
		return model.UserStatusResponse{}, err
	}
	quotaStatus, err := s.QuotaStatus(ctx, now, window, defaultQuotaByte, "")
	if err != nil {
		return model.UserStatusResponse{}, err
	}
	quotaByEmail := make(map[string]model.QuotaUserStatus, len(quotaStatus.Users))
	for _, row := range quotaStatus.Users {
		quotaByEmail[row.Email] = row
	}
	filter := strings.TrimSpace(email)
	resp := model.UserStatusResponse{Time: now.UTC().Format(time.RFC3339)}
	for _, policy := range policies {
		if filter != "" && policy.Email != filter {
			continue
		}
		expired := model.IsExpiredAt(policy.ExpiryAt, now)
		effectiveEnabled := model.IsEffectivelyEnabled(policy.Enabled, policy.ExpiryAt, now)
		row := model.UserAccessStatus{
			Username:         policy.Username,
			Email:            policy.Email,
			UUID:             policy.UUID,
			Enabled:          policy.Enabled,
			ExpiryAt:         policy.ExpiryAt,
			ExpiryDate:       model.ExpiryDateString(policy.ExpiryAt),
			Expired:          expired,
			EffectiveEnabled: effectiveEnabled,
			InboundTag:       policy.InboundTag,
		}
		if days, ok := model.DaysUntilExpiry(policy.ExpiryAt, now); ok {
			row.DaysUntilExpiry = &days
		}
		if quota, ok := quotaByEmail[policy.Email]; ok {
			row.QuotaEnabled = quota.QuotaEnabled
			row.Window30DQuotaByte = quota.Window30DQuotaByte
			row.Window30DUsageByte = quota.Window30DUsageByte
			row.BlockedByQuota = quota.BlockedByQuota
			row.BlockedAt = quota.BlockedAt
			row.HasRuntimeIdentity = quota.HasRuntimeIdentity
		}
		if effectiveEnabled {
			resp.EffectiveEnabledUsers++
		}
		if expired {
			resp.ExpiredUsers++
		}
		if row.DaysUntilExpiry != nil && !expired && effectiveEnabled && *row.DaysUntilExpiry >= 0 && *row.DaysUntilExpiry <= 2 {
			resp.Expiring2DUsers++
		}
		resp.Users = append(resp.Users, row)
	}
	sort.Slice(resp.Users, func(i, j int) bool { return resp.Users[i].Email < resp.Users[j].Email })
	return resp, nil
}
