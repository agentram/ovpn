package remote

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"ovpn/internal/model"
	"ovpn/internal/util"
)

const sqliteDriver = "sqlite"

type Counter struct {
	Name      string
	Value     int64
	UpdatedAt time.Time
}

type Store struct {
	db *sql.DB
}

// Open initializes open with the required dependencies.
func Open(ctx context.Context, baseDir string) (*Store, error) {
	if err := util.EnsureDir(baseDir); err != nil {
		return nil, err
	}
	p := filepath.Join(baseDir, "stats.db")
	db, err := sql.Open(sqliteDriver, p)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close returns close.
func (s *Store) Close() error { return s.db.Close() }

// migrate writes migrate to the local database.
func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA foreign_keys=ON;`,
		`CREATE TABLE IF NOT EXISTS counter_state (
			name TEXT PRIMARY KEY,
			value INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS user_traffic_hourly (
			email TEXT NOT NULL,
			window_start TEXT NOT NULL,
			uplink_bytes INTEGER NOT NULL,
			downlink_bytes INTEGER NOT NULL,
			PRIMARY KEY (email, window_start)
		);`,
		`CREATE TABLE IF NOT EXISTS user_traffic_daily (
			email TEXT NOT NULL,
			window_start TEXT NOT NULL,
			uplink_bytes INTEGER NOT NULL,
			downlink_bytes INTEGER NOT NULL,
			PRIMARY KEY (email, window_start)
		);`,
		`CREATE TABLE IF NOT EXISTS user_traffic_total (
			email TEXT PRIMARY KEY,
			uplink_bytes INTEGER NOT NULL,
			downlink_bytes INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS collector_meta (
			k TEXT PRIMARY KEY,
			v TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS quota_policy (
			email TEXT PRIMARY KEY,
			uuid TEXT NOT NULL,
			inbound_tag TEXT NOT NULL,
			quota_enabled INTEGER NOT NULL DEFAULT 1,
			monthly_quota_byte INTEGER,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS quota_state (
				email TEXT PRIMARY KEY,
				blocked INTEGER NOT NULL DEFAULT 0,
				blocked_month TEXT NOT NULL DEFAULT '',
				blocked_at TEXT,
				updated_at TEXT NOT NULL
			);`,
		`CREATE TABLE IF NOT EXISTS user_policy (
				email TEXT PRIMARY KEY,
				username TEXT NOT NULL,
				uuid TEXT NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1,
				expiry_at TEXT,
				inbound_tag TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// UpsertCounter writes counter changes to the local database.
func (s *Store) UpsertCounter(ctx context.Context, name string, value int64) error {
	now := util.NowUTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO counter_state (name, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(name)
		DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at
	`, name, value, now)
	return err
}

// GetCounter reads counter from the local database.
func (s *Store) GetCounter(ctx context.Context, name string) (Counter, bool, error) {
	var c Counter
	var ts string
	err := s.db.QueryRowContext(ctx, `SELECT name, value, updated_at FROM counter_state WHERE name=?`, name).Scan(&c.Name, &c.Value, &ts)
	if err == sql.ErrNoRows {
		return Counter{}, false, nil
	}
	if err != nil {
		return Counter{}, false, err
	}
	c.UpdatedAt, _ = time.Parse(time.RFC3339, ts)
	return c, true, nil
}

// AddDelta writes delta changes to the local database.
func (s *Store) AddDelta(ctx context.Context, email string, upDelta, downDelta int64, ts time.Time) error {
	hour := ts.UTC().Truncate(time.Hour)
	day := time.Date(ts.UTC().Year(), ts.UTC().Month(), ts.UTC().Day(), 0, 0, 0, 0, time.UTC)
	now := util.NowUTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_traffic_hourly (email, window_start, uplink_bytes, downlink_bytes)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(email, window_start)
		DO UPDATE SET
			uplink_bytes=user_traffic_hourly.uplink_bytes + excluded.uplink_bytes,
			downlink_bytes=user_traffic_hourly.downlink_bytes + excluded.downlink_bytes
	`, email, hour.Format(time.RFC3339), upDelta, downDelta)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_traffic_daily (email, window_start, uplink_bytes, downlink_bytes)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(email, window_start)
		DO UPDATE SET
			uplink_bytes=user_traffic_daily.uplink_bytes + excluded.uplink_bytes,
			downlink_bytes=user_traffic_daily.downlink_bytes + excluded.downlink_bytes
	`, email, day.Format(time.RFC3339), upDelta, downDelta)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_traffic_total (email, uplink_bytes, downlink_bytes, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(email)
		DO UPDATE SET
			uplink_bytes=user_traffic_total.uplink_bytes + excluded.uplink_bytes,
			downlink_bytes=user_traffic_total.downlink_bytes + excluded.downlink_bytes,
			updated_at=excluded.updated_at
	`, email, upDelta, downDelta, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SetMeta writes meta changes to the local database.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	now := util.NowUTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO collector_meta (k, v, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(k)
		DO UPDATE SET v=excluded.v, updated_at=excluded.updated_at
	`, key, value, now)
	return err
}

// GetMeta reads meta from the local database.
func (s *Store) GetMeta(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT v FROM collector_meta WHERE k=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// ListTotals reads totals from the local database.
func (s *Store) ListTotals(ctx context.Context) ([]model.UserTraffic, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT email, uplink_bytes, downlink_bytes FROM user_traffic_total ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UserTraffic
	for rows.Next() {
		var t model.UserTraffic
		t.WindowType = "total"
		t.WindowStart = time.Time{}
		if err := rows.Scan(&t.Email, &t.UplinkBytes, &t.DownlinkBytes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListDaily reads daily from the local database.
func (s *Store) ListDaily(ctx context.Context, day time.Time) ([]model.UserTraffic, error) {
	dayStart := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
		SELECT email, window_start, uplink_bytes, downlink_bytes
		FROM user_traffic_daily
		WHERE window_start=?
		ORDER BY email
	`, dayStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UserTraffic
	for rows.Next() {
		var t model.UserTraffic
		var ws string
		if err := rows.Scan(&t.Email, &ws, &t.UplinkBytes, &t.DownlinkBytes); err != nil {
			return nil, err
		}
		t.WindowType = "daily"
		t.WindowStart, _ = time.Parse(time.RFC3339, ws)
		out = append(out, t)
	}
	return out, rows.Err()
}
