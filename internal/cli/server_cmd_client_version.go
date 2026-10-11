package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
)

func parseRealityMinClientVersion(raw string) (string, error) {
	version := strings.TrimSpace(raw)
	if version == "default" {
		version = ""
	}
	return version, model.ValidateRealityMinClientVersion(version)
}

func (a *App) newServerSetRealityMinClientVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-reality-min-client-version <server> <x.y.z|default>",
		Short: "Set the REALITY minimum client version in local state",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := parseRealityMinClientVersion(args[1])
			if err != nil {
				return err
			}
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			srv.RealityMinClientVersion = version
			if err := a.store.UpdateServer(a.ctx, srv); err != nil {
				return err
			}
			printRealityMinClientVersion(version)
			fmt.Printf("Run `ovpn deploy %s` to apply the setting; existing client links stay valid.\n", srv.Name)
			return nil
		},
	}
}

func printRealityMinClientVersion(version string) {
	if version == "" {
		fmt.Println("REALITY minimum client version: Xray default")
		return
	}
	// A custom minimum is an operator choice; Xray's default protects against outdated TLS fingerprints.
	fmt.Printf("REALITY minimum client version: %s\n", version)
	fmt.Println("Custom minimum enabled: keep client cores updated; outdated TLS fingerprints can make traffic easier to identify.")
}
