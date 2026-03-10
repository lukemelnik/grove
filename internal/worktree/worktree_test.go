package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- Unit tests for pure functions (no git needed) ---

func TestSanitizeBranchName(t *testing.T) {
	if got := SanitizeBranchName("main"); got != "main" {
		t.Fatalf("SanitizeBranchName(main) = %q, want main", got)
	}
	if got := SanitizeBranchName("no-slashes"); got != "no-slashes" {
		t.Fatalf("SanitizeBranchName(no-slashes) = %q, want no-slashes", got)
	}
	if got := SanitizeBranchName(".."); got != "dotdot" {
		t.Fatalf("SanitizeBranchName(..) = %q, want dotdot", got)
	}
	if got := SanitizeBranchName("."); got != "dot" {
		t.Fatalf("SanitizeBranchName(.) = %q, want dot", got)
	}
	if got := SanitizeBranchName(""); got != "branch" {
		t.Fatalf("SanitizeBranchName(\"\") = %q, want branch", got)
	}

	slash := SanitizeBranchName("feat/auth")
	dash := SanitizeBranchName("feat-auth")
	if slash == dash {
		t.Fatalf("expected distinct names for feat/auth and feat-auth, got %q", slash)
	}
	if !strings.HasPrefix(slash, "feat-auth-") {
		t.Fatalf("expected feat/auth to keep a readable prefix, got %q", slash)
	}
}

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		name        string
		projectRoot string
		worktreeDir string
		branch      string
		want        string
	}{
		{
			name:        "relative worktree dir",
			projectRoot: "/home/user/project",
			worktreeDir: "../.grove-worktrees",
			branch:      "feat/auth",
			want:        filepath.Join("/home/user/.grove-worktrees", SanitizeBranchName("feat/auth")),
		},
		{
			name:        "absolute worktree dir",
			projectRoot: "/home/user/project",
			worktreeDir: "/tmp/worktrees",
			branch:      "feat/auth",
			want:        filepath.Join("/tmp/worktrees", SanitizeBranchName("feat/auth")),
		},
		{
			name:        "simple branch",
			projectRoot: "/project",
			worktreeDir: "../.grove-worktrees",
			branch:      "main",
			want:        "/.grove-worktrees/main",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WorktreePath(tt.projectRoot, tt.worktreeDir, tt.branch)
			if got != tt.want {
				t.Errorf("WorktreePath(%q, %q, %q) = %q, want %q",
					tt.projectRoot, tt.worktreeDir, tt.branch, got, tt.want)
			}
		})
	}
}

func branchPath(base, branch string) string {
	return filepath.Join(base, SanitizeBranchName(branch))
}

