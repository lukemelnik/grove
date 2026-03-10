package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <branch>",
		Short: "Delete a worktree and its associated tmux session/window",
		Long: `Remove a worktree and its tmux session/window. Checks for open PRs
via gh (if available) and warns before deleting branches with open PRs.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "grove delete %s: not yet implemented\n", args[0])
			return nil
		},
	}

	cmd.Flags().Bool("force", false, "skip PR check and delete anyway")
	cmd.Flags().Bool("keep-branch", false, "remove worktree but keep the git branch")

	return cmd
}
