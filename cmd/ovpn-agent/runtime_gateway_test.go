package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeRuntimeXrayClient struct {
	adds    []string
	removes []string
	addErr  error
	rmErr   error
}

func (c *fakeRuntimeXrayClient) Close() error { return nil }

func (c *fakeRuntimeXrayClient) AddUser(_ context.Context, inboundTag, email, uuid string) error {
	c.adds = append(c.adds, inboundTag+"|"+email+"|"+uuid)
	return c.addErr
}

func (c *fakeRuntimeXrayClient) RemoveUser(_ context.Context, inboundTag, email string) error {
	c.removes = append(c.removes, inboundTag+"|"+email)
	return c.rmErr
}

func TestRuntimeGatewayUsesFactoryDefaultsAndRecordsReachability(t *testing.T) {
	client := &fakeRuntimeXrayClient{}
	restore := replaceRuntimeXrayClient(func(context.Context, string) (runtimeXrayClient, error) {
		return client, nil
	})
	defer restore()

	metrics := newAgentMetrics(prometheus.NewRegistry())
	g := &runtimeGateway{apiAddr: "xray:10085", mu: &sync.Mutex{}, observer: metrics}
	if err := g.AddUser(context.Background(), "", "alice@global", "uuid-1"); err != nil {
		t.Fatalf("add user: %v", err)
	}
	if err := g.RemoveUser(context.Background(), "", "alice@global"); err != nil {
		t.Fatalf("remove user: %v", err)
	}
	if len(client.adds) != 1 || client.adds[0] != "vless-reality|alice@global|uuid-1" {
		t.Fatalf("unexpected adds: %+v", client.adds)
	}
	if len(client.removes) != 1 || client.removes[0] != "vless-reality|alice@global" {
		t.Fatalf("unexpected removes: %+v", client.removes)
	}
	if got := testutil.ToFloat64(metrics.xrayAPIReachable); got != 1 {
		t.Fatalf("expected reachable metric, got %v", got)
	}
}

func TestRuntimeGatewayReportsFactoryAndRuntimeErrors(t *testing.T) {
	metrics := newAgentMetrics(prometheus.NewRegistry())
	g := &runtimeGateway{apiAddr: "xray:10085", mu: &sync.Mutex{}, observer: metrics}

	restore := replaceRuntimeXrayClient(func(context.Context, string) (runtimeXrayClient, error) {
		return nil, errors.New("dial failed")
	})
	if err := g.AddUser(context.Background(), "tag", "alice@global", "uuid-1"); err == nil {
		t.Fatalf("expected factory add error")
	}
	restore()
	if got := testutil.ToFloat64(metrics.xrayAPIReachable); got != 0 {
		t.Fatalf("expected unreachable metric, got %v", got)
	}

	restore = replaceRuntimeXrayClient(func(context.Context, string) (runtimeXrayClient, error) {
		return &fakeRuntimeXrayClient{rmErr: errors.New("remove failed")}, nil
	})
	defer restore()
	if err := g.RemoveUser(context.Background(), "tag", "alice@global"); err == nil {
		t.Fatalf("expected remove error")
	}
}

func replaceRuntimeXrayClient(fn func(context.Context, string) (runtimeXrayClient, error)) func() {
	prev := newRuntimeXrayClient
	newRuntimeXrayClient = fn
	return func() { newRuntimeXrayClient = prev }
}
