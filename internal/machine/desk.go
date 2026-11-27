package machine

import (
	"context"
	"ovpn/internal/app"
	"ovpn/internal/app/dto"
	"ovpn/internal/machine/protocol"
)

type deskBackend interface {
	ControlReady() bool
	DeskSnapshot(context.Context) (dto.DeskSnapshot, error)
	DeskConfigure(context.Context, dto.DeskConfiguration) (dto.DeskSnapshot, error)
	DeskPause(context.Context) error
	DeskRoutes(context.Context, dto.RouteQuery) (dto.RoutePage, error)
}

func dispatchDesk(ctx context.Context, b deskBackend, r protocol.Request) (any, error) {
	bad := &app.Error{Code: "INVALID_PARAMS"}
	switch r.Method {
	case "desk.snapshot", "desk.pause":
		if protocol.Decode(r.Params, &struct{}{}) != nil {
			return nil, bad
		}
		if r.Method == "desk.snapshot" {
			return b.DeskSnapshot(ctx)
		}
		return struct{}{}, b.DeskPause(ctx)
	case "desk.configure":
		var v dto.DeskConfiguration
		if protocol.Decode(r.Params, &v) != nil {
			return nil, bad
		}
		return b.DeskConfigure(ctx, v)
	case "desk.routes":
		var v dto.RouteQuery
		if protocol.Decode(r.Params, &v) != nil {
			return nil, bad
		}
		return b.DeskRoutes(ctx, v)
	}
	return nil, &app.Error{Code: "METHOD_NOT_FOUND"}
}
