package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"ovpn/internal/model"
	"ovpn/internal/util"
	"ovpn/internal/xraycfg"
)

// buildXraySpec builds xray spec from the current inputs and defaults.
func (a *App) buildXraySpec(srv model.Server, users []model.User) (xraycfg.Spec, error) {
	fallbackUpload, fallbackDownload, err := a.realityFallbackRateLimits()
	if err != nil {
		return xraycfg.Spec{}, err
	}
	securityProfile, err := parseSecurityProfileFromEnv()
	if err != nil {
		return xraycfg.Spec{}, err
	}
	var threatDNSServers []string
	if securityProfile == xraycfg.SecurityProfileMinimal {
		threatDNSServers, err = parseThreatDNSServersFromEnv()
		if err != nil {
			return xraycfg.Spec{}, err
		}
	}
	spec := xraycfg.Spec{
		Role:                  srv.NormalizedRole(),
		Domain:                srv.Domain,
		RealityPrivateKey:     srv.RealityPrivateKey,
		RealityPublicKey:      srv.RealityPublicKey,
		RealityServerName:     srv.RealityServerName,
		RealityTarget:         srv.RealityTarget,
		SecurityProfile:       securityProfile,
		ThreatDNSServers:      threatDNSServers,
		LimitFallbackUpload:   fallbackUpload,
		LimitFallbackDownload: fallbackDownload,
		ShortIDs:              util.ParseCSV(srv.RealityShortIDs),
		APIListen:             "0.0.0.0",
		APIPort:               10085,
		LogLevel:              a.xrayLogLevel(),
		Users:                 users,
	}
	serviceUsers, err := a.vpnServiceUsers(srv)
	if err != nil {
		return xraycfg.Spec{}, err
	}
	if len(serviceUsers) > 0 {
		spec.ServiceUsers = serviceUsers
	}
	if srv.IsProxy() {
		relay, _, err := a.buildProxyRelay(srv)
		if err != nil {
			return xraycfg.Spec{}, err
		}
		spec.ProxyRelay = relay
	}
	return spec, nil
}

// realityFallbackRateLimits returns reality fallback rate limits.
func (a *App) realityFallbackRateLimits() (*xraycfg.FallbackRateLimit, *xraycfg.FallbackRateLimit, error) {
	upload, err := parseFallbackRateLimitFromEnv("OVPN_REALITY_LIMIT_FALLBACK_UPLOAD")
	if err != nil {
		return nil, nil, err
	}
	download, err := parseFallbackRateLimitFromEnv("OVPN_REALITY_LIMIT_FALLBACK_DOWNLOAD")
	if err != nil {
		return nil, nil, err
	}
	return upload, download, nil
}

// parseFallbackRateLimitFromEnv parses fallback rate limit from env and returns normalized values.
func parseFallbackRateLimitFromEnv(prefix string) (*xraycfg.FallbackRateLimit, error) {
	after, afterSet, err := parseNonNegativeInt64Env(prefix + "_AFTER_BYTES")
	if err != nil {
		return nil, err
	}
	bps, bpsSet, err := parseNonNegativeInt64Env(prefix + "_BYTES_PER_SEC")
	if err != nil {
		return nil, err
	}
	burst, burstSet, err := parseNonNegativeInt64Env(prefix + "_BURST_BYTES_PER_SEC")
	if err != nil {
		return nil, err
	}
	if !afterSet && !bpsSet && !burstSet {
		return nil, nil
	}
	return &xraycfg.FallbackRateLimit{
		AfterBytes:       after,
		BytesPerSec:      bps,
		BurstBytesPerSec: burst,
	}, nil
}

// parseNonNegativeInt64Env parses non negative int 64 env and returns normalized values.
func parseNonNegativeInt64Env(key string) (int64, bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return 0, false, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, true, fmt.Errorf("%s must be an integer >= 0", key)
	}
	if v < 0 {
		return 0, true, fmt.Errorf("%s must be >= 0", key)
	}
	return v, true, nil
}

// parseSecurityProfileFromEnv parses security profile from env and returns normalized values.
func parseSecurityProfileFromEnv() (string, error) {
	raw := strings.TrimSpace(envOr("OVPN_SECURITY_PROFILE", xraycfg.SecurityProfileMinimal))
	switch strings.ToLower(raw) {
	case "", xraycfg.SecurityProfileMinimal:
		return xraycfg.SecurityProfileMinimal, nil
	case xraycfg.SecurityProfileOff:
		return xraycfg.SecurityProfileOff, nil
	default:
		return "", fmt.Errorf("OVPN_SECURITY_PROFILE must be %q or %q", xraycfg.SecurityProfileMinimal, xraycfg.SecurityProfileOff)
	}
}

// parseThreatDNSServersFromEnv parses threat dns servers from env and returns normalized values.
func parseThreatDNSServersFromEnv() ([]string, error) {
	servers := util.ParseCSV(envOr("OVPN_THREAT_DNS_SERVERS", strings.Join([]string{"9.9.9.9", "149.112.112.112"}, ",")))
	if len(servers) == 0 {
		return nil, errors.New("OVPN_THREAT_DNS_SERVERS must contain at least one DNS server")
	}
	for _, raw := range servers {
		server := strings.TrimSpace(raw)
		if server == "" {
			return nil, errors.New("OVPN_THREAT_DNS_SERVERS must not contain empty values")
		}
		if strings.Contains(server, "://") {
			return nil, fmt.Errorf("OVPN_THREAT_DNS_SERVERS entry %q must not contain URI scheme", server)
		}
	}
	return servers, nil
}
