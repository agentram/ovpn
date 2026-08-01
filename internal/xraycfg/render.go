package xraycfg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"ovpn/internal/model"
)

type Spec struct {
	Role                   string
	ProxyPreset            string
	Domain                 string
	RealityPrivateKey      string
	RealityServerName      string
	RealityTarget          string
	RealityPublicKey       string
	VLESSServerDecryption  string
	SecurityProfile        string
	ThreatDNSServers       []string
	LimitFallbackUpload    *FallbackRateLimit
	LimitFallbackDownload  *FallbackRateLimit
	ShortIDs               []string
	EnabledProfiles        []string
	APIListen              string
	APIPort                int
	LogLevel               string
	AccessLogPath          string
	TLSSelfSNICertFile     string
	TLSSelfSNIKeyFile      string
	TLSSelfSNIFallbackDest string
	Users                  []model.User
	ServiceUsers           []ServiceUser
	ProxyRelay             *ProxyRelay
}

type FallbackRateLimit struct {
	AfterBytes       int64
	BytesPerSec      int64
	BurstBytesPerSec int64
}

type ServiceUser struct {
	UUID  string
	Email string
}

type ProxyRelay struct {
	Address     string
	Port        int
	ServiceUUID string
	ServerName  string
	PublicKey   string
	ShortID     string
}

type XrayConfig struct {
	Log       any   `json:"log"`
	DNS       any   `json:"dns,omitempty"`
	Stats     any   `json:"stats"`
	Policy    any   `json:"policy"`
	API       any   `json:"api"`
	Routing   any   `json:"routing"`
	Inbounds  []any `json:"inbounds"`
	Outbounds []any `json:"outbounds"`
}

const (
	SecurityProfileMinimal = "minimal"
	SecurityProfileOff     = "off"

	DefaultClientFingerprint = "firefox"

	DefaultTLSSelfSNICertFile     = "/etc/xray/certs/fullchain.pem"
	DefaultTLSSelfSNIKeyFile      = "/etc/xray/certs/privkey.pem"
	DefaultTLSSelfSNIFallbackDest = "ovpn-web:8080"
)

var defaultThreatDNSServers = []string{"9.9.9.9", "149.112.112.112"}

var supportedClientFingerprints = []string{
	"firefox",
	"chrome",
	"safari",
	"ios",
	"android",
	"edge",
	"360",
	"qq",
	"random",
	"randomized",
}

