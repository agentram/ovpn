package subscription

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"ovpn/internal/clientcfg"
)

const (
	maxBundleBytes = 1 << 20
	MaxEntries     = 256
)

var ErrNotFound = errors.New("subscription not found")

type Entry struct {
	Link      string     `json:"link"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	NodeID    int64      `json:"node_id,omitempty"`
}

// Bundle contains only the credentials needed to deliver one user's subscription.
// The delivery token itself is never persisted.
type Bundle struct {
	Version    int        `json:"version"`
	ID         string     `json:"id"`
	Owner      string     `json:"owner"`
	Title      string     `json:"title"`
	TokenHash  string     `json:"token_hash"`
	Entries    []Entry    `json:"entries"`
	ManagedKey string     `json:"managed_key,omitempty"`
	Disabled   bool       `json:"disabled,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

type Store struct{ root *os.Root }

func Open(dir string) (*Store, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open subscription bundle directory: %w", err)
	}
	return &Store{root: root}, nil
}

func (s *Store) Close() error { return s.root.Close() }

func validID(id string) bool {
	raw, err := hex.DecodeString(id)
	return err == nil && len(raw) == 16 && hex.EncodeToString(raw) == id
}

func validText(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 128 && strings.IndexFunc(s, unicode.IsControl) == -1
}

func validateEntries(entries []Entry) error {
	if len(entries) > MaxEntries {
		return fmt.Errorf("a subscription supports at most %d profiles", MaxEntries)
	}
	for _, entry := range entries {
		if len(entry.Link) > 8192 || strings.ContainsAny(entry.Link, "\r\n") {
			return fmt.Errorf("invalid subscription client link")
		}
		if _, err := clientcfg.FromLink(entry.Link); err != nil {
			return fmt.Errorf("invalid subscription client link")
		}
	}
	return nil
}

func (b Bundle) validate() error {
	hash, err := hex.DecodeString(b.TokenHash)
	if b.Version != 1 || !validID(b.ID) || !validText(b.Owner) || !validText(b.Title) || err != nil || len(hash) != sha256.Size {
		return fmt.Errorf("invalid subscription bundle")
	}
	return validateEntries(b.Entries)
}

func (s *Store) Read(id string) (Bundle, error) {
	if !validID(id) {
		return Bundle{}, ErrNotFound
	}
	name := id + ".json"
	info, err := s.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxBundleBytes {
		return Bundle{}, ErrNotFound
	}
	f, err := s.root.Open(name)
	if err != nil {
		return Bundle{}, ErrNotFound
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxBundleBytes+1))
	if err != nil || len(raw) > maxBundleBytes {
		return Bundle{}, ErrNotFound
	}
	var bundle Bundle
	if json.Unmarshal(raw, &bundle) != nil || bundle.ID != id || bundle.validate() != nil {
		return Bundle{}, ErrNotFound
	}
	return bundle, nil
}

func randomHex() string { return hex.EncodeToString(randomBytes(16)) }

func randomBytes(n int) []byte {
	raw := make([]byte, n)
	// crypto/rand.Read terminates the process if the system random source fails.
	_, _ = rand.Read(raw)
	return raw
}

func tokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (s *Store) lock(id string) (func(), error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid subscription id")
	}
	name := id + ".lock"
	if err := s.root.Mkdir(name, 0o700); err != nil {
		return nil, fmt.Errorf("subscription is locked; finish the other operation before retrying (lock: %s)", name)
	}
	return func() { _ = s.root.Remove(name) }, nil
}

// Publish creates a subscription when id is empty, or replaces a published snapshot.
// Existing URLs keep working unless rotate is true. Updates and revocation share a lock.
func (s *Store) Publish(id, owner, title string, entries []Entry, rotate bool) (string, string, error) {
	return s.publish(id, owner, title, entries, rotate, "", false)
}

// PublishManaged retains the delivery token on refresh and binds the bundle to
// the original VPN identity, even if the name is reused later.
func (s *Store) PublishManaged(id, owner, key string, entries []Entry, rotate, disabled bool) (string, string, error) {
	if len(key) != sha256.Size*2 {
		return "", "", fmt.Errorf("invalid managed subscription identity")
	}
	return s.publish(id, owner, "", entries, rotate, key, disabled)
}

func (s *Store) Managed() ([]Bundle, error) {
	dir, err := s.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	var bundles []Bundle
	for _, name := range names {
		id, ok := strings.CutSuffix(name, ".json")
		if !ok || !validID(id) {
			continue
		}
		b, err := s.Read(id)
		if err == nil && b.ManagedKey != "" {
			bundles = append(bundles, b)
		}
	}
	return bundles, nil
}

