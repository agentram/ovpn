package xraycfg

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestRenderProxySelfSNIPreservesRelayAndSplitRouting(t *testing.T) {
	t.Parallel()
	for _, preset := range []string{model.ProxyPresetRU, model.ProxyPresetCN} {
		t.Run(preset, func(t *testing.T) {
			spec := Spec{
				Role: model.ServerRoleProxy, ProxyPreset: preset, Domain: "proxy.example.net",
				EnabledProfiles: []string{model.TransportProfileTLSSelfSNIWeb, model.TransportProfilePlainXHTTP},
				Users:           []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "alice@example.net", Enabled: true}},
				ProxyRelay: &ProxyRelay{
					Address: "haproxy", Port: 15443, ServiceUUID: "22222222-2222-2222-2222-222222222222",
					ServerName: "backend.example.net", PublicKey: "backend-public-key", ShortID: "abcd1234",
				},
			}
			raw, err := RenderServerJSON(spec)
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Inbounds []struct {
					Tag      string
					Port     int
					Settings struct {
						Clients   []map[string]any
						Fallbacks []struct{ Dest string }
					}
					StreamSettings struct {
						Security    string
						TLSSettings struct{ Certificates []map[string]string }
					}
				}
				Outbounds []struct {
					Tag            string
					StreamSettings struct{ Security string }
				}
				Routing struct {
					Rules []struct {
						InboundTag  []string
						OutboundTag string
					}
				}
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatal(err)
			}
			foundTLS, foundRelay, foundDirect := false, false, false
			for _, inbound := range cfg.Inbounds {
				if inbound.Tag == "vless-reality" {
					t.Fatal("proxy still has the conflicting REALITY listener")
				}
				if inbound.Tag != model.TransportProfileTLSSelfSNIWeb {
					continue
				}
				foundTLS = true
				if inbound.Port != 443 || inbound.StreamSettings.Security != "tls" || len(inbound.Settings.Clients) != 1 {
					t.Fatalf("invalid TLS client inbound: %+v", inbound)
				}
				if len(inbound.Settings.Fallbacks) != 1 || inbound.Settings.Fallbacks[0].Dest != DefaultTLSSelfSNIFallbackDest {
					t.Fatalf("missing website fallback: %+v", inbound.Settings)
				}
				certs := inbound.StreamSettings.TLSSettings.Certificates
				if len(certs) != 1 || certs[0]["certificateFile"] != DefaultTLSSelfSNICertFile || certs[0]["keyFile"] != DefaultTLSSelfSNIKeyFile {
					t.Fatalf("missing TLS certificate paths: %+v", certs)
				}
			}
			for _, outbound := range cfg.Outbounds {
				if outbound.Tag == "foreign-pool" && outbound.StreamSettings.Security == "reality" {
					foundRelay = true
				}
			}
			for _, rule := range cfg.Routing.Rules {
				if rule.OutboundTag == "direct" {
					foundDirect = true
				}
			}
			last := cfg.Routing.Rules[len(cfg.Routing.Rules)-1]
			if last.OutboundTag != "foreign-pool" || !reflect.DeepEqual(last.InboundTag, spec.EnabledProfiles) {
				t.Fatalf("client profiles must retain the relay catch-all: %+v", last)
			}
			if !foundTLS || !foundRelay || !foundDirect {
				t.Fatalf("missing TLS, REALITY relay or direct routing: %t/%t/%t", foundTLS, foundRelay, foundDirect)
			}

			spec.Domain = ""
			if _, err := RenderServerJSON(spec); err == nil || !strings.Contains(err.Error(), "requires server domain") {
				t.Fatalf("proxy self-SNI must require a domain: %v", err)
			}
			spec.Domain = "proxy.example.net"
			spec.ProxyRelay = nil
			if _, err := RenderServerJSON(spec); err == nil || !strings.Contains(err.Error(), "proxy relay is required") {
				t.Fatalf("self-SNI must not bypass proxy topology validation: %v", err)
			}
		})
	}
}
