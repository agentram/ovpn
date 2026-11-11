// Package operations coordinates reviewed, durable management operations.
package operations

import (
	"context"

	"errors"
	"ovpn/internal/operations/api"
)

var ErrBusy = errors.New("WORKSPACE_BUSY")
var ErrStale = errors.New("STALE_PLAN")
var ErrInvalid = errors.New("INVALID_PARAMS")
var ErrUnavailable = errors.New("OPERATION_UNAVAILABLE")

type Field = api.Field
type Action = api.Action
type Request = api.Request
type Target = api.Target
type Plan = api.Plan
type Start = api.Start
type Step = api.Step
type Job = api.Job
type Result = api.Result

// Backend implements control use cases. It never parses terminal output for results.
type Backend interface {
	Catalog() []Action
	Prepare(context.Context, Request) (Plan, error)
	Revision(context.Context) (string, error)
	Backup(context.Context, Plan, Target) error
	Apply(context.Context, Plan, Target, bool) (Result, error)
}

// TargetLocker excludes simultaneous writers controlling the same remote node.
type TargetLocker interface {
	Lock(context.Context, Plan, Target) (context.Context, func(), error)
}

// ResumeGuard verifies that persisted intent still permits reconciliation.
type ResumeGuard interface {
	CanResume(context.Context, Plan) error
}
