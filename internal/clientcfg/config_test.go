package clientcfg

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestFromLinkPreservesClientTransports(t *testing.T) {
	for _, tc := range []struct{ name, query, security, network string }{
		{"reality", "security=reality&type=tcp&encryption=none&flow=xtls-rprx-vision&sni=target.example.net&pbk=public-key&sid=aabb&fp=firefox&spx=%2Ftest", "reality", "tcp"},
		{"tls", "security=tls&type=tcp&encryption=none&flow=xtls-rprx-vision&sni=vpn.example.net&fp=firefox&alpn=h2%2Chttp%2F1.1", "tls", "tcp"},
		{"xhttp", "security=none&type=xhttp&encryption=none&path=%2F&mode=auto", "none", "xhttp"},
		{"vlessenc", "security=none&type=xhttp&encryption=" + url.QueryEscape("mlkem768x25519plus.native.0rtt.client-key") + "&path=%2F&mode=auto", "none", "xhttp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := FromLink("vless://11111111-1111-1111-1111-111111111111@[2001:db8::1]:443?" + tc.query + "#test")
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Outbounds) != 1 || cfg.Outbounds[0]["protocol"] != "vless" {
				t.Fatal("expected one VLESS outbound")
			}
			stream := cfg.Outbounds[0]["streamSettings"].(map[string]any)
			if stream["security"] != tc.security || stream["network"] != tc.network {
				t.Fatalf("wrong stream: %#v", stream)
			}
			target := cfg.Outbounds[0]["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)
			if target["address"] != "2001:db8::1" || target["port"] != 443 {
				t.Fatal("endpoint changed")
			}
			query, _ := url.ParseQuery(tc.query)
			user := target["users"].([]any)[0].(map[string]any)
			if user["id"] != "11111111-1111-1111-1111-111111111111" || user["encryption"] != query.Get("encryption") || cfg.Remarks != "test" {
				t.Fatal("client identity, encryption or remark changed")
			}
			if flow := query.Get("flow"); flow != "" && user["flow"] != flow {
				t.Fatal("Vision flow was lost")
			}
			if tc.security == "reality" {
				reality := stream["realitySettings"].(map[string]any)
				for key, parameter := range map[string]string{"serverName": "sni", "password": "pbk", "shortId": "sid", "fingerprint": "fp", "spiderX": "spx"} {
					if reality[key] != query.Get(parameter) {
						t.Fatalf("REALITY %s changed", key)
					}
				}
			}
			if tc.security == "tls" {
				tls := stream["tlsSettings"].(map[string]any)
				if tls["serverName"] != query.Get("sni") || tls["fingerprint"] != query.Get("fp") || strings.Join(tls["alpn"].([]string), ",") != query.Get("alpn") {
					t.Fatal("TLS client parameters changed")
				}
				if _, insecure := tls["allowInsecure"]; insecure {
					t.Fatal("TLS verification disabled")
				}
			}
			if tc.network == "xhttp" {
				xhttp := stream["xhttpSettings"].(map[string]any)
				if xhttp["path"] != query.Get("path") || xhttp["mode"] != query.Get("mode") {
					t.Fatal("XHTTP client parameters changed")
				}
			}
			raw, err := cfg.JSON()
			if err != nil || !json.Valid(raw) || strings.Contains(string(raw), "inbounds") || strings.Contains(string(raw), "freedom") {
				t.Fatal("config must have no listener or direct fallback")
			}
		})
	}
}

func TestFromLinkRejectsMalformedInputWithoutEchoingCredentials(t *testing.T) {
	for _, link := range []string{
		"vless://sensitive-secret@vpn.example.net:443?type=tcp&security=reality",
		"vless://11111111-1111-1111-1111-111111111111@vpn.example.net:0?type=xhttp&security=none&encryption=none",
		"vless://11111111-1111-1111-1111-111111111111@vpn.example.net:443?type=tcp&security=none&encryption=none",
		"vless://11111111-1111-1111-1111-111111111111@vpn.example.net:443?type=tcp&security=reality&encryption=none",
	} {
		_, err := FromLink(link)
		if err == nil || strings.Contains(err.Error(), "sensitive-secret") || strings.Contains(err.Error(), "vless://") {
			t.Fatalf("expected a credential-free validation error, got %v", err)
		}
	}
}
