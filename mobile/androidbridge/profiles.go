// Package androidbridge implements OVPN Tunnel's gomobile binding.
package androidbridge

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/xtls/xray-core/infra/conf"

	"ovpn/internal/clientcfg"
)

const (
	maxImportBytes = 1 << 20
	maxProfiles    = 256
	maxLinkBytes   = 8192
)

type profile struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Host      string `json:"host"`
	Transport string `json:"transport"`
	Link      string `json:"link,omitempty"`
	Issue     string `json:"issue,omitempty"`
}

type inspection struct {
	Profiles []profile `json:"profiles,omitempty"`
	Issue    string    `json:"issue,omitempty"`
}

// Inspect returns machine-readable display metadata and accepted links for local
// encrypted storage. The returned value contains credentials and must stay local.
func Inspect(input string) string {
	result, _ := json.Marshal(inspect(input))
	return string(result)
}

func inspect(input string) inspection {
	if len(input) > maxImportBytes {
		return inspection{Issue: "too_large"}
	}
	input = strings.TrimSpace(strings.TrimPrefix(input, "\uFEFF"))
	if !strings.Contains(input, "://") {
		decoded, err := base64.StdEncoding.DecodeString(input)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(input)
		}
		if err != nil {
			return inspection{Issue: "invalid_import"}
		}
		input = strings.TrimPrefix(string(decoded), "\uFEFF")
	}

	var result inspection
	seen := make(map[string]bool)
	count := 0
	for line := range strings.SplitSeq(input, "\n") {
		link := strings.TrimSpace(line)
		if link == "" {
			continue
		}
		count++
		if count > maxProfiles {
			return inspection{Issue: "too_many"}
		}
		item, _ := parseLink(link)
		if item.Issue == "invalid_profile" {
			return inspection{Issue: item.Issue}
		}
		if item.ID != "" {
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
		}
		result.Profiles = append(result.Profiles, item)
	}
	if count == 0 {
		return inspection{Issue: "invalid_import"}
	}
	return result
}

func parseLink(link string) (profile, clientcfg.Config) {
	invalid := profile{Issue: "invalid_profile"}
	if len(link) > maxLinkBytes || !utf8.ValidString(link) || strings.ContainsFunc(link, unicode.IsSpace) {
		return invalid, clientcfg.Config{}
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "vless" || u.User == nil || u.Path != "" ||
		!validHost(u.Hostname()) {
		return invalid, clientcfg.Config{}
	}
	if _, set := u.User.Password(); set {
		return invalid, clientcfg.Config{}
	}
	id, err := uuid.Parse(u.User.Username())
	if err != nil || id == uuid.Nil {
		return invalid, clientcfg.Config{}
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return invalid, clientcfg.Config{}
	}
	for _, values := range q {
		if len(values) != 1 || strings.ContainsFunc(values[0], unicode.IsControl) {
			return invalid, clientcfg.Config{}
		}
	}
	network, security := q.Get("type"), q.Get("security")
	if network == "" || security == "" || q.Get("encryption") == "" {
		return invalid, clientcfg.Config{}
	}
	item := profile{Name: displayName(u.Fragment), Host: u.Hostname(), Transport: "unsupported"}
	if item.Name == "" {
		item.Name = item.Host
	}
	switch network {
	case "xhttp":
		item.Transport = "xhttp"
		item.Issue = "xhttp_deferred"
		if security == "none" && q.Get("encryption") == "none" {
			item.Issue = "xhttp_unencrypted"
		}
	case "tcp":
		if security != "tls" && security != "reality" || q.Get("encryption") != "none" ||
			q.Get("flow") != "xtls-rprx-vision" || (q.Get("headerType") != "" && q.Get("headerType") != "none") {
			item.Issue = "unsupported_profile"
		} else {
			item.Transport = security
		}
	default:
		item.Issue = "unsupported_profile"
	}
	// Validate even the unsupported OVPN transport variants before accepting a
	// subscription update. A broken URI must not replace a previous snapshot.
	u.User = url.User(id.String())
	u.Host = net.JoinHostPort(strings.ToLower(u.Hostname()), u.Port())
	if network == "tcp" || network == "xhttp" {
		if security == "tls" || security == "reality" {
			if !validHost(q.Get("sni")) || net.ParseIP(q.Get("sni")) != nil {
				return invalid, clientcfg.Config{}
			}
			q.Set("sni", strings.ToLower(q.Get("sni")))
		}
	}
	u.RawQuery = q.Encode()
	if item.Issue != "" {
		if network == "xhttp" {
			if _, err := clientcfg.FromLink(u.String()); err != nil {
				return invalid, clientcfg.Config{}
			}
		}
		if port, err := strconv.ParseUint(u.Port(), 10, 16); err != nil || port == 0 {
			return invalid, clientcfg.Config{}
		}
		return item, clientcfg.Config{}
	}

	for key := range q {
		switch key {
		case "type", "security", "encryption", "flow", "headerType", "sni", "fp":
		case "alpn":
			if security != "tls" {
				item.Issue = "unsupported_profile"
			}
		case "pbk", "sid", "spx":
			if security != "reality" {
				item.Issue = "unsupported_profile"
			}
		default:
			item.Issue = "unsupported_profile"
		}
	}

	cfg, err := clientcfg.FromLink(u.String())
	if err != nil {
		return invalid, clientcfg.Config{}
	}
	cfg.Remarks = ""
	raw, err := cfg.JSON()
	if err != nil {
		return invalid, clientcfg.Config{}
	}
	var xray conf.Config
	if err := json.Unmarshal(raw, &xray); err != nil {
		return invalid, clientcfg.Config{}
	}
	if _, err := xray.OutboundConfigs[0].Build(); err != nil {
		return invalid, clientcfg.Config{}
	}
	if item.Issue != "" {
		return item, clientcfg.Config{}
	}
	key := sha256.Sum256(raw)
	item.ID = hex.EncodeToString(key[:])
	item.Link = link
	return item, cfg
}

func displayName(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if b.Len() >= 120 {
			break
		}
		if !unicode.IsControl(r) && !unicode.Is(unicode.Cf, r) {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func validHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast()
	}
	if len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for part := range strings.SplitSeq(host, ".") {
		if len(part) < 1 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}
