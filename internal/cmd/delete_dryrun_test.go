package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/projectstate"
	"github.com/lukemelnik/grove/internal/tmux"
)

func TestDeleteCmd_DryRunDoesNotMutateOrCallTmux(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	repoDir := setupCreateTestRepo(t, `worktree_dir: `+worktreeDir+"\n")
	gitRun(t, repoDir, "branch", "feat/delete-dry-run")
	wtPath := filepath.Join(worktreeDir, "feat-delete-dry-run")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/delete-dry-run")

	mockWorkingDir(t, repoDir)
	origGhAvailable := ghAvailable
	origGhRunner := ghCommandRunner
	ghCalled := false
	ghAvailable = func() bool { return true }
	ghCommandRunner = func(...string) (string, error) {
		ghCalled = true
		return "[]", nil
	}
	t.Cleanup(func() {
		ghAvailable = origGhAvailable
		ghCommandRunner = origGhRunner
	})

	runner := &recordingTmuxRunner{}
	origFactory := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return runner }
	t.Cleanup(func() { tmuxRunnerFactory = origFactory })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"delete", "feat/delete-dry-run", "--dry-run"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v\nOutput: %s", err, buf.String())
	}
	if ghCalled {
		t.Fatal("dry-run should not query pull requests")
	}
	if len(runner.commands) != 0 {
		t.Fatalf("dry-run should not call tmux, got %v", runner.commands)
	}
	labelPath, err := filepath.EvalSymlinks(wtPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"branch": "feat/delete-dry-run"`) || !strings.Contains(buf.String(), labelPath) {
		t.Fatalf("expected dry-run plan with exact worktree, got:\n%s", buf.String())
	}
	gitRun(t, repoDir, "rev-parse", "--verify", "refs/heads/feat/delete-dry-run")
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree should still exist after dry-run: %v", err)
	}
	commonDirCmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	commonDirCmd.Dir = repoDir
	commonDirOutput, err := commonDirCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	commonDir := strings.TrimSpace(string(commonDirOutput))
	if _, err := os.Stat(ports.StorePath(commonDir)); !os.IsNotExist(err) {
		t.Fatalf("dry-run created port registry state: %v", err)
	}
	if _, err := os.Stat(projectstate.LockPath(commonDir)); !os.IsNotExist(err) {
		t.Fatalf("dry-run created mutation lock state: %v", err)
	}
}
