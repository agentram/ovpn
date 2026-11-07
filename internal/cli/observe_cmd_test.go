package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ovpn/internal/app/dto"
)

func TestObservationStatusDoesNotInitializeWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	root := NewRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"--data-dir", dir, "observe", "status"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var snapshot dto.ObserverSnapshot
	if err := json.Unmarshal(output.Bytes(), &snapshot); err != nil || snapshot.Running || snapshot.Owner != "stopped" || len(snapshot.Latest) != 0 {
		t.Fatal("unexpected status", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("status initialized local state")
	}
}
