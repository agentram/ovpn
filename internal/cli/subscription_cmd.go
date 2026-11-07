package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"ovpn/internal/subscription"
)

func (a *App) subscriptionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "subscription", Short: "Deliver and revoke prepared client subscriptions"}
	cmd.AddCommand(a.subscriptionServeCmd(), a.subscriptionRevokeCmd())
	return cmd
}

func (a *App) subscriptionServeCmd() *cobra.Command {
	opts := subscription.ServeOptions{}
	cmd := &cobra.Command{
		Use: "serve", Short: "Serve prepared bundles without opening local control state", Args: cobra.NoArgs,
		Annotations: map[string]string{"ovpn.local-state": "none"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.dryRun {
				return fmt.Errorf("subscription serve starts a listener; --dry-run is not supported")
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return subscription.Serve(ctx, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Dir, "bundle-dir", "", "Directory containing prepared subscription bundles only")
	cmd.Flags().StringVar(&opts.Listen, "listen", "127.0.0.1:8088", "Subscription HTTP listen address; non-loopback requires TLS")
	cmd.Flags().StringVar(&opts.CertFile, "tls-cert", "", "TLS certificate chain file")
	cmd.Flags().StringVar(&opts.KeyFile, "tls-key", "", "TLS private key file")
	_ = cmd.MarkFlagRequired("bundle-dir")
	return cmd
}

func (a *App) subscriptionRevokeCmd() *cobra.Command {
	var dir, id string
	cmd := &cobra.Command{
		Use: "revoke", Short: "Revoke delivery of a subscription without changing VPN users", Args: cobra.NoArgs,
		Annotations: map[string]string{"ovpn.local-state": "none"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.dryRun {
				return fmt.Errorf("subscription revoke removes a bundle; --dry-run is not supported")
			}
			store, err := subscription.Open(dir)
			if err != nil {
				return err
			}
			defer store.Close()
			if err := store.Revoke(id); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "subscription revoked; imported VPN credentials are unchanged")
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "bundle-dir", "", "Private directory for prepared delivery bundles")
	cmd.Flags().StringVar(&id, "id", "", "Subscription id")
	_ = cmd.MarkFlagRequired("bundle-dir")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
