package cli

import (
	"encoding/json"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
)

func (a *App) newUserDiagnoseCmd() *cobra.Command {
	var opts struct {
		server   string
		username string
		since    string
		jsonOut  bool
	}
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Show per-user connection diagnostics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, user, err := a.resolveUserOnServer(opts.server, opts.username)
			if err != nil {
				return err
			}
			since := strings.TrimSpace(opts.since)
			if since == "" {
				since = "24h"
			}
			url := a.agentURL("/diagnostics/user?email=" + neturl.QueryEscape(user.Email) + "&since=" + neturl.QueryEscape(since))
			body, err := a.fetchRemoteAgent(*srv, "GET", url, nil)
			if err != nil {
				return err
			}
			var resp model.UserDiagnosticsResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				return err
			}
			if opts.jsonOut {
				raw, _ := json.MarshalIndent(resp, "", "  ")
				fmt.Println(string(raw))
				return nil
			}
			printUserDiagnostics(resp)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.server, "server", "", "Server name")
	cmd.Flags().StringVar(&opts.username, "username", "", "Username")
	cmd.Flags().StringVar(&opts.since, "since", "24h", "Lookback duration such as 24h or RFC3339 timestamp")
	cmd.Flags().BoolVar(&opts.jsonOut, "json", false, "Print JSON")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func (a *App) newUserDebugCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "debug", Short: "Manage targeted per-user connection debug"}
	cmd.AddCommand(a.newUserDebugStartCmd(), a.newUserDebugListCmd(), a.newUserDebugShowCmd(), a.newUserDebugStopCmd())
	return cmd
}

func (a *App) newUserDebugStartCmd() *cobra.Command {
	var opts struct {
		server   string
		username string
		duration string
	}
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start short-lived per-user connection debug",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, user, err := a.resolveUserOnServer(opts.server, opts.username)
			if err != nil {
				return err
			}
			duration := strings.TrimSpace(opts.duration)
			if duration == "" {
				duration = "15m"
			}
			body, err := a.fetchRemoteAgent(*srv, "POST", a.agentURL("/diagnostics/debug/start"), map[string]string{
				"email":    user.Email,
				"duration": duration,
			})
			if err != nil {
				return err
			}
			var out struct {
				Session model.ConnectionDebugSession `json:"session"`
			}
			_ = json.Unmarshal(body, &out)
			fmt.Printf("debug started for %s until %s\n", user.Username, out.Session.ExpiresAt.UTC().Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.server, "server", "", "Server name")
	cmd.Flags().StringVar(&opts.username, "username", "", "Username")
	cmd.Flags().StringVar(&opts.duration, "duration", "15m", "Debug duration, max 24h")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func (a *App) newUserDebugListCmd() *cobra.Command {
	var opts struct {
		server  string
		jsonOut bool
	}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active per-user connection debug sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			serverName, err := requiredFlagValue("--server", opts.server)
			if err != nil {
				return err
			}
			srv, err := a.store.GetServerByName(a.ctx, serverName)
			if err != nil {
				return err
			}
			body, err := a.fetchRemoteAgent(*srv, "GET", a.agentURL("/diagnostics/debug/sessions"), nil)
			if err != nil {
				return err
			}
			var resp model.ConnectionDebugSessionsResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				return err
			}
			if opts.jsonOut {
				raw, _ := json.MarshalIndent(resp, "", "  ")
				fmt.Println(string(raw))
				return nil
			}
			users, err := a.store.ListUsers(a.ctx, srv.ID)
			if err != nil {
				return err
			}
			printDebugSessions(resp, users)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.server, "server", "", "Server name")
	cmd.Flags().BoolVar(&opts.jsonOut, "json", false, "Print JSON")
	_ = cmd.MarkFlagRequired("server")
	return cmd
}

