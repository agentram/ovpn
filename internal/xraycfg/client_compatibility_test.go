package xraycfg

import (
	"encoding/json"
	"net/url"
	"testing"

	"ovpn/internal/model"
)

func TestRealityMinimumClientVersionOnlyAppliesToServerInbound(t *testing.T) {
	for _, role := range []string{model.ServerRoleVPN, model.ServerRoleProxy} {
		for _, version := range []string{"", "1.0.0", "26.3.27"} {
			t.Run(role+"/"+version, func(t *testing.T) {
				spec := Spec{
					Role: role, RealityPrivateKey: "private", RealityServerName: "target.example.net",
					RealityTarget: "target.example.net:443", ShortIDs: []string{"abcd"},
					RealityMinClientVersion: version,
				}
				if role == model.ServerRoleProxy {
					spec.ProxyPreset = model.ProxyPresetRU
					spec.ProxyRelay = &ProxyRelay{Address: "haproxy", Port: 15443, ServiceUUID: "service", ServerName: "backend.example.net", PublicKey: "public", ShortID: "abcd"}
				}
				raw, err := RenderServerJSON(spec)
				if err != nil {
					t.Fatal(err)
				}
				var cfg struct{ Inbounds, Outbounds []map[string]any }
				if err := json.Unmarshal(raw, &cfg); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, inbound := range cfg.Inbounds {
					if inbound["tag"] != "vless-reality" {
						continue
					}
					found = true
					r := inbound["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
					got, present := r["minClientVer"]
					if present != (version != "") || (present && got != version) {
						t.Fatalf("minClientVer=%v, present=%v, version=%q", got, present, version)
					}
				}
				if !found {
					t.Fatal("REALITY inbound missing")
				}
				for _, outbound := range cfg.Outbounds {
					if settings, ok := outbound["streamSettings"].(map[string]any); ok {
						if r, ok := settings["realitySettings"].(map[string]any); ok {
							if _, present := r["minClientVer"]; present {
								t.Fatal("server version policy leaked into client relay settings")
							}
						}
					}
				}
			})
		}
	}
	spec := Spec{RealityPrivateKey: "private", RealityServerName: "target.example.net", RealityTarget: "target.example.net:443", ShortIDs: []string{"abcd"}, RealityMinClientVersion: "256.0.0"}
	if _, err := RenderServerJSON(spec); err == nil {
		t.Fatal("invalid client version reached rendered config")
	}
}

func TestClientLinksRoundTripIPv6AndEscapedLabels(t *testing.T) {
	const label = "alice 100% # &/? тест"
	for _, profile := range model.SupportedTransportProfiles() {
		for _, address := range []string{"vpn.example.net", "192.0.2.1", "2001:db8::1", "[2001:db8::1]"} {
			t.Run(profile.Name+"/"+address, func(t *testing.T) {
				link := BuildVLESSLink(LinkInput{Profile: profile.Name, Address: address, UUID: "11111111-1111-1111-1111-111111111111", ServerName: "target.example.net", Password: "public", ShortID: "abcd", Label: label})
				u, err := url.Parse(link)
				if err != nil {
					t.Fatal(err)
				}
				wantHost := address
				if address == "[2001:db8::1]" {
					wantHost = "2001:db8::1"
				}
				if u.Hostname() != wantHost || u.Port() == "" || u.Fragment != label {
					t.Fatalf("imported endpoint/label changed: hostname=%q port=%q label=%q", u.Hostname(), u.Port(), u.Fragment)
				}
			})
		}
	}
}
