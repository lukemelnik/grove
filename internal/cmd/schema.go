package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const schemaText = `# .grove.yml — Full Configuration Reference
#
# Place this file in your project root. Grove discovers it automatically,
# even from inside a worktree.

# Where worktrees are created (relative to project root).
# Default: "../.grove-worktrees"
worktree_dir: ../.grove-worktrees

# Env files to load. Paths must be relative to the project root and
# cannot escape it (no absolute paths or "../" prefixes).
# In worktrees, these files are symlinked from the main repo.
# Grove writes .env.local files next to each symlink with
# branch-specific port assignments and template-resolved vars.
env_files:
  - .env
  - apps/api/.env

# Services with base ports. The default branch (main/master) uses these
# ports directly. Other branches get a deterministic offset added,
# so branches never collide.
#
# Required fields:
#   port: base port number (1-65535)
#   env:  environment variable name to set (e.g. PORT)
services:
  api:
    port: 4000
    env: PORT
  web:
    port: 3000
    env: WEB_PORT

# Additional environment variables. Values can reference service ports
# with {{service_name.port}} templates.
env:
  VITE_API_URL: "http://localhost:{{api.port}}"
  CORS_ORIGIN: "http://localhost:{{web.port}}"
  LOG_LEVEL: debug

# Tmux workspace configuration (optional).
# If omitted, grove create still opens a basic tmux window/session.
# Use --no-tmux to skip tmux entirely.
tmux:
  # mode: "window" (default) or "session"
  #   window  — each worktree gets a window in your current tmux session
  #   session — each worktree gets its own tmux session
  mode: window

  # layout: a tmux preset or raw layout string
  #   Presets: even-horizontal, even-vertical, main-horizontal,
  #            main-vertical, tiled
  #   Raw: paste output of 'tmux list-windows' (e.g. "a]180x50,0,0{...}")
  layout: main-vertical

  # main_size: size of the main pane (only with main-horizontal/main-vertical)
  main_size: "70%"

  # panes: commands to run in each pane.
  #
  # NOTE: cmd and setup values are executed as shell commands in tmux panes.
  # Review these before running grove create in an unfamiliar repo, just as
  # you would review a Makefile, package.json scripts, or docker-compose.yml.
  #
  # Simple form — just a command string:
  #   - nvim
  #
  # Map form — with options:
  #   - cmd: pnpm dev
  #     name: dev          # identifier for --with flag
  #     optional: true     # skipped unless --all or --with dev
  #     autorun: false     # type command but don't press Enter (default: true)
  #     setup: pnpm install  # runs before cmd (always executes, even if autorun: false)
  #
  # Split form — nested pane layout (Tier 3):
  #   - split: vertical    # or "horizontal"
  #     size: "30%"
  #     panes:
  #       - cmd: logs
  #       - cmd: tests
  panes:
    - nvim
    - cmd: claude
    - cmd: pnpm dev
      name: dev
      optional: true
    - cmd: lazygit
      name: git
      optional: true
`

func newSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the full annotated .grove.yml configuration reference",
		Long: `Print a complete, annotated example of .grove.yml showing every
available field with its type, default value, and description.
Useful for agents and humans alike to understand the full config format.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprint(cmd.OutOrStdout(), schemaText)
		},
	}
}
