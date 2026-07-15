// Package worktree manages git worktree creation, listing, and removal.
//
// It handles branch resolution (local, remote tracking, new from ref),
// worktree path calculation with branch name sanitization, and provides
// functions for listing and removing worktrees.
package worktree

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
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

// SanitizeBranchName converts a branch name into a safe, collision-resistant
// directory or tmux identifier.
func SanitizeBranchName(branch string) string {
	switch branch {
	case "":
		return "branch"
	case ".":
		return "dot"
	case "..":
		return "dotdot"
	}

	var b strings.Builder
	for _, r := range branch {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}

	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		sanitized = "branch"
	}
	if sanitized != branch {
		return sanitized + "-" + shortBranchHash(branch)
	}
	return sanitized
}

func shortBranchHash(branch string) string {
	sum := sha1.Sum([]byte(branch))
	return hex.EncodeToString(sum[:4])
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
	// WorktreeCreated is true if this invocation created a worktree.
	WorktreeCreated bool
	// BranchCreated is true if this invocation created a local branch.
	BranchCreated bool
	// InitialBranchTip is the branch tip immediately before worktree checkout
	// and any repository post-checkout hook side effects.
	InitialBranchTip string
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
	resolution, branchCreated, err := m.resolveBranch(branch, fromRef)
	if err != nil {
		return nil, fmt.Errorf("resolving branch %q: %w", branch, err)
	}

	initialTip, err := m.branchTipForCheckout(branch)
	if err != nil {
		return nil, fmt.Errorf("capturing branch tip before worktree checkout; branch retained: %w", err)
	}

	// Create the worktree (-- separates options from positional args)
	_, err = m.git.Run("worktree", "add", "--", wtPath, branch)
	if err != nil {
		if branchCreated {
			currentTip, tipErr := m.branchTipForCheckout(branch)
			if tipErr != nil || currentTip != initialTip {
				return nil, fmt.Errorf("creating worktree at %s: %w (new branch %q retained because its tip could not be proven unchanged)", wtPath, err, branch)
			}
			if _, deleteErr := m.git.Run("branch", "-D", "--", branch); deleteErr != nil {
				return nil, fmt.Errorf("creating worktree at %s: %w (rollback deleting branch %q failed: %v)", wtPath, err, branch, deleteErr)
			}
		}
		return nil, fmt.Errorf("creating worktree at %s: %w", wtPath, err)
	}

	return &CreateResult{
		Path:             wtPath,
		Branch:           branch,
		Resolution:       resolution,
		Created:          true,
		WorktreeCreated:  true,
		BranchCreated:    branchCreated,
		InitialBranchTip: initialTip,
	}, nil
}

