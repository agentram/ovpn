package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"ovpn/internal/model"
)

func TestStatsCommandsFetchPrintAndCacheRows(t *testing.T) {
	app := newTestAppWithServer(t, false)
	app.remoteHTTPHook = func(_ model.Server, method, url string, payload any) ([]byte, error) {
		if method != "GET" {
			t.Fatalf("unexpected method %s payload=%v", method, payload)
		}
		switch {
		case strings.Contains(url, "/stats/daily"):
			return []byte(`[{"email":"alice@global","uplink_bytes":3,"downlink_bytes":4}]`), nil
		case strings.Contains(url, "/stats/total"):
			return []byte(`[{"email":"alice@global","uplink_bytes":10,"downlink_bytes":20},{"email":"bob@global","uplink_bytes":30,"downlink_bytes":40}]`), nil
		default:
			return nil, json.Unmarshal([]byte("bad"), &struct{}{})
		}
	}

	stdout, _, err := captureStdoutStderr(t, func() error {
		cmd := app.statsCmd()
		cmd.SetArgs([]string{"--server", " main "})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("stats total: %v", err)
	}
	if !strings.Contains(stdout, "alice@global\tuplink=10\tdownlink=20") || !strings.Contains(stdout, "bob@global") {
		t.Fatalf("unexpected stats output:\n%s", stdout)
	}

	stdout, _, err = captureStdoutStderr(t, func() error {
		cmd := app.statsCmd()
		cmd.SetArgs([]string{"user", "--server", "main", "--date", "2026-05-29"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("stats user: %v", err)
	}
	if !strings.Contains(stdout, "alice@global\t2026-05-29\tuplink=3\tdownlink=4") {
		t.Fatalf("unexpected daily stats output:\n%s", stdout)
	}

	stdout, _, err = captureStdoutStderr(t, func() error {
		cmd := app.statsCmd()
		cmd.SetArgs([]string{"sync", "--server", "main"})
		return cmd.Execute()
	})
	if err != nil {
		t.Fatalf("stats sync: %v", err)
	}
	if !strings.Contains(stdout, "synced 2 rows") {
		t.Fatalf("unexpected sync output:\n%s", stdout)
	}
	srv, err := app.store.GetServerByName(app.ctx, "main")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	rows, err := app.store.ListStatsCache(app.ctx, srv.ID, "total")
	if err != nil {
		t.Fatalf("list stats cache: %v", err)
	}
	if len(rows) != 2 || rows[0].Email != "alice@global" || rows[1].Email != "bob@global" {
		t.Fatalf("unexpected cached rows: %+v", rows)
	}
}

func TestStatsCommandsRejectBadInputAndRemoteJSON(t *testing.T) {
	app := newTestAppWithServer(t, false)
	app.remoteHTTPHook = func(model.Server, string, string, any) ([]byte, error) {
		return []byte(`not-json`), nil
	}

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "blank server", args: []string{"--server", " "}, want: "--server is required"},
		{name: "bad date", args: []string{"user", "--server", "main", "--date", "29-05-2026"}, want: "--date must be YYYY-MM-DD"},
		{name: "bad total json", args: []string{"--server", "main"}, want: "invalid character"},
		{name: "bad sync json", args: []string{"sync", "--server", "main"}, want: "invalid character"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := app.statsCmd()
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q for %v, got %v", tc.want, tc.args, err)
			}
		})
	}
}
