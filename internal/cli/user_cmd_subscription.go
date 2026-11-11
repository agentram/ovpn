package cli

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
	"ovpn/internal/subscription"
)

type subscriptionSelection struct {
	username, server string
	allProfiles      bool
}

func (a *App) subscriptionEntries(opts subscriptionSelection) ([]subscription.Entry, error) {
	username, err := requiredFlagValue("--username", opts.username)
	if err != nil {
		return nil, err
	}
	servers, err := a.listEnabledServers()
	if err != nil {
		return nil, err
	}
	if opts.server != "" {
		srv, err := a.store.GetServerByName(a.ctx, opts.server)
		if err != nil {
			return nil, err
		}
		if !srv.Enabled {
			return nil, nil
		}
		servers = []model.Server{*srv}
	}
	var entries []subscription.Entry
	var identity *model.User
	for _, srv := range servers {
		user, err := a.store.GetUser(a.ctx, srv.ID, username)
		if isNotFoundErr(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if validateClientCheckUser(srv, *user) != nil {
			continue
		}
		if identity != nil && (identity.UUID != user.UUID || identity.Email != user.Email) {
			return nil, fmt.Errorf("user identity differs across selected servers; reconcile the user before publishing")
		}
		identity = user
		profiles := []string{srv.NormalizedPrimaryProfile()}
		if opts.allProfiles {
			profiles = deployableEnabledProfiles(srv)
		}
		for _, profile := range profiles {
			link, err := buildUserProfileLink(srv, *user, profile)
			if err != nil {
				return nil, err
			}
			parsed, err := url.Parse(link)
			if err != nil {
				return nil, fmt.Errorf("invalid subscription client link")
			}
			parsed.Fragment = "ovpn-" + username + "-" + srv.Name + "-" + profile
			entries = append(entries, subscription.Entry{Link: parsed.String(), ExpiresAt: user.ExpiryDate})
		}
	}
	if len(entries) > subscription.MaxEntries {
		return nil, fmt.Errorf("too many subscription profiles; select one server")
	}
	return entries, nil
}

func subscriptionSelectionFlags(cmd *cobra.Command, opts *subscriptionSelection) {
	cmd.Flags().StringVar(&opts.username, "username", "", "Username")
	cmd.Flags().StringVar(&opts.server, "server", "", "Select one enabled server (default: all enabled servers)")
	cmd.Flags().BoolVar(&opts.allProfiles, "all-profiles", false, "Include every enabled profile (default: primary profile per server)")
	_ = cmd.MarkFlagRequired("username")
}

func (a *App) newUserSubscriptionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "subscription", Short: "Export and publish a user's subscription snapshots"}
	cmd.AddCommand(a.newUserSubscriptionExportCmd(), a.newUserSubscriptionPublishCmd())
	return cmd
}

func (a *App) newUserSubscriptionExportCmd() *cobra.Command {
	var selection subscriptionSelection
	var out, format string
	cmd := &cobra.Command{
		Use: "export", Short: "Export active client links as one subscription file", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.dryRun {
				return fmt.Errorf("subscription export writes credentials; --dry-run is not supported")
			}
			entries, err := a.subscriptionEntries(selection)
			if err != nil {
				return err
			}
			data, err := subscription.Encode(entries, time.Now(), format)
			if err != nil {
				return fmt.Errorf("subscription export: %w", err)
			}
			if err := writePrivateClientConfig(out, data); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "exported %d subscription profiles\n", len(entries))
			return nil
		},
	}
	subscriptionSelectionFlags(cmd, &selection)
	cmd.Flags().StringVar(&out, "out", "", "Output file (written atomically with mode 0600)")
	cmd.Flags().StringVar(&format, "format", "base64", "Subscription format: base64|links")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func (a *App) newUserSubscriptionPublishCmd() *cobra.Command {
	var selection subscriptionSelection
	var dir, id, origin, title string
	var rotate bool
	cmd := &cobra.Command{
		Use: "publish", Short: "Create or refresh a revocable subscription bundle", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.dryRun {
				return fmt.Errorf("subscription publish writes credentials; --dry-run is not supported")
			}
			if id == "" || rotate || origin != "" {
				if err := subscription.ValidateBaseURL(origin); err != nil {
					return err
				}
			}
			entries, err := a.subscriptionEntries(selection)
			if err != nil {
				return err
			}
			store, err := subscription.Open(dir)
			if err != nil {
				return err
			}
			defer store.Close()
			publishedID, token, err := store.Publish(id, strings.TrimSpace(selection.username), title, entries, rotate)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "subscription id: %s\nprofiles: %d\n", publishedID, len(entries))
			if token != "" {
				link, err := subscription.URL(origin, publishedID, token)
				if err != nil {
					return err
				}
				a.sharedContent = link
				fmt.Fprintln(cmd.OutOrStdout(), link)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "subscription updated; existing URL retained")
			}
			return nil
		},
	}
	subscriptionSelectionFlags(cmd, &selection)
	cmd.Flags().StringVar(&dir, "bundle-dir", "", "Private directory for prepared delivery bundles")
	cmd.Flags().StringVar(&id, "id", "", "Existing subscription id to refresh")
	cmd.Flags().StringVar(&origin, "base-url", "", "Delivery HTTPS origin (required when creating or rotating a URL)")
	cmd.Flags().StringVar(&title, "title", "", "Client subscription title (default: ovpn, or existing title)")
	cmd.Flags().BoolVar(&rotate, "rotate-token", false, "Replace the delivery token and print the new URL")
	_ = cmd.MarkFlagRequired("bundle-dir")
	return cmd
}
