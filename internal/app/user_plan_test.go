package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/app/dto"
	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

func planFixture(t *testing.T, count int) (ReadService, *local.Store, []model.Server) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := local.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	servers := make([]model.Server, count)
	for i := range servers {
		srv := model.Server{Name: fmt.Sprintf("edge-%03d", i), Host: "private-host.example.net", Domain: "private-domain.example.net",
			SSHUser: "private-operator", SSHPort: 22, XrayVersion: "26.3.27", RealityPrivateKey: "private-key",
			RealityPublicKey: "public-key", RealityShortIDs: "abcd", RealityServerName: "example.com", RealityTarget: "example.com:443", Enabled: true}
		if err := st.AddServer(ctx, &srv); err != nil {
			t.Fatal(err)
		}
		servers[i] = srv
	}
	return ReadService{DataDir: dir}, st, servers
}

func requirePlanCode(t *testing.T, err error, code string) {
	t.Helper()
	var safe *Error
	if !errors.As(err, &safe) || safe.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestUserPlanCapturesAllEnabledTargetsAndRejectsChangedIntentOrState(t *testing.T) {
	ctx := context.Background()
	svc, st, servers := planFixture(t, 3)
	servers[2].Enabled = false
	if err := st.UpdateServer(ctx, &servers[2]); err != nil {
		t.Fatal(err)
	}
	quota := int64(100 << 30)
	request := dto.UserPlanRequest{Action: "create", Username: "alice", Expiry: "2030-01-01", QuotaBytes: &quota}
	plan, err := svc.PlanUser(ctx, request)
	if err != nil || plan.ExecutionAvailable || plan.Scope != "all_enabled_servers" || len(plan.Targets) != 2 {
		t.Fatalf("invalid plan: %+v %v", plan, err)
	}
	if plan.Targets[0].Before != nil || plan.Targets[0].After.ExpiresAt != "2030-01-02T00:00:00Z" || *plan.Targets[0].After.QuotaBytes != quota {
		t.Fatal("incorrect policy preview")
	}
	for _, srv := range servers {
		users, err := st.ListUsers(ctx, srv.ID)
		if err != nil || len(users) != 0 {
			t.Fatal("planning changed control state")
		}
	}
	request.ExpectedDigest = plan.Digest
	checked, err := svc.PlanUser(ctx, request)
	if err != nil || checked.Digest != plan.Digest {
		t.Fatal("unchanged plan cannot be rechecked")
	}
	changed := request
	changed.Expiry = "2030-01-02"
	_, err = svc.PlanUser(ctx, changed)
	requirePlanCode(t, err, "STALE_PLAN")
	servers[2].Enabled = true
	if err := st.UpdateServer(ctx, &servers[2]); err != nil {
		t.Fatal(err)
	}
	_, err = svc.PlanUser(ctx, request)
	requirePlanCode(t, err, "STALE_PLAN")
	request.ExpectedDigest = ""
	refreshed, err := svc.PlanUser(ctx, request)
	if err != nil || len(refreshed.Targets) != 3 || refreshed.Digest == plan.Digest {
		t.Fatal("new scope not reflected")
	}
}

func TestUserEnablePlanPreservesQuotaAndExpiryAndReportsMissingRecords(t *testing.T) {
	ctx := context.Background()
	svc, st, servers := planFixture(t, 2)
	past := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	quota := int64(50 << 30)
	u := model.User{ServerID: servers[0].ID, Username: "alice", UUID: "11111111-1111-1111-1111-111111111111", Email: "alice@private.example",
		Enabled: false, ExpiryDate: &past, TrafficLimitByte: &quota, QuotaEnabled: true, QuotaBlocked: true, Notes: "private-note"}
	if err := st.AddUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	request := dto.UserPlanRequest{Action: "enable", Username: "alice"}
	plan, err := svc.PlanUser(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	before, after := plan.Targets[0].Before, plan.Targets[0].After
	if before.Enabled || !after.Enabled || !after.QuotaBlocked || after.ExpiresAt != before.ExpiresAt || *after.QuotaBytes != quota {
		t.Fatal("enable cleared an independent policy")
	}
	if plan.Targets[1].After != nil || strings.Join(plan.Targets[1].Issues, ",") != "USER_MISSING" {
		t.Fatal("missing assignment invented")
	}
	raw, _ := json.Marshal(plan)
	for _, secret := range []string{"11111111", "private-", "@private", "uuid", "email", "notes", "host"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("private value/field escaped: %s", secret)
		}
	}
	// A UUID rotation with unchanged timestamps still invalidates the preview.
	u.UUID = "22222222-2222-2222-2222-222222222222"
	if err := st.UpdateUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	request.ExpectedDigest = plan.Digest
	_, err = svc.PlanUser(ctx, request)
	requirePlanCode(t, err, "STALE_PLAN")
	request.ExpectedDigest = ""
	request.Action = "create"
	duplicate, err := svc.PlanUser(ctx, request)
	if err != nil || strings.Join(duplicate.Targets[0].Issues, ",") != "USER_EXISTS" {
		t.Fatal("duplicate create omitted")
	}
	u.ServerID, u.UUID, u.ID = servers[1].ID, "33333333-3333-3333-3333-333333333333", 0
	if err := st.AddUser(ctx, &u); err != nil {
		t.Fatal(err)
	}
	conflict, err := svc.PlanUser(ctx, dto.UserPlanRequest{Action: "disable", Username: "alice"})
	if err != nil || strings.Join(conflict.Issues, ",") != "IDENTITY_CONFLICT" {
		t.Fatal("fleet identity conflict omitted")
	}
}

func TestUserPlanValidatesLimitsWithoutCreatingMissingWorkspace(t *testing.T) {
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "absent")
	svc := ReadService{DataDir: missing}
	zero, large := int64(0), int64(1<<53)
	for _, request := range []dto.UserPlanRequest{
		{Action: "remove", Username: "alice"}, {Action: "create", Username: " alice"}, {Action: "create", Username: "a\nb"},
		{Action: "create", Username: "a@b"}, {Action: "create", Username: "alice", Expiry: "2030-02-30"},
		{Action: "create", Username: "alice", QuotaBytes: &zero}, {Action: "create", Username: "alice", QuotaBytes: &large},
		{Action: "enable", Username: "alice", Expiry: "2030-01-01"}, {Action: "disable", Username: "alice", ExpectedDigest: "bad"},
	} {
		_, err := svc.PlanUser(ctx, request)
		requirePlanCode(t, err, "INVALID_PARAMS")
	}
	_, err := svc.PlanUser(ctx, dto.UserPlanRequest{Action: "create", Username: "alice"})
	requirePlanCode(t, err, "WORKSPACE_UNAVAILABLE")
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("planning initialized a missing workspace")
	}
	full, _, _ := planFixture(t, 101)
	_, err = full.PlanUser(ctx, dto.UserPlanRequest{Action: "create", Username: "alice"})
	requirePlanCode(t, err, "PLAN_TOO_LARGE")
	empty, _, _ := planFixture(t, 0)
	plan, err := empty.PlanUser(ctx, dto.UserPlanRequest{Action: "create", Username: "alice"})
	if err != nil || strings.Join(plan.Issues, ",") != "NO_ENABLED_SERVERS" {
		t.Fatal("empty fleet did not report its scope")
	}
}
