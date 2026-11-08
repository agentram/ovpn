package clientcfg

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Config contains client credentials only, without listeners or a direct fallback.
type Config struct {
	Remarks   string           `json:"remarks,omitempty"`
	Log       map[string]any   `json:"log"`
	Outbounds []map[string]any `json:"outbounds"`
}

// FromLink translates an ovpn VLESS link into the Xray JSON accepted by checker.
func FromLink(link string) (Config, error) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "vless" || u.Hostname() == "" || u.User == nil {
		return Config{}, fmt.Errorf("invalid VLESS client link")
	}
	if _, err := uuid.Parse(u.User.Username()); err != nil {
		return Config{}, fmt.Errorf("invalid client UUID")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("invalid client port")
	}
	q := u.Query()
	network, security := q.Get("type"), q.Get("security")
	if network != "tcp" && network != "xhttp" {
		return Config{}, fmt.Errorf("unsupported client transport")
	}
	stream := map[string]any{"network": network, "security": security}
	switch security {
	case "reality":
		if q.Get("sni") == "" || q.Get("pbk") == "" || q.Get("sid") == "" || q.Get("fp") == "" {
			return Config{}, fmt.Errorf("incomplete REALITY client parameters")
		}
		stream["realitySettings"] = map[string]any{
			"serverName": q.Get("sni"), "password": q.Get("pbk"),
			"shortId": q.Get("sid"), "fingerprint": q.Get("fp"), "spiderX": q.Get("spx"),
		}
	case "tls":
		if q.Get("sni") == "" || q.Get("fp") == "" {
			return Config{}, fmt.Errorf("incomplete TLS client parameters")
		}
		tls := map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp")}
		if alpn := q.Get("alpn"); alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
		stream["tlsSettings"] = tls
	case "none":
		if network != "xhttp" {
			return Config{}, fmt.Errorf("unprotected client transport is not supported")
		}
	default:
		return Config{}, fmt.Errorf("unsupported client security")
	}
	if network == "xhttp" {
		stream["xhttpSettings"] = map[string]any{"path": q.Get("path"), "mode": q.Get("mode")}
	}
	encryption := q.Get("encryption")
	if encryption == "" {
		return Config{}, fmt.Errorf("client encryption setting is required")
	}
	user := map[string]any{"id": u.User.Username(), "encryption": encryption}
	if flow := q.Get("flow"); flow != "" {
		user["flow"] = flow
	}
	return Config{
		Remarks: u.Fragment,
		Log:     map[string]any{"loglevel": "none", "access": "none", "error": "none"},
		Outbounds: []map[string]any{{
			"tag": "vpn", "protocol": "vless",
			"settings": map[string]any{"vnext": []any{map[string]any{
				"address": u.Hostname(), "port": port, "users": []any{user},
			}}},
			"streamSettings": stream,
		}},
	}, nil
}

func (c Config) JSON() ([]byte, error) { return json.MarshalIndent(c, "", "  ") }
