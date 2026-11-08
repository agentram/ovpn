package local

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/model"
)

// UserPlanSnapshot reads one SQLite snapshot. Identity values are hashed privately,
// never returned to callers; server private keys and SSH credentials are not read.
func (s *Store) UserPlanSnapshot(ctx context.Context, username string) ([]dto.UserPlanTarget, string, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	hash := sha256.New()
	// Hash identity values as well as policy: updated_at has second precision and
	// cannot detect two changes made during the same second.
	for _, query := range []string{
		`SELECT id,name,role,host,domain,enabled,primary_profile,enabled_profiles,xray_version,reality_public_key,reality_short_ids,reality_server_name,reality_target,reality_min_client_version,updated_at FROM servers ORDER BY id`,
		`SELECT id,server_id,username,uuid,email,enabled,expiry_date,traffic_limit_byte,quota_enabled,quota_blocked,notes,tags_csv,updated_at FROM users ORDER BY id`,
	} {
		if err := hashPlanRows(ctx, tx, query, hash); err != nil {
			return nil, "", false, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.name,s.role,s.primary_profile,s.enabled_profiles,
		u.id,u.enabled,u.expiry_date,u.traffic_limit_byte,u.quota_enabled,u.quota_blocked
		FROM servers s LEFT JOIN users u ON u.server_id=s.id AND u.username=?
		WHERE s.enabled=1 ORDER BY s.name,s.id LIMIT 101`, username)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	targets := make([]dto.UserPlanTarget, 0)
	for rows.Next() {
		var srv model.Server
		var id, enabled, quota, quotaEnabled, blocked sql.NullInt64
		var expiry sql.NullString
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Role, &srv.PrimaryProfile, &srv.EnabledProfiles,
			&id, &enabled, &expiry, &quota, &quotaEnabled, &blocked); err != nil {
			return nil, "", false, err
		}
		target := dto.UserPlanTarget{ServerID: fmt.Sprintf("node-%d", srv.ID), Name: srv.Name,
			Role: srv.NormalizedRole(), Profiles: srv.NormalizedEnabledProfiles(), Issues: []string{}}
		if id.Valid {
			target.Before = &dto.UserPolicy{Enabled: enabled.Int64 == 1, ExpiresAt: expiry.String,
				QuotaEnabled: quotaEnabled.Int64 == 1, QuotaBlocked: blocked.Int64 == 1}
			if quota.Valid {
				target.Before.QuotaBytes = &quota.Int64
			}
			if expiry.Valid {
				if _, err := time.Parse(time.RFC3339, expiry.String); err != nil {
					target.Issues = append(target.Issues, "INVALID_EXPIRY")
				}
			}
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	if err := rows.Close(); err != nil {
		return nil, "", false, err
	}
	var conflict bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT u.username FROM users u JOIN servers s ON s.id=u.server_id
		WHERE s.enabled=1 GROUP BY u.username HAVING COUNT(DISTINCT u.uuid)>1 OR COUNT(DISTINCT u.email)>1)`).Scan(&conflict)
	if err != nil {
		return nil, "", false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", false, err
	}
	return targets, hex.EncodeToString(hash.Sum(nil)), conflict, nil
}

func hashPlanRows(ctx context.Context, tx *sql.Tx, query string, dst io.Writer) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(dst)
	if err := encoder.Encode(columns); err != nil {
		return err
	}
	values, pointers := make([]any, len(columns)), make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		if err := encoder.Encode(values); err != nil {
			return err
		}
	}
	return rows.Err()
}
