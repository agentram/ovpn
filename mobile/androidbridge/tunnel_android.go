package androidbridge

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strconv"
	"sync"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/platform"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/transport/internet"
	"golang.org/x/sys/unix"

	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/tun"
	_ "github.com/xtls/xray-core/proxy/vless/outbound"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
)

const mtu = 1280

var (
	tunnelMu     sync.Mutex
	engine       *core.Instance
	tunnelFD     = -1
	dialer       protectedDialer
	engineCancel context.CancelFunc
)

func init() { internet.UseAlternativeSystemDialer(&dialer) }

// CoreVersion reports the embedded Xray engine version.
func CoreVersion() string { return core.Version() }

// InterfaceName identifies the service-owned TUN for VPN-bound health checks.
func InterfaceName(fd int32) string {
	ifr, err := unix.NewIfreq("")
	if err != nil || unix.IoctlIfreq(int(fd), unix.TUNGETIFF, ifr) != nil {
		return ""
	}
	return ifr.Name()
}

// Start duplicates the service-owned TUN FD. Kotlin retains the original across
// reconnects. Start and Stop return only stable codes, never engine log strings.
func Start(link, endpointIP string, fd int32, protector SocketProtector) string {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	if engine != nil {
		return "engine_busy"
	}
	item, cfg := parseLink(link)
	ip := net.ParseIP(endpointIP)
	if item.Issue != "" || ip == nil || protector == nil {
		return "invalid_profile"
	}

	remote := cfg.Outbounds[0]["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)
	remote["address"] = ip.String()
	port := remote["port"].(int)
	raw, err := cfg.JSON()
	if err != nil {
		return "engine_config"
	}
	var parsed conf.Config
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "engine_config"
	}
	if err := json.Unmarshal([]byte(`[{"port":0,"protocol":"tun","settings":{"name":"ovpn","mtu":`+strconv.Itoa(mtu)+`}}]`), &parsed.InboundConfigs); err != nil {
		return "engine_config"
	}
	config, err := parsed.Build()
	if err != nil {
		return "engine_config"
	}

	dup, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return "tun_failed"
	}
	// The Android TUN constructor also sets nonblocking. Preflight it while we
	// still own cleanup, since upstream closes the descriptor if that call fails.
	if err := unix.SetNonblock(dup, true); err != nil {
		_ = unix.Close(dup)
		return "tun_failed"
	}
	tunnelFD = dup
	if err := os.Setenv(platform.TunFdKey, strconv.Itoa(dup)); err != nil {
		stopLocked()
		return "tun_failed"
	}
	defer os.Unsetenv(platform.TunFdKey)

	engine, err = core.New(config)
	if err != nil {
		stopLocked()
		return "engine_start"
	}
	ctx, cancel := context.WithCancel(context.Background())
	engineCancel = cancel
	dialer.mu.Lock()
	dialer.engine = engine
	dialer.target = &dialTarget{
		address:   xnet.TCPDestination(xnet.IPAddress(ip), xnet.Port(port)),
		protector: protector, ctx: ctx,
	}
	dialer.mu.Unlock()
	if err := engine.Start(); err != nil {
		stopLocked()
		return "engine_start"
	}
	return ""
}

// Status reports whether the native packet engine has started.
func Status() string {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	if engine != nil {
		return "running"
	}
	return "stopped"
}

// Stop closes the engine and its descriptor without releasing the service TUN.
func Stop() {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	stopLocked()
}

func stopLocked() {
	dialer.mu.Lock()
	if engineCancel != nil {
		engineCancel()
		engineCancel = nil
	}
	dialer.target, dialer.engine = nil, nil
	dialer.mu.Unlock()
	if engine != nil {
		_ = engine.Close()
		engine = nil
	}
	if tunnelFD >= 0 {
		_ = unix.Close(tunnelFD)
		tunnelFD = -1
	}
}
