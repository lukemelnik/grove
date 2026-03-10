package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"grove/internal/tmux"
)

// setupCreateTestRepo creates a temporary git repo with an initial commit,
// a .grove.yml config, and returns the repo dir and cleanup.
func setupCreateTestRepo(t *testing.T, groveYML string) (repoDir string) {
	t.Helper()

	repoDir = t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@grove.test")
	run("config", "user.name", "Grove Test")

	// Write .grove.yml
	if err := os.WriteFile(filepath.Join(repoDir, ".grove.yml"), []byte(groveYML), 0644); err != nil {
		t.Fatal(err)
	}

	// Create initial commit
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")

	return repoDir
}

func TestCreateCmd_TextOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port: 4000
    env: PORT
  web:
    port: 3000
    env: WEB_PORT
env:
  VITE_API_URL: "http://localhost:{{api.port}}"
`
	repoDir := setupCreateTestRepo(t, groveYML)

	// Create a branch
	cmd := exec.Command("git", "branch", "feat/test-create")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %s: %v", string(out), err)
	}

	// Override getWorkingDir to point to the test repo
	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()
	mockTerminal(t)

	// Run the create command
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"create", "feat/test-create", "--no-tmux"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()

	// Verify output contains key information
	if !strings.Contains(output, "feat/test-create") {
		t.Errorf("output should contain branch name, got:\n%s", output)
	}
	if !strings.Contains(output, "Worktree:") {
		t.Errorf("output should contain Worktree: line, got:\n%s", output)
	}
	if !strings.Contains(output, "Ports:") {
		t.Errorf("output should contain Ports: section, got:\n%s", output)
	}
	if !strings.Contains(output, "api:") {
		t.Errorf("output should contain api port, got:\n%s", output)
	}
	if !strings.Contains(output, "web:") {
		t.Errorf("output should contain web port, got:\n%s", output)
	}
	if !strings.Contains(output, "VITE_API_URL=") {
		t.Errorf("output should contain VITE_API_URL, got:\n%s", output)
	}

	// Verify worktree directory was created
	expectedPath := filepath.Join(worktreeDir, "feat-test-create")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("worktree directory should exist at %s", expectedPath)
	}
}

func TestCreateCmd_JSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port: 4000
    env: PORT
  web:
    port: 3000
    env: WEB_PORT
env:
  VITE_API_URL: "http://localhost:{{api.port}}"
`
	repoDir := setupCreateTestRepo(t, groveYML)

	// Create a branch
	gitCmd := exec.Command("git", "branch", "feat/json-test")
	gitCmd.Dir = repoDir
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %s: %v", string(out), err)
	}

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"create", "feat/json-test", "--json", "--no-tmux"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\nOutput: %s", err, buf.String())
	}

	// Parse JSON output
	var result createOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nRaw: %s", err, buf.String())
	}

	if result.Branch != "feat/json-test" {
		t.Errorf("expected branch=feat/json-test, got %s", result.Branch)
	}
	if result.Worktree == "" {
		t.Error("expected non-empty worktree path")
	}
	if _, ok := result.Ports["api"]; !ok {
		t.Error("expected api port in output")
	}
	if _, ok := result.Ports["web"]; !ok {
		t.Error("expected web port in output")
	}
	if _, ok := result.Env["PORT"]; !ok {
		t.Error("expected PORT in env output")
	}
	if _, ok := result.Env["WEB_PORT"]; !ok {
		t.Error("expected WEB_PORT in env output")
	}
	if v, ok := result.Env["VITE_API_URL"]; !ok {
		t.Error("expected VITE_API_URL in env output")
	} else if !strings.HasPrefix(v, "http://localhost:") {
		t.Errorf("VITE_API_URL should start with http://localhost:, got %s", v)
	}
}

func TestCreateCmd_EnvOverrides(t *testing.T) {
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

	gitCmd := exec.Command("git", "branch", "feat/env-override")
	gitCmd.Dir = repoDir
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %s: %v", string(out), err)
	}

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"create", "feat/env-override", "--json", "--no-tmux", "-e", "CUSTOM_VAR=hello", "-e", "ANOTHER=world"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\nOutput: %s", err, buf.String())
	}

	var result createOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if v, ok := result.Env["CUSTOM_VAR"]; !ok || v != "hello" {
		t.Errorf("expected CUSTOM_VAR=hello, got %q", v)
	}
	if v, ok := result.Env["ANOTHER"]; !ok || v != "world" {
		t.Errorf("expected ANOTHER=world, got %q", v)
	}
}

func TestCreateCmd_NewBranchFromRef(t *testing.T) {
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

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Create a new branch (doesn't exist) with --from main
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"create", "feat/from-ref-test", "--json", "--no-tmux", "--from", "main"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\nOutput: %s", err, buf.String())
	}

	var result createOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if result.Branch != "feat/from-ref-test" {
		t.Errorf("expected branch=feat/from-ref-test, got %s", result.Branch)
	}
	if result.Worktree == "" {
		t.Error("expected non-empty worktree path")
	}
}

