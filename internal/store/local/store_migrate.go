package local

import (
	"context"
	"strings"
)

// migrate writes migrate to the local database.
func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA foreign_keys=ON;`,
		`CREATE TABLE IF NOT EXISTS servers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			host TEXT NOT NULL,
			domain TEXT NOT NULL,
			ssh_user TEXT NOT NULL,
			ssh_port INTEGER NOT NULL DEFAULT 22,
			ssh_identity_file TEXT NOT NULL DEFAULT '',
			ssh_known_hosts_file TEXT NOT NULL DEFAULT '',
			ssh_strict_host_key INTEGER NOT NULL DEFAULT 1,
			xray_version TEXT NOT NULL,
			reality_private_key TEXT NOT NULL,
			reality_public_key TEXT NOT NULL,
			reality_short_ids TEXT NOT NULL,
			reality_server_name TEXT NOT NULL,
			reality_target TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_deploy_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			uuid TEXT NOT NULL,
			email TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			expiry_date TEXT,
			traffic_limit_byte INTEGER,
			quota_enabled INTEGER NOT NULL DEFAULT 1,
			quota_blocked INTEGER NOT NULL DEFAULT 0,
			quota_blocked_at TEXT,
			notes TEXT NOT NULL DEFAULT '',
			tags_csv TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(server_id, username),
			UNIQUE(server_id, email),
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS user_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			tag TEXT NOT NULL,
			UNIQUE(user_id, tag),
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS deploy_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			revision TEXT NOT NULL,
			config_hash TEXT NOT NULL,
			applied_by TEXT NOT NULL,
			applied_at TEXT NOT NULL,
			status TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS backup_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER,
			type TEXT NOT NULL,
			path TEXT NOT NULL,
			sha256 TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL,
			remote_path TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS stats_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			server_id INTEGER NOT NULL,
			email TEXT NOT NULL,
			window_type TEXT NOT NULL,
			window_start TEXT NOT NULL,
			uplink_bytes INTEGER NOT NULL,
			downlink_bytes INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(server_id, email, window_type, window_start),
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	optionalMigrations := []string{
		`ALTER TABLE users ADD COLUMN quota_enabled INTEGER NOT NULL DEFAULT 1;`,
		`ALTER TABLE users ADD COLUMN quota_blocked INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE users ADD COLUMN quota_blocked_at TEXT;`,
	}
	for _, stmt := range optionalMigrations {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return err
		}
	}
	return nil
}
