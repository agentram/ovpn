package cli

import "github.com/spf13/cobra"

// serverCmd builds the `server` command group for managing servers.
func (a *App) serverCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "server", Short: "Manage servers"}
	cmd.AddCommand(
		a.newServerAddCmd(),
		a.newServerBackendCmd(),
		a.newServerProfileCmd(),
		a.newServerInitCmd(),
		a.newServerListCmd(),
		a.newServerSetXrayVersionCmd(),
		a.newServerStatusCmd(),
		a.newServerBackupCmd(),
		a.newServerRestoreCmd(),
		a.newServerLogsCmd(),
		a.newServerMonitorCmd(),
		a.newServerCleanupCmd(),
	)
	return cmd
}
