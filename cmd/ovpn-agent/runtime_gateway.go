package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"ovpn/internal/xrayapi"
)

type runtimeXrayClient interface {
	Close() error
	AddUser(ctx context.Context, inboundTag, email, uuid string) error
	RemoveUser(ctx context.Context, inboundTag, email string) error
}

var newRuntimeXrayClient = func(ctx context.Context, apiAddr string) (runtimeXrayClient, error) {
	return xrayapi.New(ctx, apiAddr)
}

type runtimeGateway struct {
	apiAddr  string
	mu       *sync.Mutex
	logger   *slog.Logger
	observer *agentMetrics
}

func (g *runtimeGateway) AddUser(ctx context.Context, inboundTag, email, uuid string) error {
	if strings.TrimSpace(inboundTag) == "" {
		inboundTag = "vless-reality"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	client, err := newRuntimeXrayClient(ctx, g.apiAddr)
	if err != nil {
		g.observer.OnXrayAPIReachable(false)
		return err
	}
	defer client.Close()
	if err := client.AddUser(ctx, inboundTag, email, uuid); err != nil {
		return err
	}
	g.observer.OnXrayAPIReachable(true)
	return nil
}

func (g *runtimeGateway) RemoveUser(ctx context.Context, inboundTag, email string) error {
	if strings.TrimSpace(inboundTag) == "" {
		inboundTag = "vless-reality"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	client, err := newRuntimeXrayClient(ctx, g.apiAddr)
	if err != nil {
		g.observer.OnXrayAPIReachable(false)
		return err
	}
	defer client.Close()
	if err := client.RemoveUser(ctx, inboundTag, email); err != nil {
		return err
	}
	g.observer.OnXrayAPIReachable(true)
	return nil
}
