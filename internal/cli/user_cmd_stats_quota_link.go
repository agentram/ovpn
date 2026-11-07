package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"ovpn/internal/clientcfg"
	"ovpn/internal/clientprofile"
	"ovpn/internal/model"
	"ovpn/internal/xraycfg"
)

const quotaGBBytes int64 = 1024 * 1024 * 1024

// newUserTopCmd builds the `user top` command that lists the heaviest traffic consumers.
func (a *App) newUserTopCmd() *cobra.Command {
	var top struct {
		server string
		limit  int
	}
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Show top users by total traffic",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := requiredFlagValue("--server", top.server)
			if err != nil {
				return err
			}
			top.server = server
			srv, err := a.store.GetServerByName(a.ctx, top.server)
			if err != nil {
				return err
			}
			body, err := a.fetchRemoteAgent(*srv, "GET", a.agentURL("/stats/total"), nil)
			if err != nil {
				return err
			}
			var totals []model.UserTraffic
			if err := json.Unmarshal(body, &totals); err != nil {
				return err
			}
			users, err := a.store.ListUsers(a.ctx, srv.ID)
			if err != nil {
				return err
			}
			quotaByEmail := map[string]model.QuotaUserStatus{}
			if status, err := a.fetchQuotaStatus(*srv, ""); err == nil {
				for _, row := range status.Users {
					quotaByEmail[row.Email] = row
				}
			}
			rows := buildUserTopRows(totals, users, quotaByEmail, top.limit)
			if len(rows) == 0 {
				fmt.Println("no traffic rows")
				return nil
			}
			fmt.Println(renderUserTopTable(rows))
			return nil
		},
	}
	cmd.Flags().StringVar(&top.server, "server", "", "Server name")
	cmd.Flags().IntVar(&top.limit, "limit", 10, "Number of rows to show")
	_ = cmd.MarkFlagRequired("server")
	return cmd
}

