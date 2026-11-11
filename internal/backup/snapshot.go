package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLite's online snapshot includes committed WAL pages while collectors continue.
// Files are streamed into a private archive; symlinks cannot import external data.
func writeSnapshot(dataDir, outDir, archive string) (err error) {
	stage, err := os.MkdirTemp("", "ovpn-snapshot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	file, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if e := file.Close(); err == nil {
			err = e
		}
	}()
	gz := gzip.NewWriter(file)
	defer func() {
		if e := gz.Close(); err == nil {
			err = e
		}
	}()
	tarball := tar.NewWriter(gz)
	defer func() {
		if e := tarball.Close(); err == nil {
			err = e
		}
	}()
	return filepath.WalkDir(dataDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == outDir {
			return filepath.SkipDir
		}
		name, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "work" || entry.Name() == "managed-work" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") || strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".lock") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		source := path
		if strings.HasSuffix(name, ".db") {
			source = filepath.Join(stage, fmt.Sprint(time.Now().UnixNano())+".db")
			u := url.URL{Scheme: "file", Path: path}
			q := u.Query()
			q.Set("mode", "ro")
			q.Add("_pragma", "busy_timeout(5000)")
			u.RawQuery = q.Encode()
			db, e := sql.Open("sqlite", u.String())
			if e != nil {
				return e
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, e = db.ExecContext(ctx, "VACUUM INTO ?", source)
			cancel()
			_ = db.Close()
			if e != nil {
				return fmt.Errorf("snapshot database: %w", e)
			}
		}
		input, err := os.Open(source)
		if err != nil {
			return err
		}
		defer input.Close()
		// Atomic journal replacements must use the opened file's size, not a
		// directory entry observed before a concurrent rename.
		info, err = input.Stat()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		header.Mode = 0o600
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		if err = tarball.WriteHeader(header); err != nil {
			return err
		}
		_, err = io.CopyN(tarball, input, info.Size())
		return err
	})
}
