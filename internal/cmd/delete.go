package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/hooks"
	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

type deleteFailure struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type deleteOutput struct {
	Branch        string           `json:"branch"`
	Worktree      string           `json:"worktree"`
	DeletedBranch bool             `json:"deleted_branch"`
	KeptBranch    bool             `json:"kept_branch"`
	BranchReason  string           `json:"branch_reason,omitempty"`
	Failures      []deleteFailure  `json:"failures,omitempty"`
	Error         *structuredError `json:"error,omitempty"`
}

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <branch>",
		Short: "Delete a worktree and its associated tmux session/window",
		Long: `Remove a worktree and its tmux session/window.

Safety checks (skipped with --force):
  - Open PRs: checks via gh CLI (if available)
  - Unpushed commits: blocks if the branch has local commits not on the remote
  - Never-pushed branches: blocks if the branch has no remote tracking branch
  - Uncommitted changes: git refuses to remove dirty worktrees

Smart merge detection:
  - If the remote branch was deleted, Grove still requires no unique commits
    or verifies that the branch's patches are already in the default branch.
  - If the branch has unpushed commits but all patches are already in the
    default branch (e.g. merged via rebase), deletion proceeds without --force.`,
		Args: cobra.ExactArgs(1),
		RunE: runDelete,
	}

	cmd.Flags().Bool("force", false, "skip safety checks (open PRs, unpushed commits) and force-remove dirty worktrees")
	cmd.Flags().Bool("keep-branch", false, "remove worktree but keep the git branch")
	cmd.Flags().Bool("dry-run", false, "show what would be deleted without mutating anything")
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")

	return cmd
}

