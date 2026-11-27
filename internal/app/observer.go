package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"ovpn/internal/app/dto"
	"ovpn/internal/clientprofile"
	"ovpn/internal/deskmonitor"
	"ovpn/internal/integrations"
	"ovpn/internal/model"
	"ovpn/internal/observer"
	"ovpn/internal/operations"
	"ovpn/internal/store/local"
)

type DesktopService struct {
	ReadService
	observer  *observer.Controller
	control   *operations.Service
	monitor   *deskmonitor.Controller
	telemetry TelemetrySource
}

func NewDesktopService(ctx context.Context, dataDir string) *DesktopService {
	s := &DesktopService{ReadService: ReadService{DataDir: dataDir}}
	s.observer = observer.New(ctx, dataDir, s.prepareObservation)
	s.monitor = deskmonitor.New(ctx, dataDir, deskSource{service: s})
	return s
}
func (s *DesktopService) Close() error {
	s.monitor.Close()
	if s.control != nil {
		s.control.Close()
	}
	return s.observer.Stop()
}

func (s ReadService) prepareObservation(ctx context.Context, target dto.ObservationTarget, engine string) (integrations.Input, error) {
	id, err := serverID(target.ServerID)
	if err != nil {
		return integrations.Input{}, err
	}
	if (engine == "native" || engine == "knife-singbox") && target.Profile != model.TransportProfileRealityTCPVision && target.Profile != model.TransportProfileTLSSelfSNIWeb {
		return integrations.Input{}, &Error{Code: "UNSUPPORTED_PROFILE"}
	}
	st, err := local.OpenReadOnly(ctx, s.DataDir)
	if err != nil {
		return integrations.Input{}, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	defer st.Close()
	srv, err := st.GetClientServer(ctx, id)
	if err != nil {
		return integrations.Input{}, &Error{Code: "TARGET_UNAVAILABLE"}
	}
	u, err := st.GetUser(ctx, id, target.Username)
	if err != nil || clientprofile.ValidateCheckUser(*srv, *u) != nil {
		return integrations.Input{}, &Error{Code: "TARGET_UNAVAILABLE"}
	}
	cfg, err := clientprofile.ConfigForUser(*srv, *u, target.Profile, clientprofile.Options{})
	if err != nil {
		return integrations.Input{}, &Error{Code: "TARGET_UNAVAILABLE"}
	}
	key := observer.TargetKey(target)
	cfg.Remarks = key
	link, err := clientprofile.Link(*srv, *u, target.Profile)
	if err != nil {
		return integrations.Input{}, &Error{Code: "TARGET_UNAVAILABLE"}
	}
	link, err = integrations.OpaqueLink(link, key)
	if err != nil {
		return integrations.Input{}, &Error{Code: "TARGET_UNAVAILABLE"}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return integrations.Input{}, &Error{Code: "TARGET_UNAVAILABLE"}
	}
	sum := sha256.Sum256(raw)
	return integrations.Input{Config: cfg, Link: link, Revision: hex.EncodeToString(sum[:]), Key: key}, nil
}

func observerError(err error) error {
	if err == nil {
		return nil
	}
	var safe *observer.Error
	if errors.As(err, &safe) {
		return &Error{Code: safe.Code}
	}
	return &Error{Code: "OBSERVATION_UNAVAILABLE"}
}
func (s *DesktopService) ObserverStatus(ctx context.Context) (dto.ObserverSnapshot, error) {
	v, e := s.observer.Status(ctx)
	return v, observerError(e)
}
func (s *DesktopService) ObserverConfigure(ctx context.Context, p dto.ObservationPolicy) (dto.ObservationPolicy, error) {
	v, e := s.observer.Configure(ctx, p)
	return v, observerError(e)
}
func (s *DesktopService) ObserverStart(ctx context.Context) error {
	return observerError(s.observer.Start(ctx))
}
func (s *DesktopService) ObserverStop(context.Context) error { return observerError(s.observer.Stop()) }
func (s *DesktopService) ObserverRemove(ctx context.Context, id string) error {
	return observerError(s.observer.RemovePolicy(ctx, id))
}
func (s *DesktopService) ObserverRun(ctx context.Context, r dto.PolicyRunRequest) (dto.ObservationJob, error) {
	v, e := s.observer.RunPolicy(ctx, r)
	return v, observerError(e)
}
func (s *DesktopService) ObserverCancel(_ context.Context, id string) error {
	return observerError(s.observer.Cancel(id))
}
func (s *DesktopService) DiagnosticStart(ctx context.Context, r dto.DiagnosticRequest) (dto.ObservationJob, error) {
	v, e := s.observer.Diagnose(ctx, r)
	return v, observerError(e)
}
func (s *DesktopService) IntegrationInstall(ctx context.Context, engine string) (dto.ObservationJob, error) {
	v, e := s.observer.Install(ctx, engine)
	return v, observerError(e)
}
func (s *DesktopService) IntegrationSetup(ctx context.Context, r dto.ToolSetup) (dto.ObservationTool, error) {
	v, e := s.observer.Setup(ctx, r)
	return v, observerError(e)
}
