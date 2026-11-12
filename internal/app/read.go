package app

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/model"
	"ovpn/internal/probe"
	"ovpn/internal/store/local"
)

// Error exposes a stable code, never raw database, path or tunnel diagnostics.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

type ReadService struct{ DataDir string }

func (s ReadService) Summary(ctx context.Context) (dto.Summary, error) {
	result := dto.Summary{State: "empty", ReadOnly: true}
	st, err := local.OpenReadOnly(ctx, s.DataDir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	defer st.Close()
	result.Servers, result.EnabledServers, result.Users, err = st.InventorySummary(ctx)
	if err != nil {
		return dto.Summary{}, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	result.State = "ready"
	return result, nil
}

func serverID(raw string) (int64, error) {
	if !strings.HasPrefix(raw, "node-") {
		return 0, &Error{Code: "INVALID_PARAMS"}
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(raw, "node-"), 10, 64)
	if err != nil || id <= 0 || raw != "node-"+strconv.FormatInt(id, 10) {
		return 0, &Error{Code: "INVALID_PARAMS"}
	}
	return id, nil
}

func (s ReadService) Servers(ctx context.Context, cursor string, limit int) (dto.ServerPage, error) {
	result := dto.ServerPage{Items: make([]dto.Server, 0)}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return result, &Error{Code: "INVALID_PARAMS"}
	}
	var after int64
	var err error
	if cursor != "" {
		after, err = serverID(cursor)
		if err != nil {
			return result, err
		}
	}
	st, err := local.OpenReadOnly(ctx, s.DataDir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	defer st.Close()
	servers, err := st.ListServerInventory(ctx, after, limit+1)
	if err != nil {
		return result, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	more := len(servers) > limit
	if more {
		servers = servers[:limit]
	}
	for _, srv := range servers {
		backends := []dto.Backend{}
		if srv.IsProxy() {
			links, e := st.ListProxyBackends(ctx, srv.ID)
			if e != nil {
				return dto.ServerPage{}, &Error{Code: "WORKSPACE_UNAVAILABLE"}
			}
			for _, link := range links {
				backends = append(backends, dto.Backend{ServerID: "node-" + strconv.FormatInt(link.BackendServerID, 10), Enabled: link.Enabled, Priority: link.Priority})
			}
		}
		result.Items = append(result.Items, dto.Server{Backends: backends, ProxyPreset: srv.ProxyPreset, ID: "node-" + strconv.FormatInt(srv.ID, 10), Name: srv.Name,
			Role: srv.NormalizedRole(), Enabled: srv.Enabled, PrimaryProfile: srv.NormalizedPrimaryProfile(),
			Profiles: srv.NormalizedEnabledProfiles(), XrayVersion: srv.XrayVersion, Runtime: "not_checked"})
	}
	if more {
		result.NextCursor = result.Items[len(result.Items)-1].ID
	}
	return result, nil
}

func (s ReadService) Check(ctx context.Context, request dto.CheckRequest) (dto.Check, error) {
	id, err := serverID(request.ServerID)
	if err != nil || len(request.Username) == 0 || len(request.Username) > 128 || strings.TrimSpace(request.Username) != request.Username {
		return dto.Check{}, &Error{Code: "INVALID_PARAMS"}
	}
	st, err := local.OpenReadOnly(ctx, s.DataDir)
	if err != nil {
		return dto.Check{}, &Error{Code: "WORKSPACE_UNAVAILABLE"}
	}
	defer st.Close()
	srv, err := st.GetClientServer(ctx, id)
	if err != nil {
		return dto.Check{}, &Error{Code: "PROFILE_UNAVAILABLE"}
	}
	user, err := st.GetUser(ctx, id, request.Username)
	if err != nil {
		return dto.Check{}, &Error{Code: "PROFILE_UNAVAILABLE"}
	}
	profile := request.Profile
	if profile == "" {
		profile = srv.NormalizedPrimaryProfile()
	}
	if profile != model.TransportProfileRealityTCPVision && profile != model.TransportProfileTLSSelfSNIWeb {
		return dto.Check{}, &Error{Code: "UNSUPPORTED_PROFILE"}
	}
	opts := probe.Options{URL: "https://example.com/", ExpectedStatus: 200, Timeout: 20 * time.Second, MaxBodyBytes: 1 << 20}
	result, err := CheckClient(ctx, *srv, *user, profile, opts)
	if err != nil && result.ErrorCode == "" {
		return dto.Check{}, &Error{Code: "PROFILE_UNAVAILABLE"}
	}
	outcome := "failed"
	if result.Success {
		outcome = "passed"
	}
	return dto.Check{ServerID: request.ServerID, Profile: profile, Source: "local-native", Kind: "https.tcp", Outcome: outcome,
		StatusCode: result.StatusCode, DurationMS: result.DurationMS, TTFBMS: result.TTFBMS, Bytes: result.Bytes,
		ErrorCode: result.ErrorCode, MeasuredAt: time.Now().UTC().Format(time.RFC3339), FreshForSeconds: 300,
		Unverified: []string{"udp", "dns", "ipv6", "gui-import"}}, nil
}
