package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"ovpn/internal/deploy"
	"ovpn/internal/model"
	"ovpn/internal/util"
)

// newServerAddCmd initializes server add cmd with the required dependencies.
func (a *App) newServerAddCmd() *cobra.Command {
	var add struct {
		name         string
		role         string
		proxyPreset  string
		host         string
		domain       string
		sshUser      string
		sshPort      int
		identityFile string
		knownHosts   string
		strict       bool
		xrayVersion  string
		realitySNI   string
		realityTGT   string
		privateKey   string
		publicKey    string
		shortID      string
	}
	addCmd := &cobra.Command{
		Use:   "add",
		Short: "Register a server in local state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := util.RequireNonEmpty("name", add.name); err != nil {
				return err
			}
			if err := util.RequireNonEmpty("host", add.host); err != nil {
				return err
			}
			if err := util.RequireNonEmpty("domain", add.domain); err != nil {
				return err
			}
			add.role = model.NormalizeServerRole(add.role)
			if add.role == "" {
				return fmt.Errorf("role must be %q or %q", model.ServerRoleVPN, model.ServerRoleProxy)
			}
			existingServers, err := a.listEnabledServersByRole(add.role)
			if err != nil {
				return err
			}
			if len(existingServers) > 0 {
				if err := ensureRealityParityForServers(existingServers); err != nil {
					return err
				}
				if _, err := a.canonicalGlobalUsers(); err != nil {
					return err
				}
				baseline := existingServers[0]
				if !cmd.Flags().Changed("reality-private-key") {
					add.privateKey = baseline.RealityPrivateKey
				}
				if !cmd.Flags().Changed("reality-public-key") {
					add.publicKey = baseline.RealityPublicKey
				}
				if !cmd.Flags().Changed("reality-short-id") {
					add.shortID = baseline.RealityShortIDs
				}
				if !cmd.Flags().Changed("reality-server-name") {
					add.realitySNI = baseline.RealityServerName
				}
				if !cmd.Flags().Changed("reality-target") {
					add.realityTGT = baseline.RealityTarget
				}
				if cmd.Flags().Changed("reality-private-key") && strings.TrimSpace(add.privateKey) != strings.TrimSpace(baseline.RealityPrivateKey) {
					return fmt.Errorf("reality-private-key must match existing cluster baseline (%s)", baseline.Name)
				}
				if cmd.Flags().Changed("reality-public-key") && strings.TrimSpace(add.publicKey) != strings.TrimSpace(baseline.RealityPublicKey) {
					return fmt.Errorf("reality-public-key must match existing cluster baseline (%s)", baseline.Name)
				}
				if cmd.Flags().Changed("reality-short-id") && strings.TrimSpace(add.shortID) != strings.TrimSpace(baseline.RealityShortIDs) {
					return fmt.Errorf("reality-short-id must match existing cluster baseline (%s)", baseline.Name)
				}
				if cmd.Flags().Changed("reality-server-name") && strings.TrimSpace(add.realitySNI) != strings.TrimSpace(baseline.RealityServerName) {
					return fmt.Errorf("reality-server-name must match existing cluster baseline (%s)", baseline.Name)
				}
				if cmd.Flags().Changed("reality-target") && strings.TrimSpace(add.realityTGT) != strings.TrimSpace(baseline.RealityTarget) {
					return fmt.Errorf("reality-target must match existing cluster baseline (%s)", baseline.Name)
				}
			} else {
				if (add.privateKey == "") != (add.publicKey == "") {
					return fmt.Errorf("reality-private-key and reality-public-key must be provided together")
				}
				if add.privateKey == "" && add.publicKey == "" {
					priv, pub, err := generateX25519Pair()
					if err != nil {
						return err
					}
					add.privateKey = priv
					add.publicKey = pub
				}
				if strings.TrimSpace(add.shortID) == "" {
					add.shortID = randomShortID()
				}
			}
			srv := &model.Server{
				Name:              add.name,
				Role:              add.role,
				ProxyPreset:       add.proxyPreset,
				Host:              add.host,
				Domain:            add.domain,
				SSHUser:           add.sshUser,
				SSHPort:           add.sshPort,
				SSHIdentityFile:   add.identityFile,
				SSHKnownHostsFile: add.knownHosts,
				SSHStrictHostKey:  add.strict,
				XrayVersion:       normalizeXrayVersionTag(add.xrayVersion),
				RealityPrivateKey: add.privateKey,
				RealityPublicKey:  add.publicKey,
				RealityShortIDs:   add.shortID,
				RealityServerName: add.realitySNI,
				RealityTarget:     add.realityTGT,
				Enabled:           true,
			}
			if err := a.store.AddServer(a.ctx, srv); err != nil {
				return err
			}
			if err := a.materializeCanonicalUsersOnServer(*srv); err != nil {
				return fmt.Errorf("seed global users for %s: %w", srv.Name, err)
			}
			a.log().Info("server registered", "server", srv.Name, "host", srv.Host, "domain", srv.Domain, "xray_version", srv.XrayVersion)
			fmt.Printf("server added: %s (id=%d role=%s)\n", srv.Name, srv.ID, srv.NormalizedRole())
			if srv.IsProxy() {
				fmt.Printf("proxy preset: %s\n", srv.NormalizedProxyPreset())
			}
			fmt.Printf("reality public key: %s\n", srv.RealityPublicKey)
			fmt.Printf("reality short id: %s\n", srv.RealityShortIDs)
			if srv.ProxyServiceUUID != "" {
				fmt.Printf("proxy service uuid: %s\n", srv.ProxyServiceUUID)
			}
			return nil
		},
	}
	addCmd.Flags().StringVar(&add.name, "name", "", "server name")
	addCmd.Flags().StringVar(&add.role, "role", model.ServerRoleVPN, "Server role: vpn|proxy")
	addCmd.Flags().StringVar(&add.proxyPreset, "proxy-preset", "", "Proxy routing preset for role=proxy (default: ru)")
	addCmd.Flags().StringVar(&add.host, "host", "", "server IP or hostname")
	addCmd.Flags().StringVar(&add.domain, "domain", "", "server domain for clients")
	addCmd.Flags().StringVar(&add.sshUser, "ssh-user", os.Getenv("USER"), "SSH user")
	addCmd.Flags().IntVar(&add.sshPort, "ssh-port", 22, "SSH port")
	addCmd.Flags().StringVar(&add.identityFile, "ssh-identity", filepath.Join(util.HomeDir(), ".ssh", "id_rsa"), "SSH private key path")
	addCmd.Flags().StringVar(&add.knownHosts, "ssh-known-hosts", filepath.Join(util.HomeDir(), ".ssh", "known_hosts"), "SSH known_hosts path")
	addCmd.Flags().BoolVar(&add.strict, "ssh-strict-host-key", true, "Enable strict SSH host key checking")
	addCmd.Flags().StringVar(&add.xrayVersion, "xray-version", "26.3.27", "Pinned Xray version")
	addCmd.Flags().StringVar(&add.realitySNI, "reality-server-name", "www.microsoft.com", "REALITY serverName")
	addCmd.Flags().StringVar(&add.realityTGT, "reality-target", "www.microsoft.com:443", "REALITY target")
	addCmd.Flags().StringVar(&add.privateKey, "reality-private-key", "", "REALITY private key")
	addCmd.Flags().StringVar(&add.publicKey, "reality-public-key", "", "REALITY public key")
	addCmd.Flags().StringVar(&add.shortID, "reality-short-id", "", "REALITY short ID (hex)")
	_ = addCmd.MarkFlagRequired("name")
	_ = addCmd.MarkFlagRequired("host")
	_ = addCmd.MarkFlagRequired("domain")
	return addCmd
}