// resolveBranch ensures the branch exists locally with the correct setup.
// Returns the resolution type.
func (m *Manager) resolveBranch(branch, fromRef string) (BranchResolution, bool, error) {
	// 1. Check if branch exists locally
	if m.branchExistsLocal(branch) {
		return BranchLocal, false, nil
	}

	// 2. Check if branch exists on remote. A failed refresh must not silently
	// adopt a potentially stale cached remote branch.
	_, fetchErr := m.git.Run("fetch", "origin")
	if m.branchExistsRemote(branch) {
		if fetchErr != nil {
			return 0, false, fmt.Errorf("refreshing origin before using remote branch %q: %w", branch, fetchErr)
		}
		// Create local tracking branch from remote
		_, err := m.git.Run("branch", "--track", "--", branch, "origin/"+branch)
		if err != nil {
			return 0, false, fmt.Errorf("creating tracking branch: %w", err)
		}
		return BranchRemoteTracking, true, nil
	}

	// 3. Branch doesn't exist anywhere — create from base ref
	baseRef := fromRef
	if baseRef == "" {
		if fetchErr != nil {
			// Repositories without an origin can still create branches safely from
			// their local default branch.
			baseRef = m.DefaultBranch()
		} else {
			baseRef = m.defaultBaseRef()
		}
	}

	// Create new branch from the chosen base ref without inheriting that ref as
	// upstream. The feature branch should track its own remote branch on first
	// push, not the base branch it was created from.
	_, err := m.git.Run("branch", "--no-track", "--", branch, baseRef)
	if err != nil {
		return 0, false, fmt.Errorf("creating branch %q from %q: %w", branch, baseRef, err)
	}
	return BranchNew, true, nil
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

// CommonStateDir returns the shared Git state directory used by every linked
// worktree in the repository.
func (m *Manager) CommonStateDir() (string, error) {
	out, err := m.git.Run("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		out, err = m.git.Run("rev-parse", "--git-common-dir")
		if err != nil {
			return "", fmt.Errorf("resolving git common dir: %w", err)
		}
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("resolving git common dir: empty output")
	}
	if !filepath.IsAbs(out) {
		abs, absErr := filepath.Abs(filepath.Join(m.projectRoot, out))
		if absErr != nil {
			return "", fmt.Errorf("resolving git common dir %q: %w", out, absErr)
		}
		out = abs
	}
	return filepath.Clean(out), nil
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

// RemoveResult describes what Remove actually did.
type RemoveResult struct {
	// WorktreeRemoved is true if the worktree was removed.
	WorktreeRemoved bool
	// BranchDeleted is true if the local branch was deleted.
	BranchDeleted bool
	// BranchSkipReason explains why the branch wasn't deleted (empty if deleted or not requested).
	BranchSkipReason string
	// BranchDeleteError is the error encountered while deleting the branch after worktree removal.
	BranchDeleteError error
}

func (m *Manager) branchTipForCheckout(branch string) (string, error) {
	out, err := m.git.Run("rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("reading branch %q tip before checkout: %w", branch, err)
	}
	tip := strings.TrimSpace(out)
	if tip == "" {
		return "", fmt.Errorf("reading branch %q tip before checkout: empty output", branch)
	}
	return tip, nil
}

// BranchTip returns the current object ID for a local branch ref.
func (m *Manager) BranchTip(branch string) (string, error) {
	out, err := m.git.Run("rev-parse", "--verify", "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("reading branch %q tip: %w", branch, err)
	}
	tip := strings.TrimSpace(out)
	if tip == "" {
		return "", fmt.Errorf("reading branch %q tip: empty output", branch)
	}
	return tip, nil
}

// Remove removes a worktree and optionally deletes the branch.
// When force is false, git will refuse to remove worktrees with uncommitted changes.
// When force is true, the worktree is removed even if it has uncommitted changes.
//
// If git doesn't recognize the path as a worktree (e.g. leftover directory after
// metadata was pruned), it prunes stale metadata and removes the directory directly.
func (m *Manager) Remove(branch string, deleteBranch, force bool) (*RemoveResult, error) {
	return m.remove(branch, deleteBranch, force, "", nil)
}

// RemovalPathPolicy decides whether one untracked or ignored worktree path is
// proven tool-owned and may be discarded during an otherwise safe removal.
type RemovalPathPolicy func(worktreePath, relativePath string) bool

// RemoveIfBranchTip removes a worktree only while the branch remains at expectedTip.
// It verifies the tip before worktree removal and again before branch deletion.
func (m *Manager) RemoveIfBranchTip(branch string, deleteBranch, force bool, expectedTip string) (*RemoveResult, error) {
	return m.RemoveIfBranchTipWithPolicy(branch, deleteBranch, force, expectedTip, nil)
}

// RemoveIfBranchTipWithPolicy additionally rejects tracked changes and every
// untracked or ignored path not explicitly proven removable by policy.
func (m *Manager) RemoveIfBranchTipWithPolicy(branch string, deleteBranch, force bool, expectedTip string, policy RemovalPathPolicy) (*RemoveResult, error) {
	if strings.TrimSpace(expectedTip) == "" {
		return nil, fmt.Errorf("expected branch tip is required for checked removal")
	}
	return m.remove(branch, deleteBranch, force, strings.TrimSpace(expectedTip), policy)
}

func (m *Manager) remove(branch string, deleteBranch, force bool, expectedTip string, policy RemovalPathPolicy) (*RemoveResult, error) {
	wtPath, registered := m.resolveWorktreePath(branch)
	result := &RemoveResult{}

	if expectedTip != "" {
		currentTip, err := m.BranchTip(branch)
		if err != nil {
			return nil, err
		}
		if currentTip != expectedTip {
			return nil, fmt.Errorf("branch %q changed from %s to %s before worktree removal; aborting", branch, expectedTip, currentTip)
		}
	}

	if !force && policy != nil {
		if err := m.validateRemovalPaths(wtPath, policy); err != nil {
			return nil, err
		}
		if expectedTip != "" {
			currentTip, err := m.BranchTip(branch)
			if err != nil {
				return nil, err
			}
			if currentTip != expectedTip {
				return nil, fmt.Errorf("branch %q changed during worktree safety validation; aborting", branch)
			}
		}
	}

	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, wtPath)

	_, err := m.git.Run(args...)
	if err != nil {
		// If git doesn't recognize it as a worktree, only treat it as ghost
		// metadata when the path or registration still exists.
		if strings.Contains(err.Error(), "not a working tree") {
			_, statErr := os.Stat(wtPath)
			pathExists := statErr == nil
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("checking leftover worktree path %s: %w", wtPath, statErr)
			}
			if !registered && !pathExists {
				return nil, fmt.Errorf("removing worktree at %s: %w", wtPath, err)
			}
			if pathExists && !looksLikeGitWorktree(wtPath) {
				if registered {
					return nil, fmt.Errorf("refusing to remove registered path %s because it no longer looks like a git worktree", wtPath)
				}
				return nil, fmt.Errorf("refusing to remove unregistered path %s", wtPath)
			}

			// Prune stale worktree metadata before deleting the validated leftover.
			if pruneErr := m.Prune(); pruneErr != nil {
				return nil, fmt.Errorf("pruning stale worktree metadata before removing %s: %w", wtPath, pruneErr)
			}

			if pathExists {
				return nil, fmt.Errorf("refusing direct recursive removal of stale worktree path %s after Git stopped recognizing it; inspect and remove it manually", wtPath)
			}
		} else {
			return nil, fmt.Errorf("removing worktree at %s: %w", wtPath, err)
		}
	}
	result.WorktreeRemoved = true

	if deleteBranch {
		if expectedTip != "" {
			currentTip, err := m.BranchTip(branch)
			if err != nil {
				result.BranchSkipReason = "could not verify branch tip before deletion"
				result.BranchDeleteError = err
				return result, err
			}
			if currentTip != expectedTip {
				result.BranchSkipReason = "branch changed after worktree removal; branch preserved"
				result.BranchDeleteError = fmt.Errorf("branch %q changed from %s to %s after worktree removal; branch preserved", branch, expectedTip, currentTip)
				return result, result.BranchDeleteError
			}
		}
		_, err := m.git.Run("branch", "-D", "--", branch)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "used by worktree") {
				// The blocking worktree might be a ghost (directory deleted
				// but metadata not pruned). Prune and retry once.
				if pruneErr := m.Prune(); pruneErr != nil {
					result.BranchDeleteError = fmt.Errorf("pruning worktree metadata before deleting branch %q: %w", branch, pruneErr)
					return result, result.BranchDeleteError
				}
				_, retryErr := m.git.Run("branch", "-D", "--", branch)
				if retryErr != nil {
					if strings.Contains(retryErr.Error(), "used by worktree") {
						result.BranchSkipReason = "branch checked out in another worktree"
					} else if strings.Contains(retryErr.Error(), "not found") {
						result.BranchSkipReason = "branch already deleted"
					} else {
						result.BranchDeleteError = fmt.Errorf("deleting branch %q after prune: %w", branch, retryErr)
						return result, result.BranchDeleteError
					}
				} else {
					result.BranchDeleted = true
				}
			} else if strings.Contains(errMsg, "not found") {
				result.BranchSkipReason = "branch already deleted"
			} else {
				result.BranchSkipReason = "could not delete branch"
				result.BranchDeleteError = fmt.Errorf("deleting branch %q: %w", branch, err)
				return result, result.BranchDeleteError
			}
		} else {
			result.BranchDeleted = true
		}
	}

	return result, nil
}

