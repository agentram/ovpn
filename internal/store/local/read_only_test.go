package local

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"ovpn/internal/model"
)

func TestReadOnlyDoesNotCreateOrMigrate(t *testing.T) {
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := OpenReadOnly(ctx, missing); !os.IsNotExist(err) {
		t.Fatalf("missing workspace: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read created workspace")
	}
	dir := t.TempDir()
	writer, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := mustAddStoreTestServer(t, ctx, writer, storeCRUDServer("edge-a", model.ServerRoleVPN))
	if _, err := writer.db.Exec(`UPDATE servers SET reality_private_key='enc:v1:unreadable',vless_server_decryption='enc:v1:unreadable'`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "ovpn.db"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReadOnly(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	items, err := reader.ListServerInventory(ctx, 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("projection read failed: %v", err)
	}
	if items[0].Host != "" || items[0].SSHUser != "" || items[0].RealityPrivateKey != "" {
		t.Fatal("inventory selected private fields")
	}
	client, err := reader.GetClientServer(ctx, srv.ID)
	if err != nil || client.RealityPublicKey == "" || client.RealityPrivateKey != "" || client.VLESSServerDecryption != "" || client.SSHIdentityFile != "" {
		t.Fatalf("client projection: %v", err)
	}
	if _, err := reader.db.Exec(`UPDATE servers SET name='changed'`); err == nil {
		t.Fatal("read-only connection accepted a write")
	}
	if _, err := reader.db.Exec(`CREATE TABLE unexpected (id INTEGER)`); err == nil {
		t.Fatal("read-only connection migrated schema")
	}
	after, err := os.ReadFile(filepath.Join(dir, "ovpn.db"))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("database content changed")
	}
}
