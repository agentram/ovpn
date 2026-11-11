package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestTelemetryKeepsMissingUsageUnknownAndNeverProjectsPrivateIdentities(t *testing.T) {
	a := newTestAppWithLinkedUser(t)
	collected := ""
	available := true
	a.remoteHTTPHook = func(_ model.Server, _ string, url string, _ any) ([]byte, error) {
		if !available {
			return nil, fmt.Errorf("unavailable")
		}
		switch {
		case strings.Contains(url, "/health"):
			return []byte(fmt.Sprintf(`{"xray_api_reachable":true,"last_collect_at":%q}`, collected)), nil
		case strings.Contains(url, "/users/status"):
			return []byte(`{"time":"2026-01-01T12:00:00Z","users":[{"username":"alice","uuid":"11111111-1111-1111-1111-111111111111","email":"alice@example.com","quota_enabled":true,"window_30d_quota_byte":100,"window_30d_usage_byte":23}]}`), nil
		case strings.Contains(url, "/users/online"):
			return []byte(`{"supported":false,"sampled_at":"2026-01-01T12:00:00Z","online":[]}`), nil
		}
		return nil, fmt.Errorf("unexpected route")
	}
	d := deskControl{dataDir: filepath.Join(a.dataDir, "data"), template: a}
	rows, err := d.Telemetry(context.Background(), false)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	user := rows[0].Users[0]
	if user.UsedBytes != nil || user.RemainingBytes != nil || user.Connected != nil || user.PresenceCode != "AGENT_UPDATE_REQUIRED" {
		t.Fatal("missing data became zero or an old agent became offline")
	}
	collected = time.Now().UTC().Format(time.RFC3339)
	rows, err = d.Telemetry(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	user = rows[0].Users[0]
	if user.UsedBytes == nil || *user.UsedBytes != 23 || user.RemainingBytes == nil || *user.RemainingBytes != 77 {
		t.Fatal("incorrect per-server quota")
	}
	wire, _ := json.Marshal(rows)
	if strings.Contains(string(wire), "11111111-") || strings.Contains(string(wire), "alice@example.com") {
		t.Fatal("private runtime identity crossed into the UI")
	}
	available = false
	rows, err = d.Telemetry(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Users[0].UsedBytes != nil || rows[0].Users[0].Connected != nil {
		t.Fatal("unavailable agent retained authoritative readings")
	}
}
