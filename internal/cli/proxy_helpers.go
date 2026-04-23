package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"ovpn/internal/model"
	"ovpn/internal/util"
	"ovpn/internal/xraycfg"
)

const (
	proxyHAProxyListenPort = 15443
	proxyHAProxyStatsPort  = 8404
	proxyHAProxyAddress    = "haproxy"

	proxyGeodataRefreshInterval = 24 * time.Hour
	proxyGeodataWarnAfter       = 72 * time.Hour
)

type proxyPresetConfig struct {
	Key              string
	DirectDomainSets []string
	DirectIPSets     []string
	GeoSiteURL       string
	GeoIPURL         string
	GeoSiteCacheName string
	GeoIPCacheName   string
}

func proxyPresetConfigForServer(srv model.Server) (proxyPresetConfig, error) {
	if !srv.IsProxy() {
		return proxyPresetConfig{}, fmt.Errorf("server %s is role %s, expected proxy", srv.Name, srv.NormalizedRole())
	}
	switch srv.NormalizedProxyPreset() {
	case model.ProxyPresetRU:
		return proxyPresetConfig{
			Key:              model.ProxyPresetRU,
			DirectDomainSets: []string{"geosite:ru-available-only-inside", "regexp:.*\\.ru$", "regexp:.*\\.su$", "regexp:.*\\.xn--p1ai$"},
			DirectIPSets:     []string{"geoip:ru", "geoip:private"},
			GeoSiteURL:       "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geosite/release/geosite.dat",
			GeoIPURL:         "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geoip/release/geoip.dat",
			GeoSiteCacheName: "proxy-ru-geosite.dat",
			GeoIPCacheName:   "proxy-ru-geoip.dat",
		}, nil
	default:
		return proxyPresetConfig{}, fmt.Errorf("unsupported proxy preset %q", strings.TrimSpace(srv.ProxyPreset))
	}
}

func proxyServiceEmail() string {
	return "proxy-service@cluster"
}

func (a *App) vpnServiceUsers(srv model.Server) ([]xraycfg.ServiceUser, error) {
	if !srv.IsVPN() {
		return nil, nil
	}
	attached, err := a.store.BackendHasAttachedProxy(a.ctx, srv.ID)
	if err != nil {
		return nil, fmt.Errorf("check proxy attachments for %s: %w", srv.Name, err)
	}
	if !attached {
		return nil, nil
	}
	if strings.TrimSpace(srv.ProxyServiceUUID) == "" {
		return nil, fmt.Errorf("vpn backend %s is attached to a proxy but proxy_service_uuid is empty", srv.Name)
	}
	return []xraycfg.ServiceUser{{
		UUID:  srv.ProxyServiceUUID,
		Email: proxyServiceEmail(),
	}}, nil
}

func (a *App) ensureBackendProxyServiceUUID(backend *model.Server, existingBackends []model.Server) error {
	if !backend.IsVPN() {
		return nil
	}
	sharedUUID := ""
	multipleUUIDs := false
	for _, existing := range existingBackends {
		existingUUID := strings.TrimSpace(existing.ProxyServiceUUID)
		if existingUUID == "" {
			continue
		}
		if sharedUUID == "" {
			sharedUUID = existingUUID
			continue
		}
		if existingUUID != sharedUUID {
			multipleUUIDs = true
			break
		}
	}
	if multipleUUIDs {
		return nil
	}
	backendUUID := strings.TrimSpace(backend.ProxyServiceUUID)
	if sharedUUID == "" {
		sharedUUID = backendUUID
	}
	if sharedUUID == "" {
		sharedUUID = uuid.NewString()
	}
	for _, existing := range existingBackends {
		if strings.TrimSpace(existing.ProxyServiceUUID) != "" {
			continue
		}
		existing.ProxyServiceUUID = sharedUUID
		if err := a.store.UpdateServer(a.ctx, &existing); err != nil {
			return fmt.Errorf("assign proxy_service_uuid to backend %s: %w", existing.Name, err)
		}
	}
	if backendUUID == "" {
		backend.ProxyServiceUUID = sharedUUID
		if err := a.store.UpdateServer(a.ctx, backend); err != nil {
			return fmt.Errorf("assign proxy_service_uuid to backend %s: %w", backend.Name, err)
		}
	}
	return nil
}

func (a *App) attachedBackendServers(proxy model.Server) ([]model.Server, error) {
	if !proxy.IsProxy() {
		return nil, fmt.Errorf("server %s is role %s, expected proxy", proxy.Name, proxy.NormalizedRole())
	}
	return a.store.ListAttachedBackendServers(a.ctx, proxy.ID)
}

