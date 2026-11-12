package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ovpn/internal/model"
	"ovpn/internal/operations"

	"github.com/spf13/cobra"
)

func TestDeskCatalogCoversEveryPublicCommand(t *testing.T) {
	root := NewRootCmd()
	catalog := commandCatalog(root)
	seen := map[string]bool{}
	for _, a := range catalog {
		if a.Area == "" || deskTitles[a.ID] == "" {
			t.Errorf("missing desktop route or presentation for %s", a.ID)
		}
		if seen[a.ID] {
			t.Fatal("duplicate action", a.ID)
		}
		seen[a.ID] = true
	}
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		if cmd.Runnable() && cmd.Name() != "machine" && !seen[actionID(cmd)] {
			t.Errorf("CLI command lacks desktop coverage: %s", cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	raw, _ := json.Marshal(catalog)
	if strings.Contains(string(raw), ".ssh/") {
		t.Fatal("catalog leaked local identity paths")
	}
}
func TestDeskPlanRejectsArbitraryFlagsAndFreezesUserIdentity(t *testing.T) {
	a := newTestAppWithLinkedUser(t)
	d := deskControl{dataDir: filepath.Join(a.dataDir, "data"), template: a}
	if _, err := d.Prepare(context.Background(), operations.Request{Action: "user.add", Values: map[string]string{"username": "new-user", "data-dir": "outside"}}); err == nil {
		t.Fatal("accepted a workspace escape")
	}
	p, err := d.Prepare(context.Background(), operations.Request{Action: "user.add", Values: map[string]string{"username": "new-user"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Request.Values["uuid"] == "" || len(p.Targets) == 0 {
		t.Fatal("plan did not freeze the identity and target set")
	}
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), p.Request.Values["uuid"]) {
		t.Fatal("private client identity exposed")
	}
}

func TestDeskProfileControlsIncludeEverySupportedTransport(t *testing.T) {
	for _, action := range commandCatalog(NewRootCmd()) {
		if action.ID != "server.profile.enable" {
			continue
		}
		for _, field := range action.Arguments {
			if field.Name == "profile" && len(field.Options) != len(model.SupportedTransportProfiles()) {
				t.Fatal("a supported profile cannot be enabled from Desk")
			}
		}
	}
}
