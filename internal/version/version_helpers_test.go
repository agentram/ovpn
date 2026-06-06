package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentAndReadVersionFileBranches(t *testing.T) {
	old := pinnedVersion
	pinnedVersion = "9.8.7"
	if got := Current(); got != "9.8.7" {
		t.Fatalf("pinned current got %q", got)
	}
	pinnedVersion = old
	t.Cleanup(func() { pinnedVersion = old })

	path := filepath.Join(t.TempDir(), "VERSION")
	if err := os.WriteFile(path, []byte("1.2.3\n"), 0o600); err != nil {
		t.Fatalf("write version: %v", err)
	}
	if got, err := ReadVersionFile(path); err != nil || got != "1.2.3" {
		t.Fatalf("read version got %q err=%v", got, err)
	}
	if _, err := ReadVersionFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatalf("expected missing version file error")
	}
	if root, err := repoRoot(); err != nil || root == "" {
		t.Fatalf("repo root got %q err=%v", root, err)
	}
	if _, err := TopChangelogVersion("# none\n"); err == nil {
		t.Fatalf("expected changelog missing version error")
	}
}
