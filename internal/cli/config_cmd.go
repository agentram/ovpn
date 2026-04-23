package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"ovpn/internal/deploy"
	"ovpn/internal/xraycfg"
)

// configCmd prepares config cmd files and filesystem state.
func (a *App) configCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Render and validate config"}
	var server string

	render := &cobra.Command{
		Use:   "render",
		Short: "Render xray config JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, server)
			if err != nil {
				return err
			}
			users, err := a.store.ListUsers(a.ctx, srv.ID)
			if err != nil {
				return err
			}
			spec, err := a.buildXraySpec(*srv, users)
			if err != nil {
				return err
			}
			jsonRaw, err := xraycfg.RenderServerJSON(spec)
			if err != nil {
				return err
			}
			a.log().Debug("rendered xray config", "server", srv.Name, "users", len(users), "bytes", len(jsonRaw))
			fmt.Println(string(jsonRaw))
			return nil
		},
	}
	render.Flags().StringVar(&server, "server", "", "Server name")
	_ = render.MarkFlagRequired("server")

	validate := &cobra.Command{
		Use:   "validate",
		Short: "Validate rendered config (JSON + optional docker xray test)",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, server)
			if err != nil {
				return err
			}
			users, err := a.store.ListUsers(a.ctx, srv.ID)
			if err != nil {
				return err
			}
			spec, err := a.buildXraySpec(*srv, users)
			if err != nil {
				return err
			}
			jsonRaw, err := xraycfg.RenderServerJSON(spec)
			if err != nil {
				return err
			}
			var tmp map[string]any
			if err := json.Unmarshal(jsonRaw, &tmp); err != nil {
				return fmt.Errorf("json invalid: %w", err)
			}
			xrayImage := "ghcr.io/xtls/xray-core:" + normalizeXrayVersionTag(srv.XrayVersion)
			configFile := filepath.Join(os.TempDir(), fmt.Sprintf("ovpn-validate-%s.json", srv.Name))
			defer os.Remove(configFile)
			if err := os.WriteFile(configFile, jsonRaw, 0o644); err != nil {
				return err
			}
			if _, err := exec.LookPath("docker"); err == nil {
				a.log().Debug("running docker-based xray config validation", "server", srv.Name, "xray_image", xrayImage)
				var extraMounts []string
				if srv.IsProxy() {
					geositePath, geoipPath, err := a.ensureProxyGeodataAssets()
					if err != nil {
						return err
					}
					extraMounts = append(extraMounts,
						"-v", fmt.Sprintf("%s:/usr/local/share/xray/geosite.dat:ro", geositePath),
						"-v", fmt.Sprintf("%s:/usr/local/share/xray/geoip.dat:ro", geoipPath),
					)
				}
				if err := deploy.ValidateConfigWithDockerAndMounts(a.ctx, xrayImage, configFile, extraMounts); err != nil {
					return err
				}
			}
			fmt.Println("config valid")
			return nil
		},
	}
	validate.Flags().StringVar(&server, "server", "", "Server name")
	_ = validate.MarkFlagRequired("server")

	cmd.AddCommand(render, validate)
	return cmd
}
