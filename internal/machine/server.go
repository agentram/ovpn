package machine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"

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
				value = protocol.Hello{ProtocolVersion: protocol.Version, EngineVersion: version.Current(), Capabilities: []string{"inventory.read", "users.read", "checks.run"}}
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
	default:
		return nil, &app.Error{Code: "METHOD_NOT_FOUND"}
	}
}

func wireError(code string) *protocol.Error {
	return &protocol.Error{Code: code, Message: "The request could not be completed (" + code + ")."}
}