// RenderServerJSON renders a complete Xray server config (inbounds, routing, outbounds) as JSON from spec.
func RenderServerJSON(spec Spec) ([]byte, error) {
	spec.SecurityProfile = normalizeSecurityProfile(spec.SecurityProfile)
	spec.Role = model.NormalizeServerRole(spec.Role)
	if spec.Role == "" {
		spec.Role = model.ServerRoleVPN
	}
	if spec.SecurityProfile == SecurityProfileMinimal && len(spec.ThreatDNSServers) == 0 {
		spec.ThreatDNSServers = append([]string(nil), defaultThreatDNSServers...)
	}
	spec.EnabledProfiles = normalizeEnabledProfiles(spec.EnabledProfiles)
	if includesProfile(spec.EnabledProfiles, model.TransportProfileTLSSelfSNIWeb) {
		if strings.TrimSpace(spec.TLSSelfSNICertFile) == "" {
			spec.TLSSelfSNICertFile = DefaultTLSSelfSNICertFile
		}
		if strings.TrimSpace(spec.TLSSelfSNIKeyFile) == "" {
			spec.TLSSelfSNIKeyFile = DefaultTLSSelfSNIKeyFile
		}
		if strings.TrimSpace(spec.TLSSelfSNIFallbackDest) == "" {
			spec.TLSSelfSNIFallbackDest = DefaultTLSSelfSNIFallbackDest
		}
	}
	if err := ValidateSpec(spec); err != nil {
		return nil, err
	}
	if spec.APIListen == "" {
		spec.APIListen = "0.0.0.0"
	}
	if spec.APIPort == 0 {
		spec.APIPort = 10085
	}
	if profilesNeedReality(spec.EnabledProfiles) && len(spec.ShortIDs) == 0 {
		return nil, fmt.Errorf("at least one short ID is required")
	}
	// Backward compatibility: older ovpn versions persisted REALITY keys in std-base64.
	// Normalize to URL-safe raw base64 before rendering, so existing local DB state still deploys.
	if profilesNeedReality(spec.EnabledProfiles) {
		spec.RealityPrivateKey = normalizeX25519KeyBase64(spec.RealityPrivateKey)
	}
	users := buildClientUsers(spec.Users, spec.ServiceUsers, "xtls-rprx-vision")
	plainUsers := buildClientUsers(spec.Users, spec.ServiceUsers, "")

	realitySettings := map[string]any{
		"show":        false,
		"target":      spec.RealityTarget,
		"xver":        0,
		"serverNames": []string{spec.RealityServerName},
		"privateKey":  spec.RealityPrivateKey,
		"shortIds":    spec.ShortIDs,
	}
	if spec.LimitFallbackUpload != nil {
		realitySettings["limitFallbackUpload"] = map[string]any{
			"afterBytes":       spec.LimitFallbackUpload.AfterBytes,
			"bytesPerSec":      spec.LimitFallbackUpload.BytesPerSec,
			"burstBytesPerSec": spec.LimitFallbackUpload.BurstBytesPerSec,
		}
	}
	if spec.LimitFallbackDownload != nil {
		realitySettings["limitFallbackDownload"] = map[string]any{
			"afterBytes":       spec.LimitFallbackDownload.AfterBytes,
			"bytesPerSec":      spec.LimitFallbackDownload.BytesPerSec,
			"burstBytesPerSec": spec.LimitFallbackDownload.BurstBytesPerSec,
		}
	}

	rules := []any{
		map[string]any{
			"type":        "field",
			"inboundTag":  []string{"api"},
			"outboundTag": "api",
		},
	}
	logConfig := map[string]any{
		"loglevel": normalizeLogLevel(spec.LogLevel),
	}
	if strings.TrimSpace(spec.AccessLogPath) != "" {
		logConfig["access"] = strings.TrimSpace(spec.AccessLogPath)
	}
	cfg := XrayConfig{
		Log:   logConfig,
		Stats: map[string]any{},
		Policy: map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"statsUserUplink":   true,
					"statsUserDownlink": true,
				},
			},
			"system": map[string]any{
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		},
		API: map[string]any{
			"tag":      "api",
			"services": []string{"StatsService", "HandlerService"},
		},
		Routing: map[string]any{
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
		Inbounds: []any{
			map[string]any{
				// API must listen on container network (not localhost) so ovpn-agent sidecar can reach it.
				"listen":   spec.APIListen,
				"port":     spec.APIPort,
				"protocol": "dokodemo-door",
				"settings": map[string]any{"address": "127.0.0.1"},
				"tag":      "api",
			},
		},
		Outbounds: baseOutbounds(spec),
	}
	tlsSelfSNISettings := map[string]any{
		"alpn": []string{"http/1.1"},
		"certificates": []any{
			map[string]any{
				"certificateFile": spec.TLSSelfSNICertFile,
				"keyFile":         spec.TLSSelfSNIKeyFile,
			},
		},
	}
	cfg.Inbounds = append(cfg.Inbounds, buildProfileInbounds(spec.EnabledProfiles, users, plainUsers, realitySettings, tlsSelfSNISettings, spec.TLSSelfSNIFallbackDest, spec.VLESSServerDecryption)...)
	if spec.SecurityProfile == SecurityProfileMinimal {
		cfg.DNS = map[string]any{
			"queryStrategy": "UseIPv4",
			"servers":       spec.ThreatDNSServers,
		}
		cfg.Routing.(map[string]any)["rules"] = append(rules,
			map[string]any{
				"type":        "field",
				"ip":          []string{"::/0"},
				"outboundTag": "block",
			},
			map[string]any{
				"type":        "field",
				"protocol":    []string{"bittorrent"},
				"outboundTag": "block",
			},
			map[string]any{
				"type":        "field",
				"domain":      []string{"geosite:category-public-tracker"},
				"outboundTag": "block",
			},
		)
	}
	if spec.Role == model.ServerRoleProxy {
		proxyPreset, err := resolveProxyPreset(spec.ProxyPreset)
		if err != nil {
			return nil, err
		}
		cfg.Routing.(map[string]any)["rules"] = append(cfg.Routing.(map[string]any)["rules"].([]any),
			map[string]any{
				"type":        "field",
				"domain":      proxyPreset.DirectDomains,
				"outboundTag": "direct",
			},
			map[string]any{
				"type":        "field",
				"ip":          proxyPreset.DirectIPs,
				"outboundTag": "direct",
			},
			map[string]any{
				"type":        "field",
				"inboundTag":  profileInboundTags(spec.EnabledProfiles),
				"outboundTag": "foreign-pool",
			},
		)
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return b, nil
}

