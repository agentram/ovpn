package subscription

import (
	"crypto/subtle"
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
	"time"
)

// Invites stores activation records separately from the published bundles.
type Invites struct{ root *os.Root }

type invitation struct {
	Bundle     string    `json:"bundle"`
	Generation string    `json:"generation"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func OpenInvites(dir string) (*Invites, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open invitation directory: %w", err)
	}
	return &Invites{root: root}, nil
}

func (s *Invites) Close() error { return s.root.Close() }

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
	if err != nil || json.Unmarshal(raw, &record) != nil || !validDigest(record.Generation) {
		return Bundle{}, invitation{}, ErrNotFound
	}
	bundle, err := bundles.Read(record.Bundle)
	if err != nil || subtle.ConstantTimeCompare([]byte(bundle.TokenHash), []byte(record.Generation)) != 1 {
		return Bundle{}, invitation{}, ErrNotFound
	}
	return bundle, record, nil
}

func (s *Invites) redeem(bundles *Store, code, session string, now time.Time) error {
	if !validToken(code) || !validToken(session) || code == session {
		return ErrNotFound
	}
	id := tokenHash(code)
	bundle, record, err := s.bundle(id, bundles)
	if err != nil {
		return err
	}
	if _, err := Encode(bundle.Entries, now, "base64"); err != nil {
		return err
	}
	receipt := []byte(tokenHash(session))
	name := id + ".claim"
	if !now.Before(record.ExpiresAt) {
		return s.verifyClaim(name, receipt)
	}
	// Install a complete receipt without overwriting a concurrent claim.
	if err := s.create(name, receipt); errors.Is(err, os.ErrExist) {
		return s.verifyClaim(name, receipt)
	} else {
		return err
	}
}

func (s *Invites) deviceBundle(bundles *Store, id, session string) (Bundle, error) {
	if !validDigest(id) || !validToken(session) || s.verifyClaim(id+".claim", []byte(tokenHash(session))) != nil {
		return Bundle{}, ErrNotFound
	}
	bundle, _, err := s.bundle(id, bundles)
	return bundle, err
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
	return s.root.Link(tmp, name)
}
