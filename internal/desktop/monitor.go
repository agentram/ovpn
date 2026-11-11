package desktop

import (
	"context"
	"ovpn/internal/app/dto"
)

func (w *Worker) DeskSnapshot(ctx context.Context) (dto.DeskSnapshot, error) {
	var v dto.DeskSnapshot
	e := w.call(ctx, "desk.snapshot", struct{}{}, &v)
	return v, e
}
func (w *Worker) DeskConfigure(ctx context.Context, r dto.DeskConfiguration) (dto.DeskSnapshot, error) {
	var v dto.DeskSnapshot
	e := w.call(ctx, "desk.configure", r, &v)
	return v, e
}
func (w *Worker) DeskPause(ctx context.Context) error {
	return w.call(ctx, "desk.pause", struct{}{}, nil)
}
func (w *Worker) DeskRoutes(ctx context.Context, r dto.RouteQuery) (dto.RoutePage, error) {
	var v dto.RoutePage
	e := w.call(ctx, "desk.routes", r, &v)
	return v, e
}
