package desktop

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/machine/protocol"
	"ovpn/internal/version"
)

var errWorker = errors.New("WORKER_UNAVAILABLE")

type Config struct{ Path, SHA256, DataDir string }

type Worker struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Scanner
	gate     chan struct{}
	done     chan struct{}
	closed   atomic.Bool
	sequence atomic.Uint64
	once     sync.Once
	Hello    protocol.Hello
}

func Launch(ctx context.Context, cfg Config) (*Worker, error) {
	if err := verifyWorker(cfg); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, cfg.Path, "--data-dir", cfg.DataDir, "machine", "--stdio", "--protocol", "1") // #nosec G204 -- absolute bundled executable verified against the host's embedded digest.
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, errWorker
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, errWorker
	}
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		return nil, errWorker
	}
	w := &Worker{cmd: cmd, stdin: in, stdout: bufio.NewScanner(out), gate: make(chan struct{}, 1), done: make(chan struct{})}
	w.stdout.Buffer(make([]byte, 4096), protocol.MaxMessageBytes)
	go func() { _ = cmd.Wait(); w.closed.Store(true); close(w.done) }()
	handshake, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := w.call(handshake, "engine.hello", struct {
		ProtocolVersion int `json:"protocolVersion"`
	}{protocol.Version}, &w.Hello); err != nil {
		w.Close()
		return nil, err
	}
	if w.Hello.ProtocolVersion != protocol.Version || w.Hello.EngineVersion != version.Current() {
		w.Close()
		return nil, errors.New("WORKER_VERSION_MISMATCH")
	}
	return w, nil
}

func verifyWorker(cfg Config) error {
	if !filepath.IsAbs(cfg.Path) {
		return errors.New("WORKER_PATH_INVALID")
	}
	info, err := os.Lstat(cfg.Path)
	if err != nil || !info.Mode().IsRegular() {
		return errWorker
	}
	expected, err := hex.DecodeString(cfg.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return errors.New("WORKER_HASH_INVALID")
	}
	f, err := os.Open(cfg.Path) // #nosec G304 -- absolute bundled path only.
	if err != nil {
		return errWorker
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errWorker
	}
	if hex.EncodeToString(h.Sum(nil)) != hex.EncodeToString(expected) {
		return errors.New("WORKER_HASH_MISMATCH")
	}
	return nil
}

func (w *Worker) Alive() bool { return !w.closed.Load() }

func (w *Worker) Close() {
	w.once.Do(func() { w.closed.Store(true); _ = w.stdin.Close(); _ = w.cmd.Process.Kill(); <-w.done })
}

func (w *Worker) call(ctx context.Context, method string, params, target any) error {
	select {
	case w.gate <- struct{}{}:
		defer func() { <-w.gate }()
	case <-ctx.Done():
		return errWorker
	}
	if w.closed.Load() {
		return errWorker
	}
	id := fmt.Sprintf("desk-%d", w.sequence.Add(1))
	raw, err := json.Marshal(params)
	if err != nil {
		return errors.New("INVALID_PARAMS")
	}
	request, err := json.Marshal(protocol.Request{ID: id, Method: method, Params: raw})
	if err != nil || len(request) >= protocol.MaxMessageBytes-1 {
		return errors.New("INVALID_PARAMS")
	}
	if _, err := w.stdin.Write(append(request, '\n')); err != nil {
		w.Close()
		return errWorker
	}
	type reply struct {
		value protocol.Response
		err   error
	}
	result := make(chan reply, 1)
	go func() {
		if !w.stdout.Scan() {
			result <- reply{err: errWorker}
			return
		}
		var response protocol.Response
		err := protocol.Decode(w.stdout.Bytes(), &response)
		if err != nil || response.ID != id || (response.Error == nil) == (len(response.Result) == 0) {
			result <- reply{err: errWorker}
			return
		}
		result <- reply{value: response}
	}()
	select {
	case <-ctx.Done():
		w.Close()
		return errWorker
	case response := <-result:
		if response.err != nil {
			w.Close()
			return errWorker
		}
		if response.value.Error != nil {
			return errors.New(response.value.Error.Code)
		}
		if protocol.Decode(response.value.Result, target) != nil {
			w.Close()
			return errWorker
		}
		return nil
	}
}

func (w *Worker) Summary(ctx context.Context) (dto.Summary, error) {
	var result dto.Summary
	err := w.call(ctx, "workspace.summary", struct{}{}, &result)
	return result, err
}
func (w *Worker) Servers(ctx context.Context, cursor string) (dto.ServerPage, error) {
	var result dto.ServerPage
	if len(cursor) > 64 {
		return result, errors.New("INVALID_PARAMS")
	}
	err := w.call(ctx, "servers.list", struct {
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}{cursor, 50}, &result)
	return result, err
}
func (w *Worker) Check(ctx context.Context, request dto.CheckRequest) (dto.Check, error) {
	var result dto.Check
	if len(request.ServerID) > 64 || len(request.Username) > 128 || len(request.Profile) > 128 {
		return result, errors.New("INVALID_PARAMS")
	}
	err := w.call(ctx, "checks.run", request, &result)
	return result, err
}

func (w *Worker) Users(ctx context.Context, cursor string) (dto.UserPage, error) {
	var result dto.UserPage
	if len(cursor) > 4096 {
		return result, errors.New("INVALID_PARAMS")
	}
	err := w.call(ctx, "users.list", struct {
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}{cursor, 50}, &result)
	return result, err
}

func (w *Worker) PlanUser(ctx context.Context, request dto.UserPlanRequest) (dto.UserPlan, error) {
	var result dto.UserPlan
	if len(request.Username) > 128 || len(request.Action) > 16 || len(request.Expiry) > 10 || len(request.ExpectedDigest) > 64 {
		return result, errors.New("INVALID_PARAMS")
	}
	err := w.call(ctx, "users.plan", request, &result)
	return result, err
}
