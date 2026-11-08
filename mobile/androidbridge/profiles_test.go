package androidbridge

import (
	"encoding/base64"
	"strings"
	"testing"

	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

func fixtureLink(transport string) string {
	return xraycfg.BuildVLESSLink(xraycfg.LinkInput{
		Profile: transport, Address: "vpn.example.test", ServerName: "sni.example.test",
		UUID: "389fa2c5-8d43-47fc-a223-1bd35ed2f02b", ShortID: "ab0123456789abcd",
		Password:    base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdefghijklmnopqrstuv")),
		Fingerprint: "chrome", Label: "Example node",
	})
}

func TestOVPNImports(t *testing.T) {
	tls := fixtureLink(model.TransportProfileTLSSelfSNIWeb)
	reality := fixtureLink(model.TransportProfileRealityTCPVision)
	plain := fixtureLink(model.TransportProfilePlainXHTTP)
	mixed := strings.Join([]string{tls, reality, plain}, "\r\n")
	for _, input := range []string{mixed, base64.StdEncoding.EncodeToString([]byte(mixed)), base64.RawStdEncoding.EncodeToString([]byte(mixed))} {
		result := inspect(input)
		if result.Issue != "" || len(result.Profiles) != 3 {
			t.Fatalf("unexpected import result: issue=%q rows=%d", result.Issue, len(result.Profiles))
		}
		for i, want := range []string{"tls", "reality", "xhttp"} {
			if result.Profiles[i].Transport != want {
				t.Fatalf("row %d transport = %q, want %q", i, result.Profiles[i].Transport, want)
			}
		}
		if row := result.Profiles[2]; row.Issue != "xhttp_unencrypted" || row.Link != "" {
			t.Fatal("unprotected XHTTP must remain incompatible")
		}
	}
}

func TestEffectiveIdentityAndRejectedChanges(t *testing.T) {
	valid := fixtureLink(model.TransportProfileTLSSelfSNIWeb)
	first, _ := parseLink(valid)
	renamed, _ := parseLink(strings.Split(valid, "#")[0] + "#Renamed")
	rekeyed, _ := parseLink(strings.Replace(valid, "389fa2c5", "516c2e13", 1))
	if first.ID == "" || first.ID != renamed.ID || first.ID == rekeyed.ID {
		t.Fatal("identity must depend on connection settings and ignore labels")
	}
	for _, test := range []struct{ name, input, want string }{
		{"body limit", strings.Repeat("a", maxImportBytes+1), "too_large"},
		{"profile limit", strings.Repeat(valid+"\n", maxProfiles+1), "too_many"},
		{"truncated link", valid + "\nvless://broken", "invalid_profile"},
		{"duplicate parameter", strings.Replace(valid, "type=tcp", "type=tcp&type=xhttp", 1), "invalid_profile"},
		{"invalid REALITY key", strings.Replace(fixtureLink(model.TransportProfileRealityTCPVision),
			base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdefghijklmnopqrstuv")), "broken", 1), "invalid_profile"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := inspect(test.input); got.Issue != test.want || len(got.Profiles) != 0 {
				t.Fatalf("issue=%q rows=%d, want %q and zero rows", got.Issue, len(got.Profiles), test.want)
			}
		})
	}
	for _, suffix := range []string{"&allowInsecure=1", "&encryption=none"} {
		modified := strings.Replace(valid, "#", suffix+"#", 1)
		if got, _ := parseLink(modified); got.Link != "" || got.ID != "" {
			t.Fatal("TLS overrides and duplicate encryption must not be accepted")
		}
	}
}
