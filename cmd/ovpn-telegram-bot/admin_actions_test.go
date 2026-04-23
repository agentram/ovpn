package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type noopServiceOperator struct{}

func (noopServiceOperator) Restart(context.Context, string) error { return nil }

func TestBeginRestartConfirmDisabled(t *testing.T) {
	t.Parallel()

	c := &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
		})},
	}
	b := &bot{tg: c, prompts: map[int64]promptState{}, confirms: map[int64]confirmState{}}
	if err := b.beginRestartConfirm(context.Background(), 11, "xray"); err != nil {
		t.Fatalf("beginRestartConfirm: %v", err)
	}
	if _, ok := b.getConfirm(11); ok {
		t.Fatalf("did not expect pending confirmation when admin is disabled")
	}
}

func TestBeginRestartConfirmSetsPending(t *testing.T) {
	t.Parallel()

	c := &telegramClient{
		token: "token",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{}}`)), Header: make(http.Header)}, nil
		})},
	}
	b := &bot{
		tg:         c,
		adminToken: "enabled",
		operator:   noopServiceOperator{},
		prompts:    map[int64]promptState{},
		confirms:   map[int64]confirmState{},
	}
	if err := b.beginRestartConfirm(context.Background(), 11, "ovpn-agent"); err != nil {
		t.Fatalf("beginRestartConfirm: %v", err)
	}
	st, ok := b.getConfirm(11)
	if !ok {
		t.Fatalf("expected pending confirmation")
	}
	if st.Kind != "restart" || len(st.Services) != 1 || st.Services[0] != "ovpn-agent" {
		t.Fatalf("unexpected pending state: %+v", st)
	}
}

func TestNewDockerServiceOperatorIncludesHAProxy(t *testing.T) {
	t.Parallel()

	op := newDockerServiceOperator("/var/run/docker.sock")
	if got := op.containers["haproxy"]; got != "ovpn-haproxy" {
		t.Fatalf("unexpected haproxy container mapping: %q", got)
	}
}
