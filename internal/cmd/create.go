package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/lukemelnik/grove/internal/hooks"
	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

// createOutput is the structured JSON output for grove create --json.
type createOutput struct {
	Worktree string         `json:"worktree"`
	Branch   string         `json:"branch"`
	Ports    map[string]int `json:"ports"`
}

func newCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <branch>",
		Short: "Create a worktree for the given branch",
		Long: `Create a git worktree with deterministic port assignment and optional
tmux workspace. If a tmux config is present in .grove.yml, sets up
a tmux session/window with panes and environment variables.

Use --no-open when you want to provision the worktree, ports, env files,
and hooks without opening tmux. Use 'grove open <branch>' later to open or
restore the full tmux workspace, or 'grove enter <branch>' to enter the
worktree in the current pane.

Branch resolution:
  1. Branch exists locally — use it
  2. Branch exists on remote — fetch and create local tracking branch
  3. Branch is new — create from --from ref (default: origin/main)

Optional panes:
  Panes marked with "optional: true" in .grove.yml are skipped by default.
  Include them with --all (all optional panes) or --with <name> (by name).

  Example .grove.yml:
    tmux:
      panes:
        - nvim
        - cmd: pnpm dev
          name: dev
          optional: true

Tmux explicit split rules in .grove.yml:
  split: horizontal => children go left-to-right
  split: vertical   => children go top-to-bottom
  Child order matters: first child is left/top, second is right/bottom.
  size applies along the split axis (width for horizontal, height for vertical).
  To subdivide only one region further, nest another split inside that child.

  Example: two side-by-side pi panes with a small full-width terminal on the bottom:
    tmux:
      panes:
        - split: vertical
          panes:
            - split: horizontal
              panes:
                - pi
                - pi
            - cmd: ""
              name: terminal
              size: "20%"

Run 'grove schema' for the full .grove.yml configuration reference.`,
		Args: cobra.ExactArgs(1),
		RunE: runCreate,
	}

	cmd.Flags().String("from", "", "base branch for new branches (default: origin/main)")
	cmd.Flags().Bool("no-open", false, "provision only; create/reuse worktree, env, and hooks without opening tmux")
	cmd.Flags().Bool("no-tmux", false, "deprecated alias for --no-open")
	cmd.Flags().Bool("all", false, "include optional panes")
	cmd.Flags().StringArray("with", nil, "include specific optional pane(s)")
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	cmd.Flags().Bool("attach", true, "auto-attach to the tmux session/window")

	return cmd
}

// tmuxRunnerFactory creates a tmux runner. It is a var so tests can override it.
var tmuxRunnerFactory = func() tmux.Runner {
	return tmux.NewRunner()
}

