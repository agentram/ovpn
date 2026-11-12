package app

import (
	"context"
	"errors"

	"ovpn/internal/operations"
)

func (s *DesktopService) ControlReady() bool { return s.control != nil }

// AttachControl injects the management adapters at the CLI composition root.
func (s *DesktopService) AttachControl(ctx context.Context, backend operations.Backend) {
	s.control = operations.New(ctx, s.DataDir, backend)
	s.control.OnFinish = func(plan operations.Plan) {
		s.monitor.Invalidate()
		switch plan.Action {
		case "server.ha-apply", "restart", "deploy", "server.init", "server.restore", "user.quota-reset":
			nodes := map[string]bool{}
			for _, target := range plan.Targets {
				nodes[target.ID] = true
			}
			s.monitor.InvalidateRoutes(nodes, plan.Request.Values["username"])
		}
	}
	if source, ok := backend.(TelemetrySource); ok {
		s.telemetry = source
	}
}
func controlError(err error) error {
	if err == nil {
		return nil
	}
	for _, safe := range []error{operations.ErrBusy, operations.ErrStale, operations.ErrInvalid, operations.ErrUnavailable} {
		if errors.Is(err, safe) {
			return &Error{Code: safe.Error()}
		}
	}
	switch err.Error() {
	case "HA_BACKENDS_REQUIRED", "HA_BACKENDS_INCOMPATIBLE", "NO_ENABLED_SERVERS", "IDENTITY_CONFLICT", "USER_ASSIGNMENT_CONFLICT", "SSH_HOST_UNREACHABLE", "SSH_ACCESS_FAILED", "SSH_HOST_KEY_CHANGED", "DELIVERY_UNAVAILABLE":
		return &Error{Code: err.Error()}
	}
	return &Error{Code: "OPERATION_UNAVAILABLE"}
}
func (s *DesktopService) Catalog(context.Context) ([]operations.Action, error) {
	if s.control == nil {
		return nil, &Error{Code: "OPERATION_UNAVAILABLE"}
	}
	return s.control.Catalog(), nil
}
func (s *DesktopService) OperationPlan(ctx context.Context, r operations.Request) (operations.Plan, error) {
	if s.control == nil {
		return operations.Plan{}, &Error{Code: "OPERATION_UNAVAILABLE"}
	}
	v, e := s.control.Plan(ctx, r)
	return v, controlError(e)
}
func (s *DesktopService) OperationStart(ctx context.Context, r operations.Start) (operations.Job, error) {
	if s.control == nil {
		return operations.Job{}, &Error{Code: "OPERATION_UNAVAILABLE"}
	}
	v, e := s.control.Start(ctx, r)
	return v, controlError(e)
}
func (s *DesktopService) OperationList(context.Context) ([]operations.Job, error) {
	if s.control == nil {
		return nil, &Error{Code: "OPERATION_UNAVAILABLE"}
	}
	v, e := s.control.List()
	return v, controlError(e)
}
func (s *DesktopService) OperationResume(_ context.Context, id string) (operations.Job, error) {
	if s.control == nil {
		return operations.Job{}, &Error{Code: "OPERATION_UNAVAILABLE"}
	}
	v, e := s.control.Resume(id)
	return v, controlError(e)
}
func (s *DesktopService) OperationCancel(_ context.Context, id string) error {
	if s.control == nil {
		return &Error{Code: "OPERATION_UNAVAILABLE"}
	}
	return controlError(s.control.Cancel(id))
}
func (s *DesktopService) OperationResult(_ context.Context, id string) (operations.Result, error) {
	if s.control == nil {
		return operations.Result{}, &Error{Code: "OPERATION_UNAVAILABLE"}
	}
	v, e := s.control.Result(id)
	return v, controlError(e)
}
