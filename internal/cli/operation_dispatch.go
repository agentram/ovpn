package cli

import (
	"fmt"
	"time"

	"ovpn/internal/operations"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Both entry points execute the same registered management handlers under the
// operation coordinator. Structured inputs do not cross a shell boundary.
func attachOperationHandlers(root *cobra.Command, a *App) {
	definitions := map[string]operations.Action{}
	for _, action := range commandCatalog(root) {
		definitions[action.ID] = action
	}
	var wrap func(*cobra.Command)
	wrap = func(command *cobra.Command) {
		id := actionID(command)
		if original := command.RunE; original != nil && definitions[id].Mutation {
			command.RunE = func(command *cobra.Command, args []string) error {
				if a.internal || a.dryRun {
					return original(command, args)
				}
				request := operations.Request{Action: id, Values: map[string]string{}, Arguments: args}
				command.Flags().Visit(func(flag *pflag.Flag) {
					if root.PersistentFlags().Lookup(flag.Name) == nil {
						request.Values[flag.Name] = flag.Value.String()
					}
				})
				journalDir := a.dataDir
				if command.Annotations["ovpn.local-state"] == "none" {
					if dir := request.Values["bundle-dir"]; dir != "" {
						journalDir = dir
					} else {
						return original(command, args)
					}
				}
				service := operations.New(command.Context(), journalDir, &deskControl{dataDir: a.dataDir, template: a})
				defer service.Close()
				plan, err := service.Plan(command.Context(), request)
				if err != nil {
					return err
				}
				job, err := service.Start(command.Context(), operations.Start{PlanID: plan.ID, Digest: plan.Digest, Key: operationRequestKey(), Confirmed: true})
				if err != nil {
					return err
				}
				ticker := time.NewTicker(50 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-command.Context().Done():
						_ = service.Cancel(job.ID)
						return command.Context().Err()
					case <-ticker.C:
						rows, err := service.List()
						if err != nil {
							return err
						}
						for _, row := range rows {
							if row.ID != job.ID {
								continue
							}
							switch row.State {
							case "queued", "running":
								continue
							}
							if result, err := service.Result(job.ID); err == nil {
								if result.Text != "" {
									fmt.Fprintln(a.output(), result.Text)
								}
								if result.Content != "" {
									fmt.Fprintln(a.output(), result.Content)
								}
							}
							if row.State != "succeeded" {
								if original := service.Failure(job.ID); original != nil {
									return original
								}
								return fmt.Errorf("operation %s: %s (%s)", row.ID, row.State, row.Code)
							}
							return nil
						}
					}
				}
			}
		}
		for _, child := range command.Commands() {
			wrap(child)
		}
	}
	wrap(root)
}
func operationRequestKey() string { return strings.ReplaceAll(uuid.NewString(), "-", "") }
