package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
)

type reconcileAction struct {
	kind     string
	server   string
	username string
	details  string
}

// newUserReconcileCmd initializes user reconcile cmd with the required dependencies.
func (a *App) newUserReconcileCmd() *cobra.Command {
	var reconcile struct {
		fromServer string
		toServer   string
		all        bool
		apply      bool
	}
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile users from one server to others (dry-run by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reconcile.fromServer) == "" {
				return fmt.Errorf("--from-server is required")
			}
			if strings.TrimSpace(reconcile.toServer) != "" && reconcile.all {
				return fmt.Errorf("cannot use --to-server and --all together")
			}

			source, err := a.store.GetServerByName(a.ctx, reconcile.fromServer)
			if err != nil {
				return err
			}
			targets, err := a.reconcileTargets(*source, reconcile.toServer, reconcile.all)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				return fmt.Errorf("no target servers selected")
			}

			srcUsers, err := a.store.ListUsers(a.ctx, source.ID)
			if err != nil {
				return err
			}
			srcByName := make(map[string]model.User, len(srcUsers))
			for _, u := range srcUsers {
				srcByName[u.Username] = u
			}

			var actions []reconcileAction
			for _, target := range targets {
				targetUsers, err := a.store.ListUsers(a.ctx, target.ID)
				if err != nil {
					return err
				}
				targetByName := make(map[string]model.User, len(targetUsers))
				for _, u := range targetUsers {
					targetByName[u.Username] = u
				}

				for username, srcUser := range srcByName {
					dstUser, ok := targetByName[username]
					if !ok {
						actions = append(actions, reconcileAction{kind: "add", server: target.Name, username: username})
						continue
					}
					if usersEquivalentForReconcile(srcUser, dstUser) {
						continue
					}
					actions = append(actions, reconcileAction{
						kind:     "update",
						server:   target.Name,
						username: username,
						details:  strings.Join(diffReconcileFields(srcUser, dstUser), ","),
					})
				}
				for username := range targetByName {
					if _, ok := srcByName[username]; ok {
						continue
					}
					actions = append(actions, reconcileAction{kind: "delete", server: target.Name, username: username})
				}
			}

			sort.Slice(actions, func(i, j int) bool {
				if actions[i].server == actions[j].server {
					if actions[i].kind == actions[j].kind {
						return actions[i].username < actions[j].username
					}
					return actions[i].kind < actions[j].kind
				}
				return actions[i].server < actions[j].server
			})

			if len(actions) == 0 {
				fmt.Println("reconcile: no changes needed")
				return nil
			}

			fmt.Printf("reconcile plan: from %s -> %d target(s)\n", source.Name, len(targets))
			for _, action := range actions {
				if strings.TrimSpace(action.details) == "" {
					fmt.Printf("- %s %s on %s\n", action.kind, action.username, action.server)
					continue
				}
				fmt.Printf("- %s %s on %s (%s)\n", action.kind, action.username, action.server, action.details)
			}

			if !reconcile.apply {
				fmt.Println("dry-run mode. Re-run with --apply to persist changes.")
				return nil
			}

			for _, target := range targets {
				targetUsers, err := a.store.ListUsers(a.ctx, target.ID)
				if err != nil {
					return err
				}
				targetByName := make(map[string]model.User, len(targetUsers))
				for _, u := range targetUsers {
					targetByName[u.Username] = u
				}

				for username, srcUser := range srcByName {
					dstUser, ok := targetByName[username]
					if !ok {
						add := srcUser
						add.ID = 0
						add.ServerID = target.ID
						add.CreatedAt = srcUser.CreatedAt
						add.UpdatedAt = srcUser.UpdatedAt
						if err := a.store.AddUser(a.ctx, &add); err != nil {
							return err
						}
						continue
					}
					if usersEquivalentForReconcile(srcUser, dstUser) {
						continue
					}
					updated := dstUser
					updated.Username = srcUser.Username
					updated.UUID = srcUser.UUID
					updated.Email = srcUser.Email
					updated.Enabled = srcUser.Enabled
					updated.ExpiryDate = cloneTimePtr(srcUser.ExpiryDate)
					updated.TrafficLimitByte = cloneInt64Ptr(srcUser.TrafficLimitByte)
					updated.QuotaEnabled = srcUser.QuotaEnabled
					updated.QuotaBlocked = srcUser.QuotaBlocked
					updated.QuotaBlockedAt = cloneTimePtr(srcUser.QuotaBlockedAt)
					updated.Notes = srcUser.Notes
					updated.TagsCSV = srcUser.TagsCSV
					if err := a.store.UpdateUser(a.ctx, &updated); err != nil {
						return err
					}
				}
				for username := range targetByName {
					if _, ok := srcByName[username]; ok {
						continue
					}
					if err := a.store.DeleteUser(a.ctx, target.ID, username); err != nil {
						return err
					}
				}
			}

			fmt.Println("reconcile applied to local state. Run deploy for affected servers.")
			return nil
		},
	}
	cmd.Flags().StringVar(&reconcile.fromServer, "from-server", "", "Source server name")
	cmd.Flags().StringVar(&reconcile.toServer, "to-server", "", "Target server name (optional)")
	cmd.Flags().BoolVar(&reconcile.all, "all", false, "Reconcile into all other servers")
	cmd.Flags().BoolVar(&reconcile.apply, "apply", false, "Persist planned changes")
	return cmd
}

