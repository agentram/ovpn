package cli

import (
	"encoding/json"
	"fmt"
	neturl "net/url"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
)

// statsCmd builds the Cobra command for stats.
func (a *App) statsCmd() *cobra.Command {
	var server string
	var day string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show/sync traffic stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, server)
			if err != nil {
				return err
			}
			body, err := a.fetchRemoteAgent(*srv, "GET", a.agentURL("/stats/total"), nil)
			if err != nil {
				return err
			}
			a.log().Debug("stats total fetched", "server", srv.Name, "bytes", len(body))
			var rows []model.UserTraffic
			if err := json.Unmarshal(body, &rows); err != nil {
				return err
			}
			for _, r := range rows {
				fmt.Printf("%s\tuplink=%d\tdownlink=%d\n", r.Email, r.UplinkBytes, r.DownlinkBytes)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "Server name")
	_ = cmd.MarkFlagRequired("server")

	userCmd := &cobra.Command{
		Use:   "user",
		Short: "Show daily per-user traffic",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, server)
			if err != nil {
				return err
			}
			if day == "" {
				day = time.Now().UTC().Format("2006-01-02")
			}
			url := a.agentURL("/stats/daily?date=" + neturl.QueryEscape(day))
			body, err := a.fetchRemoteAgent(*srv, "GET", url, nil)
			if err != nil {
				return err
			}
			a.log().Debug("stats daily fetched", "server", srv.Name, "date", day, "bytes", len(body))
			var rows []model.UserTraffic
			if err := json.Unmarshal(body, &rows); err != nil {
				return err
			}
			for _, r := range rows {
				fmt.Printf("%s\t%s\tuplink=%d\tdownlink=%d\n", r.Email, day, r.UplinkBytes, r.DownlinkBytes)
			}
			return nil
		},
	}
	userCmd.Flags().StringVar(&server, "server", "", "Server name")
	userCmd.Flags().StringVar(&day, "date", "", "Date YYYY-MM-DD")
	_ = userCmd.MarkFlagRequired("server")

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Collect remote totals and cache in local SQLite",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, server)
			if err != nil {
				return err
			}
			body, err := a.fetchRemoteAgent(*srv, "GET", a.agentURL("/stats/total"), nil)
			if err != nil {
				return err
			}
			a.log().Debug("stats sync fetched", "server", srv.Name, "bytes", len(body))
			var rows []model.UserTraffic
			if err := json.Unmarshal(body, &rows); err != nil {
				return err
			}
			now := time.Now().UTC().Truncate(time.Hour)
			for _, r := range rows {
				r.ServerID = srv.ID
				r.WindowType = "total"
				r.WindowStart = now
				if err := a.store.UpsertStatsCache(a.ctx, r); err != nil {
					return err
				}
			}
			fmt.Printf("synced %d rows\n", len(rows))
			return nil
		},
	}
	syncCmd.Flags().StringVar(&server, "server", "", "Server name")
	_ = syncCmd.MarkFlagRequired("server")

	cmd.AddCommand(userCmd, syncCmd)
	return cmd
}
