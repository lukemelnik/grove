package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <branch>",
		Short: "Create a worktree for the given branch",
		Long: `Create a git worktree with deterministic port assignment and optional
tmux workspace. If a tmux config is present in .grove.yml, sets up
a tmux session/window with panes and environment variables.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "grove create %s: not yet implemented\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringArrayP("env", "e", nil, "environment variable override (KEY=VALUE, repeatable)")
	cmd.Flags().String("from", "", "base branch for new branches (default: origin/main)")
	cmd.Flags().Bool("no-tmux", false, "skip tmux, just create worktree and print info")
	cmd.Flags().Bool("all", false, "include optional panes")
	cmd.Flags().StringArray("with", nil, "include specific optional pane(s)")
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	cmd.Flags().Bool("attach", true, "auto-attach to the tmux session/window")

	return cmd
}
