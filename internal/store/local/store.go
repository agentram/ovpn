package local

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite"

	"ovpn/internal/util"
)

const sqliteDriver = "sqlite"
const sqliteBusyTimeoutMS = 5000

type Store struct {
	db *sql.DB
}

// Open opens (creating and migrating as needed) the local SQLite state database under dataDir.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := util.EnsureDir(dataDir); err != nil {
		return nil, err
	}
	dbPath, err := filepath.Abs(filepath.Join(dataDir, "ovpn.db"))
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(sqliteDriver, sqliteDSN(dbPath))
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

func sqliteDSN(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Add("_pragma", "busy_timeout("+strconv.Itoa(sqliteBusyTimeoutMS)+")")
	u.RawQuery = q.Encode()
	return u.String()
}
