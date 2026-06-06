package cli

import "github.com/spf13/cobra"

// userCmd builds the `user` command group for managing users.
func (a *App) userCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage users"}
	cmd.AddCommand(
		a.newUserAddCmd(),
		a.newUserRemoveCmd(),
		a.newUserEnableCmd(),
		a.newUserDisableCmd(),
		a.newUserExpirySetCmd(),
		a.newUserExpiryClearCmd(),
		a.newUserReconcileCmd(),
		a.newUserListCmd(),
		a.newUserShowCmd(),
		a.newUserTopCmd(),
		a.newUserDiagnoseCmd(),
		a.newUserDebugCmd(),
		a.newUserQuotaSetCmd(),
		a.newUserQuotaResetCmd(),
		a.newUserLinkCmd(),
	)
	return cmd
}
