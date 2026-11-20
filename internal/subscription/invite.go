package subscription

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Invites stores activation records separately from the published bundles.
type Invites struct {
	root        *os.Root
	accountOnce sync.Once
	accountDB   *sql.DB
	accountErr  error
}

type invitation struct {
	Bundle     string    `json:"bundle"`
	Generation string    `json:"generation"`
	ExpiresAt  time.Time `json:"expires_at"`
	EmailTag   string    `json:"email_tag,omitempty"`
}

func OpenInvites(dir string) (*Invites, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open invitation directory: %w", err)
	}
	return &Invites{root: root}, nil
}

func (s *Invites) Close() error {
	if s.accountDB != nil {
		return errors.Join(s.accountDB.Close(), s.root.Close())
	}
	return s.root.Close()
}

func newToken() string { return base64.RawURLEncoding.EncodeToString(randomBytes(32)) }

func validToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == token
}

func validDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

// Issue creates a one-time activation code for the current bundle generation.
func (s *Invites) Issue(bundle Bundle, origin string, now time.Time, ttl time.Duration) (string, error) {
	return s.IssueForEmail(bundle, origin, now, ttl, "")
}

// IssueForEmail restricts a setup code to a verified recipient at activation.
func (s *Invites) IssueForEmail(bundle Bundle, origin string, now time.Time, ttl time.Duration, email string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || ValidateBaseURL(origin) != nil || u.Scheme != "https" {
		return "", errors.New("an HTTPS origin is required for invitations")
	}
	if port := u.Port(); port != "" {
		if value, err := strconv.ParseUint(port, 10, 16); err != nil || value == 0 {
			return "", errors.New("invalid invitation origin port")
		}
	}
	prefix := "OT1:" + strings.TrimRight(origin, "/") + "#"
	if len(prefix)+base64.RawURLEncoding.EncodedLen(32) > 512 {
		return "", errors.New("invitation origin is too long")
	}
	if ttl < time.Minute || ttl > 24*time.Hour {
		return "", errors.New("invitation lifetime must be between one minute and 24 hours")
	}
	if err := bundle.validate(); err != nil {
		return "", err
	}
	if _, err := Encode(bundle.Entries, now, "base64"); err != nil {
		return "", err
	}
	code := newToken()
	record := invitation{Bundle: bundle.ID, Generation: bundle.TokenHash, ExpiresAt: now.Add(ttl).UTC()}
	if email != "" {
		record.EmailTag, err = s.emailTag(email)
		if err != nil {
			return "", err
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	if err := s.create(tokenHash(code)+".invite", raw); err != nil {
		return "", fmt.Errorf("write invitation: %w", err)
	}
	return prefix + code, nil
}

func (s *Invites) bundle(id string, bundles *Store) (Bundle, invitation, error) {
	if !validDigest(id) {
		return Bundle{}, invitation{}, ErrNotFound
	}
	raw, err := s.read(id + ".invite")
	var record invitation
	if err != nil || json.Unmarshal(raw, &record) != nil || !validDigest(record.Generation) ||
		(record.EmailTag != "" && !validDigest(record.EmailTag)) {
		return Bundle{}, invitation{}, ErrNotFound
	}
	bundle, err := bundles.Read(record.Bundle)
	if err != nil || subtle.ConstantTimeCompare([]byte(bundle.TokenHash), []byte(record.Generation)) != 1 {
		return Bundle{}, invitation{}, ErrNotFound
	}
	return bundle, record, nil
}

func (s *Invites) redeem(ctx context.Context, bundles *Store, code, session, subject, tag string, requireEmail bool, now time.Time) error {
	if !validToken(code) || !validToken(session) || code == session {
		return ErrNotFound
	}
	id := tokenHash(code)
	bundle, record, err := s.bundle(id, bundles)
	if err != nil || (requireEmail && record.EmailTag == "") ||
		(record.EmailTag != "" && subtle.ConstantTimeCompare([]byte(record.EmailTag), []byte(tag)) != 1) {
		return ErrNotFound
	}
	if _, err := Encode(bundle.Entries, now, "base64"); err != nil {
		return err
	}
	claim := func() error {
		receipt := activationReceipt(session, subject)
		name := id + ".claim"
		if !now.Before(record.ExpiresAt) {
			return s.verifyClaim(name, receipt)
		}
		if err := s.create(name, receipt); errors.Is(err, os.ErrExist) {
			return s.verifyClaim(name, receipt)
		} else {
			return err
		}
	}
	if subject == "" {
		return claim()
	}
	db, err := s.accounts()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	linked, err := lookupAccount(ctx, tx, subject, tag)
	if err != nil {
		return err
	}
	if linked != "" && linked != id {
		if current, _, err := s.bundle(linked, bundles); err == nil {
			if _, err := Encode(current.Entries, now, "base64"); err == nil {
				return ErrNotFound
			}
		}
	}
	if err := claim(); err != nil {
		return err
	}
	// If the database transaction fails after the claim is written, the same
	// subject and receipt can retry; no new assignment becomes visible.
	if err := putAccount(ctx, tx, subject, tag, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Invites) deviceBundle(bundles *Store, id, session, subject string) (Bundle, error) {
	if !validDigest(id) || !validToken(session) || s.verifyClaim(id+".claim", activationReceipt(session, subject)) != nil {
		return Bundle{}, ErrNotFound
	}
	bundle, _, err := s.bundle(id, bundles)
	return bundle, err
}

func activationReceipt(session, subject string) []byte {
	if subject == "" {
		return []byte(tokenHash(session))
	}
	return []byte(tokenHash("account:v1:" + subject + ":" + session))
}

func (s *Invites) verifyClaim(name string, receipt []byte) error {
	raw, err := s.read(name)
	if err != nil || subtle.ConstantTimeCompare(raw, receipt) != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Invites) read(name string) ([]byte, error) {
	info, err := s.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 512 {
		return nil, ErrNotFound
	}
	f, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 513))
}

func (s *Invites) create(name string, value []byte) error {
	return s.write(name, value, false)
}

func (s *Invites) write(name string, value []byte, replace bool) error {
	tmp := ".invite-" + randomHex()
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer s.root.Remove(tmp)
	if _, err := f.Write(value); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if replace {
		return s.root.Rename(tmp, name)
	}
	return s.root.Link(tmp, name)
}