// ghCommandRunner runs gh CLI commands. It is a var so tests can override it.
var ghCommandRunner = func(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ghAvailable checks if the gh CLI is installed.
var ghAvailable = func() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

func runDelete(cmd *cobra.Command, args []string) error {
	branch := args[0]

	force, _ := cmd.Flags().GetBool("force")
	keepBranch, _ := cmd.Flags().GetBool("keep-branch")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	jsonOutput := shouldOutputJSON(cmd)

	// Step 1: Discover and load config
	cwd, err := getWorkingDir()
	if err != nil {
		return outputError(cmd, newCodedError("project_discovery_failed", fmt.Errorf("getting working directory: %w", err)))
	}

	configPath, projectRoot, err := config.Discover(cwd)
	if err != nil {
		return outputError(cmd, newCodedError("project_discovery_failed", err))
	}

	cfg, err := config.LoadNoValidate(configPath)
	if err != nil {
		return outputError(cmd, newCodedError("config_invalid", err))
	}
	if err := config.ValidateHooks(cfg.Hooks); err != nil {
		return outputError(cmd, newCodedError("delete_hook_invalid", err))
	}

	// Step 2: Set up read-only worktree discovery for planning.
	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)
	commonStateDir, err := wtMgr.CommonStateDir()
	if err != nil {
		return outputError(cmd, newCodedError("git_common_dir_failed", err))
	}
	ctx := &projectContext{ConfigPath: configPath, ProjectRoot: projectRoot, CommonStateDir: commonStateDir, Config: cfg, Worktrees: wtMgr}

	wtPath := worktree.WorktreePath(projectRoot, cfg.WorktreeDir, branch)
	if worktrees, listErr := wtMgr.List(); listErr == nil {
		for _, wt := range worktrees {
			if wt.Branch == branch {
				wtPath = wt.Path
				break
			}
		}
	}
	deleteBranch := !keepBranch
	if dryRun {
		plan := struct {
			Branch             string `json:"branch"`
			Worktree           string `json:"worktree"`
			WouldDeleteBranch  bool   `json:"would_delete_branch"`
			WouldDestroyTmux   bool   `json:"would_destroy_tmux"`
			TmuxDestroyMatcher string `json:"tmux_destroy_matcher"`
		}{Branch: branch, Worktree: wtPath, WouldDeleteBranch: deleteBranch, WouldDestroyTmux: true, TmuxDestroyMatcher: "labeled"}
		if jsonOutput {
			data, _ := json.MarshalIndent(plan, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Would delete worktree for branch %q\n", branch)
		fmt.Fprintf(cmd.OutOrStdout(), "Worktree: %s\n", wtPath)
		fmt.Fprintf(cmd.OutOrStdout(), "Would delete branch: %t\n", deleteBranch)
		fmt.Fprintln(cmd.OutOrStdout(), "Would destroy tmux: labeled target for exact worktree")
		return nil
	}

	// Step 3: Run remote and unpushed safety checks only for a real deletion.
	expectedBranchTip := ""
	if !force {
		initialTip, tipErr := wtMgr.BranchTip(branch)
		if tipErr != nil {
			return outputError(cmd, newCodedError("delete_branch_tip_check_failed", tipErr))
		}
		expectedBranchTip = initialTip
		if ghAvailable() {
			hasOpenPR, prNum, checkErr := checkOpenPRs(branch)
			if checkErr != nil {
				return outputError(cmd, newCodedError("delete_pr_check_failed", fmt.Errorf("could not check for open PRs — use --force to delete anyway: %w", checkErr)))
			} else if hasOpenPR {
				return outputError(cmd, newCodedError("delete_blocked_open_pr", fmt.Errorf("branch %q has an open PR (#%s) — use --force to delete anyway", branch, prNum)))
			}
		} else {
			fmt.Fprintln(cmd.ErrOrStderr(), "Note: skipping PR check — gh not found")
		}

		status, count, err := wtMgr.CheckUnpushed(branch)
		if err != nil {
			return outputError(cmd, newCodedError("delete_unpushed_check_failed", fmt.Errorf("could not check for unpushed commits on branch %q — use --force to delete anyway: %w", branch, err)))
		}
		switch status {
		case worktree.UnpushedNoRemote:
			return outputError(cmd, newCodedError("delete_blocked_unpushed", fmt.Errorf("branch %q has never been pushed to a remote — use --force to delete anyway, or push first with: git push -u origin %s", branch, branch)))
		case worktree.UnpushedGone:
			defaultBranch := wtMgr.DefaultBranch()
			hasUnique, uniqueErr := wtMgr.BranchHasUniqueCommits(branch, defaultBranch)
			if uniqueErr != nil {
				return outputError(cmd, newCodedError("delete_unpushed_check_failed", fmt.Errorf("could not check whether branch %q has unique commits — use --force to delete anyway: %w", branch, uniqueErr)))
			}
			if !hasUnique {
				fmt.Fprintf(cmd.ErrOrStderr(), "Note: remote branch was deleted and branch has no unique commits\n")
				break
			}
			contentMerged, cherryErr := wtMgr.IsBranchContentMerged(branch, defaultBranch)
			if cherryErr != nil {
				return outputError(cmd, newCodedError("delete_unpushed_check_failed", fmt.Errorf("could not verify whether branch %q content is merged into %s — use --force to delete anyway: %w", branch, defaultBranch, cherryErr)))
			}
			if !contentMerged {
				return outputError(cmd, newCodedError("delete_blocked_unpushed", fmt.Errorf("branch %q has unique local commits and its deleted remote is not proof of merge — use --force to delete anyway, or preserve/push the commits first", branch)))
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Note: remote branch was deleted and branch content already in %s\n", defaultBranch)
		case worktree.UnpushedCommits:
			// Before blocking, check if the branch content is already in the
			// default branch (handles rebase merges where commit SHAs differ
			// but the patches are identical).
			defaultBranch := wtMgr.DefaultBranch()
			contentMerged, cherryErr := wtMgr.IsBranchContentMerged(branch, defaultBranch)
			if cherryErr != nil {
				return outputError(cmd, newCodedError("delete_unpushed_check_failed", fmt.Errorf("could not verify whether branch %q content is merged into %s — use --force to delete anyway: %w", branch, defaultBranch, cherryErr)))
			}
			if contentMerged {
				fmt.Fprintf(cmd.ErrOrStderr(), "Note: branch content already in %s (merged via rebase)\n", defaultBranch)
			} else {
				noun := "commit"
				if count > 1 {
					noun = "commits"
				}
				return outputError(cmd, newCodedError("delete_blocked_unpushed", fmt.Errorf("branch %q has %d unpushed %s — use --force to delete anyway, or push first with: git push origin %s", branch, count, noun, branch)))
			}
		}
		afterChecksTip, tipErr := wtMgr.BranchTip(branch)
		if tipErr != nil {
			return outputError(cmd, newCodedError("delete_branch_tip_check_failed", tipErr))
		}
		if afterChecksTip != expectedBranchTip {
			return outputError(cmd, newCodedError("delete_branch_changed", fmt.Errorf("branch %q changed during delete safety checks; aborting to preserve work", branch)))
		}
	}

	// Step 4: Serialize persistent assignment, hooks, and destructive cleanup.
	lock, err := acquireWorkflowLock(cmd.Context(), ctx)
	if err != nil {
		return outputError(cmd, err)
	}
	current, currentErr := findWorktreeByBranch(ctx, branch)
	if currentErr != nil {
		cleanupErr := reconcilePortRegistry(ctx)
		releaseErr := lock.Release()
		return outputError(cmd, newCodedError("worktree_not_found", combineCleanupErrors(currentErr, cleanupErr, releaseErr)))
	}
	wtPath = current.Path
	if cfg.Hooks != nil && len(cfg.Hooks.PreDelete) > 0 {
		assignment, assignErr := assignPersistentPorts(ctx, branch)
		if assignErr != nil {
			_ = lock.Release()
			return outputError(cmd, newCodedError("delete_port_assignment_failed", assignErr))
		}
		outputMode := configuredHookOutputMode(cfg.Hooks)
		hookStdout := cmd.OutOrStdout()
		if jsonOutput && outputMode == hooks.OutputStream {
			hookStdout = cmd.ErrOrStderr()
		}
		hookOpts := hooks.RunOpts{
			Branch:         branch,
			WorktreePath:   wtPath,
			ProjectRoot:    projectRoot,
			Ports:          assignment.Ports,
			Stdout:         hookStdout,
			Stderr:         cmd.ErrOrStderr(),
			EnvPassthrough: cfg.Hooks.EnvPassthrough,
			OutputMode:     outputMode,
			Context:        cmd.Context(),
			Timeout:        cfg.Hooks.Timeout,
		}
		if hookErr := hooks.RunPreDelete(cfg.Hooks.PreDelete, hookOpts); hookErr != nil {
			releaseErr := lock.Release()
			return outputError(cmd, newCodedError("delete_hook_failed", combineCleanupErrors(fmt.Errorf("pre-delete hook failed: %w", hookErr), releaseErr)))
		}
	}

	if !force {
		beforeRemovalTip, tipErr := wtMgr.BranchTip(branch)
		if tipErr != nil {
			releaseErr := lock.Release()
			return outputError(cmd, newCodedError("delete_branch_tip_check_failed", combineCleanupErrors(tipErr, releaseErr)))
		}
		if beforeRemovalTip != expectedBranchTip {
			releaseErr := lock.Release()
			return outputError(cmd, newCodedError("delete_branch_changed", combineCleanupErrors(fmt.Errorf("branch %q changed before worktree removal; aborting to preserve work", branch), releaseErr)))
		}
	}

	// Discover exact Grove-owned tmux IDs while the worktree path still exists,
	// so path aliases remain comparable after Git removes the directory.
	tmuxMgr := tmux.NewManager(tmuxRunnerFactory())
	tmuxTargets, tmuxDiscoveryErr := tmuxMgr.FindTargets(projectRoot, branch, wtPath)
	if tmuxDiscoveryErr != nil {
		releaseErr := lock.Release()
		return outputError(cmd, newCodedError("delete_tmux_discovery_failed", combineCleanupErrors(tmuxDiscoveryErr, releaseErr)))
	}

	// Step 5: Remove git worktree and optionally delete branch.
	var result *worktree.RemoveResult
	var removeErr error
	if !force {
		result, removeErr = wtMgr.RemoveIfBranchTipWithPolicy(branch, deleteBranch, false, expectedBranchTip, managedRemovalPolicy(ctx))
	} else {
		result, removeErr = wtMgr.Remove(branch, deleteBranch, true)
	}
	if removeErr != nil && (result == nil || !result.WorktreeRemoved) {
		releaseErr := lock.Release()
		return outputError(cmd, newCodedError("delete_worktree_failed", combineCleanupErrors(fmt.Errorf("removing worktree: %w", removeErr), releaseErr)))
	}

	var failures []deleteFailure
	if result != nil && result.WorktreeRemoved {
		if registryErr := reconcilePortRegistry(ctx); registryErr != nil {
			failures = append(failures, deleteFailure{Stage: "registry", Message: registryErr.Error()})
		}
	}
	if releaseErr := lock.Release(); releaseErr != nil {
		failures = append(failures, deleteFailure{Stage: "lock", Message: releaseErr.Error()})
	}

	// Step 6: Remove only the exact IDs proven Grove-owned before deletion.
	if result != nil && result.WorktreeRemoved {
		if _, killErr := tmuxMgr.DestroyTargets(tmuxTargets); killErr != nil {
			failures = append(failures, deleteFailure{Stage: "tmux", Message: killErr.Error()})
		}
	}
	if removeErr != nil {
		failures = append(failures, deleteFailure{Stage: "branch", Message: removeErr.Error()})
	}

	out := deleteOutput{
		Branch:        branch,
		Worktree:      wtPath,
		DeletedBranch: deleteBranch && result != nil && result.BranchDeleted,
		KeptBranch:    !deleteBranch || (deleteBranch && result != nil && !result.BranchDeleted),
		Failures:      failures,
	}
	if result != nil {
		out.BranchReason = result.BranchSkipReason
	}
	if len(failures) > 0 {
		out.Error = &structuredError{Code: "delete_partial_failure", Message: fmt.Sprintf("delete completed with %d failure(s)", len(failures))}
	}
	if jsonOutput {
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
	} else {
		w := cmd.OutOrStdout()
		fmt.Fprintf(w, "Deleted worktree for branch %q\n", branch)
		if !deleteBranch {
			fmt.Fprintf(w, "Kept branch %q\n", branch)
		} else if result != nil && result.BranchDeleted {
			fmt.Fprintf(w, "Deleted branch %q\n", branch)
		} else if result != nil && result.BranchSkipReason != "" {
			fmt.Fprintf(w, "Kept branch %q (%s)\n", branch, result.BranchSkipReason)
		}
		for _, failure := range failures {
			fmt.Fprintf(cmd.ErrOrStderr(), "Delete partial failure (%s): %s\n", failure.Stage, failure.Message)
		}
	}
	if len(failures) > 0 {
		return &reportedError{cause: newCodedError("delete_partial_failure", fmt.Errorf("delete completed with %d failure(s)", len(failures)))}
	}
	return nil
}

// checkOpenPRs checks if the branch has any open pull requests.
// Returns (hasOpenPR, prNumber, error).
func checkOpenPRs(branch string) (bool, string, error) {
	out, err := ghCommandRunner("pr", "list", "--head", branch, "--state", "open", "--json", "number", "--limit", "1")
	if err != nil {
		return false, "", fmt.Errorf("running gh pr list: %w", err)
	}

	var prs []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &prs); err != nil {
		return false, "", fmt.Errorf("parsing pr list output: %w", err)
	}

	if len(prs) == 0 {
		return false, "", nil
	}

	return true, fmt.Sprintf("%d", prs[0].Number), nil
}
