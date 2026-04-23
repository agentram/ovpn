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
	routing, ok := obj["routing"].(map[string]any)
	if !ok {
		t.Fatalf("routing missing")
	}
	rules, _ := routing["rules"].([]any)
	if len(rules) < 3 {
		t.Fatalf("expected api + security rules, got %d", len(rules))
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
		t.Fatalf("expected only api rule, got %d", len(rules))
	}
}
