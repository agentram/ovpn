package observation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"ovpn/internal/app/dto"

	_ "modernc.org/sqlite"
)

const HistoryLimit = 5000

type Store struct{ db *sql.DB }

func Directory(dataDir string) string { return filepath.Join(dataDir, "observations") }

func PrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid observation directory")
	}
	return os.Chmod(path, 0o700)
}

func Open(dataDir string, write bool) (*Store, error) {
	dir := Directory(dataDir)
	if write {
		if err := PrivateDir(dir); err != nil {
			return nil, err
		}
	}
	path, err := filepath.Abs(filepath.Join(dir, "history.db"))
	if err != nil {
		return nil, err
	}
	if !write {
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(3000)")
	if !write {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if write {
		for _, stmt := range []string{
			`PRAGMA journal_mode=WAL`,
			`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY,value TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS policies (id TEXT PRIMARY KEY,payload TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS tools (id TEXT PRIMARY KEY,payload TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS jobs (id TEXT PRIMARY KEY,payload TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS samples (id INTEGER PRIMARY KEY AUTOINCREMENT, sample_key TEXT NOT NULL, measured_at TEXT NOT NULL,payload TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS sample_key_id ON samples(sample_key,id)`,
			`CREATE TABLE IF NOT EXISTS incidents (id TEXT PRIMARY KEY,payload TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS budgets (id TEXT NOT NULL,day TEXT NOT NULL,attempts INTEGER NOT NULL,PRIMARY KEY(id,day))`,
		} {
			if _, err = db.Exec(stmt); err != nil {
				_ = db.Close()
				return nil, err
			}
		}
		if err = os.Chmod(path, 0o600); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Setting(ctx context.Context, key string) string {
	var value string
	_ = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	return value
}

func (s *Store) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) Save(ctx context.Context, table, id string, value any) error {
	if table != "policies" && table != "tools" && table != "jobs" && table != "incidents" {
		return errors.New("invalid observation collection")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO `+table+`(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, id, string(raw))
	if err == nil && (table == "jobs" || table == "incidents") {
		_, err = s.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE rowid NOT IN (SELECT rowid FROM `+table+` ORDER BY rowid DESC LIMIT 100)`)
	}
	return err
}

func (s *Store) List(ctx context.Context, table string) ([]json.RawMessage, error) {
	if table != "policies" && table != "tools" && table != "jobs" && table != "incidents" {
		return nil, errors.New("invalid observation collection")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM `+table+` ORDER BY rowid DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]json.RawMessage, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		result = append(result, json.RawMessage(raw))
	}
	return result, rows.Err()
}

func (s *Store) RemovePolicy(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM policies WHERE id=?`, id)
	return err
}

// Reserve charges attempts before traffic, including interrupted attempts after a crash.
func (s *Store) Reserve(ctx context.Context, key string, now time.Time, limit int) (bool, error) {
	var attempts int
	err := s.db.QueryRowContext(ctx, `INSERT INTO budgets(id,day,attempts) VALUES(?,?,1)
		ON CONFLICT(id,day) DO UPDATE SET attempts=attempts+1 WHERE attempts<? RETURNING attempts`, key, now.UTC().Format("2006-01-02"), limit).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) Add(ctx context.Context, key string, sample dto.Observation) error {
	raw, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO samples(sample_key,measured_at,payload) VALUES(?,?,?)`, key, sample.MeasuredAt, string(raw)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM samples WHERE id NOT IN (SELECT id FROM samples ORDER BY id DESC LIMIT 5000) OR measured_at<?`, time.Now().UTC().Add(-7*24*time.Hour).Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM jobs WHERE rowid NOT IN (SELECT rowid FROM jobs ORDER BY rowid DESC LIMIT 100)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM incidents WHERE rowid NOT IN (SELECT rowid FROM incidents ORDER BY rowid DESC LIMIT 100)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM budgets WHERE day<?`, time.Now().UTC().Add(-8*24*time.Hour).Format("2006-01-02")); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Samples(ctx context.Context, latest bool) ([]dto.Observation, error) {
	query := `SELECT id,payload FROM samples WHERE measured_at>=? ORDER BY id DESC LIMIT 50`
	if latest {
		query = `SELECT id,payload FROM samples WHERE id IN (SELECT MAX(id) FROM samples WHERE measured_at>=? GROUP BY sample_key) ORDER BY id DESC LIMIT 100`
	}
	rows, err := s.db.QueryContext(ctx, query, time.Now().UTC().Add(-7*24*time.Hour).Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]dto.Observation, 0)
	for rows.Next() {
		var id int64
		var raw string
		var sample dto.Observation
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &sample); err != nil {
			return nil, err
		}
		sample.ID = id
		items = append(items, sample)
	}
	return items, rows.Err()
}

func (s *Store) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM samples WHERE measured_at>=?`, time.Now().UTC().Add(-7*24*time.Hour).Format(time.RFC3339)).Scan(&count)
	return count, err
}
