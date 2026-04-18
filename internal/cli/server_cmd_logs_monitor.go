package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/deploy"
	"ovpn/internal/telegrambot"
)

// newServerLogsCmd initializes server logs cmd with the required dependencies.
func (a *App) newServerLogsCmd() *cobra.Command {
	var service string
	var tail int
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <server>",
		Short: "Show remote docker compose logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tail <= 0 {
				return errors.New("--tail must be > 0")
			}
			serviceArg, err := validateComposeService(service)
			if err != nil {
				return err
			}
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			followArg := ""
			if follow {
				followArg = " --follow"
			}
			remoteCmd := fmt.Sprintf("set -e; cd %s; files='-f docker-compose.yml'; if [ -f docker-compose.monitoring.yml ]; then files='-f docker-compose.yml -f docker-compose.monitoring.yml'; fi; sudo docker compose --env-file .env $files logs --tail %d%s%s", deploy.RemoteDir, tail, followArg, serviceArg)
			runner := a.newRunner("server.logs")
			a.log().Info("fetching remote logs", "server", srv.Name, "host", srv.Host, "service", emptyAsAll(service), "tail", tail, "follow", follow)
			if follow {
				return runner.ExecStream(a.ctx, sshFromServer(*srv), remoteCmd, os.Stdout, os.Stderr)
			}
			res, err := runner.Exec(a.ctx, sshFromServer(*srv), remoteCmd)
			if err != nil {
				return fmt.Errorf("fetch logs for server %s on %s: %w", srv.Name, srv.Host, err)
			}
			if strings.TrimSpace(res.Stdout) != "" {
				fmt.Print(res.Stdout)
			}
			if strings.TrimSpace(res.Stderr) != "" {
				fmt.Fprint(os.Stderr, res.Stderr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&service, "service", "", "Service name: xray|ovpn-agent|prometheus|alertmanager|grafana|node-exporter|cadvisor|ovpn-telegram-bot (default: all)")
	cmd.Flags().IntVar(&tail, "tail", 200, "Number of lines from the end of logs")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow log output")
	return cmd
}

// newServerMonitorCmd initializes server monitor cmd with the required dependencies.
func (a *App) newServerMonitorCmd() *cobra.Command {
	monitorCmd := &cobra.Command{
		Use:   "monitor",
		Short: "Manage optional monitoring stack (Prometheus, Alertmanager, Grafana)",
	}
	monitorCmd.AddCommand(
		a.newServerMonitorUpCmd(),
		a.newServerMonitorDownCmd(),
		a.newServerMonitorStatusCmd(),
		a.newServerMonitorTelegramSetupCmd(),
	)
	return monitorCmd
}

// newServerMonitorUpCmd initializes server monitor up cmd with the required dependencies.
func (a *App) newServerMonitorUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up <server>",
		Short: "Start monitoring stack on server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			runner := a.newRunner("server.monitor.up")
			if err := deploy.MonitoringUpRemote(a.ctx, runner, sshFromServer(*srv)); err != nil {
				return err
			}
			fmt.Println("monitoring stack started")
			return nil
		},
	}
}

// newServerMonitorDownCmd initializes server monitor down cmd with the required dependencies.
func (a *App) newServerMonitorDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down <server>",
		Short: "Stop monitoring stack on server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			runner := a.newRunner("server.monitor.down")
			if err := deploy.MonitoringDownRemote(a.ctx, runner, sshFromServer(*srv)); err != nil {
				return err
			}
			fmt.Println("monitoring stack stopped")
			return nil
		},
	}
}

// newServerMonitorStatusCmd initializes server monitor status cmd with the required dependencies.
func (a *App) newServerMonitorStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <server>",
		Short: "Show monitoring stack status on server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			runner := a.newRunner("server.monitor.status")
			out, err := deploy.MonitoringStatusRemote(a.ctx, runner, sshFromServer(*srv))
			if err != nil {
				return err
			}
			fmt.Println(out)
			return nil
		},
	}
}

