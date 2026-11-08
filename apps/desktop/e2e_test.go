package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"ovpn/internal/desktop"
)

func TestDeskBrowserJourneys(t *testing.T) {
	worker := os.Getenv("OVPN_E2E_WORKER")
	fixture := os.Getenv("OVPN_E2E_FIXTURE")
	if worker == "" || fixture == "" {
		t.Skip("run make e2e-desktop to exercise Desk in Chromium with the real worker")
	}
	worker, err := filepath.Abs(worker)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	dir := t.TempDir()
	if err := exec.CommandContext(ctx, fixture, "workspace", dir).Run(); err != nil {
		t.Fatal(err)
	}
	service := &DeskService{cfg: desktop.Config{Path: worker, SHA256: fmt.Sprintf("%x", sha256.Sum256(binary)), DataDir: dir}}
	if err := service.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	defer service.ServiceShutdown()

	web, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(web)))
	mux.HandleFunc("POST /wails/runtime", func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Args struct {
				Name string            `json:"methodName"`
				Args []json.RawMessage `json:"args"`
			} `json:"args"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&envelope); err != nil {
			http.Error(w, "invalid runtime request", http.StatusBadRequest)
			return
		}
		value, err := callDesk(r.Context(), service, envelope.Args.Name, envelope.Args.Args)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"kind": "RuntimeError", "message": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	})
	mux.HandleFunc("POST /fixture/disable-alice", func(w http.ResponseWriter, r *http.Request) {
		if err := exec.CommandContext(r.Context(), fixture, "disable-alice", dir).Run(); err != nil {
			http.Error(w, "fixture update failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	cmd := exec.CommandContext(ctx, "npm", "run", "e2e")
	cmd.Dir = "frontend"
	cmd.Env = append(os.Environ(), "OVPN_E2E_DESK_URL="+server.URL)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

// Replace the WebView transport only. The production JS runtime still sends
// its binding envelope and the production Go binding calls the verified worker.
func callDesk(ctx context.Context, service *DeskService, qualified string, raw []json.RawMessage) (any, error) {
	name, ok := strings.CutPrefix(qualified, "main.DeskService.")
	if !ok || strings.HasPrefix(name, "Service") {
		return nil, fmt.Errorf("unknown runtime method")
	}
	method := reflect.ValueOf(service).MethodByName(name)
	if !method.IsValid() {
		return nil, fmt.Errorf("unknown runtime method")
	}
	typ := method.Type()
	args := make([]reflect.Value, 0, typ.NumIn())
	index := 0
	for i := range typ.NumIn() {
		if typ.In(i) == reflect.TypeFor[context.Context]() {
			args = append(args, reflect.ValueOf(ctx))
			continue
		}
		if index >= len(raw) {
			return nil, fmt.Errorf("missing runtime argument")
		}
		value := reflect.New(typ.In(i))
		if err := json.Unmarshal(raw[index], value.Interface()); err != nil {
			return nil, fmt.Errorf("invalid runtime argument")
		}
		args = append(args, value.Elem())
		index++
	}
	if index != len(raw) || typ.NumOut() < 1 || typ.NumOut() > 2 {
		return nil, fmt.Errorf("invalid runtime signature")
	}
	result := method.Call(args)
	if err, _ := result[len(result)-1].Interface().(error); err != nil {
		return nil, err
	}
	if len(result) == 2 {
		return result[0].Interface(), nil
	}
	return nil, nil
}
