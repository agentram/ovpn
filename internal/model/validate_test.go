package model

import (
	"strings"
	"testing"
)

func TestServerValidate(t *testing.T) {
	t.Parallel()

	valid := Server{
		Name:              "main",
		Host:              "1.2.3.4",
		Domain:            "example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcdef01",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid server, got error: %v", err)
	}

	validProxy := valid
	validProxy.Role = ServerRoleProxy
	if err := validProxy.Validate(); err != nil {
		t.Fatalf("expected valid proxy server with default preset, got error: %v", err)
	}

	invalid := valid
	invalid.Name = ""
	invalid.Host = ""
	invalid.SSHPort = 70000
	invalid.RealityPrivateKey = ""
	invalid.RealityServerName = "*.example.com"
	if err := invalid.Validate(); err == nil {
		t.Fatalf("expected validation error")
	} else {
		for _, want := range []string{"name is required", "host is required", "ssh_port", "reality_private_key", "reality_server_name must not contain wildcard"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error to contain %q, got %q", want, err.Error())
			}
		}
	}

	invalidProxyPreset := validProxy
	invalidProxyPreset.ProxyPreset = "de"
	if err := invalidProxyPreset.Validate(); err == nil || !strings.Contains(err.Error(), "proxy_preset") {
		t.Fatalf("expected proxy preset validation error, got %v", err)
	}

	invalidVPNPreset := valid
	invalidVPNPreset.ProxyPreset = "ru"
	if err := invalidVPNPreset.Validate(); err == nil || !strings.Contains(err.Error(), "only supported for proxy role") {
		t.Fatalf("expected vpn proxy_preset validation error, got %v", err)
	}
}

func TestUserValidate(t *testing.T) {
	t.Parallel()

	quota := int64(1024)
	valid := User{
		ServerID:         1,
		Username:         "alice",
		UUID:             "11111111-1111-1111-1111-111111111111",
		Email:            "alice@example.com",
		TrafficLimitByte: &quota,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid user, got error: %v", err)
	}

	badQuota := int64(-1)
	invalid := valid
	invalid.ServerID = 0
	invalid.Email = "bad-email"
	invalid.TrafficLimitByte = &badQuota
	if err := invalid.Validate(); err == nil {
		t.Fatalf("expected validation error")
	} else {
		for _, want := range []string{"server_id is required", "email is invalid", "traffic_limit_byte"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error to contain %q, got %q", want, err.Error())
			}
		}
	}
}