func buildClientUsers(users []model.User, serviceUsers []ServiceUser, flow string) []map[string]any {
	out := make([]map[string]any, 0, len(users)+len(serviceUsers))
	for _, u := range users {
		if !u.Enabled {
			continue
		}
		row := map[string]any{
			"id":    u.UUID,
			"email": u.Email,
		}
		if strings.TrimSpace(flow) != "" {
			row["flow"] = flow
		}
		out = append(out, row)
	}
	for _, svc := range serviceUsers {
		row := map[string]any{
			"id":    svc.UUID,
			"email": svc.Email,
		}
		if strings.TrimSpace(flow) != "" {
			row["flow"] = flow
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["email"].(string) < out[j]["email"].(string)
	})
	return out
}

func normalizeEnabledProfiles(raw []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(raw)+1)
	if len(raw) == 0 {
		raw = []string{model.TransportProfileRealityTCPVision}
	}
	for _, p := range raw {
		name := model.NormalizeTransportProfile(p)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return []string{model.TransportProfileRealityTCPVision}
	}
	return out
}

func buildProfileInbounds(profiles []string, visionUsers []map[string]any, plainUsers []map[string]any, realitySettings map[string]any, tlsSelfSNISettings map[string]any, tlsSelfSNIFallbackDest string, vlessServerDecryption string) []any {
	out := make([]any, 0, len(profiles))
	for _, profile := range profiles {
		switch profile {
		case model.TransportProfileRealityTCPVision:
			out = append(out, vlessInbound("vless-reality", 443, visionUsers, map[string]any{
				"network":         "tcp",
				"security":        "reality",
				"realitySettings": realitySettings,
			}))
		case model.TransportProfilePlainXHTTP:
			out = append(out, vlessInbound("vless-xhttp-plain", 13179, plainUsers, map[string]any{
				"network":  "xhttp",
				"security": "none",
				"xhttpSettings": map[string]any{
					"path": "/",
					"mode": "auto",
				},
			}))
		case model.TransportProfileVLESSEncXHTTP:
			out = append(out, vlessInboundWithSettings("vless-xhttp-vlessenc", 13180, visionUsers, map[string]any{
				"network":  "xhttp",
				"security": "none",
				"xhttpSettings": map[string]any{
					"path": "/",
					"mode": "auto",
				},
			}, map[string]any{
				"decryption": vlessServerDecryption,
			}))
		case model.TransportProfileTLSSelfSNIWeb:
			out = append(out, vlessInboundWithSettings("vless-tcp-tls-selfsni-web", 443, visionUsers, map[string]any{
				"network":     "tcp",
				"security":    "tls",
				"tlsSettings": tlsSelfSNISettings,
			}, map[string]any{
				"fallbacks": []any{
					map[string]any{"dest": tlsSelfSNIFallbackDest},
				},
			}))
		}
	}
	return out
}

