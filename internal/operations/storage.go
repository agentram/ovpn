package operations

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"ovpn/internal/observation"
)

type record struct {
	Version  int             `json:"version"`
	Plan     Plan            `json:"plan"`
	Request  Request         `json:"request"`
	Revision string          `json:"revision"`
	Snapshot json.RawMessage `json:"snapshot"`
	Job      *Job            `json:"job,omitempty"`
	Key      string          `json:"key,omitempty"`
}

func directory(workspace string) string { return filepath.Join(workspace, "operations") }

func privateRoot(workspace string) (*os.Root, error) {
	path := directory(workspace)
	if err := observation.PrivateDir(path); err != nil {
		return nil, err
	}
	return os.OpenRoot(path)
}

// WithWriter coordinates the CLI and the desktop without changing observer ownership.
func WithWriter(workspace string, fn func() error) error {
	unlock, err := observation.Lock(filepath.Join(directory(workspace), "writer"))
	if err != nil {
		return ErrBusy
	}
	defer unlock()
	return fn()
}

func key(root *os.Root) ([]byte, error) {
	value, err := root.ReadFile("journal.key")
	if err == nil && len(value) == 32 {
		return value, nil
	}
	if err == nil {
		return nil, ErrUnavailable
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	value = make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, value); err != nil {
		return nil, err
	}
	f, err := root.OpenFile("journal.key", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return key(root)
	}
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(value); err != nil {
		f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return nil, err
	}
	return value, f.Close()
}

func envelope(root *os.Root) (cipher.AEAD, error) {
	k, err := key(root)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func save(root *os.Root, value record) error {
	value.Version = 1
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	box, err := envelope(root)
	if err != nil {
		return err
	}
	nonce := make([]byte, box.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	raw = box.Seal(nonce, nonce, raw, []byte(value.Plan.ID))
	name := value.Plan.ID + ".tmp"
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := root.Rename(name, value.Plan.ID+".json.enc"); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	if runtime.GOOS == "windows" {
		return nil
	}
	return directory.Sync()
}

func read(root *os.Root, id string) (record, error) {
	if !validID(id) {
		return record{}, ErrInvalid
	}
	raw, err := root.ReadFile(id + ".json.enc")
	if err != nil {
		return record{}, ErrUnavailable
	}
	box, err := envelope(root)
	if err != nil || len(raw) < box.NonceSize() {
		return record{}, ErrUnavailable
	}
	plain, err := box.Open(nil, raw[:box.NonceSize()], raw[box.NonceSize():], []byte(id))
	if err != nil {
		return record{}, ErrUnavailable
	}
	var r record
	if json.Unmarshal(plain, &r) != nil || r.Version != 1 {
		return r, ErrUnavailable
	}
	r.Plan.Request, r.Plan.Revision, r.Plan.Snapshot = r.Request, r.Revision, r.Snapshot
	return r, nil
}

func records(root *os.Root) ([]record, error) {
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := []record{}
	for _, entry := range names {
		if id, ok := strings.CutSuffix(entry.Name(), ".json.enc"); ok {
			r, err := read(root, id)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
		}
	}
	return out, nil
}
