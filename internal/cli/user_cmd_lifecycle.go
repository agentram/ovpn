package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"ovpn/internal/model"
)

// newUserAddCmd initializes user add cmd with the required dependencies.
func (a *App) newUserAddCmd() *cobra.Command {
	var add struct {
		username string
		uuid     string
		email    string
		expiry   string
		quota    int64
		notes    string
		tags     string
	}
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add user",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			if add.uuid == "" {
				add.uuid = uuid.NewString()
			}
			email := strings.TrimSpace(add.email)
			if email == "" {
				email = defaultUserEmail(add.username)
			}
			expiry, err := model.ParseExpiryDate(add.expiry)
			if err != nil {
				return err
			}

			var quota *int64
			if add.quota > 0 {
				quota = &add.quota
			}

			template := model.User{
				Username:         add.username,
				UUID:             add.uuid,
				Email:            email,
				Enabled:          true,
				ExpiryDate:       expiry,
				TrafficLimitByte: quota,
				QuotaEnabled:     true,
				Notes:            add.notes,
				TagsCSV:          add.tags,
			}

			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, add.username); err == nil {
					return fmt.Errorf("user %s already exists on %s", add.username, srv.Name)
				} else if !isNotFoundErr(err) {
					return err
				}
			}

			for _, srv := range targets {
				user := template
				user.ServerID = srv.ID
				if err := a.addUserOnServer(srv, &user); err != nil {
					return fmt.Errorf("add user on %s: %w", srv.Name, err)
				}
			}

			if len(targets) == 1 {
				fmt.Printf("user added: %s\n", add.username)
			} else {
				fmt.Printf("user added on %d servers: %s\n", len(targets), add.username)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&add.username, "username", "", "Username")
	cmd.Flags().StringVar(&add.uuid, "uuid", "", "UUID (auto if empty)")
	cmd.Flags().StringVar(&add.email, "email", "", "Email used by Xray stats (shared across servers)")
	cmd.Flags().StringVar(&add.expiry, "expiry", "", "Expiry date YYYY-MM-DD")
	cmd.Flags().Int64Var(&add.quota, "quota-bytes", 0, "Rolling 30d traffic quota in bytes (default 200GB when unset)")
	cmd.Flags().StringVar(&add.notes, "notes", "", "Notes")
	cmd.Flags().StringVar(&add.tags, "tags", "", "Comma separated tags")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// newUserExpirySetCmd initializes user expiry-set cmd with the required dependencies.
func (a *App) newUserExpirySetCmd() *cobra.Command {
	var set struct {
		username string
		date     string
	}
	cmd := &cobra.Command{
		Use:   "expiry-set",
		Short: "Set user expiration date",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			expiry, err := model.ParseExpiryDate(set.date)
			if err != nil {
				return err
			}
			if expiry == nil {
				return fmt.Errorf("--date is required")
			}
			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, set.username); err != nil {
					if isNotFoundErr(err) {
						return fmt.Errorf("user %s does not exist on %s", set.username, srv.Name)
					}
					return err
				}
			}
			for _, srv := range targets {
				if err := a.setUserExpiryOnServer(srv, set.username, expiry); err != nil {
					return fmt.Errorf("set expiry on %s: %w", srv.Name, err)
				}
			}
			if len(targets) == 1 {
				fmt.Printf("expiry updated for user %s\n", set.username)
			} else {
				fmt.Printf("expiry updated for user %s on %d servers\n", set.username, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&set.username, "username", "", "Username")
	cmd.Flags().StringVar(&set.date, "date", "", "Expiry date YYYY-MM-DD")
	_ = cmd.MarkFlagRequired("username")
	_ = cmd.MarkFlagRequired("date")
	return cmd
}

