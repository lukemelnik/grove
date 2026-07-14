package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/hooks"
	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

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

	// Step 1: Discover and load config
	cwd, err := getWorkingDir()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	configPath, projectRoot, err := config.Discover(cwd)
	if err != nil {
		return err
	}

	cfg, err := config.LoadNoValidate(configPath)
	if err != nil {
		return err
	}

	// Step 2: Check for open PRs (unless --force)
	if !force {
		if ghAvailable() {
			hasOpenPR, prNum, err := checkOpenPRs(branch)
			if err != nil {
				// Non-fatal — just warn and continue
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not check for open PRs: %v\n", err)
			} else if hasOpenPR {
				return fmt.Errorf("branch %q has an open PR (#%s) — use --force to delete anyway", branch, prNum)
			}
		} else {
			fmt.Fprintln(cmd.ErrOrStderr(), "Note: skipping PR check — gh not found")
		}
	}

	// Step 3: Check for unpushed commits (unless --force)
	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)

	if !force {
		status, count, err := wtMgr.CheckUnpushed(branch)
		if err != nil {
			return fmt.Errorf("could not check for unpushed commits on branch %q — use --force to delete anyway: %w", branch, err)
		}
		switch status {
		case worktree.UnpushedNoRemote:
			return fmt.Errorf("branch %q has never been pushed to a remote — use --force to delete anyway, or push first with: git push -u origin %s", branch, branch)
		case worktree.UnpushedGone:
			defaultBranch := wtMgr.DefaultBranch()
			hasUnique, uniqueErr := wtMgr.BranchHasUniqueCommits(branch, defaultBranch)
			if uniqueErr != nil {
				return fmt.Errorf("could not check whether branch %q has unique commits — use --force to delete anyway: %w", branch, uniqueErr)
			}
			if !hasUnique {
				fmt.Fprintf(cmd.ErrOrStderr(), "Note: remote branch was deleted and branch has no unique commits\n")
				break
			}
			contentMerged, cherryErr := wtMgr.IsBranchContentMerged(branch, defaultBranch)
			if cherryErr != nil {
				return fmt.Errorf("could not verify whether branch %q content is merged into %s — use --force to delete anyway: %w", branch, defaultBranch, cherryErr)
			}
			if !contentMerged {
				return fmt.Errorf("branch %q has unique local commits and its deleted remote is not proof of merge — use --force to delete anyway, or preserve/push the commits first", branch)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Note: remote branch was deleted and branch content already in %s\n", defaultBranch)
		case worktree.UnpushedCommits:
			// Before blocking, check if the branch content is already in the
			// default branch (handles rebase merges where commit SHAs differ
			// but the patches are identical).
			defaultBranch := wtMgr.DefaultBranch()
			contentMerged, cherryErr := wtMgr.IsBranchContentMerged(branch, defaultBranch)
			if cherryErr != nil {
				return fmt.Errorf("could not verify whether branch %q content is merged into %s — use --force to delete anyway: %w", branch, defaultBranch, cherryErr)
			}
			if contentMerged {
				fmt.Fprintf(cmd.ErrOrStderr(), "Note: branch content already in %s (merged via rebase)\n", defaultBranch)
			} else {
				noun := "commit"
				if count > 1 {
					noun = "commits"
				}
				return fmt.Errorf("branch %q has %d unpushed %s — use --force to delete anyway, or push first with: git push origin %s", branch, count, noun, branch)
			}
		}
	}

	if cfg.Hooks != nil && len(cfg.Hooks.PreDelete) > 0 {
		if err := config.ValidateHookScripts("pre_delete", cfg.Hooks.PreDelete); err != nil {
			return err
		}
	}

	wtPath := worktree.WorktreePath(projectRoot, cfg.WorktreeDir, branch)
	if worktrees, listErr := wtMgr.List(); listErr == nil {
		for _, wt := range worktrees {
			if wt.Branch == branch {
				wtPath = wt.Path
				break
			}
		}
	}

	// Step 4: Run pre-delete hooks before any destructive cleanup.
	if cfg.Hooks != nil && len(cfg.Hooks.PreDelete) > 0 {
		preDeletePorts := map[string]int{}
		if len(cfg.Services) > 0 {
			assignment, assignErr := ports.Assign(cfg.Services, branch, ports.DefaultMaxOffset, wtMgr.DefaultBranch())
			if assignErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not assign hook ports: %v\n", assignErr)
			} else {
				preDeletePorts = assignment.Ports
			}
		}
		hookOpts := hooks.RunOpts{
			Branch:       branch,
			WorktreePath: wtPath,
			ProjectRoot:  projectRoot,
			Ports:        preDeletePorts,
			Stdout:       cmd.OutOrStdout(),
			Stderr:       cmd.ErrOrStderr(),
		}
		if err := hooks.RunPreDelete(cfg.Hooks.PreDelete, hookOpts); err != nil {
			return fmt.Errorf("pre-delete hook failed: %w", err)
		}
	}

	// Step 5: Remove git worktree and optionally delete branch
	deleteBranch := !keepBranch
	result, err := wtMgr.Remove(branch, deleteBranch, force)
	if err != nil {
		return fmt.Errorf("removing worktree: %w", err)
	}

	// Step 6: Remove tmux targets after the worktree was removed. Prefer Grove
	// labels so renamed windows are cleaned up, then fall back to legacy name lookup.
	tmuxCfg := cfg.Tmux
	if tmuxCfg == nil {
		tmuxCfg = &config.TmuxConfig{}
	}
	{
		tmuxRunner := tmuxRunnerFactory()
		tmuxMgr := tmux.NewManager(tmuxRunner)

		// The worktree path may no longer resolve after removal, so match labels
		// by the still-existing project root and branch.
		killedLabeled, killErr := tmuxMgr.DestroyLabeled(projectRoot, branch, "")
		if killErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not kill labeled tmux target: %v\n", killErr)
		}
		if !killedLabeled {
			mode := tmuxCfg.Mode
			if mode == "" {
				mode = "window"
			}
			name := tmuxMgr.ResolveName(branch, mode)

			// Try to kill — ignore errors (session/window may not be running)
			switch mode {
			case "session":
				if tmuxMgr.HasSession(name) {
					if err := tmuxMgr.Destroy(branch, tmuxCfg); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not kill tmux session: %v\n", err)
					}
				}
			case "window":
				if tmuxMgr.HasWindow(name) {
					if err := tmuxMgr.Destroy(branch, tmuxCfg); err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not kill tmux window: %v\n", err)
					}
				}
			}
		}
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Deleted worktree for branch %q\n", branch)
	if !deleteBranch {
		fmt.Fprintf(w, "Kept branch %q\n", branch)
	} else if result.BranchDeleted {
		fmt.Fprintf(w, "Deleted branch %q\n", branch)
	} else if result.BranchSkipReason != "" {
		fmt.Fprintf(w, "Kept branch %q (%s)\n", branch, result.BranchSkipReason)
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
