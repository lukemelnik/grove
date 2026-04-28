package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/worktree"
)

func TestEnterCmd_LaunchesShellInWorktreeWithGroveEnv(t *testing.T) {
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
env:
  API_URL: "http://localhost:{{api.port}}"
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/enter")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/enter")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/enter")

	mockWorkingDir(t, repoDir)
	t.Setenv("SHELL", "/bin/test-shell")

	var launched shellLaunchOptions
	origLauncher := shellLauncher
	shellLauncher = func(opts shellLaunchOptions) error {
		launched = opts
		return nil
	}
	t.Cleanup(func() { shellLauncher = origLauncher })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"enter", "feat/enter"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("enter failed: %v\nOutput: %s", err, buf.String())
	}
	if launched.Shell != "/bin/test-shell" {
		t.Fatalf("expected configured shell, got %q", launched.Shell)
	}
	gotDir, _ := filepath.EvalSymlinks(launched.Dir)
	wantDir, _ := filepath.EvalSymlinks(wtPath)
	if gotDir != wantDir {
		t.Fatalf("expected shell cwd %s, got %s", wtPath, launched.Dir)
	}
	if !strings.Contains(buf.String(), "type exit or Ctrl-D to return") {
		t.Fatalf("expected enter guidance, got:\n%s", buf.String())
	}

	envMap := map[string]string{}
	for _, pair := range launched.Env {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	if envMap["GROVE_BRANCH"] != "feat/enter" {
		t.Fatalf("expected GROVE_BRANCH=feat/enter, got %q", envMap["GROVE_BRANCH"])
	}
	gotWorktree, _ := filepath.EvalSymlinks(envMap["GROVE_WORKTREE"])
	if gotWorktree != wantDir {
		t.Fatalf("expected GROVE_WORKTREE=%q, got %q", wantDir, envMap["GROVE_WORKTREE"])
	}
	gotRoot, _ := filepath.EvalSymlinks(envMap["GROVE_PROJECT_ROOT"])
	wantRoot, _ := filepath.EvalSymlinks(repoDir)
	if gotRoot != wantRoot {
		t.Fatalf("expected GROVE_PROJECT_ROOT=%q, got %q", wantRoot, envMap["GROVE_PROJECT_ROOT"])
	}
	if envMap["PORT"] == "" {
		t.Fatal("expected managed service port env")
	}
	if envMap["API_URL"] == "" {
		t.Fatal("expected managed global env")
	}
}
