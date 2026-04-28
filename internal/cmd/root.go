// Package cmd contains the Cobra command definitions for the grove CLI.
package cmd

import (
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

// NewRootCmd creates the root grove command.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "grove",
		Short:   "Deterministic worktree workspaces",
		Version: Version,
		Long: `Grove manages git worktrees with deterministic port assignment,
environment variable injection, and optional tmux workspace orchestration.

Configure per-project with a .grove.yml and run 'grove create <branch>'
to get an isolated worktree with the right ports, env vars, and
optionally a full tmux workspace.

Key commands:
  grove init           Create .grove.yml interactively or via flags
  grove init --service api:4000:PORT --pane nvim   (non-interactive)
  grove schema         Print the full .grove.yml reference, including tmux split examples
  grove create <branch>  Create worktree + workspace
  grove create <branch> --no-open  Provision only; do not open tmux
  grove open <branch>    Open or restore the full tmux workspace
  grove enter <branch>   Enter a worktree in the current pane
  grove list           List active worktrees and ports
  grove clean          Remove stale worktrees

Config defaults:
  worktree_dir is optional; if omitted Grove uses ../.grove-worktrees/<repo-name>
  Set worktree_dir only when you want a different location

Env files in .grove.yml:
  env_files is for shared root-level env symlinks like .env.apple
  services.<name>.env_file is for service-scoped env files like apps/api/.env

Tmux layout quick rules for .grove.yml:
  split: horizontal => children go left-to-right
  split: vertical   => children go top-to-bottom
  Child order: first child is left/top, second is right/bottom
  Full-width pane on the bottom/top => outer split should be vertical
  Full-height pane on the left/right => outer split should be horizontal

Need help translating a pane layout into .grove.yml?
  grove create --help  Tmux split direction rules + nested layout example
  grove init --help    Notes on flat --pane flags vs nested YAML layouts
  grove schema         Full annotated config reference with tmux examples

Agent workflow:
  Use grove create <branch> --no-open --json to provision without stealing tmux focus
  Use grove list --json to discover worktrees and use the returned worktree path as cwd
  Use grove open <branch> only when asked to open or restore the full tmux UI
  Avoid grove enter and interactive grove list unless explicitly asked
  Do not pass --force to delete/clean without explicit user approval`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(
		newInitCmd(),
		newCreateCmd(),
		newOpenCmd(),
		newEnterCmd(),
		newAttachCmd(),
		newDeleteCmd(),
		newCleanCmd(),
		newListCmd(),
		newStatusCmd(),
		newSchemaCmd(),
		newCompletionCmd(),
	)

	return rootCmd
}
