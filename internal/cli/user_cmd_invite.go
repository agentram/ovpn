package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"ovpn/internal/subscription"

	"github.com/spf13/cobra"
)

func (a *App) newUserInviteCmd() *cobra.Command {
	var selection subscriptionSelection
	var origin, dir, inviteDir string
	cmd := &cobra.Command{Use: "invite", Short: "Create a one-time setup code with verified HTTPS delivery", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if a.dryRun {
			return fmt.Errorf("issuing access sends traffic and writes credentials; --dry-run is not supported")
		}
		if dir == "" {
			dir = filepath.Join(a.dataDir, "delivery", "bundles")
		}
		if inviteDir == "" {
			inviteDir = filepath.Join(a.dataDir, "delivery", "invites")
		}
		if err := subscription.ValidateBaseURL(origin); err != nil {
			return fmt.Errorf("DELIVERY_UNAVAILABLE")
		}
		entries, err := a.subscriptionEntries(selection)
		if err != nil {
			return err
		}
		bundles, err := subscription.Open(dir)
		if err != nil {
			return err
		}
		defer bundles.Close()
		id, token, err := bundles.Publish("", selection.username, "", entries, false)
		if err != nil {
			return err
		}
		keep := false
		defer func() {
			if !keep {
				_ = bundles.Revoke(id)
			}
		}()
		link, err := subscription.URL(origin, id, token)
		if err != nil {
			return err
		}
		encoded, err := subscription.Encode(entries, time.Now(), "base64")
		if err != nil {
			return err
		}
		if err = subscription.VerifyDelivery(cmd.Context(), origin, link, encoded); err != nil {
			return err
		}
		bundle, err := bundles.Read(id)
		if err != nil {
			return err
		}
		invites, err := subscription.OpenInvites(inviteDir)
		if err != nil {
			return err
		}
		defer invites.Close()
		code, err := invites.Issue(bundle, origin, time.Now(), 30*time.Minute)
		if err != nil {
			return err
		}
		keep = true
		a.sharedContent = code
		a.sharedData, _ = json.Marshal(map[string]string{"subscriptionId": id})
		fmt.Fprintln(a.output(), code)
		return nil
	}}
	subscriptionSelectionFlags(cmd, &selection)
	selection.allProfiles = true
	cmd.Flags().Lookup("all-profiles").DefValue = "true"
	cmd.Flags().StringVar(&origin, "base-url", "", "HTTPS address of the prepared profile delivery service")
	cmd.Flags().StringVar(&dir, "bundle-dir", "", "Prepared bundle directory (default: workspace/delivery/bundles)")
	cmd.Flags().StringVar(&inviteDir, "invite-dir", "", "Invitation directory (default: workspace/delivery/invites)")
	_ = cmd.MarkFlagRequired("base-url")
	return cmd
}
