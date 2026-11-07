package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"ovpn/internal/app"
	"ovpn/internal/app/dto"
)

func (a *App) newUserPlanCmd() *cobra.Command {
	var request dto.UserPlanRequest
	var quota int64
	cmd := &cobra.Command{Use: "plan", Short: "Review a local user change without applying it (JSON)", Args: cobra.NoArgs,
		Annotations: map[string]string{"ovpn.local-state": "none"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("quota-bytes") {
				request.QuotaBytes = &quota
			}
			plan, err := (app.ReadService{DataDir: a.dataDir}).PlanUser(cmd.Context(), request)
			if err != nil {
				return fmt.Errorf("user plan: %w", err)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(plan)
		}}
	cmd.Flags().StringVar(&request.Action, "action", "", "Intent: create|enable|disable")
	cmd.Flags().StringVar(&request.Username, "username", "", "Username")
	cmd.Flags().StringVar(&request.Expiry, "expiry", "", "Create only: expiry date YYYY-MM-DD")
	cmd.Flags().Int64Var(&quota, "quota-bytes", 0, "Create only: explicit per-server rolling 30d quota (>0)")
	cmd.Flags().StringVar(&request.ExpectedDigest, "expect-digest", "", "Reject a changed local snapshot or intent")
	_ = cmd.MarkFlagRequired("action")
	_ = cmd.MarkFlagRequired("username")
	return cmd
}
