package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"
)

// mockTerminal overrides isTerminal to simulate a TTY for text-mode tests.
func mockTerminal(t *testing.T) {
	t.Helper()
	orig := isTerminal
	isTerminal = func(int) bool { return true }
	t.Cleanup(func() { isTerminal = orig })
}

// mockWorkingDir overrides getWorkingDir to return the given directory.
func mockWorkingDir(t *testing.T, dir string) {
	t.Helper()
	orig := getWorkingDir
	getWorkingDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { getWorkingDir = orig })
}

// mockTmuxRunner overrides tmuxRunnerFactory with a no-op runner.
func mockTmuxRunner(t *testing.T) {
	t.Helper()
	orig := tmuxRunnerFactory
	tmuxRunnerFactory = func() tmux.Runner { return &noopTmuxRunner{} }
	t.Cleanup(func() { tmuxRunnerFactory = orig })
}

// gitRun is a helper to run git commands in a directory, fataling on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
	}
}

type cleanMockGitRunner struct {
	responses map[string]struct {
		output string
		err    error
	}
}

func newCleanMockGitRunner() *cleanMockGitRunner {
	return &cleanMockGitRunner{
		responses: make(map[string]struct {
			output string
			err    error
		}),
	}
}

func (m *cleanMockGitRunner) On(args string, output string, err error) {
	m.responses[args] = struct {
		output string
		err    error
	}{output: output, err: err}
}

func (m *cleanMockGitRunner) Run(args ...string) (string, error) {
	key := strings.Join(args, " ")
	if resp, ok := m.responses[key]; ok {
		return resp.output, resp.err
	}
	return "", fmt.Errorf("unexpected git command: %s", key)
}

func TestCleanCmd_DryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	// Create a branch at the same commit as main (so it's merged), then create a worktree
	gitRun(t, repoDir, "branch", "feat/merged-branch")
	gitRun(t, repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-merged-branch"), "feat/merged-branch")

	mockTerminal(t)
	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean", "--dry-run"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean --dry-run failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "feat/merged-branch") {
		t.Errorf("expected merged branch in dry-run output, got:\n%s", output)
	}
	if !strings.Contains(output, "unchanged") {
		t.Errorf("expected 'unchanged' reason in output, got:\n%s", output)
	}
	// Dry run should NOT clean anything
	if strings.Contains(output, "Cleaned") {
		t.Errorf("dry-run should not clean anything, got:\n%s", output)
	}
}

func TestCleanCmd_ForceClean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	gitRun(t, repoDir, "branch", "feat/to-clean")
	gitRun(t, repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-to-clean"), "feat/to-clean")

	mockTerminal(t)
	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean", "--force"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean --force failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "Cleaned feat/to-clean") {
		t.Errorf("expected 'Cleaned feat/to-clean' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Pruned worktree metadata") {
		t.Errorf("expected 'Pruned worktree metadata' in output, got:\n%s", output)
	}

	// Verify the branch was deleted
	gitCmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/to-clean")
	gitCmd.Dir = repoDir
	if _, err := gitCmd.CombinedOutput(); err == nil {
		t.Error("branch feat/to-clean should have been deleted")
	}
}

func TestCleanCmd_PreservesDirtyWorktreeWithoutForce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	gitRun(t, repoDir, "branch", "feat/dirty-clean")
	wtPath := filepath.Join(worktreeDir, "feat-dirty-clean")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/dirty-clean")

	dirtyFile := filepath.Join(wtPath, "README.md")
	if err := os.WriteFile(dirtyFile, []byte("# Dirty change\n"), 0644); err != nil {
		t.Fatalf("failed to dirty worktree: %v", err)
	}

	mockTerminal(t)
	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)

	origStdin := stdinReader
	stdinReader = strings.NewReader("y\n")
	t.Cleanup(func() { stdinReader = origStdin })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "Warning: could not remove worktree for feat/dirty-clean") {
		t.Errorf("expected warning about dirty worktree, got:\n%s", output)
	}
	if strings.Contains(output, "Cleaned feat/dirty-clean") {
		t.Errorf("dirty worktree should not be cleaned without --force, got:\n%s", output)
	}

	gitCmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/dirty-clean")
	gitCmd.Dir = repoDir
	if _, err := gitCmd.CombinedOutput(); err != nil {
		t.Error("branch feat/dirty-clean should still exist")
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree path should still exist: %v", err)
	}
}

