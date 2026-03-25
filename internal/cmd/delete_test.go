package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/tmux"
)

func TestDeleteCmd_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	// Create a branch and worktree
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	run("branch", "feat/delete-test")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-delete-test"), "feat/delete-test")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Mock gh as unavailable
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	defer func() { ghAvailable = origGhAvailable }()

	// Mock tmux runner
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	defer func() { tmuxRunnerFactory = origFactory }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/delete-test", "--force"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("delete command failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "Deleted worktree") {
		t.Errorf("expected 'Deleted worktree' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Deleted branch") {
		t.Errorf("expected 'Deleted branch' in output, got:\n%s", output)
	}
}

func TestDeleteCmd_KeepBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	run("branch", "feat/keep-branch")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-keep-branch"), "feat/keep-branch")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	defer func() { ghAvailable = origGhAvailable }()

	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	defer func() { tmuxRunnerFactory = origFactory }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/keep-branch", "--keep-branch", "--force"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("delete command failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "Kept branch") {
		t.Errorf("expected 'Kept branch' in output, got:\n%s", output)
	}

	// Verify the branch still exists
	gitCmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/keep-branch")
	gitCmd.Dir = repoDir
	if _, err := gitCmd.CombinedOutput(); err != nil {
		t.Error("branch should still exist after --keep-branch")
	}
}

func TestDeleteCmd_OpenPR_Aborts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	run("branch", "feat/pr-test")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-pr-test"), "feat/pr-test")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Mock gh as available and returning an open PR
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return true }
	defer func() { ghAvailable = origGhAvailable }()

	origGhRunner := ghCommandRunner
	ghCommandRunner = func(args ...string) (string, error) {
		return `[{"number":42}]`, nil
	}
	defer func() { ghCommandRunner = origGhRunner }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/pr-test"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when branch has open PR")
	}
	if !strings.Contains(err.Error(), "open PR") {
		t.Errorf("expected 'open PR' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "#42") {
		t.Errorf("expected PR number in error, got: %v", err)
	}
}

func TestDeleteCmd_OpenPR_Force(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	run("branch", "feat/force-test")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-force-test"), "feat/force-test")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Mock gh as available and returning an open PR
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return true }
	defer func() { ghAvailable = origGhAvailable }()

	origGhRunner := ghCommandRunner
	ghCommandRunner = func(args ...string) (string, error) {
		return `[{"number":42}]`, nil
	}
	defer func() { ghCommandRunner = origGhRunner }()

	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	defer func() { tmuxRunnerFactory = origFactory }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/force-test", "--force"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("delete --force should succeed even with open PR: %v\nOutput: %s", err, buf.String())
	}

	if !strings.Contains(buf.String(), "Deleted worktree") {
		t.Errorf("expected 'Deleted worktree' in output, got:\n%s", buf.String())
	}
}

func TestDeleteCmd_GhNotAvailable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	run("branch", "feat/no-gh")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-no-gh"), "feat/no-gh")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Mock gh as unavailable
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	defer func() { ghAvailable = origGhAvailable }()

	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	defer func() { tmuxRunnerFactory = origFactory }()

	rootCmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"delete", "feat/no-gh", "--force"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("delete should succeed when gh is not available: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	if !strings.Contains(stdout.String(), "Deleted worktree") {
		t.Errorf("expected 'Deleted worktree' in output, got:\n%s", stdout.String())
	}
}

func TestDeleteCmd_MissingBranchArg(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing branch argument")
	}
}

func TestDeleteCmd_UnpushedBranch_Blocks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	run("branch", "feat/unpushed")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-unpushed"), "feat/unpushed")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	defer func() { ghAvailable = origGhAvailable }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/unpushed"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for branch never pushed to remote")
	}
	if !strings.Contains(err.Error(), "never been pushed") {
		t.Errorf("expected 'never been pushed' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected '--force' hint in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "git push") {
		t.Errorf("expected 'git push' hint in error, got: %v", err)
	}
}

func TestDeleteCmd_UnpushedBranch_ForceOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	run("branch", "feat/unpushed-force")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-unpushed-force"), "feat/unpushed-force")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	defer func() { ghAvailable = origGhAvailable }()

	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	defer func() { tmuxRunnerFactory = origFactory }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/unpushed-force", "--force"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("delete --force should succeed for unpushed branch: %v\nOutput: %s", err, buf.String())
	}

	if !strings.Contains(buf.String(), "Deleted worktree") {
		t.Errorf("expected 'Deleted worktree' in output, got:\n%s", buf.String())
	}
}

