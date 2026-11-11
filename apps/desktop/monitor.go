package main

import (
	"context"
	"ovpn/internal/app/dto"
	"time"
)

func (s *DeskService) DeskSnapshot(ctx context.Context) (dto.DeskSnapshot, error) {
	w, e := s.connect()
	if e != nil {
		return dto.DeskSnapshot{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.DeskSnapshot(ctx)
}
func (s *DeskService) DeskConfigure(ctx context.Context, r dto.DeskConfiguration) (dto.DeskSnapshot, error) {
	w, e := s.connect()
	if e != nil {
		return dto.DeskSnapshot{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.DeskConfigure(ctx, r)
}
func (s *DeskService) DeskPause(ctx context.Context) error {
	w, e := s.connect()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.DeskPause(ctx)
}
func (s *DeskService) DeskRoutes(ctx context.Context, r dto.RouteQuery) (dto.RoutePage, error) {
	w, e := s.connect()
	if e != nil {
		return dto.RoutePage{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.DeskRoutes(ctx, r)
}
