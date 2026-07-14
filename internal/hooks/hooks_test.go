package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestRunPostCreate_EnvVarsAndCwd(t *testing.T) {
	projectRoot := t.TempDir()
	worktreePath := t.TempDir()
	outFile := filepath.Join(worktreePath, "hook-output.txt")

	writeScript(t, filepath.Join(projectRoot, "scripts", "test.sh"),
		"#!/bin/bash\nenv | grep GROVE_ | sort > "+outFile+"\npwd >> "+outFile+"\n")

	opts := RunOpts{
		Branch:       "feat/test",
		WorktreePath: worktreePath,
		ProjectRoot:  projectRoot,
		Ports:        map[string]int{"api": 4042, "web": 3042},
	}

	warnings := RunPostCreate([]string{"scripts/test.sh"}, opts)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("script output not found: %v", err)
	}
	output := string(data)

	for _, want := range []string{
		"GROVE_BRANCH=feat/test",
		"GROVE_PORT_API=4042",
		"GROVE_PORT_WEB=3042",
		"GROVE_PROJECT_ROOT=" + projectRoot,
		"GROVE_WORKTREE=" + worktreePath,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in output:\n%s", want, output)
		}
	}

	// Verify cwd is worktree path
	if !strings.Contains(output, worktreePath) {
		t.Errorf("expected cwd %s in output:\n%s", worktreePath, output)
	}
}

func TestRunPostCreate_ScriptNotFound(t *testing.T) {
	opts := RunOpts{
		ProjectRoot:  t.TempDir(),
		WorktreePath: t.TempDir(),
		Ports:        map[string]int{},
	}

	warnings := RunPostCreate([]string{"scripts/nonexistent.sh"}, opts)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "not found") {
		t.Errorf("expected 'not found' warning, got: %s", warnings[0])
	}
}

func TestRunPostCreate_ScriptFails(t *testing.T) {
	projectRoot := t.TempDir()
	writeScript(t, filepath.Join(projectRoot, "fail.sh"), "#!/bin/bash\nexit 1\n")

	opts := RunOpts{
		ProjectRoot:  projectRoot,
		WorktreePath: t.TempDir(),
		Ports:        map[string]int{},
	}

	warnings := RunPostCreate([]string{"fail.sh"}, opts)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "failed") {
		t.Errorf("expected 'failed' warning, got: %s", warnings[0])
	}
}

func TestRunPreDelete_EnvVarsAndCwd(t *testing.T) {
	projectRoot := t.TempDir()
	worktreePath := t.TempDir()
	outFile := filepath.Join(projectRoot, "pre-delete-output.txt")

	writeScript(t, filepath.Join(projectRoot, "scripts", "pre-delete.sh"),
		"#!/bin/bash\nenv | grep GROVE_ | sort > "+outFile+"\npwd >> "+outFile+"\n")

	opts := RunOpts{
		Branch:       "feat/delete",
		WorktreePath: worktreePath,
		ProjectRoot:  projectRoot,
		Ports:        map[string]int{"api": 4042},
	}

	if err := RunPreDelete([]string{"scripts/pre-delete.sh"}, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("script output not found: %v", err)
	}
	output := string(data)
	for _, want := range []string{
		"GROVE_BRANCH=feat/delete",
		"GROVE_PORT_API=4042",
		"GROVE_PROJECT_ROOT=" + projectRoot,
		"GROVE_WORKTREE=" + worktreePath,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in output:\n%s", want, output)
		}
	}
	if !strings.Contains(output, worktreePath) {
		t.Errorf("expected cwd %s in output:\n%s", worktreePath, output)
	}
}

func TestRunPreDelete_ScriptNotFoundErrors(t *testing.T) {
	opts := RunOpts{
		ProjectRoot:  t.TempDir(),
		WorktreePath: t.TempDir(),
		Ports:        map[string]int{},
	}

	err := RunPreDelete([]string{"scripts/nonexistent.sh"}, opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestRunPreDelete_ScriptFails(t *testing.T) {
	projectRoot := t.TempDir()
	writeScript(t, filepath.Join(projectRoot, "fail.sh"), "#!/bin/bash\nexit 1\n")

	opts := RunOpts{
		ProjectRoot:  projectRoot,
		WorktreePath: t.TempDir(),
		Ports:        map[string]int{},
	}

	err := RunPreDelete([]string{"fail.sh"}, opts)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "hooks.pre_delete") || !strings.Contains(err.Error(), "failed") {
		t.Errorf("expected pre_delete failure, got: %v", err)
	}
}

func TestRunPostCreate_EmptyList(t *testing.T) {
	warnings := RunPostCreate(nil, RunOpts{})
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for empty list, got %v", warnings)
	}
}

func TestRunPostCreate_SequentialOrder(t *testing.T) {
	projectRoot := t.TempDir()
	worktreePath := t.TempDir()
	outFile := filepath.Join(worktreePath, "order.txt")

	writeScript(t, filepath.Join(projectRoot, "first.sh"),
		"#!/bin/bash\necho first > "+outFile+"\n")
	writeScript(t, filepath.Join(projectRoot, "second.sh"),
		"#!/bin/bash\necho second >> "+outFile+"\n")

	opts := RunOpts{
		ProjectRoot:  projectRoot,
		WorktreePath: worktreePath,
		Ports:        map[string]int{},
	}

	warnings := RunPostCreate([]string{"first.sh", "second.sh"}, opts)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	data, _ := os.ReadFile(outFile)
	lines := strings.TrimSpace(string(data))
	if lines != "first\nsecond" {
		t.Errorf("expected ordered output, got: %q", lines)
	}
}

