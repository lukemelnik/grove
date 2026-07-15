package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	if !strings.Contains(output, `"worktree":`) || !strings.Contains(output, `"deleted_branch": true`) {
		t.Errorf("expected JSON deletion result, got:\n%s", output)
	}
}

func TestDeleteCmd_StreamHookKeepsJSONStdoutClean(t *testing.T) {
	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
hooks:
  output: stream
  pre_delete:
    - scripts/stream.sh
`
	repoDir := setupCreateTestRepo(t, groveYML)
	scriptsDir := filepath.Join(repoDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "stream.sh"), []byte("#!/bin/sh\nprintf hook-stream-output\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoDir, "add", "scripts/stream.sh")
	gitRun(t, repoDir, "commit", "-m", "add streaming hook")
	gitRun(t, repoDir, "branch", "feat/delete-stream-json")
	gitRun(t, repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-delete-stream-json"), "feat/delete-stream-json")

	mockWorkingDir(t, repoDir)
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	t.Cleanup(func() { ghAvailable = origGhAvailable })
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"delete", "feat/delete-stream-json", "--force", "--json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("delete failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	var out deleteOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nRaw: %s", err, stdout.String())
	}
	if strings.Contains(stdout.String(), "hook-stream-output") {
		t.Fatalf("stdout contained hook text: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "hook-stream-output") {
		t.Fatalf("stderr did not receive streamed hook output: %s", stderr.String())
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
	if !strings.Contains(output, `"kept_branch": true`) {
		t.Errorf("expected JSON kept-branch result, got:\n%s", output)
	}

	// Verify the branch still exists
	gitCmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/keep-branch")
	gitCmd.Dir = repoDir
	if _, err := gitCmd.CombinedOutput(); err != nil {
		t.Error("branch should still exist after --keep-branch")
	}
}

func TestDeleteCmd_PreDeleteHookRunsBeforeRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
hooks:
  pre_delete:
    - scripts/pre-delete.sh
`
	repoDir := setupCreateTestRepo(t, groveYML)

	if err := os.MkdirAll(filepath.Join(repoDir, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	hookScript := `#!/bin/bash
if [ ! -d "$GROVE_WORKTREE" ]; then
  exit 7
fi
{
  echo "GROVE_BRANCH=$GROVE_BRANCH"
  echo "GROVE_WORKTREE=$GROVE_WORKTREE"
  pwd
} > "$GROVE_PROJECT_ROOT/pre-delete-ran.txt"
`
	if err := os.WriteFile(filepath.Join(repoDir, "scripts", "pre-delete.sh"), []byte(hookScript), 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoDir, "add", "scripts/pre-delete.sh")
	gitRun(t, repoDir, "commit", "-m", "add pre-delete hook")
	gitRun(t, repoDir, "branch", "feat/pre-delete")
	wtPath := filepath.Join(worktreeDir, "feat-pre-delete")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/pre-delete")
	wantWtPath := wtPath
	if resolved, err := filepath.EvalSymlinks(wtPath); err == nil {
		wantWtPath = resolved
	}

	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/pre-delete", "--force"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("delete command failed: %v\nOutput: %s", err, buf.String())
	}

	data, err := os.ReadFile(filepath.Join(repoDir, "pre-delete-ran.txt"))
	if err != nil {
		t.Fatalf("pre-delete marker not found: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "GROVE_BRANCH=feat/pre-delete") {
		t.Errorf("hook did not receive branch, got:\n%s", content)
	}
	if !strings.Contains(content, "GROVE_WORKTREE="+wantWtPath) {
		t.Errorf("hook did not receive worktree path, got:\n%s", content)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("expected worktree to be removed, stat err: %v", err)
	}
}

func TestDeleteCmd_PreDeleteHookCommitAbortsRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
hooks:
  pre_delete:
    - scripts/commit-pre-delete.sh
`
	repoDir := setupCreateTestRepo(t, groveYML)

	if err := os.MkdirAll(filepath.Join(repoDir, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	hookScript := `#!/bin/bash
set -e
cd "$GROVE_WORKTREE"
printf 'created by hook\n' > hook-created.txt
git add hook-created.txt
git commit -m 'hook creates work'
`
	if err := os.WriteFile(filepath.Join(repoDir, "scripts", "commit-pre-delete.sh"), []byte(hookScript), 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoDir, "add", "scripts/commit-pre-delete.sh")
	gitRun(t, repoDir, "commit", "-m", "add committing pre-delete hook")
	gitRun(t, repoDir, "branch", "feat/pre-delete-commit")
	tipCmd := exec.Command("git", "rev-parse", "refs/heads/feat/pre-delete-commit")
	tipCmd.Dir = repoDir
	tipOut, tipErr := tipCmd.Output()
	if tipErr != nil {
		t.Fatalf("reading test branch tip: %v", tipErr)
	}
	gitRun(t, repoDir, "update-ref", "refs/remotes/origin/feat/pre-delete-commit", strings.TrimSpace(string(tipOut)))
	gitRun(t, repoDir, "config", "branch.feat/pre-delete-commit.remote", "origin")
	gitRun(t, repoDir, "config", "branch.feat/pre-delete-commit.merge", "refs/heads/feat/pre-delete-commit")
	wtPath := filepath.Join(worktreeDir, "feat-pre-delete-commit")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/pre-delete-commit")

	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	t.Cleanup(func() { ghAvailable = origGhAvailable })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/pre-delete-commit"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected delete to abort after hook changed branch\nOutput: %s", buf.String())
	}
	if !strings.Contains(buf.String()+err.Error(), "changed before worktree removal") {
		t.Fatalf("expected branch-changed abort, got err=%v output=%s", err, buf.String())
	}
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Fatalf("worktree should remain after hook-created commit: %v", statErr)
	}
	if _, verifyErr := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "refs/heads/feat/pre-delete-commit").CombinedOutput(); verifyErr != nil {
		t.Fatal("branch should remain after hook-created commit")
	}
}

func TestDeleteCmd_PreDeleteHookFailureAbortsRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
hooks:
  pre_delete:
    - scripts/fail-pre-delete.sh
`
	repoDir := setupCreateTestRepo(t, groveYML)

	if err := os.MkdirAll(filepath.Join(repoDir, "scripts"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "scripts", "fail-pre-delete.sh"), []byte("#!/bin/bash\nexit 17\n"), 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoDir, "add", "scripts/fail-pre-delete.sh")
	gitRun(t, repoDir, "commit", "-m", "add failing pre-delete hook")
	gitRun(t, repoDir, "branch", "feat/pre-delete-fails")
	wtPath := filepath.Join(worktreeDir, "feat-pre-delete-fails")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/pre-delete-fails")

	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/pre-delete-fails", "--force"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected pre-delete failure")
	}
	if !strings.Contains(err.Error(), "pre-delete hook failed") {
		t.Errorf("expected pre-delete failure, got: %v", err)
	}
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Errorf("expected worktree to remain after hook failure, stat err: %v", statErr)
	}
	gitCmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/pre-delete-fails")
	gitCmd.Dir = repoDir
	if _, verifyErr := gitCmd.CombinedOutput(); verifyErr != nil {
		t.Error("branch should remain after hook failure")
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

	if !strings.Contains(buf.String(), `"worktree":`) {
		t.Errorf("expected JSON deletion result, got:\n%s", buf.String())
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

	if !strings.Contains(stdout.String(), `"worktree":`) {
		t.Errorf("expected JSON deletion result, got:\n%s", stdout.String())
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

	if !strings.Contains(buf.String(), `"worktree":`) {
		t.Errorf("expected JSON deletion result, got:\n%s", buf.String())
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

	if !strings.Contains(stdout.String(), `"worktree":`) {
		t.Errorf("expected JSON deletion result, got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "remote branch was deleted") {
		t.Errorf("expected 'remote branch was deleted' note in stderr, got:\n%s", stderr.String())
	}
}

func TestDeleteCmd_GoneBranchWithUniqueLocalCommit_BlocksDelete(t *testing.T) {
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

	run(remoteDir, "init", "--bare", "-b", "main")
	run(repoDir, "remote", "add", "origin", remoteDir)
	run(repoDir, "push", "-u", "origin", "main")
	run(repoDir, "checkout", "-b", "feat/gone-unique")
	run(repoDir, "push", "-u", "origin", "feat/gone-unique")
	run(repoDir, "checkout", "main")
	wtPath := filepath.Join(worktreeDir, "feat-gone-unique")
	run(repoDir, "worktree", "add", wtPath, "feat/gone-unique")
	run(repoDir, "push", "origin", "--delete", "feat/gone-unique")
	run(repoDir, "fetch", "--prune")

	if err := os.WriteFile(filepath.Join(wtPath, "local-only.txt"), []byte("local only\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(wtPath, "add", "local-only.txt")
	run(wtPath, "commit", "-m", "local only")

	mockWorkingDir(t, repoDir)
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	t.Cleanup(func() { ghAvailable = origGhAvailable })

	runner := &recordingTmuxRunner{}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/gone-unique"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected delete to block gone branch with unique local commit; output: %s", buf.String())
	}
	if !strings.Contains(err.Error(), "unique local commits") {
		t.Fatalf("expected unique local commits error, got: %v", err)
	}
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Fatalf("expected worktree to remain: %v", statErr)
	}
	for _, command := range runner.commands {
		if len(command) > 0 && (command[0] == "kill-window" || command[0] == "kill-session") {
			t.Fatalf("tmux target should not be killed when delete is blocked, got %v", runner.commands)
		}
	}
}

func TestDeleteCmd_RemoveFailureDoesNotKillTmux(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: window
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/dirty-delete")
	wtPath := filepath.Join(worktreeDir, "feat-dirty-delete")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/dirty-delete")
	if err := os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mockWorkingDir(t, repoDir)
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	t.Cleanup(func() { ghAvailable = origGhAvailable })

	runner := &recordingTmuxRunner{
		outputs: map[string]string{
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}":                                 "",
			"list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "@9\tdev\trenamed\t" + repoDir + "\tfeat/dirty-delete\t" + wtPath + "\tcanonical",
		},
	}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/dirty-delete", "--force"})

	// Make the path no longer look like a worktree so Remove fails even with --force.
	if err := os.Remove(filepath.Join(wtPath, ".git")); err != nil {
		t.Fatal(err)
	}

	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected remove failure; output: %s", buf.String())
	}
	for _, command := range runner.commands {
		if len(command) > 0 && (command[0] == "kill-window" || command[0] == "kill-session") {
			t.Fatalf("tmux target should not be killed when worktree removal fails, got %v", runner.commands)
		}
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

	if !strings.Contains(stdout.String(), `"worktree":`) {
		t.Errorf("expected JSON deletion result, got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "merged via rebase") {
		t.Errorf("expected 'merged via rebase' note in stderr, got:\n%s", stderr.String())
	}
}

func TestDeleteCmd_JSONPartialTmuxFailureIncludesStructuredError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	worktreeDir := t.TempDir()
	repoDir := setupCreateTestRepo(t, "worktree_dir: "+worktreeDir+"\n")
	branch := "feat/delete-partial"
	gitRun(t, repoDir, "branch", branch)
	wtPath := filepath.Join(worktreeDir, "feat-delete-partial")
	gitRun(t, repoDir, "worktree", "add", wtPath, branch)
	mockWorkingDir(t, repoDir)
	oldGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	t.Cleanup(func() { ghAvailable = oldGhAvailable })

	runner := &recordingTmuxRunner{
		outputs: map[string]string{
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}":                                 "",
			"list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "@partial\tdev\trenamed\t" + repoDir + "\t" + branch + "\t" + wtPath + "\tcanonical",
		},
		errors: map[string]error{
			"list-windows -t dev -F #{window_id}": fmt.Errorf("synthetic count failure"),
		},
	}
	oldFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = oldFactory })

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"delete", branch, "--force", "--json"})
	err := root.Execute()
	if err == nil || !ErrorAlreadyReported(err) {
		t.Fatalf("expected reported partial failure, got %v; commands=%v; stdout=%s; stderr=%s", err, runner.commands, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"code": "delete_partial_failure"`) || !strings.Contains(stdout.String(), `"stage": "tmux"`) {
		t.Fatalf("partial JSON missing structured error: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("partial JSON emitted duplicate stderr: %s", stderr.String())
	}
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Fatalf("worktree was not removed before tmux partial failure: %v", statErr)
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

func TestDeleteCmd_KillsLabeledRenamedWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: window
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/delete-labeled")
	wtPath := filepath.Join(worktreeDir, "feat-delete-labeled")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/delete-labeled")

	mockWorkingDir(t, repoDir)
	origGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	t.Cleanup(func() { ghAvailable = origGhAvailable })

	labelPath, err := filepath.EvalSymlinks(wtPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := &recordingTmuxRunner{
		outputs: map[string]string{
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}":                                 "",
			"list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "@7\tdev\trenamed\t" + repoDir + "\tfeat/delete-labeled\t" + labelPath + "\tcanonical",
		},
	}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/delete-labeled", "--force"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("delete failed: %v\nOutput: %s", err, buf.String())
	}
	foundKill := false
	for _, command := range runner.commands {
		if len(command) == 3 && command[0] == "kill-window" && command[2] == "@7" {
			foundKill = true
		}
	}
	if !foundKill {
		t.Fatalf("expected labeled window kill, got %v", runner.commands)
	}
}
