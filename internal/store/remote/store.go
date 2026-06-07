package remote

import (
	"context"
	"database/sql"
	"fmt"
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

// Open opens (creating and migrating as needed) the agent's SQLite database under baseDir.
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

// Close closes the underlying database.
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
			email TEXT NOT NULL,
			uuid TEXT NOT NULL,
			inbound_tag TEXT NOT NULL,
			quota_enabled INTEGER NOT NULL DEFAULT 1,
			monthly_quota_byte INTEGER,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (email, inbound_tag)
		);`,
		`CREATE TABLE IF NOT EXISTS quota_state (
				email TEXT PRIMARY KEY,
				blocked INTEGER NOT NULL DEFAULT 0,
				blocked_month TEXT NOT NULL DEFAULT '',
				blocked_at TEXT,
				updated_at TEXT NOT NULL
			);`,
		`CREATE TABLE IF NOT EXISTS user_policy (
				email TEXT NOT NULL,
				username TEXT NOT NULL,
				uuid TEXT NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1,
				expiry_at TEXT,
				inbound_tag TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				PRIMARY KEY (email, inbound_tag)
			);`,
		`CREATE TABLE IF NOT EXISTS connection_hourly (
				email TEXT NOT NULL,
				window_start TEXT NOT NULL,
				accepted_count INTEGER NOT NULL DEFAULT 0,
				rejected_count INTEGER NOT NULL DEFAULT 0,
				dest_ipv4_count INTEGER NOT NULL DEFAULT 0,
				dest_ipv6_count INTEGER NOT NULL DEFAULT 0,
				dest_domain_count INTEGER NOT NULL DEFAULT 0,
				dest_unknown_count INTEGER NOT NULL DEFAULT 0,
				source_overflow_count INTEGER NOT NULL DEFAULT 0,
				last_seen_at TEXT,
				updated_at TEXT NOT NULL,
				PRIMARY KEY (email, window_start)
			);`,
		`CREATE TABLE IF NOT EXISTS connection_source_hourly (
				email TEXT NOT NULL,
				window_start TEXT NOT NULL,
				source_bucket TEXT NOT NULL,
				count INTEGER NOT NULL DEFAULT 0,
				updated_at TEXT NOT NULL,
				PRIMARY KEY (email, window_start, source_bucket)
			);`,
		`CREATE TABLE IF NOT EXISTS connection_port_hourly (
				email TEXT NOT NULL,
				window_start TEXT NOT NULL,
				port INTEGER NOT NULL,
				count INTEGER NOT NULL DEFAULT 0,
				updated_at TEXT NOT NULL,
				PRIMARY KEY (email, window_start, port)
			);`,
		`CREATE TABLE IF NOT EXISTS connection_debug_sessions (
				email TEXT PRIMARY KEY,
				started_at TEXT NOT NULL,
				expires_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);`,
		`CREATE TABLE IF NOT EXISTS connection_debug_events (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				email TEXT NOT NULL,
				ts TEXT NOT NULL,
				result TEXT NOT NULL,
				source_network TEXT NOT NULL DEFAULT '',
				destination TEXT NOT NULL DEFAULT '',
				destination_port INTEGER NOT NULL DEFAULT 0,
				destination_family TEXT NOT NULL DEFAULT 'unknown',
				created_at TEXT NOT NULL
			);`,
		`CREATE INDEX IF NOT EXISTS idx_connection_hourly_window ON connection_hourly(window_start);`,
		`CREATE INDEX IF NOT EXISTS idx_connection_source_hourly_window ON connection_source_hourly(window_start);`,
		`CREATE INDEX IF NOT EXISTS idx_connection_port_hourly_window ON connection_port_hourly(window_start);`,
		`CREATE INDEX IF NOT EXISTS idx_connection_debug_events_email_ts ON connection_debug_events(email, ts);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureCompositePolicyPK(ctx, "quota_policy", `CREATE TABLE quota_policy (
			email TEXT NOT NULL,
			uuid TEXT NOT NULL,
			inbound_tag TEXT NOT NULL,
			quota_enabled INTEGER NOT NULL DEFAULT 1,
			monthly_quota_byte INTEGER,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (email, inbound_tag)
		);`, "email, uuid, inbound_tag, quota_enabled, monthly_quota_byte, updated_at"); err != nil {
		return err
	}
	if err := s.ensureCompositePolicyPK(ctx, "user_policy", `CREATE TABLE user_policy (
			email TEXT NOT NULL,
			username TEXT NOT NULL,
			uuid TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			expiry_at TEXT,
			inbound_tag TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (email, inbound_tag)
		);`, "email, username, uuid, enabled, expiry_at, inbound_tag, updated_at"); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureCompositePolicyPK(ctx context.Context, table string, createSQL string, columns string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var pkCols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if pk > 0 {
			pkCols = append(pkCols, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(pkCols) == 2 && pkCols[0] == "email" && pkCols[1] == "inbound_tag" {
		return nil
	}
	if len(pkCols) == 0 {
		return fmt.Errorf("%s has no primary key after migration", table)
	}
	legacy := table + "_legacy_single_inbound"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+legacy); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` RENAME TO `+legacy); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO `+table+` (`+columns+`) SELECT `+columns+` FROM `+legacy+` WHERE TRIM(COALESCE(email, '')) != '' AND TRIM(COALESCE(inbound_tag, '')) != ''`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE `+legacy); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertCounter stores the latest cumulative value for a named Xray counter.
