package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/subscription"
)

func (a *App) subscriptionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "subscription", Short: "Deliver and revoke prepared client subscriptions"}
	cmd.AddCommand(a.subscriptionServeCmd(), a.subscriptionRevokeCmd(), a.subscriptionInviteCmd())
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
	cmd.Flags().StringVar(&opts.InviteDir, "invite-dir", "", "Private writable invitation directory")
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

func (a *App) subscriptionInviteCmd() *cobra.Command {
	var dir, inviteDir, id, origin, out string
	var ttl time.Duration
	var checkService bool
	cmd := &cobra.Command{
		Use: "invite", Short: "Create a one-time O Tunnel setup code", Args: cobra.NoArgs,
		Annotations: map[string]string{"ovpn.local-state": "none"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.dryRun {
				return fmt.Errorf("subscription invite writes credentials; --dry-run is not supported")
			}
			bundles, err := subscription.Open(dir)
			if err != nil {
				return err
			}
			defer bundles.Close()
			bundle, err := bundles.Read(id)
			if err != nil {
				return err
			}
			invites, err := subscription.OpenInvites(inviteDir)
			if err != nil {
				return err
			}
			defer invites.Close()
			if checkService {
				if err := subscription.VerifyDeliveryHealth(cmd.Context(), origin); err != nil {
					return err
				}
			}
			code, err := invites.Issue(bundle, origin, time.Now(), ttl)
			if err != nil {
				return err
			}
			a.sharedContent = code
			if out != "" {
				return writeQRCodePNG(code, out)
			}
			fmt.Fprintln(cmd.OutOrStdout(), code)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "bundle-dir", "", "Private directory for prepared delivery bundles")
	cmd.Flags().StringVar(&inviteDir, "invite-dir", "", "Private writable invitation directory")
	cmd.Flags().StringVar(&id, "id", "", "Published subscription ID")
	cmd.Flags().StringVar(&origin, "base-url", "", "HTTPS origin of the subscription service")
	cmd.Flags().StringVar(&out, "out", "", "Save a private QR PNG instead of printing the code")
	cmd.Flags().BoolVar(&checkService, "check-service", false, "Verify HTTPS delivery before issuing a setup code")
	cmd.Flags().DurationVar(&ttl, "ttl", 30*time.Minute, "Time allowed to activate the invitation")
	for _, flag := range []string{"bundle-dir", "invite-dir", "id", "base-url"} {
		_ = cmd.MarkFlagRequired(flag)
	}
	return cmd
}
