package desktop

import (
	"context"
	operations "ovpn/internal/operations/api"
)

func (w *Worker) Catalog(ctx context.Context) ([]operations.Action, error) {
	var v []operations.Action
	e := w.call(ctx, "operations.catalog", struct{}{}, &v)
	return v, e
}
func (w *Worker) OperationPlan(ctx context.Context, r operations.Request) (operations.Plan, error) {
	var v operations.Plan
	e := w.call(ctx, "operations.plan", r, &v)
	return v, e
}
func (w *Worker) OperationStart(ctx context.Context, r operations.Start) (operations.Job, error) {
	var v operations.Job
	e := w.call(ctx, "operations.start", r, &v)
	return v, e
}
func (w *Worker) OperationList(ctx context.Context) ([]operations.Job, error) {
	var v []operations.Job
	e := w.call(ctx, "operations.list", struct{}{}, &v)
	return v, e
}
func (w *Worker) OperationResume(ctx context.Context, id string) (operations.Job, error) {
	var v operations.Job
	e := w.call(ctx, "operations.resume", map[string]string{"id": id}, &v)
	return v, e
}
func (w *Worker) OperationCancel(ctx context.Context, id string) error {
	return w.call(ctx, "operations.cancel", map[string]string{"id": id}, nil)
}
func (w *Worker) OperationResult(ctx context.Context, id string) (operations.Result, error) {
	var v operations.Result
	e := w.call(ctx, "operations.result", map[string]string{"id": id}, &v)
	return v, e
}
