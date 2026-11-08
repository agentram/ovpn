package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ovpn/internal/app/dto"
	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

func TestUserPlanCLIUsesReadOnlyServiceWithoutRequiringSourceOrInitializingState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := local.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := model.Server{Name: "edge-a", Host: "node.example.net", Domain: "node.example.net", SSHUser: "root", SSHPort: 22,
		XrayVersion: "26.3.27", RealityPrivateKey: "private-key", RealityPublicKey: "public-key", RealityShortIDs: "abcd",
		RealityServerName: "example.com", RealityTarget: "example.com:443", Enabled: true}
	if err := st.AddServer(ctx, &srv); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	root := NewRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"--data-dir", dir, "user", "plan", "--action", "create", "--username", "alice", "--quota-bytes", "1234"})
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	var plan dto.UserPlan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil || len(plan.Targets) != 1 || plan.ExecutionAvailable || *plan.Targets[0].After.QuotaBytes != 1234 {
		t.Fatalf("unexpected plan: %+v %v", plan, err)
	}
	missing := filepath.Join(t.TempDir(), "absent")
	root = NewRootCmd()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"--data-dir", missing, "user", "plan", "--action", "create", "--username", "alice"})
	if err := root.ExecuteContext(ctx); err == nil {
		t.Fatal("missing workspace accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("CLI planning created local state")
	}
}