func (s *Store) UpsertCounter(ctx context.Context, name string, value int64) error {
	now := util.NowUTC().Format(time.RFC3339)
	return upsertCounterTx(ctx, s.db, name, value, now)
}

// execer is satisfied by both *sql.DB and *sql.Tx so the write helpers can run inside or
// outside a transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func upsertCounterTx(ctx context.Context, db execer, name string, value int64, now string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO counter_state (name, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(name)
		DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at
	`, name, value, now)
	return err
}

// GetCounter returns the last stored value for a named counter, if present.
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

// AddDelta adds a traffic delta to a user's hourly, daily, and total aggregates in one transaction.
func (s *Store) AddDelta(ctx context.Context, email string, upDelta, downDelta int64, ts time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := addDeltaTx(ctx, tx, email, upDelta, downDelta, ts); err != nil {
		return err
	}
	return tx.Commit()
}

// AddDeltaAndAdvanceCounter persists a usage delta and advances the source counter in a single
// transaction. Doing both atomically prevents double-counting: if the counter were advanced in a
// separate write that failed (or the process crashed in between), the next collection would
// recompute the same delta against the stale counter value and add it twice.
func (s *Store) AddDeltaAndAdvanceCounter(ctx context.Context, email string, upDelta, downDelta int64, ts time.Time, counterName string, counterValue int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if upDelta > 0 || downDelta > 0 {
		if err := addDeltaTx(ctx, tx, email, upDelta, downDelta, ts); err != nil {
			return err
		}
	}
	if err := upsertCounterTx(ctx, tx, counterName, counterValue, util.NowUTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

func addDeltaTx(ctx context.Context, tx execer, email string, upDelta, downDelta int64, ts time.Time) error {
	hour := ts.UTC().Truncate(time.Hour)
	day := time.Date(ts.UTC().Year(), ts.UTC().Month(), ts.UTC().Day(), 0, 0, 0, 0, time.UTC)
	now := util.NowUTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_traffic_hourly (email, window_start, uplink_bytes, downlink_bytes)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(email, window_start)
		DO UPDATE SET
			uplink_bytes=user_traffic_hourly.uplink_bytes + excluded.uplink_bytes,
			downlink_bytes=user_traffic_hourly.downlink_bytes + excluded.downlink_bytes
	`, email, hour.Format(time.RFC3339), upDelta, downDelta); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_traffic_daily (email, window_start, uplink_bytes, downlink_bytes)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(email, window_start)
		DO UPDATE SET
			uplink_bytes=user_traffic_daily.uplink_bytes + excluded.uplink_bytes,
			downlink_bytes=user_traffic_daily.downlink_bytes + excluded.downlink_bytes
	`, email, day.Format(time.RFC3339), upDelta, downDelta); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO user_traffic_total (email, uplink_bytes, downlink_bytes, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(email)
		DO UPDATE SET
			uplink_bytes=user_traffic_total.uplink_bytes + excluded.uplink_bytes,
			downlink_bytes=user_traffic_total.downlink_bytes + excluded.downlink_bytes,
			updated_at=excluded.updated_at
	`, email, upDelta, downDelta, now)
	return err
}

// SetMeta stores a collector metadata key/value pair.
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

// GetMeta returns a collector metadata value, if present.
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

// ListTotals returns cumulative per-user traffic totals.
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

// ListDaily returns per-user traffic for the given day.
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
