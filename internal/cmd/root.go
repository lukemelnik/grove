// Package cmd contains the Cobra command definitions for the grove CLI.
package cmd

import (
	"github.com/spf13/cobra"
)

// NewRootCmd creates the root grove command.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "grove",
		Short: "Deterministic worktree workspaces",
		Long: `Grove manages git worktrees with deterministic port assignment,
environment variable injection, and optional tmux workspace orchestration.

Configure per-project with a .grove.yml and run 'grove create <branch>'
to get an isolated worktree with the right ports, env vars, and
optionally a full tmux workspace.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(
		newInitCmd(),
		newCreateCmd(),
		newAttachCmd(),
		newDeleteCmd(),
		newListCmd(),
		newStatusCmd(),
	)

	return rootCmd
}
