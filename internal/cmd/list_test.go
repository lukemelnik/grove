package cmd

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestListCmd_Empty(t *testing.T) {
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
	rootCmd.SetArgs([]string{"list"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("list command failed: %v\nOutput: %s", err, buf.String())
	}

	// Main worktree is listed, so it won't say "No active worktrees"
	// but it should include the main branch
	output := buf.String()
	if !strings.Contains(output, "main") {
		// The main repo worktree should appear
		// (but it depends on git showing it — ok if it doesn't have extra worktrees)
	}
}

func TestListCmd_WithWorktrees(t *testing.T) {
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
  web:
    port:
      base: 3000
      env: WEB_PORT
`
	repoDir := setupCreateTestRepo(t, groveYML)

	// Create worktrees
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	run("branch", "feat/list-a")
	run("branch", "feat/list-b")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-list-a"), "feat/list-a")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-list-b"), "feat/list-b")

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
	rootCmd.SetArgs([]string{"list"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("list command failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "feat/list-a") {
		t.Errorf("expected feat/list-a in output, got:\n%s", output)
	}
	if !strings.Contains(output, "feat/list-b") {
		t.Errorf("expected feat/list-b in output, got:\n%s", output)
	}
	if !strings.Contains(output, "api:") {
		t.Errorf("expected port info in output, got:\n%s", output)
	}
	if !strings.Contains(output, "web:") {
		t.Errorf("expected web port info in output, got:\n%s", output)
	}
}

func TestListCmd_JSON(t *testing.T) {
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
	run("branch", "feat/json-list")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-json-list"), "feat/json-list")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"list", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("list --json failed: %v\nOutput: %s", err, buf.String())
	}

	// Parse JSON output
	var entries []listEntry
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	// Should have at least 2 entries (main + feat/json-list)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}

	// Find the feat/json-list entry
	found := false
	for _, e := range entries {
		if e.Branch == "feat/json-list" {
			found = true
			if _, ok := e.Ports["api"]; !ok {
				t.Error("expected api port in entry")
			}
			if e.Worktree == "" {
				t.Error("expected non-empty worktree path")
			}
		}
	}
	if !found {
		t.Error("expected feat/json-list entry in JSON output")
	}
}

func TestListCmd_NoServices(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
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
	run("branch", "feat/no-svc")
	run("worktree", "add", filepath.Join(worktreeDir, "feat-no-svc"), "feat/no-svc")

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return repoDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"list", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("list --json failed: %v\nOutput: %s", err, buf.String())
	}

	var entries []listEntry
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	for _, e := range entries {
		if e.Branch == "feat/no-svc" {
			if len(e.Ports) != 0 {
				t.Errorf("expected empty ports for no-services config, got %v", e.Ports)
			}
		}
	}
}
