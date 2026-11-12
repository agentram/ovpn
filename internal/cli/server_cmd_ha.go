package cli

import (
	"fmt"
	"ovpn/internal/ha"

	"github.com/spf13/cobra"
)

// The operation coordinator expands this command into backend-first checkpoints.
func (a *App) newServerHAApplyCmd() *cobra.Command {
	return &cobra.Command{Use: "ha-apply <server>", Short: "Apply HA backends before the proxy, stopping on a failed dependency", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if a.operationTarget == 0 {
			if !a.dryRun {
				return fmt.Errorf("HA application requires a reviewed operation plan")
			}
			proxy, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			backends, err := a.attachedBackendServers(*proxy)
			if err != nil {
				return err
			}
			nodes, err := ha.Plan(*proxy, backends)
			if err != nil {
				return err
			}
			for i, node := range nodes {
				fmt.Fprintf(a.output(), "%d. apply %s\n", i+1, node.Name)
			}
			return nil
		}
		server, err := a.store.GetServerByID(a.ctx, a.operationTarget)
		if err != nil {
			return err
		}
		return a.initOrDeployServer(*server, server.LastDeployAt == nil)
	}}
}
