package operations

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"ovpn/internal/observation"
	"ovpn/internal/util"
)

type record struct {
	Version  int             `json:"version"`
	Plan     Plan            `json:"plan"`
	Request  Request         `json:"request"`
	Revision string          `json:"revision"`
	Snapshot json.RawMessage `json:"snapshot"`
	Job      *Job            `json:"job,omitempty"`
	Key      string          `json:"key,omitempty"`
}

func directory(workspace string) string { return filepath.Join(workspace, "operations") }

func privateRoot(workspace string) (*os.Root, error) {
	path := directory(workspace)
	if err := observation.PrivateDir(path); err != nil {
		return nil, err
	}
	return os.OpenRoot(path)
}

// WithWriter coordinates the CLI and the desktop without changing observer ownership.
func WithWriter(workspace string, fn func() error) error {
	unlock, err := observation.Lock(filepath.Join(directory(workspace), "writer"))
	if err != nil {
		return ErrBusy
	}
	defer unlock()
	return fn()
}

func key(root *os.Root) ([]byte, error) {
	value, err := root.ReadFile("journal.key")
	if err == nil && len(value) == 32 {
		return value, nil
	}
	if err == nil {
		return nil, ErrUnavailable
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	value = make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, value); err != nil {
		return nil, err
	}
	temp := "key-" + token() + ".tmp"
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	defer root.Remove(temp)
	if _, err = f.Write(value); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := root.Link(temp, "journal.key"); errors.Is(err, os.ErrExist) {
		return key(root)
	} else if err != nil {
		return nil, err
	}
	return value, nil
}

func envelope(root *os.Root) (cipher.AEAD, error) {
	k, err := key(root)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

const planLifetime = 15 * time.Minute
const terminalRetention = 200
const activityLimit = 100

type journal struct {
	db  *sql.DB
	box cipher.AEAD
}

func openJournal(workspace string) (_ *journal, err error) {
	root, err := privateRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	box, err := envelope(root)
	if err != nil {
		return nil, err
	}
	const name = "journal.db"
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		err = file.Close()
	}
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrUnavailable
	}
	path, err := filepath.Abs(filepath.Join(directory(workspace), name))
	if err != nil {
		return nil, err
	}
	u := util.FileURL(path)
	params := u.Query()
	params.Set("_pragma", "busy_timeout(5000)")
	params.Set("_txlock", "immediate")
	u.RawQuery = params.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()
	j := &journal{db: db, box: box}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return nil, err
	}
	if version == 0 {
		err = j.migrate(root)
	} else if version != 1 {
		err = ErrUnavailable
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// Migrate imports the former encrypted records once, under a SQLite write
// transaction. A second process observes either all imported records or none.
func (j *journal) migrate(root *os.Root) error {
	tx, err := j.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version == 1 {
		return err
	}
	if _, err = tx.Exec(`
CREATE TABLE records (
    id TEXT PRIMARY KEY,
    created INTEGER NOT NULL,
    started INTEGER NOT NULL,
    retired_at INTEGER NOT NULL,
    request_key TEXT UNIQUE,
    payload BLOB NOT NULL
);
CREATE INDEX activity ON records(started DESC, id DESC) WHERE started != 0;
CREATE INDEX unused_plans ON records(created) WHERE started = 0;
CREATE INDEX terminal_jobs ON records(retired_at DESC, id DESC) WHERE retired_at != 0;
`); err != nil {
		return err
	}
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	names, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return err
	}
	var migrated []string
	for _, entry := range names {
		id, ok := strings.CutSuffix(entry.Name(), ".json.enc")
		if !ok || !validID(id) {
			continue
		}
		raw, err := root.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		r, err := j.decode(id, raw)
		if err != nil {
			return err
		}
		if err = put(tx, r, raw); err != nil {
			return err
		}
		migrated = append(migrated, entry.Name())
	}
	if _, err = tx.Exec("PRAGMA user_version = 1"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, name := range migrated {
		_ = root.Remove(name)
	}
	return nil
}

func put(writer interface {
	Exec(string, ...any) (sql.Result, error)
}, r record, raw []byte) error {
	var started int64
	var retiredAt int64
	var requestKey any
	if r.Job != nil {
		started = r.Job.StartedAt.UnixNano()
		// Keep interrupted and partially completed operations available for recovery.
		if r.Job.FinishedAt != nil && (r.Job.State == "succeeded" || r.Job.Code == "STALE_PLAN") {
			retiredAt = r.Job.FinishedAt.UnixNano()
		}
		requestKey = r.Key
	}
	_, err := writer.Exec(`
INSERT INTO records (id, created, started, retired_at, request_key, payload) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET started=excluded.started, retired_at=excluded.retired_at,
    request_key=excluded.request_key, payload=excluded.payload
`, r.Plan.ID, r.Plan.CreatedAt.UnixNano(), started, retiredAt, requestKey, raw)
	return err
}

func (j *journal) save(value record) error {
	value.Version = 1
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	nonce := make([]byte, j.box.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	raw = j.box.Seal(nonce, nonce, raw, []byte(value.Plan.ID))
	return put(j.db, value, raw)
}

func (j *journal) decode(id string, raw []byte) (record, error) {
	if len(raw) < j.box.NonceSize() {
		return record{}, ErrUnavailable
	}
	plain, err := j.box.Open(nil, raw[:j.box.NonceSize()], raw[j.box.NonceSize():], []byte(id))
	if err != nil {
		return record{}, ErrUnavailable
	}
	var r record
	if json.Unmarshal(plain, &r) != nil || r.Version != 1 || r.Plan.ID != id {
		return record{}, ErrUnavailable
	}
	r.Plan.Request, r.Plan.Revision, r.Plan.Snapshot = r.Request, r.Revision, r.Snapshot
	return r, nil
}

func (j *journal) read(id string) (record, error) {
	if !validID(id) {
		return record{}, ErrInvalid
	}
	var raw []byte
	if err := j.db.QueryRow("SELECT payload FROM records WHERE id = ?", id).Scan(&raw); err != nil {
		return record{}, ErrUnavailable
	}
	return j.decode(id, raw)
}

func (j *journal) activity() ([]Job, error) {
	rows, err := j.db.Query("SELECT id, payload FROM records WHERE started != 0 ORDER BY started DESC, id DESC LIMIT ?", activityLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		record, err := j.decode(id, raw)
		if err != nil || record.Job == nil {
			return nil, ErrUnavailable
		}
		jobs = append(jobs, *record.Job)
	}
	return jobs, rows.Err()
}

func (j *journal) prune(now time.Time) ([]string, error) {
	rows, err := j.db.Query(`
DELETE FROM records
WHERE (started = 0 AND created < ?) OR id IN (
    SELECT id FROM records WHERE retired_at != 0 ORDER BY retired_at DESC, id DESC LIMIT -1 OFFSET ?
)
RETURNING id
`, now.Add(-planLifetime).UnixNano(), terminalRetention)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var removed []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		removed = append(removed, id)
	}
	return removed, rows.Err()
}
