package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

// staleWorktree describes a worktree that is a candidate for cleaning.
type staleWorktree struct {
	Branch   string `json:"branch"`
	Reason   string `json:"reason"`
	Worktree string `json:"worktree"`
}

// cleanOutput is the JSON output for grove clean.
type cleanFailure struct {
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Stage    string `json:"stage"`
	Message  string `json:"message"`
}

type cleanOutput struct {
	Cleaned  []staleWorktree  `json:"cleaned"`
	Failures []cleanFailure   `json:"failures,omitempty"`
	Pruned   bool             `json:"pruned"`
	Error    *structuredError `json:"error,omitempty"`
}

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove worktrees for stale branches",
		Long: `Find and remove worktrees whose branches are stale:

  - Gone branches: local branches whose remote tracking branch was deleted
    and that no longer have unique commits beyond the default branch.
  - Merged branches: branches fully merged into the main/master branch
    (detected via git branch --merged). Note: this only catches regular
    merges and fast-forwards, not squash or rebase merges.
  - Unchanged branches: branches with no unique commits beyond the default
    branch — created but never worked on.
  - With --all: also include gone branches that still have unique commits.
    This is useful for squash/rebase merge workflows after the remote branch
    has been deleted, but remote deletion alone is not proof of safe cleanup.

For each stale worktree, the associated tmux session/window is killed,
the git worktree is removed, and the local branch is deleted.`,
		Args: cobra.NoArgs,
		RunE: runClean,
	}

	cmd.Flags().Bool("dry-run", false, "show what would be cleaned without doing it")
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	cmd.Flags().Bool("discard-changes", false, "skip confirmation and discard tracked and nonignored untracked stale-worktree data")
	cmd.Flags().Bool("all", false, "also include gone branches that still have unique commits")
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")

	return cmd
}

