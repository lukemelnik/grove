package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const schemaText = `# .grove.yml — Full Configuration Reference
#
# Place this file in your project root. Grove discovers it automatically,
# even from inside a worktree.

# Optional override for where worktrees are created (relative to project root).
# Default when omitted: "../.grove-worktrees/<repo-name>"
# Set this only when you want a different location.
# Example override:
# worktree_dir: ../.grove-worktrees/shared

# Env files to symlink from the main repo (for files not tied to a service).
# Paths must be relative to the project root and cannot escape it.
# Grove writes .env.local files next to each symlink with managed vars.
# Service env files (see services below) are auto-included — no need to
# list them here too.
env_files:
  - .env

# Services with base ports. The default branch (main/master) uses the base
# ports directly. Other branches get a deterministic offset added,
# so branches never collide.
#
# Each service can declare:
#   port:     base port and the env var name that receives the assigned port (optional)
#   env_file: the .env file for this service (symlinked + .env.local written)
#             required when using service-level env below
#   env:      additional env vars scoped to this service's .env.local
#
# Services without a port block are env-only — they get env vars written
# but skip port assignment. Useful for services that share another
# service's port (e.g. a desktop wrapper) or don't listen on a port at all
# (e.g. background workers, build tools).
#
# Template variables:
#   {{service_name.port}} — resolves to the assigned port for a service
#   {{branch}}            — resolves to the worktree branch name
services:
  api:
    env_file: apps/api/.env
    port:
      base: 4000
      var: PORT
    env:
      CORS_ORIGIN: "http://localhost:{{web.port}}"
  web:
    env_file: apps/web/.env
    port:
      base: 3000
      var: WEB_PORT
    env:
      VITE_API_URL: "http://localhost:{{api.port}}"
      VITE_APP_URL: "http://localhost:{{web.port}}"
      VITE_WORKTREE_NAME: "{{branch}}"
  # Env-only service (no port block) — just gets env vars written.
  # desktop:
  #   env_file: apps/desktop/.env
  #   env:
  #     PORT: "{{web.port}}"

# Additional environment variables (global, written to all .env.local files).
# Most configs won't need this — prefer service-level env above.
# Values can reference {{service_name.port}} and {{branch}} templates.
env:
  LOG_LEVEL: debug

# Tmux workspace configuration (optional).
# If omitted, grove create still opens a basic tmux window/session.
# Use --no-tmux to skip tmux entirely.
# In session mode, Grove injects top-level env vars plus each service's port
# env var via tmux as a fallback. Service-scoped env values stay in that
# service's .env.local file.
tmux:
  # mode: "window" (default) or "session"
  #   window  — each worktree gets a window in your current tmux session
  #             (run grove create from inside tmux)
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
  #   split: horizontal => children go left-to-right
  #   split: vertical   => children go top-to-bottom
  #   Child order matters: first child is left/top, second is right/bottom.
  #   size applies along the split axis: width for horizontal, height for vertical.
  #   To subdivide only one region further, nest another split inside that child.
  #
  # Example: two side-by-side pi panes with a small full-width terminal on the bottom:
  #   - split: vertical
  #     panes:
  #       - split: horizontal
  #         panes:
  #           - pi
  #           - pi
  #       - cmd: ""
  #         name: terminal
  #         size: "20%"
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
