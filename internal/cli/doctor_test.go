package cli

import (
	"strings"
	"testing"
)

func TestIsFloatingXrayTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tag      string
		floating bool
	}{
		{tag: "", floating: true},
		{tag: "latest", floating: true},
		{tag: "main", floating: true},
		{tag: "dev-tun0", floating: true},
		{tag: "26.3.27", floating: false},
		{tag: "v26.3.27", floating: false},
		{tag: "1.8.24", floating: false},
		{tag: "sha-f650d87", floating: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			if got := isFloatingXrayTag(tc.tag); got != tc.floating {
				t.Fatalf("isFloatingXrayTag(%q)=%v, want %v", tc.tag, got, tc.floating)
			}
		})
	}
}

func TestBuildDoctorDiskCommandUsesStatementSeparators(t *testing.T) {
	t.Parallel()

	cmd := buildDoctorDiskCommand()
	if strings.Contains(cmd, "do;") {
		t.Fatalf("did not expect shell control token 'do;' in command, got %q", cmd)
	}
	if !strings.Contains(cmd, "set -u\nfor p in / /var /opt/ovpn /var/lib/docker; do") {
		t.Fatalf("expected newline-separated preamble, got %q", cmd)
	}
}

func TestWithRemoteTimeoutWrapsCommand(t *testing.T) {
	t.Parallel()

	got := withRemoteTimeout(7, "echo ok")
	if !strings.Contains(got, "timeout 7 sh -c") {
		t.Fatalf("expected timeout wrapper, got %q", got)
	}
	if !strings.Contains(got, "echo ok") {
		t.Fatalf("expected wrapped command contents, got %q", got)
	}
}

func TestSplitRealityTargetHostPort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in       string
		wantHost string
		wantPort string
	}{
		{in: "www.microsoft.com:443", wantHost: "www.microsoft.com", wantPort: "443"},
		{in: "1.2.3.4:8443", wantHost: "1.2.3.4", wantPort: "8443"},
		{in: "[2001:db8::1]:443", wantHost: "2001:db8::1", wantPort: "443"},
		{in: "example.com", wantHost: "example.com", wantPort: ""},
		{in: "", wantHost: "", wantPort: ""},
	}

	for _, tc := range cases {
		host, port := splitRealityTargetHostPort(tc.in)
		if host != tc.wantHost || port != tc.wantPort {
			t.Fatalf("splitRealityTargetHostPort(%q)=(%q,%q), want (%q,%q)", tc.in, host, port, tc.wantHost, tc.wantPort)
		}
	}
}
