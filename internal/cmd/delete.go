package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lukemelnik/grove/internal/config"
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
  - Uncommitted changes: git refuses to remove dirty worktrees`,
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
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not check for unpushed commits: %v\n", err)
		} else {
			switch status {
			case worktree.UnpushedNoRemote:
				return fmt.Errorf("branch %q has never been pushed to a remote — use --force to delete anyway, or push first with: git push -u origin %s", branch, branch)
			case worktree.UnpushedCommits:
				noun := "commit"
				if count > 1 {
					noun = "commits"
				}
				return fmt.Errorf("branch %q has %d unpushed %s — use --force to delete anyway, or push first with: git push origin %s", branch, count, noun, branch)
			}
		}
	}

	// Step 4: Remove tmux session/window (if tmux config exists)
	if cfg.Tmux != nil {
		tmuxRunner := tmuxRunnerFactory()
		tmuxMgr := tmux.NewManager(tmuxRunner)

		mode := cfg.Tmux.Mode
		if mode == "" {
			mode = "window"
		}
		name := tmuxMgr.ResolveName(branch, mode)

		// Try to kill — ignore errors (session/window may not be running)
		switch mode {
		case "session":
			if tmuxMgr.HasSession(name) {
				if err := tmuxMgr.Destroy(branch, cfg.Tmux); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not kill tmux session: %v\n", err)
				}
			}
		case "window":
			if tmuxMgr.HasWindow(name) {
				if err := tmuxMgr.Destroy(branch, cfg.Tmux); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not kill tmux window: %v\n", err)
				}
			}
		}
	}

	// Step 5: Remove git worktree and optionally delete branch
	deleteBranch := !keepBranch
	result, err := wtMgr.Remove(branch, deleteBranch, force)
	if err != nil {
		return fmt.Errorf("removing worktree: %w", err)
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
