package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active worktrees with their branch, path, and ports",
		Long:  `List all active grove-managed worktrees with their branch names, paths, and port assignments.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "grove list: not yet implemented")
			return nil
		},
	}

	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")

	return cmd
}
