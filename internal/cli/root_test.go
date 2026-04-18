package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"ovpn/internal/model"
	"ovpn/internal/store/local"
)

func TestNewRootCmdRegistersTopLevelCommands(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	var names []string
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}

	for _, want := range []string{"server", "doctor", "user", "stats", "config", "deploy", "restart", "version"} {
		if !slices.Contains(names, want) {
			t.Fatalf("expected top-level command %q, got %v", want, names)
		}
	}
}

func TestServerCommandTreeContainsExpectedSubcommands(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).serverCmd()
	var names []string
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	for _, want := range []string{"add", "init", "list", "set-xray-version", "status", "backup", "restore", "logs", "monitor", "cleanup"} {
		if !slices.Contains(names, want) {
			t.Fatalf("expected server subcommand %q, got %v", want, names)
		}
	}
}

func TestUserAddRequiresFlags(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).userCmd()
	cmd.SetArgs([]string{"add"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected missing flag error")
	}
	if !strings.Contains(err.Error(), "required flag") || !strings.Contains(err.Error(), "username") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserQuotaSetRequiresFlags(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).userCmd()
	cmd.SetArgs([]string{"quota-set"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected missing flag error")
	}
	if !strings.Contains(err.Error(), "required flag") || !strings.Contains(err.Error(), "username") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserExpirySetRequiresFlags(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).userCmd()
	cmd.SetArgs([]string{"expiry-set"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected missing flag error")
	}
	if !strings.Contains(err.Error(), "required flag") || !strings.Contains(err.Error(), "username") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserExpiryClearRequiresFlags(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).userCmd()
	cmd.SetArgs([]string{"expiry-clear"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected missing flag error")
	}
	if !strings.Contains(err.Error(), "required flag") || !strings.Contains(err.Error(), "username") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserCommandTreeContainsExpectedSubcommands(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).userCmd()
	var names []string
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	for _, want := range []string{"add", "rm", "enable", "disable", "expiry-set", "expiry-clear", "reconcile", "list", "show", "top", "quota-set", "quota-reset", "link"} {
		if !slices.Contains(names, want) {
			t.Fatalf("expected user subcommand %q, got %v", want, names)
		}
	}
}

func TestServerAddRequiresFlags(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"add", "--name", "main", "--host", "1.2.3.4"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected missing flag error")
	}
	if !strings.Contains(err.Error(), "required flag") || !strings.Contains(err.Error(), "domain") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServerSetXrayVersionRequiresArgs(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).serverCmd()
	cmd.SetArgs([]string{"set-xray-version", "main"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected missing arg error")
	}
	if !strings.Contains(err.Error(), "accepts 2 arg(s)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStatsRequiresServerFlag(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).statsCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected missing server flag error")
	}
	if !strings.Contains(err.Error(), "required flag") || !strings.Contains(err.Error(), "server") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFirstShortID(t *testing.T) {
	t.Parallel()

	if got := firstShortID(" a1 , b2 "); got != "a1" {
		t.Fatalf("unexpected first short id: %q", got)
	}
	if got := firstShortID(" "); got != "" {
		t.Fatalf("expected empty short id, got %q", got)
	}
}

func TestNormalizeXrayVersionTag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "26.3.27", want: "26.3.27"},
		{in: "v26.3.27", want: "26.3.27"},
		{in: "  v26.3.27  ", want: "26.3.27"},
		{in: "vnext", want: "vnext"},
		{in: "", want: ""},
	}
	for _, tc := range cases {
		if got := normalizeXrayVersionTag(tc.in); got != tc.want {
			t.Fatalf("normalizeXrayVersionTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateComposeService(t *testing.T) {
	t.Parallel()

	got, err := validateComposeService("xray")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != " xray" {
		t.Fatalf("unexpected service arg: %q", got)
	}
	got, err = validateComposeService("ovpn-telegram-bot")
	if err != nil {
		t.Fatalf("unexpected error for ovpn-telegram-bot: %v", err)
	}
	if got != " ovpn-telegram-bot" {
		t.Fatalf("unexpected telegram bot service arg: %q", got)
	}
	if _, err := validateComposeService("bad"); err == nil {
		t.Fatalf("expected validation error for unknown service")
	}
}

func TestDoctorRequiresServerArg(t *testing.T) {
	t.Parallel()

	cmd := (&App{}).doctorCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected missing server arg error")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg(s)") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvOrPrefersFileOverride(t *testing.T) {
	tmp := t.TempDir()
	secretFile := filepath.Join(tmp, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	t.Setenv("OVPN_TEST_SECRET", "from-env")
	t.Setenv("OVPN_TEST_SECRET_FILE", secretFile)

	if got := envOr("OVPN_TEST_SECRET", "fallback"); got != "from-file" {
		t.Fatalf("expected file override value, got %q", got)
	}
}

func TestEnvOrFallsBackWhenFileMissing(t *testing.T) {
	t.Setenv("OVPN_TEST_SECRET", "from-env")
	t.Setenv("OVPN_TEST_SECRET_FILE", filepath.Join(t.TempDir(), "missing.txt"))

	if got := envOr("OVPN_TEST_SECRET", "fallback"); got != "from-env" {
		t.Fatalf("expected env fallback value, got %q", got)
	}
}

func TestEnvOrUsesFallbackWhenUnset(t *testing.T) {
	t.Setenv("OVPN_TEST_SECRET", "")
	t.Setenv("OVPN_TEST_SECRET_FILE", "")

	if got := envOr("OVPN_TEST_SECRET", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback value, got %q", got)
	}
}

func TestParseFallbackRateLimitFromEnv(t *testing.T) {
	t.Setenv("OVPN_REALITY_LIMIT_FALLBACK_UPLOAD_AFTER_BYTES", "4096")
	t.Setenv("OVPN_REALITY_LIMIT_FALLBACK_UPLOAD_BYTES_PER_SEC", "2048")
	t.Setenv("OVPN_REALITY_LIMIT_FALLBACK_UPLOAD_BURST_BYTES_PER_SEC", "4096")

	got, err := parseFallbackRateLimitFromEnv("OVPN_REALITY_LIMIT_FALLBACK_UPLOAD")
	if err != nil {
		t.Fatalf("parseFallbackRateLimitFromEnv: %v", err)
	}
	if got == nil {
		t.Fatalf("expected fallback limit, got nil")
		return
	}
	if got.AfterBytes != 4096 || got.BytesPerSec != 2048 || got.BurstBytesPerSec != 4096 {
		t.Fatalf("unexpected fallback limit: %+v", got)
	}
}

func TestParseFallbackRateLimitFromEnvRejectsNegative(t *testing.T) {
	t.Setenv("OVPN_REALITY_LIMIT_FALLBACK_UPLOAD_BYTES_PER_SEC", "-1")
	if _, err := parseFallbackRateLimitFromEnv("OVPN_REALITY_LIMIT_FALLBACK_UPLOAD"); err == nil {
		t.Fatalf("expected validation error for negative value")
	}
}

func TestParseSecurityProfileFromEnv(t *testing.T) {
	t.Setenv("OVPN_SECURITY_PROFILE", "")
	got, err := parseSecurityProfileFromEnv()
	if err != nil {
		t.Fatalf("parseSecurityProfileFromEnv: %v", err)
	}
	if got != "minimal" {
		t.Fatalf("expected default minimal profile, got %q", got)
	}

	t.Setenv("OVPN_SECURITY_PROFILE", "off")
	got, err = parseSecurityProfileFromEnv()
	if err != nil {
		t.Fatalf("parseSecurityProfileFromEnv(off): %v", err)
	}
	if got != "off" {
		t.Fatalf("expected off profile, got %q", got)
	}

	t.Setenv("OVPN_SECURITY_PROFILE", "invalid")
	if _, err := parseSecurityProfileFromEnv(); err == nil {
		t.Fatalf("expected error for invalid security profile")
	}
}

func TestInferTelegramOwnerUserID(t *testing.T) {
	t.Parallel()

	if got := inferTelegramOwnerUserID("42", "100,200"); got != "42" {
		t.Fatalf("explicit owner must win, got %q", got)
	}
	if got := inferTelegramOwnerUserID("", "123456789"); got != "123456789" {
		t.Fatalf("expected fallback owner from notify ids, got %q", got)
	}
	if got := inferTelegramOwnerUserID("", ""); got != "" {
		t.Fatalf("expected empty owner fallback, got %q", got)
	}
}

func TestNormalizeTelegramDeployIDs(t *testing.T) {
	t.Parallel()

	notify, owner, err := normalizeTelegramDeployIDs(" 123456789 , 123 ", "")
	if err != nil {
		t.Fatalf("normalizeTelegramDeployIDs: %v", err)
	}
	if notify != "123456789,123" {
		t.Fatalf("unexpected notify ids: %q", notify)
	}
	if owner != "123456789" {
		t.Fatalf("unexpected owner fallback: %q", owner)
	}

	notify, owner, err = normalizeTelegramDeployIDs("123456789", "42")
	if err != nil {
		t.Fatalf("normalizeTelegramDeployIDs explicit owner: %v", err)
	}
	if notify != "123456789" || owner != "42" {
		t.Fatalf("unexpected normalized values: notify=%q owner=%q", notify, owner)
	}

	if _, _, err := normalizeTelegramDeployIDs("bad", ""); err == nil {
		t.Fatalf("expected invalid notify ids error")
	}
	if _, _, err := normalizeTelegramDeployIDs("", "1,2"); err == nil {
		t.Fatalf("expected invalid owner ids error")
	}
}

func TestDefaultUserEmail(t *testing.T) {
	t.Parallel()

	if got := defaultUserEmail("alice"); got != "alice@global" {
		t.Fatalf("unexpected default user email: %q", got)
	}
}

func TestParseThreatDNSServersFromEnv(t *testing.T) {
	t.Setenv("OVPN_THREAT_DNS_SERVERS", "")
	servers, err := parseThreatDNSServersFromEnv()
	if err != nil {
		t.Fatalf("parseThreatDNSServersFromEnv default: %v", err)
	}
	if len(servers) == 0 {
		t.Fatalf("expected default DNS servers")
	}

	t.Setenv("OVPN_THREAT_DNS_SERVERS", "9.9.9.9,1.1.1.2")
	servers, err = parseThreatDNSServersFromEnv()
	if err != nil {
		t.Fatalf("parseThreatDNSServersFromEnv custom: %v", err)
	}
	if len(servers) != 2 || servers[0] != "9.9.9.9" || servers[1] != "1.1.1.2" {
		t.Fatalf("unexpected parsed DNS servers: %+v", servers)
	}

	t.Setenv("OVPN_THREAT_DNS_SERVERS", "https://9.9.9.9")
	if _, err := parseThreatDNSServersFromEnv(); err == nil {
		t.Fatalf("expected validation error for URI scheme")
	}
}

func TestQuotaPercent(t *testing.T) {
	t.Parallel()

	if got := quotaPercent(50, 200); got != 25 {
		t.Fatalf("unexpected percent: %f", got)
	}
	if got := quotaPercent(1, 0); got != 0 {
		t.Fatalf("expected zero for invalid quota, got %f", got)
	}
}

func TestBuildUserTopRows(t *testing.T) {
	t.Parallel()

	totals := []model.UserTraffic{
		{Email: "bob@example.com", UplinkBytes: 100, DownlinkBytes: 200},
		{Email: "alice@example.com", UplinkBytes: 300, DownlinkBytes: 300},
	}
	users := []model.User{
		{Username: "alice", Email: "alice@example.com"},
		{Username: "bob", Email: "bob@example.com"},
	}
	quota := map[string]model.QuotaUserStatus{
		"alice@example.com": {Window30DUsageByte: 600, Window30DQuotaByte: 1000, BlockedByQuota: false},
		"bob@example.com":   {Window30DUsageByte: 300, Window30DQuotaByte: 300, BlockedByQuota: true},
	}
	rows := buildUserTopRows(totals, users, quota, 10)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Email != "alice@example.com" {
		t.Fatalf("expected alice first, got %s", rows[0].Email)
	}
	if rows[1].Email != "bob@example.com" || !rows[1].BlockedByQuota {
		t.Fatalf("unexpected bob row: %+v", rows[1])
	}
	if rows[0].QuotaPercent == nil || *rows[0].QuotaPercent != 60 {
		t.Fatalf("unexpected alice quota percent: %+v", rows[0].QuotaPercent)
	}
}

func TestCleanupRemoveBackupsEffective(t *testing.T) {
	t.Parallel()

	cases := []struct {
		keepBackups   bool
		removeBackups bool
		want          bool
	}{
		{keepBackups: true, removeBackups: false, want: false},
		{keepBackups: false, removeBackups: false, want: true},
		{keepBackups: true, removeBackups: true, want: true},
		{keepBackups: false, removeBackups: true, want: true},
	}
	for _, tc := range cases {
		if got := cleanupRemoveBackupsEffective(tc.keepBackups, tc.removeBackups); got != tc.want {
			t.Fatalf("cleanupRemoveBackupsEffective(keep=%v, remove=%v)=%v, want %v", tc.keepBackups, tc.removeBackups, got, tc.want)
		}
	}
}

func TestValidateCleanupConfirm(t *testing.T) {
	t.Parallel()

	if err := validateCleanupConfirm("CLEANUP", false); err != nil {
		t.Fatalf("expected valid confirmation, got %v", err)
	}
	if err := validateCleanupConfirm("", true); err != nil {
		t.Fatalf("dry-run should allow missing confirmation, got %v", err)
	}
	if err := validateCleanupConfirm("", false); err == nil {
		t.Fatalf("expected error when confirmation is missing outside dry-run")
	}
}

func TestEnsureRecentBackupForCleanup(t *testing.T) {
	ctx := context.Background()
	store, err := local.Open(ctx, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := &model.Server{
		Name:              "main",
		Host:              "1.2.3.4",
		Domain:            "example.com",
		SSHUser:           "debian",
		SSHPort:           22,
		XrayVersion:       "26.3.27",
		RealityPrivateKey: "priv",
		RealityPublicKey:  "pub",
		RealityShortIDs:   "abcd",
		RealityServerName: "www.microsoft.com",
		RealityTarget:     "www.microsoft.com:443",
		Enabled:           true,
	}
	if err := store.AddServer(ctx, server); err != nil {
		t.Fatalf("add server: %v", err)
	}

	app := &App{ctx: ctx, store: store}

	if _, _, err := app.ensureRecentBackupForCleanup(*server, 30*24*time.Hour); err == nil {
		t.Fatalf("expected backup check failure without backups")
	}

	oldBackup := &model.BackupRecord{
		ServerID:   server.ID,
		Type:       "server",
		Path:       "/tmp/old.tgz",
		SHA256:     "old",
		CreatedBy:  "tester",
		RemotePath: "/opt/ovpn-backups/old.tgz",
		CreatedAt:  time.Now().UTC().Add(-40 * 24 * time.Hour),
	}
	if err := store.AddBackupRecord(ctx, oldBackup); err != nil {
		t.Fatalf("add old backup record: %v", err)
	}
	if _, _, err := app.ensureRecentBackupForCleanup(*server, 30*24*time.Hour); err == nil {
		t.Fatalf("expected backup check failure for stale backup")
	}

	recent := &model.BackupRecord{
		ServerID:   server.ID,
		Type:       "server",
		Path:       "/tmp/new.tgz",
		SHA256:     "new",
		CreatedBy:  "tester",
		RemotePath: "/opt/ovpn-backups/new.tgz",
		CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
	}
	if err := store.AddBackupRecord(ctx, recent); err != nil {
		t.Fatalf("add recent backup record: %v", err)
	}

	rec, age, err := app.ensureRecentBackupForCleanup(*server, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("expected recent backup to pass, got %v", err)
	}
	if rec.Path != recent.Path {
		t.Fatalf("expected latest backup path %q, got %q", recent.Path, rec.Path)
	}
	if age <= 0 {
		t.Fatalf("expected positive backup age, got %s", age)
	}
}
