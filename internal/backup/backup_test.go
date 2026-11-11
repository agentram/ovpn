package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/ssh"
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

	for _, want := range []string{"ovpn-agent-data", "sudo cp -a", "sudo rm -rf \"$TMP/stack/logs\"", "sudo tar -czf", "find /opt/ovpn-backups -maxdepth 1 -type f -name 'main-*.tgz'", "NR>7"} {
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

func TestRemoteBackupRestoreUseRunnerAndWrapErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := ssh.Config{Host: "vpn.example.com"}
	server := model.Server{Name: "main"}
	runner := &ssh.Runner{DryRun: true}
	remotePath, err := RemoteBackup(ctx, runner, cfg, server)
	if err != nil {
		t.Fatalf("remote backup: %v", err)
	}
	if !strings.HasPrefix(remotePath, "/opt/ovpn-backups/main-") {
		t.Fatalf("unexpected remote backup path=%q", remotePath)
	}
	if err := RemoteRestore(ctx, runner, cfg, remotePath); err != nil {
		t.Fatalf("remote restore: %v", err)
	}
}

func TestLocalBackupCapturesCommittedWALAndNeverFollowsSymlinks(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(data, "ovpn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL; CREATE TABLE users (name TEXT); INSERT INTO users VALUES ('alice')"); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err = os.WriteFile(outside, []byte("private-external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(data, "external")); err != nil {
		t.Fatal(err)
	}
	archive, _, err := LocalBackup(data, filepath.Join(data, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(archive)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("archive is not private")
	}
	restored := t.TempDir()
	if err = LocalRestore(restored, archive); err != nil {
		t.Fatal(err)
	}
	copy, err := sql.Open("sqlite", filepath.Join(restored, "ovpn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var user string
	if err = copy.QueryRow("SELECT name FROM users").Scan(&user); err != nil || user != "alice" {
		t.Fatal("backup lost committed WAL data", err)
	}
	if _, err = os.Stat(filepath.Join(restored, "external")); !os.IsNotExist(err) {
		t.Fatal("external symlink was included")
	}
}
func TestRemoteRestoreRejectsShellInput(t *testing.T) {
	if err := RemoteRestore(context.Background(), &ssh.Runner{DryRun: true}, ssh.Config{}, "/opt/ovpn-backups/x.tgz;touch injected"); err == nil {
		t.Fatal("accepted shell input")
	}
}

func TestBackupNamesAndRestoreOrderingProtectExistingRuntime(t *testing.T) {
	for _, name := range []string{"Friendly server", "server; echo unsafe", "../../backup", "用户服务器"} {
		path := "/opt/ovpn-backups/" + archiveName(name, 1) + "-sample.tgz"
		if !safeArchivePath(path) {
			t.Fatal("server label escaped its archive name")
		}
	}
	script := buildRemoteRestoreScript("/opt/ovpn-backups/main.tgz")
	verified := strings.Index(script, "sha256sum -c")
	stopped := strings.Index(script, "docker-compose.yml down")
	replaced := strings.Index(script, "sudo rm -rf /opt/ovpn")
	started := strings.Index(script, "up -d --force-recreate")
	if verified < 0 || stopped <= verified || replaced <= stopped || started <= replaced {
		t.Fatal("restore did not verify, stop, replace and restart in that order")
	}
	if !strings.Contains(script, "*.db-wal") {
		t.Fatal("stale WAL files can override the restored snapshot")
	}
}

func TestRemoteBackupAndRestoreScriptsParseBeforeExecution(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("requires a POSIX shell")
	}
	for _, script := range []string{buildRemoteBackupScript("/opt/ovpn-backups/example.tgz", "example"), buildRemoteRestoreScript("/opt/ovpn-backups/example.tgz")} {
		command := exec.Command("sh", "-n")
		command.Stdin = strings.NewReader(script)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("invalid backup script: %s", out)
		}
	}
}
