package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"ovpn/internal/app/dto"
	"ovpn/internal/desktop"
	"ovpn/internal/machine/protocol"
	"ovpn/internal/util"
)

//go:embed all:frontend/dist
var assets embed.FS

var workerSHA256 string

type DeskService struct {
	mu     sync.Mutex
	ctx    context.Context
	cfg    desktop.Config
	worker *desktop.Worker
}

func (s *DeskService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.ctx = ctx
	return nil
}
func (s *DeskService) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker != nil {
		s.worker.Close()
	}
	return nil
}

func (s *DeskService) connect() (*desktop.Worker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker != nil {
		if s.worker.Alive() {
			return s.worker, nil
		}
		s.worker.Close()
		s.worker = nil
	}
	w, err := desktop.Launch(s.ctx, s.cfg)
	if err != nil {
		return nil, errors.New("WORKER_UNAVAILABLE")
	}
	s.worker = w
	return w, nil
}

func (s *DeskService) Hello() (protocol.Hello, error) {
	w, err := s.connect()
	if err != nil {
		return protocol.Hello{}, err
	}
	return w.Hello, nil
}
func (s *DeskService) Summary(ctx context.Context) (dto.Summary, error) {
	w, err := s.connect()
	if err != nil {
		return dto.Summary{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.Summary(ctx)
}
func (s *DeskService) Servers(ctx context.Context, cursor string) (dto.ServerPage, error) {
	w, err := s.connect()
	if err != nil {
		return dto.ServerPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.Servers(ctx, cursor)
}
func (s *DeskService) Users(ctx context.Context, cursor string) (dto.UserPage, error) {
	w, err := s.connect()
	if err != nil {
		return dto.UserPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.Users(ctx, cursor)
}
func (s *DeskService) Check(ctx context.Context, request dto.CheckRequest) (dto.Check, error) {
	w, err := s.connect()
	if err != nil {
		return dto.Check{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return w.Check(ctx, request)
}

func main() {
	_ = os.Unsetenv("FRONTEND_DEVSERVER_URL")
	dataDir := flag.String("data-dir", util.DefaultDataDir(), "Existing OVPN workspace")
	flag.Parse()
	executable, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	workerName := "ovpn-worker"
	if filepath.Ext(executable) == ".exe" {
		workerName += ".exe"
	}
	service := &DeskService{cfg: desktop.Config{Path: filepath.Join(filepath.Dir(executable), workerName), SHA256: workerSHA256, DataDir: *dataDir}}
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		os.Exit(1)
	}
	app := application.New(application.Options{
		Name: "OVPN Desk", Description: "Local Xray fleet management",
		Services: []application.Service{application.NewService(service)},
		Assets:   application.AssetOptions{Handler: application.BundledAssetFileServer(frontend), DisableLogging: true},
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "OVPN Desk", URL: "/", Width: 1280, Height: 860, MinWidth: 1024, MinHeight: 720, DevToolsEnabled: false})
	if err := app.Run(); err != nil {
		os.Exit(1)
	}
}
