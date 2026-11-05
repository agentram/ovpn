package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/app"
	"ovpn/internal/probe"
)

func (a *App) newUserProbeCmd() *cobra.Command {
	var server, username, profile string
	var jsonOutput bool
	opts := probe.Options{}
	cmd := &cobra.Command{
		Use: "probe", Short: "Check HTTPS traffic through a user's client configuration", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.dryRun {
				return fmt.Errorf("user probe sends traffic; --dry-run is not supported")
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			srv, err := a.store.GetServerByName(a.ctx, server)
			if err != nil {
				return err
			}
			user, err := a.store.GetUser(a.ctx, srv.ID, username)
			if err != nil {
				return err
			}
			result, checkErr := app.CheckClient(a.ctx, *srv, *user, profile, opts)
			if checkErr != nil && result.ErrorCode == "" {
				return checkErr
			}
			if jsonOutput {
				if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
					return err
				}
			} else if result.Success {
				fmt.Printf("traffic probe passed: HTTP %d, %d bytes, %d ms (TTFB %d ms)\n", result.StatusCode, result.Bytes, result.DurationMS, result.TTFBMS)
			} else {
				fmt.Printf("traffic probe failed: %s (%d ms)\n", result.ErrorCode, result.DurationMS)
			}
			return checkErr
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "Server name")
	cmd.Flags().StringVar(&username, "username", "", "Username")
	cmd.Flags().StringVar(&profile, "profile", "", "Transport profile (default: server primary profile)")
	cmd.Flags().StringVar(&opts.URL, "url", "https://example.com/", "HTTPS URL to request through the tunnel")
	cmd.Flags().IntVar(&opts.ExpectedStatus, "expect-status", 200, "Required HTTP response status")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", 20*time.Second, "Overall probe timeout (maximum 5m)")
	cmd.Flags().Int64Var(&opts.MaxBodyBytes, "max-bytes", 1<<20, "Maximum response body bytes (maximum 16 MiB)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print a credential-free JSON result")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}
