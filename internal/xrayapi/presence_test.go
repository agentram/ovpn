package xrayapi

import (
	"context"
	"net"
	"reflect"
	"testing"

	command "github.com/xtls/xray-core/app/stats/command"
	"google.golang.org/grpc"
)

type onlineFixture struct {
	command.UnimplementedStatsServiceServer
}

func (onlineFixture) GetAllOnlineUsers(context.Context, *command.GetAllOnlineUsersRequest) (*command.GetAllOnlineUsersResponse, error) {
	return &command.GetAllOnlineUsersResponse{Users: []string{"user>>>alice@example.test>>>online", "user>>>bob@example.test>>>online"}}, nil
}
func TestOnlineUsersReadsPresenceWithoutRequestingClientAddresses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	command.RegisterStatsServiceServer(server, onlineFixture{})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	client, err := New(context.Background(), listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	users, err := client.OnlineUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(users, []string{"alice@example.test", "bob@example.test"}) {
		t.Fatal("incorrect user projection")
	}
}
