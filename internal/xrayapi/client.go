package xrayapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	handlercommand "github.com/xtls/xray-core/app/proxyman/command"
	statscommand "github.com/xtls/xray-core/app/stats/command"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/proxy/vless"
)

type Client struct {
	conn    *grpc.ClientConn
	stats   statscommand.StatsServiceClient
	handler handlercommand.HandlerServiceClient
}

const defaultVLESSFlow = "xtls-rprx-vision"

// New initializes new with the required dependencies.
func New(ctx context.Context, addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	if err := waitForReady(ctx, conn, 5*time.Second); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Client{
		conn:    conn,
		stats:   statscommand.NewStatsServiceClient(conn),
		handler: handlercommand.NewHandlerServiceClient(conn),
	}, nil
}

// waitForReady runs for ready loop until context cancellation or error.
func waitForReady(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if !conn.WaitForStateChange(waitCtx, state) {
			return fmt.Errorf("xray gRPC connection timed out (last state: %s)", state)
		}
	}
}

// Close returns close.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// QueryStats handles query stats HTTP behavior for this service.
func (c *Client) QueryStats(ctx context.Context, pattern string, reset bool) (map[string]int64, error) {
	resp, err := c.stats.QueryStats(ctx, &statscommand.QueryStatsRequest{
		Pattern: pattern,
		Reset_:  reset,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(resp.Stat))
	for _, st := range resp.Stat {
		out[st.Name] = st.Value
	}
	return out, nil
}

// AddUser applies user and returns an error on failure.
func (c *Client) AddUser(ctx context.Context, inboundTag, email, uuid string) error {
	acc := accountForInbound(inboundTag, uuid)
	op := &handlercommand.AddUserOperation{
		User: &protocol.User{
			Email:   email,
			Level:   0,
			Account: serial.ToTypedMessage(acc),
		},
	}
	_, err := c.handler.AlterInbound(ctx, &handlercommand.AlterInboundRequest{
		Tag:       inboundTag,
		Operation: serial.ToTypedMessage(op),
	})
	return err
}

// accountForInbound returns account for inbound.
func accountForInbound(inboundTag, uuid string) *vless.Account {
	acc := &vless.Account{Id: uuid}
	// Keep runtime adds aligned with rendered config for REALITY/VLESS inbounds.
	if strings.TrimSpace(inboundTag) == "" || strings.EqualFold(inboundTag, "vless-reality") {
		acc.Flow = defaultVLESSFlow
	}
	return acc
}

// RemoveUser applies user and returns an error on failure.
func (c *Client) RemoveUser(ctx context.Context, inboundTag, email string) error {
	op := &handlercommand.RemoveUserOperation{Email: email}
	_, err := c.handler.AlterInbound(ctx, &handlercommand.AlterInboundRequest{
		Tag:       inboundTag,
		Operation: serial.ToTypedMessage(op),
	})
	return err
}

type UserCounter struct {
	Email    string
	Uplink   int64
	Downlink int64
}

// ParseUserCounters parses user counters and returns normalized values.
func ParseUserCounters(stats map[string]int64) map[string]UserCounter {
	out := map[string]UserCounter{}
	for name, val := range stats {
		if !strings.HasPrefix(name, "user>>>") {
			continue
		}
		parts := strings.Split(name, ">>>")
		if len(parts) < 4 {
			continue
		}
		email := parts[1]
		dir := parts[3]
		entry := out[email]
		entry.Email = email
		switch dir {
		case "uplink":
			entry.Uplink = val
		case "downlink":
			entry.Downlink = val
		}
		out[email] = entry
	}
	return out
}

// EnsureAPIReachable executes api reachable flow and returns the first error.
func EnsureAPIReachable(ctx context.Context, addr string) error {
	c, err := New(ctx, addr)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.QueryStats(ctx, "", false)
	if err != nil {
		return fmt.Errorf("xray API not reachable: %w", err)
	}
	return nil
}
