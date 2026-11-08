package app

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestCheckUsersPagesOnlyActiveAssignmentsOnSelectedServer(t *testing.T) {
	ctx := context.Background()
	svc, store, servers := planFixture(t, 3)
	servers[2].Enabled = false
	if err := store.UpdateServer(ctx, &servers[2]); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	for _, entry := range []struct {
		name              string
		server            int
		disabled, blocked bool
		expiry            *time.Time
	}{
		{name: "active-a"},
		{name: "active-b", expiry: &future},
		{name: "expired", expiry: &past},
		{name: "disabled", disabled: true},
		{name: "quota", blocked: true},
		{name: "other-node", server: 1},
		{name: "disabled-node", server: 2},
	} {
		user := model.User{ServerID: servers[entry.server].ID, Username: entry.name,
			UUID: "11111111-1111-1111-1111-111111111111", Email: entry.name + "@example.net",
			Enabled: !entry.disabled, QuotaBlocked: entry.blocked, ExpiryDate: entry.expiry}
		if err := store.AddUser(ctx, &user); err != nil {
			t.Fatal(err)
		}
	}
	node := fmt.Sprintf("node-%d", servers[0].ID)
	first, err := svc.CheckUsers(ctx, node, "", 1)
	if err != nil || !reflect.DeepEqual(first.Items, []string{"active-a"}) || first.NextCursor != "active-a" {
		t.Fatalf("first page: %+v, %v", first, err)
	}
	second, err := svc.CheckUsers(ctx, node, first.NextCursor, 1)
	if err != nil || !reflect.DeepEqual(second.Items, []string{"active-b"}) || second.NextCursor != "" {
		t.Fatalf("second page: %+v, %v", second, err)
	}
	disabled, err := svc.CheckUsers(ctx, fmt.Sprintf("node-%d", servers[2].ID), "", 0)
	if err != nil || len(disabled.Items) != 0 {
		t.Fatalf("disabled server returned users: %+v, %v", disabled, err)
	}
	_, err = svc.CheckUsers(ctx, "node-01", "", 1)
	requirePlanCode(t, err, "INVALID_PARAMS")
}