func TestCleanCmd_JSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	gitRun(t, repoDir, "branch", "feat/json-clean")
	gitRun(t, repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-json-clean"), "feat/json-clean")

	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean", "--json", "--force"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean --json failed: %v\nOutput: %s", err, buf.String())
	}

	var result cleanOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if len(result.Cleaned) == 0 {
		t.Fatal("expected at least one cleaned worktree")
	}
	found := false
	for _, c := range result.Cleaned {
		if c.Branch == "feat/json-clean" {
			found = true
			if c.Reason != "unchanged" {
				t.Errorf("expected reason 'unchanged', got %q", c.Reason)
			}
		}
	}
	if !found {
		t.Error("expected feat/json-clean in cleaned list")
	}
	if !result.Pruned {
		t.Error("expected pruned to be true")
	}
}

func TestCleanCmd_NoStaleWorktrees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	mockTerminal(t)
	mockWorkingDir(t, repoDir)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean", "--dry-run"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean failed: %v\nOutput: %s", err, buf.String())
	}

	if !strings.Contains(buf.String(), "No stale worktrees found") {
		t.Errorf("expected 'No stale worktrees found', got:\n%s", buf.String())
	}
}

func TestCleanCmd_NoStaleJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	mockWorkingDir(t, repoDir)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean --json failed: %v\nOutput: %s", err, buf.String())
	}

	var result cleanOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if len(result.Cleaned) != 0 {
		t.Errorf("expected empty cleaned list, got %d entries", len(result.Cleaned))
	}
}

func TestCleanCmd_AutoJSONRequiresForce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)
	wtPath := filepath.Join(worktreeDir, "feat-auto-json")

	gitRun(t, repoDir, "branch", "feat/auto-json")
	gitRun(t, repoDir, "worktree", "add", wtPath, "feat/auto-json")

	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)

	origTerminal := isTerminal
	isTerminal = func(int) bool { return false }
	t.Cleanup(func() { isTerminal = origTerminal })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected clean to require --force in auto-JSON mode")
	}
	if !ErrorAlreadyReported(err) {
		t.Fatalf("expected structured error output, got: %v", err)
	}
	if !strings.Contains(buf.String(), "requires --force") {
		t.Fatalf("expected --force guidance, got:\n%s", buf.String())
	}

	gitCmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/auto-json")
	gitCmd.Dir = repoDir
	if _, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatal("branch should still exist after auto-JSON refusal")
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree path should still exist: %v", err)
	}
}

func TestCleanCmd_MainBranchNeverCleaned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	mockTerminal(t)
	mockWorkingDir(t, repoDir)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean", "--dry-run"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean failed: %v\nOutput: %s", err, buf.String())
	}

	// The output should not mention "main" as a stale branch
	output := buf.String()
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "main") && (strings.Contains(line, "merged") || strings.Contains(line, "unchanged")) {
			t.Error("main branch should never be listed as stale")
		}
	}
}

func TestCleanCmd_Abort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	gitRun(t, repoDir, "branch", "feat/abort-test")
	gitRun(t, repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-abort-test"), "feat/abort-test")

	mockTerminal(t)
	mockWorkingDir(t, repoDir)
	mockTmuxRunner(t)

	// Simulate user answering "n" to the confirmation prompt
	origStdin := stdinReader
	stdinReader = strings.NewReader("n\n")
	t.Cleanup(func() { stdinReader = origStdin })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "Aborted") {
		t.Errorf("expected 'Aborted' in output, got:\n%s", output)
	}
	// Branch should still exist
	gitCmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/feat/abort-test")
	gitCmd.Dir = repoDir
	if _, err := gitCmd.CombinedOutput(); err != nil {
		t.Error("branch should still exist after aborting")
	}
}

