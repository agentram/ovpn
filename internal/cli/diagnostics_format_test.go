package cli

import (
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestDiagnosticsTablesUsePrettyTableFormatting(t *testing.T) {
	ts := time.Date(2026, 6, 5, 12, 1, 0, 0, time.UTC)

	events := renderDebugEventsTable([]model.ConnectionDebugEvent{{
		Timestamp:         ts,
		Result:            "accepted",
		SourceNetwork:     "198.51.100.0/24",
		Destination:       "api.telegram.org",
		DestinationPort:   443,
		DestinationFamily: "domain",
	}})
	for _, want := range []string{"TIMESTAMP", "RESULT", "SOURCE NETWORK", "DESTINATION", "PORT", "FAMILY", "api.telegram.org", "443"} {
		if !strings.Contains(events, want) {
			t.Fatalf("expected %q in debug events table:\n%s", want, events)
		}
	}
	if strings.Contains(events, "\t") {
		t.Fatalf("debug events table should not use raw tabs:\n%s", events)
	}

	sessions := renderDebugSessionsTable([]model.ConnectionDebugSession{{
		Email:     "alice@global",
		StartedAt: ts,
		ExpiresAt: ts.Add(15 * time.Minute),
	}}, map[string]string{"alice@global": "alice"})
	for _, want := range []string{"USERNAME", "EMAIL", "STARTED", "EXPIRES", "alice", "alice@global"} {
		if !strings.Contains(sessions, want) {
			t.Fatalf("expected %q in debug sessions table:\n%s", want, sessions)
		}
	}
	if strings.Contains(sessions, "\t") {
		t.Fatalf("debug sessions table should not use raw tabs:\n%s", sessions)
	}
}
