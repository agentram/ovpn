package main

import (
	"strings"
	"testing"
)

func TestNormalizeServiceName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"xray":          "xray",
		"haproxy":       "haproxy",
		"agent":         "ovpn-agent",
		"ovpn-agent":    "ovpn-agent",
		"prom":          "prometheus",
		"alert":         "alertmanager",
		"node_exporter": "node-exporter",
		"cadvisor":      "cadvisor",
	}
	for in, want := range cases {
		got, ok := normalizeServiceName(in)
		if !ok {
			t.Fatalf("expected %q to be valid", in)
		}
		if got != want {
			t.Fatalf("normalizeServiceName(%q)=%q want=%q", in, got, want)
		}
	}
	if _, ok := normalizeServiceName("unknown"); ok {
		t.Fatalf("unknown service should be invalid")
	}
}

func TestRestartableServicesHelp(t *testing.T) {
	t.Parallel()

	if got := restartableServicesHelp(false); strings.Contains(got, "haproxy") {
		t.Fatalf("unexpected haproxy in non-proxy restart help: %q", got)
	}
	if got := restartableServicesHelp(true); !strings.Contains(got, "haproxy") {
		t.Fatalf("expected haproxy in proxy restart help: %q", got)
	}
}

func TestComputeOverallWithCriticalFailure(t *testing.T) {
	t.Parallel()
	s := auditSnapshot{
		Services: []serviceCheck{
			{Key: "ovpn-agent", Healthy: false, Critical: true},
			{Key: "grafana", Healthy: false, Critical: false},
		},
	}
	if got := computeOverall(s); got != "FAIL" {
		t.Fatalf("expected FAIL, got %s", got)
	}
}

func TestComputeOverallWithWarningsOnly(t *testing.T) {
	t.Parallel()
	s := auditSnapshot{
		CollectorState: "stale",
		Services: []serviceCheck{
			{Key: "ovpn-agent", Healthy: true, Critical: true},
			{Key: "grafana", Healthy: true, Critical: false},
		},
	}
	if got := computeOverall(s); got != "WARN" {
		t.Fatalf("expected WARN, got %s", got)
	}
}
