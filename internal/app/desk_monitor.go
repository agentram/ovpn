package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"

	"ovpn/internal/app/dto"
	"ovpn/internal/clientprofile"
	"ovpn/internal/deskmonitor"
	"ovpn/internal/integrations"
	"ovpn/internal/model"
	"ovpn/internal/observation"
	"ovpn/internal/store/local"
)

type TelemetrySource interface {
	Telemetry(context.Context, bool) ([]dto.ServerLive, error)
}
type deskSource struct{ service *DesktopService }

func (d deskSource) Targets(ctx context.Context) ([]deskmonitor.Target, error) {
	store, err := local.OpenReadOnly(ctx, d.service.DataDir)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	servers, err := store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	targets := []deskmonitor.Target{}
	for _, server := range servers {
		if !server.Enabled {
			continue
		}
		var topology []model.ProxyBackend
		if server.IsProxy() {
			topology, err = store.ListProxyBackends(ctx, server.ID)
			if err != nil {
				return nil, err
			}
		}
		users, err := store.ListUsers(ctx, server.ID)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			if clientprofile.ValidateCheckUser(server, user) != nil {
				continue
			}
			for _, profile := range server.NormalizedEnabledProfiles() {
				engine := "checker"
				if profile == model.TransportProfileRealityTCPVision || profile == model.TransportProfileTLSSelfSNIWeb {
					engine = "native"
				}
				raw, _ := json.Marshal(struct {
					Server   model.Server
					User     model.User
					Profile  string
					Topology []model.ProxyBackend
				}{server, user, profile, topology})
				sum := sha256.Sum256(raw)
				target := dto.ObservationTarget{ServerID: fmt.Sprintf("node-%d", server.ID), Username: user.Username, Profile: profile}
				targets = append(targets, deskmonitor.Target{Target: target, Engine: engine, Revision: hex.EncodeToString(sum[:])})
			}
		}
	}
	return targets, nil
}
func (d deskSource) Telemetry(ctx context.Context, presenceOnly bool) ([]dto.ServerLive, error) {
	if d.service.telemetry == nil {
		return nil, &Error{Code: "TELEMETRY_UNAVAILABLE"}
	}
	return d.service.telemetry.Telemetry(ctx, presenceOnly)
}
func (d deskSource) Measure(ctx context.Context, target deskmonitor.Target) dto.Observation {
	input, err := d.service.prepareObservation(ctx, target.Target, target.Engine)
	if err != nil {
		return dto.Observation{Outcome: "skipped", Code: "TARGET_UNAVAILABLE", FailureClass: "policy"}
	}
	var tool integrations.Tool
	if target.Engine == "checker" {
		store, err := observation.Open(d.service.DataDir, false)
		if err != nil {
			return dto.Observation{Outcome: "observer_error", Code: "TOOL_UNAVAILABLE"}
		}
		defer store.Close()
		rows, err := store.List(ctx, "tools")
		if err != nil {
			return dto.Observation{Outcome: "observer_error", Code: "TOOL_UNAVAILABLE"}
		}
		for _, raw := range rows {
			var candidate integrations.Tool
			if json.Unmarshal(raw, &candidate) == nil && candidate.Engine == "checker" {
				tool = candidate
				break
			}
		}
		if tool.Path == "" {
			_, _ = d.service.observer.Install(ctx, "checker")
			return dto.Observation{Outcome: "observer_error", Code: "TOOL_UNAVAILABLE"}
		}
	}
	runner := integrations.Runner{WorkDir: filepath.Join(observation.Directory(d.service.DataDir), "managed-work")}
	return runner.Run(ctx, target.Engine, tool, input)
}

func (s *DesktopService) DeskSnapshot(context.Context) (dto.DeskSnapshot, error) {
	s.monitor.Pulse()
	return s.monitor.Snapshot(), nil
}
func (s *DesktopService) DeskConfigure(ctx context.Context, r dto.DeskConfiguration) (dto.DeskSnapshot, error) {
	if r.Enabled && !r.TrafficConfirmed {
		return dto.DeskSnapshot{}, &Error{Code: "TRAFFIC_CONFIRMATION_REQUIRED"}
	}
	if err := s.monitor.Configure(r.Enabled); err != nil {
		return dto.DeskSnapshot{}, &Error{Code: "OBSERVATION_UNAVAILABLE"}
	}
	targets, _ := (deskSource{service: s}).Targets(ctx)
	needsChecker := false
	for _, target := range targets {
		if target.Engine == "checker" {
			needsChecker = true
			break
		}
	}
	if r.Enabled && needsChecker {
		status, err := s.observer.Status(ctx)
		configured := false
		if err == nil {
			for _, tool := range status.Tools {
				if tool.Engine == "checker" && tool.Configured {
					configured = true
				}
			}
		}
		if !configured {
			_, _ = s.observer.Install(ctx, "checker")
		}
	}
	return s.monitor.Snapshot(), nil
}
func (s *DesktopService) DeskPause(context.Context) error { return s.monitor.Stop() }
func (s *DesktopService) DeskRoutes(_ context.Context, r dto.RouteQuery) (dto.RoutePage, error) {
	if r.Limit == 0 {
		r.Limit = 100
	}
	offset := 0
	if r.Cursor != "" {
		value, err := strconv.Atoi(r.Cursor)
		if err != nil || value < 0 {
			return dto.RoutePage{}, &Error{Code: "INVALID_PARAMS"}
		}
		offset = value
	}
	if r.Limit < 1 || r.Limit > 100 {
		return dto.RoutePage{}, &Error{Code: "INVALID_PARAMS"}
	}
	rows := s.monitor.Routes(r.ServerID, r.Username)
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i].Target, rows[j].Target
		if a.ServerID != b.ServerID {
			return a.ServerID < b.ServerID
		}
		if a.Username != b.Username {
			return a.Username < b.Username
		}
		return a.Profile < b.Profile
	})
	if offset > len(rows) {
		offset = len(rows)
	}
	end := offset + r.Limit
	if end > len(rows) {
		end = len(rows)
	}
	result := dto.RoutePage{Items: rows[offset:end]}
	if end < len(rows) {
		result.NextCursor = strconv.Itoa(end)
	}
	return result, nil
}
