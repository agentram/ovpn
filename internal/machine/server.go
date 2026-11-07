package machine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"ovpn/internal/app"
	"ovpn/internal/app/dto"
	"ovpn/internal/machine/protocol"
	"ovpn/internal/version"
)

type Backend interface {
	Summary(context.Context) (dto.Summary, error)
	Servers(context.Context, string, int) (dto.ServerPage, error)
	Users(context.Context, string, int) (dto.UserPage, error)
	Check(context.Context, dto.CheckRequest) (dto.Check, error)
}

type userPlanner interface {
	PlanUser(context.Context, dto.UserPlanRequest) (dto.UserPlan, error)
}

type checkUserLister interface {
	CheckUsers(context.Context, string, string, int) (dto.CheckUserPage, error)
}

type observerBackend interface {
	ObserverStatus(context.Context) (dto.ObserverSnapshot, error)
	ObserverConfigure(context.Context, dto.ObservationPolicy) (dto.ObservationPolicy, error)
	ObserverStart(context.Context) error
	ObserverStop(context.Context) error
	ObserverRemove(context.Context, string) error
	ObserverRun(context.Context, dto.PolicyRunRequest) (dto.ObservationJob, error)
	ObserverCancel(context.Context, string) error
	DiagnosticStart(context.Context, dto.DiagnosticRequest) (dto.ObservationJob, error)
	IntegrationInstall(context.Context, string) (dto.ObservationJob, error)
	IntegrationSetup(context.Context, dto.ToolSetup) (dto.ObservationTool, error)
}

// Serve handles one bounded request at a time. EOF or process termination ends the session.
func Serve(ctx context.Context, in io.Reader, out io.Writer, backend Backend) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), protocol.MaxMessageBytes)
	encoder := json.NewEncoder(out)
	ready := false
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		request := protocol.Request{}
		if err := protocol.Decode(scanner.Bytes(), &request); err != nil || !protocol.ValidID(request.ID) || len(request.Params) == 0 || request.Params[0] != '{' {
			if err := encoder.Encode(protocol.Response{Error: wireError("INVALID_REQUEST")}); err != nil {
				return err
			}
			continue
		}
		var value any
		var err error
		if request.Method == "engine.hello" {
			var params struct {
				ProtocolVersion int `json:"protocolVersion"`
			}
			if protocol.Decode(request.Params, &params) != nil {
				err = &app.Error{Code: "INVALID_PARAMS"}
			} else if params.ProtocolVersion != protocol.Version {
				ready = false
				err = &app.Error{Code: "PROTOCOL_MISMATCH"}
			} else {
				ready = true
				capabilities := []string{"inventory.read", "users.read", "checks.run"}
				if _, ok := backend.(userPlanner); ok {
					capabilities = append(capabilities, "users.plan.read")
				}
				if _, ok := backend.(checkUserLister); ok {
					capabilities = append(capabilities, "checks.users.read")
				}
				if _, ok := backend.(observerBackend); ok {
					capabilities = append(capabilities, "observer.read", "observer.configure", "observer.run", "integrations.install", "diagnostics.run")
				}
				value = protocol.Hello{ProtocolVersion: protocol.Version, EngineVersion: version.Current(), Capabilities: capabilities}
			}
		} else if !ready {
			err = &app.Error{Code: "HANDSHAKE_REQUIRED"}
		} else {
			value, err = dispatch(ctx, backend, request)
		}
		response := protocol.Response{ID: request.ID}
		if err != nil {
			var safe *app.Error
			if errors.As(err, &safe) {
				response.Error = wireError(safe.Code)
			} else {
				response.Error = wireError("INTERNAL_ERROR")
			}
		} else {
			response.Result, err = json.Marshal(value)
			if err != nil || len(response.Result) > protocol.MaxMessageBytes-256 {
				response.Result = nil
				response.Error = wireError("RESPONSE_TOO_LARGE")
			}
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	if scanner.Err() != nil {
		_ = encoder.Encode(protocol.Response{Error: wireError("MESSAGE_TOO_LARGE")})
		return errors.New("machine input unavailable")
	}
	return nil
}

