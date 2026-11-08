package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ovpn/internal/app"
	"ovpn/internal/app/dto"

	"github.com/spf13/cobra"
)

func (a *App) observeCmd() *cobra.Command {
	root := &cobra.Command{Use: "observe", Short: "Run opt-in traffic observation without changing server state"}
	command := func(use, short string, fn func(*cobra.Command, *app.DesktopService, []string) error) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Annotations: map[string]string{"ovpn.local-state": "none"}, RunE: func(cmd *cobra.Command, args []string) error {
			if a.dryRun {
				return fmt.Errorf("observe does not support --dry-run; status reads without starting checks")
			}
			service := app.NewDesktopService(cmd.Context(), a.dataDir)
			defer service.Close()
			return fn(cmd, service, args)
		}}
	}
	output := func(cmd *cobra.Command, v any) error { return json.NewEncoder(cmd.OutOrStdout()).Encode(v) }
	status := command("status", "Show policies, jobs, samples and incidents (JSON)", func(cmd *cobra.Command, s *app.DesktopService, _ []string) error {
		v, e := s.ObserverStatus(cmd.Context())
		if e != nil {
			return e
		}
		return output(cmd, v)
	})
	status.Args = cobra.NoArgs
	run := command("run", "Observe enabled policies in this foreground process", func(cmd *cobra.Command, s *app.DesktopService, _ []string) error {
		if err := s.ObserverStart(cmd.Context()); err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		<-ctx.Done()
		return nil
	})
	run.Args = cobra.NoArgs
	install := command("install <checker|knife>", "Install a checksum-pinned observation tool", func(cmd *cobra.Command, s *app.DesktopService, args []string) error {
		job, err := s.IntegrationInstall(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return waitObservationJob(cmd, s, job.ID)
	})
	install.Args = cobra.ExactArgs(1)
	var setup dto.ToolSetup
	tool := command("tool-set", "Register an existing pinned-version binary with an explicit SHA-256", func(cmd *cobra.Command, s *app.DesktopService, _ []string) error {
		v, e := s.IntegrationSetup(cmd.Context(), setup)
		if e != nil {
			return e
		}
		return output(cmd, v)
	})
	tool.Args = cobra.NoArgs
	tool.Flags().StringVar(&setup.Engine, "engine", "", "checker|knife")
	tool.Flags().StringVar(&setup.Path, "binary", "", "Absolute binary path")
	tool.Flags().StringVar(&setup.SHA256, "sha256", "", "Binary SHA-256")
	for _, flag := range []string{"engine", "binary", "sha256"} {
		_ = tool.MarkFlagRequired(flag)
	}
	policy := &cobra.Command{Use: "policy", Short: "Manage explicit observation scope"}
	var p dto.ObservationPolicy
	var server, username, profile string
	add := command("add", "Save a one-target native/checker policy", func(cmd *cobra.Command, s *app.DesktopService, _ []string) error {
		target, err := observationTarget(cmd.Context(), s, server, username, profile)
		if err != nil {
			return err
		}
		p.Targets = []dto.ObservationTarget{target}
		v, e := s.ObserverConfigure(cmd.Context(), p)
		if e != nil {
			return e
		}
		return output(cmd, v)
	})
	add.Args = cobra.NoArgs
	add.Flags().StringVar(&p.Name, "name", "", "Policy name")
	add.Flags().StringVar(&p.Engine, "engine", "native", "native|checker")
	add.Flags().IntVar(&p.IntervalSeconds, "interval", 300, "Interval in seconds (60..3600)")
	add.Flags().IntVar(&p.MaxChecksPerDay, "daily-limit", 300, "Reserved checks per target/source per UTC day (1..2000)")
	add.Flags().BoolVar(&p.Enabled, "enable", false, "Enable when the observer process runs")
	add.Flags().BoolVar(&p.TrafficConfirmed, "confirm-traffic", false, "Authorize traffic charged to the selected identity")
	add.Flags().StringVar(&server, "server", "", "Server name or node ID")
	add.Flags().StringVar(&username, "username", "", "Existing active monitoring username")
	add.Flags().StringVar(&profile, "profile", "", "Transport profile (default primary)")
	_ = add.MarkFlagRequired("server")
	_ = add.MarkFlagRequired("username")
	pause := command("pause <policy-id>", "Pause future checks for a policy", func(cmd *cobra.Command, s *app.DesktopService, args []string) error {
		snapshot, err := s.ObserverStatus(cmd.Context())
		if err != nil {
			return err
		}
		for _, p := range snapshot.Policies {
			if p.ID == args[0] {
				p.Enabled = false
				v, e := s.ObserverConfigure(cmd.Context(), p)
				if e != nil {
					return e
				}
				return output(cmd, v)
			}
		}
		return fmt.Errorf("POLICY_NOT_FOUND")
	})
	pause.Args = cobra.ExactArgs(1)
	remove := command("rm <policy-id>", "Remove a policy and retain its observation history", func(cmd *cobra.Command, s *app.DesktopService, args []string) error {
		if e := s.ObserverRemove(cmd.Context(), args[0]); e != nil {
			return e
		}
		return output(cmd, struct {
			Removed bool `json:"removed"`
		}{true})
	})
	remove.Args = cobra.ExactArgs(1)
	policy.AddCommand(add, pause, remove)
	var confirmed bool
	once := command("once <policy-id>", "Run one policy cycle and print its redacted results", func(cmd *cobra.Command, s *app.DesktopService, args []string) error {
		j, e := s.ObserverRun(cmd.Context(), dto.PolicyRunRequest{PolicyID: args[0], TrafficConfirmed: confirmed})
		if e != nil {
			return e
		}
		return waitObservationJob(cmd, s, j.ID)
	})
	once.Args = cobra.ExactArgs(1)
	once.Flags().BoolVar(&confirmed, "confirm-traffic", false, "Authorize this check cycle")
	var diagnostic dto.DiagnosticRequest
	var diagServer, diagUser, diagProfile string
	diagnose := command("diagnose", "Run a finite Knife diagnostic with speed tests and IP lookup disabled", func(cmd *cobra.Command, s *app.DesktopService, _ []string) error {
		target, e := observationTarget(cmd.Context(), s, diagServer, diagUser, diagProfile)
		if e != nil {
			return e
		}
		diagnostic.Targets = []dto.ObservationTarget{target}
		j, e := s.DiagnosticStart(cmd.Context(), diagnostic)
		if e != nil {
			return e
		}
		return waitObservationJob(cmd, s, j.ID)
	})
	diagnose.Args = cobra.NoArgs
	diagnose.Flags().StringVar(&diagnostic.Engine, "engine", "knife-xray", "knife-xray|knife-singbox")
	diagnose.Flags().StringVar(&diagServer, "server", "", "Server name or node ID")
	diagnose.Flags().StringVar(&diagUser, "username", "", "Existing active monitoring username")
	diagnose.Flags().StringVar(&diagProfile, "profile", "", "Transport profile (default primary)")
	diagnose.Flags().BoolVar(&diagnostic.TrafficConfirmed, "confirm-traffic", false, "Authorize this diagnostic's traffic")
	_ = diagnose.MarkFlagRequired("server")
	_ = diagnose.MarkFlagRequired("username")
	root.AddCommand(status, run, install, tool, policy, once, diagnose)
	return root
}