func TestCreateCmd_ReuseExistingWorktree(t *testing.T) {
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

	gitCmd := exec.Command("git", "branch", "feat/reuse-cmd")
	gitCmd.Dir = repoDir
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %s: %v", string(out), err)
	}

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()
	mockTerminal(t)

	// First create
	rootCmd1 := NewRootCmd()
	var buf1 bytes.Buffer
	rootCmd1.SetOut(&buf1)
	rootCmd1.SetErr(&buf1)
	rootCmd1.SetArgs([]string{"create", "feat/reuse-cmd", "--no-tmux"})

	if err := rootCmd1.Execute(); err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	if !strings.Contains(buf1.String(), "Created worktree") {
		t.Errorf("first create should say Created, got:\n%s", buf1.String())
	}

	// Second create (should reuse)
	rootCmd2 := NewRootCmd()
	var buf2 bytes.Buffer
	rootCmd2.SetOut(&buf2)
	rootCmd2.SetErr(&buf2)
	rootCmd2.SetArgs([]string{"create", "feat/reuse-cmd", "--no-tmux"})

	if err := rootCmd2.Execute(); err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	if !strings.Contains(buf2.String(), "Reusing existing worktree") {
		t.Errorf("second create should say Reusing, got:\n%s", buf2.String())
	}
}

func TestCreateCmd_MinimalConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	// Minimal config with no services
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	gitCmd := exec.Command("git", "branch", "feat/minimal")
	gitCmd.Dir = repoDir
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %s: %v", string(out), err)
	}

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"create", "feat/minimal", "--json", "--no-tmux"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\nOutput: %s", err, buf.String())
	}

	var result createOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if result.Branch != "feat/minimal" {
		t.Errorf("expected branch=feat/minimal, got %s", result.Branch)
	}
	if len(result.Ports) != 0 {
		t.Errorf("expected empty ports for no-services config, got %v", result.Ports)
	}
}

func TestCreateCmd_MissingBranchArg(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"create"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing branch argument")
	}
}

func TestCreateCmd_JSONOutputStillSetsUpTmux(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: session
`
	repoDir := setupCreateTestRepo(t, groveYML)

	gitCmd := exec.Command("git", "branch", "feat/json-tmux")
	gitCmd.Dir = repoDir
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %s: %v", string(out), err)
	}

	mockWorkingDir(t, repoDir)

	runner := &recordingTmuxRunner{}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"create", "feat/json-tmux", "--json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("create --json failed: %v\nOutput: %s", err, buf.String())
	}

	var result createOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nRaw: %s", err, buf.String())
	}
	if result.Branch != "feat/json-tmux" {
		t.Fatalf("expected branch feat/json-tmux, got %s", result.Branch)
	}
	if !tmuxCommandSeen(runner, "new-session") {
		t.Error("expected tmux session creation in JSON mode")
	}
	if !tmuxCommandSeen(runner, "attach") && !tmuxCommandSeen(runner, "switch-client") {
		t.Error("expected explicit JSON mode to preserve attach behavior")
	}
}

func TestCreateCmd_AutoJSONSkipsAttachButSetsUpTmux(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
tmux:
  mode: session
`
	repoDir := setupCreateTestRepo(t, groveYML)

	gitCmd := exec.Command("git", "branch", "feat/auto-json")
	gitCmd.Dir = repoDir
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch failed: %s: %v", string(out), err)
	}

	mockWorkingDir(t, repoDir)

	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	runner := &recordingTmuxRunner{}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	rootCmd.SetOut(&outBuf)
	rootCmd.SetErr(&errBuf)
	rootCmd.SetArgs([]string{"create", "feat/auto-json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("create auto-JSON failed: %v\nstdout: %s\nstderr: %s", err, outBuf.String(), errBuf.String())
	}

	var result createOutput
	if err := json.Unmarshal(outBuf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse auto-JSON output: %v\nRaw: %s", err, outBuf.String())
	}
	if result.Branch != "feat/auto-json" {
		t.Fatalf("expected branch feat/auto-json, got %s", result.Branch)
	}
	if !tmuxCommandSeen(runner, "new-session") {
		t.Error("expected tmux session creation in auto-JSON mode")
	}
	if tmuxCommandSeen(runner, "attach") || tmuxCommandSeen(runner, "switch-client") {
		t.Error("auto-JSON mode should not attach unless explicitly requested")
	}
}

func tmuxCommandSeen(runner *recordingTmuxRunner, name string) bool {
	for _, cmd := range runner.commands {
		if len(cmd) > 0 && cmd[0] == name {
			return true
		}
	}
	return false
}
