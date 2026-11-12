package main

import (
	"context"
	operations "ovpn/internal/operations/api"
	"time"
)

func (s *DeskService) Catalog(ctx context.Context) ([]operations.Action, error) {
	w, e := s.connect()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.Catalog(ctx)
}
func (s *DeskService) OperationPlan(ctx context.Context, r operations.Request) (operations.Plan, error) {
	w, e := s.connect()
	if e != nil {
		return operations.Plan{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.OperationPlan(ctx, r)
}
func (s *DeskService) OperationStart(ctx context.Context, r operations.Start) (operations.Job, error) {
	w, e := s.connect()
	if e != nil {
		return operations.Job{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.OperationStart(ctx, r)
}
func (s *DeskService) OperationList(ctx context.Context) ([]operations.Job, error) {
	w, e := s.connect()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.OperationList(ctx)
}
func (s *DeskService) OperationResume(ctx context.Context, id string) (operations.Job, error) {
	w, e := s.connect()
	if e != nil {
		return operations.Job{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.OperationResume(ctx, id)
}
func (s *DeskService) OperationCancel(ctx context.Context, id string) error {
	w, e := s.connect()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.OperationCancel(ctx, id)
}
func (s *DeskService) OperationResult(ctx context.Context, id string) (operations.Result, error) {
	w, e := s.connect()
	if e != nil {
		return operations.Result{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.OperationResult(ctx, id)
}