func (m *Manager) validateRemovalPaths(worktreePath string, policy RemovalPathPolicy) error {
	checkTracked := func() error {
		tracked, err := m.git.Run("--no-optional-locks", "-C", worktreePath, "status", "--porcelain=v1", "-z", "--untracked-files=no", "--ignore-submodules=none")
		if err != nil {
			return fmt.Errorf("checking tracked worktree changes: %w", err)
		}
		if tracked != "" {
			return fmt.Errorf("refusing to remove worktree with uncommitted tracked changes")
		}
		return nil
	}
	if err := checkTracked(); err != nil {
		return err
	}

	commands := [][]string{
		{"-C", worktreePath, "ls-files", "--others", "--exclude-standard", "-z"},
		{"-C", worktreePath, "ls-files", "--others", "--ignored", "--exclude-standard", "-z"},
	}
	seen := map[string]bool{}
	var managedPaths []string
	for _, args := range commands {
		out, err := m.git.Run(args...)
		if err != nil {
			return fmt.Errorf("checking untracked and ignored worktree files: %w", err)
		}
		for _, path := range strings.Split(out, "\x00") {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			if !policy(worktreePath, filepath.FromSlash(path)) {
				return fmt.Errorf("refusing to remove worktree with non-Grove untracked or ignored path %q", path)
			}
			managedPaths = append(managedPaths, path)
		}
	}

	// Git intentionally treats ignored files as disposable, but refuses other
	// untracked files without --force. Remove only the exact Grove-owned files
	// proven above, then keep Git's own non-force cleanliness check as a final
	// defense against files that appear concurrently.
	for _, path := range managedPaths {
		rel := filepath.FromSlash(path)
		if !policy(worktreePath, rel) {
			return fmt.Errorf("managed path %q changed during removal safety validation", path)
		}
		if err := os.Remove(filepath.Join(worktreePath, rel)); err != nil {
			return fmt.Errorf("removing Grove-owned worktree path %q: %w", path, err)
		}
	}

	if err := checkTracked(); err != nil {
		return err
	}
	for _, args := range commands {
		out, err := m.git.Run(args...)
		if err != nil {
			return fmt.Errorf("rechecking untracked and ignored worktree files: %w", err)
		}
		if out != "" {
			return fmt.Errorf("worktree changed during removal safety validation; refusing removal")
		}
	}
	return nil
}

