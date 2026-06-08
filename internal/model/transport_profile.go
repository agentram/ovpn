package model

import (
	"slices"
	"strings"
)

const (
	TransportProfileRealityTCPVision = "vless-reality-tcp-vision"
	TransportProfileXHTTPPlain       = "vless-xhttp-plain"
	TransportProfileRealityXHTTP     = "vless-reality-xhttp"
	TransportProfileGRPCReality      = "vless-grpc-reality"
	TransportProfileWSTLSWeb         = "vless-ws-tls-web"
)

type TransportProfile struct {
	Name        string
	Status      string
	Port        int
	Description string
}

func SupportedTransportProfiles() []TransportProfile {
	return []TransportProfile{
		{
			Name:        TransportProfileRealityTCPVision,
			Status:      "stable",
			Port:        443,
			Description: "VLESS over TCP with REALITY and xtls-rprx-vision flow.",
		},
		{
			Name:        TransportProfileXHTTPPlain,
			Status:      "fallback",
			Port:        13179,
			Description: "VLESS over XHTTP without stream security on a high port.",
		},
		{
			Name:        TransportProfileRealityXHTTP,
			Status:      "experimental",
			Port:        8443,
			Description: "VLESS over XHTTP with REALITY for clients that support it.",
		},
		{
			Name:        TransportProfileGRPCReality,
			Status:      "deprecated",
			Port:        8444,
			Description: "VLESS over gRPC with REALITY; kept only for old test links.",
		},
		{
			Name:        TransportProfileWSTLSWeb,
			Status:      "planned",
			Port:        8445,
			Description: "VLESS over WebSocket/TLS behind a normal HTTPS endpoint.",
		},
	}
}

func DefaultPrimaryTransportProfile() string {
	return TransportProfileRealityTCPVision
}

func DefaultEnabledTransportProfiles() string {
	return TransportProfileRealityTCPVision
}

func NormalizeTransportProfile(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	for _, p := range SupportedTransportProfiles() {
		if raw == p.Name {
			return p.Name
		}
	}
	return ""
}

func TransportProfileByName(raw string) (TransportProfile, bool) {
	name := NormalizeTransportProfile(raw)
	if name == "" {
		return TransportProfile{}, false
	}
	for _, p := range SupportedTransportProfiles() {
		if p.Name == name {
			return p, true
		}
	}
	return TransportProfile{}, false
}

func RenderSupportedTransportProfiles() []string {
	return []string{
		TransportProfileRealityTCPVision,
		TransportProfileXHTTPPlain,
	}
}

func TransportProfileRenderSupported(profile string) bool {
	profile = NormalizeTransportProfile(profile)
	return slices.Contains(RenderSupportedTransportProfiles(), profile)
}

func RenderSupportedTransportProfilesText() string {
	return strings.Join(RenderSupportedTransportProfiles(), ", ")
}

func ParseTransportProfilesCSV(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		name := NormalizeTransportProfile(part)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return []string{DefaultPrimaryTransportProfile()}
	}
	return out
}

func JoinTransportProfiles(profiles []string) string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range profiles {
		name := NormalizeTransportProfile(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		out = append(out, DefaultPrimaryTransportProfile())
	}
	return strings.Join(out, ",")
}

func TransportProfileEnabled(enabledCSV string, profile string) bool {
	profile = NormalizeTransportProfile(profile)
	if profile == "" {
		return false
	}
	return slices.Contains(ParseTransportProfilesCSV(enabledCSV), profile)
}

func EffectivePrimaryTransportProfile(primary string, enabledCSV string) string {
	primary = NormalizeTransportProfile(primary)
	enabled := ParseTransportProfilesCSV(enabledCSV)
	if primary != "" && slices.Contains(enabled, primary) {
		return primary
	}
	return enabled[0]
}
