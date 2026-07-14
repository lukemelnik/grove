package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/projectstate"
	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"
)

func TestPersistentPortAssignmentsStayStableAcrossCollidingWorktrees(t *testing.T) {
	worktreeDir := t.TempDir()
	repoDir := setupCreateTestRepo(t, fmt.Sprintf("worktree_dir: %s\nservices:\n  api:\n    port:\n      base: 4000\n      var: PORT\n", worktreeDir))

	byOffset := make(map[int]string)
	var first, second string
	for i := 0; i < 20000 && second == ""; i++ {
		branch := fmt.Sprintf("feat/collision-%d", i)
		offset := ports.HashOffset(branch, ports.DefaultMaxOffset)
		if prior := byOffset[offset]; prior != "" {
			first, second = prior, branch
		} else {
			byOffset[offset] = branch
		}
	}
	if second == "" {
		t.Fatal("could not find deterministic hash collision")
	}
	worktreePaths := make(map[string]string)
	for _, branch := range []string{first, second} {
		path := filepath.Join(worktreeDir, strings.ReplaceAll(branch, "/", "-"))
		worktreePaths[branch] = path
		gitRun(t, repoDir, "branch", branch)
		gitRun(t, repoDir, "worktree", "add", path, branch)
	}

	mockWorkingDir(t, repoDir)
	pc, err := loadProjectContext()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := acquireWorkflowLock(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assignPersistentPorts(pc, first); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}

	registry, err := ports.NewStore(pc.CommonStateDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	firstRecord := registry.Branches[first]
	secondRecord := registry.Branches[second]
	if firstRecord.Offset == secondRecord.Offset || firstRecord.Ports["api"] == secondRecord.Ports["api"] {
		t.Fatalf("colliding branches were not separated: %#v %#v", firstRecord, secondRecord)
	}

	third := "feat/added-later"
	gitRun(t, repoDir, "branch", third)
	gitRun(t, repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-added-later"), third)
	lock, err = acquireWorkflowLock(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assignPersistentPorts(pc, third); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	registry, err = ports.NewStore(pc.CommonStateDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.Branches[first]; got.Offset != firstRecord.Offset || got.Ports["api"] != firstRecord.Ports["api"] {
		t.Fatalf("first assignment changed after adding branch: before=%#v after=%#v", firstRecord, got)
	}
	if got := registry.Branches[second]; got.Offset != secondRecord.Offset || got.Ports["api"] != secondRecord.Ports["api"] {
		t.Fatalf("second assignment changed after adding branch: before=%#v after=%#v", secondRecord, got)
	}

	mockWorkingDir(t, worktreePaths[second])
	root := NewRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"status", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status failed: %v\n%s", err, output.String())
	}
	wantPort := fmt.Sprintf(`"api": %d`, secondRecord.Ports["api"])
	if !strings.Contains(output.String(), wantPort) {
		t.Fatalf("status did not report registry assignment %s: %s", wantPort, output.String())
	}
}

func TestCreateEnvFailureDoesNotRetainRegistryReservation(t *testing.T) {
	worktreeDir := t.TempDir()
	repoDir := setupCreateTestRepo(t, fmt.Sprintf("worktree_dir: %s\nservices:\n  api:\n    port:\n      base: 4000\n      var: PORT\nenv:\n  BROKEN: '{{missing.port}}'\n", worktreeDir))
	mockWorkingDir(t, repoDir)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"create", "feat/invalid-env", "--no-open", "--json"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected managed env failure; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	pc, err := loadProjectContext()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ports.NewStore(pc.CommonStateDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := registry.Branches["feat/invalid-env"]; exists {
		t.Fatalf("failed create retained registry record: %#v", registry.Branches["feat/invalid-env"])
	}
	verify := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/invalid-env")
	verify.Dir = repoDir
	if err := verify.Run(); err == nil {
		t.Fatal("failed create retained local branch")
	}
}

func TestCreateLockContentionFailsBeforeWorktreeMutation(t *testing.T) {
	worktreeDir := t.TempDir()
	repoDir := setupCreateTestRepo(t, fmt.Sprintf("worktree_dir: %s\nservices:\n  api:\n    port:\n      base: 4000\n      var: PORT\n", worktreeDir))
	mockWorkingDir(t, repoDir)
	pc, err := loadProjectContext()
	if err != nil {
		t.Fatal(err)
	}
	held, err := projectstate.AcquireLock(context.Background(), pc.CommonStateDir, projectstate.LockOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	root := NewRootCmd()
	root.SetContext(ctx)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"create", "feat/locked", "--no-open", "--json"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected lock contention failure; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `"code":"project_lock_unavailable"`) {
		t.Fatalf("missing stable lock error code: %s", stderr.String())
	}
	verify := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/locked")
	verify.Dir = repoDir
	if err := verify.Run(); err == nil {
		t.Fatal("branch was created despite lock contention")
	}
	if _, err := os.Stat(filepath.Join(worktreeDir, "feat-locked")); !os.IsNotExist(err) {
		t.Fatalf("worktree path was mutated despite lock contention: %v", err)
	}
}

func TestCreateEnvSyncFailureRollsBackNewWorktreeButKeepsExistingBranch(t *testing.T) {
	worktreeDir := t.TempDir()
	repoDir := setupCreateTestRepo(t, fmt.Sprintf("worktree_dir: %s\nenv_files: [config/runtime]\nservices:\n  api:\n    port:\n      base: 4000\n      var: PORT\n", worktreeDir))
	if err := os.MkdirAll(filepath.Join(repoDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-runtime", filepath.Join(repoDir, "config", "runtime")); err != nil {
		t.Fatal(err)
	}
	branch := "feat/env-sync-failure"
	gitRun(t, repoDir, "branch", branch)
	mockWorkingDir(t, repoDir)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"create", branch, "--no-open", "--json"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected env sync failure; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	worktreePath := worktree.WorktreePath(repoDir, worktreeDir, branch)
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("new worktree was not rolled back: %v", err)
	}
	verify := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
	verify.Dir = repoDir
	if err := verify.Run(); err != nil {
		t.Fatal("pre-existing branch was deleted during rollback")
	}
	pc, err := loadProjectContext()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ports.NewStore(pc.CommonStateDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := registry.Branches[branch]; exists {
		t.Fatal("rolled-back worktree retained a port reservation")
	}
}

func TestCreateTmuxFailureRollsBackOnlyBeforeHooksProduceFiles(t *testing.T) {
	for _, tc := range []struct {
		name       string
		withHook   bool
		wantRetain bool
	}{
		{name: "no hooks rolls back", wantRetain: false},
		{name: "post-create hook output is retained", withHook: true, wantRetain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worktreeDir := t.TempDir()
			configText := fmt.Sprintf("worktree_dir: %s\ntmux:\n  mode: session\n", worktreeDir)
			if tc.withHook {
				configText += "hooks:\n  post_create: [scripts/write-output.sh]\n"
			}
			repoDir := setupCreateTestRepo(t, configText)
			if tc.withHook {
				scriptDir := filepath.Join(repoDir, "scripts")
				if err := os.MkdirAll(scriptDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(scriptDir, "write-output.sh"), []byte("#!/bin/sh\nprintf generated > \"$GROVE_WORKTREE/hook-output\"\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			branch := "feat/tmux-failure"
			gitRun(t, repoDir, "branch", branch)
			mockWorkingDir(t, repoDir)
			worktreePath := worktree.WorktreePath(repoDir, worktreeDir, branch)
			runner := &recordingTmuxRunner{errors: map[string]error{
				strings.Join([]string{"new-session", "-d", "-s", tmux.SessionName(branch), "-c", worktreePath}, " "): fmt.Errorf("synthetic tmux failure"),
			}}
			oldFactory := tmuxRunnerFactory
			tmuxRunnerFactory = func() tmux.Runner { return runner }
			t.Cleanup(func() { tmuxRunnerFactory = oldFactory })

			root := NewRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"create", branch, "--json"})
			if err := root.Execute(); err == nil {
				t.Fatalf("expected tmux failure; stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			_, statErr := os.Stat(worktreePath)
			if tc.wantRetain {
				if statErr != nil {
					t.Fatalf("hook worktree was removed: %v", statErr)
				}
				if got, err := os.ReadFile(filepath.Join(worktreePath, "hook-output")); err != nil || string(got) != "generated" {
					t.Fatalf("hook output not retained: %q, %v", got, err)
				}
				if !strings.Contains(stderr.String(), "retained because post-create hooks") {
					t.Fatalf("retention not reported: %s", stderr.String())
				}
			} else if !os.IsNotExist(statErr) {
				t.Fatalf("new worktree was not rolled back: %v", statErr)
			}
		})
	}
}

func TestDeleteWiresHookTimeoutBeforeMutation(t *testing.T) {
	worktreeDir := t.TempDir()
	repoDir := setupCreateTestRepo(t, fmt.Sprintf("worktree_dir: %s\nhooks:\n  timeout: 20ms\n  pre_delete: [scripts/slow.sh]\n", worktreeDir))
	scriptDir := filepath.Join(repoDir, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "slow.sh"), []byte("#!/bin/sh\nsleep 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoDir, "branch", "feat/slow-delete")
	worktreePath := filepath.Join(worktreeDir, "feat-slow-delete")
	gitRun(t, repoDir, "worktree", "add", worktreePath, "feat/slow-delete")
	mockWorkingDir(t, repoDir)
	oldGhAvailable := ghAvailable
	ghAvailable = func() bool { return false }
	t.Cleanup(func() { ghAvailable = oldGhAvailable })

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"delete", "feat/slow-delete", "--force", "--json"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected hook timeout; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `"code":"delete_hook_failed"`) || strings.Contains(stderr.String(), "sleep") {
		t.Fatalf("unexpected timeout output: %s", stderr.String())
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree changed after hook timeout: %v", err)
	}
}

func TestCreateWiresHookPassthroughWithSafeOutput(t *testing.T) {
	worktreeDir := t.TempDir()
	repoDir := setupCreateTestRepo(t, fmt.Sprintf("worktree_dir: %s\nhooks:\n  timeout: 1s\n  env_passthrough: [SAFE_HOOK_VALUE]\n  post_create: [scripts/check-env.sh]\n", worktreeDir))
	scriptDir := filepath.Join(repoDir, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s|%s' \"$SAFE_HOOK_VALUE\" \"${UNPASSED_SECRET-unset}\" > \"$GROVE_PROJECT_ROOT/hook-result\"\nprintf 'SHOULD_NOT_STREAM:%s\\n' \"$SAFE_HOOK_VALUE\"\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "check-env.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SAFE_HOOK_VALUE", "allowed-value")
	t.Setenv("UNPASSED_SECRET", "must-not-pass")
	gitRun(t, repoDir, "branch", "feat/hook-env")
	mockWorkingDir(t, repoDir)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"create", "feat/hook-env", "--no-open", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("create failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	result, err := os.ReadFile(filepath.Join(repoDir, "hook-result"))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "allowed-value|unset" {
		t.Fatalf("hook environment = %q", result)
	}
	if strings.Contains(stdout.String(), "SHOULD_NOT_STREAM") || strings.Contains(stderr.String(), "SHOULD_NOT_STREAM") || strings.Contains(stdout.String(), "allowed-value") || strings.Contains(stderr.String(), "allowed-value") {
		t.Fatalf("hook output or value leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