func (m *Manager) resolveWorktreePath(branch string) (string, bool) {
	worktrees, err := m.List()
	if err == nil {
		for _, wt := range worktrees {
			if wt.Branch == branch {
				return wt.Path, true
			}
		}
	}
	return WorktreePath(m.projectRoot, m.worktreeDir, branch), false
}

func looksLikeGitWorktree(path string) bool {
	data, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}

	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir:") {
		return false
	}

	gitDir := strings.TrimSpace(strings.TrimPrefix(content, "gitdir:"))
	gitDir = filepath.Clean(gitDir)
	worktreesDir := string(filepath.Separator) + "worktrees" + string(filepath.Separator)
	return strings.Contains(gitDir, worktreesDir)
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

// DefaultBranch returns the default branch name (e.g. "main" or "master").
// It tries to detect from the remote HEAD, then falls back to "main".
func (m *Manager) DefaultBranch() string {
	out, err := m.git.Run("symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		// Output is like "refs/remotes/origin/main"
		if strings.HasPrefix(ref, "refs/remotes/origin/") {
			return strings.TrimPrefix(ref, "refs/remotes/origin/")
		}
	}
	// Fallback: check if "main" or "master" exists locally
	if m.branchExistsLocal("main") {
		return "main"
	}
	if m.branchExistsLocal("master") {
		return "master"
	}
	return "main"
}

