package local

import (
	"context"
	"errors"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/util"
)

// AddUser writes user changes to the local database.
func (s *Store) AddUser(ctx context.Context, u *model.User) error {
	if !u.QuotaEnabled {
		u.QuotaEnabled = true
	}
	if err := u.Validate(); err != nil {
		return err
	}
	now := util.NowUTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (
			server_id, username, uuid, email, enabled, expiry_date, traffic_limit_byte, quota_enabled, quota_blocked, quota_blocked_at, notes, tags_csv, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, u.ServerID, u.Username, u.UUID, u.Email, boolToInt(u.Enabled), nullableTime(u.ExpiryDate), nullableInt64(u.TrafficLimitByte), boolToInt(u.QuotaEnabled), boolToInt(u.QuotaBlocked), nullableTime(u.QuotaBlockedAt), u.Notes, u.TagsCSV, now, now)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	u.ID = id
	return s.syncTags(ctx, u)
}

// UpdateUser writes user changes to the local database.
func (s *Store) UpdateUser(ctx context.Context, u *model.User) error {
	if err := u.Validate(); err != nil {
		return err
	}
	now := util.NowUTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET username=?, uuid=?, email=?, enabled=?, expiry_date=?, traffic_limit_byte=?, quota_enabled=?, quota_blocked=?, quota_blocked_at=?, notes=?, tags_csv=?, updated_at=?
		WHERE id=?
	`, u.Username, u.UUID, u.Email, boolToInt(u.Enabled), nullableTime(u.ExpiryDate), nullableInt64(u.TrafficLimitByte), boolToInt(u.QuotaEnabled), boolToInt(u.QuotaBlocked), nullableTime(u.QuotaBlockedAt), u.Notes, u.TagsCSV, now, u.ID)
	if err != nil {
		return err
	}
	return s.syncTags(ctx, u)
}

// DeleteUser writes user changes to the local database.
func (s *Store) DeleteUser(ctx context.Context, serverID int64, username string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE server_id=? AND username=?`, serverID, username)
	return err
}

// GetUser reads user from the local database.
func (s *Store) GetUser(ctx context.Context, serverID int64, username string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, server_id, username, uuid, email, enabled, expiry_date, traffic_limit_byte, quota_enabled, quota_blocked, quota_blocked_at, notes, tags_csv, created_at, updated_at
		FROM users WHERE server_id=? AND username=?
	`, serverID, username)
	return scanUser(row)
}

// ListUsers reads users from the local database.
func (s *Store) ListUsers(ctx context.Context, serverID int64) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, server_id, username, uuid, email, enabled, expiry_date, traffic_limit_byte, quota_enabled, quota_blocked, quota_blocked_at, notes, tags_csv, created_at, updated_at
		FROM users WHERE server_id=? ORDER BY username
	`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUserRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// ListEnabledUsers reads enabled users from the local database.
func (s *Store) ListEnabledUsers(ctx context.Context, serverID int64) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, server_id, username, uuid, email, enabled, expiry_date, traffic_limit_byte, quota_enabled, quota_blocked, quota_blocked_at, notes, tags_csv, created_at, updated_at
		FROM users WHERE server_id=? AND enabled=1 ORDER BY username
	`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUserRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// syncTags executes tags flow and returns the first error.
func (s *Store) syncTags(ctx context.Context, u *model.User) error {
	if u.ID == 0 {
		return errors.New("user ID is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_tags WHERE user_id=?`, u.ID); err != nil {
		return err
	}
	for _, tag := range util.ParseCSV(u.TagsCSV) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_tags (user_id, tag) VALUES (?, ?)`, u.ID, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}
