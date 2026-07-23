package model

import (
	"reflect"
	"testing"
)

func TestNormalizeTransportProfileAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":              TransportProfileRealityTCPVision,
		"default":       TransportProfileRealityTCPVision,
		"tcp":           TransportProfileRealityTCPVision,
		"vless-reality": TransportProfileRealityTCPVision,
		"xhttp":         TransportProfileRealityXHTTP,
		"plain-xhttp":   TransportProfilePlainXHTTP,
		"self-sni":      TransportProfileTLSSelfSNIWeb,
		"vless-tls":     TransportProfileTLSSelfSNIWeb,
		"ws":            TransportProfileWSTLSWeb,
		"unknown":       "",
	}
	for raw, want := range cases {
		if got := NormalizeTransportProfile(raw); got != want {
			t.Fatalf("NormalizeTransportProfile(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeEnabledProfilesKeepsPrimaryAndKnownOrder(t *testing.T) {
	t.Parallel()

	got := NormalizeEnabledProfiles(TransportProfilePlainXHTTP, "xhttp,tcp,plain-xhttp,xhttp")
	want := []string{
		TransportProfileRealityTCPVision,
		TransportProfileRealityXHTTP,
		TransportProfilePlainXHTTP,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeEnabledProfiles = %#v, want %#v", got, want)
	}
}

func TestServerTransportProfileDefaults(t *testing.T) {
	t.Parallel()

	var srv Server
	if got := srv.NormalizedPrimaryProfile(); got != TransportProfileRealityTCPVision {
		t.Fatalf("default primary = %q", got)
	}
	if !srv.IsTransportProfileEnabled(TransportProfileRealityTCPVision) {
		t.Fatalf("default server should enable tcp reality profile")
	}
}
