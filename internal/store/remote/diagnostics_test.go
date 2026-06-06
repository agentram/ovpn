package remote

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"ovpn/internal/model"
)

func TestConnectionDiagnosticsAggregatesAndSourceCap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 5, 12, 30, 0, 0, time.UTC)
	events := []model.ConnectionEvent{
		{Timestamp: now, Email: "alice@global", Result: "accepted", SourceNetwork: "198.51.100.0/24", Destination: "api.telegram.org", DestinationPort: 443, DestinationFamily: "domain"},
		{Timestamp: now.Add(time.Minute), Email: "alice@global", Result: "rejected", SourceNetwork: "198.51.101.0/24", Destination: "2a00:1450::1", DestinationPort: 443, DestinationFamily: "ipv6"},
		{Timestamp: now.Add(2 * time.Minute), Email: "alice@global", Result: "accepted", SourceNetwork: "198.51.102.0/24", Destination: "203.0.113.10", DestinationPort: 5222, DestinationFamily: "ipv4"},
	}
	if err := store.RecordConnectionEvents(ctx, events, 2, 1000, now); err != nil {
		t.Fatalf("record events: %v", err)
	}
	got, err := store.ConnectionDiagnostics(ctx, "alice@global", now.Add(-time.Hour), now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("connection diagnostics: %v", err)
	}
	if got.AcceptedCount != 2 || got.RejectedCount != 1 || got.DestinationIPv6Count != 1 {
		t.Fatalf("unexpected aggregate: %+v", got)
	}
	if got.ApproxSourceNetworks != 2 || got.SourceNetworksOverflow != 1 {
		t.Fatalf("unexpected source cap: %+v", got)
	}
	if len(got.TopPorts) != 2 || got.TopPorts[0].Port != 443 || got.TopPorts[0].Count != 2 {
		t.Fatalf("unexpected top ports: %+v", got.TopPorts)
	}
}

func TestConnectionDebugSessionAndRetention(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	session, err := store.StartDebugSession(ctx, "alice@global", 48*time.Hour, now)
	if err != nil {
		t.Fatalf("start debug: %v", err)
	}
	if session.ExpiresAt.Sub(now) != 24*time.Hour {
		t.Fatalf("debug duration should be capped at 24h, got %s", session.ExpiresAt.Sub(now))
	}
	var events []model.ConnectionEvent
	for i := 0; i < 5; i++ {
		events = append(events, model.ConnectionEvent{
			Timestamp:         now.Add(time.Duration(i) * time.Second),
			Email:             "alice@global",
			Result:            "accepted",
			SourceNetwork:     fmt.Sprintf("198.51.%d.0/24", i),
			Destination:       "api.telegram.org",
			DestinationPort:   443,
			DestinationFamily: "domain",
		})
	}
	if err := store.RecordConnectionEvents(ctx, events, 256, 3, now); err != nil {
		t.Fatalf("record events: %v", err)
	}
	debugEvents, err := store.ListDebugEvents(ctx, "alice@global", now.Add(-time.Minute), now.Add(time.Minute), 1000)
	if err != nil {
		t.Fatalf("list debug events: %v", err)
	}
	if len(debugEvents) != 3 {
		t.Fatalf("expected debug cap of 3 events, got %d", len(debugEvents))
	}
	sessions, err := store.ListDebugSessions(ctx, now)
	if err != nil {
		t.Fatalf("list debug sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Email != "alice@global" {
		t.Fatalf("unexpected active debug sessions: %+v", sessions)
	}
	if err := store.CleanupConnectionDiagnostics(ctx, now.Add(25*time.Hour), 7*24*time.Hour); err != nil {
		t.Fatalf("cleanup expired debug session: %v", err)
	}
	sessions, err = store.ListDebugSessions(ctx, now.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("list debug sessions after cleanup: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no active debug sessions after cleanup, got %+v", sessions)
	}
	debugEvents, err = store.ListDebugEvents(ctx, "alice@global", now.Add(-time.Minute), now.Add(time.Minute), 1000)
	if err != nil {
		t.Fatalf("list debug events after session cleanup: %v", err)
	}
	if len(debugEvents) != 0 {
		t.Fatalf("expected expired debug events pruned, got %d", len(debugEvents))
	}
	got, err := store.ConnectionDiagnostics(ctx, "alice@global", now.Add(-time.Hour), now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("connection diagnostics before retention cleanup: %v", err)
	}
	if got.AcceptedCount != 5 {
		t.Fatalf("expected aggregates kept after debug TTL cleanup, got %+v", got)
	}
	if err := store.CleanupConnectionDiagnostics(ctx, now.Add(8*24*time.Hour), 7*24*time.Hour); err != nil {
		t.Fatalf("cleanup diagnostics: %v", err)
	}
	got, err = store.ConnectionDiagnostics(ctx, "alice@global", now.Add(-time.Hour), now.Add(8*24*time.Hour), 10)
	if err != nil {
		t.Fatalf("connection diagnostics after cleanup: %v", err)
	}
	if got.AcceptedCount != 0 {
		t.Fatalf("expected old aggregates pruned, got %+v", got)
	}
}

func TestStopDebugSessionDeletesCapturedEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if _, err := store.StartDebugSession(ctx, "alice@global", 15*time.Minute, now); err != nil {
		t.Fatalf("start debug: %v", err)
	}
	events := []model.ConnectionEvent{
		{
			Timestamp:         now,
			Email:             "alice@global",
			Result:            "accepted",
			SourceNetwork:     "198.51.100.0/24",
			Destination:       "api.telegram.org",
			DestinationPort:   443,
			DestinationFamily: "domain",
		},
	}
	if err := store.RecordConnectionEvents(ctx, events, 256, 1000, now); err != nil {
		t.Fatalf("record events: %v", err)
	}
	if err := store.StopDebugSession(ctx, "alice@global"); err != nil {
		t.Fatalf("stop debug: %v", err)
	}
	sessions, err := store.ListDebugSessions(ctx, now)
	if err != nil {
		t.Fatalf("list debug sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected stopped session removed, got %+v", sessions)
	}
	debugEvents, err := store.ListDebugEvents(ctx, "alice@global", now.Add(-time.Minute), now.Add(time.Minute), 1000)
	if err != nil {
		t.Fatalf("list debug events: %v", err)
	}
	if len(debugEvents) != 0 {
		t.Fatalf("expected stopped debug events removed, got %d", len(debugEvents))
	}
	got, err := store.ConnectionDiagnostics(ctx, "alice@global", now.Add(-time.Hour), now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("connection diagnostics: %v", err)
	}
	if got.AcceptedCount != 1 {
		t.Fatalf("expected aggregate kept after debug stop, got %+v", got)
	}
}
