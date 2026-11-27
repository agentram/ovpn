package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacePathRoundTrip(t *testing.T) {
	ctx := context.Background()
	directory := filepath.Join(t.TempDir(), "Desk state # % 数据")
	writer, err := Open(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.db.ExecContext(ctx, "CREATE TABLE fixture_path(value INTEGER); INSERT INTO fixture_path VALUES (17)"); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(directory, "ovpn.db")); err != nil {
		t.Fatal("database written outside requested workspace", err)
	}
	reader, err := OpenReadOnly(ctx, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var value int
	if err = reader.db.QueryRowContext(ctx, "SELECT value FROM fixture_path").Scan(&value); err != nil || value != 17 {
		t.Fatal("read-only workspace failed to round-trip", err)
	}
	if _, err = reader.db.ExecContext(ctx, "INSERT INTO fixture_path VALUES(18)"); err == nil {
		t.Fatal("read-only workspace permitted mutation")
	}
}
