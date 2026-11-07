package androidbridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
)

type protectFunc func(fd int32) bool

func (f protectFunc) Protect(fd int32) bool { return f(fd) }

func TestProtectedDialRejectsBeforeConnect(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dest := xnet.DestinationFromAddr(l.Addr())
	var calls int
	target := &dialTarget{address: dest, ctx: ctx, protector: protectFunc(func(fd int32) bool {
		calls++
		return false
	})}
	d := &protectedDialer{target: target}

	wrong := dest
	wrong.Port++
	if _, err := d.Dial(ctx, nil, wrong, nil); !errors.Is(err, errBlockedSocket) || calls != 0 {
		t.Fatal("unapproved destinations must be blocked before opening a socket")
	}
	if _, err := d.Dial(ctx, nil, dest, nil); !errors.Is(err, errBlockedSocket) || calls != 1 {
		t.Fatal("a failed protection callback must abort the dial")
	}
	tcp := l.(*net.TCPListener)
	_ = tcp.SetDeadline(time.Now().Add(50 * time.Millisecond))
	if accepted, err := tcp.Accept(); err == nil {
		accepted.Close()
		t.Fatal("blocked dial reached the listener")
	}
}
