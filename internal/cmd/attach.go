package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <branch>",
		Short: "Attach to an existing worktree's tmux session/window",
		Long: `Jump back to an existing worktree. If a tmux session/window is running,
attach to it. If the worktree exists but no tmux session/window is active,
create one and attach.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "grove attach %s: not yet implemented\n", args[0])
			return nil
		},
	}
}
