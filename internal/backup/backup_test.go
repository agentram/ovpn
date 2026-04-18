package backup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalBackupRestore(t *testing.T) {
	tmp, err := os.MkdirTemp("", "ovpn-backup-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	data := filepath.Join(tmp, "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive, sha, err := LocalBackup(data, filepath.Join(tmp, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if archive == "" || sha == "" {
		t.Fatalf("empty backup outputs")
	}
	if err := os.WriteFile(filepath.Join(data, "file.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LocalRestore(data, archive); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(data, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("unexpected restore content: %s", string(b))
	}
}

func TestBuildManifest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	m := BuildManifest("main", "server", "/tmp/a.tgz", "deadbeef", "/opt/ovpn-backups/a.tgz", "alice", now)
	if m.Version != 1 || m.ServerName != "main" || m.Type != "server" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	raw, err := m.JSON()
	if err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("manifest json invalid: %v", err)
	}
}

func TestLocalBackupExcludesNestedBackupDir(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	data := filepath.Join(tmp, "data")
	if err := os.MkdirAll(filepath.Join(data, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "backups", "old.tgz"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	archive, _, err := LocalBackup(data, filepath.Join(data, "backups"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("tar", "-tzf", archive).CombinedOutput()
	if err != nil {
		t.Fatalf("list archive contents: %v: %s", err, string(out))
	}
	listing := string(out)
	if strings.Contains(listing, "backups/") {
		t.Fatalf("backup archive should exclude nested backups dir, got listing: %s", listing)
	}
	if !strings.Contains(listing, "file.txt") {
		t.Fatalf("backup archive should contain user data file, got listing: %s", listing)
	}
}

func TestBuildRemoteScripts(t *testing.T) {
	t.Parallel()

	backup := buildRemoteBackupScript("/opt/ovpn-backups/main-20260405T120000.tgz", "main")
	restore := buildRemoteRestoreScript("/opt/ovpn-backups/main-20260405T120000.tgz")

	for _, want := range []string{"ovpn-agent-data", "sudo cp -a", "sudo tar -czf", "find /opt/ovpn-backups -maxdepth 1 -type f -name 'main-*.tgz'", "NR>7"} {
		if !strings.Contains(backup, want) {
			t.Fatalf("backup script missing %q: %q", want, backup)
		}
	}
	for _, want := range []string{"sudo tar -xzf", "sudo rm -rf /opt/ovpn", "ovpn-agent-data"} {
		if !strings.Contains(restore, want) {
			t.Fatalf("restore script missing %q: %q", want, restore)
		}
	}
}

func TestPruneMatchingBackupsKeepsNewestN(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	names := []string{
		"ovpn-local-20260101T000001.tgz",
		"ovpn-local-20260102T000001.tgz",
		"ovpn-local-20260103T000001.tgz",
		"ovpn-local-20260104T000001.tgz",
		"ovpn-local-20260105T000001.tgz",
		"ovpn-local-20260106T000001.tgz",
		"ovpn-local-20260107T000001.tgz",
		"ovpn-local-20260108T000001.tgz",
		"ovpn-local-20260109T000001.tgz",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed backup %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "note.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("seed non-backup file: %v", err)
	}

	if err := pruneMatchingBackups(tmp, "ovpn-local-*.tgz", 7); err != nil {
		t.Fatalf("prune backups: %v", err)
	}

	remaining, err := filepath.Glob(filepath.Join(tmp, "ovpn-local-*.tgz"))
	if err != nil {
		t.Fatalf("glob remaining backups: %v", err)
	}
	if len(remaining) != 7 {
		t.Fatalf("expected 7 backups after prune, got %d", len(remaining))
	}
	for _, removed := range []string{"ovpn-local-20260101T000001.tgz", "ovpn-local-20260102T000001.tgz"} {
		if _, err := os.Stat(filepath.Join(tmp, removed)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be pruned", removed)
		}
	}
	if _, err := os.Stat(filepath.Join(tmp, "note.txt")); err != nil {
		t.Fatalf("non-backup file should remain untouched: %v", err)
	}
}