// newUserExpiryClearCmd initializes user expiry-clear cmd with the required dependencies.
func (a *App) newUserExpiryClearCmd() *cobra.Command {
	var clear struct {
		username string
	}
	cmd := &cobra.Command{
		Use:   "expiry-clear",
		Short: "Clear user expiration date",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, clear.username); err != nil {
					if isNotFoundErr(err) {
						return fmt.Errorf("user %s does not exist on %s", clear.username, srv.Name)
					}
					return err
				}
			}
			for _, srv := range targets {
				if err := a.setUserExpiryOnServer(srv, clear.username, nil); err != nil {
					return fmt.Errorf("clear expiry on %s: %w", srv.Name, err)
				}
			}
			if len(targets) == 1 {
				fmt.Printf("expiry cleared for user %s\n", clear.username)
			} else {
				fmt.Printf("expiry cleared for user %s on %d servers\n", clear.username, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&clear.username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// newUserRemoveCmd initializes user remove cmd with the required dependencies.
func (a *App) newUserRemoveCmd() *cobra.Command {
	var rm struct {
		username string
	}
	cmd := &cobra.Command{
		Use:   "rm",
		Short: "Remove user",
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, rm.username); err != nil {
					if isNotFoundErr(err) {
						return fmt.Errorf("user %s does not exist on %s", rm.username, srv.Name)
					}
					return err
				}
			}
			for _, srv := range targets {
				if err := a.removeUserOnServer(srv, rm.username); err != nil {
					return fmt.Errorf("remove user on %s: %w", srv.Name, err)
				}
			}
			if len(targets) == 1 {
				fmt.Println("user removed")
			} else {
				fmt.Printf("user removed on %d servers\n", len(targets))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&rm.username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// newUserEnableCmd initializes user enable cmd with the required dependencies.
func (a *App) newUserEnableCmd() *cobra.Command {
	return a.newUserSetEnabledCmd(true)
}

// newUserDisableCmd initializes user disable cmd with the required dependencies.
func (a *App) newUserDisableCmd() *cobra.Command {
	return a.newUserSetEnabledCmd(false)
}

// newUserSetEnabledCmd opens user set enabled cmd storage and applies required initialization.
func (a *App) newUserSetEnabledCmd(enable bool) *cobra.Command {
	var toggle struct {
		username string
	}
	name := "disable"
	short := "Disable user"
	if enable {
		name = "enable"
		short = "Enable user"
	}
	cmd := &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, toggle.username); err != nil {
					if isNotFoundErr(err) {
						return fmt.Errorf("user %s does not exist on %s", toggle.username, srv.Name)
					}
					return err
				}
			}
			for _, srv := range targets {
				if err := a.setUserEnabledOnServer(srv, toggle.username, enable); err != nil {
					return fmt.Errorf("set user %s on %s: %w", name, srv.Name, err)
				}
			}
			if len(targets) == 1 {
				fmt.Printf("user %s %sd\n", toggle.username, name)
			} else {
				fmt.Printf("user %s %sd on %d servers\n", toggle.username, name, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&toggle.username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// newUserListCmd initializes user list cmd with the required dependencies.
func (a *App) newUserListCmd() *cobra.Command {
	var list struct{ server string }
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List users",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, list.server)
			if err != nil {
				return err
			}
			users, err := a.store.ListUsers(a.ctx, srv.ID)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			quotaByEmail := map[string]model.QuotaUserStatus{}
			status, quotaErr := a.fetchQuotaStatus(*srv, "")
			if quotaErr == nil {
				for _, row := range status.Users {
					quotaByEmail[row.Email] = row
				}
			}
			rows := buildUserAuditRows(users, quotaByEmail, now)
			fmt.Println(renderUserAuditTable(rows))
			if quotaErr != nil {
				fmt.Fprintf(os.Stderr, "quota runtime unavailable: %v\n", quotaErr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&list.server, "server", "", "Server name")
	_ = cmd.MarkFlagRequired("server")
	return cmd
}

// newUserShowCmd initializes user show cmd with the required dependencies.
func (a *App) newUserShowCmd() *cobra.Command {
	var show struct{ server, username string }
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show user",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, show.server)
			if err != nil {
				return err
			}
			u, err := a.store.GetUser(a.ctx, srv.ID, show.username)
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			out := map[string]any{
				"server":            srv.Name,
				"username":          u.Username,
				"email":             u.Email,
				"uuid":              u.UUID,
				"enabled":           u.Enabled,
				"expiry_date":       model.ExpiryDateString(u.ExpiryDate),
				"expired":           model.IsExpiredAt(u.ExpiryDate, now),
				"effective_enabled": model.IsEffectivelyEnabled(u.Enabled, u.ExpiryDate, now),
				"notes":             u.Notes,
				"tags":              u.TagsCSV,
			}
			status, err := a.fetchQuotaStatus(*srv, u.Email)
			if err != nil {
				out["quota_error"] = err.Error()
			} else if len(status.Users) > 0 {
				quota := status.Users[0]
				out["quota"] = quota
				out["quota_summary"] = quotaSummary(quota)
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
	cmd.Flags().StringVar(&show.server, "server", "", "Server name")
	cmd.Flags().StringVar(&show.username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// defaultUserEmail builds stable global email identity for users without explicit email.
func defaultUserEmail(username string) string {
	return fmt.Sprintf("%s@global", strings.TrimSpace(username))
}
