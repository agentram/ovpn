package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

// newUserTopCmd initializes user top cmd with the required dependencies.
func (a *App) newUserTopCmd() *cobra.Command {
	var top struct {
		server string
		limit  int
	}
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Show top users by total traffic",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, top.server)
			if err != nil {
				return err
			}
			body, err := a.fetchRemoteAgent(*srv, "GET", a.agentURL("/stats/total"), nil)
			if err != nil {
				return err
			}
			var totals []model.UserTraffic
			if err := json.Unmarshal(body, &totals); err != nil {
				return err
			}
			users, err := a.store.ListUsers(a.ctx, srv.ID)
			if err != nil {
				return err
			}
			quotaByEmail := map[string]model.QuotaUserStatus{}
			if status, err := a.fetchQuotaStatus(*srv, ""); err == nil {
				for _, row := range status.Users {
					quotaByEmail[row.Email] = row
				}
			}
			rows := buildUserTopRows(totals, users, quotaByEmail, top.limit)
			if len(rows) == 0 {
				fmt.Println("no traffic rows")
				return nil
			}
			fmt.Println("rank\tusername\temail\ttotal_bytes\tuplink\tdownlink\tquota_pct\tblocked")
			for _, row := range rows {
				quotaPct := "-"
				if row.QuotaPercent != nil {
					quotaPct = fmt.Sprintf("%.1f", *row.QuotaPercent)
				}
				fmt.Printf("%d\t%s\t%s\t%d\t%d\t%d\t%s\t%v\n",
					row.Rank,
					row.Username,
					row.Email,
					row.TotalBytes,
					row.UplinkBytes,
					row.DownlinkBytes,
					quotaPct,
					row.BlockedByQuota,
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&top.server, "server", "", "Server name")
	cmd.Flags().IntVar(&top.limit, "limit", 10, "Number of rows to show")
	_ = cmd.MarkFlagRequired("server")
	return cmd
}

// newUserQuotaResetCmd initializes user quota reset cmd with the required dependencies.
func (a *App) newUserQuotaResetCmd() *cobra.Command {
	var quotaReset struct {
		username string
	}
	cmd := &cobra.Command{
		Use:   "quota-reset",
		Short: "Clear quota block for user and re-add at runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, quotaReset.username); err != nil {
					if isNotFoundErr(err) {
						return fmt.Errorf("user %s does not exist on %s", quotaReset.username, srv.Name)
					}
					return err
				}
			}
			for _, srv := range targets {
				if err := a.resetUserQuotaOnServer(srv, quotaReset.username); err != nil {
					return fmt.Errorf("quota reset on %s: %w", srv.Name, err)
				}
			}
			if len(targets) == 1 {
				fmt.Printf("quota reset for user %s\n", quotaReset.username)
			} else {
				fmt.Printf("quota reset for user %s on %d servers\n", quotaReset.username, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&quotaReset.username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// newUserQuotaSetCmd initializes user quota set cmd with the required dependencies.
func (a *App) newUserQuotaSetCmd() *cobra.Command {
	var quotaSet struct {
		username    string
		monthlyByte int64
		enabled     bool
	}
	cmd := &cobra.Command{
		Use:   "quota-set",
		Short: "Set per-user rolling 30d quota policy",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, quotaSet.username); err != nil {
					if isNotFoundErr(err) {
						return fmt.Errorf("user %s does not exist on %s", quotaSet.username, srv.Name)
					}
					return err
				}
			}
			for _, srv := range targets {
				if err := a.setUserQuotaOnServer(srv, quotaSet.username, quotaSet.monthlyByte, quotaSet.enabled); err != nil {
					return fmt.Errorf("quota set on %s: %w", srv.Name, err)
				}
			}
			if len(targets) == 1 {
				fmt.Printf("quota updated for user %s\n", quotaSet.username)
			} else {
				fmt.Printf("quota updated for user %s on %d servers\n", quotaSet.username, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&quotaSet.username, "username", "", "Username")
	cmd.Flags().Int64Var(&quotaSet.monthlyByte, "monthly-bytes", 0, "Rolling 30d quota in bytes (0 uses default 200GB)")
	cmd.Flags().BoolVar(&quotaSet.enabled, "enabled", true, "Enable rolling 30d quota enforcement for this user")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// newUserLinkCmd initializes user link cmd with the required dependencies.
func (a *App) newUserLinkCmd() *cobra.Command {
	var link struct{ server, username string }
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Generate vless:// link",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, link.server)
			if err != nil {
				return err
			}
			u, err := a.store.GetUser(a.ctx, srv.ID, link.username)
			if err != nil {
				return err
			}
			address := srv.Domain
			if address == "" {
				address = srv.Host
			}
			shortID := firstShortID(srv.RealityShortIDs)
			if shortID == "" {
				return fmt.Errorf("server %s has no REALITY short-id configured", srv.Name)
			}
			vless := xraycfg.BuildVLESSLink(xraycfg.LinkInput{Address: address, Port: 443, UUID: u.UUID, ServerName: srv.RealityServerName, Password: srv.RealityPublicKey, ShortID: shortID, Label: "ovpn-" + u.Username})
			fmt.Println(vless)
			return nil
		},
	}
	cmd.Flags().StringVar(&link.server, "server", "", "Server name")
	cmd.Flags().StringVar(&link.username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}
