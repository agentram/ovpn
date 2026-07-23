package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

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
		server      string
		username    string
		profile     string
		fingerprint string
		spiderX     string
		qr          bool
		qrFile      string
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
				fingerprint: link.fingerprint,
				spiderX:     link.spiderX,
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
	cmd.Flags().BoolVar(&link.qr, "qr", true, "Print a terminal QR code after the link; use --qr=false for link-only output")
	cmd.Flags().StringVar(&link.qrFile, "qr-file", "", "Save a PNG QR code to this path")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}

func (a *App) newUserQRCmd() *cobra.Command {
	var qr struct {
		server      string
		username    string
		profile     string
		fingerprint string
		spiderX     string
		out         string
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
				fingerprint: qr.fingerprint,
				spiderX:     qr.spiderX,
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
	cmd.Flags().StringVar(&qr.out, "out", "", "Output PNG path")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func (a *App) newUserExportCmd() *cobra.Command {
	var export struct {
		server       string
		username     string
		out          string
		allProfiles  bool
		profile      string
		fingerprint  string
		fingerprints string
		spiderX      string
	}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export user links and QR codes",
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
			profiles := []string{export.profile}
			if export.allProfiles {
				profiles = srv.NormalizedEnabledProfiles()
			}
			if !export.allProfiles && strings.TrimSpace(export.profile) == "" {
				profiles = []string{srv.NormalizedPrimaryProfile()}
			}
			fingerprints, multiFingerprintExport, err := exportFingerprints(export.fingerprint, export.fingerprints)
			if err != nil {
				return err
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
						fingerprint: fp,
						spiderX:     export.spiderX,
					})
					if err != nil {
						return err
					}
					base := fmt.Sprintf("%s-%s-%s", srv.Name, u.Username, normalizedProfile)
					if includeFingerprintInName {
						base += "-fp-" + xraycfg.NormalizeClientFingerprint(fp)
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
	cmd.Flags().BoolVar(&export.allProfiles, "all-profiles", false, "Export every enabled server profile")
	cmd.Flags().StringVar(&export.profile, "profile", "", "Export one profile (default: server primary profile)")
	cmd.Flags().StringVar(&export.fingerprint, "fingerprint", "", "Client TLS fingerprint for REALITY/TLS links (default: "+xraycfg.DefaultClientFingerprint+")")
	cmd.Flags().StringVar(&export.fingerprints, "fingerprints", "", "Comma-separated fingerprint variants to export for REALITY/TLS profiles")
	cmd.Flags().StringVar(&export.spiderX, "spider-x", "", "REALITY spiderX path override (default: stable per-user path)")
	_ = cmd.MarkFlagRequired("server")
	_ = cmd.MarkFlagRequired("username")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

type userLinkOptions struct {
	fingerprint string
	spiderX     string
}

func buildUserProfileLink(srv model.Server, u model.User, profile string, opts ...userLinkOptions) (string, error) {
	options := userLinkOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}
	rawProfile := strings.TrimSpace(profile)
	requestedProfile := rawProfile != ""
	if requestedProfile {
		profile = model.NormalizeTransportProfile(rawProfile)
		if profile == "" {
			return "", unsupportedTransportProfileError(rawProfile)
		}
	} else {
		profile = srv.NormalizedPrimaryProfile()
	}
	meta, ok := model.LookupTransportProfile(profile)
	if !ok {
		return "", unsupportedTransportProfileError(profile)
	}
	if meta.Status == "planned" {
		return "", plannedTransportProfileError(profile, srv.Name)
	}
	if !srv.IsTransportProfileEnabled(profile) {
		return "", fmt.Errorf("profile %s is not enabled on server %s; enable and deploy it first with `ovpn server profile enable %s %s` and `ovpn deploy %s`, or choose an enabled profile from `ovpn server profile list %s`", profile, srv.Name, srv.Name, profile, srv.Name, srv.Name)
	}
	fingerprint := ""
	if profileUsesClientFingerprint(profile) {
		fingerprint = xraycfg.NormalizeClientFingerprint(options.fingerprint)
		if fingerprint == "" {
			return "", fmt.Errorf("unsupported --fingerprint %q; supported values: %s", strings.TrimSpace(options.fingerprint), xraycfg.SupportedClientFingerprintsText())
		}
	} else if strings.TrimSpace(options.fingerprint) != "" {
		return "", fmt.Errorf("--fingerprint is only supported for REALITY/TLS profiles; profile %s does not use it", profile)
	}
	address := srv.Domain
	if address == "" {
		address = srv.Host
	}
	var shortID string
	var spiderX string
	if meta.Kind == "reality" {
		shortID = firstShortID(srv.RealityShortIDs)
		if shortID == "" {
			return "", fmt.Errorf("server %s has no REALITY short-id configured for profile %s; check `ovpn server profile list %s` and try a non-REALITY profile such as %s if it is enabled", srv.Name, profile, srv.Name, model.TransportProfilePlainXHTTP)
		}
		var err error
		spiderX, err = normalizeSpiderX(options.spiderX)
		if err != nil {
			return "", err
		}
		if spiderX == "" {
			spiderX = defaultSpiderX(srv, u, profile)
		}
	} else if strings.TrimSpace(options.spiderX) != "" {
		return "", fmt.Errorf("--spider-x is only supported for REALITY profiles; profile %s does not use it", profile)
	}
	serverName := srv.RealityServerName
	if meta.Kind == "tls-selfsni-web" {
		serverName = address
	}
	label := "ovpn-" + u.Username
	if requestedProfile || profile != model.TransportProfileRealityTCPVision {
		label += "-" + profile
	}
	return xraycfg.BuildVLESSLink(xraycfg.LinkInput{
		Address:     address,
		Port:        meta.Port,
		UUID:        u.UUID,
		ServerName:  serverName,
		Password:    srv.RealityPublicKey,
		ShortID:     shortID,
		Profile:     profile,
		Label:       label,
		Fingerprint: fingerprint,
		SpiderX:     spiderX,
	}), nil
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

func profileUsesClientFingerprint(profile string) bool {
	meta, ok := model.LookupTransportProfile(model.NormalizeTransportProfile(profile))
	if !ok {
		return false
	}
	return meta.Kind == "reality" || meta.Kind == "tls-selfsni-web"
}

func normalizeSpiderX(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	if !strings.HasPrefix(v, "/") {
		return "", fmt.Errorf("--spider-x must start with /")
	}
	if strings.ContainsAny(v, " \t\r\n") {
		return "", fmt.Errorf("--spider-x must not contain spaces")
	}
	for _, r := range v {
		if r > 127 {
			return "", fmt.Errorf("--spider-x must be URL-safe ASCII")
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '/', '.', '_', '-', '~':
			continue
		default:
			return "", fmt.Errorf("--spider-x must be URL-safe ASCII path characters")
		}
	}
	return v, nil
}

func defaultSpiderX(srv model.Server, u model.User, profile string) string {
	sum := sha256.Sum256([]byte(srv.Name + "\x00" + u.Username + "\x00" + model.NormalizeTransportProfile(profile)))
	return "/assets/" + hex.EncodeToString(sum[:])[:12] + ".js"
}
