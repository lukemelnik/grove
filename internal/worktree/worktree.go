// Package worktree manages git worktree creation, listing, and removal.
//
// It handles branch resolution (local, remote tracking, new from ref),
// worktree path calculation with branch name sanitization, and provides
// functions for listing and removing worktrees.
package worktree

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitRunner executes git commands. This interface exists to allow testing
// without actually running git.
type GitRunner interface {
	// Run executes a git command and returns its combined output.
	Run(args ...string) (string, error)
}

// realGitRunner executes git commands via os/exec.
type realGitRunner struct {
	// repoDir is the working directory for git commands.
	repoDir string
}

// NewGitRunner creates a GitRunner that runs real git commands in the given directory.
func NewGitRunner(repoDir string) GitRunner {
	return &realGitRunner{repoDir: repoDir}
}

func (g *realGitRunner) Run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.repoDir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), output, err)
	}
	return output, nil
}

// SanitizeBranchName converts a branch name into a safe directory name
// by replacing slashes with dashes.
func SanitizeBranchName(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// WorktreePath computes the absolute path for a worktree given the project root,
// worktree directory configuration, and branch name.
func WorktreePath(projectRoot, worktreeDir, branch string) string {
	sanitized := SanitizeBranchName(branch)
	base := worktreeDir
	if !filepath.IsAbs(base) {
		base = filepath.Join(projectRoot, base)
	}
	return filepath.Join(base, sanitized)
}

// Info holds information about a single worktree.
type Info struct {
	// Path is the absolute path to the worktree directory.
	Path string
	// Branch is the branch checked out in this worktree (without refs/heads/ prefix).
	Branch string
	// IsBare indicates this is a bare repository entry.
	IsBare bool
}

// BranchResolution describes how a branch was resolved.
type BranchResolution int

const (
	// BranchLocal means the branch already existed locally.
	BranchLocal BranchResolution = iota
	// BranchRemoteTracking means the branch was fetched from a remote and a tracking branch was created.
	BranchRemoteTracking
	// BranchNew means the branch was newly created from a base ref.
	BranchNew
)

// String returns a human-readable description of the resolution.
func (r BranchResolution) String() string {
	switch r {
	case BranchLocal:
		return "local"
	case BranchRemoteTracking:
		return "remote-tracking"
	case BranchNew:
		return "new"
	default:
		return "unknown"
	}
}

// CreateResult holds the result of creating a worktree.
type CreateResult struct {
	// Path is the absolute path to the created worktree.
	Path string
	// Branch is the branch name.
	Branch string
	// Resolution describes how the branch was resolved.
	Resolution BranchResolution
	// Created is true if a new worktree was created, false if reused.
	Created bool
}

// Manager handles worktree operations.
type Manager struct {
	git         GitRunner
	projectRoot string
	worktreeDir string
}

// NewManager creates a new worktree Manager.
func NewManager(git GitRunner, projectRoot, worktreeDir string) *Manager {
	return &Manager{
		git:         git,
		projectRoot: projectRoot,
		worktreeDir: worktreeDir,
	}
}

// Create creates a git worktree for the given branch. If a worktree already
// exists for this branch, it is reused.
//
// Branch resolution:
//  1. Branch exists locally -> use it
//  2. Branch exists on remote only -> fetch, create local tracking branch
//  3. Branch doesn't exist anywhere -> fetch default branch, create new branch from baseRef
//
// The fromRef parameter overrides the base branch for new branches.
// If empty, "origin/main" is used as the default.
func (m *Manager) Create(branch, fromRef string) (*CreateResult, error) {
	wtPath := WorktreePath(m.projectRoot, m.worktreeDir, branch)

	// Check if worktree already exists for this branch
	existing, err := m.List()
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}
	for _, wt := range existing {
		if wt.Branch == branch {
			return &CreateResult{
				Path:       wt.Path,
				Branch:     branch,
				Resolution: BranchLocal,
				Created:    false,
			}, nil
		}
	}

	// Determine branch resolution
	resolution, err := m.resolveBranch(branch, fromRef)
	if err != nil {
		return nil, fmt.Errorf("resolving branch %q: %w", branch, err)
	}

	// Create the worktree
	_, err = m.git.Run("worktree", "add", wtPath, branch)
	if err != nil {
		return nil, fmt.Errorf("creating worktree at %s: %w", wtPath, err)
	}

	return &CreateResult{
		Path:       wtPath,
		Branch:     branch,
		Resolution: resolution,
		Created:    true,
	}, nil
}

