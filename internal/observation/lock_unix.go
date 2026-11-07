//go:build darwin || linux

package observation

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func Lock(dataDir string) (func(), error) {
	dir := Directory(dataDir)
	if err := PrivateDir(dir); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "owner.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
