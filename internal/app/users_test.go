package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

func TestUsersInventoryGroupsPoliciesWithoutCredentials(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := local.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)
	for i := range 3 {
		srv := model.Server{Name: fmt.Sprintf("edge-%d", i), Host: "node.example.net", Domain: "node.example.net", SSHUser: "root", SSHPort: 22, XrayVersion: "26.3.27", RealityPrivateKey: "private-key", RealityPublicKey: "public-key", RealityShortIDs: "abcd", RealityServerName: "example.com", RealityTarget: "example.com:443", Enabled: i < 2}
		if err := st.AddServer(ctx, &srv); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"alice", "bob", "carol"} {
			id := "11111111-1111-1111-1111-111111111111"
			if name == "carol" && i == 1 || name == "alice" && i == 2 {
				id = "22222222-2222-2222-2222-222222222222"
			}
			expiry := &future
			if name == "bob" {
				expiry = &past
			}
			limit := int64(100)
			if i == 1 {
				limit = 50
			}
			user := model.User{ServerID: srv.ID, Username: name, UUID: id, Email: name + "@private.example", Notes: "private-note", Enabled: name != "alice" || i != 1, ExpiryDate: expiry, TrafficLimitByte: &limit, QuotaEnabled: true, QuotaBlocked: name == "bob" && i == 0}
			if err := st.AddUser(ctx, &user); err != nil {
				t.Fatal(err)
			}
		}
	}
	svc := ReadService{DataDir: dir}
	page, err := svc.Users(ctx, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.NextCursor != "bob" {
		t.Fatalf("bad page: %+v", page)
	}
	alice, bob := page.Items[0], page.Items[1]
	if alice.Username != "alice" || alice.ConfiguredNodes != 3 || alice.EnabledNodes != 2 || alice.EligibleNodes != 1 || alice.DisabledNodes != 1 || !alice.PolicyDiff || alice.IdentityConflict || alice.QuotaMinBytes != 50 || alice.QuotaMaxBytes != 100 {
		t.Fatalf("incorrect alice aggregate: %+v", alice)
	}
	if bob.EligibleNodes != 0 || bob.ExpiredNodes != 2 || bob.BlockedNodes != 1 {
		t.Fatalf("incorrect blocked/expired aggregate: %+v", bob)
	}
	next, err := svc.Users(ctx, page.NextCursor, 2)
	if err != nil || len(next.Items) != 1 || next.NextCursor != "" || !next.Items[0].IdentityConflict {
		t.Fatalf("conflict/page missing: %+v %v", next, err)
	}
	raw, _ := json.Marshal(page)
	for _, secret := range []string{"11111111", "22222222", "private-", "@private", "uuid", "email", "notes", "host"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret/field escaped projection: %s", secret)
		}
	}
	for _, cursor := range []string{strings.Repeat("x", 4097), string([]byte{0xff})} {
		if _, err := svc.Users(ctx, cursor, 50); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
	if _, err := svc.Users(ctx, "", 101); err == nil {
		t.Fatal("unbounded page accepted")
	}
	empty, err := (ReadService{DataDir: filepath.Join(t.TempDir(), "absent")}).Users(ctx, "", 50)
	if err != nil || len(empty.Items) != 0 {
		t.Fatal("missing workspace did not produce empty users")
	}
}
