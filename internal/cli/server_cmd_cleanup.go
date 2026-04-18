package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/deploy"
	"ovpn/internal/model"
	"ovpn/internal/telegrambot"
)

// newServerCleanupCmd initializes server cleanup cmd with the required dependencies.
func (a *App) newServerCleanupCmd() *cobra.Command {
	var cleanup struct {
		keepBackups       bool
		removeBackups     bool
		removeVolumes     bool
		removeLocal       bool
		includeMonitoring bool
		skipBackupCheck   bool
		confirm           string
	}
	cmd := &cobra.Command{
		Use:   "cleanup <server>",
		Short: "Safely decommission ovpn runtime on server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			defer func() {
				status := "success"
				severity := "warning"
				message := fmt.Sprintf("server=%s host=%s include_monitoring=%v remove_volumes=%v remove_backups=%v remove_local=%v",
					srv.Name, srv.Host, cleanup.includeMonitoring, cleanup.removeVolumes,
					cleanupRemoveBackupsEffective(cleanup.keepBackups, cleanup.removeBackups), cleanup.removeLocal)
				if err != nil {
					status = "failure"
					severity = "error"
					message = err.Error()
				}
				a.sendTelegramNotifyEvent(*srv, telegrambot.NotifyEvent{
					Event:    "cleanup",
					Status:   status,
					Severity: severity,
					Source:   "ovpn-cli",
					Message:  message,
				})
			}()

			removeBackupsEffective := cleanupRemoveBackupsEffective(cleanup.keepBackups, cleanup.removeBackups)
			fmt.Printf("cleanup plan for server %s (%s)\n", srv.Name, srv.Host)
			fmt.Printf("- include monitoring cleanup: %v\n", cleanup.includeMonitoring)
			fmt.Printf("- remove runtime compose stack: %v\n", true)
			fmt.Printf("- remove runtime directory (%s): %v\n", deploy.RemoteDir, true)
			fmt.Printf("- remove ovpn docker volumes: %v\n", cleanup.removeVolumes)
			fmt.Printf("- remove remote backups (%s): %v\n", deploy.RemoteBackupDir, removeBackupsEffective)
			fmt.Printf("- remove local metadata: %v\n", cleanup.removeLocal)
			fmt.Printf("- dry-run mode: %v\n", a.dryRun)

			if cleanup.skipBackupCheck {
				fmt.Println("backup check: skipped (--skip-backup-check)")
			} else {
				rec, age, err := a.ensureRecentBackupForCleanup(*srv, 30*24*time.Hour)
				if err != nil {
					return err
				}
				fmt.Printf("backup check: latest backup %s (%s ago)\n", rec.Path, roundDuration(age))
			}

			if err := validateCleanupConfirm(cleanup.confirm, a.dryRun); err != nil {
				return err
			}

			runner := a.newRunner("server.cleanup")
			cleanupOpts := deploy.CleanupOptions{
				IncludeMonitoring: cleanup.includeMonitoring,
				RemoveVolumes:     cleanup.removeVolumes,
				RemoveBackups:     removeBackupsEffective,
			}
			if err := deploy.CleanupRemote(a.ctx, runner, sshFromServer(*srv), cleanupOpts); err != nil {
				return fmt.Errorf("cleanup remote runtime on %s for server %s: %w", srv.Host, srv.Name, err)
			}

			if cleanup.removeLocal {
				if a.dryRun {
					fmt.Printf("dry-run: local metadata would be removed for server %s\n", srv.Name)
				} else {
					if err := a.finalizeCleanupLocalState(srv, true); err != nil {
						return fmt.Errorf("remove local metadata for server %s: %w", srv.Name, err)
					}
					fmt.Printf("local metadata removed for server %s\n", srv.Name)
				}
			} else {
				if a.dryRun {
					fmt.Printf("dry-run: local metadata would be kept and server %s would be marked disabled\n", srv.Name)
				} else {
					wasEnabled := srv.Enabled
					if err := a.finalizeCleanupLocalState(srv, false); err != nil {
						return fmt.Errorf("mark server %s disabled in local metadata: %w", srv.Name, err)
					}
					if wasEnabled {
						fmt.Printf("local metadata kept; server %s marked disabled\n", srv.Name)
					} else {
						fmt.Printf("local metadata kept; server %s already disabled\n", srv.Name)
					}
				}
			}

			if a.dryRun {
				fmt.Println("cleanup dry-run complete")
				return nil
			}
			fmt.Println("cleanup complete")
			return nil
		},
	}
	cmd.Flags().BoolVar(&cleanup.keepBackups, "keep-backups", true, "Keep remote backups on server")
	cmd.Flags().BoolVar(&cleanup.removeBackups, "remove-backups", false, "Remove remote backups on server")
	cmd.Flags().BoolVar(&cleanup.removeVolumes, "remove-volumes", true, "Remove ovpn-related Docker volumes")
	cmd.Flags().BoolVar(&cleanup.removeLocal, "remove-local", false, "Remove local metadata for this server")
	cmd.Flags().BoolVar(&cleanup.includeMonitoring, "include-monitoring", true, "Cleanup monitoring stack if present")
	cmd.Flags().BoolVar(&cleanup.skipBackupCheck, "skip-backup-check", false, "Skip recent-backup safety check")
	cmd.Flags().StringVar(&cleanup.confirm, "confirm", "", "Confirmation token required for destructive cleanup (must be CLEANUP)")
	return cmd
}

// finalizeCleanupLocalState applies local-state side effects after remote cleanup.
// When removeLocal is true it deletes server record and dependent local rows.
// Otherwise it keeps metadata but marks server disabled to exclude it from global fan-out operations.
func (a *App) finalizeCleanupLocalState(srv *model.Server, removeLocal bool) error {
	if removeLocal {
		return a.store.DeleteServerByName(a.ctx, srv.Name)
	}
	srv.Enabled = false
	return a.store.UpdateServer(a.ctx, srv)
}
