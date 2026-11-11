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

// BuildManifest assembles a backup Manifest describing an archive and its provenance.
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

// JSON marshals the manifest as indented JSON.
func (m Manifest) JSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// LocalBackup prepares local backup files and filesystem state.
func LocalBackup(dataDir, outDir string) (string, string, error) {
	if outDir == "" {
		outDir = filepath.Join(dataDir, "backups")
	}
	var err error
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return "", "", err
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return "", "", err
	}
	if dataDir == outDir {
		return "", "", fmt.Errorf("backup output must be separate from the workspace root")
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", "", err
	}
	if err := os.Chmod(outDir, 0o700); err != nil {
		return "", "", err
	}
	ts := time.Now().UTC().Format("20060102T150405.000000000")
	archive := filepath.Join(outDir, fmt.Sprintf("ovpn-local-%s.tgz", ts))
	if err := writeSnapshot(dataDir, outDir, archive); err != nil {
		_ = os.Remove(archive)
		return "", "", err
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

// RemoteBackup creates a timestamped archive of the remote runtime directory and returns its remote path.
func RemoteBackup(ctx context.Context, runner *ssh.Runner, cfg ssh.Config, server model.Server) (remotePath string, err error) {
	ts := time.Now().UTC().Format("20060102T150405.000000000")
	remotePath = fmt.Sprintf("%s/%s-%s.tgz", deploy.RemoteBackupDir, archiveName(server.Name, server.ID), ts)
	cmd := buildRemoteBackupScript(remotePath, archiveName(server.Name, server.ID))
	_, err = runner.Exec(ctx, cfg, cmd)
	if err != nil {
		return "", fmt.Errorf("remote backup on %s for server %s: %w", cfg.Host, server.Name, err)
	}
	return remotePath, err
}

// buildRemoteBackupScript renders the shell script that archives the remote runtime directory.
func buildRemoteBackupScript(remotePath string, serverName string) string {
	return fmt.Sprintf(`set -e
sudo mkdir -p %[1]s
VOLUME=$(sudo docker volume ls -q --filter label=com.docker.compose.project=ovpn --filter label=com.docker.compose.volume=ovpn-agent-data)
TMP=$(mktemp -d)
PAUSED=""
WATCHDOG=""
trap 'if [ -n "$PAUSED" ]; then sudo docker unpause "$PAUSED" >/dev/null 2>&1 || true; fi; if [ -n "$WATCHDOG" ]; then kill "$WATCHDOG" 2>/dev/null || true; fi; sudo rm -rf "$TMP"' EXIT HUP INT TERM
sudo cp -a %[2]s "$TMP/stack"
sudo rm -rf "$TMP/stack/logs"
if [ -n "$VOLUME" ]; then
  MP=$(sudo docker volume inspect "$VOLUME" --format '{{.Mountpoint}}')
  mkdir -p "$TMP/stats"
  if command -v python3 >/dev/null 2>&1; then
  sudo python3 - "$MP" "$TMP/stats" <<'PYTHON'
import pathlib, shutil, sqlite3, sys
source, destination = map(pathlib.Path, sys.argv[1:])
for path in source.iterdir():
    if path.is_symlink() or not path.is_file() or path.name.endswith(("-wal", "-shm")):
        continue
    target = destination / path.name
    if path.suffix == ".db":
        with sqlite3.connect(path.as_uri() + "?mode=ro", uri=True) as live:
            with sqlite3.connect(target) as snapshot:
                live.backup(snapshot)
    else:
        shutil.copy2(path, target)
PYTHON
  else
    AGENT=$(cd %[2]s && sudo docker compose --env-file .env -f docker-compose.yml ps -aq ovpn-agent)
    if [ -n "$AGENT" ] && [ "$(sudo docker inspect "$AGENT" --format '{{.State.Running}}')" = true ] && [ "$(sudo docker inspect "$AGENT" --format '{{.State.Paused}}')" = false ]; then
      PAUSED="$AGENT"
      sudo docker pause "$PAUSED" >/dev/null
      nohup sh -c 'sleep 15; sudo docker unpause "$1" >/dev/null 2>&1' sh "$PAUSED" >/dev/null 2>&1 &
      WATCHDOG=$!
    fi
    sudo timeout 10 cp -a "$MP/." "$TMP/stats/"
    if [ -n "$PAUSED" ]; then sudo docker unpause "$PAUSED" >/dev/null; PAUSED=""; fi
    if [ -n "$WATCHDOG" ]; then kill "$WATCHDOG" 2>/dev/null || true; WATCHDOG=""; fi
  fi
fi
sudo tar -czf %[3]s -C "$TMP" .
sudo chmod 600 %[3]s
sudo tar -tzf %[3]s >/dev/null
sudo sha256sum %[3]s | sudo tee %[3]s.sha256 >/dev/null
sudo rm -rf "$TMP"
old_archives=$(find %[1]s -maxdepth 1 -type f -name '%[4]s-*.tgz' -printf '%%T@ %%p\n' | sort -nr | awk 'NR>%[5]d {print $2}')
if [ -n "$old_archives" ]; then
  printf '%%s\n' "$old_archives" | xargs -r sudo rm -f
fi
`, deploy.RemoteBackupDir, deploy.RemoteDir, remotePath, serverName, defaultRemoteBackupRetention)
}

// RemoteRestore unpacks a previously created backup archive over the remote runtime directory.
func RemoteRestore(ctx context.Context, runner *ssh.Runner, cfg ssh.Config, archiveRemotePath string) error {
	if !safeArchivePath(archiveRemotePath) {
		return fmt.Errorf("invalid remote archive path")
	}
	cmd := buildRemoteRestoreScript(archiveRemotePath)
	_, err := runner.Exec(ctx, cfg, cmd)
	if err != nil {
		return fmt.Errorf("remote restore on %s from %s: %w", cfg.Host, archiveRemotePath, err)
	}
	return nil
}

// buildRemoteRestoreScript renders the shell script that restores a backup archive into the runtime directory.
func buildRemoteRestoreScript(archiveRemotePath string) string {
	return fmt.Sprintf(`set -e
if sudo test -f %[1]s.sha256; then sudo sha256sum -c %[1]s.sha256 >/dev/null; fi
sudo tar -tzf %[1]s >/dev/null
TMP=$(mktemp -d)
sudo tar -xzf %[1]s -C "$TMP"
if [ -f %[2]s/docker-compose.yml ]; then (cd %[2]s && sudo docker compose --env-file .env -f docker-compose.yml down); fi
sudo rm -rf %[2]s
sudo mkdir -p %[2]s
sudo cp -a "$TMP/stack/." %[2]s/
(cd %[2]s && sudo docker compose --env-file .env -f docker-compose.yml create)
VOLUME=$(sudo docker volume ls -q --filter label=com.docker.compose.project=ovpn --filter label=com.docker.compose.volume=ovpn-agent-data)
if [ -d "$TMP/stats" ] && [ -z "$VOLUME" ]; then echo 'Backup volume unavailable' >&2; exit 1; fi
if [ -n "$VOLUME" ] && [ -d "$TMP/stats" ]; then
  MP=$(sudo docker volume inspect "$VOLUME" --format '{{.Mountpoint}}')
  sudo find "$MP" -maxdepth 1 -type f \( -name '*.db' -o -name '*.db-wal' -o -name '*.db-shm' \) -delete
  sudo cp -a "$TMP/stats/." "$MP/"
fi
sudo rm -rf "$TMP"
cd %[2]s
sudo docker compose --env-file .env -f docker-compose.yml up -d --force-recreate
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

func safeArchivePath(path string) bool {
	if !strings.HasPrefix(path, deploy.RemoteBackupDir+"/") || !strings.HasSuffix(path, ".tgz") {
		return false
	}
	for _, r := range path {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("/._-", r) {
			return false
		}
	}
	return filepath.Clean(path) == path
}

func archiveName(name string, id int64) string {
	var out strings.Builder
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			out.WriteRune(r)
		} else {
			out.WriteByte('-')
		}
	}
	safe := strings.Trim(out.String(), "-")
	if safe == "" {
		safe = fmt.Sprintf("server-%d", id)
	}
	return safe
}
