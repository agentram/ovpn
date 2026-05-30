package model

import "strings"

const (
	ProxyPresetRU = "ru"
	ProxyPresetCN = "cn"
)

// NormalizeProxyPreset canonicalizes a proxy preset name, returning "" when unsupported.
func NormalizeProxyPreset(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ProxyPresetRU:
		return ProxyPresetRU
	case ProxyPresetCN, "china":
		return ProxyPresetCN
	default:
		return ""
	}
}

// SupportedProxyPresetsText returns supported proxy presets for errors and help text.
func SupportedProxyPresetsText() string {
	return strings.Join([]string{ProxyPresetRU, ProxyPresetCN}, ", ")
}

// NormalizedProxyPreset returns the normalized proxy preset for proxy role servers.
func (s Server) NormalizedProxyPreset() string {
	if !s.IsProxy() {
		return ""
	}
	return NormalizeProxyPreset(s.ProxyPreset)
}