func (a *App) ensureVPNBackendsCompatible(backends []model.Server) error {
	if len(backends) == 0 {
		return fmt.Errorf("proxy server has no attached backends")
	}
	for _, backend := range backends {
		if !backend.IsVPN() {
			return fmt.Errorf("server %s is role %s, expected vpn backend", backend.Name, backend.NormalizedRole())
		}
	}
	base := backends[0]
	var issues []string
	for _, backend := range backends[1:] {
		diff := realityParityDiff(base, backend, true)
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

func (a *App) buildProxyRelay(proxy model.Server) (*xraycfg.ProxyRelay, []model.Server, error) {
	backends, err := a.attachedBackendServers(proxy)
	if err != nil {
		return nil, nil, err
	}
	if err := a.ensureVPNBackendsCompatible(backends); err != nil {
		return nil, nil, err
	}
	base := backends[0]
	shortID := firstShortID(base.RealityShortIDs)
	if shortID == "" {
		return nil, nil, fmt.Errorf("backend %s has no REALITY short id configured", base.Name)
	}
	return &xraycfg.ProxyRelay{
		Address:     proxyHAProxyAddress,
		Port:        proxyHAProxyListenPort,
		ServiceUUID: base.ProxyServiceUUID,
		ServerName:  base.RealityServerName,
		PublicKey:   base.RealityPublicKey,
		ShortID:     shortID,
	}, backends, nil
}

func (a *App) ensureProxyGeodataAssets(srv model.Server) (string, string, error) {
	preset, err := proxyPresetConfigForServer(srv)
	if err != nil {
		return "", "", err
	}
	geositePath, geoipPath, err := a.proxyGeodataPaths(srv)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(envOr("OVPN_PROXY_GEOSITE_PATH", "")) != "" {
		return geositePath, geoipPath, nil
	}
	cacheDir := filepath.Dir(geositePath)
	if err := util.EnsureDir(cacheDir); err != nil {
		return "", "", err
	}
	if err := a.ensureDownloadedAsset(a.ctx, envOr("OVPN_PROXY_GEOSITE_URL", preset.GeoSiteURL), geositePath, proxyGeodataRefreshInterval); err != nil {
		return "", "", err
	}
	if err := a.ensureDownloadedAsset(a.ctx, envOr("OVPN_PROXY_GEOIP_URL", preset.GeoIPURL), geoipPath, proxyGeodataRefreshInterval); err != nil {
		return "", "", err
	}
	return geositePath, geoipPath, nil
}

func (a *App) proxyGeodataPaths(srv model.Server) (string, string, error) {
	if geosite := strings.TrimSpace(envOr("OVPN_PROXY_GEOSITE_PATH", "")); geosite != "" {
		geoip := strings.TrimSpace(envOr("OVPN_PROXY_GEOIP_PATH", ""))
		if geoip == "" {
			return "", "", fmt.Errorf("OVPN_PROXY_GEOIP_PATH is required when OVPN_PROXY_GEOSITE_PATH is set")
		}
		if err := ensureReadableFile(geosite); err != nil {
			return "", "", err
		}
		if err := ensureReadableFile(geoip); err != nil {
			return "", "", err
		}
		return geosite, geoip, nil
	}
	preset, err := proxyPresetConfigForServer(srv)
	if err != nil {
		return "", "", err
	}
	cacheDir := filepath.Join(a.dataDir, "geodata")
	return filepath.Join(cacheDir, preset.GeoSiteCacheName), filepath.Join(cacheDir, preset.GeoIPCacheName), nil
}

func (a *App) proxyGeodataState(srv model.Server) ([]string, bool, error) {
	geositePath, geoipPath, err := a.proxyGeodataPaths(srv)
	if err != nil {
		return nil, false, err
	}
	preset, err := proxyPresetConfigForServer(srv)
	if err != nil {
		return nil, false, err
	}
	paths := []struct {
		label string
		path  string
	}{
		{label: "geosite", path: geositePath},
		{label: "geoip", path: geoipPath},
	}
	var details []string
	stale := false
	for _, item := range paths {
		st, err := os.Stat(item.path)
		if err != nil {
			return nil, false, fmt.Errorf("%s asset %s: %w", item.label, item.path, err)
		}
		age := time.Since(st.ModTime()).Round(time.Minute)
		details = append(details,
			fmt.Sprintf("%s_path=%s", item.label, item.path),
			fmt.Sprintf("%s_age=%s", item.label, age),
		)
		if age > proxyGeodataWarnAfter {
			stale = true
		}
	}
	details = append(details, "proxy_preset="+preset.Key)
	return details, stale, nil
}

func ensureReadableFile(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory, expected file", path)
	}
	return nil
}

func (a *App) ensureDownloadedAsset(ctx context.Context, url string, dst string, maxAge time.Duration) error {
	if st, err := os.Stat(dst); err == nil && time.Since(st.ModTime()) <= maxAge {
		return nil
	}
	tmp := dst + ".tmp"
	defer os.Remove(tmp)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