func dispatch(ctx context.Context, backend Backend, request protocol.Request) (any, error) {
	bad := &app.Error{Code: "INVALID_PARAMS"}
	if strings.HasPrefix(request.Method, "observer.") || strings.HasPrefix(request.Method, "integrations.") || strings.HasPrefix(request.Method, "diagnostics.") {
		if control, ok := backend.(observerBackend); ok {
			return dispatchObserver(ctx, control, request)
		}
		return nil, &app.Error{Code: "METHOD_NOT_FOUND"}
	}
	switch request.Method {
	case "workspace.summary":
		if protocol.Decode(request.Params, &struct{}{}) != nil {
			return nil, bad
		}
		return backend.Summary(ctx)
	case "servers.list":
		var params struct {
			Cursor string `json:"cursor,omitempty"`
			Limit  int    `json:"limit,omitempty"`
		}
		if protocol.Decode(request.Params, &params) != nil {
			return nil, bad
		}
		return backend.Servers(ctx, params.Cursor, params.Limit)
	case "users.list":
		var params struct {
			Cursor string `json:"cursor,omitempty"`
			Limit  int    `json:"limit,omitempty"`
		}
		if protocol.Decode(request.Params, &params) != nil {
			return nil, bad
		}
		return backend.Users(ctx, params.Cursor, params.Limit)
	case "checks.run":
		var params dto.CheckRequest
		if protocol.Decode(request.Params, &params) != nil {
			return nil, bad
		}
		return backend.Check(ctx, params)
	case "checks.users":
		var params struct {
			ServerID string `json:"serverId"`
			Cursor   string `json:"cursor,omitempty"`
			Limit    int    `json:"limit,omitempty"`
		}
		if protocol.Decode(request.Params, &params) != nil {
			return nil, bad
		}
		if users, ok := backend.(checkUserLister); ok {
			return users.CheckUsers(ctx, params.ServerID, params.Cursor, params.Limit)
		}
		return nil, &app.Error{Code: "METHOD_NOT_FOUND"}
	case "users.plan":
		var params dto.UserPlanRequest
		if protocol.Decode(request.Params, &params) != nil {
			return nil, bad
		}
		if planner, ok := backend.(userPlanner); ok {
			return planner.PlanUser(ctx, params)
		}
		return nil, &app.Error{Code: "METHOD_NOT_FOUND"}
	default:
		return nil, &app.Error{Code: "METHOD_NOT_FOUND"}
	}
}

func dispatchObserver(ctx context.Context, backend observerBackend, request protocol.Request) (any, error) {
	bad := &app.Error{Code: "INVALID_PARAMS"}
	ack := func(err error) (any, error) {
		return struct {
			Accepted bool `json:"accepted"`
		}{err == nil}, err
	}
	switch request.Method {
	case "observer.status", "observer.start", "observer.stop":
		if protocol.Decode(request.Params, &struct{}{}) != nil {
			return nil, bad
		}
		if request.Method == "observer.status" {
			return backend.ObserverStatus(ctx)
		}
		if request.Method == "observer.start" {
			return ack(backend.ObserverStart(ctx))
		}
		return ack(backend.ObserverStop(ctx))
	case "observer.configure":
		var p dto.ObservationPolicy
		if protocol.Decode(request.Params, &p) != nil {
			return nil, bad
		}
		return backend.ObserverConfigure(ctx, p)
	case "observer.remove", "observer.cancel":
		var p struct {
			ID string `json:"id"`
		}
		if protocol.Decode(request.Params, &p) != nil || len(p.ID) != 32 {
			return nil, bad
		}
		if request.Method == "observer.remove" {
			return ack(backend.ObserverRemove(ctx, p.ID))
		}
		return ack(backend.ObserverCancel(ctx, p.ID))
	case "observer.run":
		var p dto.PolicyRunRequest
		if protocol.Decode(request.Params, &p) != nil {
			return nil, bad
		}
		return backend.ObserverRun(ctx, p)
	case "diagnostics.start":
		var p dto.DiagnosticRequest
		if protocol.Decode(request.Params, &p) != nil {
			return nil, bad
		}
		return backend.DiagnosticStart(ctx, p)
	case "integrations.install":
		var p struct {
			Engine string `json:"engine"`
		}
		if protocol.Decode(request.Params, &p) != nil {
			return nil, bad
		}
		return backend.IntegrationInstall(ctx, p.Engine)
	case "integrations.setup":
		var p dto.ToolSetup
		if protocol.Decode(request.Params, &p) != nil {
			return nil, bad
		}
		return backend.IntegrationSetup(ctx, p)
	default:
		return nil, &app.Error{Code: "METHOD_NOT_FOUND"}
	}
}

func wireError(code string) *protocol.Error {
	return &protocol.Error{Code: code, Message: "The request could not be completed (" + code + ")."}
}
