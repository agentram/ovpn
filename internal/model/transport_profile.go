package model

import "strings"

var transportProfiles = []TransportProfile{
	{
		Name:        TransportProfileRealityTCPVision,
		Kind:        "reality",
		Status:      "deprecated",
		Port:        443,
		InboundTag:  "vless-reality",
		Description: "VLESS over TCP with REALITY and xtls-rprx-vision flow; kept for existing links and client compatibility.",
	},
	{
		Name:        TransportProfileRealityXHTTP,
		Kind:        "reality",
		Status:      "experimental",
		Port:        8443,
		InboundTag:  "vless-reality-xhttp",
		Description: "VLESS over XHTTP with REALITY for clients that support XHTTP.",
	},
	{
		Name:        TransportProfilePlainXHTTP,
		Kind:        "plain-xhttp",
		Status:      "fallback",
		Port:        13179,
		InboundTag:  "vless-xhttp-plain",
		Description: "VLESS over XHTTP without stream security on a high port; operator-controlled fallback for degraded REALITY paths.",
	},
	{
		Name:        TransportProfileWSTLSWeb,
		Kind:        "tls-web",
		Status:      "planned",
		Port:        8445,
		InboundTag:  "vless-ws-tls-web",
		Description: "VLESS over WebSocket/TLS behind a normal HTTPS web endpoint; requires certificates and a web front end.",
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

// NormalizeTransportProfile canonicalizes a profile name.
func NormalizeTransportProfile(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "", "default", "tcp", "reality", "vision", "vless-reality", TransportProfileRealityTCPVision:
		return TransportProfileRealityTCPVision
	case "xhttp", "reality-xhttp", TransportProfileRealityXHTTP:
		return TransportProfileRealityXHTTP
	case "plain-xhttp", "xhttp-plain", "emergency-xhttp", TransportProfilePlainXHTTP:
		return TransportProfilePlainXHTTP
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
