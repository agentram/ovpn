package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"ovpn/internal/model"
)

// newServerProfileCmd builds the `server profile` command group for transport profiles.
func (a *App) newServerProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage server transport profiles",
	}
	cmd.AddCommand(
		a.newServerProfileListCmd(),
		a.newServerProfileEnableCmd(),
		a.newServerProfileDisableCmd(),
	)
	return cmd
}

func (a *App) newServerProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <server>",
		Short: "List transport profiles for a server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			fmt.Println(renderServerProfileTable(*srv))
			return nil
		},
	}
}

func (a *App) newServerProfileEnableCmd() *cobra.Command {
	var setPrimary bool
	cmd := &cobra.Command{
		Use:   "enable <server> <profile>",
		Short: "Enable a transport profile locally",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, profile, err := a.loadProfileMutationTarget(args[0], args[1])
			if err != nil {
				return err
			}
			enabled := model.ParseTransportProfilesCSV(srv.EnabledProfiles)
			if !slices.Contains(enabled, profile) {
				enabled = append(enabled, profile)
			}
			srv.EnabledProfiles = model.JoinTransportProfiles(enabled)
			if setPrimary {
				srv.PrimaryProfile = profile
			}
			srv.PrimaryProfile = model.EffectivePrimaryTransportProfile(srv.PrimaryProfile, srv.EnabledProfiles)
			if err := a.store.UpdateServer(a.ctx, srv); err != nil {
				return err
			}
			fmt.Printf("enabled profile %s on %s\n", profile, srv.Name)
			if srv.PrimaryProfile == profile {
				fmt.Printf("primary profile: %s\n", profile)
			}
			fmt.Printf("next: ovpn deploy %s\n", srv.Name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&setPrimary, "primary", false, "Also make this profile primary for default link generation")
	return cmd
}

func (a *App) newServerProfileDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <server> <profile>",
		Short: "Disable a transport profile locally",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, profile, err := a.loadProfileMutationTarget(args[0], args[1])
			if err != nil {
				return err
			}
			enabled := model.ParseTransportProfilesCSV(srv.EnabledProfiles)
			if !slices.Contains(enabled, profile) {
				return fmt.Errorf("profile %s is already disabled on %s", profile, srv.Name)
			}
			if len(enabled) == 1 {
				return fmt.Errorf("cannot disable the last enabled profile on %s", srv.Name)
			}
			next := make([]string, 0, len(enabled)-1)
			for _, item := range enabled {
				if item != profile {
					next = append(next, item)
				}
			}
			srv.EnabledProfiles = model.JoinTransportProfiles(next)
			srv.PrimaryProfile = model.EffectivePrimaryTransportProfile(srv.PrimaryProfile, srv.EnabledProfiles)
			if err := a.store.UpdateServer(a.ctx, srv); err != nil {
				return err
			}
			fmt.Printf("disabled profile %s on %s\n", profile, srv.Name)
			fmt.Printf("primary profile: %s\n", srv.PrimaryProfile)
			fmt.Printf("next: ovpn deploy %s\n", srv.Name)
			return nil
		},
	}
}

func (a *App) loadProfileMutationTarget(serverName string, profileName string) (*model.Server, string, error) {
	srv, err := a.store.GetServerByName(a.ctx, serverName)
	if err != nil {
		return nil, "", err
	}
	profile := model.NormalizeTransportProfile(profileName)
	if profile == "" {
		return nil, "", fmt.Errorf("unknown profile %q; known profiles: %s", profileName, supportedTransportProfilesText())
	}
	if !model.TransportProfileRenderSupported(profile) {
		return nil, "", fmt.Errorf("profile %s is not supported by deploy in this build; supported now: %s", profile, model.RenderSupportedTransportProfilesText())
	}
	return srv, profile, nil
}

func renderServerProfileTable(srv model.Server) string {
	enabled := model.ParseTransportProfilesCSV(srv.EnabledProfiles)
	primary := model.EffectivePrimaryTransportProfile(srv.PrimaryProfile, srv.EnabledProfiles)
	tw := table.NewWriter()
	tw.SetStyle(table.StyleRounded)
	tw.AppendHeader(table.Row{"Profile", "Status", "Port", "Enabled", "Primary", "Description"})
	for _, profile := range model.SupportedTransportProfiles() {
		tw.AppendRow(table.Row{
			profile.Name,
			profile.Status,
			profile.Port,
			slices.Contains(enabled, profile.Name),
			profile.Name == primary,
			profile.Description,
		})
	}
	return tw.Render()
}

func supportedTransportProfilesText() string {
	var names []string
	for _, profile := range model.SupportedTransportProfiles() {
		names = append(names, profile.Name)
	}
	return strings.Join(names, ", ")
}