// newServerMonitorTelegramSetupCmd initializes server monitor telegram setup cmd with the required dependencies.
func (a *App) newServerMonitorTelegramSetupCmd() *cobra.Command {
	var setup struct {
		token         string
		notifyChatIDs string
		ownerUserID   string
		monitorUp     bool
		testNotify    bool
	}
	cmd := &cobra.Command{
		Use:   "telegram-setup <server>",
		Short: "Automate Telegram monitoring setup (token, deploy, monitor up, test notify)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token := strings.TrimSpace(setup.token)
			if token == "" {
				token = envOr("OVPN_TELEGRAM_BOT_TOKEN", "")
			}
			if strings.TrimSpace(token) == "" {
				return errors.New("telegram token is required; pass --token or set OVPN_TELEGRAM_BOT_TOKEN / OVPN_TELEGRAM_BOT_TOKEN_FILE")
			}

			notifyChatIDs := strings.TrimSpace(setup.notifyChatIDs)
			if notifyChatIDs == "" {
				notifyChatIDs = envOr("OVPN_TELEGRAM_NOTIFY_CHAT_IDS", "")
			}

			ownerUserID := strings.TrimSpace(setup.ownerUserID)
			if ownerUserID == "" {
				ownerUserID = envOr("OVPN_TELEGRAM_OWNER_USER_ID", "")
			}
			if strings.TrimSpace(notifyChatIDs) != "" {
				if _, err := telegrambot.ParseIDSetCSV(notifyChatIDs); err != nil {
					return fmt.Errorf("invalid notify chat ids: %w", err)
				}
			}
			ownerUserID = inferTelegramOwnerUserID(ownerUserID, notifyChatIDs)
			ownerIDs, err := telegrambot.ParseIDSliceCSV(ownerUserID)
			if err != nil {
				return fmt.Errorf("invalid owner user id: %w", err)
			}
			if len(ownerIDs) != 1 {
				return errors.New("owner user id is required when notify chat ids are empty; provide exactly one numeric telegram user id")
			}
			ownerUserID = fmt.Sprintf("%d", ownerIDs[0])
			if notifyChatIDs == "" {
				notifyChatIDs = ownerUserID
			}

			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}

			fmt.Printf("telegram setup plan for %s (%s)\n", srv.Name, srv.Host)
			fmt.Printf("- upload token file: %s\n", deploy.RemoteDir+"/monitoring/secrets/telegram_bot_token")
			fmt.Printf("- notify chat ids: %s\n", emptyAsAll(notifyChatIDs))
			fmt.Printf("- owner user id: %s\n", emptyAsAll(ownerUserID))
			fmt.Printf("- deploy runtime bundle: %v\n", true)
			fmt.Printf("- start monitoring stack: %v\n", setup.monitorUp)
			fmt.Printf("- send test telegram notify: %v\n", setup.testNotify)
			fmt.Printf("- dry-run mode: %v\n", a.dryRun)

			restoreEnv := setEnvOverrides(map[string]string{
				"OVPN_TELEGRAM_NOTIFY_CHAT_IDS": notifyChatIDs,
				"OVPN_TELEGRAM_OWNER_USER_ID":   ownerUserID,
			})
			defer restoreEnv()

			if err := a.initOrDeployServer(*srv, false); err != nil {
				return err
			}
			if err := a.uploadTelegramBotToken(*srv, token); err != nil {
				return err
			}
			if setup.monitorUp {
				runner := a.newRunner("server.monitor.telegram_setup.up")
				if err := deploy.MonitoringUpRemote(a.ctx, runner, sshFromServer(*srv)); err != nil {
					return err
				}
			}
			if err := a.waitForRemoteHTTPReady(*srv, "http://127.0.0.1:"+a.telegramBotHostPort()+"/health", 45*time.Second); err != nil {
				return fmt.Errorf("telegram bot did not become ready on %s: %w; check `ovpn server logs %s --service ovpn-telegram-bot --tail 200`", srv.Host, err, srv.Name)
			}
			if setup.testNotify {
				_, err := a.fetchRemoteHTTP(*srv, "POST", a.telegramNotifyURL(), telegrambot.NotifyEvent{
					Event:    "telegram_setup",
					Status:   "success",
					Severity: "info",
					Source:   "ovpn-cli",
					Message:  fmt.Sprintf("telegram setup completed for server %s", srv.Name),
				})
				if err != nil {
					return fmt.Errorf("send test telegram notification: %w", err)
				}
			}
			fmt.Println("telegram setup complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&setup.token, "token", "", "Telegram bot token (or set OVPN_TELEGRAM_BOT_TOKEN / OVPN_TELEGRAM_BOT_TOKEN_FILE)")
	cmd.Flags().StringVar(&setup.notifyChatIDs, "notify-chat-ids", "", "Comma-separated Telegram chat IDs for alerts/events")
	cmd.Flags().StringVar(&setup.ownerUserID, "owner-user-id", "", "Telegram owner user ID")
	cmd.Flags().BoolVar(&setup.monitorUp, "monitor-up", true, "Bring monitoring stack up after deploy")
	cmd.Flags().BoolVar(&setup.testNotify, "test-notify", true, "Send a test Telegram notification after setup")
	return cmd
}
