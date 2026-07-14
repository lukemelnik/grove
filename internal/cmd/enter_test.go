package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/worktree"
)

func TestEnterCmd_MainWorktreePreservesCanonicalEnvSource(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
env_files:
  - config/secrets
`
	repoDir := setupCreateTestRepo(t, groveYML)
	if err := os.MkdirAll(filepath.Join(repoDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(repoDir, "config", "secrets")
	want := []byte("synthetic enter fixture\n")
	if err := os.WriteFile(sourcePath, want, 0600); err != nil {
		t.Fatal(err)
	}

	mockWorkingDir(t, repoDir)
	launched := false
	origLauncher := shellLauncher
	shellLauncher = func(opts shellLaunchOptions) error {
		launched = true
		gotDir, gotErr := filepath.EvalSymlinks(opts.Dir)
		wantDir, wantErr := filepath.EvalSymlinks(repoDir)
		if gotErr != nil || wantErr != nil || gotDir != wantDir {
			t.Fatalf("shell dir = %q (%v), want main worktree %q (%v)", gotDir, gotErr, wantDir, wantErr)
		}
		return nil
	}
	t.Cleanup(func() { shellLauncher = origLauncher })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"enter", "main"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("enter main failed: %v\nOutput: %s", err, buf.String())
	}
	if !launched {
		t.Fatal("expected shell launch after safe main-worktree sync")
	}
	got, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("reading synthetic source after enter: %v", err)
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
}

func TestEnterCmd_MainDoesNotAlterOrCreateGeneratedLocals(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
env_files:
  - config/runtime
env:
  BRANCH_HASH: "branch-hash-{{branch}}"
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
	origLauncher := shellLauncher
	shellLauncher = func(shellLaunchOptions) error { return nil }
	t.Cleanup(func() { shellLauncher = origLauncher })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"enter", "main"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("enter main failed: %v\nOutput: %s", err, buf.String())
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

func TestEnterCmd_EnvCollisionDoesNotLaunchShell(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
env_files:
  - config/secrets
`
	repoDir := setupCreateTestRepo(t, groveYML)
	gitRun(t, repoDir, "branch", "feat/enter-env-collision")
	wtPath := worktree.WorktreePath(repoDir, worktreeDir, "feat/enter-env-collision")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/enter-env-collision")

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
	launched := false
	origLauncher := shellLauncher
	shellLauncher = func(shellLaunchOptions) error {
		launched = true
		return nil
	}
	t.Cleanup(func() { shellLauncher = origLauncher })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"enter", "feat/enter-env-collision"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected enter to reject a regular env destination")
	}
	if launched {
		t.Fatal("shell launched despite unsafe env synchronization")
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || !bytes.Equal(got, wantDestination) {
		t.Fatalf("destination changed: got %q err=%v", got, readErr)
	}
}

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
