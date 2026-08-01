package cli

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteValidationTLSSelfSNICertificate(t *testing.T) {
	dir, err := writeValidationTLSSelfSNICertificate()
	if err != nil {
		t.Fatalf("write validation certificate: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	certificate := filepath.Join(dir, "fullchain.pem")
	key := filepath.Join(dir, "privkey.pem")
	if _, err := tls.LoadX509KeyPair(certificate, key); err != nil {
		t.Fatalf("load generated validation certificate: %v", err)
	}
	info, err := os.Stat(key)
	if err != nil {
		t.Fatalf("stat generated validation key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected private key mode 0600, got %o", info.Mode().Perm())
	}
}
