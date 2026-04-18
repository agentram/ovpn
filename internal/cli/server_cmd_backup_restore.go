package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"ovpn/internal/backup"
	"ovpn/internal/model"
	"ovpn/internal/telegrambot"
)

// newServerBackupCmd initializes server backup cmd with the required dependencies.
func (a *App) newServerBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup <server>",
		Short: "Create remote and local backups",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			defer func() {
				status := "success"
				severity := "info"
				message := fmt.Sprintf("server=%s host=%s", srv.Name, srv.Host)
				if err != nil {
					status = "failure"
					severity = "error"
					message = err.Error()
				}
				a.sendTelegramNotifyEvent(*srv, telegrambot.NotifyEvent{
					Event:    "backup",
					Status:   status,
					Severity: severity,
					Source:   "ovpn-cli",
					Message:  message,
				})
			}()
			runner := a.newRunner("server.backup")
			a.log().Info("creating backup", "server", srv.Name, "host", srv.Host)
			remotePath, err := backup.RemoteBackup(a.ctx, runner, sshFromServer(*srv), *srv)
			if err != nil {
				return fmt.Errorf("remote backup for server %s on %s: %w", srv.Name, srv.Host, err)
			}
			fmt.Printf("remote backup: %s\n", remotePath)
			archive, sha, err := backup.LocalBackup(a.dataDir, filepath.Join(a.dataDir, "backups"))
			if err != nil {
				return fmt.Errorf("local backup for server %s: %w", srv.Name, err)
			}
			rec := &model.BackupRecord{ServerID: srv.ID, Type: "server", Path: archive, SHA256: sha, CreatedBy: os.Getenv("USER"), RemotePath: remotePath}
			if err := a.store.AddBackupRecord(a.ctx, rec); err != nil {
				return err
			}
			fmt.Printf("local backup: %s\n", archive)
			return nil
		},
	}
}

// newServerRestoreCmd initializes server restore cmd with the required dependencies.
func (a *App) newServerRestoreCmd() *cobra.Command {
	var remotePath string
	cmd := &cobra.Command{
		Use:   "restore <server>",
		Short: "Restore remote stack from a remote backup archive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			if remotePath == "" {
				return errors.New("--remote-path is required")
			}
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			defer func() {
				status := "success"
				severity := "info"
				message := fmt.Sprintf("server=%s host=%s remote_path=%s", srv.Name, srv.Host, remotePath)
				if err != nil {
					status = "failure"
					severity = "error"
					message = err.Error()
				}
				a.sendTelegramNotifyEvent(*srv, telegrambot.NotifyEvent{
					Event:    "restore",
					Status:   status,
					Severity: severity,
					Source:   "ovpn-cli",
					Message:  message,
				})
			}()
			runner := a.newRunner("server.restore")
			a.log().Info("restoring backup", "server", srv.Name, "host", srv.Host, "remote_path", remotePath)
			if err := backup.RemoteRestore(a.ctx, runner, sshFromServer(*srv), remotePath); err != nil {
				return fmt.Errorf("restore server %s on %s: %w", srv.Name, srv.Host, err)
			}
			fmt.Println("restore complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&remotePath, "remote-path", "", "Remote backup archive path")
	return cmd
}
