package model

import "strings"

const (
	TransportProfileStatusDefault        = "default"
	TransportProfileStatusFallback       = "fallback"
	TransportProfileStatusCamouflage     = "camouflage"
	TransportProfileStatusDecommissioned = "decomm"
)

var transportProfiles = []TransportProfile{
	{
		Name:        TransportProfileRealityTCPVision,
		Kind:        "reality",
		Status:      TransportProfileStatusDefault,
		Port:        443,
		InboundTag:  "vless-reality",
		Description: "Default VLESS over TCP with REALITY and xtls-rprx-vision flow; kept for compatibility and as the baseline REALITY profile.",
	},
	{
		Name:        TransportProfileRealityXHTTP,
		Kind:        "reality",
		Status:      TransportProfileStatusDecommissioned,
		Port:        8443,
		InboundTag:  "vless-reality-xhttp",
		Description: "Decommissioned REALITY/XHTTP profile; retained only so older local state can be identified and cleaned up.",
	},
	{
		Name:        TransportProfilePlainXHTTP,
		Kind:        "plain-xhttp",
		Status:      TransportProfileStatusFallback,
		Port:        13179,
		InboundTag:  "vless-xhttp-plain",
		Description: "VLESS over XHTTP without stream security on a high port; operator-controlled fallback for degraded REALITY paths.",
	},
	{
		Name:        TransportProfileTLSSelfSNIWeb,
		Kind:        "tls-selfsni-web",
		Status:      TransportProfileStatusCamouflage,
		Port:        443,
		InboundTag:  "vless-tcp-tls-selfsni-web",
		Description: "VLESS over TCP/TLS with xtls-rprx-vision and HTTPS fallback to a normal internal web service.",
	},
	{
		Name:        TransportProfileWSTLSWeb,
		Kind:        "tls-web",
		Status:      TransportProfileStatusDecommissioned,
		Port:        8445,
		InboundTag:  "vless-ws-tls-web",
		Description: "Decommissioned WebSocket/TLS web-front idea; use vless-tcp-tls-selfsni-web for HTTPS fallback camouflage.",
	},
}

// SupportedTransportProfiles returns all known profiles in stable display order.
func SupportedTransportProfiles() []TransportProfile {
	out := make([]TransportProfile, len(transportProfiles))
	copy(out, transportProfiles)
	return out
}

// SupportedTransportProfilesText returns supported profile names for help text and errors.
func SupportedTransportProfilesText() string {
	names := make([]string, 0, len(transportProfiles))
	for _, profile := range transportProfiles {
		names = append(names, profile.Name)
	}
	return strings.Join(names, ", ")
}

// LookupTransportProfile returns profile metadata and whether it exists.
func LookupTransportProfile(name string) (TransportProfile, bool) {
	name = NormalizeTransportProfile(name)
	for _, p := range transportProfiles {
		if p.Name == name {
			return p, true
		}
	}
	return TransportProfile{}, false
}

// Deployable reports whether the profile may be enabled, rendered, and used for new links.
func (p TransportProfile) Deployable() bool {
	return p.Status != TransportProfileStatusDecommissioned
}

// NormalizeTransportProfile canonicalizes a profile name.
func NormalizeTransportProfile(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "", "default", "tcp", "reality", "vision", "vless-reality", TransportProfileRealityTCPVision:
		return TransportProfileRealityTCPVision
	case "reality-xhttp", TransportProfileRealityXHTTP:
		return TransportProfileRealityXHTTP
	case "xhttp", "plain-xhttp", "xhttp-plain", "emergency-xhttp", TransportProfilePlainXHTTP:
		return TransportProfilePlainXHTTP
	case "tls", "selfsni", "self-sni", "tls-selfsni", "tls-selfsni-web", "vless-tls", TransportProfileTLSSelfSNIWeb:
		return TransportProfileTLSSelfSNIWeb
	case "ws", "websocket", "ws-tls", "web", TransportProfileWSTLSWeb:
		return TransportProfileWSTLSWeb
	default:
		return ""
	}
}

// NormalizeEnabledProfiles returns unique enabled profile names in display order.
func NormalizeEnabledProfiles(primary string, raw string) []string {
	seen := map[string]bool{}
	var candidates []string
	if strings.TrimSpace(raw) != "" {
		candidates = append(candidates, strings.Split(raw, ",")...)
	}
	candidates = append([]string{primary}, candidates...)
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name := NormalizeTransportProfile(candidate)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		out = append(out, TransportProfileRealityTCPVision)
	}
	return sortProfilesByKnownOrder(out)
}

// EnabledProfilesCSV returns a normalized comma-separated profile list.
func EnabledProfilesCSV(primary string, raw string) string {
	return strings.Join(NormalizeEnabledProfiles(primary, raw), ",")
}

// NormalizedPrimaryProfile returns the server's primary transport profile.
func (s Server) NormalizedPrimaryProfile() string {
	primary := NormalizeTransportProfile(s.PrimaryProfile)
	if primary == "" {
		return TransportProfileRealityTCPVision
	}
	return primary
}

// NormalizedEnabledProfiles returns the server's enabled transport profiles.
func (s Server) NormalizedEnabledProfiles() []string {
	return NormalizeEnabledProfiles(s.NormalizedPrimaryProfile(), s.EnabledProfiles)
}

// EnabledProfilesCSV returns normalized enabled profiles for persistence.
func (s Server) EnabledProfilesCSV() string {
	return EnabledProfilesCSV(s.NormalizedPrimaryProfile(), s.EnabledProfiles)
}

// IsTransportProfileEnabled reports whether the profile is enabled on the server.
func (s Server) IsTransportProfileEnabled(profile string) bool {
	name := NormalizeTransportProfile(profile)
	if name == "" {
		return false
	}
	for _, enabled := range s.NormalizedEnabledProfiles() {
		if enabled == name {
			return true
		}
	}
	return false
}

func sortProfilesByKnownOrder(in []string) []string {
	seen := map[string]bool{}
	for _, item := range in {
		seen[item] = true
	}
	out := make([]string, 0, len(in))
	for _, known := range transportProfiles {
		if seen[known.Name] {
			out = append(out, known.Name)
			delete(seen, known.Name)
		}
	}
	for _, item := range in {
		if seen[item] {
			out = append(out, item)
			delete(seen, item)
		}
	}
	return out
}
