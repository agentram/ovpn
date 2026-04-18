package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"ovpn/internal/logx"
	"ovpn/internal/store/local"
	"ovpn/internal/util"
)

type App struct {
	ctx      context.Context
	store    *local.Store
	dataDir  string
	dryRun   bool
	repoRoot string
	logger   *slog.Logger
	logLevel string
	debug    bool
	verbose  bool
}

var errRuntimeQuotaBlocked = errors.New("runtime add skipped: user is blocked by quota")

// NewRootCmd initializes root cmd with the required dependencies.
func NewRootCmd() *cobra.Command {
	app := &App{ctx: context.Background()}
	cmd := &cobra.Command{
		Use:   "ovpn",
		Short: "ovpn manages self-hosted Xray VLESS+REALITY servers over SSH",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if app.debug || app.verbose {
				app.logLevel = "debug"
			}
			level, err := logx.ParseLevel(app.logLevel)
			if err != nil {
				return err
			}
			app.logger = logx.NewTextLogger(level).With("component", "ovpn-cli")
			slog.SetDefault(app.logger)
			app.ctx = cmd.Context()
			app.logger.Debug("starting command", "command", cmd.CommandPath(), "dry_run", app.dryRun)
			if app.dataDir == "" {
				app.dataDir = util.DefaultDataDir()
			}
			if err := util.EnsureDir(app.dataDir); err != nil {
				return fmt.Errorf("ensure data dir %s: %w", app.dataDir, err)
			}
			st, err := local.Open(app.ctx, app.dataDir)
			if err != nil {
				return fmt.Errorf("open local store: %w", err)
			}
			app.store = st
			wd, _ := os.Getwd()
			app.repoRoot = wd
			app.logger.Debug("command context initialized", "data_dir", app.dataDir, "repo_root", app.repoRoot)
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if app.store != nil {
				_ = app.store.Close()
			}
			if app.logger != nil {
				app.logger.Debug("command completed", "command", cmd.CommandPath())
			}
		},
	}
	cmd.PersistentFlags().StringVar(&app.dataDir, "data-dir", util.DefaultDataDir(), "ovpn local data directory")
	cmd.PersistentFlags().BoolVar(&app.dryRun, "dry-run", false, "preview actions without mutating remote resources")
	cmd.PersistentFlags().StringVar(&app.logLevel, "log-level", "info", "Log level: debug|info|warn|error")
	cmd.PersistentFlags().BoolVar(&app.debug, "debug", false, "Enable debug logs (shorthand for --log-level=debug)")
	cmd.PersistentFlags().BoolVar(&app.verbose, "verbose", false, "Enable debug logs (alias for --debug)")

	cmd.AddCommand(app.serverCmd())
	cmd.AddCommand(app.doctorCmd())
	cmd.AddCommand(app.userCmd())
	cmd.AddCommand(app.statsCmd())
	cmd.AddCommand(app.configCmd())
	cmd.AddCommand(app.deployCmd())
	cmd.AddCommand(app.restartCmd())
	cmd.AddCommand(app.versionCmd())
	return cmd
}
