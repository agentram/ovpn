package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ovpn/internal/deploy"
	"ovpn/internal/model"
	"ovpn/internal/ssh"
	"ovpn/internal/util"
)

const (
	defaultLocalBackupRetention  = 7
	defaultRemoteBackupRetention = 7
)

type Manifest struct {
	Version    int       `json:"version"`
	ServerName string    `json:"server_name"`
	Type       string    `json:"type"`
	Archive    string    `json:"archive"`
	SHA256     string    `json:"sha256,omitempty"`
	RemotePath string    `json:"remote_path,omitempty"`
	CreatedBy  string    `json:"created_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// BuildManifest builds manifest from the current inputs and defaults.
func BuildManifest(serverName, backupType, archivePath, sha256, remotePath, createdBy string, now time.Time) Manifest {
	return Manifest{
		Version:    1,
		ServerName: serverName,
		Type:       backupType,
		Archive:    archivePath,
		SHA256:     sha256,
		RemotePath: remotePath,
		CreatedBy:  createdBy,
		CreatedAt:  now.UTC(),
	}
}

// JSON returns json.
func (m Manifest) JSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// LocalBackup prepares local backup files and filesystem state.
func LocalBackup(dataDir, outDir string) (string, string, error) {
	if outDir == "" {
		outDir = filepath.Join(dataDir, "backups")
	}
	dataDir = filepath.Clean(dataDir)
	outDir = filepath.Clean(outDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
	}
	ts := time.Now().UTC().Format("20060102T150405")
	archive := filepath.Join(outDir, fmt.Sprintf("ovpn-local-%s.tgz", ts))
	args := []string{"-czf", archive, "-C", dataDir}
	// When backup output lives under dataDir (default ~/.ovpn/backups), exclude that subtree
	// to avoid recursive/ever-growing archives and tar self-inclusion warnings.
	if rel, err := filepath.Rel(dataDir, outDir); err == nil {
		rel = filepath.ToSlash(rel)
		if rel != "." && rel != "" && rel != ".." && !strings.HasPrefix(rel, "../") {
			args = append(args, "--exclude=./"+strings.TrimSuffix(rel, "/"))
		}
	}
	args = append(args, ".")
	cmd := exec.Command("tar", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("create local backup: %w: %s", err, strings.TrimSpace(string(out)))
	}
	h, err := util.SHA256File(archive)
	if err != nil {
		return "", "", err
	}
	_ = pruneMatchingBackups(outDir, "ovpn-local-*.tgz", defaultLocalBackupRetention)
	return archive, h, nil
}

// LocalRestore prepares local restore files and filesystem state.
func LocalRestore(dataDir, archive string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("tar", "-xzf", archive, "-C", dataDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restore local backup: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoteBackup executes remote backup against remote hosts over SSH.
func RemoteBackup(ctx context.Context, runner *ssh.Runner, cfg ssh.Config, server model.Server) (remotePath string, err error) {
	ts := time.Now().UTC().Format("20060102T150405")
	remotePath = fmt.Sprintf("%s/%s-%s.tgz", deploy.RemoteBackupDir, server.Name, ts)
	cmd := buildRemoteBackupScript(remotePath, server.Name)
	_, err = runner.Exec(ctx, cfg, cmd)
	if err != nil {
		return "", fmt.Errorf("remote backup on %s for server %s: %w", cfg.Host, server.Name, err)
	}
	return remotePath, err
}

// buildRemoteBackupScript builds remote backup script from the current inputs and defaults.
func buildRemoteBackupScript(remotePath string, serverName string) string {
	return fmt.Sprintf(`set -e
sudo mkdir -p %[1]s
VOLUME=$(sudo docker volume ls --format '{{.Name}}' | grep 'ovpn-agent-data' | head -n1 || true)
TMP=$(mktemp -d)
sudo cp -a %[2]s "$TMP/stack"
if [ -n "$VOLUME" ]; then
  MP=$(sudo docker volume inspect "$VOLUME" --format '{{.Mountpoint}}')
  mkdir -p "$TMP/stats"
  sudo cp -a "$MP/." "$TMP/stats/" || true
fi
sudo tar -czf %[3]s -C "$TMP" .
sudo rm -rf "$TMP"
old_archives=$(find %[1]s -maxdepth 1 -type f -name '%[4]s-*.tgz' -printf '%%T@ %%p\n' | sort -nr | awk 'NR>%[5]d {print $2}')
if [ -n "$old_archives" ]; then
  printf '%%s\n' "$old_archives" | xargs -r sudo rm -f
fi
`, deploy.RemoteBackupDir, deploy.RemoteDir, remotePath, serverName, defaultRemoteBackupRetention)
}

// RemoteRestore executes remote restore against remote hosts over SSH.
func RemoteRestore(ctx context.Context, runner *ssh.Runner, cfg ssh.Config, archiveRemotePath string) error {
	cmd := buildRemoteRestoreScript(archiveRemotePath)
	_, err := runner.Exec(ctx, cfg, cmd)
	if err != nil {
		return fmt.Errorf("remote restore on %s from %s: %w", cfg.Host, archiveRemotePath, err)
	}
	return nil
}

// buildRemoteRestoreScript builds remote restore script from the current inputs and defaults.
func buildRemoteRestoreScript(archiveRemotePath string) string {
	return fmt.Sprintf(`set -e
TMP=$(mktemp -d)
sudo tar -xzf %[1]s -C "$TMP"
sudo rm -rf %[2]s
sudo mkdir -p %[2]s
sudo cp -a "$TMP/stack/." %[2]s/
VOLUME=$(sudo docker volume ls --format '{{.Name}}' | grep 'ovpn-agent-data' | head -n1 || true)
if [ -n "$VOLUME" ] && [ -d "$TMP/stats" ]; then
  MP=$(sudo docker volume inspect "$VOLUME" --format '{{.Mountpoint}}')
  sudo cp -a "$TMP/stats/." "$MP/" || true
fi
rm -rf "$TMP"
`, archiveRemotePath, deploy.RemoteDir)
}

// pruneMatchingBackups keeps only the newest keep archives matching pattern in dir.
func pruneMatchingBackups(dir, pattern string, keep int) error {
	if keep < 1 {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return err
	}
	if len(matches) <= keep {
		return nil
	}
	sort.Strings(matches)
	for _, p := range matches[:len(matches)-keep] {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
