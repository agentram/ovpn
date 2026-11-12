package observation

import (
	"context"
	"path/filepath"
	"testing"
)

func TestObservationPathRoundTrip(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "Desk state # % 数据")
	writer, err := Open(directory, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Set(context.Background(), "fixture", "retained"); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(directory, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if reader.Setting(context.Background(), "fixture") != "retained" {
		t.Fatal("history did not round-trip")
	}
	if err = reader.Set(context.Background(), "fixture", "changed"); err == nil {
		t.Fatal("read-only history permitted mutation")
	}
}
