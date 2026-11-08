package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"ovpn/internal/clientcfg"

	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	_ "github.com/xtls/xray-core/proxy/vless/outbound"
	_ "github.com/xtls/xray-core/transport/internet/headers/noop"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/tagged/taggedimpl"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/tls"
)

type Tunnel struct{ instance *core.Instance }

// Open starts a private client instance without opening a listening port.
func Open(ctx context.Context, clientConfig clientcfg.Config) (*Tunnel, error) {
	if len(clientConfig.Outbounds) != 1 || clientConfig.Outbounds[0]["protocol"] != "vless" {
		return nil, fmt.Errorf("traffic probes require one VLESS outbound")
	}
	stream, ok := clientConfig.Outbounds[0]["streamSettings"].(map[string]any)
	if !ok || stream["network"] != "tcp" || stream["security"] != "reality" && stream["security"] != "tls" {
		return nil, fmt.Errorf("built-in probes support TCP/REALITY and TCP/TLS; export XHTTP profiles to Xray Checker")
	}
	raw, err := clientConfig.JSON()
	if err != nil {
		return nil, fmt.Errorf("invalid client configuration")
	}
	var config conf.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("invalid client configuration")
	}
	built, err := config.Build()
	if err != nil {
		return nil, fmt.Errorf("client configuration rejected by the pinned Xray core")
	}
	instance, err := core.NewWithContext(ctx, built)
	if err != nil {
		return nil, fmt.Errorf("could not initialize the client Xray core")
	}
	if err := instance.Start(); err != nil {
		_ = instance.Close()
		return nil, fmt.Errorf("could not start the client Xray core")
	}
	return &Tunnel{instance: instance}, nil
}

func (t *Tunnel) Close() error { return t.instance.Close() }

func (t *Tunnel) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("traffic probes require TCP")
	}
	destination, err := xnet.ParseDestination("tcp:" + address)
	if err != nil {
		return nil, fmt.Errorf("invalid probe destination")
	}
	return core.Dial(ctx, t.instance, destination)
}