func TestCleanCmd_DryRunJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + "\n"
	repoDir := setupCreateTestRepo(t, groveYML)

	gitRun(t, repoDir, "branch", "feat/dry-json")
	gitRun(t, repoDir, "worktree", "add", filepath.Join(worktreeDir, "feat-dry-json"), "feat/dry-json")

	mockWorkingDir(t, repoDir)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"clean", "--json", "--dry-run"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("clean --json --dry-run failed: %v\nOutput: %s", err, buf.String())
	}

	var result cleanOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	// Dry run in JSON mode should list candidates but pruned should be false
	if len(result.Cleaned) == 0 {
		t.Fatal("expected stale worktrees in dry-run JSON output")
	}
	if result.Pruned {
		t.Error("dry-run should not report pruned=true")
	}
}

func TestParseGoneBranches(t *testing.T) {
	input := `  feat/active   abc1234 [origin/feat/active] some commit
  feat/gone-one def5678 [origin/feat/gone-one: gone] old commit
* main           111aaaa [origin/main] latest
  feat/gone-two  222bbbb [origin/feat/gone-two: gone] another old
  feat/tracking  333cccc [origin/feat/tracking: behind 2] behind commit
`
	got := worktree.ParseGoneBranchesForTest(input)
	expected := []string{"feat/gone-one", "feat/gone-two"}

	if len(got) != len(expected) {
		t.Fatalf("expected %d gone branches, got %d: %v", len(expected), len(got), got)
	}
	for i, branch := range expected {
		if got[i] != branch {
			t.Errorf("expected %q at index %d, got %q", branch, i, got[i])
		}
	}
}

func TestFindStaleWorktrees_SkipsGoneBranchesWithUniqueCommits(t *testing.T) {
	git := newCleanMockGitRunner()
	git.On("worktree list --porcelain",
		"worktree /project\nHEAD abc\nbranch refs/heads/main\n\n"+
			"worktree /worktrees/feat-gone\nHEAD def\nbranch refs/heads/feat/gone\n", nil)
	git.On("symbolic-ref refs/remotes/origin/HEAD", "refs/remotes/origin/main", nil)
	git.On("branch -vv",
		"  feat/gone def5678 [origin/feat/gone: gone] local only\n"+
			"* main 111aaaa [origin/main] latest\n", nil)
	git.On("branch --merged main", "* main\n", nil)
	git.On("rev-list --count main..feat/gone", "1", nil)

	mgr := worktree.NewManager(git, "/project", "/worktrees")
	stale, err := findStaleWorktrees(mgr)
	if err != nil {
		t.Fatalf("findStaleWorktrees failed: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("expected gone branch with unique commits to be skipped, got: %+v", stale)
	}
}

func TestParseMergedBranches(t *testing.T) {
	input := `* main
  feat/merged-one
  feat/merged-two
`
	got := worktree.ParseMergedBranchesForTest(input)
	expected := []string{"feat/merged-one", "feat/merged-two"}

	if len(got) != len(expected) {
		t.Fatalf("expected %d merged branches, got %d: %v", len(expected), len(got), got)
	}
	for i, branch := range expected {
		if got[i] != branch {
			t.Errorf("expected %q at index %d, got %q", branch, i, got[i])
		}
	}
}

func TestParseMergedBranches_SkipsCurrentBranch(t *testing.T) {
	input := `* main
  feat/merged
`
	got := worktree.ParseMergedBranchesForTest(input)
	for _, b := range got {
		if b == "main" {
			t.Error("current branch (marked with *) should be skipped")
		}
	}
}
