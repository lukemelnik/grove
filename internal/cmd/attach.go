package cmd

import "github.com/spf13/cobra"

func newAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <branch>",
		Short: "Alias for grove open <branch>",
		Long: `Jump back to an existing worktree's tmux session/window.

This command is kept for backwards compatibility. Prefer 'grove open <branch>'.`,
		Args: cobra.ExactArgs(1),
		RunE: runAttach,
	}
}

func runAttach(cmd *cobra.Command, args []string) error {
	return openBranch(cmd, args[0], false)
}
