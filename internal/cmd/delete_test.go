package cmd

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"grove/internal/tmux"
)

func TestDeleteCmd_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port: 4000
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
    port: 4000
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
	rootCmd.SetArgs([]string{"delete", "feat/keep-branch", "--keep-branch"})

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
    port: 4000
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
    port: 4000
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
    port: 4000
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
	rootCmd.SetArgs([]string{"delete", "feat/no-gh"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("delete should succeed when gh is not available: %v", err)
	}

	// Should print a note about skipping PR check
	if !strings.Contains(stderr.String(), "skipping PR check") {
		t.Errorf("expected 'skipping PR check' note, stderr:\n%s", stderr.String())
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
