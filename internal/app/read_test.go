package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/app/dto"
	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

func TestInventoryIsPagedAndRedacted(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := local.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := range 3 {
		srv := model.Server{Name: fmt.Sprintf("edge-%d", i), Host: "private-inventory.example.net", Domain: "private-domain.example.net", SSHUser: "private-operator", SSHPort: 22, SSHIdentityFile: "/private/key", XrayVersion: "26.3.27", RealityPrivateKey: "private-secret", RealityPublicKey: "public-key", RealityShortIDs: "abcd", RealityServerName: "example.com", RealityTarget: "example.com:443", Enabled: i != 2}
		if err := st.AddServer(ctx, &srv); err != nil {
			t.Fatal(err)
		}
		user := model.User{ServerID: srv.ID, Username: "monitor", UUID: "11111111-1111-1111-1111-111111111111", Email: "monitor@example.net", Enabled: true}
		if err := st.AddUser(ctx, &user); err != nil {
			t.Fatal(err)
		}
	}
	svc := ReadService{DataDir: dir}
	summary, err := svc.Summary(ctx)
	if err != nil || summary.Servers != 3 || summary.EnabledServers != 2 || summary.Users != 1 || !summary.ReadOnly {
		t.Fatalf("summary %+v: %v", summary, err)
	}
	first, err := svc.Servers(ctx, "", 2)
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page: %v", err)
	}
	second, err := svc.Servers(ctx, first.NextCursor, 2)
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second page: %v", err)
	}
	raw, _ := json.Marshal(first)
	for _, secret := range []string{"private-", "11111111", "ssh", "reality_private", "host", "UUID"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("unexpected field or material: %s", secret)
		}
	}
	for _, srv := range first.Items {
		if srv.Runtime != "not_checked" {
			t.Fatal("inventory asserted runtime health")
		}
	}
	for _, bad := range []struct {
		cursor string
		limit  int
	}{{"node-01", 2}, {"node--1", 2}, {"node-1", 101}, {"node-1", -1}} {
		if _, err := svc.Servers(ctx, bad.cursor, bad.limit); err == nil {
			t.Fatal("invalid paging accepted")
		}
	}
	for _, request := range []dto.CheckRequest{{ServerID: "node-1", Username: "monitor", Profile: model.TransportProfilePlainXHTTP}, {ServerID: "node-1", Username: "missing"}, {ServerID: "node-3", Username: "monitor"}} {
		if _, err := svc.Check(ctx, request); err == nil {
			t.Fatal("unsupported/inactive check accepted")
		}
	}
}

func TestEmptyAndIncompatibleWorkspacesAreNotInitialized(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "absent")
	svc := ReadService{DataDir: dir}
	view, err := svc.Summary(ctx)
	if err != nil || view.State != "empty" {
		t.Fatalf("empty summary: %v", err)
	}
	page, err := svc.Servers(ctx, "", 0)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("empty inventory: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("workspace created by a query")
	}
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ovpn.db"), []byte("not a database; private payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc.DataDir = dir
	_, err = svc.Summary(ctx)
	if err == nil || err.Error() != "WORKSPACE_UNAVAILABLE" {
		t.Fatalf("unsafe database error: %v", err)
	}
}