func TestRunPostCreate_ContinuesAfterFailure(t *testing.T) {
	projectRoot := t.TempDir()
	worktreePath := t.TempDir()
	markerFile := filepath.Join(worktreePath, "ran.txt")

	writeScript(t, filepath.Join(projectRoot, "fail.sh"), "#!/bin/bash\nexit 1\n")
	writeScript(t, filepath.Join(projectRoot, "ok.sh"),
		"#!/bin/bash\necho ok > "+markerFile+"\n")

	opts := RunOpts{
		ProjectRoot:  projectRoot,
		WorktreePath: worktreePath,
		Ports:        map[string]int{},
	}

	warnings := RunPostCreate([]string{"fail.sh", "ok.sh"}, opts)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}

	// Second script should still run
	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		t.Error("expected second script to run after first script failed")
	}
}

func TestRunPreDelete_OutputSummaryRedactsChildOutput(t *testing.T) {
	projectRoot := t.TempDir()
	writeScript(t, filepath.Join(projectRoot, "leak.sh"), "#!/bin/bash\necho secret-payload >&2\nexit 1\n")
	err := RunPreDelete([]string{"leak.sh"}, RunOpts{ProjectRoot: projectRoot, WorktreePath: t.TempDir()})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-payload") {
		t.Fatalf("child output leaked: %v", err)
	}
}

func TestRunPreDelete_OutputStreamIsExplicit(t *testing.T) {
	projectRoot := t.TempDir()
	worktreePath := t.TempDir()
	writeScript(t, filepath.Join(projectRoot, "stream.sh"), "#!/bin/sh\nprintf visible-output\n")
	var stdout bytes.Buffer
	if err := RunPreDelete([]string{"stream.sh"}, RunOpts{
		ProjectRoot:  projectRoot,
		WorktreePath: worktreePath,
		OutputMode:   OutputStream,
		Stdout:       &stdout,
	}); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "visible-output" {
		t.Fatalf("stream output = %q", stdout.String())
	}
}

func TestRunPreDelete_Timeout(t *testing.T) {
	projectRoot := t.TempDir()
	writeScript(t, filepath.Join(projectRoot, "slow.sh"), "#!/bin/bash\nsleep 1\n")
	err := RunPreDelete([]string{"slow.sh"}, RunOpts{ProjectRoot: projectRoot, WorktreePath: t.TempDir(), Timeout: 10 * time.Millisecond})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestRunPreDelete_TimeoutKillsDescendantProcesses(t *testing.T) {
	projectRoot := t.TempDir()
	worktreePath := t.TempDir()
	marker := filepath.Join(worktreePath, "descendant-survived")
	script := "#!/bin/sh\n(sleep 0.2; printf survived > \"" + marker + "\") &\nwait\n"
	writeScript(t, filepath.Join(projectRoot, "spawn.sh"), script)

	err := RunPreDelete([]string{"spawn.sh"}, RunOpts{
		ProjectRoot:  projectRoot,
		WorktreePath: worktreePath,
		Timeout:      20 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("hook descendant survived timeout: %v", err)
	}
}

func TestRunPreDelete_RejectsSymlinkEscape(t *testing.T) {
	projectRoot := t.TempDir()
	outside := t.TempDir()
	writeScript(t, filepath.Join(outside, "evil.sh"), "#!/bin/bash\nexit 0\n")
	if err := os.Symlink(filepath.Join(outside, "evil.sh"), filepath.Join(projectRoot, "evil.sh")); err != nil {
		t.Fatal(err)
	}
	err := RunPreDelete([]string{"evil.sh"}, RunOpts{ProjectRoot: projectRoot, WorktreePath: t.TempDir()})
	if err == nil {
		t.Fatal("expected symlink escape error")
	}
}

func TestRunPreDelete_EnvAllowlistAndPassthrough(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "nope")
	t.Setenv("GROVE_SECRET_TOKEN", "also-nope")
	t.Setenv("EXTRA_OK", "yes")
	projectRoot := t.TempDir()
	worktreePath := t.TempDir()
	outFile := filepath.Join(worktreePath, "env.txt")
	writeScript(t, filepath.Join(projectRoot, "env.sh"), "#!/bin/bash\nenv | sort > "+outFile+"\n")
	err := RunPreDelete([]string{"env.sh"}, RunOpts{ProjectRoot: projectRoot, WorktreePath: worktreePath, EnvPassthrough: []string{"EXTRA_OK"}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "EXTRA_OK=yes") {
		t.Fatalf("missing passthrough env: %s", out)
	}
	if strings.Contains(out, "SECRET_TOKEN=nope") || strings.Contains(out, "GROVE_SECRET_TOKEN=also-nope") {
		t.Fatalf("unexpected env leak: %s", out)
	}
}