func runClean(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
	discardChanges, _ := cmd.Flags().GetBool("discard-changes")
	includeAll, _ := cmd.Flags().GetBool("all")
	jsonOutput := shouldOutputJSON(cmd)

	// Step 1: Discover and load config
	cwd, err := getWorkingDir()
	if err != nil {
		return outputError(cmd, fmt.Errorf("getting working directory: %w", err))
	}

	configPath, projectRoot, err := config.Discover(cwd)
	if err != nil {
		return outputError(cmd, err)
	}

	cfg, err := config.LoadNoValidate(configPath)
	if err != nil {
		return outputError(cmd, err)
	}

	// Step 2: Set up worktree manager
	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)
	commonStateDir, err := wtMgr.CommonStateDir()
	if err != nil {
		return outputError(cmd, newCodedError("git_common_dir_failed", err))
	}
	ctx := &projectContext{ConfigPath: configPath, ProjectRoot: projectRoot, CommonStateDir: commonStateDir, Config: cfg, Worktrees: wtMgr}

	// Step 3: Find stale branches
	stale, err := findStaleWorktrees(wtMgr, includeAll)
	if err != nil {
		return outputError(cmd, fmt.Errorf("finding stale worktrees: %w", err))
	}

	if len(stale) == 0 {
		if jsonOutput {
			out := cleanOutput{Cleaned: []staleWorktree{}, Pruned: false}
			data, _ := json.MarshalIndent(out, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), "No stale worktrees found")
		return nil
	}

	// Step 4: Show what would be cleaned
	w := cmd.OutOrStdout()

	if dryRun {
		if jsonOutput {
			out := cleanOutput{Cleaned: stale, Pruned: false}
			data, _ := json.MarshalIndent(out, "", "  ")
			fmt.Fprintln(w, string(data))
			return nil
		}
		fmt.Fprintf(w, "Found %d stale worktrees:\n", len(stale))
		for _, s := range stale {
			fmt.Fprintf(w, "  %s (%s) — %s\n", s.Branch, s.Reason, s.Worktree)
		}
		return nil
	}

	confirmBypass := force || discardChanges
	if !confirmBypass && jsonOutput {
		return outputError(cmd, fmt.Errorf("clean requires --force when stdout is not a terminal or --json is enabled; use --dry-run to preview"))
	}

	// Step 5: Confirm (unless --force/--discard-changes or JSON mode)
	if !confirmBypass && !jsonOutput {
		fmt.Fprintf(w, "Found %d stale worktrees:\n", len(stale))
		for _, s := range stale {
			fmt.Fprintf(w, "  %s (%s) — %s\n", s.Branch, s.Reason, s.Worktree)
		}
		fmt.Fprintf(w, "\nClean these worktrees? [y/N] ")
		scanner := bufio.NewScanner(stdinReader)
		if !scanYesNo(scanner, false) {
			fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}

	// Step 6: Clean each stale worktree
	var cleaned []staleWorktree
	var failures []cleanFailure
	for _, s := range stale {
		// Stale branches can still have local edits, so only force removal when
		// the user explicitly asked for it.
		lock, lockErr := acquireWorkflowLock(cmd.Context(), ctx)
		if lockErr != nil {
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "registry", Message: lockErr.Error()})
			continue
		}
		tmuxMgr := tmux.NewManager(tmuxRunnerFactory())
		tmuxTargets, discoveryErr := tmuxMgr.FindTargets(projectRoot, s.Branch, s.Worktree)
		if discoveryErr != nil {
			_ = lock.Release()
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "tmux-discovery", Message: discoveryErr.Error()})
			continue
		}
		expectedTip, revalidateErr := revalidateStaleWorktree(wtMgr, s, includeAll)
		if revalidateErr != nil {
			_ = lock.Release()
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "revalidate", Message: revalidateErr.Error()})
			if !jsonOutput {
				fmt.Fprintf(cmd.ErrOrStderr(), "Skipped cleaning %s: %v\n", s.Branch, revalidateErr)
			}
			continue
		}
		result, err := wtMgr.RemoveIfBranchTipWithPolicy(s.Branch, true, discardChanges, expectedTip, managedRemovalPolicy(ctx))
		if err != nil && (result == nil || !result.WorktreeRemoved) {
			_ = lock.Release()
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "worktree", Message: err.Error()})
			if !jsonOutput {
				fmt.Fprintf(cmd.ErrOrStderr(), "Failed to clean %s: %v\n", s.Branch, err)
			}
			continue
		}
		if result == nil || !result.WorktreeRemoved {
			_ = lock.Release()
			continue
		}
		branchFailureStart := len(failures)
		if registryErr := reconcilePortRegistry(ctx); registryErr != nil {
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "registry", Message: registryErr.Error()})
		}
		if releaseErr := lock.Release(); releaseErr != nil {
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "lock", Message: releaseErr.Error()})
		}

		if _, tmuxErr := tmuxMgr.DestroyTargets(tmuxTargets); tmuxErr != nil {
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "tmux", Message: tmuxErr.Error()})
			if !jsonOutput {
				fmt.Fprintf(cmd.ErrOrStderr(), "Failed to clean tmux for %s: %v\n", s.Branch, tmuxErr)
			}
		}

		if result.BranchDeleteError != nil {
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "branch", Message: result.BranchDeleteError.Error()})
			if !jsonOutput {
				fmt.Fprintf(cmd.ErrOrStderr(), "Failed to delete branch %s: %v\n", s.Branch, result.BranchDeleteError)
			}
		} else if !result.BranchDeleted {
			message := result.BranchSkipReason
			if message == "" {
				message = "branch was not deleted"
			}
			failures = append(failures, cleanFailure{Branch: s.Branch, Worktree: s.Worktree, Stage: "branch", Message: message})
		}

		if len(failures) == branchFailureStart {
			cleaned = append(cleaned, s)
			if !jsonOutput {
				fmt.Fprintf(w, "Cleaned %s\n", s.Branch)
			}
		}
	}

	// Step 7: Prune worktree metadata under the same project mutation protocol.
	pruned := false
	pruneLock, lockErr := acquireWorkflowLock(cmd.Context(), ctx)
	if lockErr != nil {
		failures = append(failures, cleanFailure{Stage: "lock", Message: lockErr.Error()})
	} else {
		pruneErr := wtMgr.Prune()
		releaseErr := pruneLock.Release()
		switch {
		case pruneErr != nil:
			failures = append(failures, cleanFailure{Stage: "prune", Message: pruneErr.Error()})
			if !jsonOutput {
				fmt.Fprintf(cmd.ErrOrStderr(), "Failed to prune worktree metadata: %v\n", pruneErr)
			}
		case releaseErr != nil:
			failures = append(failures, cleanFailure{Stage: "lock", Message: releaseErr.Error()})
		default:
			pruned = true
			if !jsonOutput {
				fmt.Fprintln(w, "Pruned worktree metadata")
			}
		}
	}

	// Step 8: JSON output
	if jsonOutput {
		if cleaned == nil {
			cleaned = []staleWorktree{}
		}
		out := cleanOutput{Cleaned: cleaned, Failures: failures, Pruned: pruned}
		if len(failures) > 0 {
			out.Error = &structuredError{Code: "clean_partial_failure", Message: fmt.Sprintf("clean completed with %d failure(s)", len(failures))}
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(w, string(data))
	}
	if len(failures) > 0 {
		return &reportedError{cause: newCodedError("clean_partial_failure", fmt.Errorf("clean completed with %d failure(s)", len(failures)))}
	}

	return nil
}

