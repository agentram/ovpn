package main

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"ovpn/internal/xrayapi"
)

type runtimeGateway struct {
	apiAddr  string
	mu       *sync.Mutex
	logger   *slog.Logger
	observer *agentMetrics
}

// AddUser applies user and returns an error on failure.
func (g *runtimeGateway) AddUser(ctx context.Context, inboundTag, email, uuid string) error {
	if strings.TrimSpace(inboundTag) == "" {
		inboundTag = "vless-reality"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	client, err := xrayapi.New(ctx, g.apiAddr)
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

// RemoveUser applies user and returns an error on failure.
func (g *runtimeGateway) RemoveUser(ctx context.Context, inboundTag, email string) error {
	if strings.TrimSpace(inboundTag) == "" {
		inboundTag = "vless-reality"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	client, err := xrayapi.New(ctx, g.apiAddr)
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
