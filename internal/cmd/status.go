package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show info for the current worktree",
		Long:  `Show branch, worktree path, port assignments, and environment variables for the current worktree (detected from cwd).`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "grove status: not yet implemented")
			return nil
		},
	}

	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")

	return cmd
}