func vlessInbound(tag string, port int, users []map[string]any, streamSettings map[string]any) map[string]any {
	return vlessInboundWithSettings(tag, port, users, streamSettings, nil)
}

func vlessInboundWithSettings(tag string, port int, users []map[string]any, streamSettings map[string]any, extraSettings map[string]any) map[string]any {
	settings := map[string]any{
		"clients":    users,
		"decryption": "none",
	}
	for k, v := range extraSettings {
		settings[k] = v
	}
	return map[string]any{
		"tag":            tag,
		"listen":         "0.0.0.0",
		"port":           port,
		"protocol":       "vless",
		"settings":       settings,
		"streamSettings": streamSettings,
		"sniffing": map[string]any{
			"enabled":      true,
			"destOverride": []string{"http", "tls", "quic"},
			"routeOnly":    true,
		},
	}
}

func profileInboundTags(profiles []string) []string {
	tags := make([]string, 0, len(profiles))
	for _, profile := range normalizeEnabledProfiles(profiles) {
		meta, ok := model.LookupTransportProfile(profile)
		if !ok || strings.TrimSpace(meta.InboundTag) == "" {
			continue
		}
		tags = append(tags, meta.InboundTag)
	}
	if len(tags) == 0 {
		return []string{"vless-reality"}
	}
	return tags
}

func baseOutbounds(spec Spec) []any {
	if spec.Role != model.ServerRoleProxy || spec.ProxyRelay == nil {
		return []any{
			freedomOutbound("direct"),
			map[string]any{"protocol": "blackhole", "tag": "block"},
			freedomOutbound("api"),
		}
	}
	return []any{
		map[string]any{"protocol": "blackhole", "tag": "block"},
		freedomOutbound("direct"),
		freedomOutbound("api"),
		map[string]any{
			"tag":      "foreign-pool",
			"protocol": "vless",
			"settings": map[string]any{
				"vnext": []any{
					map[string]any{
						"address": spec.ProxyRelay.Address,
						"port":    spec.ProxyRelay.Port,
						"users": []any{
							map[string]any{
								"id":         spec.ProxyRelay.ServiceUUID,
								"encryption": "none",
								"flow":       "xtls-rprx-vision",
							},
						},
					},
				},
			},
			"streamSettings": map[string]any{
				"network":  "tcp",
				"security": "reality",
				"realitySettings": map[string]any{
					"serverName":  spec.ProxyRelay.ServerName,
					"publicKey":   spec.ProxyRelay.PublicKey,
					"shortId":     spec.ProxyRelay.ShortID,
					"fingerprint": "chrome",
					"spiderX":     "/",
				},
			},
		},
	}
}

func freedomOutbound(tag string) map[string]any {
	return map[string]any{
		"protocol": "freedom",
		"tag":      tag,
		"settings": map[string]any{
			"domainStrategy": "UseIPv4",
		},
	}
}

// normalizeX25519KeyBase64 re-encodes a 32-byte X25519 key to URL-safe raw base64, accepting legacy encodings.
func normalizeX25519KeyBase64(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		if b, err := enc.DecodeString(raw); err == nil && len(b) == 32 {
			return base64.RawURLEncoding.EncodeToString(b)
		}
	}
	return raw
}

// normalizeLogLevel canonicalizes an Xray log level, defaulting to warning.
func normalizeLogLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return "debug"
	case "info":
		return "info"
	case "warning", "warn":
		return "warning"
	case "error":
		return "error"
	default:
		return "warning"
	}
}

// normalizeSecurityProfile canonicalizes the security profile to minimal or off, returning "" when invalid.
func normalizeSecurityProfile(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", SecurityProfileMinimal:
		return SecurityProfileMinimal
	case SecurityProfileOff:
		return SecurityProfileOff
	default:
		return ""
	}
}

type proxyPreset struct {
	DirectDomains []string
	DirectIPs     []string
}

