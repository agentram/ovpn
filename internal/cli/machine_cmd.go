package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"ovpn/internal/app"
	"ovpn/internal/machine"
	"ovpn/internal/machine/protocol"
)

func (a *App) machineCmd() *cobra.Command {
	var stdio bool
	var requestedProtocol int
	cmd := &cobra.Command{Use: "machine", Short: "Serve the local desktop JSON-lines protocol", Args: cobra.NoArgs,
		Annotations: map[string]string{"ovpn.local-state": "none"}, SilenceErrors: true, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !stdio || requestedProtocol != protocol.Version || a.dryRun {
				return fmt.Errorf("machine requires --stdio --protocol %d and does not support --dry-run", protocol.Version)
			}
			backend := app.NewDesktopService(cmd.Context(), a.dataDir)
			backend.AttachControl(cmd.Context(), &deskControl{dataDir: a.dataDir, template: a, interactive: true})
			defer backend.Close()
			return machine.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), backend)
		}}
	cmd.Flags().BoolVar(&stdio, "stdio", false, "Use bounded JSON lines on stdin/stdout")
	cmd.Flags().IntVar(&requestedProtocol, "protocol", 0, "Required machine protocol version")
	return cmd
}
