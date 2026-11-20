package subscription

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"ovpn/internal/util"
)

var ErrAccountConflict = errors.New("verified email is already linked or has changed")

type accountIdentity struct {
	subject string
	email   string
}

func canonicalEmail(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	address, err := mail.ParseAddress(value)
	if err != nil || len(value) > 254 || address.Address != value || address.Name != "" ||
		strings.Count(value, "@") != 1 || strings.ContainsAny(value, "\"<>") {
		return "", errors.New("a valid email address is required")
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return "", errors.New("use an ASCII email address")
		}
	}
	return strings.ToLower(value), nil
}

func (s *Invites) emailTag(email string) (string, error) {
	canonical, err := canonicalEmail(email)
	if err != nil {
		return "", err
	}
	const name = "account-email.key"
	key, err := s.read(name)
	if errors.Is(err, ErrNotFound) {
		if _, dbErr := s.root.Lstat("accounts.db"); !errors.Is(dbErr, os.ErrNotExist) {
			// Another process may have initialized the key and registry.
			key, err = s.read(name)
			if err != nil {
				return "", errors.New("restore the account email key alongside the account registry")
			}
		} else {
			err = s.create(name, randomBytes(32))
			if err == nil || errors.Is(err, os.ErrExist) {
				key, err = s.read(name)
			}
		}
	}
	if err != nil || len(key) != 32 {
		return "", errors.New("account email key is unavailable")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("ovpn-account-email:v1\x00" + canonical))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *Invites) accounts() (*sql.DB, error) {
	s.accountOnce.Do(func() {
		s.accountDB, s.accountErr = s.openAccounts()
	})
	return s.accountDB, s.accountErr
}

func (s *Invites) openAccounts() (*sql.DB, error) {
	const filename = "accounts.db"
	file, err := s.root.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		err = file.Close()
	}
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := s.root.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("account registry is unavailable")
	}
	path, err := filepath.Abs(filepath.Join(s.root.Name(), filename))
	if err != nil {
		return nil, err
	}
	u := util.FileURL(path)
	q := u.Query()
	q.Set("_pragma", "busy_timeout(5000)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS accounts (
		subject TEXT PRIMARY KEY, email_tag TEXT NOT NULL UNIQUE, invitation TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// lookupAccount verifies both indexes. Neither the provider's subject nor
// email address is stored in clear text.
func lookupAccount(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, subject, tag string) (string, error) {
	var storedSubject, storedTag, invite string
	err := q.QueryRowContext(ctx,
		"SELECT subject, email_tag, invitation FROM accounts WHERE subject = ? OR email_tag = ?", subject, tag,
	).Scan(&storedSubject, &storedTag, &invite)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if storedSubject != subject || storedTag != tag {
		return "", ErrAccountConflict
	}
	return invite, nil
}

func putAccount(ctx context.Context, tx *sql.Tx, subject, tag, invite string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO accounts(subject, email_tag, invitation) VALUES (?, ?, ?)
		ON CONFLICT(subject) DO UPDATE SET invitation = excluded.invitation`, subject, tag, invite)
	return err
}

// accountInvitation also upgrades old subject-only assignments on their next
// verified sign-in. Uninvited accounts do not create rows.
func (s *Invites) accountInvitation(ctx context.Context, subject, tag string) (string, error) {
	db, err := s.accounts()
	if err != nil {
		return "", err
	}
	link, err := lookupAccount(ctx, db, subject, tag)
	if err != nil || link != "" {
		return link, err
	}
	legacy, err := s.read(subject + ".account")
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil || !validDigest(string(legacy)) {
		return "", errors.New("account assignment is unavailable")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	link, err = lookupAccount(ctx, tx, subject, tag)
	if err != nil || link != "" {
		return link, err
	}
	if err := putAccount(ctx, tx, subject, tag, string(legacy)); err != nil {
		return "", err
	}
	return string(legacy), tx.Commit()
}