// resolveBranch ensures the branch exists locally with the correct setup.
// Returns the resolution type.
func (m *Manager) resolveBranch(branch, fromRef string) (BranchResolution, error) {
	// 1. Check if branch exists locally
	if m.branchExistsLocal(branch) {
		return BranchLocal, nil
	}

	// 2. Check if branch exists on remote
	// Fetch first to ensure we have up-to-date remote refs
	_, _ = m.git.Run("fetch", "origin")

	if m.branchExistsRemote(branch) {
		// Create local tracking branch from remote
		_, err := m.git.Run("branch", "--track", branch, "origin/"+branch)
		if err != nil {
			return 0, fmt.Errorf("creating tracking branch: %w", err)
		}
		return BranchRemoteTracking, nil
	}

	// 3. Branch doesn't exist anywhere — create from base ref
	baseRef := fromRef
	if baseRef == "" {
		baseRef = m.defaultBaseRef()
	}

	// Note: we already fetched origin above, no need to fetch again.

	// Create new branch from base ref
	_, err := m.git.Run("branch", branch, baseRef)
	if err != nil {
		return 0, fmt.Errorf("creating branch %q from %q: %w", branch, baseRef, err)
	}
	return BranchNew, nil
}

// defaultBaseRef returns the default base ref (origin/main or origin/master).
func (m *Manager) defaultBaseRef() string {
	// Try to detect the default branch from the remote
	out, err := m.git.Run("symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		// Output is like "refs/remotes/origin/main"
		ref := strings.TrimSpace(out)
		if ref != "" {
			return ref
		}
	}
	// Fallback to origin/main
	return "origin/main"
}

// branchExistsLocal checks if a branch exists locally.
func (m *Manager) branchExistsLocal(branch string) bool {
	_, err := m.git.Run("rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

// branchExistsRemote checks if a branch exists on origin.
func (m *Manager) branchExistsRemote(branch string) bool {
	_, err := m.git.Run("rev-parse", "--verify", "refs/remotes/origin/"+branch)
	return err == nil
}

// List returns information about all worktrees in the repository.
func (m *Manager) List() ([]Info, error) {
	out, err := m.git.Run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}
	return parseWorktreeList(out), nil
}

// parseWorktreeList parses the output of `git worktree list --porcelain`.
//
// Format:
//
//	worktree /path/to/worktree
//	HEAD <sha>
//	branch refs/heads/main
//	<blank line>
//	worktree /path/to/another
//	HEAD <sha>
//	branch refs/heads/feat-x
//	<blank line>
func parseWorktreeList(output string) []Info {
	var results []Info
	if output == "" {
		return results
	}

	// Split into blocks separated by blank lines
	blocks := strings.Split(output, "\n\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		var info Info
		lines := strings.Split(block, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "worktree ") {
				info.Path = strings.TrimPrefix(line, "worktree ")
			} else if strings.HasPrefix(line, "branch ") {
				ref := strings.TrimPrefix(line, "branch ")
				// Strip refs/heads/ prefix
				info.Branch = strings.TrimPrefix(ref, "refs/heads/")
			} else if line == "bare" {
				info.IsBare = true
			}
		}

		if info.Path != "" {
			results = append(results, info)
		}
	}

	return results
}

// Remove removes a worktree and optionally deletes the branch.
func (m *Manager) Remove(branch string, deleteBranch bool) error {
	wtPath := WorktreePath(m.projectRoot, m.worktreeDir, branch)

	// Remove the worktree (--force to handle dirty worktrees)
	_, err := m.git.Run("worktree", "remove", "--force", wtPath)
	if err != nil {
		return fmt.Errorf("removing worktree at %s: %w", wtPath, err)
	}

	if deleteBranch {
		// Delete the local branch (-D to force delete even if unmerged)
		_, err := m.git.Run("branch", "-D", branch)
		if err != nil {
			return fmt.Errorf("deleting branch %q: %w", branch, err)
		}
	}

	return nil
}

// FindByPath finds the worktree info for a given working directory path.
// This walks up from the given directory looking for a match against known worktrees.
func (m *Manager) FindByPath(dir string) (*Info, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute path: %w", err)
	}
	// Resolve symlinks (e.g., macOS /var -> /private/var)
	absDir, err = filepath.EvalSymlinks(absDir)
	if err != nil {
		return nil, fmt.Errorf("resolving symlinks: %w", err)
	}

	worktrees, err := m.List()
	if err != nil {
		return nil, err
	}

	for _, wt := range worktrees {
		wtAbs, err := filepath.Abs(wt.Path)
		if err != nil {
			continue
		}
		// Resolve symlinks for the worktree path too
		wtAbs, err = filepath.EvalSymlinks(wtAbs)
		if err != nil {
			continue
		}
		// Check if dir is within this worktree
		if absDir == wtAbs || strings.HasPrefix(absDir, wtAbs+string(filepath.Separator)) {
			return &wt, nil
		}
	}

	return nil, fmt.Errorf("no worktree found for path %s", dir)
}
