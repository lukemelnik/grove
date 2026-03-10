package cmd

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusCmd_InsideWorktree(t *testing.T) {
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
	run("branch", "feat/status-test")
	wtPath := filepath.Join(worktreeDir, "feat-status-test")
	run("worktree", "add", wtPath, "feat/status-test")

	// Pretend cwd is inside the worktree
	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return wtPath, nil }
	defer func() { getWorkingDir = origGetWd }()

	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return true }
	defer func() { isTerminal = origIsTerminal }()

	// We need .grove.yml discoverable from inside the worktree.
	// The worktree is a sibling directory; config discovery walks up.
	// Copy .grove.yml into the worktree for discoverability.
	// Actually, the worktree is a checkout of the repo which already has .grove.yml.
	// Let's verify it's there, otherwise skip this approach.

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"status"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("status command failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "feat/status-test") {
		t.Errorf("expected branch name in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Branch:") {
		t.Errorf("expected 'Branch:' label in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Worktree:") {
		t.Errorf("expected 'Worktree:' label in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Ports:") {
		t.Errorf("expected 'Ports:' section in output, got:\n%s", output)
	}
	if !strings.Contains(output, "api:") {
		t.Errorf("expected api port in output, got:\n%s", output)
	}
	if strings.Contains(output, "Env:") {
		t.Errorf("output should NOT contain Env section, got:\n%s", output)
	}
}

func TestStatusCmd_JSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port: 4000
    env: PORT
env:
  VITE_API_URL: "http://localhost:{{api.port}}"
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
	run("branch", "feat/status-json")
	wtPath := filepath.Join(worktreeDir, "feat-status-json")
	run("worktree", "add", wtPath, "feat/status-json")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return wtPath, nil }
	defer func() { getWorkingDir = origGetWd }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"status", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("status --json failed: %v\nOutput: %s", err, buf.String())
	}

	var result statusOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if result.Branch != "feat/status-json" {
		t.Errorf("expected branch=feat/status-json, got %s", result.Branch)
	}
	if result.Worktree == "" {
		t.Error("expected non-empty worktree path")
	}
	if _, ok := result.Ports["api"]; !ok {
		t.Error("expected api port in output")
	}
	// Env should not be present in output
	rawJSON := buf.Bytes()
	var rawMap map[string]interface{}
	if err := json.Unmarshal(rawJSON, &rawMap); err == nil {
		if _, hasEnv := rawMap["env"]; hasEnv {
			t.Error("JSON output should NOT contain env field")
		}
	}
}

func TestStatusCmd_NotInsideWorktree(t *testing.T) {
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

	// Point cwd to a random temp dir that is NOT a worktree
	randomDir := t.TempDir()
	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return randomDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// We need config discovery to work from randomDir. But it won't find .grove.yml
	// in a random temp dir. So the error will be about config not found.
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"status"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when not inside a worktree")
	}
	// Should get either "no .grove.yml found" or "not inside a grove worktree"
	_ = repoDir // used for setup only
}

func TestStatusCmd_InsideMainWorktree(t *testing.T) {
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

	// Point cwd to the main repo (which is itself a worktree for main branch)
	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return true }
	defer func() { isTerminal = origIsTerminal }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"status"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("status should work inside main worktree: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "main") {
		t.Errorf("expected 'main' branch in output, got:\n%s", output)
	}
}

func TestStatusCmd_MinimalConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"status", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("status --json failed: %v\nOutput: %s", err, buf.String())
	}

	var result statusOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if result.Branch != "main" {
		t.Errorf("expected branch=main, got %s", result.Branch)
	}
	if len(result.Ports) != 0 {
		t.Errorf("expected empty ports for no-services config, got %v", result.Ports)
	}
}
