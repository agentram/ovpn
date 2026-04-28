package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestVersionCommandPrintsPinnedVersion(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	cmd := (&App{}).versionCmd()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command: %v", err)
	}
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "1.3.0" {
		t.Fatalf("version output = %q, want %q", got, "1.3.0")
	}
}