// newUserQuotaResetCmd builds the `user quota-reset` command that clears a user's quota block.
func (a *App) newUserQuotaResetCmd() *cobra.Command {
	var quotaReset struct {
		username string
	}
	cmd := &cobra.Command{
		Use:   "quota-reset",
		Short: "Clear quota block for user and re-add at runtime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			username, err := requiredFlagValue("--username", quotaReset.username)
			if err != nil {
				return err
			}
			quotaReset.username = username
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, quotaReset.username); err != nil {
					if isNotFoundErr(err) {
						return fmt.Errorf("user %s does not exist on %s", quotaReset.username, srv.Name)
					}
					return err
				}
			}
			for _, srv := range targets {
				if err := a.resetUserQuotaOnServer(srv, quotaReset.username); err != nil {
					return fmt.Errorf("quota reset on %s: %w", srv.Name, err)
				}
			}
			if len(targets) == 1 {
				fmt.Printf("quota reset for user %s\n", quotaReset.username)
			} else {
				fmt.Printf("quota reset for user %s on %d servers\n", quotaReset.username, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&quotaReset.username, "username", "", "Username")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

// newUserQuotaSetCmd builds the `user quota-set` command that changes a user's rolling quota.
func (a *App) newUserQuotaSetCmd() *cobra.Command {
	var quotaSet struct {
		username    string
		monthlyByte int64
		monthlyGB   int64
		enabled     bool
	}
	cmd := &cobra.Command{
		Use:   "quota-set",
		Short: "Set per-user rolling 30d quota policy",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			if len(args) == 1 && strings.EqualFold(args[0], "gb") && cmd.Flags().Changed("monthly-gb") {
				return nil
			}
			return fmt.Errorf("unexpected argument %q; use --monthly-gb 400 or --monthly-bytes <bytes>", args[0])
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			username, err := requiredFlagValue("--username", quotaSet.username)
			if err != nil {
				return err
			}
			quotaSet.username = username
			monthlyByte, err := quotaBytesFromFlags(
				quotaSet.monthlyByte,
				quotaSet.monthlyGB,
				cmd.Flags().Changed("monthly-bytes"),
				cmd.Flags().Changed("monthly-gb"),
			)
			if err != nil {
				return err
			}
			targets, err := a.resolveUserMutationServers()
			if err != nil {
				return err
			}
			for _, srv := range targets {
				if _, err := a.store.GetUser(a.ctx, srv.ID, quotaSet.username); err != nil {
					if isNotFoundErr(err) {
						return fmt.Errorf("user %s does not exist on %s", quotaSet.username, srv.Name)
					}
					return err
				}
			}
			for _, srv := range targets {
				if err := a.setUserQuotaOnServer(srv, quotaSet.username, monthlyByte, quotaSet.enabled); err != nil {
					return fmt.Errorf("quota set on %s: %w", srv.Name, err)
				}
			}
			if len(targets) == 1 {
				fmt.Printf("quota updated for user %s\n", quotaSet.username)
			} else {
				fmt.Printf("quota updated for user %s on %d servers\n", quotaSet.username, len(targets))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&quotaSet.username, "username", "", "Username")
	cmd.Flags().Int64Var(&quotaSet.monthlyByte, "monthly-bytes", 0, "Rolling 30d quota in bytes (0 uses default 300GB)")
	cmd.Flags().Int64Var(&quotaSet.monthlyGB, "monthly-gb", 0, "Rolling 30d quota in GB, using 1024^3 bytes per GB (0 uses default 300GB)")
	cmd.Flags().BoolVar(&quotaSet.enabled, "enabled", true, "Enable rolling 30d quota enforcement for this user")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func quotaBytesFromFlags(monthlyBytes int64, monthlyGB int64, monthlyBytesSet bool, monthlyGBSet bool) (int64, error) {
	if monthlyBytes < 0 {
		return 0, fmt.Errorf("--monthly-bytes must be >= 0")
	}
	if monthlyGB < 0 {
		return 0, fmt.Errorf("--monthly-gb must be >= 0")
	}
	if monthlyBytesSet && monthlyGBSet {
		return 0, fmt.Errorf("use only one of --monthly-bytes or --monthly-gb")
	}
	if monthlyGBSet {
		if monthlyGB > (1<<63-1)/quotaGBBytes {
			return 0, fmt.Errorf("--monthly-gb is too large")
		}
		return monthlyGB * quotaGBBytes, nil
	}
	return monthlyBytes, nil
}

// newUserLinkCmd builds the `user link` command that prints a user's VLESS link and QR code.
func (a *App) newUserLinkCmd() *cobra.Command {
	var link struct {
		server        string
		username      string
		profile       string
		fingerprint   string
		spiderX       string
		noSpiderX     bool
		legacyReality bool
		qr            bool
		qrFile        string
	}
	cmd := &cobra.Command{
		Use:   "link",
		Short: "Generate vless:// link",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := requiredFlagValue("--server", link.server)
			if err != nil {
				return err
			}
			username, err := requiredFlagValue("--username", link.username)
			if err != nil {
				return err
			}
			link.server = server
			link.username = username
			srv, err := a.store.GetServerByName(a.ctx, link.server)
			if err != nil {
				return err
			}
			u, err := a.store.GetUser(a.ctx, srv.ID, link.username)
			if err != nil {
				return err
			}
			vless, err := buildUserProfileLink(*srv, *u, link.profile, userLinkOptions{
				fingerprint:   link.fingerprint,
				spiderX:       link.spiderX,
				noSpiderX:     link.noSpiderX,
				legacyReality: link.legacyReality,
			})
			if err != nil {
				return err
			}
			fmt.Println(vless)
			if link.qr {
				qrText, err := renderTerminalQRCode(vless)
				if err != nil {
					return fmt.Errorf("render QR: %w", err)
				}
				fmt.Print(qrText)
			}
			if link.qrFile != "" {
				if err := writeQRCodePNG(vless, link.qrFile); err != nil {
					return fmt.Errorf("write QR file: %w", err)
				}
				fmt.Fprintf(os.Stderr, "QR saved: %s\n", filepath.Clean(link.qrFile))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&link.server, "server", "", "Server name")
	cmd.Flags().StringVar(&link.username, "username", "", "Username")
	cmd.Flags().StringVar(&link.profile, "profile", "", "Transport profile (default: server primary profile)")
	cmd.Flags().StringVar(&link.fingerprint, "fingerprint", "", "Client TLS fingerprint for REALITY/TLS links (default: "+xraycfg.DefaultClientFingerprint+")")
	cmd.Flags().StringVar(&link.spiderX, "spider-x", "", "REALITY spiderX path override (default: stable per-user path)")
	cmd.Flags().BoolVar(&link.noSpiderX, "no-spider-x", false, "Do not include REALITY spiderX in generated links")
	cmd.Flags().BoolVar(&link.legacyReality, "legacy-reality", false, "Generate legacy REALITY client params (fp=chrome, no spx)")
	cmd.Flags().BoolVar(&link.qr, "qr", true, "Print a terminal QR code after the link; use --qr=false for link-only output")
	cmd.Flags().StringVar(&link.qrFile, "qr-file", "", "Save a PNG QR code to this path")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func (a *App) newUserQRCmd() *cobra.Command {
	var qr struct {
		server        string
		username      string
		profile       string
		fingerprint   string
		spiderX       string
		noSpiderX     bool
		legacyReality bool
		out           string
	}
	cmd := &cobra.Command{
		Use:   "qr",
		Short: "Save a user profile QR code",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := requiredFlagValue("--server", qr.server)
			if err != nil {
				return err
			}
			username, err := requiredFlagValue("--username", qr.username)
			if err != nil {
				return err
			}
			out, err := requiredFlagValue("--out", qr.out)
			if err != nil {
				return err
			}
			srv, err := a.store.GetServerByName(a.ctx, server)
			if err != nil {
				return err
			}
			u, err := a.store.GetUser(a.ctx, srv.ID, username)
			if err != nil {
				return err
			}
			vless, err := buildUserProfileLink(*srv, *u, qr.profile, userLinkOptions{
				fingerprint:   qr.fingerprint,
				spiderX:       qr.spiderX,
				noSpiderX:     qr.noSpiderX,
				legacyReality: qr.legacyReality,
			})
			if err != nil {
				return err
			}
			if err := writeQRCodePNG(vless, out); err != nil {
				return fmt.Errorf("write QR file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "QR saved: %s\n", filepath.Clean(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&qr.server, "server", "", "Server name")
	cmd.Flags().StringVar(&qr.username, "username", "", "Username")
	cmd.Flags().StringVar(&qr.profile, "profile", "", "Transport profile (default: server primary profile)")
	cmd.Flags().StringVar(&qr.fingerprint, "fingerprint", "", "Client TLS fingerprint for REALITY/TLS links (default: "+xraycfg.DefaultClientFingerprint+")")
	cmd.Flags().StringVar(&qr.spiderX, "spider-x", "", "REALITY spiderX path override (default: stable per-user path)")
	cmd.Flags().BoolVar(&qr.noSpiderX, "no-spider-x", false, "Do not include REALITY spiderX in generated links")
	cmd.Flags().BoolVar(&qr.legacyReality, "legacy-reality", false, "Generate legacy REALITY client params (fp=chrome, no spx)")
	cmd.Flags().StringVar(&qr.out, "out", "", "Output PNG path")
	cmd.Flags().StringVar(&qr.out, "qr-file", "", "Alias for --out")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func (a *App) newUserExportCmd() *cobra.Command {
	var export struct {
		server        string
		username      string
		out           string
		allProfiles   bool
		profile       string
		fingerprint   string
		fingerprints  string
		spiderX       string
		noSpiderX     bool
		legacyReality bool
		format        string
	}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export user links, QR codes, or Xray Checker configurations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := requiredFlagValue("--server", export.server)
			if err != nil {
				return err
			}
			username, err := requiredFlagValue("--username", export.username)
			if err != nil {
				return err
			}
			out, err := requiredFlagValue("--out", export.out)
			if err != nil {
				return err
			}
			srv, err := a.store.GetServerByName(a.ctx, server)
			if err != nil {
				return err
			}
			u, err := a.store.GetUser(a.ctx, srv.ID, username)
			if err != nil {
				return err
			}
			if export.format != "links" && export.format != "xray-checker" {
				return fmt.Errorf("--format must be links or xray-checker")
			}
			info, err := os.Stat(out)
			if err != nil {
				return fmt.Errorf("output directory %s: %w", out, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("output path is not a directory: %s", out)
			}
			if strings.TrimSpace(export.fingerprint) != "" && strings.TrimSpace(export.fingerprints) != "" {
				return fmt.Errorf("use only one of --fingerprint or --fingerprints")
			}
			if export.legacyReality && strings.TrimSpace(export.fingerprints) != "" {
				return fmt.Errorf("use only one of --legacy-reality or --fingerprints")
			}
			profiles := []string{export.profile}
			if export.allProfiles {
				profiles = deployableEnabledProfiles(*srv)
			}
			if !export.allProfiles && strings.TrimSpace(export.profile) == "" {
				profiles = []string{srv.NormalizedPrimaryProfile()}
			}
			fingerprints, multiFingerprintExport, err := exportFingerprints(export.fingerprint, export.fingerprints)
			if err != nil {
				return err
			}
			if export.legacyReality {
				fingerprints = []string{""}
				multiFingerprintExport = false
			}
			for _, profile := range profiles {
				normalizedProfile, err := normalizeRequestedProfileForExport(*srv, profile)
				if err != nil {
					return err
				}
				profileFingerprints := fingerprints
				includeFingerprintInName := multiFingerprintExport || strings.TrimSpace(export.fingerprint) != ""
				if !profileUsesClientFingerprint(normalizedProfile) {
					profileFingerprints = []string{""}
					includeFingerprintInName = false
				}
				for _, fp := range profileFingerprints {
					vless, err := buildUserProfileLink(*srv, *u, normalizedProfile, userLinkOptions{
						fingerprint:   fp,
						spiderX:       export.spiderX,
						noSpiderX:     export.noSpiderX,
						legacyReality: export.legacyReality,
					})
					if err != nil {
						return err
					}
					base := fmt.Sprintf("%s-%s-%s", srv.Name, u.Username, normalizedProfile)
					if includeFingerprintInName {
						base += "-fp-" + xraycfg.NormalizeClientFingerprint(fp)
					}
					if export.format == "xray-checker" {
						if err := validateClientCheckUser(*srv, *u); err != nil {
							return err
						}
						config, err := clientcfg.FromLink(vless)
						if err != nil {
							return err
						}
						data, err := config.JSON()
						if err != nil {
							return err
						}
						path := filepath.Join(out, base+".json")
						if err := writePrivateClientConfig(path, data); err != nil {
							return err
						}
						fmt.Printf("exported %s\n", filepath.Clean(path))
						continue
					}
					linkPath := filepath.Join(out, base+".txt")
					if err := os.WriteFile(linkPath, []byte(vless+"\n"), 0o600); err != nil {
						return err
					}
					qrPath := filepath.Join(out, base+".png")
					if err := writeQRCodePNG(vless, qrPath); err != nil {
						return fmt.Errorf("write QR file: %w", err)
					}
					fmt.Printf("exported %s\n", filepath.Clean(qrPath))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&export.server, "server", "", "Server name")
	cmd.Flags().StringVar(&export.username, "username", "", "Username")
	cmd.Flags().StringVar(&export.out, "out", "", "Output directory")
	cmd.Flags().StringVar(&export.format, "format", "links", "Export format: links|xray-checker")
	cmd.Flags().BoolVar(&export.allProfiles, "all-profiles", false, "Export every enabled server profile")
	cmd.Flags().StringVar(&export.profile, "profile", "", "Export one profile (default: server primary profile)")
	cmd.Flags().StringVar(&export.fingerprint, "fingerprint", "", "Client TLS fingerprint for REALITY/TLS links (default: "+xraycfg.DefaultClientFingerprint+")")
	cmd.Flags().StringVar(&export.fingerprints, "fingerprints", "", "Comma-separated fingerprint variants to export for REALITY/TLS profiles")
	cmd.Flags().StringVar(&export.spiderX, "spider-x", "", "REALITY spiderX path override (default: stable per-user path)")
	cmd.Flags().BoolVar(&export.noSpiderX, "no-spider-x", false, "Do not include REALITY spiderX in generated links")
	cmd.Flags().BoolVar(&export.legacyReality, "legacy-reality", false, "Generate legacy REALITY client params (fp=chrome, no spx)")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

type userLinkOptions struct {
	fingerprint   string
	spiderX       string
	noSpiderX     bool
	legacyReality bool
}

func buildUserProfileLink(srv model.Server, u model.User, profile string, opts ...userLinkOptions) (string, error) {
	options := userLinkOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}
	return clientprofile.Link(srv, u, profile, options.shared())
}

func (o userLinkOptions) shared() clientprofile.Options {
	return clientprofile.Options{Fingerprint: o.fingerprint, SpiderX: o.spiderX, NoSpiderX: o.noSpiderX, LegacyReality: o.legacyReality}
}

func exportFingerprints(single string, list string) ([]string, bool, error) {
	if strings.TrimSpace(list) == "" {
		fp := xraycfg.NormalizeClientFingerprint(single)
		if fp == "" {
			return nil, false, fmt.Errorf("unsupported --fingerprint %q; supported values: %s", strings.TrimSpace(single), xraycfg.SupportedClientFingerprintsText())
		}
		return []string{fp}, false, nil
	}
	parts := strings.Split(list, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, raw := range parts {
		fp := xraycfg.NormalizeClientFingerprint(raw)
		if fp == "" {
			return nil, false, fmt.Errorf("unsupported --fingerprints value %q; supported values: %s", strings.TrimSpace(raw), xraycfg.SupportedClientFingerprintsText())
		}
		if !seen[fp] {
			out = append(out, fp)
			seen[fp] = true
		}
	}
	if len(out) == 0 {
		return nil, false, fmt.Errorf("--fingerprints must include at least one fingerprint")
	}
	return out, true, nil
}

func normalizeRequestedProfileForExport(srv model.Server, profile string) (string, error) {
	if strings.TrimSpace(profile) == "" {
		return srv.NormalizedPrimaryProfile(), nil
	}
	normalized := model.NormalizeTransportProfile(profile)
	if normalized == "" {
		return "", unsupportedTransportProfileError(profile)
	}
	return normalized, nil
}

func deployableEnabledProfiles(srv model.Server) []string {
	profiles := srv.NormalizedEnabledProfiles()
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		_, ok := model.LookupTransportProfile(profile)
		if ok {
			out = append(out, profile)
		}
	}
	return out
}

func profileUsesClientFingerprint(profile string) bool {
	return clientprofile.UsesClientFingerprint(profile)
}
