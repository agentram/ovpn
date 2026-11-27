package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionDoesNotOpenWorkspace(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "not-created")
	var output bytes.Buffer
	root := newRootCmd(&App{ctx: context.Background(), writer: &output})
	root.SetArgs([]string{"--data-dir", directory, "version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("missing version")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("version initialized a workspace")
	}
}
