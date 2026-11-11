package machine

import (
	"context"
	"ovpn/internal/app"
	"ovpn/internal/machine/protocol"
	"ovpn/internal/operations"
)

type controlBackend interface {
	ControlReady() bool
	Catalog(context.Context) ([]operations.Action, error)
	OperationPlan(context.Context, operations.Request) (operations.Plan, error)
	OperationStart(context.Context, operations.Start) (operations.Job, error)
	OperationList(context.Context) ([]operations.Job, error)
	OperationResume(context.Context, string) (operations.Job, error)
	OperationCancel(context.Context, string) error
	OperationResult(context.Context, string) (operations.Result, error)
}

func dispatchControl(ctx context.Context, backend controlBackend, request protocol.Request) (any, error) {
	bad := &app.Error{Code: "INVALID_PARAMS"}
	switch request.Method {
	case "operations.catalog", "operations.list":
		if protocol.Decode(request.Params, &struct{}{}) != nil {
			return nil, bad
		}
		if request.Method == "operations.catalog" {
			return backend.Catalog(ctx)
		}
		return backend.OperationList(ctx)
	case "operations.plan":
		var r operations.Request
		if protocol.Decode(request.Params, &r) != nil {
			return nil, bad
		}
		return backend.OperationPlan(ctx, r)
	case "operations.start":
		var r operations.Start
		if protocol.Decode(request.Params, &r) != nil {
			return nil, bad
		}
		return backend.OperationStart(ctx, r)
	case "operations.resume", "operations.cancel", "operations.result":
		var r struct {
			ID string `json:"id"`
		}
		if protocol.Decode(request.Params, &r) != nil {
			return nil, bad
		}
		switch request.Method {
		case "operations.resume":
			return backend.OperationResume(ctx, r.ID)
		case "operations.cancel":
			return struct{}{}, backend.OperationCancel(ctx, r.ID)
		default:
			return backend.OperationResult(ctx, r.ID)
		}
	}
	return nil, &app.Error{Code: "METHOD_NOT_FOUND"}
}
