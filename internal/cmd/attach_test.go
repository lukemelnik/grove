package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"grove/internal/tmux"
)

func TestAttachCmd_NoWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	groveYML := `worktree_dir: /tmp/grove-attach-test
services:
  api:
    port: 4000
    env: PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"attach", "nonexistent-branch"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when attaching to nonexistent worktree")
	}
	if !strings.Contains(err.Error(), "no worktree found") {
		t.Errorf("expected 'no worktree found' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "grove create") {
		t.Errorf("expected 'grove create' suggestion in error, got: %v", err)
	}
}

func TestAttachCmd_WorktreeExistsNoTmuxConfig(t *testing.T) {
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
	run("branch", "feat/attach-test")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-attach-test"), "feat/attach-test")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Use a mock tmux runner that does nothing
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	defer func() { tmuxRunnerFactory = origFactory }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"attach", "feat/attach-test"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("attach command failed: %v\nOutput: %s", err, buf.String())
	}

	// No tmux config — should still succeed using default tmux config
}

func TestAttachCmd_MissingBranchArg(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"attach"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing branch argument")
	}
}

func TestAttachCmd_WorktreeExistsWithTmux(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port: 4000
    env: PORT
tmux:
  mode: session
  layout: main-vertical
  panes:
    - nvim
    - claude
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
	run("branch", "feat/tmux-attach")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-tmux-attach"), "feat/tmux-attach")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Mock tmux runner: session does not exist, so it should create one
	mock := &recordingTmuxRunner{
		hasSessionResult: false,
		hasWindowResult:  false,
	}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return mock }
	defer func() { tmuxRunnerFactory = origFactory }()

	// Unset TMUX to simulate being outside tmux
	origTmux := os.Getenv("TMUX")
	os.Setenv("TMUX", "")
	defer os.Setenv("TMUX", origTmux)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"attach", "feat/tmux-attach"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("attach command failed: %v\nOutput: %s", err, buf.String())
	}

	// Verify tmux commands were issued (session creation)
	foundNewSession := false
	for _, cmd := range mock.commands {
		if len(cmd) > 0 && cmd[0] == "new-session" {
			foundNewSession = true
		}
	}
	if !foundNewSession {
		t.Error("expected new-session command when no tmux session exists")
	}
}

// noopTmuxRunner is a tmux runner that does nothing (all commands succeed).
type noopTmuxRunner struct{}

func (r *noopTmuxRunner) Run(args ...string) (string, error) {
	return "", nil
}

// recordingTmuxRunner records commands and allows controlling has-session/has-window results.
type recordingTmuxRunner struct {
	commands         [][]string
	hasSessionResult bool
	hasWindowResult  bool
}

func (r *recordingTmuxRunner) Run(args ...string) (string, error) {
	r.commands = append(r.commands, args)

	if len(args) > 0 {
		switch args[0] {
		case "has-session":
			if !r.hasSessionResult {
				return "", exec.ErrNotFound
			}
		case "list-windows":
			if !r.hasWindowResult {
				return "", exec.ErrNotFound
			}
		}
	}

	return "", nil
}