func resolveProxyPreset(raw string) (proxyPreset, error) {
	switch model.NormalizeProxyPreset(raw) {
	case model.ProxyPresetRU:
		return proxyPreset{
			DirectDomains: []string{"geosite:ru-available-only-inside", "regexp:.*\\.ru$", "regexp:.*\\.su$", "regexp:.*\\.xn--p1ai$"},
			DirectIPs:     []string{"geoip:ru", "geoip:private"},
		}, nil
	case model.ProxyPresetCN:
		return proxyPreset{
			DirectDomains: []string{"geosite:cn", "regexp:.*\\.cn$", "regexp:.*\\.xn--fiqs8s$", "regexp:.*\\.xn--fiqz9s$", "regexp:.*\\.xn--55qx5d$", "regexp:.*\\.xn--io0a7i$"},
			DirectIPs:     []string{"geoip:cn", "geoip:private"},
		}, nil
	default:
		return proxyPreset{}, fmt.Errorf("proxy preset must be one of: %s", model.SupportedProxyPresetsText())
	}
}

// ValidateSpec checks that a Spec has the REALITY and security fields required to render a valid config.
func ValidateSpec(spec Spec) error {
	if spec.SecurityProfile == "" {
		spec.SecurityProfile = SecurityProfileMinimal
	}
	spec.EnabledProfiles = normalizeEnabledProfiles(spec.EnabledProfiles)
	if spec.SecurityProfile != SecurityProfileMinimal && spec.SecurityProfile != SecurityProfileOff {
		return fmt.Errorf("security profile must be %q or %q", SecurityProfileMinimal, SecurityProfileOff)
	}
	if profilesNeedReality(spec.EnabledProfiles) {
		if strings.TrimSpace(spec.RealityPrivateKey) == "" {
			return fmt.Errorf("reality private key is required")
		}
		if strings.TrimSpace(spec.RealityServerName) == "" {
			return fmt.Errorf("reality server name is required")
		}
		if strings.Contains(spec.RealityServerName, "*") {
			return fmt.Errorf("reality server name must not contain wildcard '*'")
		}
		if strings.TrimSpace(spec.RealityTarget) == "" {
			return fmt.Errorf("reality target is required")
		}
		if len(spec.ShortIDs) == 0 {
			return fmt.Errorf("at least one short ID is required")
		}
	}
	if spec.SecurityProfile == SecurityProfileMinimal {
		if len(spec.ThreatDNSServers) == 0 {
			return fmt.Errorf("threat dns servers are required when security profile is %q", SecurityProfileMinimal)
		}
		for _, raw := range spec.ThreatDNSServers {
			server := strings.TrimSpace(raw)
			if server == "" {
				return fmt.Errorf("threat dns servers must not contain empty values")
			}
			if strings.Contains(server, "://") {
				return fmt.Errorf("threat dns server %q must not contain URI scheme", server)
			}
		}
	}
	if spec.LimitFallbackUpload != nil {
		if spec.LimitFallbackUpload.AfterBytes < 0 || spec.LimitFallbackUpload.BytesPerSec < 0 || spec.LimitFallbackUpload.BurstBytesPerSec < 0 {
			return fmt.Errorf("limitFallbackUpload values must be >= 0")
		}
	}
	if spec.LimitFallbackDownload != nil {
		if spec.LimitFallbackDownload.AfterBytes < 0 || spec.LimitFallbackDownload.BytesPerSec < 0 || spec.LimitFallbackDownload.BurstBytesPerSec < 0 {
			return fmt.Errorf("limitFallbackDownload values must be >= 0")
		}
	}
	for _, raw := range spec.EnabledProfiles {
		name := model.NormalizeTransportProfile(raw)
		if name == "" {
			return fmt.Errorf("unsupported transport profile %q", raw)
		}
	}
	if includesProfile(spec.EnabledProfiles, model.TransportProfileTLSSelfSNIWeb) {
		if includesProfile(spec.EnabledProfiles, model.TransportProfileRealityTCPVision) {
			return fmt.Errorf("%s conflicts with %s because both require 443/tcp; disable one profile before deploy", model.TransportProfileTLSSelfSNIWeb, model.TransportProfileRealityTCPVision)
		}
		if spec.Role == model.ServerRoleProxy {
			return fmt.Errorf("%s is only supported on vpn servers", model.TransportProfileTLSSelfSNIWeb)
		}
		if strings.TrimSpace(spec.Domain) == "" {
			return fmt.Errorf("%s requires server domain for TLS SNI", model.TransportProfileTLSSelfSNIWeb)
		}
		if strings.TrimSpace(spec.TLSSelfSNICertFile) == "" || strings.TrimSpace(spec.TLSSelfSNIKeyFile) == "" {
			return fmt.Errorf("%s requires TLS certificate and key files", model.TransportProfileTLSSelfSNIWeb)
		}
		if strings.TrimSpace(spec.TLSSelfSNIFallbackDest) == "" {
			return fmt.Errorf("%s requires fallback destination", model.TransportProfileTLSSelfSNIWeb)
		}
	}
	if includesProfile(spec.EnabledProfiles, model.TransportProfileVLESSEncXHTTP) {
		if spec.Role == model.ServerRoleProxy {
			return fmt.Errorf("%s is only supported on vpn servers", model.TransportProfileVLESSEncXHTTP)
		}
		if !validVLESSEncryptionConfigValue(spec.VLESSServerDecryption, vlessServerPrefix) {
			return fmt.Errorf("%s requires a valid ML-KEM-768 native server decryption value; enable the profile with `ovpn server profile enable <server> %s`", model.TransportProfileVLESSEncXHTTP, model.TransportProfileVLESSEncXHTTP)
		}
	}
	if spec.Role != "" && spec.Role != model.ServerRoleVPN && spec.Role != model.ServerRoleProxy {
		return fmt.Errorf("role must be %q or %q", model.ServerRoleVPN, model.ServerRoleProxy)
	}
	for _, u := range spec.Users {
		if !u.Enabled {
			continue
		}
		if strings.TrimSpace(u.UUID) == "" {
			return fmt.Errorf("enabled user %q is missing uuid", u.Username)
		}
		if strings.TrimSpace(u.Email) == "" {
			return fmt.Errorf("enabled user %q is missing email", u.Username)
		}
	}
	for _, svc := range spec.ServiceUsers {
		if strings.TrimSpace(svc.UUID) == "" || strings.TrimSpace(svc.Email) == "" {
			return fmt.Errorf("service users must include uuid and email")
		}
	}
	if spec.Role == model.ServerRoleProxy {
		if _, err := resolveProxyPreset(spec.ProxyPreset); err != nil {
			return err
		}
		if spec.ProxyRelay == nil {
			return fmt.Errorf("proxy relay is required for proxy role")
		}
		if strings.TrimSpace(spec.ProxyRelay.Address) == "" || spec.ProxyRelay.Port <= 0 {
			return fmt.Errorf("proxy relay address and port are required")
		}
		if strings.TrimSpace(spec.ProxyRelay.ServiceUUID) == "" {
			return fmt.Errorf("proxy relay service uuid is required")
		}
		if strings.TrimSpace(spec.ProxyRelay.ServerName) == "" || strings.TrimSpace(spec.ProxyRelay.PublicKey) == "" || strings.TrimSpace(spec.ProxyRelay.ShortID) == "" {
			return fmt.Errorf("proxy relay reality parameters are required")
		}
	}
	return nil
}

