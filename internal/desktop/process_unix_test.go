//go:build darwin || linux

package desktop

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerShutdownReleasesDescendantListener(t *testing.T) {
	state := filepath.Join(t.TempDir(), "child-endpoint")
	t.Setenv("OVPN_DESKTOP_WORKER_TEST", "with-child")
	t.Setenv("OVPN_DESKTOP_CHILD_STATE", state)
	w, err := Launch(context.Background(), helperConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var endpoint []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		endpoint, err = os.ReadFile(state)
		if err == nil && len(endpoint) != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(endpoint) == 0 {
		t.Fatal("descendant did not start")
	}
	if listener, err := net.Listen("tcp", string(endpoint)); err == nil {
		_ = listener.Close()
		t.Fatal("descendant listener was not held")
	}
	w.Close()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if listener, err := net.Listen("tcp", string(endpoint)); err == nil {
			_ = listener.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker shutdown left an owned descendant alive")
}
