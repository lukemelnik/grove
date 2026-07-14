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
		return outputError(cmd, newCodedError("worktree_not_found", err))
	}

	// Validate managed env and persisted state before making any tmux query.
	// Existing canonical targets still take the read-only fast path below.
	if _, _, err := resolveRuntimeEnv(ctx, branch); err != nil {
		return outputError(cmd, newCodedError("open_env_invalid", err))
	}

	tmuxCfg := effectiveTmuxConfig(ctx.Config)
	tmuxMgr := tmux.NewManager(tmuxRunnerFactory())
	mode := tmuxCfg.Mode
	if mode == "" {
		mode = "window"
	}

	canonical, canonicalExists, err := tmuxMgr.FindCanonical(ctx.ProjectRoot, branch, found.Path, "")
	if err != nil {
		return outputError(cmd, newCodedError("tmux_discovery_failed", err))
	}
	if !newWindow && canonicalExists {
		lock, lockErr := acquireWorkflowLock(cmd.Context(), ctx)
		if lockErr != nil {
			return outputError(cmd, lockErr)
		}
		current, currentErr := findWorktreeByBranch(ctx, branch)
		releaseErr := lock.Release()
		if currentErr != nil {
			return outputError(cmd, newCodedError("worktree_not_found", combineCleanupErrors(currentErr, releaseErr)))
		}
		samePath, samePathErr := sameWorktreeEnvRoot(current.Path, found.Path)
		if samePathErr != nil || !samePath {
			return outputError(cmd, newCodedError("worktree_changed", combineCleanupErrors(fmt.Errorf("worktree path changed from %s to %s; retry open", found.Path, current.Path), samePathErr, releaseErr)))
		}
		if releaseErr != nil {
			return outputError(cmd, newCodedError("project_lock_release_failed", releaseErr))
		}
		if err := tmuxMgr.AttachTarget(canonical); err != nil {
			return outputError(cmd, newCodedError("open_tmux_failed", err))
		}
		return nil
	}

	_, managed, releaseLock, err := resolvePersistentRuntimeEnv(cmd.Context(), ctx, branch)
	if err != nil {
		return outputError(cmd, err)
	}
	current, currentErr := findWorktreeByBranch(ctx, branch)
	if currentErr != nil {
		cleanupErr := reconcilePortRegistry(ctx)
		releaseErr := releaseLock()
		return outputError(cmd, newCodedError("worktree_not_found", combineCleanupErrors(currentErr, cleanupErr, releaseErr)))
	}
	found = current
	if err := syncWorktreeEnv(ctx.Config, ctx.ProjectRoot, found.Path, managed); err != nil {
		releaseErr := releaseLock()
		return outputError(cmd, combineCleanupErrors(err, releaseErr))
	}
	if err := releaseLock(); err != nil {
		return outputError(cmd, newCodedError("project_lock_release_failed", err))
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
		return outputError(cmd, newCodedError("open_tmux_failed", fmt.Errorf("setting up tmux workspace: %w", err)))
	}

	return nil
}