func (s *Store) publish(id, owner, title string, entries []Entry, rotate bool, key string, disabled bool) (string, string, error) {
	if !validText(owner) || title != "" && !validText(title) {
		return "", "", fmt.Errorf("invalid subscription owner or title")
	}
	if err := validateEntries(entries); err != nil {
		return "", "", err
	}
	create := id == ""
	if create {
		if len(entries) == 0 {
			return "", "", fmt.Errorf("no active user profiles to publish")
		}
		id = randomHex()
	}
	unlock, err := s.lock(id)
	if err != nil {
		return "", "", err
	}
	defer unlock()
	bundle := Bundle{Version: 1, ID: id, Owner: owner, Title: "ovpn"}
	if create {
		if _, err := s.root.Lstat(id + ".json"); !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("subscription already exists")
		}
	} else {
		bundle, err = s.Read(id)
		if err != nil {
			return "", "", err
		}
		if bundle.Owner != owner || bundle.ManagedKey != key {
			return "", "", fmt.Errorf("subscription belongs to a different user")
		}
		if bundle.Disabled && !disabled {
			return "", "", fmt.Errorf("subscription has been disabled")
		}
	}
	if title != "" {
		bundle.Title = title
	}
	bundle.Entries, bundle.ManagedKey, bundle.Disabled = entries, key, disabled
	if key != "" {
		now := time.Now().UTC()
		bundle.UpdatedAt = &now
	}
	token := ""
	if create || rotate {
		token = base64.RawURLEncoding.EncodeToString(randomBytes(32))
		bundle.TokenHash = tokenHash(token)
	}
	if err := s.write(bundle); err != nil {
		return "", "", err
	}
	return id, token, nil
}

// RestoreManagedRotation keeps previously issued access usable when a new
// link could not be verified. A newer publisher's changes are never reverted.
func (s *Store) RestoreManagedRotation(previous Bundle, discardedToken string) error {
	unlock, err := s.lock(previous.ID)
	if err != nil {
		return err
	}
	defer unlock()
	current, err := s.Read(previous.ID)
	if err != nil {
		return err
	}
	if previous.ManagedKey == "" || current.ManagedKey != previous.ManagedKey ||
		current.Disabled || current.TokenHash != tokenHash(discardedToken) {
		return fmt.Errorf("subscription changed while checking its new link")
	}
	// Preserve entries published with the current generation.
	current.TokenHash = previous.TokenHash
	return s.write(current)
}

func (s *Store) write(bundle Bundle) error {
	raw, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil || len(raw) > maxBundleBytes {
		return fmt.Errorf("subscription bundle exceeds the size limit")
	}
	tmp := ".subscription-" + randomHex()
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer s.root.Remove(tmp)
	if _, err := f.Write(raw); err != nil {
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
	return s.root.Rename(tmp, bundle.ID+".json")
}

func (s *Store) Revoke(id string) error {
	bundle, err := s.Read(id)
	if err != nil {
		return err
	}
	if bundle.ManagedKey != "" {
		_, _, err := s.PublishManaged(id, bundle.Owner, bundle.ManagedKey, nil, false, true)
		return err
	}
	unlock, err := s.lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.root.Remove(id + ".json"); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else {
		return err
	}
}

// ProfileRevision identifies the exact list delivered to the client.
func ProfileRevision(encoded string) string { return tokenHash(encoded)[:16] }

func Encode(entries []Entry, now time.Time, format string) ([]byte, error) {
	if format != "base64" && format != "links" {
		return nil, fmt.Errorf("subscription format must be base64 or links")
	}
	if err := validateEntries(entries); err != nil {
		return nil, err
	}
	var links []string
	for _, entry := range entries {
		if entry.ExpiresAt == nil || now.Before(*entry.ExpiresAt) {
			links = append(links, entry.Link)
		}
	}
	if len(links) == 0 {
		return nil, ErrNotFound
	}
	data := strings.Join(links, "\n") + "\n"
	if format == "base64" {
		data = base64.StdEncoding.EncodeToString([]byte(data))
	}
	return []byte(data), nil
}

// ValidateBaseURL accepts an HTTPS origin, or literal loopback HTTP for local use.
func ValidateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return fmt.Errorf("--base-url must be an HTTPS origin without credentials, a path, query or fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if u.Scheme == "http" && err == nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("--base-url requires HTTPS outside literal loopback addresses")
}

func URL(origin, id, token string) (string, error) {
	if err := ValidateBaseURL(origin); err != nil {
		return "", err
	}
	return strings.TrimRight(origin, "/") + "/sub/" + id + "/" + token, nil
}