func (a *App) newUserDebugShowCmd() *cobra.Command {
	var opts struct {
		server   string
		username string
		since    string
		jsonOut  bool
	}
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show captured per-user connection debug events",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, user, err := a.resolveUserOnServer(opts.server, opts.username)
			if err != nil {
				return err
			}
			since := strings.TrimSpace(opts.since)
			if since == "" {
				since = "15m"
			}
			url := a.agentURL("/diagnostics/debug/events?email=" + neturl.QueryEscape(user.Email) + "&since=" + neturl.QueryEscape(since))
			body, err := a.fetchRemoteAgent(*srv, "GET", url, nil)
			if err != nil {
				return err
			}
			var resp model.ConnectionDebugEventsResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				return err
			}
			if opts.jsonOut {
				raw, _ := json.MarshalIndent(resp, "", "  ")
				fmt.Println(string(raw))
				return nil
			}
			printDebugEvents(resp)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.server, "server", "", "Server name")
	cmd.Flags().StringVar(&opts.username, "username", "", "Username")
	cmd.Flags().StringVar(&opts.since, "since", "15m", "Lookback duration such as 15m or RFC3339 timestamp")
	cmd.Flags().BoolVar(&opts.jsonOut, "json", false, "Print JSON")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func (a *App) newUserDebugStopCmd() *cobra.Command {
	var opts struct {
		server   string
		username string
	}
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop per-user connection debug",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, user, err := a.resolveUserOnServer(opts.server, opts.username)
			if err != nil {
				return err
			}
			if _, err := a.fetchRemoteAgent(*srv, "POST", a.agentURL("/diagnostics/debug/stop"), map[string]string{"email": user.Email}); err != nil {
				return err
			}
			fmt.Printf("debug stopped for %s\n", user.Username)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.server, "server", "", "Server name")
	cmd.Flags().StringVar(&opts.username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func (a *App) resolveUserOnServer(serverName string, username string) (*model.Server, model.User, error) {
	server, err := requiredFlagValue("--server", serverName)
	if err != nil {
		return nil, model.User{}, err
	}
	userName, err := requiredFlagValue("--username", username)
	if err != nil {
		return nil, model.User{}, err
	}
	srv, err := a.store.GetServerByName(a.ctx, server)
	if err != nil {
		return nil, model.User{}, err
	}
	user, err := a.store.GetUser(a.ctx, srv.ID, userName)
	if err != nil {
		return nil, model.User{}, err
	}
	return srv, *user, nil
}

func printUserDiagnostics(resp model.UserDiagnosticsResponse) {
	fmt.Printf("user: %s (%s)\n", firstNonEmpty(resp.Username, "-"), resp.Email)
	if resp.User != nil {
		fmt.Printf("enabled: %v effective: %v expired: %v quota_blocked: %v\n",
			resp.User.Enabled, resp.User.EffectiveEnabled, resp.User.Expired, resp.User.BlockedByQuota)
		if resp.User.Window30DQuotaByte > 0 {
			fmt.Printf("quota: %.1f%% (%s / %s)\n",
				quotaPercent(resp.User.Window30DUsageByte, resp.User.Window30DQuotaByte),
				formatBytes(resp.User.Window30DUsageByte),
				formatBytes(resp.User.Window30DQuotaByte),
			)
		}
	}
	fmt.Println("traffic:")
	for _, row := range resp.TrafficWindows {
		total := row.TotalBytes
		if total == 0 {
			total = row.UplinkBytes + row.DownlinkBytes
		}
		fmt.Printf("  %s\ttotal=%s\tuplink=%s\tdownlink=%s\n", row.Window, formatBytes(total), formatBytes(row.UplinkBytes), formatBytes(row.DownlinkBytes))
	}
	conn := resp.Connections
	lastSeen := "never"
	if conn.LastSeenAt != nil {
		lastSeen = conn.LastSeenAt.UTC().Format(time.RFC3339)
	}
	fmt.Println("connections:")
	fmt.Printf("  last_seen=%s accepted=%d rejected=%d approx_source_networks=%d ipv6_destinations=%d\n",
		lastSeen, conn.AcceptedCount, conn.RejectedCount, conn.ApproxSourceNetworks, conn.DestinationIPv6Count)
	if len(conn.TopPorts) > 0 {
		parts := make([]string, 0, len(conn.TopPorts))
		for _, p := range conn.TopPorts {
			parts = append(parts, fmt.Sprintf("%d=%d", p.Port, p.Count))
		}
		fmt.Println("  top_ports=" + strings.Join(parts, ","))
	}
	if conn.DebugActive && conn.DebugExpiresAt != nil {
		fmt.Printf("  debug_active=true debug_expires_at=%s\n", conn.DebugExpiresAt.UTC().Format(time.RFC3339))
	}
	for _, hint := range resp.Hints {
		fmt.Println("hint: " + hint)
	}
}

func printDebugEvents(resp model.ConnectionDebugEventsResponse) {
	if len(resp.Events) == 0 {
		fmt.Println("no debug events")
		return
	}
	fmt.Println("timestamp\tresult\tsource_network\tdestination\tport\tfamily")
	for _, ev := range resp.Events {
		fmt.Printf("%s\t%s\t%s\t%s\t%d\t%s\n",
			ev.Timestamp.UTC().Format(time.RFC3339),
			ev.Result,
			defaultDash(ev.SourceNetwork),
			defaultDash(ev.Destination),
			ev.DestinationPort,
			ev.DestinationFamily,
		)
	}
}

func printDebugSessions(resp model.ConnectionDebugSessionsResponse, users []model.User) {
	if len(resp.Sessions) == 0 {
		fmt.Println("no active debug sessions")
		return
	}
	usernames := make(map[string]string, len(users))
	for _, user := range users {
		usernames[user.Email] = user.Username
	}
	fmt.Println("active debug sessions:")
	for _, session := range resp.Sessions {
		username := defaultDash(usernames[session.Email])
		fmt.Printf("- username=%s email=%s started=%s expires=%s\n",
			username,
			session.Email,
			session.StartedAt.UTC().Format(time.RFC3339),
			session.ExpiresAt.UTC().Format(time.RFC3339),
		)
	}
}

func formatBytes(v int64) string {
	if v <= 0 {
		return "0B"
	}
	const gib = 1024 * 1024 * 1024
	if v >= gib {
		return fmt.Sprintf("%.2fGiB", float64(v)/gib)
	}
	const mib = 1024 * 1024
	if v >= mib {
		return fmt.Sprintf("%.2fMiB", float64(v)/mib)
	}
	const kib = 1024
	if v >= kib {
		return fmt.Sprintf("%.2fKiB", float64(v)/kib)
	}
	return fmt.Sprintf("%dB", v)
}

func defaultDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return strings.TrimSpace(v)
}