func observationTarget(ctx context.Context, s *app.DesktopService, server, username, profile string) (dto.ObservationTarget, error) {
	cursor := ""
	for {
		page, err := s.Servers(ctx, cursor, 100)
		if err != nil {
			return dto.ObservationTarget{}, err
		}
		for _, node := range page.Items {
			if node.Name == server || node.ID == server {
				if profile == "" {
					profile = node.PrimaryProfile
				}
				return dto.ObservationTarget{ServerID: node.ID, Username: strings.TrimSpace(username), Profile: profile}, nil
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return dto.ObservationTarget{}, fmt.Errorf("SERVER_NOT_FOUND")
}

func waitObservationJob(cmd *cobra.Command, s *app.DesktopService, id string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 8*time.Minute)
	defer cancel()
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = s.ObserverCancel(context.Background(), id)
			return fmt.Errorf("OBSERVATION_CANCELED")
		case <-ticker.C:
			snapshot, e := s.ObserverStatus(ctx)
			if e != nil {
				return e
			}
			for _, job := range snapshot.Jobs {
				if job.ID != id || job.State == "running" {
					continue
				}
				results := []dto.Observation{}
				for _, sample := range snapshot.History {
					if sample.JobID == id {
						results = append(results, sample)
					}
				}
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					Job     dto.ObservationJob `json:"job"`
					Results []dto.Observation  `json:"results"`
				}{job, results}); err != nil {
					return err
				}
				if job.State != "completed" {
					return fmt.Errorf("observation job failed (%s)", job.Code)
				}
				for _, sample := range results {
					if sample.Outcome != "passed" {
						return fmt.Errorf("OBSERVATION_NOT_PASSED")
					}
				}
				return nil
			}
		}
	}
}
