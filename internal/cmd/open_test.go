package cmd

import (
	"bytes"
	"os"
	"path/filepath"
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
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}":                                 "",
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

func TestOpenCmd_DoesNotAttachToUnlabeledSameNameTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: window
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/unlabeled")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/unlabeled")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/unlabeled")

	mockWorkingDir(t, repoDir)
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")

	runner := &recordingTmuxRunner{
		hasWindowResult: true,
		currentSession:  "dev",
		outputs: map[string]string{
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}":                                 "",
			"list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "",
			"new-window -P -F #{window_id} -t dev -n " + tmux.SessionName("feat/unlabeled") + " -c " + wtPath:                                                     "@55",
		},
	}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"open", "feat/unlabeled"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("open failed: %v\nOutput: %s", err, buf.String())
	}
	if !tmuxCommandSeen(runner, "new-window") {
		t.Fatalf("expected unlabeled target to be ignored and recreated, got %v", runner.commands)
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

func TestOpenCmd_MainWorktreeRecreationPreservesCanonicalEnvSource(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
env_files:
  - config/secrets
tmux:
  mode: session
  panes:
    - nvim
`
	repoDir := setupCreateTestRepo(t, groveYML)
	if err := os.MkdirAll(filepath.Join(repoDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(repoDir, "config", "secrets")
	want := []byte("synthetic fixture bytes\nline two\n")
	if err := os.WriteFile(sourcePath, want, 0600); err != nil {
		t.Fatal(err)
	}

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
	rootCmd.SetArgs([]string{"open", "main"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("open main failed: %v\nOutput: %s", err, buf.String())
	}
	got, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("reading synthetic source after open: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("canonical source changed: got %q, want %q", got, want)
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		t.Fatalf("lstat synthetic source: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("canonical source mode = %v, want regular file", info.Mode())
	}
	if !tmuxCommandSeen(runner, "new-session") {
		t.Fatalf("expected absent canonical workspace to be recreated, got %v", runner.commands)
	}
}

func TestOpenCmd_MainRecreationDoesNotAlterOrCreateGeneratedLocals(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
env_files:
  - config/runtime
env:
  BRANCH_HASH: "branch-hash-{{branch}}"
tmux:
  mode: session
`
	repoDir := setupCreateTestRepo(t, groveYML)
	if err := os.MkdirAll(filepath.Join(repoDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(repoDir, "config", "runtime")
	localPath := filepath.Join(repoDir, "config", "runtime.local")
	runtimeWant := []byte("RUNTIME=canonical\n")
	localWant := []byte("user local bytes\n")
	if err := os.WriteFile(runtimePath, runtimeWant, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, localWant, 0600); err != nil {
		t.Fatal(err)
	}
	missingLocal := filepath.Join(repoDir, "config", "missing.local")

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
	rootCmd.SetArgs([]string{"open", "main"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("open main failed: %v\nOutput: %s", err, buf.String())
	}
	gotRuntime, _ := os.ReadFile(runtimePath)
	gotLocal, _ := os.ReadFile(localPath)
	if !bytes.Equal(gotRuntime, runtimeWant) || !bytes.Equal(gotLocal, localWant) {
		t.Fatalf("main env files changed: runtime=%q local=%q", gotRuntime, gotLocal)
	}
	if strings.Contains(string(gotLocal), "branch-hash-main") {
		t.Fatalf("generated branch value appeared in canonical local: %q", gotLocal)
	}
	if _, err := os.Lstat(missingLocal); !os.IsNotExist(err) {
		t.Fatalf("missing local existence error = %v, want missing", err)
	}
}

func TestOpenCmd_MainNewWindowDoesNotCreateGeneratedLocals(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
env_files:
  - config/runtime
env:
  BRANCH_HASH: "branch-hash-{{branch}}"
tmux:
  mode: window
`
	repoDir := setupCreateTestRepo(t, groveYML)
	if err := os.MkdirAll(filepath.Join(repoDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "config", "runtime"), []byte("RUNTIME=canonical\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localPath := filepath.Join(repoDir, "config", "runtime.local")

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
	rootCmd.SetArgs([]string{"open", "main", "--new-window"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("open main --new-window failed: %v\nOutput: %s", err, buf.String())
	}
	if _, err := os.Lstat(localPath); !os.IsNotExist(err) {
		t.Fatalf("generated local existence error = %v, want missing", err)
	}
}

func TestOpenCmd_EnvCollisionFailsClosedBeforeTmux(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
env_files:
  - config/secrets
tmux:
  mode: session
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/env-collision")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/env-collision")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/env-collision")

	for _, root := range []string{repoDir, wtPath} {
		if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repoDir, "config", "secrets"), []byte("canonical fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(wtPath, "config", "secrets")
	wantDestination := []byte("user-managed fixture\n")
	if err := os.WriteFile(destination, wantDestination, 0600); err != nil {
		t.Fatal(err)
	}

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
	rootCmd.SetArgs([]string{"open", "feat/env-collision"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected open to reject a regular env destination")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("expected actionable regular-file collision error, got %v", err)
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || !bytes.Equal(got, wantDestination) {
		t.Fatalf("destination changed: got %q err=%v", got, readErr)
	}
	if tmuxCommandSeen(runner, "new-session") || tmuxCommandSeen(runner, "new-window") {
		t.Fatalf("unsafe env sync must fail before workspace launch, got %v", runner.commands)
	}
}

func TestOpenCmd_NewWindowEnvCollisionFailsBeforeCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
env_files:
  - config/secrets
tmux:
  mode: window
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/new-window-collision")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/new-window-collision")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/new-window-collision")
	for _, root := range []string{repoDir, wtPath} {
		if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repoDir, "config", "secrets"), []byte("canonical fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(wtPath, "config", "secrets")
	wantDestination := []byte("user fixture\n")
	if err := os.WriteFile(destination, wantDestination, 0600); err != nil {
		t.Fatal(err)
	}

	mockWorkingDir(t, repoDir)
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")
	runner := &recordingTmuxRunner{
		currentSession: "dev",
		outputs: map[string]string{
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}":                                 "",
			"list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}": "@9\tdev\trenamed\t" + repoDir + "\tfeat/new-window-collision\t" + wtPath + "\tcanonical",
		},
	}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"open", "feat/new-window-collision", "--new-window"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected --new-window to reject unsafe env synchronization")
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || !bytes.Equal(got, wantDestination) {
		t.Fatalf("destination changed: got %q err=%v", got, readErr)
	}
	if tmuxCommandSeen(runner, "new-window") {
		t.Fatalf("extra window created despite unsafe env sync: %v", runner.commands)
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
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}":                                 "",
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
			"list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}":                                 "grove-session\t" + repoDir + "\tfeat/open-auto-json\t" + wtPath + "\tcanonical",
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
