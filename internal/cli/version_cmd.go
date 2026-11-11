package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"ovpn/internal/version"
)

// versionCmd prints the pinned CLI version.
func (a *App) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print pinned ovpn version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(a.output(), version.Current())
			return nil
		},
	}
}
