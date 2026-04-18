package xraycfg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"ovpn/internal/model"
)

type Spec struct {
	Domain                string
	RealityPrivateKey     string
	RealityServerName     string
	RealityTarget         string
	SecurityProfile       string
	ThreatDNSServers      []string
	LimitFallbackUpload   *FallbackRateLimit
	LimitFallbackDownload *FallbackRateLimit
	ShortIDs              []string
	APIListen             string
	APIPort               int
	LogLevel              string
	Users                 []model.User
}

type FallbackRateLimit struct {
	AfterBytes       int64
	BytesPerSec      int64
	BurstBytesPerSec int64
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
)

var defaultThreatDNSServers = []string{"9.9.9.9", "149.112.112.112"}

// RenderServerJSON renders server json into the format expected by callers.
func RenderServerJSON(spec Spec) ([]byte, error) {
	spec.SecurityProfile = normalizeSecurityProfile(spec.SecurityProfile)
	if spec.SecurityProfile == SecurityProfileMinimal && len(spec.ThreatDNSServers) == 0 {
		spec.ThreatDNSServers = append([]string(nil), defaultThreatDNSServers...)
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
	if len(spec.ShortIDs) == 0 {
		return nil, fmt.Errorf("at least one short ID is required")
	}
	// Backward compatibility: older ovpn versions persisted REALITY keys in std-base64.
	// Normalize to URL-safe raw base64 before rendering, so existing local DB state still deploys.
	spec.RealityPrivateKey = normalizeX25519KeyBase64(spec.RealityPrivateKey)
	users := make([]map[string]any, 0, len(spec.Users))
	for _, u := range spec.Users {
		if !u.Enabled {
			continue
		}
		users = append(users, map[string]any{
			"id":    u.UUID,
			"email": u.Email,
			"flow":  "xtls-rprx-vision",
		})
	}
	// Keep stable user ordering so rendered config diffs stay deterministic.
	sort.Slice(users, func(i, j int) bool {
		return users[i]["email"].(string) < users[j]["email"].(string)
	})

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
	cfg := XrayConfig{
		Log: map[string]any{
			"loglevel": normalizeLogLevel(spec.LogLevel),
		},
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
			"domainStrategy": "IPIfNonMatch",
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
			map[string]any{
				"tag":      "vless-reality",
				"listen":   "0.0.0.0",
				"port":     443,
				"protocol": "vless",
				"settings": map[string]any{
					"clients":    users,
					"decryption": "none",
				},
				"streamSettings": map[string]any{
					"network":         "tcp",
					"security":        "reality",
					"realitySettings": realitySettings,
				},
				"sniffing": map[string]any{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"routeOnly":    true,
				},
			},
		},
		Outbounds: []any{
			map[string]any{"protocol": "freedom", "tag": "direct"},
			map[string]any{"protocol": "blackhole", "tag": "block"},
			map[string]any{"protocol": "freedom", "tag": "api"},
		},
	}
	if spec.SecurityProfile == SecurityProfileMinimal {
		cfg.DNS = map[string]any{
			"servers": spec.ThreatDNSServers,
		}
		cfg.Routing.(map[string]any)["rules"] = append(rules,
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
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return b, nil
}

// normalizeX25519KeyBase64 normalizes x 25519 key base 64 and applies fallback defaults.
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

// normalizeLogLevel normalizes log level and applies fallback defaults.
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

// normalizeSecurityProfile normalizes security profile and applies fallback defaults.
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

// ValidateSpec executes spec flow and returns the first error.
func ValidateSpec(spec Spec) error {
	if spec.SecurityProfile == "" {
		spec.SecurityProfile = SecurityProfileMinimal
	}
	if spec.SecurityProfile != SecurityProfileMinimal && spec.SecurityProfile != SecurityProfileOff {
		return fmt.Errorf("security profile must be %q or %q", SecurityProfileMinimal, SecurityProfileOff)
	}
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
	if len(spec.ShortIDs) == 0 {
		return fmt.Errorf("at least one short ID is required")
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
	return nil
}

type LinkInput struct {
	Address    string
	Port       int
	UUID       string
	ServerName string
	Password   string
	ShortID    string
	Flow       string
	Label      string
}

// BuildVLESSLink builds vless link from the current inputs and defaults.
func BuildVLESSLink(in LinkInput) string {
	if in.Port == 0 {
		in.Port = 443
	}
	if in.Flow == "" {
		in.Flow = "xtls-rprx-vision"
	}
	label := in.Label
	if strings.TrimSpace(label) == "" {
		label = "ovpn"
	}
	// Keep pbk query key for broad client compatibility.
	return fmt.Sprintf(
		"vless://%s@%s:%d?security=reality&encryption=none&pbk=%s&fp=chrome&type=tcp&flow=%s&sni=%s&sid=%s#%s",
		in.UUID,
		in.Address,
		in.Port,
		in.Password,
		in.Flow,
		in.ServerName,
		in.ShortID,
		urlEscapeLabel(label),
	)
}

// urlEscapeLabel returns url escape label.
func urlEscapeLabel(v string) string {
	replacer := strings.NewReplacer(" ", "%20", "#", "%23")
	return replacer.Replace(v)
}
