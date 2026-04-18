package local

import (
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ovpn/internal/util"
)

const (
	encryptedFieldPrefix = "enc:v1:"
	secretKeyEnv         = "OVPN_SECRET_KEY"
)

var (
	secretKeyOnce      sync.Once
	cachedSecretKey    []byte
	cachedSecretKeyErr error
	noKeyWarnOnce      sync.Once
)

// encryptSensitiveField returns encrypt sensitive field.
func encryptSensitiveField(plain string) (string, error) {
	// This helper is intentionally field-level so we can incrementally protect
	// sensitive columns without changing the full DB format.
	if strings.TrimSpace(plain) == "" {
		return plain, nil
	}
	key, configured, err := loadSecretKey()
	if err != nil {
		return "", err
	}
	if !configured {
		warnMissingSecretKeyOnce()
		return plain, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := cryptorand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plain), nil)
	payload := append(nonce, ciphertext...)
	return encryptedFieldPrefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

// decryptSensitiveField returns decrypt sensitive field.
func decryptSensitiveField(value string) (string, error) {
	// Non-prefixed values are legacy/plaintext records and remain readable.
	if !strings.HasPrefix(value, encryptedFieldPrefix) {
		return value, nil
	}
	key, configured, err := loadSecretKey()
	if err != nil {
		return "", err
	}
	if !configured {
		return "", errors.New("sensitive field is encrypted but no key is configured (set OVPN_SECRET_KEY or ~/.ovpn/secret.key)")
	}
	raw := strings.TrimPrefix(value, encryptedFieldPrefix)
	payload, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("decode encrypted field: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", errors.New("encrypted payload is too short")
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt sensitive field: %w", err)
	}
	return string(plain), nil
}

// loadSecretKey returns load secret key.
func loadSecretKey() ([]byte, bool, error) {
	secretKeyOnce.Do(func() {
		keyRaw, ok, err := readRawSecretKey()
		if err != nil {
			cachedSecretKeyErr = err
			return
		}
		if !ok {
			return
		}
		key, err := parseSecretKey(keyRaw)
		if err != nil {
			cachedSecretKeyErr = err
			return
		}
		cachedSecretKey = key
	})
	if cachedSecretKeyErr != nil {
		return nil, false, cachedSecretKeyErr
	}
	if len(cachedSecretKey) == 0 {
		return nil, false, nil
	}
	return cachedSecretKey, true, nil
}

// readRawSecretKey returns raw secret key for callers.
func readRawSecretKey() (string, bool, error) {
	if v := strings.TrimSpace(os.Getenv(secretKeyEnv)); v != "" {
		return v, true, nil
	}
	secretPath := filepath.Join(util.DefaultDataDir(), "secret.key")
	b, err := os.ReadFile(secretPath) // #nosec G304 -- fixed operator local path only.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s: %w", secretPath, err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", false, nil
	}
	return v, true, nil
}

// parseSecretKey parses secret key and returns normalized values.
func parseSecretKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty secret key")
	}
	if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, errors.New("invalid secret key format: expected 32-byte raw string, 64-char hex, or base64-encoded 32-byte value")
}

// warnMissingSecretKeyOnce returns warn missing secret key once.
func warnMissingSecretKeyOnce() {
	noKeyWarnOnce.Do(func() {
		log.Printf("warning: sensitive local DB fields are stored in plaintext; set %s or ~/.ovpn/secret.key to enable AES-GCM field protection", secretKeyEnv)
	})
}

// resetSecretKeyCacheForTests returns reset secret key cache for tests.
func resetSecretKeyCacheForTests() {
	secretKeyOnce = sync.Once{}
	cachedSecretKey = nil
	cachedSecretKeyErr = nil
	noKeyWarnOnce = sync.Once{}
}