func runCreate(cmd *cobra.Command, args []string) error {
	branch := args[0]

	fromRef, _ := cmd.Flags().GetString("from")
	noOpen, _ := cmd.Flags().GetBool("no-open")
	noTmux, _ := cmd.Flags().GetBool("no-tmux")
	provisionOnly := noOpen || noTmux
	explicitJSON, _ := cmd.Flags().GetBool("json")
	jsonOutput := shouldOutputJSON(cmd)
	includeAll, _ := cmd.Flags().GetBool("all")
	includeWith, _ := cmd.Flags().GetStringArray("with")
	attach, _ := cmd.Flags().GetBool("attach")
	if jsonOutput && !explicitJSON && !cmd.Flags().Changed("attach") {
		// In auto-JSON mode we are typically being driven by another program, so
		// set up tmux but avoid switching the caller into an interactive session.
		attach = false
	}

	ctx, err := loadProjectContext()
	if err != nil {
		return outputError(cmd, err)
	}

	portAssignment, managed, releaseLock, err := resolvePersistentRuntimeEnv(cmd.Context(), ctx, branch)
	if err != nil {
		return outputError(cmd, err)
	}

	result, err := ctx.Worktrees.Create(branch, fromRef)
	if err != nil {
		cleanupErr := reconcilePortRegistry(ctx)
		releaseErr := releaseLock()
		return outputError(cmd, newCodedError("create_worktree_failed", combineCleanupErrors(fmt.Errorf("creating worktree: %w", err), cleanupErr, releaseErr)))
	}

	if err := syncWorktreeEnv(ctx.Config, ctx.ProjectRoot, result.Path, managed); err != nil {
		cleanupErr := rollbackCreatedResourcesLocked(ctx, result)
		releaseErr := releaseLock()
		return outputError(cmd, newCodedError("create_env_failed", combineCleanupErrors(err, cleanupErr, releaseErr)))
	}
	if err := releaseLock(); err != nil {
		return outputError(cmd, newCodedError("project_lock_release_failed", fmt.Errorf("releasing project mutation lock; created resources were retained: %w", err)))
	}

	postCreateHooksRan := ctx.Config.Hooks != nil && len(ctx.Config.Hooks.PostCreate) > 0
	if postCreateHooksRan {
		outputMode := configuredHookOutputMode(ctx.Config.Hooks)
		hookStdout := cmd.OutOrStdout()
		if jsonOutput && outputMode == hooks.OutputStream {
			hookStdout = cmd.ErrOrStderr()
		}
		hookOpts := hooks.RunOpts{
			Branch:         branch,
			WorktreePath:   result.Path,
			ProjectRoot:    ctx.ProjectRoot,
			Ports:          portAssignment.Ports,
			Stdout:         hookStdout,
			Stderr:         cmd.ErrOrStderr(),
			EnvPassthrough: ctx.Config.Hooks.EnvPassthrough,
			OutputMode:     outputMode,
			Context:        cmd.Context(),
			Timeout:        ctx.Config.Hooks.Timeout,
		}
		if warnings := hooks.RunPostCreate(ctx.Config.Hooks.PostCreate, hookOpts); len(warnings) > 0 {
			for _, w := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", w)
			}
		}
	}

	if provisionOnly {
		if jsonOutput {
			if err := outputJSON(cmd, result, portAssignment.Ports); err != nil {
				return outputError(cmd, newCodedError("create_output_failed", err))
			}
		} else {
			if err := outputText(cmd, result, portAssignment.Ports); err != nil {
				return outputError(cmd, newCodedError("create_output_failed", err))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\nNext steps:\n  grove open %s   # open or restore the full tmux workspace\n  grove enter %s  # enter this worktree in the current pane\n", result.Branch, result.Branch)
		}
		return nil
	}

	tmuxMgr := tmux.NewManager(tmuxRunnerFactory())
	tmuxOpts := tmux.Options{
		ProjectRoot:  ctx.ProjectRoot,
		Branch:       branch,
		WorktreePath: result.Path,
		Env:          managed.SessionEnv(),
		TmuxConfig:   effectiveTmuxConfig(ctx.Config),
		IncludeAll:   includeAll,
		IncludeWith:  includeWith,
		Attach:       attach,
		Role:         tmux.RoleCanonical,
	}
	if err := tmuxMgr.Create(tmuxOpts); err != nil {
		setupErr := fmt.Errorf("setting up tmux workspace: %w", err)
		return outputError(cmd, newCodedError("create_tmux_failed", fmt.Errorf("%w; worktree and branch retained because tmux setup may have produced worktree side effects", setupErr)))
	}

	if jsonOutput {
		if err := outputJSON(cmd, result, portAssignment.Ports); err != nil {
			return outputError(cmd, newCodedError("create_output_failed", err))
		}
	} else if err := outputText(cmd, result, portAssignment.Ports); err != nil {
		return outputError(cmd, newCodedError("create_output_failed", err))
	}

	return nil
}

func rollbackCreatedResourcesLocked(ctx *projectContext, result *worktree.CreateResult) error {
	var cleanupErr error
	if result != nil && (result.WorktreeCreated || result.BranchCreated) {
		if _, err := ctx.Worktrees.Remove(result.Branch, result.BranchCreated, true); err != nil {
			cleanupErr = fmt.Errorf("rolling back created resources: %w", err)
		}
	}
	if err := reconcilePortRegistry(ctx); err != nil {
		cleanupErr = combineCleanupErrors(cleanupErr, fmt.Errorf("reconciling port registry after rollback: %w", err))
	}
	return cleanupErr
}

func combineCleanupErrors(original error, cleanupErrors ...error) error {
	for _, cleanupErr := range cleanupErrors {
		if cleanupErr == nil {
			continue
		}
		if original == nil {
			original = cleanupErr
			continue
		}
		original = fmt.Errorf("%w (cleanup failed: %v)", original, cleanupErr)
	}
	return original
}

func outputJSON(cmd *cobra.Command, result *worktree.CreateResult, assignedPorts map[string]int) error {
	out := createOutput{
		Worktree: result.Path,
		Branch:   result.Branch,
		Ports:    assignedPorts,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func outputText(cmd *cobra.Command, result *worktree.CreateResult, assignedPorts map[string]int) error {
	w := cmd.OutOrStdout()

	if result.Created {
		fmt.Fprintf(w, "Created worktree for branch %q (%s)\n", result.Branch, result.Resolution)
	} else {
		fmt.Fprintf(w, "Reusing existing worktree for branch %q\n", result.Branch)
	}

	fmt.Fprintf(w, "Worktree: %s\n", result.Path)

	if len(assignedPorts) > 0 {
		fmt.Fprintln(w, "Ports:")
		names := make([]string, 0, len(assignedPorts))
		for name := range assignedPorts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(w, "  %s: %d\n", name, assignedPorts[name])
		}
	}

	return nil
}

// getWorkingDir returns the current working directory.
// This is a var so tests can override it.
var getWorkingDir = os.Getwd