// reconcileTargets resolves target servers for reconcile.
func (a *App) reconcileTargets(source model.Server, toServer string, all bool) ([]model.Server, error) {
	if strings.TrimSpace(toServer) != "" {
		target, err := a.store.GetServerByName(a.ctx, toServer)
		if err != nil {
			return nil, err
		}
		if target.ID == source.ID {
			return nil, fmt.Errorf("source and target servers are the same")
		}
		return []model.Server{*target}, nil
	}

	servers, err := a.store.ListServers(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.Server, 0, len(servers))
	for _, srv := range servers {
		if srv.ID == source.ID {
			continue
		}
		if all || srv.Enabled {
			out = append(out, srv)
		}
	}
	return out, nil
}

// usersEquivalentForReconcile reports whether two users are fully equal for reconcile behavior.
func usersEquivalentForReconcile(left model.User, right model.User) bool {
	if left.Username != right.Username ||
		left.UUID != right.UUID ||
		left.Email != right.Email ||
		left.Enabled != right.Enabled ||
		normalizeTimePtr(left.ExpiryDate) != normalizeTimePtr(right.ExpiryDate) ||
		normalizeTimePtr(left.QuotaBlockedAt) != normalizeTimePtr(right.QuotaBlockedAt) ||
		left.QuotaEnabled != right.QuotaEnabled ||
		left.QuotaBlocked != right.QuotaBlocked ||
		strings.TrimSpace(left.Notes) != strings.TrimSpace(right.Notes) ||
		normalizeTagsCSV(left.TagsCSV) != normalizeTagsCSV(right.TagsCSV) {
		return false
	}
	leftLimit := int64(0)
	rightLimit := int64(0)
	if left.TrafficLimitByte != nil {
		leftLimit = *left.TrafficLimitByte
	}
	if right.TrafficLimitByte != nil {
		rightLimit = *right.TrafficLimitByte
	}
	return leftLimit == rightLimit
}

// diffReconcileFields returns a short field-diff list for reconcile reporting.
func diffReconcileFields(src model.User, dst model.User) []string {
	var out []string
	if src.UUID != dst.UUID {
		out = append(out, "uuid")
	}
	if src.Email != dst.Email {
		out = append(out, "email")
	}
	if src.Enabled != dst.Enabled {
		out = append(out, "enabled")
	}
	if normalizeTimePtr(src.ExpiryDate) != normalizeTimePtr(dst.ExpiryDate) {
		out = append(out, "expiry")
	}
	if normalizeTimePtr(src.QuotaBlockedAt) != normalizeTimePtr(dst.QuotaBlockedAt) {
		out = append(out, "quota_blocked_at")
	}
	if src.QuotaEnabled != dst.QuotaEnabled {
		out = append(out, "quota_enabled")
	}
	if src.QuotaBlocked != dst.QuotaBlocked {
		out = append(out, "quota_blocked")
	}
	if strings.TrimSpace(src.Notes) != strings.TrimSpace(dst.Notes) {
		out = append(out, "notes")
	}
	if normalizeTagsCSV(src.TagsCSV) != normalizeTagsCSV(dst.TagsCSV) {
		out = append(out, "tags")
	}
	leftLimit := int64(0)
	rightLimit := int64(0)
	if src.TrafficLimitByte != nil {
		leftLimit = *src.TrafficLimitByte
	}
	if dst.TrafficLimitByte != nil {
		rightLimit = *dst.TrafficLimitByte
	}
	if leftLimit != rightLimit {
		out = append(out, "traffic_limit")
	}
	return out
}