// BranchHasUniqueCommits returns true if the branch has commits not in baseBranch.
func (m *Manager) BranchHasUniqueCommits(branch, baseBranch string) (bool, error) {
	out, err := m.git.Run("rev-list", "--count", baseBranch+".."+branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "0", nil
}

// UnpushedStatus describes whether a branch has commits not on the remote.
type UnpushedStatus int

const (
	// UnpushedNone means all commits are pushed to the remote.
	UnpushedNone UnpushedStatus = iota
	// UnpushedCommits means the branch has commits not on the remote.
	UnpushedCommits
	// UnpushedNoRemote means the branch has no remote tracking branch
	// and was never configured to track one (truly never pushed).
	UnpushedNoRemote
	// UnpushedGone means the branch had a remote tracking branch that was
	// deleted (e.g. after a PR was merged and the remote branch was auto-deleted).
	UnpushedGone
)

// CheckUnpushed checks if a branch has unpushed commits.
// Returns the status and the number of unpushed commits (0 for NoRemote/Gone).
//
// When the remote tracking ref is missing, it distinguishes between branches
// that were never pushed (UnpushedNoRemote) and branches whose remote was
// deleted after a merge (UnpushedGone) by checking git branch config.
func (m *Manager) CheckUnpushed(branch string) (UnpushedStatus, int, error) {
	remote, remoteErr := m.git.Run("config", "--get", "branch."+branch+".remote")
	remote = strings.TrimSpace(remote)
	if remoteErr != nil || remote == "" || remote == "." {
		return UnpushedNoRemote, 0, nil
	}
	mergeRef, mergeErr := m.git.Run("config", "--get", "branch."+branch+".merge")
	if mergeErr != nil || strings.TrimSpace(mergeRef) == "" {
		return UnpushedNoRemote, 0, nil
	}

	upstream, err := m.git.Run("rev-parse", "--symbolic-full-name", branch+"@{upstream}")
	if err != nil {
		// A real configured remote plus merge ref with no resolvable local
		// remote-tracking ref indicates a previously tracked, now-gone branch.
		return UnpushedGone, 0, nil
	}

	upstream = strings.TrimSpace(upstream)
	expectedPrefix := "refs/remotes/" + remote + "/"
	if upstream == "" || !strings.HasPrefix(upstream, expectedPrefix) {
		// Local upstreams and custom non-remote namespaces are not proof that
		// any commit exists outside this repository.
		return UnpushedNoRemote, 0, nil
	}

	// Count commits ahead of the validated remote-tracking upstream.
	out, err := m.git.Run("rev-list", "--count", upstream+".."+branch)
	if err != nil {
		return 0, 0, fmt.Errorf("counting unpushed commits: %w", err)
	}

	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || count < 0 {
		return 0, 0, fmt.Errorf("parsing unpushed commit count")
	}
	if count > 0 {
		return UnpushedCommits, count, nil
	}
	return UnpushedNone, 0, nil
}

// IsBranchContentMerged checks if all of a branch's patches are already applied
// in the target branch, even if the commit SHAs differ (as happens with rebase
// or cherry-pick merges). It uses git cherry which compares patch-ids.
//
// Returns true if every commit on branch (not in "into") has a matching patch
// in "into", or if the branch has no unique commits at all.
//
// Note: this reliably detects rebase merges but may not detect squash merges
// (where multiple commits are combined into one with a different patch-id).
func (m *Manager) IsBranchContentMerged(branch, into string) (bool, error) {
	out, err := m.git.Run("cherry", into, branch)
	if err != nil {
		return false, err
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		// No unique commits — branch is same as or ancestor of target
		return true, nil
	}
	for _, line := range strings.Split(trimmed, "\n") {
		if strings.HasPrefix(line, "+ ") {
			// This commit's patch is NOT in the target branch
			return false, nil
		}
	}
	// All commits prefixed with "- " — all patches already applied
	return true, nil
}

// GoneBranches returns branch names whose remote tracking branch has been deleted.
// These are branches that show [gone] in `git branch -vv` output.
func (m *Manager) GoneBranches() ([]string, error) {
	out, err := m.git.Run("branch", "-vv")
	if err != nil {
		return nil, fmt.Errorf("listing branches: %w", err)
	}
	return parseGoneBranches(out), nil
}

// parseGoneBranches parses `git branch -vv` output for branches marked [gone].
func parseGoneBranches(output string) []string {
	var gone []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip the current branch marker
		if strings.HasPrefix(line, "* ") {
			line = line[2:]
		}
		// Strip the "+" prefix for branches checked out in other worktrees
		if strings.HasPrefix(line, "+ ") {
			line = line[2:]
		}
		if !strings.Contains(line, "[") {
			continue
		}
		// Look for ": gone]" which indicates the remote tracking branch is deleted
		if !strings.Contains(line, ": gone]") {
			continue
		}
		// Branch name is the first field
		fields := strings.Fields(line)
		if len(fields) > 0 {
			gone = append(gone, fields[0])
		}
	}
	return gone
}

// MergedBranches returns branch names that are fully merged into the given branch.
func (m *Manager) MergedBranches(into string) ([]string, error) {
	out, err := m.git.Run("branch", "--merged", into)
	if err != nil {
		return nil, fmt.Errorf("listing merged branches: %w", err)
	}
	return parseMergedBranches(out), nil
}

// parseMergedBranches parses `git branch --merged` output.
func parseMergedBranches(output string) []string {
	var merged []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip current branch (marked with *)
		if strings.HasPrefix(line, "* ") {
			continue
		}
		// Strip the "+" prefix for branches checked out in other worktrees
		if strings.HasPrefix(line, "+ ") {
			line = line[2:]
		}
		merged = append(merged, strings.TrimSpace(line))
	}
	return merged
}

// Prune runs `git worktree prune` to clean up stale worktree metadata.
func (m *Manager) Prune() error {
	_, err := m.git.Run("worktree", "prune")
	if err != nil {
		return fmt.Errorf("pruning worktrees: %w", err)
	}
	return nil
}