func TestDeleteCmd_GoneBranch_AllowsDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)
	remoteDir := t.TempDir()

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}

	// Set up a remote so we can push and then delete the remote branch
	run(remoteDir, "init", "--bare", "-b", "main")
	run(repoDir, "remote", "add", "origin", remoteDir)
	run(repoDir, "push", "-u", "origin", "main")

	// Create feature branch, push it, create worktree
	run(repoDir, "branch", "feat/gone-test")
	run(repoDir, "push", "-u", "origin", "feat/gone-test")
	run(repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-gone-test"), "feat/gone-test")

	// Delete the remote branch (simulates GitHub auto-delete after PR merge)
	run(repoDir, "push", "origin", "--delete", "feat/gone-test")
	run(repoDir, "fetch", "--prune")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	defer func() { ghAvailable = origGhAvailable }()

	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	defer func() { tmuxRunnerFactory = origFactory }()

	// Delete WITHOUT --force — should succeed because the branch is "gone"
	rootCmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"delete", "feat/gone-test"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("delete should succeed for gone branch without --force: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	if !strings.Contains(stdout.String(), "Deleted worktree") {
		t.Errorf("expected 'Deleted worktree' in output, got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "remote branch was deleted") {
		t.Errorf("expected 'remote branch was deleted' note in stderr, got:\n%s", stderr.String())
	}
}

func TestDeleteCmd_RebaseMerged_AllowsDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)
	remoteDir := t.TempDir()

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed in %s: %s: %v", strings.Join(args, " "), dir, string(out), err)
		}
	}

	// Set up a remote
	run(remoteDir, "init", "--bare", "-b", "main")
	run(repoDir, "remote", "add", "origin", remoteDir)
	run(repoDir, "push", "-u", "origin", "main")

	// Create feature branch with a commit and push
	run(repoDir, "checkout", "-b", "feat/rebase-test")
	featFile := filepath.Join(repoDir, "feature.txt")
	if err := os.WriteFile(featFile, []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(repoDir, "add", "feature.txt")
	run(repoDir, "commit", "-m", "add feature")
	run(repoDir, "push", "-u", "origin", "feat/rebase-test")

	// Switch to main and simulate a rebase merge (cherry-pick the commit)
	run(repoDir, "checkout", "main")
	run(repoDir, "cherry-pick", "feat/rebase-test")
	run(repoDir, "push", "origin", "main")

	// Back on feature, amend the commit (same patch, different SHA) to simulate
	// local divergence from origin/feat/rebase-test after a rebase
	run(repoDir, "checkout", "feat/rebase-test")
	run(repoDir, "commit", "--amend", "-m", "add feature (rebased)")

	// Now local feat/rebase-test has a different SHA than origin/feat/rebase-test
	// but the patch content is already on main via the cherry-pick.

	// Create worktree for the feature branch
	run(repoDir, "checkout", "main") // switch away before creating worktree
	run(repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-rebase-test"), "feat/rebase-test")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	defer func() { ghAvailable = origGhAvailable }()

	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	defer func() { tmuxRunnerFactory = origFactory }()

	// Delete WITHOUT --force — should succeed because content is merged
	rootCmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"delete", "feat/rebase-test"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("delete should succeed for rebase-merged branch without --force: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	if !strings.Contains(stdout.String(), "Deleted worktree") {
		t.Errorf("expected 'Deleted worktree' in output, got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "merged via rebase") {
		t.Errorf("expected 'merged via rebase' note in stderr, got:\n%s", stderr.String())
	}
}

func TestCheckOpenPRs_NoPRs(t *testing.T) {
	origGhRunner := ghCommandRunner
	ghCommandRunner = func(args ...string) (string, error) {
		return "[]", nil
	}
	defer func() { ghCommandRunner = origGhRunner }()

	has, _, err := checkOpenPRs("feat/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Error("expected no open PRs")
	}
}

func TestCheckOpenPRs_HasPR(t *testing.T) {
	origGhRunner := ghCommandRunner
	ghCommandRunner = func(args ...string) (string, error) {
		return `[{"number":123}]`, nil
	}
	defer func() { ghCommandRunner = origGhRunner }()

	has, num, err := checkOpenPRs("feat/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Error("expected open PR")
	}
	if num != "123" {
		t.Errorf("expected PR number 123, got %s", num)
	}
}

func TestCheckOpenPRs_GhError(t *testing.T) {
	origGhRunner := ghCommandRunner
	ghCommandRunner = func(args ...string) (string, error) {
		return "", exec.ErrNotFound
	}
	defer func() { ghCommandRunner = origGhRunner }()

	_, _, err := checkOpenPRs("feat/test")
	if err == nil {
		t.Error("expected error when gh fails")
	}
}
