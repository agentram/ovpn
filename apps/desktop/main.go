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

//go:embed assets/icon.png
var icon []byte

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
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.worker.DeskPause(ctx)
		_ = s.worker.ObserverAction(ctx, "stop", "")
		cancel()
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

func (s *DeskService) CheckUsers(ctx context.Context, serverID, cursor string) (dto.CheckUserPage, error) {
	w, err := s.connect()
	if err != nil {
		return dto.CheckUserPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.CheckUsers(ctx, serverID, cursor)
}

func (s *DeskService) PlanUser(ctx context.Context, request dto.UserPlanRequest) (dto.UserPlan, error) {
	w, err := s.connect()
	if err != nil {
		return dto.UserPlan{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.PlanUser(ctx, request)
}

func (s *DeskService) ObserverStatus(ctx context.Context) (dto.ObserverSnapshot, error) {
	w, err := s.connect()
	if err != nil {
		return dto.ObserverSnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.ObserverStatus(ctx)
}
func (s *DeskService) ObserverConfigure(ctx context.Context, p dto.ObservationPolicy) (dto.ObservationPolicy, error) {
	w, err := s.connect()
	if err != nil {
		return dto.ObservationPolicy{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.ObserverConfigure(ctx, p)
}
func (s *DeskService) ObserverAction(ctx context.Context, action, id string) error {
	w, err := s.connect()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.ObserverAction(ctx, action, id)
}
func (s *DeskService) ObserverRun(ctx context.Context, p dto.PolicyRunRequest) (dto.ObservationJob, error) {
	w, err := s.connect()
	if err != nil {
		return dto.ObservationJob{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.ObserverRun(ctx, p)
}
func (s *DeskService) DiagnosticStart(ctx context.Context, p dto.DiagnosticRequest) (dto.ObservationJob, error) {
	w, err := s.connect()
	if err != nil {
		return dto.ObservationJob{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.DiagnosticStart(ctx, p)
}
func (s *DeskService) IntegrationInstall(ctx context.Context, engine string) (dto.ObservationJob, error) {
	w, err := s.connect()
	if err != nil {
		return dto.ObservationJob{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.IntegrationInstall(ctx, engine)
}
func (s *DeskService) IntegrationSetup(ctx context.Context, p dto.ToolSetup) (dto.ObservationTool, error) {
	w, err := s.connect()
	if err != nil {
		return dto.ObservationTool{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.IntegrationSetup(ctx, p)
}

func main() {
	_ = os.Unsetenv("FRONTEND_DEVSERVER_URL")
	defaultDir := os.Getenv("OVPN_DESK_DATA_DIR")
	if defaultDir == "" {
		defaultDir = util.DefaultDataDir()
	}
	dataDir := flag.String("data-dir", defaultDir, "Existing OVPN workspace")
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
		Name: "OVPN Desk", Icon: icon, Description: "Local Xray fleet management",
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
