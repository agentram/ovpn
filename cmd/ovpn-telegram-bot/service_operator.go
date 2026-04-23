package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type serviceOperator interface {
	Restart(ctx context.Context, service string) error
}

type dockerServiceOperator struct {
	http       *http.Client
	containers map[string]string
}

func newDockerServiceOperator(socketPath string) *dockerServiceOperator {
	transport := &http.Transport{}
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socketPath)
	}
	return &dockerServiceOperator{
		http: &http.Client{Timeout: 12 * time.Second, Transport: transport},
		containers: map[string]string{
			"xray":          "ovpn-xray",
			"haproxy":       "ovpn-haproxy",
			"ovpn-agent":    "ovpn-agent",
			"prometheus":    "ovpn-prometheus",
			"alertmanager":  "ovpn-alertmanager",
			"grafana":       "ovpn-grafana",
			"node-exporter": "ovpn-node-exporter",
			"cadvisor":      "ovpn-cadvisor",
		},
	}
}

func (o *dockerServiceOperator) Restart(ctx context.Context, service string) error {
	if o == nil {
		return fmt.Errorf("service operator is not configured")
	}
	container, ok := o.containers[strings.TrimSpace(strings.ToLower(service))]
	if !ok {
		return fmt.Errorf("service %q is not restartable", service)
	}
	restartURL := &url.URL{Scheme: "http", Host: "docker", Path: path.Join("/containers", container, "restart")}
	q := restartURL.Query()
	q.Set("t", "10")
	restartURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, restartURL.String(), nil)
	if err != nil {
		return err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker restart failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
