package cli

import "github.com/spf13/cobra"

// serverCmd builds the Cobra command for server.
func (a *App) serverCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "server", Short: "Manage servers"}
	cmd.AddCommand(
		a.newServerAddCmd(),
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
