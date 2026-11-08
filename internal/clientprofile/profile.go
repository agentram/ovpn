package clientprofile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"ovpn/internal/clientcfg"
	"ovpn/internal/model"
	"ovpn/internal/util"
	"ovpn/internal/xraycfg"
)

type Options struct {
	Fingerprint   string
	SpiderX       string
	NoSpiderX     bool
	LegacyReality bool
}

func ValidateCheckUser(srv model.Server, user model.User) error {
	if !srv.Enabled || !user.Enabled || model.IsExpiredAt(user.ExpiryDate, time.Now().UTC()) || user.QuotaBlocked {
		return fmt.Errorf("client checks require an enabled server and an active user")
	}
	return nil
}

func ConfigForUser(srv model.Server, user model.User, profile string, opts Options) (clientcfg.Config, error) {
	if err := ValidateCheckUser(srv, user); err != nil {
		return clientcfg.Config{}, err
	}
	link, err := Link(srv, user, profile, opts)
	if err != nil {
		return clientcfg.Config{}, err
	}
	return clientcfg.FromLink(link)
}

func unsupportedProfile(raw string) error {
	return fmt.Errorf("unsupported transport profile %q; supported profiles: %s", raw, model.SupportedTransportProfilesText())
}
func firstShortID(csv string) string {
	items := util.ParseCSV(csv)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func Link(srv model.Server, u model.User, profile string, opts ...Options) (string, error) {
	options := Options{}
	if len(opts) > 0 {
		options = opts[0]
	}
	rawProfile := strings.TrimSpace(profile)
	requestedProfile := rawProfile != ""
	if requestedProfile {
		profile = model.NormalizeTransportProfile(rawProfile)
		if profile == "" {
			return "", unsupportedProfile(rawProfile)
		}
	} else {
		profile = srv.NormalizedPrimaryProfile()
	}
	meta, ok := model.LookupTransportProfile(profile)
	if !ok {
		return "", unsupportedProfile(profile)
	}
	if !srv.IsTransportProfileEnabled(profile) {
		return "", fmt.Errorf("profile %s is not enabled on server %s; enable and deploy it first with `ovpn server profile enable %s %s` and `ovpn deploy %s`, or choose an enabled profile from `ovpn server profile list %s`", profile, srv.Name, srv.Name, profile, srv.Name, srv.Name)
	}
	if options.LegacyReality {
		if meta.Kind != "reality" {
			return "", fmt.Errorf("--legacy-reality is only supported for REALITY profiles; profile %s does not use it", profile)
		}
		if strings.TrimSpace(options.Fingerprint) != "" || strings.TrimSpace(options.SpiderX) != "" || options.NoSpiderX {
			return "", fmt.Errorf("--legacy-reality already sets --fingerprint chrome and omits spiderX; do not combine it with --fingerprint, --spider-x, or --no-spider-x")
		}
		options.Fingerprint = "chrome"
		options.NoSpiderX = true
	}
	fingerprint := ""
	if UsesClientFingerprint(profile) {
		fingerprint = xraycfg.NormalizeClientFingerprint(options.Fingerprint)
		if fingerprint == "" {
			return "", fmt.Errorf("unsupported --fingerprint %q; supported values: %s", strings.TrimSpace(options.Fingerprint), xraycfg.SupportedClientFingerprintsText())
		}
	} else if strings.TrimSpace(options.Fingerprint) != "" {
		return "", fmt.Errorf("--fingerprint is only supported for REALITY/TLS profiles; profile %s does not use it", profile)
	}
	address := srv.Domain
	if address == "" {
		address = srv.Host
	}
	var shortID string
	var spiderX string
	if meta.Kind == "reality" {
		shortID = firstShortID(srv.RealityShortIDs)
		if shortID == "" {
			return "", fmt.Errorf("server %s has no REALITY short-id configured for profile %s; check `ovpn server profile list %s` and try a non-REALITY profile such as %s if it is enabled", srv.Name, profile, srv.Name, model.TransportProfilePlainXHTTP)
		}
		if options.NoSpiderX && strings.TrimSpace(options.SpiderX) != "" {
			return "", fmt.Errorf("use only one of --spider-x or --no-spider-x")
		}
		var err error
		spiderX, err = NormalizeSpiderX(options.SpiderX)
		if err != nil {
			return "", err
		}
		if spiderX == "" && !options.NoSpiderX {
			spiderX = DefaultSpiderX(srv, u, profile)
		}
	} else if strings.TrimSpace(options.SpiderX) != "" {
		return "", fmt.Errorf("--spider-x is only supported for REALITY profiles; profile %s does not use it", profile)
	} else if options.NoSpiderX {
		return "", fmt.Errorf("--no-spider-x is only supported for REALITY profiles; profile %s does not use it", profile)
	}
	serverName := srv.RealityServerName
	if meta.Kind == "tls-selfsni-web" {
		serverName = address
	}
	encryption := ""
	if profile == model.TransportProfileVLESSEncXHTTP {
		encryption = strings.TrimSpace(srv.VLESSClientEncryption)
		if !model.IsValidVLESSEncryptionClientValue(encryption) {
			return "", fmt.Errorf("profile %s has no valid client encryption value on server %s; disable and re-enable the profile to provision its cluster key pair", profile, srv.Name)
		}
	}
	label := "ovpn-" + u.Username
	if requestedProfile || profile != model.TransportProfileRealityTCPVision {
		label += "-" + profile
	}
	return xraycfg.BuildVLESSLink(xraycfg.LinkInput{
		Address:     address,
		Port:        meta.Port,
		UUID:        u.UUID,
		ServerName:  serverName,
		Password:    srv.RealityPublicKey,
		ShortID:     shortID,
		Profile:     profile,
		Label:       label,
		Fingerprint: fingerprint,
		SpiderX:     spiderX,
		Encryption:  encryption,
	}), nil
}

func UsesClientFingerprint(profile string) bool {
	meta, ok := model.LookupTransportProfile(model.NormalizeTransportProfile(profile))
	if !ok {
		return false
	}
	return meta.Kind == "reality" || meta.Kind == "tls-selfsni-web"
}

func NormalizeSpiderX(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	if !strings.HasPrefix(v, "/") {
		return "", fmt.Errorf("--spider-x must start with /")
	}
	if strings.ContainsAny(v, " \t\r\n") {
		return "", fmt.Errorf("--spider-x must not contain spaces")
	}
	for _, r := range v {
		if r > 127 {
			return "", fmt.Errorf("--spider-x must be URL-safe ASCII")
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '/', '.', '_', '-', '~':
			continue
		default:
			return "", fmt.Errorf("--spider-x must be URL-safe ASCII path characters")
		}
	}
	return v, nil
}

func DefaultSpiderX(srv model.Server, u model.User, profile string) string {
	sum := sha256.Sum256([]byte(srv.Name + "\x00" + u.Username + "\x00" + model.NormalizeTransportProfile(profile)))
	return "/assets/" + hex.EncodeToString(sum[:])[:12] + ".js"
}
