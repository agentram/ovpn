// Package ha defines the shared backend-pool validation and application order.
package ha

import (
	"errors"
	"fmt"
	"ovpn/internal/model"
	"strings"
)

var ErrInvalid = errors.New("INVALID_PARAMS")
var ErrRequired = errors.New("HA_BACKENDS_REQUIRED")
var ErrIncompatible = errors.New("HA_BACKENDS_INCOMPATIBLE")

// Plan returns the reviewed backend order followed by the entry server.
func Plan(proxy model.Server, backends []model.Server) ([]model.Server, error) {
	if !proxy.IsProxy() || !proxy.Enabled {
		return nil, ErrInvalid
	}
	if len(backends) == 0 {
		return nil, ErrRequired
	}
	if ValidatePool(backends) != nil {
		return nil, ErrIncompatible
	}
	for _, node := range backends {
		if strings.TrimSpace(node.ProxyServiceUUID) == "" {
			return nil, ErrIncompatible
		}
	}
	return append(append([]model.Server(nil), backends...), proxy), nil
}

// ValidatePool retains the existing CLI compatibility rules and field-level explanations.
func ValidatePool(backends []model.Server) error {
	if len(backends) == 0 {
		return fmt.Errorf("proxy server has no attached backends")
	}
	for _, backend := range backends {
		if !backend.IsVPN() {
			return fmt.Errorf("server %s is role %s, expected vpn backend", backend.Name, backend.NormalizedRole())
		}
		if !backend.IsTransportProfileEnabled(model.TransportProfileRealityTCPVision) {
			return fmt.Errorf("vpn backend %s requires %s on 443/tcp for the proxy relay", backend.Name, model.TransportProfileRealityTCPVision)
		}
	}
	base := backends[0]
	var issues []string
	for _, backend := range backends[1:] {
		diff := []string{}
		for _, field := range []struct{ name, left, right string }{{"reality_private_key", base.RealityPrivateKey, backend.RealityPrivateKey}, {"reality_public_key", base.RealityPublicKey, backend.RealityPublicKey}, {"reality_short_ids", base.RealityShortIDs, backend.RealityShortIDs}, {"proxy_service_uuid", base.ProxyServiceUUID, backend.ProxyServiceUUID}} {
			if strings.TrimSpace(field.left) != strings.TrimSpace(field.right) {
				diff = append(diff, field.name)
			}
		}
		if len(diff) == 0 {
			continue
		}
		issues = append(issues, fmt.Sprintf("server %s differs from %s in: %s", backend.Name, base.Name, strings.Join(diff, ", ")))
	}
	if len(issues) > 0 {
		return fmt.Errorf("REALITY parity check failed:\n- %s", strings.Join(issues, "\n- "))
	}
	return nil
}
