package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
