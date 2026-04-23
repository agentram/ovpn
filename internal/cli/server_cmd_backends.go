package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"ovpn/internal/model"
)

// newServerBackendCmd initializes server backend cmd with the required dependencies.
func (a *App) newServerBackendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backend",
		Short: "Manage proxy backend attachments",
	}
	cmd.AddCommand(
		a.newServerBackendAttachCmd(),
		a.newServerBackendDetachCmd(),
		a.newServerBackendListCmd(),
	)
	return cmd
}

func (a *App) newServerBackendAttachCmd() *cobra.Command {
	var opts struct {
		proxy    string
		backend  string
		priority int
	}
	cmd := &cobra.Command{
		Use:   "attach",
		Short: "Attach a vpn backend to a proxy server",
		RunE: func(cmd *cobra.Command, args []string) error {
			proxy, err := a.store.GetServerByName(a.ctx, opts.proxy)
			if err != nil {
				return err
			}
			if !proxy.IsProxy() {
				return fmt.Errorf("server %s is role %s, expected proxy", proxy.Name, proxy.NormalizedRole())
			}
			backend, err := a.store.GetServerByName(a.ctx, opts.backend)
			if err != nil {
				return err
			}
			if !backend.IsVPN() {
				return fmt.Errorf("server %s is role %s, expected vpn backend", backend.Name, backend.NormalizedRole())
			}
			existingBackends, err := a.attachedBackendServers(*proxy)
			if err != nil {
				return err
			}
			if err := a.ensureBackendProxyServiceUUID(backend, existingBackends); err != nil {
				return err
			}
			candidatePool := make([]model.Server, 0, len(existingBackends)+1)
			for _, existing := range existingBackends {
				if existing.ID == backend.ID {
					continue
				}
				candidatePool = append(candidatePool, existing)
			}
			candidatePool = append(candidatePool, *backend)
			if err := a.ensureVPNBackendsCompatible(candidatePool); err != nil {
				return err
			}
			if err := a.store.UpsertProxyBackend(a.ctx, &model.ProxyBackend{
				ProxyServerID:   proxy.ID,
				BackendServerID: backend.ID,
				Enabled:         true,
				Priority:        opts.priority,
			}); err != nil {
				return err
			}
			fmt.Printf("backend attached: proxy=%s backend=%s priority=%d\n", proxy.Name, backend.Name, opts.priority)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.proxy, "proxy", "", "Proxy server name")
	cmd.Flags().StringVar(&opts.backend, "backend", "", "Backend vpn server name")
	cmd.Flags().IntVar(&opts.priority, "priority", 100, "Backend priority (lower is preferred in list output)")
	_ = cmd.MarkFlagRequired("proxy")
	_ = cmd.MarkFlagRequired("backend")
	return cmd
}

func (a *App) newServerBackendDetachCmd() *cobra.Command {
	var opts struct {
		proxy   string
		backend string
	}
	cmd := &cobra.Command{
		Use:   "detach",
		Short: "Detach a vpn backend from a proxy server",
		RunE: func(cmd *cobra.Command, args []string) error {
			proxy, err := a.store.GetServerByName(a.ctx, opts.proxy)
			if err != nil {
				return err
			}
			backend, err := a.store.GetServerByName(a.ctx, opts.backend)
			if err != nil {
				return err
			}
			if err := a.store.DeleteProxyBackend(a.ctx, proxy.ID, backend.ID); err != nil {
				return err
			}
			fmt.Printf("backend detached: proxy=%s backend=%s\n", proxy.Name, backend.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.proxy, "proxy", "", "Proxy server name")
	cmd.Flags().StringVar(&opts.backend, "backend", "", "Backend vpn server name")
	_ = cmd.MarkFlagRequired("proxy")
	_ = cmd.MarkFlagRequired("backend")
	return cmd
}

func (a *App) newServerBackendListCmd() *cobra.Command {
	var proxyName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List vpn backends attached to a proxy server",
		RunE: func(cmd *cobra.Command, args []string) error {
			proxy, err := a.store.GetServerByName(a.ctx, proxyName)
			if err != nil {
				return err
			}
			mappings, err := a.store.ListProxyBackends(a.ctx, proxy.ID)
			if err != nil {
				return err
			}
			if len(mappings) == 0 {
				fmt.Println("no backends")
				return nil
			}
			for _, mapping := range mappings {
				name := fmt.Sprintf("%d", mapping.BackendServerID)
				if mapping.BackendServer != nil {
					name = mapping.BackendServer.Name
				}
				state := "disabled"
				if mapping.Enabled {
					state = "enabled"
				}
				fmt.Printf("%s\tpriority=%d\t%s\n", name, mapping.Priority, state)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&proxyName, "proxy", "", "Proxy server name")
	_ = cmd.MarkFlagRequired("proxy")
	return cmd
}
