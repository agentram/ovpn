package local

import (
	"context"
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"

	"ovpn/internal/util"
)

const sqliteDriver = "sqlite"

type Store struct {
	db *sql.DB
}

// Open initializes open with the required dependencies.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := util.EnsureDir(dataDir); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, "ovpn.db")
	db, err := sql.Open(sqliteDriver, dbPath)
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
