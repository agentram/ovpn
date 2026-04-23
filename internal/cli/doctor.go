package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"ovpn/internal/doctor"
	"ovpn/internal/model"
)

type doctorOptions struct {
	jsonOutput  bool
	includeLogs bool
	tail        int
}

type agentHealth struct {
	OK               bool   `json:"ok"`
	Service          string `json:"service"`
	XrayAPI          string `json:"xray_api"`
	XrayAPIReachable *bool  `json:"xray_api_reachable,omitempty"`
	LastCollectAt    string `json:"last_collect_at,omitempty"`
	LastResetAt      string `json:"last_reset_at,omitempty"`
	DBPath           string `json:"db_path,omitempty"`
}

// doctorCmd builds the Cobra command for doctor.
func (a *App) doctorCmd() *cobra.Command {
	var opts doctorOptions
	cmd := &cobra.Command{
		Use:          "doctor <server>",
		Short:        "Run diagnostics for a server",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.tail <= 0 {
				return errors.New("--tail must be > 0")
			}
			report, err := a.runDoctor(args[0], opts)
			verboseOutput := a.verbose || a.debug || strings.EqualFold(a.logLevel, "debug")
			if printErr := printDoctorReport(report, opts.jsonOutput, verboseOutput); printErr != nil {
				return printErr
			}
			if err != nil {
				return err
			}
			if report.OverallStatus == doctor.StatusFail {
				return errors.New("doctor detected failing checks")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print diagnostics report as JSON")
	cmd.Flags().BoolVar(&opts.includeLogs, "include-logs", false, "Include recent logs from remote services")
	cmd.Flags().IntVar(&opts.tail, "tail", 100, "Number of log lines per service when --include-logs is used")
	return cmd
}

// runDoctor executes doctor flow and returns the first error.
func (a *App) runDoctor(serverName string, opts doctorOptions) (doctor.Report, error) {
	report := doctor.NewReport(serverName)
	srv, err := a.store.GetServerByName(a.ctx, serverName)
	if err != nil {
		report.Add(doctor.Check{
			Name:    "Local config sanity",
			Status:  doctor.StatusFail,
			Message: "server not found in local state",
			Details: []string{err.Error()},
			Hint:    "Run `ovpn server list` and register the server with `ovpn server add` if missing.",
		})
		addRemoteSkips(&report, true)
		report.Finalize()
		return report, fmt.Errorf("server %q not found: %w", serverName, err)
	}

	report.Add(checkLocalConfig(*srv))
	report.Add(a.checkProxyTopology(*srv))
	cfg := sshFromServer(*srv)
	runner := a.newRunner("doctor")

	sshCheck, sshOK := a.checkSSH(runner, cfg)
	report.Add(sshCheck)
	if !sshOK {
		// Stop remote checks early when SSH is broken; keep report readable with explicit skips.
		addRemoteSkips(&report, false)
		report.Finalize()
		return report, nil
	}

	report.Add(a.checkSudo(runner, cfg))
	report.Add(a.checkDocker(runner, cfg))
	report.Add(a.checkDeployFiles(runner, cfg, *srv))
	serviceUsers, err := a.vpnServiceUsers(*srv)
	if err != nil {
		report.Add(doctor.Check{
			Name:    "Proxy backend service identity",
			Status:  doctor.StatusFail,
			Message: "backend proxy relay identity is misconfigured in local state",
			Details: []string{err.Error()},
			Hint:    "Re-attach the backend to a proxy or set proxy_service_uuid before deploying HA.",
		})
	} else if len(serviceUsers) > 0 {
		report.Add(a.checkProxyServiceRuntimeIdentity(runner, cfg, *srv))
	}
	report.Add(a.checkComposeState(runner, cfg, *srv))
	report.Add(a.checkXrayConfig(runner, cfg))
	report.Add(a.checkAgentHealth(runner, cfg, *srv))
	report.Add(a.checkDisk(runner, cfg))

	if opts.includeLogs {
		logs := a.collectDoctorLogs(*srv, opts.tail)
		if len(logs) > 0 {
			report.Logs = logs
		}
	}

	report.Finalize()
	return report, nil
}

func (a *App) checkProxyTopology(srv model.Server) doctor.Check {
	check := doctor.Check{
		Name:    "Proxy topology",
		Status:  doctor.StatusPass,
		Message: "server role topology is coherent",
	}
	if !srv.IsProxy() {
		check.Details = []string{"role=" + srv.NormalizedRole()}
		return check
	}
	backends, err := a.attachedBackendServers(srv)
	if err != nil {
		check.Status = doctor.StatusFail
		check.Message = "proxy backend attachments could not be read"
		check.Details = []string{err.Error()}
		check.Hint = "Use `ovpn server backend list --proxy <server>` and repair local state."
		return check
	}
	if len(backends) == 0 {
		check.Status = doctor.StatusFail
		check.Message = "proxy has no attached backends"
		check.Hint = "Attach at least one vpn backend with `ovpn server backend attach --proxy <proxy> --backend <vpn>`."
		return check
	}
	if err := a.ensureVPNBackendsCompatible(backends); err != nil {
		check.Status = doctor.StatusFail
		check.Message = "attached backends are not parity-compatible"
		check.Details = []string{err.Error()}
		check.Hint = "Align REALITY and proxy service UUID values across backend vpn servers."
		return check
	}
	var names []string
	for _, backend := range backends {
		names = append(names, backend.Name)
	}
	check.Details = []string{
		"role=proxy",
		"proxy_preset=" + srv.NormalizedProxyPreset(),
		fmt.Sprintf("backends=%s", strings.Join(names, ",")),
	}
	geodataDetails, geodataStale, err := a.proxyGeodataState(srv)
	if err != nil {
		check.Status = doctor.StatusFail
		check.Message = "proxy geodata assets are missing or unreadable"
		check.Details = append(check.Details, err.Error())
		check.Hint = "Run `ovpn config validate --server <proxy>` or deploy the proxy to refresh geodata assets."
		return check
	}
	check.Details = append(check.Details, geodataDetails...)
	if geodataStale {
		check.Status = doctor.StatusWarn
		check.Message = "proxy topology is coherent but geodata assets are stale"
		check.Hint = "Re-run proxy deploy or `ovpn config validate --server <proxy>` to refresh geodata assets."
	}
	return check
}

// printDoctorReport returns print doctor report.
func printDoctorReport(report doctor.Report, jsonOutput, verbose bool) error {
	if jsonOutput {
		raw, err := report.JSON()
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	fmt.Print(doctor.FormatHuman(report, verbose))
	return nil
}

// checkLocalConfig returns check local config.
func checkLocalConfig(srv model.Server) doctor.Check {
	check := doctor.Check{
		Name:    "Local config sanity",
		Status:  doctor.StatusPass,
		Message: "local server record looks valid",
		Details: []string{
			fmt.Sprintf("server=%s", srv.Name),
			fmt.Sprintf("host=%s", srv.Host),
			fmt.Sprintf("domain=%s", srv.Domain),
			fmt.Sprintf("ssh=%s:%d", srv.SSHUser, srv.SSHPort),
			fmt.Sprintf("xray_version=%s", srv.XrayVersion),
		},
	}
	if err := srv.Validate(); err != nil {
		check.Status = doctor.StatusFail
		check.Message = "local server record is invalid"
		check.Details = append(check.Details, "validation_error="+err.Error())
		check.Hint = "Fix server fields in local state or recreate the server entry."
		return check
	}
	var warns []string
	if isFloatingXrayTag(srv.XrayVersion) {
		warns = append(warns, "xray_version uses a floating tag; pin an explicit version")
	}
	if strings.EqualFold(strings.TrimSpace(srv.Domain), strings.TrimSpace(srv.Host)) {
		warns = append(warns, "domain equals host; DNS-based camouflage is limited")
	}
	targetHost, targetPort := splitRealityTargetHostPort(srv.RealityTarget)
	if targetPort == "" {
		warns = append(warns, "reality_target has no explicit port; prefer host:443 for predictable fallback behavior")
	} else if targetPort != "443" {
		warns = append(warns, "reality_target uses non-443 port; this may look less realistic for TLS camouflage")
	}
	if strings.EqualFold(strings.TrimSpace(targetHost), strings.TrimSpace(srv.Host)) || strings.EqualFold(strings.TrimSpace(targetHost), strings.TrimSpace(srv.Domain)) {
		warns = append(warns, "reality_target points to this VPN host/domain; choose an external realistic target")
	}
	if len(warns) > 0 {
		check.Status = doctor.StatusWarn
		check.Message = "local server record is usable but has warnings"
		check.Details = append(check.Details, warns...)
	}
	return check
}

// isFloatingXrayTag reports whether floating xray tag.
func isFloatingXrayTag(tag string) bool {
	v := strings.ToLower(strings.TrimSpace(tag))
	switch v {
	case "", "latest", "main":
		return true
	}
	return strings.HasPrefix(v, "dev-")
}

// splitRealityTargetHostPort returns split reality target host port.
func splitRealityTargetHostPort(raw string) (string, string) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", ""
	}
	if strings.HasPrefix(target, "[") {
		closing := strings.LastIndex(target, "]")
		if closing > 1 {
			host := strings.TrimSpace(target[1:closing])
			rest := strings.TrimSpace(target[closing+1:])
			if strings.HasPrefix(rest, ":") && len(rest) > 1 {
				return host, strings.TrimPrefix(rest, ":")
			}
			return host, ""
		}
	}
	if idx := strings.LastIndex(target, ":"); idx > 0 && idx < len(target)-1 && !strings.Contains(target[idx+1:], ":") {
		return strings.TrimSpace(target[:idx]), strings.TrimSpace(target[idx+1:])
	}
	return target, ""
}
