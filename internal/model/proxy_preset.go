package model

import "strings"

const (
	ProxyPresetRU = "ru"
)

// NormalizeProxyPreset normalizes proxy preset and applies fallback defaults.
func NormalizeProxyPreset(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ProxyPresetRU:
		return ProxyPresetRU
	default:
		return ""
	}
}

// NormalizedProxyPreset returns the normalized proxy preset for proxy role servers.
func (s Server) NormalizedProxyPreset() string {
	if !s.IsProxy() {
		return ""
	}
	return NormalizeProxyPreset(s.ProxyPreset)
}
