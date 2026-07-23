package xraycfg

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestRenderServerJSONIncludesRequiredSections(t *testing.T) {
	b, err := RenderServerJSON(Spec{
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd1234"},
		Users:             []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "u@example.com", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	api, ok := obj["api"].(map[string]any)
	if !ok {
		t.Fatalf("api missing")
	}
	svcs, _ := api["services"].([]any)
	all := strings.Join(toStrings(svcs), ",")
	if !strings.Contains(all, "StatsService") || !strings.Contains(all, "HandlerService") {
		t.Fatalf("api services missing: %v", all)
	}
	dns, ok := obj["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns missing in minimal profile")
	}
	servers, _ := dns["servers"].([]any)
	if len(servers) == 0 {
		t.Fatalf("dns servers missing in minimal profile")
	}
	if got := dns["queryStrategy"]; got != "UseIPv4" {
		t.Fatalf("dns queryStrategy = %v, want UseIPv4", got)
	}
	routing, ok := obj["routing"].(map[string]any)
	if !ok {
		t.Fatalf("routing missing")
	}
	rules, _ := routing["rules"].([]any)
	if len(rules) < 2 {
		t.Fatalf("expected api + security rules, got %d", len(rules))
	}
	if got := routing["domainStrategy"]; got != "AsIs" {
		t.Fatalf("routing domainStrategy = %v, want AsIs", got)
	}
	assertNoIPv6BlockRule(t, rules)
	outbounds, ok := obj["outbounds"].([]any)
	if !ok {
		t.Fatalf("outbounds missing")
	}
	freedomTags := map[string]bool{}
	for _, outbound := range outbounds {
		ob, ok := outbound.(map[string]any)
		if !ok || ob["protocol"] != "freedom" {
			continue
		}
		tag, _ := ob["tag"].(string)
		if tag != "direct" && tag != "api" {
			continue
		}
		settings, _ := ob["settings"].(map[string]any)
		if got := settings["domainStrategy"]; got != "UseIPv4" {
			t.Fatalf("%s domainStrategy = %v, want UseIPv4", tag, got)
		}
		freedomTags[tag] = true
	}
	if !freedomTags["direct"] || !freedomTags["api"] {
		t.Fatalf("expected direct and api freedom outbounds, got %v", freedomTags)
	}
}

func TestRenderServerJSONIncludesAccessLogWhenConfigured(t *testing.T) {
	raw, err := RenderServerJSON(Spec{
		Domain:            "vpn.example.com",
		RealityPrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd"},
		AccessLogPath:     "/var/log/ovpn/xray-access.log",
		Users: []model.User{{
			Username: "alice",
			UUID:     "11111111-1111-1111-1111-111111111111",
			Email:    "alice@global",
			Enabled:  true,
		}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	logCfg := cfg["log"].(map[string]any)
	if got := logCfg["access"]; got != "/var/log/ovpn/xray-access.log" {
		t.Fatalf("unexpected access log path: %v", got)
	}
}

func TestBuildVLESSLink(t *testing.T) {
	link := BuildVLESSLink(LinkInput{
		Address:    "example.com",
		UUID:       "11111111-1111-1111-1111-111111111111",
		ServerName: "www.microsoft.com",
		Password:   "pubkey",
		ShortID:    "abcd",
		Label:      "ovpn user",
		SpiderX:    "/assets/user.js",
	})
	if !strings.HasPrefix(link, "vless://") {
		t.Fatalf("bad prefix: %s", link)
	}
	if !strings.Contains(link, "pbk=pubkey") {
		t.Fatalf("missing pbk")
	}
	if !strings.Contains(link, "#ovpn%20user") {
		t.Fatalf("label not escaped: %s", link)
	}
	if !strings.Contains(link, "fp=firefox") || !strings.Contains(link, "spx=%2Fassets%2Fuser.js") {
		t.Fatalf("missing hardened defaults: %s", link)
	}
}

func TestBuildVLESSLinkProfiles(t *testing.T) {
	t.Parallel()

	xhttp := BuildVLESSLink(LinkInput{
		Address:    "example.com",
		UUID:       "11111111-1111-1111-1111-111111111111",
		ServerName: "www.microsoft.com",
		Password:   "pubkey",
		ShortID:    "abcd",
		Profile:    model.TransportProfileRealityXHTTP,
		Label:      "ovpn xhttp",
		SpiderX:    "/xhttp-spider",
	})
	for _, want := range []string{":8443?", "fp=firefox", "type=xhttp", "path=%2Fovpn-xhttp", "mode=auto", "spx=%2Fxhttp-spider", "#ovpn%20xhttp"} {
		if !strings.Contains(xhttp, want) {
			t.Fatalf("xhttp link missing %q: %s", want, xhttp)
		}
	}
	if strings.Contains(xhttp, "flow=xtls-rprx-vision") {
		t.Fatalf("xhttp link should not include vision flow: %s", xhttp)
	}

	plainXHTTP := BuildVLESSLink(LinkInput{
		Address: "example.com",
		UUID:    "11111111-1111-1111-1111-111111111111",
		Profile: model.TransportProfilePlainXHTTP,
		Label:   "ovpn plain",
	})
	for _, want := range []string{":13179?", "security=none", "type=xhttp", "path=%2F", "mode=auto", "#ovpn%20plain"} {
		if !strings.Contains(plainXHTTP, want) {
			t.Fatalf("plain xhttp link missing %q: %s", want, plainXHTTP)
		}
	}
	if strings.Contains(plainXHTTP, "pbk=") || strings.Contains(plainXHTTP, "sni=") || strings.Contains(plainXHTTP, "sid=") {
		t.Fatalf("plain xhttp link should not include REALITY params: %s", plainXHTTP)
	}

	tlsSelfSNI := BuildVLESSLink(LinkInput{
		Address:    "example.com",
		UUID:       "11111111-1111-1111-1111-111111111111",
		ServerName: "example.com",
		Profile:    model.TransportProfileTLSSelfSNIWeb,
		Label:      "ovpn tls",
	})
	for _, want := range []string{":443?", "security=tls", "type=tcp", "flow=xtls-rprx-vision", "sni=example.com", "alpn=http%2F1.1", "fp=firefox", "headerType=none", "#ovpn%20tls"} {
		if !strings.Contains(tlsSelfSNI, want) {
			t.Fatalf("tls self-sni link missing %q: %s", want, tlsSelfSNI)
		}
	}
	if strings.Contains(tlsSelfSNI, "pbk=") || strings.Contains(tlsSelfSNI, "sid=") {
		t.Fatalf("tls self-sni link should not include REALITY params: %s", tlsSelfSNI)
	}
}

func TestBuildVLESSLinkHonorsExplicitFingerprint(t *testing.T) {
	t.Parallel()

	link := BuildVLESSLink(LinkInput{
		Address:     "example.com",
		UUID:        "11111111-1111-1111-1111-111111111111",
		ServerName:  "www.microsoft.com",
		Password:    "pubkey",
		ShortID:     "abcd",
		Fingerprint: "chrome",
		SpiderX:     "/compat",
	})
	for _, want := range []string{"fp=chrome", "spx=%2Fcompat"} {
		if !strings.Contains(link, want) {
			t.Fatalf("explicit link missing %q: %s", want, link)
		}
	}
}

func TestRenderServerJSONIncludesEnabledTransportProfiles(t *testing.T) {
	t.Parallel()

	raw, err := RenderServerJSON(Spec{
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd1234"},
		EnabledProfiles: []string{
			model.TransportProfileRealityTCPVision,
			model.TransportProfileRealityXHTTP,
			model.TransportProfilePlainXHTTP,
		},
		Users: []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "u@example.com", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal rendered config: %v", err)
	}
	inbounds, _ := cfg["inbounds"].([]any)
	tags := map[string]map[string]any{}
	for _, rawInbound := range inbounds {
		inbound, _ := rawInbound.(map[string]any)
		tag, _ := inbound["tag"].(string)
		tags[tag] = inbound
	}
	if tags["vless-reality"] == nil || tags["vless-reality-xhttp"] == nil || tags["vless-xhttp-plain"] == nil {
		t.Fatalf("expected tcp, reality xhttp, and plain xhttp inbounds, got tags %#v", tags)
	}
	xhttpStream, _ := tags["vless-reality-xhttp"]["streamSettings"].(map[string]any)
	if got := xhttpStream["network"]; got != "xhttp" {
		t.Fatalf("xhttp network = %v", got)
	}
	xhttpSettings, _ := xhttpStream["xhttpSettings"].(map[string]any)
	if got := xhttpSettings["path"]; got != "/ovpn-xhttp" {
		t.Fatalf("xhttp path = %v", got)
	}
	plainStream, _ := tags["vless-xhttp-plain"]["streamSettings"].(map[string]any)
	if got := plainStream["security"]; got != "none" {
		t.Fatalf("plain xhttp security = %v", got)
	}
	plainXHTTPSettings, _ := plainStream["xhttpSettings"].(map[string]any)
	if got := plainXHTTPSettings["path"]; got != "/" {
		t.Fatalf("plain xhttp path = %v", got)
	}
}

func TestRenderServerJSONIncludesTLSSelfSNIWebFallback(t *testing.T) {
	t.Parallel()

	raw, err := RenderServerJSON(Spec{
		Domain:                 "example.com",
		SecurityProfile:        SecurityProfileMinimal,
		ThreatDNSServers:       []string{"9.9.9.9"},
		EnabledProfiles:        []string{model.TransportProfileTLSSelfSNIWeb},
		TLSSelfSNICertFile:     "/certs/fullchain.pem",
		TLSSelfSNIKeyFile:      "/certs/privkey.pem",
		TLSSelfSNIFallbackDest: "ovpn-web:8080",
		Users:                  []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "u@example.com", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal rendered config: %v", err)
	}
	inbounds, _ := cfg["inbounds"].([]any)
	var inbound map[string]any
	for _, rawInbound := range inbounds {
		candidate, _ := rawInbound.(map[string]any)
		if candidate["tag"] == "vless-tcp-tls-selfsni-web" {
			inbound = candidate
			break
		}
	}
	if inbound == nil {
		t.Fatalf("expected tls self-sni inbound in %v", inbounds)
	}
	if got := inbound["port"]; got != float64(443) {
		t.Fatalf("tls self-sni port = %v, want 443", got)
	}
	stream, _ := inbound["streamSettings"].(map[string]any)
	if got := stream["security"]; got != "tls" {
		t.Fatalf("tls self-sni security = %v", got)
	}
	tlsSettings, _ := stream["tlsSettings"].(map[string]any)
	certs, _ := tlsSettings["certificates"].([]any)
	if len(certs) != 1 {
		t.Fatalf("expected one certificate entry, got %v", certs)
	}
	cert, _ := certs[0].(map[string]any)
	if cert["certificateFile"] != "/certs/fullchain.pem" || cert["keyFile"] != "/certs/privkey.pem" {
		t.Fatalf("unexpected cert config: %v", cert)
	}
	settings, _ := inbound["settings"].(map[string]any)
	fallbacks, _ := settings["fallbacks"].([]any)
	if len(fallbacks) != 1 {
		t.Fatalf("expected one fallback, got %v", fallbacks)
	}
	fallback, _ := fallbacks[0].(map[string]any)
	if got := fallback["dest"]; got != "ovpn-web:8080" {
		t.Fatalf("fallback dest = %v, want ovpn-web:8080", got)
	}
	clients, _ := settings["clients"].([]any)
	client, _ := clients[0].(map[string]any)
	if got := client["flow"]; got != "xtls-rprx-vision" {
		t.Fatalf("tls self-sni client flow = %v", got)
	}
}

func TestValidateSpecRejectsTLSSelfSNIRealityPortConflict(t *testing.T) {
	t.Parallel()

	err := ValidateSpec(Spec{
		Domain:                 "example.com",
		RealityPrivateKey:      "priv",
		RealityServerName:      "www.microsoft.com",
		RealityTarget:          "www.microsoft.com:443",
		ShortIDs:               []string{"abcd"},
		ThreatDNSServers:       []string{"9.9.9.9"},
		EnabledProfiles:        []string{model.TransportProfileRealityTCPVision, model.TransportProfileTLSSelfSNIWeb},
		TLSSelfSNICertFile:     "/certs/fullchain.pem",
		TLSSelfSNIKeyFile:      "/certs/privkey.pem",
		TLSSelfSNIFallbackDest: "ovpn-web:8080",
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("expected tls self-sni 443 conflict, got %v", err)
	}
}

func TestValidateSpecRejectsTLSSelfSNIMissingCertificate(t *testing.T) {
	t.Parallel()

	err := ValidateSpec(Spec{
		Domain:                 "example.com",
		ThreatDNSServers:       []string{"9.9.9.9"},
		EnabledProfiles:        []string{model.TransportProfileTLSSelfSNIWeb},
		TLSSelfSNIFallbackDest: "ovpn-web:8080",
	})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected tls certificate validation error, got %v", err)
	}
}

func TestValidateSpecRejectsPlannedTransportProfile(t *testing.T) {
	t.Parallel()

	err := ValidateSpec(Spec{
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd"},
		ThreatDNSServers:  []string{"9.9.9.9"},
		EnabledProfiles:   []string{model.TransportProfileWSTLSWeb},
	})
	if err == nil || !strings.Contains(err.Error(), "not renderable yet") {
		t.Fatalf("expected planned profile validation error, got %v", err)
	}
}

func TestValidateSpec(t *testing.T) {
	t.Parallel()

	spec := Spec{
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		SecurityProfile:   SecurityProfileMinimal,
		ThreatDNSServers:  []string{"9.9.9.9"},
		ShortIDs:          []string{"abcd"},
		Users:             []model.User{{Username: "alice", UUID: "u1", Email: "alice@example.com", Enabled: true}},
	}
	if err := ValidateSpec(spec); err != nil {
		t.Fatalf("expected valid spec, got: %v", err)
	}

	spec.RealityPrivateKey = ""
	if err := ValidateSpec(spec); err == nil {
		t.Fatalf("expected invalid spec")
	}

	spec.RealityPrivateKey = "priv"
	spec.RealityServerName = "*.example.com"
	if err := ValidateSpec(spec); err == nil {
		t.Fatalf("expected wildcard server name to be invalid")
	}

	spec.RealityServerName = "www.microsoft.com"
	spec.SecurityProfile = "unknown"
	if err := ValidateSpec(spec); err == nil {
		t.Fatalf("expected invalid security profile")
	}
}

func TestRenderServerJSONFromFixture(t *testing.T) {
	t.Parallel()

	spec := Spec{
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"short01"},
		Users: []model.User{
			{Username: "bob", UUID: "22222222-2222-2222-2222-222222222222", Email: "bob@example.com", Enabled: true},
			{Username: "alice", UUID: "11111111-1111-1111-1111-111111111111", Email: "alice@example.com", Enabled: true},
			{Username: "disabled", UUID: "33333333-3333-3333-3333-333333333333", Email: "disabled@example.com", Enabled: false},
		},
	}
	gotRaw, err := RenderServerJSON(spec)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	wantRaw, err := os.ReadFile(filepath.Join("testdata", "render_expected.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var got any
	var want any
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rendered JSON does not match fixture")
	}
}

func toStrings(in []any) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func assertNoIPv6BlockRule(t *testing.T, rules []any) {
	t.Helper()
	for _, rule := range rules {
		r, ok := rule.(map[string]any)
		if !ok || r["outboundTag"] != "block" {
			continue
		}
		ips, _ := r["ip"].([]any)
		for _, ip := range ips {
			if ip == "::/0" {
				t.Fatalf("unexpected IPv6 block rule in routing rules: %v", rules)
			}
		}
	}
}

func TestNormalizeX25519KeyBase64(t *testing.T) {
	t.Parallel()

	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(i*7 + 3)
	}
	std := base64.RawStdEncoding.EncodeToString(keyBytes)
	want := base64.RawURLEncoding.EncodeToString(keyBytes)
	if got := normalizeX25519KeyBase64(std); got != want {
		t.Fatalf("normalizeX25519KeyBase64(std) = %q, want %q", got, want)
	}
	if got := normalizeX25519KeyBase64(want); got != want {
		t.Fatalf("normalizeX25519KeyBase64(url) = %q, want %q", got, want)
	}
	if got := normalizeX25519KeyBase64("not-a-key"); got != "not-a-key" {
		t.Fatalf("expected passthrough for non-key input, got %q", got)
	}
}

func TestRenderServerJSONNormalizesRealityPrivateKey(t *testing.T) {
	t.Parallel()

	keyBytes := make([]byte, 32)
	for i := range keyBytes {
		keyBytes[i] = byte(255 - i)
	}
	std := base64.RawStdEncoding.EncodeToString(keyBytes)
	want := base64.RawURLEncoding.EncodeToString(keyBytes)

	raw, err := RenderServerJSON(Spec{
		RealityPrivateKey: std,
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd1234"},
		Users:             []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "u@example.com", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(raw), `"privateKey": "`+want+`"`) {
		t.Fatalf("expected normalized private key in rendered json")
	}
}

func TestRenderServerJSONIncludesFallbackRateLimitsWhenProvided(t *testing.T) {
	t.Parallel()

	raw, err := RenderServerJSON(Spec{
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd1234"},
		Users:             []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "u@example.com", Enabled: true}},
		LimitFallbackUpload: &FallbackRateLimit{
			AfterBytes:       4096,
			BytesPerSec:      2048,
			BurstBytesPerSec: 4096,
		},
		LimitFallbackDownload: &FallbackRateLimit{
			AfterBytes:       8192,
			BytesPerSec:      3072,
			BurstBytesPerSec: 6144,
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(raw), `"limitFallbackUpload"`) {
		t.Fatalf("expected limitFallbackUpload in rendered JSON")
	}
	if !strings.Contains(string(raw), `"limitFallbackDownload"`) {
		t.Fatalf("expected limitFallbackDownload in rendered JSON")
	}
}

func TestRenderServerJSONProxyRoutesForeignTrafficThroughRelay(t *testing.T) {
	t.Parallel()

	raw, err := RenderServerJSON(Spec{
		Role:              model.ServerRoleProxy,
		ProxyPreset:       model.ProxyPresetRU,
		RealityPrivateKey: "priv",
		RealityPublicKey:  "backend-pub",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd1234"},
		ThreatDNSServers:  []string{"1.1.1.2"},
		Users:             []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "client@example.com", Enabled: true}},
		ServiceUsers:      []ServiceUser{{UUID: "22222222-2222-2222-2222-222222222222", Email: "proxy-service@cluster"}},
		ProxyRelay: &ProxyRelay{
			Address:     "haproxy",
			Port:        15443,
			ServiceUUID: "22222222-2222-2222-2222-222222222222",
			ServerName:  "backend.example.com",
			PublicKey:   "backend-pub",
			ShortID:     "beefcafe",
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal rendered config: %v", err)
	}

	inbounds, _ := obj["inbounds"].([]any)
	if len(inbounds) < 2 {
		t.Fatalf("expected api and client inbound, got %d", len(inbounds))
	}
	clientInbound, _ := inbounds[1].(map[string]any)
	settings, _ := clientInbound["settings"].(map[string]any)
	clients, _ := settings["clients"].([]any)
	if len(clients) != 2 {
		t.Fatalf("expected user and service client, got %d", len(clients))
	}

	outbounds, _ := obj["outbounds"].([]any)
	foundForeignPool := false
	for _, outbound := range outbounds {
		entry, _ := outbound.(map[string]any)
		if entry["tag"] == "foreign-pool" {
			foundForeignPool = true
			break
		}
	}
	if !foundForeignPool {
		t.Fatalf("expected foreign-pool outbound in proxy config")
	}

	routing, _ := obj["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	var foundRUDirect bool
	var foundForeignDefault bool
	for _, rawRule := range rules {
		rule, _ := rawRule.(map[string]any)
		if rule["outboundTag"] == "direct" {
			if domains, ok := rule["domain"].([]any); ok {
				all := strings.Join(toStrings(domains), ",")
				if strings.Contains(all, "geosite:ru-available-only-inside") {
					foundRUDirect = true
				}
			}
		}
		if rule["outboundTag"] == "foreign-pool" {
			if inboundTags, ok := rule["inboundTag"].([]any); ok && strings.Contains(strings.Join(toStrings(inboundTags), ","), "vless-reality") {
				foundForeignDefault = true
			}
		}
	}
	if !foundRUDirect {
		t.Fatalf("expected Russian direct routing rule in proxy config")
	}
	if !foundForeignDefault {
		t.Fatalf("expected default foreign routing rule in proxy config")
	}
}

func TestValidateSpecRejectsUnknownProxyPreset(t *testing.T) {
	t.Parallel()

	err := ValidateSpec(Spec{
		Role:              model.ServerRoleProxy,
		ProxyPreset:       "de",
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ThreatDNSServers:  []string{"1.1.1.2"},
		ShortIDs:          []string{"abcd1234"},
		ProxyRelay: &ProxyRelay{
			Address:     "haproxy",
			Port:        15443,
			ServiceUUID: "22222222-2222-2222-2222-222222222222",
			ServerName:  "backend.example.com",
			PublicKey:   "backend-pub",
			ShortID:     "beefcafe",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "proxy preset") {
		t.Fatalf("expected proxy preset validation error, got %v", err)
	}
}

func TestRenderServerJSONProxySupportsChinaPreset(t *testing.T) {
	t.Parallel()

	raw, err := RenderServerJSON(Spec{
		Role:              model.ServerRoleProxy,
		ProxyPreset:       model.ProxyPresetCN,
		RealityPrivateKey: "priv",
		RealityPublicKey:  "backend-pub",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd1234"},
		ThreatDNSServers:  []string{"1.1.1.2"},
		Users:             []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "client@example.com", Enabled: true}},
		ServiceUsers:      []ServiceUser{{UUID: "22222222-2222-2222-2222-222222222222", Email: "proxy-service@cluster"}},
		ProxyRelay: &ProxyRelay{
			Address:     "haproxy",
			Port:        15443,
			ServiceUUID: "22222222-2222-2222-2222-222222222222",
			ServerName:  "backend.example.com",
			PublicKey:   "backend-pub",
			ShortID:     "beefcafe",
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	rendered := string(raw)
	for _, want := range []string{"geosite:cn", "regexp:.*\\\\.cn$", "geoip:cn", "foreign-pool"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered proxy config to contain %q", want)
		}
	}
	if strings.Contains(rendered, "geosite:ru-available-only-inside") {
		t.Fatalf("china preset should not include russian direct routing rules")
	}
}

func TestRenderServerJSONVPNIncludesProxyServiceUser(t *testing.T) {
	t.Parallel()

	raw, err := RenderServerJSON(Spec{
		Role:              model.ServerRoleVPN,
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		ShortIDs:          []string{"abcd1234"},
		Users:             []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "client@example.com", Enabled: true}},
		ServiceUsers:      []ServiceUser{{UUID: "22222222-2222-2222-2222-222222222222", Email: "proxy-service@cluster"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal rendered config: %v", err)
	}
	inbounds, _ := obj["inbounds"].([]any)
	if len(inbounds) < 2 {
		t.Fatalf("expected api and client inbound, got %d", len(inbounds))
	}
	clientInbound, _ := inbounds[1].(map[string]any)
	settings, _ := clientInbound["settings"].(map[string]any)
	clients, _ := settings["clients"].([]any)
	if len(clients) != 2 {
		t.Fatalf("expected end-user and proxy service user, got %d", len(clients))
	}
	var foundServiceUser bool
	for _, rawClient := range clients {
		client, _ := rawClient.(map[string]any)
		if client["email"] == "proxy-service@cluster" && client["id"] == "22222222-2222-2222-2222-222222222222" {
			foundServiceUser = true
			break
		}
	}
	if !foundServiceUser {
		t.Fatalf("expected proxy service user in vpn inbound clients: %#v", clients)
	}
}

func TestValidateSpecRejectsNegativeFallbackRateLimits(t *testing.T) {
	t.Parallel()

	spec := Spec{
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		SecurityProfile:   SecurityProfileMinimal,
		ThreatDNSServers:  []string{"9.9.9.9"},
		ShortIDs:          []string{"abcd"},
		Users:             []model.User{{Username: "alice", UUID: "u1", Email: "alice@example.com", Enabled: true}},
		LimitFallbackUpload: &FallbackRateLimit{
			AfterBytes: -1,
		},
	}
	if err := ValidateSpec(spec); err == nil {
		t.Fatalf("expected invalid spec with negative fallback limit")
	}
}

func TestRenderServerJSONProfileOffSkipsSecurityRoutingAndDNS(t *testing.T) {
	t.Parallel()

	raw, err := RenderServerJSON(Spec{
		RealityPrivateKey: "priv",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		SecurityProfile:   SecurityProfileOff,
		ShortIDs:          []string{"abcd1234"},
		Users:             []model.User{{UUID: "11111111-1111-1111-1111-111111111111", Email: "u@example.com", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := obj["dns"]; ok {
		t.Fatalf("dns should be absent when security profile is off")
	}
	routing, ok := obj["routing"].(map[string]any)
	if !ok {
		t.Fatalf("routing missing")
	}
	rules, _ := routing["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("expected only api routing rule, got %d", len(rules))
	}
	assertNoIPv6BlockRule(t, rules)
}
