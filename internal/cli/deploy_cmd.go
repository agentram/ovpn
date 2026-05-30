package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"ovpn/internal/deploy"
	"ovpn/internal/telegrambot"
)

// deployCmd builds the `deploy` command that renders and pushes the runtime stack to a server.
func (a *App) deployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy <server>",
		Short: "Render and deploy stack to remote server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			a.log().Info("deploy command started", "server", srv.Name, "host", srv.Host)
			return a.initOrDeployServer(*srv, false)
		},
	}
	return cmd
}

// restartCmd builds the `restart` command that restarts xray and ovpn-agent via docker compose.
func (a *App) restartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart <server>",
		Short: "Restart xray and ovpn-agent via docker compose",
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
					Event:    "restart",
					Status:   status,
					Severity: severity,
					Source:   "ovpn-cli",
					Message:  message,
				})
			}()
			runner := a.newRunner("restart")
			a.log().Info("restarting remote services", "server", srv.Name, "host", srv.Host)
			if err := deploy.RestartRemote(a.ctx, runner, sshFromServer(*srv)); err != nil {
				return fmt.Errorf("restart server %s on %s: %w", srv.Name, srv.Host, err)
			}
			fmt.Println("restart complete")
			return nil
		},
	}
	return cmd
}
