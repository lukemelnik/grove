package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"
)

func TestOpenCmd_NoWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := "worktree_dir: " + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)
	mockWorkingDir(t, repoDir)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"open", "feat/missing"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected open to fail for missing worktree")
	}
	if !strings.Contains(err.Error(), "grove create feat/missing") {
		t.Fatalf("expected create suggestion, got %v", err)
	}
}

func TestOpenCmd_AttachesToLabeledRenamedWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: window
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/open-labeled")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/open-labeled")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/open-labeled")

	mockWorkingDir(t, repoDir)
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")

	runner := &recordingTmuxRunner{
		outputs: map[string]string{
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "",
			"list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "@9\tdev\trenamed-by-user\t" + repoDir + "\tfeat/open-labeled\t" + wtPath + "\tcanonical",
		},
	}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"open", "feat/open-labeled"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("open failed: %v\nOutput: %s", err, buf.String())
	}
	if !tmuxCommandSeen(runner, "select-window") {
		t.Fatalf("expected select-window, got %v", runner.commands)
	}
	for _, command := range runner.commands {
		if len(command) > 0 && (command[0] == "new-window" || command[0] == "new-session") {
			t.Fatalf("expected existing labeled target to be reused, got %v", runner.commands)
		}
	}
}

func TestOpenCmd_RecreatesMissingCanonicalTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: window
  panes:
    - nvim
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/open-recreate")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/open-recreate")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/open-recreate")

	mockWorkingDir(t, repoDir)
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")

	runner := &recordingTmuxRunner{currentSession: "dev"}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"open", "feat/open-recreate"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("open failed: %v\nOutput: %s", err, buf.String())
	}
	if !tmuxCommandSeen(runner, "new-window") {
		t.Fatalf("expected missing target to be recreated, got %v", runner.commands)
	}
	if !tmuxCommandSeen(runner, "setw") {
		t.Fatalf("expected recreated window to be labeled, got %v", runner.commands)
	}
}

func TestOpenCmd_NewWindowCreatesExtraWhenCanonicalExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: window
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/open-extra")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/open-extra")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/open-extra")

	mockWorkingDir(t, repoDir)
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")

	runner := &recordingTmuxRunner{
		currentSession: "dev",
		outputs: map[string]string{
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "",
			"list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "@9\tdev\trenamed\t" + repoDir + "\tfeat/open-extra\t" + wtPath + "\tcanonical",
		},
	}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"open", "feat/open-extra", "--new-window"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("open --new-window failed: %v\nOutput: %s", err, buf.String())
	}
	if !tmuxCommandSeen(runner, "new-window") {
		t.Fatalf("expected forced new window, got %v", runner.commands)
	}
	foundExtraRole := false
	for _, command := range runner.commands {
		if len(command) == 5 && command[0] == "setw" && command[3] == "@grove.role" && command[4] == "extra" {
			foundExtraRole = true
		}
	}
	if !foundExtraRole {
		t.Fatalf("expected new window to be labeled extra, got %v", runner.commands)
	}
}

func TestOpenCmd_InvalidServiceTemplateFailsBeforeTmux(t *testing.T) {
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
    env_file: apps/api/config
    env:
      API_URL: "http://localhost:{{branh}}"
tmux:
  mode: session
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/open-bad-env")
	gitRun(t, repoDir, "worktree", "add", worktree.WorktreePath(repoDir, worktreeDir, "feat/open-bad-env"), "feat/open-bad-env")

	mockWorkingDir(t, repoDir)

	runner := &recordingTmuxRunner{}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"open", "feat/open-bad-env"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected invalid env template to fail")
	}
	if !strings.Contains(err.Error(), "resolving managed environment") {
		t.Fatalf("expected env error, got %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("expected no tmux commands before env failure, got %v", runner.commands)
	}
}

func TestAttachCmd_RoutesThroughOpen(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: session
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/attach-open")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/attach-open")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/attach-open")

	mockWorkingDir(t, repoDir)
	t.Setenv("TMUX", "")

	runner := &recordingTmuxRunner{}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"attach", "feat/attach-open"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("attach failed: %v\nOutput: %s", err, buf.String())
	}
	if !tmuxCommandSeen(runner, "new-session") {
		t.Fatalf("expected attach alias to create/open via open flow, got %v", runner.commands)
	}
}

func TestOpenCmd_DoesNotPanicInAutoJSONWhenExistingTargetAttaches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: session
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/open-auto-json")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/open-auto-json")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/open-auto-json")

	mockWorkingDir(t, repoDir)
	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })
	t.Setenv("TMUX", "")

	runner := &recordingTmuxRunner{
		outputs: map[string]string{
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "grove-session\t" + repoDir + "\tfeat/open-auto-json\t" + wtPath + "\tcanonical",
			"list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "",
		},
	}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"open", "feat/open-auto-json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("open should attach without panic/error: %v\nOutput: %s", err, buf.String())
	}
}
