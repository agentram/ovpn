package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDockerServiceOperatorRestartBranches(t *testing.T) {
	t.Parallel()

	if err := (*dockerServiceOperator)(nil).Restart(context.Background(), "xray"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected nil operator error, got %v", err)
	}

	op := newDockerServiceOperator("/tmp/docker.sock")
	op.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/containers/ovpn-xray/restart" || req.URL.Query().Get("t") != "10" {
			t.Fatalf("unexpected docker request: %s %s", req.Method, req.URL.String())
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	if err := op.Restart(context.Background(), " XRAY "); err != nil {
		t.Fatalf("restart xray: %v", err)
	}
	if err := op.Restart(context.Background(), "unknown"); err == nil || !strings.Contains(err.Error(), "not restartable") {
		t.Fatalf("expected unknown service error, got %v", err)
	}

	op.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("boom")), Header: make(http.Header)}, nil
	})}
	if err := op.Restart(context.Background(), "grafana"); err == nil || !strings.Contains(err.Error(), "docker restart failed") {
		t.Fatalf("expected docker status error, got %v", err)
	}

	op.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("socket closed")
	})}
	if err := op.Restart(context.Background(), "grafana"); err == nil || !strings.Contains(err.Error(), "socket closed") {
		t.Fatalf("expected docker transport error, got %v", err)
	}
}

func TestTelegramHTTPTransportHelpers(t *testing.T) {
	t.Parallel()

	if got := compactIPs([]string{" 1.1.1.1 ", "", "1.1.1.1", "2.2.2.2"}); len(got) != 2 || got[0] != "1.1.1.1" || got[1] != "2.2.2.2" {
		t.Fatalf("unexpected compact IPs: %+v", got)
	}
	client := newTelegramHTTPClient(slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"149.154.167.220"})
	if client == nil || client.Timeout != 45*time.Second || client.Transport == nil {
		t.Fatalf("unexpected telegram http client: %+v", client)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	_, err := dialTelegramWithFallback(ctx, nil, &net.Dialer{}, "tcp", "bad-address", []string{"127.0.0.1"})
	if err == nil {
		t.Fatalf("expected canceled dial error for non host-port address")
	}
}