func TestBranchResolution_String(t *testing.T) {
	tests := []struct {
		r    BranchResolution
		want string
	}{
		{BranchLocal, "local"},
		{BranchRemoteTracking, "remote-tracking"},
		{BranchNew, "new"},
		{BranchResolution(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.r.String()
			if got != tt.want {
				t.Errorf("BranchResolution(%d).String() = %q, want %q", tt.r, got, tt.want)
			}
		})
	}
}

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []Info
	}{
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name: "single worktree",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main`,
			want: []Info{
				{Path: "/home/user/project", Branch: "main"},
			},
		},
		{
			name: "multiple worktrees",
			output: `worktree /home/user/project
HEAD abc123
branch refs/heads/main

worktree /home/user/.grove-worktrees/feat-auth
HEAD def456
branch refs/heads/feat/auth`,
			want: []Info{
				{Path: "/home/user/project", Branch: "main"},
				{Path: "/home/user/.grove-worktrees/feat-auth", Branch: "feat/auth"},
			},
		},
		{
			name: "bare repository",
			output: `worktree /home/user/project.git
HEAD abc123
bare`,
			want: []Info{
				{Path: "/home/user/project.git", IsBare: true},
			},
		},
		{
			name: "detached HEAD",
			output: `worktree /home/user/project
HEAD abc123
detached`,
			want: []Info{
				{Path: "/home/user/project"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorktreeList(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("parseWorktreeList() returned %d entries, want %d", len(got), len(tt.want))
			}
			for i, g := range got {
				w := tt.want[i]
				if g.Path != w.Path || g.Branch != w.Branch || g.IsBare != w.IsBare {
					t.Errorf("entry[%d] = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

// --- Mock git runner for unit tests ---

type mockGitRunner struct {
	// calls records all git commands that were run.
	calls [][]string
	// responses maps "arg1 arg2 ..." to (output, error).
	responses map[string]mockResponse
	// defaultResponse is returned when no matching response is found.
	defaultResponse *mockResponse
}

type mockResponse struct {
	output string
	err    error
}

func newMockGitRunner() *mockGitRunner {
	return &mockGitRunner{
		responses: make(map[string]mockResponse),
	}
}

func (m *mockGitRunner) On(args string, output string, err error) {
	m.responses[args] = mockResponse{output: output, err: err}
}

func (m *mockGitRunner) OnDefault(output string, err error) {
	m.defaultResponse = &mockResponse{output: output, err: err}
}

func (m *mockGitRunner) Run(args ...string) (string, error) {
	m.calls = append(m.calls, args)
	key := strings.Join(args, " ")
	if resp, ok := m.responses[key]; ok {
		return resp.output, resp.err
	}
	if m.defaultResponse != nil {
		return m.defaultResponse.output, m.defaultResponse.err
	}
	return "", fmt.Errorf("unexpected git command: %s", key)
}

func (m *mockGitRunner) wasCalled(args string) bool {
	for _, call := range m.calls {
		if strings.Join(call, " ") == args {
			return true
		}
	}
	return false
}

// --- Manager tests with mock git ---

func TestManager_Create_ExistingWorktree(t *testing.T) {
	git := newMockGitRunner()
	path := branchPath("/worktrees", "feat/auth")
	git.On("worktree list --porcelain",
		"worktree /project\nHEAD abc\nbranch refs/heads/main\n\n"+
			"worktree "+path+"\nHEAD def\nbranch refs/heads/feat/auth\n", nil)

	mgr := NewManager(git, "/project", "/worktrees")
	result, err := mgr.Create("feat/auth", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Created {
		t.Error("expected Created=false for existing worktree")
	}
	if result.Branch != "feat/auth" {
		t.Errorf("expected branch=feat/auth, got %s", result.Branch)
	}
	if result.Path != path {
		t.Errorf("expected path=%s, got %s", path, result.Path)
	}
}

func TestManager_Create_LocalBranch(t *testing.T) {
	git := newMockGitRunner()
	path := branchPath("/worktrees", "feat/auth")
	git.On("worktree list --porcelain",
		"worktree /project\nHEAD abc\nbranch refs/heads/main\n", nil)
	git.On("rev-parse --verify refs/heads/feat/auth", "abc123", nil)
	git.On("worktree add -- "+path+" feat/auth", "", nil)

	mgr := NewManager(git, "/project", "/worktrees")
	result, err := mgr.Create("feat/auth", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Created {
		t.Error("expected Created=true")
	}
	if result.Resolution != BranchLocal {
		t.Errorf("expected resolution=local, got %s", result.Resolution)
	}
}

func TestManager_Create_RemoteBranch(t *testing.T) {
	git := newMockGitRunner()
	path := branchPath("/worktrees", "feat/auth")
	git.On("worktree list --porcelain",
		"worktree /project\nHEAD abc\nbranch refs/heads/main\n", nil)
	git.On("rev-parse --verify refs/heads/feat/auth", "", fmt.Errorf("not found"))
	git.On("fetch origin", "", nil)
	git.On("rev-parse --verify refs/remotes/origin/feat/auth", "abc123", nil)
	git.On("branch --track -- feat/auth origin/feat/auth", "", nil)
	git.On("worktree add -- "+path+" feat/auth", "", nil)

	mgr := NewManager(git, "/project", "/worktrees")
	result, err := mgr.Create("feat/auth", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Created {
		t.Error("expected Created=true")
	}
	if result.Resolution != BranchRemoteTracking {
		t.Errorf("expected resolution=remote-tracking, got %s", result.Resolution)
	}
}

func TestManager_Create_NewBranch_DefaultBase(t *testing.T) {
	git := newMockGitRunner()
	path := branchPath("/worktrees", "feat/new")
	git.On("worktree list --porcelain",
		"worktree /project\nHEAD abc\nbranch refs/heads/main\n", nil)
	git.On("rev-parse --verify refs/heads/feat/new", "", fmt.Errorf("not found"))
	git.On("fetch origin", "", nil)
	git.On("rev-parse --verify refs/remotes/origin/feat/new", "", fmt.Errorf("not found"))
	git.On("symbolic-ref refs/remotes/origin/HEAD", "refs/remotes/origin/main", nil)
	git.On("branch -- feat/new refs/remotes/origin/main", "", nil)
	git.On("worktree add -- "+path+" feat/new", "", nil)

	mgr := NewManager(git, "/project", "/worktrees")
	result, err := mgr.Create("feat/new", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Created {
		t.Error("expected Created=true")
	}
	if result.Resolution != BranchNew {
		t.Errorf("expected resolution=new, got %s", result.Resolution)
	}
}

func TestManager_Create_NewBranch_CustomFrom(t *testing.T) {
	git := newMockGitRunner()
	path := branchPath("/worktrees", "feat/new")
	git.On("worktree list --porcelain",
		"worktree /project\nHEAD abc\nbranch refs/heads/main\n", nil)
	git.On("rev-parse --verify refs/heads/feat/new", "", fmt.Errorf("not found"))
	git.On("fetch origin", "", nil)
	git.On("rev-parse --verify refs/remotes/origin/feat/new", "", fmt.Errorf("not found"))
	git.On("branch -- feat/new origin/develop", "", nil)
	git.On("worktree add -- "+path+" feat/new", "", nil)

	mgr := NewManager(git, "/project", "/worktrees")
	result, err := mgr.Create("feat/new", "origin/develop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Created {
		t.Error("expected Created=true")
	}
	if result.Resolution != BranchNew {
		t.Errorf("expected resolution=new, got %s", result.Resolution)
	}
	if !git.wasCalled("branch -- feat/new origin/develop") {
		t.Error("expected branch creation from origin/develop")
	}
}

func TestManager_Remove(t *testing.T) {
	t.Run("remove worktree and branch", func(t *testing.T) {
		git := newMockGitRunner()
		path := branchPath("/worktrees", "feat/auth")
		git.On("worktree remove "+path, "", nil)
		git.On("branch -D -- feat/auth", "", nil)

		mgr := NewManager(git, "/project", "/worktrees")
		_, err := mgr.Remove("feat/auth", true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !git.wasCalled("branch -D -- feat/auth") {
			t.Error("expected branch deletion")
		}
	})

	t.Run("remove worktree keep branch", func(t *testing.T) {
		git := newMockGitRunner()
		path := branchPath("/worktrees", "feat/auth")
		git.On("worktree remove "+path, "", nil)

		mgr := NewManager(git, "/project", "/worktrees")
		_, err := mgr.Remove("feat/auth", false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if git.wasCalled("branch -D -- feat/auth") {
			t.Error("branch should not have been deleted")
		}
	})

	t.Run("force remove dirty worktree", func(t *testing.T) {
		git := newMockGitRunner()
		path := branchPath("/worktrees", "feat/auth")
		git.On("worktree remove --force "+path, "", nil)
		git.On("branch -D -- feat/auth", "", nil)

		mgr := NewManager(git, "/project", "/worktrees")
		_, err := mgr.Remove("feat/auth", true, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !git.wasCalled("worktree remove --force " + path) {
			t.Error("expected --force flag when force=true")
		}
	})

	t.Run("non-force remove dirty worktree fails", func(t *testing.T) {
		git := newMockGitRunner()
		path := branchPath("/worktrees", "feat/dirty")
		git.On("worktree remove "+path, "", fmt.Errorf("git worktree remove: has uncommitted changes"))

		mgr := NewManager(git, "/project", "/worktrees")
		_, err := mgr.Remove("feat/dirty", true, false)
		if err == nil {
			t.Fatal("expected error when removing dirty worktree without force")
		}
		if !strings.Contains(err.Error(), "uncommitted changes") {
			t.Errorf("expected 'uncommitted changes' in error, got: %v", err)
		}
	})

	t.Run("does not delete branch when no worktree matches", func(t *testing.T) {
		git := newMockGitRunner()
		path := branchPath("/worktrees", "feat/missing")
		git.On("worktree remove --force "+path, "", fmt.Errorf("%s", "git worktree remove: "+path+" is not a working tree"))
		git.On("worktree list --porcelain",
			"worktree /project\nHEAD abc\nbranch refs/heads/main\n", nil)

		mgr := NewManager(git, "/project", "/worktrees")
		_, err := mgr.Remove("feat/missing", true, true)
		if err == nil {
			t.Fatal("expected error when no matching worktree exists")
		}
		if git.wasCalled("branch -D -- feat/missing") {
			t.Error("branch should not be deleted when no worktree matches")
		}
	})

	t.Run("deletes branch when stale worktree metadata still exists", func(t *testing.T) {
		git := newMockGitRunner()
		path := branchPath("/worktrees", "feat/ghost")
		git.On("worktree remove --force "+path, "", fmt.Errorf("%s", "git worktree remove: "+path+" is not a working tree"))
		git.On("worktree list --porcelain",
			"worktree /project\nHEAD abc\nbranch refs/heads/main\n\n"+
				"worktree "+path+"\nHEAD def\nbranch refs/heads/feat/ghost\n", nil)
		git.On("worktree prune", "", nil)
		git.On("branch -D -- feat/ghost", "", nil)

		mgr := NewManager(git, "/project", "/worktrees")
		result, err := mgr.Remove("feat/ghost", true, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.BranchDeleted {
			t.Error("expected branch deletion after ghost cleanup")
		}
	})

	t.Run("uses registered worktree path before falling back to computed path", func(t *testing.T) {
		git := newMockGitRunner()
		git.On("worktree list --porcelain",
			"worktree /project\nHEAD abc\nbranch refs/heads/main\n\n"+
				"worktree /worktrees/feat-auth\nHEAD def\nbranch refs/heads/feat/auth\n", nil)
		git.On("worktree remove /worktrees/feat-auth", "", nil)
		git.On("branch -D -- feat/auth", "", nil)

		mgr := NewManager(git, "/project", "/worktrees")
		_, err := mgr.Remove("feat/auth", true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !git.wasCalled("worktree remove /worktrees/feat-auth") {
			t.Fatal("expected removal to target the registered worktree path")
		}
	})

	t.Run("refuses to delete unregistered directories", func(t *testing.T) {
		projectRoot := t.TempDir()
		worktreeDir := filepath.Join(projectRoot, "worktrees")
		path := branchPath(worktreeDir, "feat/bogus")
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("failed to create path: %v", err)
		}

		git := newMockGitRunner()
		git.On("worktree list --porcelain",
			"worktree "+projectRoot+"\nHEAD abc\nbranch refs/heads/main\n", nil)
		git.On("worktree remove --force "+path, "", fmt.Errorf("%s", "git worktree remove: "+path+" is not a working tree"))

		mgr := NewManager(git, projectRoot, worktreeDir)
		_, err := mgr.Remove("feat/bogus", true, true)
		if err == nil {
			t.Fatal("expected error when path is not a registered worktree")
		}
		if !strings.Contains(err.Error(), "refusing to remove unregistered path") {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected directory to remain on disk: %v", statErr)
		}
		if git.wasCalled("branch -D -- feat/bogus") {
			t.Fatal("branch should not be deleted when the worktree path is unregistered")
		}
	})
}

func TestManager_List(t *testing.T) {
	git := newMockGitRunner()
	git.On("worktree list --porcelain",
		"worktree /project\nHEAD abc\nbranch refs/heads/main\n\n"+
			"worktree /worktrees/feat-auth\nHEAD def\nbranch refs/heads/feat/auth\n", nil)

	mgr := NewManager(git, "/project", "/worktrees")
	list, err := mgr.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(list))
	}
	if list[0].Branch != "main" {
		t.Errorf("expected first branch=main, got %s", list[0].Branch)
	}
	if list[1].Branch != "feat/auth" {
		t.Errorf("expected second branch=feat/auth, got %s", list[1].Branch)
	}
}

// --- Integration tests with real git ---

// setupTestRepo creates a temporary git repo with an initial commit and returns
// the repo path and a cleanup function.
func setupTestRepo(t *testing.T) (repoDir string, worktreeDir string) {
	t.Helper()

	// Create temp dirs
	repoDir = t.TempDir()
	worktreeDir = t.TempDir()

	// Initialize a repo
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@grove.test")
	run("config", "user.name", "Grove Test")

	// Create initial commit
	initialFile := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(initialFile, []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial commit")

	return repoDir, worktreeDir
}

func TestIntegration_CreateWorktree_LocalBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	repoDir, worktreeDir := setupTestRepo(t)
	git := NewGitRunner(repoDir)

	// Create a local branch
	gitExec := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	gitExec("branch", "feat/local-test")

	mgr := NewManager(git, repoDir, worktreeDir)
	result, err := mgr.Create("feat/local-test", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !result.Created {
		t.Error("expected Created=true")
	}
	if result.Resolution != BranchLocal {
		t.Errorf("expected resolution=local, got %s", result.Resolution)
	}
	expectedPath := filepath.Join(worktreeDir, SanitizeBranchName("feat/local-test"))
	if result.Path != expectedPath {
		t.Errorf("expected path=%s, got %s", expectedPath, result.Path)
	}

	// Verify worktree directory exists
	if _, err := os.Stat(result.Path); os.IsNotExist(err) {
		t.Error("worktree directory was not created")
	}
}

func TestIntegration_CreateWorktree_NewBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	repoDir, worktreeDir := setupTestRepo(t)
	git := NewGitRunner(repoDir)

	mgr := NewManager(git, repoDir, worktreeDir)

	// Create with --from pointing to main (since no remote exists in test)
	result, err := mgr.Create("feat/brand-new", "main")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !result.Created {
		t.Error("expected Created=true")
	}
	if result.Resolution != BranchNew {
		t.Errorf("expected resolution=new, got %s", result.Resolution)
	}

	// Verify worktree directory exists
	if _, err := os.Stat(result.Path); os.IsNotExist(err) {
		t.Error("worktree directory was not created")
	}
}

func TestIntegration_CreateWorktree_Reuse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	repoDir, worktreeDir := setupTestRepo(t)
	git := NewGitRunner(repoDir)

	// Create a local branch
	gitExec := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	gitExec("branch", "feat/reuse-test")

	mgr := NewManager(git, repoDir, worktreeDir)

	// First creation
	result1, err := mgr.Create("feat/reuse-test", "")
	if err != nil {
		t.Fatalf("first Create failed: %v", err)
	}
	if !result1.Created {
		t.Error("expected first Create to create worktree")
	}

	// Second creation should reuse
	result2, err := mgr.Create("feat/reuse-test", "")
	if err != nil {
		t.Fatalf("second Create failed: %v", err)
	}
	if result2.Created {
		t.Error("expected second Create to reuse worktree")
	}
	// Resolve symlinks before comparing (macOS /var -> /private/var)
	path1, _ := filepath.EvalSymlinks(result1.Path)
	path2, _ := filepath.EvalSymlinks(result2.Path)
	if path1 != path2 {
		t.Errorf("expected same path, got %s vs %s", path1, path2)
	}
}

func TestIntegration_RemoveWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	repoDir, worktreeDir := setupTestRepo(t)
	git := NewGitRunner(repoDir)

	gitExec := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	gitExec("branch", "feat/remove-test")

	mgr := NewManager(git, repoDir, worktreeDir)

	// Create worktree
	result, err := mgr.Create("feat/remove-test", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Remove worktree (keep branch, no force)
	_, err = mgr.Remove("feat/remove-test", false, false)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify directory is gone
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Error("worktree directory should have been removed")
	}

	// Branch should still exist
	_, err = git.Run("rev-parse", "--verify", "refs/heads/feat/remove-test")
	if err != nil {
		t.Error("branch should still exist after remove with deleteBranch=false")
	}
}

func TestIntegration_RemoveWorktreeAndBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	repoDir, worktreeDir := setupTestRepo(t)
	git := NewGitRunner(repoDir)

	gitExec := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	gitExec("branch", "feat/remove-branch-test")

	mgr := NewManager(git, repoDir, worktreeDir)

	// Create and then remove with branch deletion
	_, err := mgr.Create("feat/remove-branch-test", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err = mgr.Remove("feat/remove-branch-test", true, false)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Branch should be gone
	_, err = git.Run("rev-parse", "--verify", "refs/heads/feat/remove-branch-test")
	if err == nil {
		t.Error("branch should have been deleted")
	}
}

func TestIntegration_ListWorktrees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	repoDir, worktreeDir := setupTestRepo(t)
	git := NewGitRunner(repoDir)

	gitExec := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}

	mgr := NewManager(git, repoDir, worktreeDir)

	// Initially just the main worktree
	list, err := mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 worktree initially, got %d", len(list))
	}

	// Create two worktrees
	gitExec("branch", "feat/list-a")
	gitExec("branch", "feat/list-b")
	_, err = mgr.Create("feat/list-a", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	_, err = mgr.Create("feat/list-b", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should now have 3 worktrees (main + 2)
	list, err = mgr.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 worktrees, got %d", len(list))
	}
}

func TestIntegration_FindByPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	repoDir, worktreeDir := setupTestRepo(t)
	git := NewGitRunner(repoDir)

	gitExec := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
		}
	}
	gitExec("branch", "feat/find-test")

	mgr := NewManager(git, repoDir, worktreeDir)
	result, err := mgr.Create("feat/find-test", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Find by exact path
	info, err := mgr.FindByPath(result.Path)
	if err != nil {
		t.Fatalf("FindByPath failed: %v", err)
	}
	if info.Branch != "feat/find-test" {
		t.Errorf("expected branch=feat/find-test, got %s", info.Branch)
	}

	// Find by subdirectory path
	subDir := filepath.Join(result.Path, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	info, err = mgr.FindByPath(subDir)
	if err != nil {
		t.Fatalf("FindByPath subdir failed: %v", err)
	}
	if info.Branch != "feat/find-test" {
		t.Errorf("expected branch=feat/find-test, got %s", info.Branch)
	}

	// Not found
	_, err = mgr.FindByPath("/nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestCheckUnpushed_NoRemote(t *testing.T) {
	mock := newMockGitRunner()
	mock.On("rev-parse --verify refs/remotes/origin/feat/test", "", fmt.Errorf("not found"))
	mgr := NewManager(mock, "/repo", "../.worktrees")

	status, count, err := mgr.CheckUnpushed("feat/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != UnpushedNoRemote {
		t.Errorf("expected UnpushedNoRemote, got %d", status)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

func TestCheckUnpushed_HasUnpushed(t *testing.T) {
	mock := newMockGitRunner()
	mock.On("rev-parse --verify refs/remotes/origin/feat/test", "abc123", nil)
	mock.On("rev-list --count origin/feat/test..feat/test", "3", nil)
	mgr := NewManager(mock, "/repo", "../.worktrees")

	status, count, err := mgr.CheckUnpushed("feat/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != UnpushedCommits {
		t.Errorf("expected UnpushedCommits, got %d", status)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestCheckUnpushed_AllPushed(t *testing.T) {
	mock := newMockGitRunner()
	mock.On("rev-parse --verify refs/remotes/origin/feat/test", "abc123", nil)
	mock.On("rev-list --count origin/feat/test..feat/test", "0", nil)
	mgr := NewManager(mock, "/repo", "../.worktrees")

	status, count, err := mgr.CheckUnpushed("feat/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != UnpushedNone {
		t.Errorf("expected UnpushedNone, got %d", status)
	}
	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}
