package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
)

func (a *App) newServerProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage server transport profiles",
	}
	cmd.AddCommand(
		a.newServerProfileListCmd(),
		a.newServerProfileEnableCmd(),
		a.newServerProfileDisableCmd(),
		a.newServerProfileSwitchCmd(),
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
			enabled := map[string]bool{}
			for _, p := range srv.NormalizedEnabledProfiles() {
				enabled[p] = true
			}
			fmt.Println(renderServerProfileTable(*srv, enabled))
			return nil
		},
	}
}

func (a *App) newServerProfileEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <server> <profile>",
		Short: "Enable a transport profile on a server",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			profile := model.NormalizeTransportProfile(args[1])
			if profile == "" {
				return unsupportedTransportProfileError(args[1])
			}
			if profile == model.TransportProfileWSTLSWeb {
				return plannedTransportProfileError(profile, srv.Name)
			}
			profiles := append(srv.NormalizedEnabledProfiles(), profile)
			srv.EnabledProfiles = model.EnabledProfilesCSV(srv.NormalizedPrimaryProfile(), strings.Join(profiles, ","))
			if err := a.store.UpdateServer(a.ctx, srv); err != nil {
				return err
			}
			fmt.Printf("enabled profile %s on %s\n", profile, srv.Name)
			fmt.Println("redeploy the server before using generated links for newly enabled profiles")
			return nil
		},
	}
}

func (a *App) newServerProfileDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <server> <profile>",
		Short: "Disable a non-primary transport profile on a server",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			profile := model.NormalizeTransportProfile(args[1])
			if profile == "" {
				return unsupportedTransportProfileError(args[1])
			}
			primary := srv.NormalizedPrimaryProfile()
			if profile == primary {
				return fmt.Errorf("cannot disable primary profile %s on %s; switch primary first with `ovpn server profile switch %s <other-profile>`, then redeploy", profile, srv.Name, srv.Name)
			}
			current := srv.NormalizedEnabledProfiles()
			next := make([]string, 0, len(current))
			removed := false
			for _, item := range current {
				if item == profile {
					removed = true
					continue
				}
				next = append(next, item)
			}
			if !removed {
				fmt.Printf("profile %s is already disabled on %s\n", profile, srv.Name)
				return nil
			}
			srv.EnabledProfiles = model.EnabledProfilesCSV(primary, strings.Join(next, ","))
			if err := a.store.UpdateServer(a.ctx, srv); err != nil {
				return err
			}
			fmt.Printf("disabled profile %s on %s\n", profile, srv.Name)
			fmt.Println("redeploy the server before assuming old links for this profile have stopped working")
			return nil
		},
	}
}

func (a *App) newServerProfileSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch <server> <profile>",
		Short: "Set the primary transport profile for generated links",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			profile := model.NormalizeTransportProfile(args[1])
			if profile == "" {
				return unsupportedTransportProfileError(args[1])
			}
			if profile == model.TransportProfileWSTLSWeb {
				return plannedTransportProfileError(profile, srv.Name)
			}
			srv.PrimaryProfile = profile
			srv.EnabledProfiles = model.EnabledProfilesCSV(profile, srv.EnabledProfiles)
			if err := a.store.UpdateServer(a.ctx, srv); err != nil {
				return err
			}
			fmt.Printf("primary profile for %s: %s\n", srv.Name, profile)
			fmt.Println("redeploy the server before using generated links if the profile was not already live")
			return nil
		},
	}
}

func unsupportedTransportProfileError(raw string) error {
	return fmt.Errorf("unsupported transport profile %q; supported profiles: %s", raw, model.SupportedTransportProfilesText())
}

func plannedTransportProfileError(profile string, serverName string) error {
	return fmt.Errorf("%s is planned but not deployable yet; choose an enabled deployable profile from `ovpn server profile list %s`", profile, serverName)
}
