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
type cleanOutput struct {
	Cleaned []staleWorktree `json:"cleaned"`
	Pruned  bool            `json:"pruned"`
}

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove worktrees for stale (gone or merged) branches",
		Long: `Find and remove worktrees whose branches are stale:

  - Gone branches: local branches whose remote tracking branch was deleted
    (typically after merging a PR). This is the most reliable detection
    method and works regardless of merge strategy (merge, squash, or rebase).
    Enable "Automatically delete head branches" in your GitHub repo settings
    to ensure remote branches are cleaned up after merging.
  - Merged branches: branches fully merged into the main/master branch
    (detected via git branch --merged). Note: this only catches regular
    merges and fast-forwards, not squash or rebase merges.
  - Unchanged branches: branches with no unique commits beyond the default
    branch — created but never worked on.

For each stale worktree, the associated tmux session/window is killed,
the git worktree is removed, and the local branch is deleted.`,
		Args: cobra.NoArgs,
		RunE: runClean,
	}

	cmd.Flags().Bool("dry-run", false, "show what would be cleaned without doing it")
	cmd.Flags().Bool("force", false, "skip confirmation prompt")
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")

	return cmd
}

func runClean(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")
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

	cfg, err := config.Load(configPath)
	if err != nil {
		return outputError(cmd, err)
	}

	// Step 2: Set up worktree manager
	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)

	// Step 3: Find stale branches
	stale, err := findStaleWorktrees(wtMgr)
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

	if !force && jsonOutput {
		return outputError(cmd, fmt.Errorf("clean requires --force when stdout is not a terminal or --json is enabled; use --dry-run to preview"))
	}

	// Step 5: Confirm (unless --force or JSON mode)
	if !force && !jsonOutput {
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
	for _, s := range stale {
		// Kill tmux session/window if configured
		if cfg.Tmux != nil {
			destroyTmuxForBranch(cmd, s.Branch, cfg.Tmux)
		}

		// Stale branches can still have local edits, so only force removal when
		// the user explicitly asked for it.
		if _, err := wtMgr.Remove(s.Branch, true, force); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not remove worktree for %s: %v\n", s.Branch, err)
			continue
		}

		cleaned = append(cleaned, s)
		if !jsonOutput {
			fmt.Fprintf(w, "Cleaned %s\n", s.Branch)
		}
	}

	// Step 7: Prune worktree metadata
	pruned := false
	if err := wtMgr.Prune(); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not prune worktree metadata: %v\n", err)
	} else {
		pruned = true
		if !jsonOutput {
			fmt.Fprintln(w, "Pruned worktree metadata")
		}
	}

	// Step 8: JSON output
	if jsonOutput {
		if cleaned == nil {
			cleaned = []staleWorktree{}
		}
		out := cleanOutput{Cleaned: cleaned, Pruned: pruned}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Fprintln(w, string(data))
	}

	return nil
}

// findStaleWorktrees finds worktrees whose branches are gone or merged.
func findStaleWorktrees(wtMgr *worktree.Manager) ([]staleWorktree, error) {
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
		// Non-fatal: merged detection might fail in some repo states
		mergedBranches = nil
	}

	// Build the merged set for quick lookup
	mergedSet := make(map[string]bool)
	for _, b := range mergedBranches {
		mergedSet[b] = true
	}

	// Build the gone set
	goneSet := make(map[string]bool)
	for _, b := range goneBranches {
		goneSet[b] = true
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
		if err != nil || hasUnique {
			continue
		}
		seen[branch] = true
		stale = append(stale, staleWorktree{
			Branch:   branch,
			Reason:   "gone",
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
		if err == nil && !hasUnique {
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

// destroyTmuxForBranch kills the tmux session/window for a branch, ignoring errors.
func destroyTmuxForBranch(cmd *cobra.Command, branch string, tmuxCfg *config.TmuxConfig) {
	tmuxRunner := tmuxRunnerFactory()
	tmuxMgr := tmux.NewManager(tmuxRunner)

	mode := tmuxCfg.Mode
	if mode == "" {
		mode = "window"
	}
	name := tmuxMgr.ResolveName(branch, mode)

	switch mode {
	case "session":
		if tmuxMgr.HasSession(name) {
			if err := tmuxMgr.Destroy(branch, tmuxCfg); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not kill tmux session for %s: %v\n", branch, err)
			}
		}
	case "window":
		if tmuxMgr.HasWindow(name) {
			if err := tmuxMgr.Destroy(branch, tmuxCfg); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not kill tmux window for %s: %v\n", branch, err)
			}
		}
	}
}
