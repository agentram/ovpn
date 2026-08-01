package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"ovpn/internal/deploy"
	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

// configCmd prepares config cmd files and filesystem state.
func (a *App) configCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Render and validate config"}
	var server string

	render := &cobra.Command{
		Use:   "render",
		Short: "Render xray config JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			trimmedServer, err := requiredFlagValue("--server", server)
			if err != nil {
				return err
			}
			server = trimmedServer
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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			trimmedServer, err := requiredFlagValue("--server", server)
			if err != nil {
				return err
			}
			server = trimmedServer
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
					geositePath, geoipPath, err := a.ensureProxyGeodataAssets(*srv)
					if err != nil {
						return err
					}
					extraMounts = append(extraMounts,
						"-v", fmt.Sprintf("%s:/usr/local/share/xray/geosite.dat:ro", geositePath),
						"-v", fmt.Sprintf("%s:/usr/local/share/xray/geoip.dat:ro", geoipPath),
					)
				}
				if srv.IsTransportProfileEnabled(model.TransportProfileTLSSelfSNIWeb) {
					certDir, err := writeValidationTLSSelfSNICertificate()
					if err != nil {
						return err
					}
					defer os.RemoveAll(certDir)
					extraMounts = append(extraMounts, "-v", fmt.Sprintf("%s:/etc/xray/certs:ro", certDir))
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

// writeValidationTLSSelfSNICertificate creates a disposable certificate so local
// Xray validation can parse a self-SNI profile. Deploy validation uses the real
// Ansible-managed certificate on the target host.
func writeValidationTLSSelfSNICertificate() (string, error) {
	dir, err := os.MkdirTemp("", "ovpn-validate-selfsni-")
	if err != nil {
		return "", fmt.Errorf("create temporary TLS validation directory: %w", err)
	}
	cleanup := func(err error) (string, error) {
		_ = os.RemoveAll(dir)
		return "", err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return cleanup(fmt.Errorf("generate temporary TLS validation key: %w", err))
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return cleanup(fmt.Errorf("generate temporary TLS validation serial: %w", err))
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "ovpn-validation.invalid"},
		DNSNames:     []string{"ovpn-validation.invalid"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certificate, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return cleanup(fmt.Errorf("generate temporary TLS validation certificate: %w", err))
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return cleanup(fmt.Errorf("encode temporary TLS validation key: %w", err))
	}
	if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), 0o644); err != nil {
		return cleanup(fmt.Errorf("write temporary TLS validation certificate: %w", err))
	}
	if err := os.WriteFile(filepath.Join(dir, "privkey.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return cleanup(fmt.Errorf("write temporary TLS validation key: %w", err))
	}
	return dir, nil
}
