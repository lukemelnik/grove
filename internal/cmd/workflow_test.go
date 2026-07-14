package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/env"
)

func TestSyncWorktreeEnv_SameRootAliasSkipsAllDiskMutation(t *testing.T) {
	repoDir := t.TempDir()
	cfg := workflowEnvImmutabilityConfig()
	managed := workflowManagedEnv(t, cfg, "main")

	if err := os.MkdirAll(filepath.Join(repoDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(repoDir, "config", "runtime")
	localPath := filepath.Join(repoDir, "config", "runtime.local")
	runtimeWant := []byte("PORT=3000\n")
	localWant := []byte("user-owned local bytes\n")
	if err := os.WriteFile(runtimePath, runtimeWant, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, localWant, 0600); err != nil {
		t.Fatal(err)
	}

	aliasRoot := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repoDir, aliasRoot); err != nil {
		t.Fatal(err)
	}
	if err := syncWorktreeEnv(cfg, repoDir, aliasRoot, managed); err != nil {
		t.Fatalf("syncWorktreeEnv failed: %v", err)
	}

	assertFileBytes(t, runtimePath, runtimeWant)
	assertFileBytes(t, localPath, localWant)
	assertNotContainsFile(t, localPath, "branch-hash-main")
}

func TestSyncWorktreeEnv_AmbiguousAliasResolutionFailsClosedBeforeMutation(t *testing.T) {
	repoDir := t.TempDir()
	worktreeDir := filepath.Join(t.TempDir(), "worktree")
	if err := os.MkdirAll(filepath.Join(repoDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktreeDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := workflowEnvImmutabilityConfig()
	managed := workflowManagedEnv(t, cfg, "feat/ambiguous")

	destination := filepath.Join(worktreeDir, "config", "runtime.local")
	want := []byte("user local survives\n")
	if err := os.WriteFile(destination, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "config", "runtime"), []byte("PORT=3000\n"), 0600); err != nil {
		t.Fatal(err)
	}

	origEval := workflowEvalSymlinks
	workflowEvalSymlinks = func(string) (string, error) { return "", errors.New("synthetic alias failure") }
	t.Cleanup(func() { workflowEvalSymlinks = origEval })

	if err := syncWorktreeEnv(cfg, repoDir, worktreeDir, managed); err != nil {
		t.Fatalf("syncWorktreeEnv failed closed by skipping mutation, got error: %v", err)
	}
	assertFileBytes(t, destination, want)
	assertMissing(t, filepath.Join(worktreeDir, "config", "runtime"))
}

func TestSyncWorktreeEnv_LocalCollisionPreflightPreservesExistingSymlink(t *testing.T) {
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()
	cfg := workflowEnvImmutabilityConfig()
	managed := workflowManagedEnv(t, cfg, "feat/preflight")

	source := filepath.Join(repoDir, "config", "runtime")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("canonical\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(worktreeDir, "config", "runtime")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	oldSource := filepath.Join(worktreeDir, "config", "old-runtime")
	if err := os.WriteFile(oldSource, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("old-runtime", destination); err != nil {
		t.Fatal(err)
	}
	localPath := destination + ".local"
	localWant := []byte("user local\n")
	if err := os.WriteFile(localPath, localWant, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := syncWorktreeEnv(cfg, repoDir, worktreeDir, managed); err == nil {
		t.Fatal("expected user-owned local collision")
	}
	if target, err := os.Readlink(destination); err != nil || target != "old-runtime" {
		t.Fatalf("env symlink changed before local preflight failure: target=%q err=%v", target, err)
	}
	assertFileBytes(t, localPath, localWant)
}

func TestSyncWorktreeEnv_LinkedWorktreeStillGetsGeneratedLocals(t *testing.T) {
	repoDir := t.TempDir()
	worktreeDir := t.TempDir()
	cfg := workflowEnvImmutabilityConfig()
	managed := workflowManagedEnv(t, cfg, "feat/linked")

	if err := os.MkdirAll(filepath.Join(repoDir, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "config", "runtime"), []byte("PORT=3000\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := syncWorktreeEnv(cfg, repoDir, worktreeDir, managed); err != nil {
		t.Fatalf("syncWorktreeEnv failed: %v", err)
	}

	linkedRuntime := filepath.Join(worktreeDir, "config", "runtime")
	info, err := os.Lstat(linkedRuntime)
	if err != nil {
		t.Fatalf("expected linked runtime: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("runtime destination mode = %v, want symlink", info.Mode())
	}
	localBytes, err := os.ReadFile(filepath.Join(worktreeDir, "config", "runtime.local"))
	if err != nil {
		t.Fatalf("expected generated local: %v", err)
	}
	content := string(localBytes)
	if !strings.Contains(content, "BRANCH_HASH=branch-hash-feat/linked") || !strings.Contains(content, "PORT=") {
		t.Fatalf("generated local missing managed values: %q", content)
	}
}

func workflowEnvImmutabilityConfig() *config.Config {
	return &config.Config{
		EnvFiles: []string{"config/runtime"},
		Services: map[string]config.Service{
			"api": {Port: config.ServicePort{Base: 4100, Env: "PORT"}},
		},
		Env: map[string]string{"BRANCH_HASH": "branch-hash-{{branch}}"},
	}
}

func workflowManagedEnv(t *testing.T, cfg *config.Config, branch string) *env.ManagedEnv {
	t.Helper()
	managed, err := env.BuildManagedEnv(cfg, map[string]int{"api": 4107}, branch)
	if err != nil {
		t.Fatal(err)
	}
	return managed
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s changed: got %q, want %q", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s existence error = %v, want missing", path, err)
	}
}

func assertNotContainsFile(t *testing.T, path, needle string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if strings.Contains(string(got), needle) {
		t.Fatalf("%s contains %q: %q", path, needle, got)
	}
}
