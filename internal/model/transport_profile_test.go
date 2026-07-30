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
		"xhttp":         "",
		"plain-xhttp":   TransportProfilePlainXHTTP,
		"self-sni":      TransportProfileTLSSelfSNIWeb,
		"vless-tls":     TransportProfileTLSSelfSNIWeb,
		"unknown":       "",
	}
	for raw, want := range cases {
		if got := NormalizeTransportProfile(raw); got != want {
			t.Fatalf("NormalizeTransportProfile(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeEnabledProfilesDropsRemovedNamesAndKeepsKnownOrder(t *testing.T) {
	t.Parallel()

	got := NormalizeEnabledProfiles(TransportProfilePlainXHTTP, "vless-reality-xhttp,tcp,plain-xhttp,xhttp")
	want := []string{
		TransportProfileRealityTCPVision,
		TransportProfilePlainXHTTP,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeEnabledProfiles = %#v, want %#v", got, want)
	}
}

func TestRemovedTransportProfilesAreNotSupported(t *testing.T) {
	t.Parallel()

	for _, removed := range []string{
		"vless-reality-xhttp",
		"vless-ws-tls-web",
	} {
		if got := NormalizeTransportProfile(removed); got != "" {
			t.Fatalf("removed profile %q normalized to %q", removed, got)
		}
		if _, ok := LookupTransportProfile(removed); ok {
			t.Fatalf("removed profile %q is still supported", removed)
		}
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