// newServerInitCmd initializes server init cmd with the required dependencies.
func (a *App) newServerInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init <server>",
		Short: "Bootstrap docker/compose and deploy stack to remote server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			a.log().Info("initializing server", "server", srv.Name, "host", srv.Host)
			return a.initOrDeployServer(*srv, true)
		},
	}
}

// newServerListCmd initializes server list cmd with the required dependencies.
func (a *App) newServerListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			servers, err := a.store.ListServers(a.ctx)
			if err != nil {
				return err
			}
			if len(servers) == 0 {
				fmt.Println("no servers")
				return nil
			}
			for _, s := range servers {
				status := "disabled"
				if s.Enabled {
					status = "enabled"
				}
				fmt.Printf("%d\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Name, s.NormalizedRole(), s.Host, s.Domain, s.XrayVersion, status)
			}
			return nil
		},
	}
}

// newServerSetXrayVersionCmd initializes server set xray version cmd with the required dependencies.
func (a *App) newServerSetXrayVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-xray-version <server> <version>",
		Short: "Set pinned Xray version in local state",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverName := strings.TrimSpace(args[0])
			version := normalizeXrayVersionTag(strings.TrimSpace(args[1]))
			if version == "" {
				return errors.New("version is required")
			}
			srv, err := a.store.GetServerByName(a.ctx, serverName)
			if err != nil {
				return err
			}
			prev := srv.XrayVersion
			srv.XrayVersion = version
			if err := a.store.UpdateServer(a.ctx, srv); err != nil {
				return err
			}
			a.log().Info("xray version pin updated", "server", srv.Name, "previous", prev, "current", srv.XrayVersion)
			fmt.Printf("server %s xray version: %s -> %s\n", srv.Name, prev, srv.XrayVersion)
			return nil
		},
	}
}

// newServerStatusCmd initializes server status cmd with the required dependencies.
func (a *App) newServerStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <server>",
		Short: "Show remote compose status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := a.store.GetServerByName(a.ctx, args[0])
			if err != nil {
				return err
			}
			runner := a.newRunner("server.status")
			a.log().Debug("requesting remote status", "server", srv.Name, "host", srv.Host)
			text, err := deploy.RemoteStatus(a.ctx, runner, sshFromServer(*srv))
			if err != nil {
				return fmt.Errorf("status server %s on %s: %w", srv.Name, srv.Host, err)
			}
			fmt.Println(text)
			return nil
		},
	}
}
