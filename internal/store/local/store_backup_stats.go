package local

import (
	"context"
	"database/sql"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/util"
)

// AddDeployRevision writes deploy revision changes to the local database.
func (s *Store) AddDeployRevision(ctx context.Context, rev *model.DeployRevision) error {
	now := util.NowUTC().Format(time.RFC3339)
	if rev.AppliedAt.IsZero() {
		rev.AppliedAt = util.NowUTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO deploy_revisions (server_id, revision, config_hash, applied_by, applied_at, status, description)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, rev.ServerID, rev.Revision, rev.ConfigHash, rev.AppliedBy, rev.AppliedAt.Format(time.RFC3339), rev.Status, rev.Description)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE servers SET updated_at=? WHERE id=?`, now, rev.ServerID)
	return err
}

// AddBackupRecord writes backup record changes to the local database.
func (s *Store) AddBackupRecord(ctx context.Context, rec *model.BackupRecord) error {
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = util.NowUTC()
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO backup_records (server_id, type, path, sha256, created_at, created_by, remote_path)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, nullableInt64v(rec.ServerID), rec.Type, rec.Path, rec.SHA256, rec.CreatedAt.Format(time.RFC3339), rec.CreatedBy, rec.RemotePath)
	if err != nil {
		return err
	}
	rec.ID, _ = res.LastInsertId()
	return nil
}

// ListBackupRecords reads backup records from the local database.
func (s *Store) ListBackupRecords(ctx context.Context, serverID int64) ([]model.BackupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, server_id, type, path, sha256, created_at, created_by, remote_path
		FROM backup_records
		WHERE server_id=? OR server_id IS NULL
		ORDER BY id DESC
	`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BackupRecord
	for rows.Next() {
		var r model.BackupRecord
		var sid sql.NullInt64
		var created string
		if err := rows.Scan(&r.ID, &sid, &r.Type, &r.Path, &r.SHA256, &created, &r.CreatedBy, &r.RemotePath); err != nil {
			return nil, err
		}
		if sid.Valid {
			r.ServerID = sid.Int64
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertStatsCache writes stats cache changes to the local database.
func (s *Store) UpsertStatsCache(ctx context.Context, t model.UserTraffic) error {
	now := util.NowUTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO stats_cache (server_id, email, window_type, window_start, uplink_bytes, downlink_bytes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id, email, window_type, window_start)
		DO UPDATE SET
			uplink_bytes=excluded.uplink_bytes,
			downlink_bytes=excluded.downlink_bytes,
			updated_at=excluded.updated_at
	`, t.ServerID, t.Email, t.WindowType, t.WindowStart.Format(time.RFC3339), t.UplinkBytes, t.DownlinkBytes, now)
	return err
}

// ListStatsCache reads stats cache from the local database.
func (s *Store) ListStatsCache(ctx context.Context, serverID int64, windowType string) ([]model.UserTraffic, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT server_id, email, window_start, window_type, uplink_bytes, downlink_bytes
		FROM stats_cache
		WHERE server_id=? AND window_type=?
		ORDER BY email, window_start
	`, serverID, windowType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UserTraffic
	for rows.Next() {
		var t model.UserTraffic
		var ws string
		if err := rows.Scan(&t.ServerID, &t.Email, &ws, &t.WindowType, &t.UplinkBytes, &t.DownlinkBytes); err != nil {
			return nil, err
		}
		t.WindowStart, _ = time.Parse(time.RFC3339, ws)
		out = append(out, t)
	}
	return out, rows.Err()
}
