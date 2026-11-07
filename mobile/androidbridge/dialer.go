package androidbridge

import (
	"context"
	"errors"
	"net"
	"sync"
	"syscall"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport/internet"
)

// SocketProtector protects and binds a socket to the chosen physical network.
type SocketProtector interface {
	Protect(fd int32) bool
}

type dialTarget struct {
	address   xnet.Destination
	protector SocketProtector
	ctx       context.Context
	cancel    context.CancelFunc
}

type protectedDialer struct {
	mu     sync.RWMutex
	engine *core.Instance
	target *dialTarget
}

var errBlockedSocket = errors.New("outbound socket blocked")

func (d *protectedDialer) Dial(ctx context.Context, _ xnet.Address, dest xnet.Destination, _ *internet.SocketConfig) (net.Conn, error) {
	d.mu.RLock()
	target := d.target
	owner := d.engine
	d.mu.RUnlock()
	if target == nil || owner != core.FromContext(ctx) || dest != target.address || target.ctx.Err() != nil {
		return nil, errBlockedSocket
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(target.ctx, cancel)
	defer stop()
	dialer := net.Dialer{
		Timeout: 8 * time.Second,
		ControlContext: func(_ context.Context, _, _ string, c syscall.RawConn) error {
			return protectSocket(c, target.protector)
		},
	}
	conn, err := dialer.DialContext(ctx, "tcp", dest.NetAddr())
	if err != nil {
		return nil, err
	}
	// Shut down in-flight traffic as soon as the owning engine is retired.
	return &ownedConn{Conn: conn, stop: context.AfterFunc(target.ctx, func() { _ = conn.Close() })}, nil
}

func (*protectedDialer) DestIpAddress() xnet.IP { return nil }

func protectSocket(raw syscall.RawConn, protector SocketProtector) error {
	var allowed bool
	if err := raw.Control(func(fd uintptr) { allowed = protector.Protect(int32(fd)) }); err != nil {
		return err
	}
	if !allowed {
		return errBlockedSocket
	}
	return nil
}

type ownedConn struct {
	net.Conn
	stop func() bool
}

func (c *ownedConn) Close() error {
	c.stop()
	return c.Conn.Close()
}