func includesProfile(profiles []string, want string) bool {
	want = model.NormalizeTransportProfile(want)
	for _, profile := range profiles {
		if model.NormalizeTransportProfile(profile) == want {
			return true
		}
	}
	return false
}

func profilesNeedReality(profiles []string) bool {
	for _, profile := range normalizeEnabledProfiles(profiles) {
		meta, ok := model.LookupTransportProfile(profile)
		if ok && meta.Kind == "reality" {
			return true
		}
	}
	return false
}

type LinkInput struct {
	Address     string
	Port        int
	UUID        string
	ServerName  string
	Password    string
	ShortID     string
	Flow        string
	Label       string
	Profile     string
	Fingerprint string
	SpiderX     string
	Encryption  string
}

const (
	vlessServerPrefix = "mlkem768x25519plus.native.600s."
	vlessClientPrefix = "mlkem768x25519plus.native.0rtt."
)

func validVLESSEncryptionConfigValue(value, prefix string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, prefix) &&
		strings.TrimSpace(strings.TrimPrefix(value, prefix)) != "" &&
		!strings.ContainsAny(value, " \t\r\n")
}

func NormalizeClientFingerprint(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return DefaultClientFingerprint
	}
	for _, fp := range supportedClientFingerprints {
		if v == fp {
			return v
		}
	}
	return ""
}