func revalidateStaleWorktree(wtMgr *worktree.Manager, candidate staleWorktree, includeGoneUnique bool) (string, error) {
	beforeTip, err := wtMgr.BranchTip(candidate.Branch)
	if err != nil {
		return "", fmt.Errorf("candidate branch is no longer available: %w", err)
	}
	stale, err := findStaleWorktrees(wtMgr, includeGoneUnique)
	if err != nil {
		return "", fmt.Errorf("rechecking stale status: %w", err)
	}
	stillSafe := false
	for _, current := range stale {
		if current.Branch == candidate.Branch && current.Worktree == candidate.Worktree && current.Reason == candidate.Reason {
			stillSafe = true
			break
		}
	}
	if !stillSafe {
		return "", fmt.Errorf("candidate is no longer stale at the same branch/path/reason")
	}
	afterTip, err := wtMgr.BranchTip(candidate.Branch)
	if err != nil {
		return "", fmt.Errorf("candidate branch disappeared during revalidation: %w", err)
	}
	if beforeTip != afterTip {
		return "", fmt.Errorf("branch %q changed during revalidation; aborting to preserve work", candidate.Branch)
	}
	return beforeTip, nil
}

// findStaleWorktrees finds worktrees whose branches are stale.
func findStaleWorktrees(wtMgr *worktree.Manager, includeGoneUnique bool) ([]staleWorktree, error) {
	// Get all worktrees
	worktrees, err := wtMgr.List()
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}

	// Build a set of branches that have worktrees
	wtBranches := make(map[string]string) // branch -> path
	for _, wt := range worktrees {
		if wt.IsBare || wt.Branch == "" {
			continue
		}
		wtBranches[wt.Branch] = wt.Path
	}

	// Detect the default branch to skip it
	defaultBranch := wtMgr.DefaultBranch()

	// Find gone branches
	goneBranches, err := wtMgr.GoneBranches()
	if err != nil {
		return nil, err
	}

	// Find merged branches
	mergedBranches, err := wtMgr.MergedBranches(defaultBranch)
	if err != nil {
		return nil, fmt.Errorf("listing branches merged into %s: %w", defaultBranch, err)
	}

	// Collect stale worktrees (only those that have an active worktree)
	seen := make(map[string]bool)
	var stale []staleWorktree

	// Gone branches first (higher priority reason)
	for _, branch := range goneBranches {
		if branch == defaultBranch {
			continue
		}
		path, hasWorktree := wtBranches[branch]
		if !hasWorktree {
			continue
		}
		if seen[branch] {
			continue
		}
		hasUnique, err := wtMgr.BranchHasUniqueCommits(branch, defaultBranch)
		if err != nil {
			return nil, fmt.Errorf("checking unique commits for gone branch %q: %w", branch, err)
		}
		reason := "gone"
		if hasUnique {
			if !includeGoneUnique {
				continue
			}
			reason = "gone-unique"
		}
		seen[branch] = true
		stale = append(stale, staleWorktree{
			Branch:   branch,
			Reason:   reason,
			Worktree: path,
		})
	}

	// Merged branches — distinguish "merged" (had work) from "unchanged" (no commits)
	for _, branch := range mergedBranches {
		if branch == defaultBranch {
			continue
		}
		path, hasWorktree := wtBranches[branch]
		if !hasWorktree {
			continue
		}
		if seen[branch] {
			continue
		}
		seen[branch] = true
		reason := "merged"
		hasUnique, err := wtMgr.BranchHasUniqueCommits(branch, defaultBranch)
		if err != nil {
			return nil, fmt.Errorf("checking unique commits for merged branch %q: %w", branch, err)
		}
		if !hasUnique {
			reason = "unchanged"
		}
		stale = append(stale, staleWorktree{
			Branch:   branch,
			Reason:   reason,
			Worktree: path,
		})
	}

	// Sort by branch name for deterministic output
	sort.Slice(stale, func(i, j int) bool {
		return stale[i].Branch < stale[j].Branch
	})

	return stale, nil
}
