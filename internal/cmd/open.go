package cmd

import (
	"fmt"

	"github.com/lukemelnik/grove/internal/tmux"

	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open <branch>",
		Short: "Open or restore a worktree's full tmux workspace",
		Long: `Open or restore the full Grove tmux workspace for an existing worktree.

If Grove can find a labeled canonical tmux session/window for the branch, it
switches or attaches to that target even if it has been renamed. If the target
was closed, Grove recreates the full tmux layout from .grove.yml.

Use --new-window to create an additional full tmux window for the worktree.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			newWindow, _ := cmd.Flags().GetBool("new-window")
			return openBranch(cmd, args[0], newWindow)
		},
	}

	cmd.Flags().Bool("new-window", false, "create an additional full tmux window even if the canonical workspace exists")
	return cmd
}

func openBranch(cmd *cobra.Command, branch string, newWindow bool) error {
	ctx, err := loadProjectContext()
	if err != nil {
		return outputError(cmd, err)
	}

	found, err := findWorktreeByBranch(ctx, branch)
	if err != nil {
		return outputError(cmd, err)
	}

	_, managed, err := resolveRuntimeEnv(ctx, branch)
	if err != nil {
		return outputError(cmd, err)
	}

	tmuxCfg := effectiveTmuxConfig(ctx.Config)
	tmuxMgr := tmux.NewManager(tmuxRunnerFactory())
	mode := tmuxCfg.Mode
	if mode == "" {
		mode = "window"
	}

	canonical, canonicalExists := tmuxMgr.FindCanonical(ctx.ProjectRoot, branch, found.Path, "")
	if !newWindow && canonicalExists {
		if err := tmuxMgr.AttachTarget(canonical); err != nil {
			return outputError(cmd, err)
		}
		return nil
	}

	if !newWindow {
		name := tmuxMgr.ResolveName(branch, mode)
		legacyExists := (mode == "session" && tmuxMgr.HasSession(name)) || (mode == "window" && tmuxMgr.HasWindow(name))
		if legacyExists {
			if err := tmuxMgr.Attach(name, mode); err != nil {
				return outputError(cmd, err)
			}
			return nil
		}
	}

	if err := syncWorktreeEnv(cmd, ctx.Config, ctx.ProjectRoot, found.Path, managed); err != nil {
		return outputError(cmd, err)
	}

	role := tmux.RoleCanonical
	if newWindow && canonicalExists {
		role = tmux.RoleExtra
	}

	tmuxOpts := tmux.Options{
		ProjectRoot:    ctx.ProjectRoot,
		Branch:         branch,
		WorktreePath:   found.Path,
		Env:            managed.SessionEnv(),
		TmuxConfig:     tmuxCfg,
		Attach:         true,
		Role:           role,
		ForceNewWindow: newWindow,
	}
	if err := tmuxMgr.Create(tmuxOpts); err != nil {
		return outputError(cmd, fmt.Errorf("setting up tmux workspace: %w", err))
	}

	return nil
}