func SupportedClientFingerprintsText() string {
	return strings.Join(supportedClientFingerprints, ", ")
}

// BuildVLESSLink renders a client VLESS connection URL from in.
func BuildVLESSLink(in LinkInput) string {
	profile := model.NormalizeTransportProfile(in.Profile)
	if profile == "" {
		profile = model.TransportProfileRealityTCPVision
	}
	if in.Port == 0 {
		switch profile {
		case model.TransportProfilePlainXHTTP:
			in.Port = 13179
		case model.TransportProfileVLESSEncXHTTP:
			in.Port = 13180
		default:
			in.Port = 443
		}
	}
	if in.Flow == "" {
		in.Flow = "xtls-rprx-vision"
	}
	label := in.Label
	if strings.TrimSpace(label) == "" {
		label = "ovpn"
	}
	fingerprint := NormalizeClientFingerprint(in.Fingerprint)
	if fingerprint == "" {
		fingerprint = DefaultClientFingerprint
	}
	spiderX := strings.TrimSpace(in.SpiderX)
	switch profile {
	case model.TransportProfilePlainXHTTP:
		return fmt.Sprintf(
			"vless://%s@%s:%d?security=none&encryption=none&type=xhttp&path=%s&mode=auto#%s",
			in.UUID,
			in.Address,
			in.Port,
			url.QueryEscape("/"),
			urlEscapeLabel(label),
		)
	case model.TransportProfileTLSSelfSNIWeb:
		return fmt.Sprintf(
			"vless://%s@%s:%d?security=tls&encryption=none&fp=%s&type=tcp&flow=%s&sni=%s&alpn=%s&headerType=none#%s",
			in.UUID,
			in.Address,
			in.Port,
			fingerprint,
			in.Flow,
			in.ServerName,
			url.QueryEscape("http/1.1"),
			urlEscapeLabel(label),
		)
	case model.TransportProfileVLESSEncXHTTP:
		return fmt.Sprintf(
			"vless://%s@%s:%d?security=none&encryption=%s&type=xhttp&path=%s&mode=auto&flow=%s#%s",
			in.UUID,
			in.Address,
			in.Port,
			url.QueryEscape(in.Encryption),
			url.QueryEscape("/"),
			in.Flow,
			urlEscapeLabel(label),
		)
	}
	// Keep pbk query key for broad client compatibility.
	spx := ""
	if spiderX != "" {
		spx = "&spx=" + url.QueryEscape(spiderX)
	}
	return fmt.Sprintf(
		"vless://%s@%s:%d?security=reality&encryption=none&pbk=%s&fp=%s&type=tcp&flow=%s&sni=%s&sid=%s%s#%s",
		in.UUID,
		in.Address,
		in.Port,
		in.Password,
		fingerprint,
		in.Flow,
		in.ServerName,
		in.ShortID,
		spx,
		urlEscapeLabel(label),
	)
}

// urlEscapeLabel escapes a value for use as the fragment label of a VLESS link.
func urlEscapeLabel(v string) string {
	replacer := strings.NewReplacer(" ", "%20", "#", "%23")
	return replacer.Replace(v)
}
