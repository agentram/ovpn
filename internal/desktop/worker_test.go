package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/machine"
)

type helperBackend struct{}

func (helperBackend) Summary(context.Context) (dto.Summary, error) {
	return dto.Summary{State: "ready", Servers: 2, ReadOnly: true}, nil
}
func (helperBackend) Servers(context.Context, string, int) (dto.ServerPage, error) {
	return dto.ServerPage{Items: []dto.Server{}}, nil
}
func (helperBackend) Check(ctx context.Context, _ dto.CheckRequest) (dto.Check, error) {
	<-ctx.Done()
	return dto.Check{}, ctx.Err()
}

func TestMain(m *testing.M) {
	if helper := os.Getenv("OVPN_DESKTOP_WORKER_TEST"); helper != "" {
		if helper == "noise" {
			_, _ = os.Stdout.WriteString("private-noise\n")
			return
		}
		if err := machine.Serve(context.Background(), os.Stdin, os.Stdout, helperBackend{}); err != nil {
			os.Exit(1)
		}
		return
	}
	os.Exit(m.Run())
}

func helperConfig(t *testing.T) Config {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return Config{Path: path, SHA256: hex.EncodeToString(digest[:]), DataDir: t.TempDir()}
}

func TestWorkerRoundTripConcurrentQueriesAndShutdown(t *testing.T) {
	t.Setenv("OVPN_DESKTOP_WORKER_TEST", "serve")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w, err := Launch(ctx, helperConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			summary, err := w.Summary(ctx)
			if err != nil || summary.Servers != 2 || !summary.ReadOnly {
				t.Errorf("query failed: %v", err)
			}
		})
	}
	wg.Wait()
	w.Close()
	if _, err := w.Summary(ctx); err == nil {
		t.Fatal("closed worker accepted query")
	}
}

func TestWorkerTimeoutKillsUncertainSession(t *testing.T) {
	t.Setenv("OVPN_DESKTOP_WORKER_TEST", "serve")
	w, err := Launch(context.Background(), helperConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := w.Check(ctx, dto.CheckRequest{ServerID: "node-1", Username: "monitor"}); err == nil {
		t.Fatal("hung request completed")
	}
	if _, err := w.Summary(context.Background()); err == nil {
		t.Fatal("timed-out session was reused")
	}
}

func TestWorkerProtocolNoiseIsRedacted(t *testing.T) {
	t.Setenv("OVPN_DESKTOP_WORKER_TEST", "noise")
	_, err := Launch(context.Background(), helperConfig(t))
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("protocol noise escaped")
	}
}

func TestWorkerRejectsUntrustedExecutableBeforeLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker")
	raw := []byte("not executable")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if err := verifyWorker(Config{Path: path, SHA256: hex.EncodeToString(hash[:])}); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{{Path: "relative-worker", SHA256: hex.EncodeToString(hash[:])}, {Path: path, SHA256: "bad"}, {Path: path, SHA256: hex.EncodeToString(make([]byte, 32))}} {
		if _, err := Launch(context.Background(), cfg); err == nil {
			t.Fatal("untrusted worker accepted")
		}
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err == nil {
		if err := verifyWorker(Config{Path: link, SHA256: hex.EncodeToString(hash[:])}); err == nil {
			t.Fatal("symlink worker accepted")
		}
	}
}
