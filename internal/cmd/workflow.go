package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/env"
	"github.com/lukemelnik/grove/internal/hooks"
	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/projectstate"
	"github.com/lukemelnik/grove/internal/worktree"
)

type projectContext struct {
	ConfigPath     string
	ProjectRoot    string
	CommonStateDir string
	Config         *config.Config
	Worktrees      *worktree.Manager
}

func loadProjectContext() (*projectContext, error) {
	cwd, err := getWorkingDir()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	configPath, projectRoot, err := config.Discover(cwd)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)

	commonStateDir, err := wtMgr.CommonStateDir()
	if err != nil {
		return nil, err
	}

	return &projectContext{
		ConfigPath:     configPath,
		ProjectRoot:    projectRoot,
		CommonStateDir: commonStateDir,
		Config:         cfg,
		Worktrees:      wtMgr,
	}, nil
}

func resolveRuntimeEnv(ctx *projectContext, branch string) (*ports.Assignment, *env.ManagedEnv, error) {
	assignment, err := readPortAssignment(ctx, branch)
	if err != nil {
		return nil, nil, fmt.Errorf("assigning ports: %w", err)
	}
	managed, err := env.BuildManagedEnv(ctx.Config, assignment.Ports, branch)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving managed environment: %w", err)
	}
	return assignment, managed, nil
}

func resolvePersistentRuntimeEnv(ctx context.Context, pc *projectContext, branch string) (*ports.Assignment, *env.ManagedEnv, func() error, error) {
	lock, err := acquireWorkflowLock(ctx, pc)
	if err != nil {
		return nil, nil, nil, err
	}
	assignment, err := assignPersistentPorts(pc, branch)
	if err != nil {
		_ = lock.Release()
		return nil, nil, nil, err
	}
	managed, err := env.BuildManagedEnv(pc.Config, assignment.Ports, branch)
	if err != nil {
		cleanupErr := reconcilePortRegistry(pc)
		releaseErr := lock.Release()
		return nil, nil, nil, combineCleanupErrors(fmt.Errorf("resolving managed environment: %w", err), cleanupErr, releaseErr)
	}
	return assignment, managed, lock.Release, nil
}

func acquireWorkflowLock(ctx context.Context, pc *projectContext) (*projectstate.Lock, error) {
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Hooks may run for up to one hour. Keep remote-host stale recovery above
	// that bound so a live mutation cannot be stolen while a hook is running.
	lock, err := projectstate.AcquireLock(lockCtx, pc.CommonStateDir, projectstate.LockOptions{StaleAfter: 2 * time.Hour})
	if err != nil {
		return nil, newCodedError("project_lock_unavailable", fmt.Errorf("acquiring project mutation lock: %w", err))
	}
	return lock, nil
}

func assignPersistentPorts(pc *projectContext, requestedBranch string) (*ports.Assignment, error) {
	if len(pc.Config.Services) == 0 {
		return &ports.Assignment{Ports: map[string]int{}}, nil
	}
	r, err := migratePortRegistry(pc, requestedBranch)
	if err != nil {
		return nil, err
	}
	rec := r.Branches[requestedBranch]
	return &ports.Assignment{Ports: rec.Ports, Offset: rec.Offset}, nil
}

func migratePortRegistry(pc *projectContext, requestedBranch string) (*ports.Registry, error) {
	store := ports.NewStore(pc.CommonStateDir)
	r, err := store.Load()
	if err != nil {
		return nil, newCodedError("port_registry_invalid", fmt.Errorf("repair the Git common-state Grove port registry: %w", err))
	}
	active, err := activeBranchNames(pc, requestedBranch)
	if err != nil {
		return nil, err
	}
	activeSet := map[string]bool{}
	for _, b := range active {
		activeSet[b] = true
	}
	for b := range r.Branches {
		if !activeSet[b] {
			delete(r.Branches, b)
		}
	}
	defaultBranch := pc.Worktrees.DefaultBranch()
	for _, branch := range orderBranchesForAllocation(active, defaultBranch) {
		rec, _, err := ports.Allocate(r, pc.Config.Services, branch, defaultBranch, ports.DefaultMaxOffset)
		if err != nil {
			return nil, newCodedError("port_registry_repair_required", fmt.Errorf("port registry needs repair for branch %q: %w", branch, err))
		}
		r.Branches[branch] = rec
	}
	if err := store.Save(r); err != nil {
		return nil, fmt.Errorf("saving port registry: %w", err)
	}
	return r, nil
}

func activeBranchNames(pc *projectContext, requestedBranch string) ([]string, error) {
	worktrees, err := pc.Worktrees.List()
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}
	seen := map[string]bool{}
	for _, wt := range worktrees {
		if !wt.IsBare && wt.Branch != "" {
			seen[wt.Branch] = true
		}
	}
	if requestedBranch != "" {
		seen[requestedBranch] = true
	}
	branches := make([]string, 0, len(seen))
	for b := range seen {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	return branches, nil
}

func orderBranchesForAllocation(branches []string, defaultBranch string) []string {
	out := append([]string(nil), branches...)
	sort.Strings(out)
	if defaultBranch == "" {
		return out
	}
	for i, b := range out {
		if b == defaultBranch {
			copy(out[1:i+1], out[0:i])
			out[0] = b
			break
		}
	}
	return out
}

func readPortAssignment(pc *projectContext, branch string) (*ports.Assignment, error) {
	if len(pc.Config.Services) == 0 {
		return &ports.Assignment{Ports: map[string]int{}}, nil
	}
	store := ports.NewStore(pc.CommonStateDir)
	r, err := store.Load()
	if err != nil {
		return nil, newCodedError("port_registry_invalid", fmt.Errorf("repair the Git common-state Grove port registry: %w", err))
	}
	active, err := activeBranchNames(pc, branch)
	if err != nil {
		return nil, err
	}
	activeSet := make(map[string]bool, len(active))
	for _, activeBranch := range active {
		activeSet[activeBranch] = true
	}
	for recordedBranch := range r.Branches {
		if !activeSet[recordedBranch] {
			delete(r.Branches, recordedBranch)
		}
	}
	defaultBranch := pc.Worktrees.DefaultBranch()
	assignments := make(map[string]*ports.Assignment, len(active))
	for _, activeBranch := range orderBranchesForAllocation(active, defaultBranch) {
		rec, assignment, allocateErr := ports.Allocate(r, pc.Config.Services, activeBranch, defaultBranch, ports.DefaultMaxOffset)
		if allocateErr != nil {
			return nil, newCodedError("port_registry_repair_required", fmt.Errorf("port registry needs repair for branch %q: %w", activeBranch, allocateErr))
		}
		r.Branches[activeBranch] = rec
		assignments[activeBranch] = assignment
	}
	assignment, ok := assignments[branch]
	if !ok {
		return nil, fmt.Errorf("no port assignment found for branch %q", branch)
	}
	return assignment, nil
}

func reconcilePortRegistry(pc *projectContext) error {
	if len(pc.Config.Services) == 0 {
		if _, err := os.Stat(ports.StorePath(pc.CommonStateDir)); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
	}
	active, err := activeBranchNames(pc, "")
	if err != nil {
		return err
	}
	return ports.NewStore(pc.CommonStateDir).Reconcile(active)
}

func findWorktreeByBranch(ctx *projectContext, branch string) (*worktree.Info, error) {
	worktrees, err := ctx.Worktrees.List()
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}

	for _, wt := range worktrees {
		if wt.Branch == branch {
			found := wt
			return &found, nil
		}
	}

	return nil, fmt.Errorf("no worktree found for branch %q — create it first with: grove create %s", branch, branch)
}

func syncWorktreeEnv(cfg *config.Config, projectRoot, worktreePath string, managed *env.ManagedEnv) error {
	sameRoot, err := sameWorktreeEnvRoot(projectRoot, worktreePath)
	if err != nil {
		return fmt.Errorf("checking worktree environment root identity: %w", err)
	}
	if sameRoot {
		return nil
	}

	allEnvFiles := cfg.AllEnvFiles()
	if len(allEnvFiles) == 0 {
		return nil
	}

	mappings, err := managed.EnvLocalMappings(cfg, projectRoot)
	if err != nil {
		return fmt.Errorf("building .env.local mappings: %w", err)
	}
	// Preflight both mutation classes before applying either, so a deterministic
	// local-file collision cannot leave env symlinks partially synchronized.
	if err := env.PreflightEnvFiles(allEnvFiles, projectRoot, worktreePath); err != nil {
		return fmt.Errorf("preflighting env files: %w", err)
	}
	if err := env.PreflightEnvLocals(mappings, worktreePath); err != nil {
		return fmt.Errorf("preflighting managed env files: %w", err)
	}
	if err := env.SymlinkEnvFiles(allEnvFiles, projectRoot, worktreePath); err != nil {
		return fmt.Errorf("synchronizing env files: %w", err)
	}
	if len(mappings) > 0 {
		if err := env.WriteEnvLocals(mappings, worktreePath); err != nil {
			return fmt.Errorf("writing managed env files: %w", err)
		}
	}

	return nil
}

var (
	workflowEvalSymlinks = filepath.EvalSymlinks
	workflowStat         = os.Stat
)

func sameWorktreeEnvRoot(projectRoot, worktreePath string) (bool, error) {
	projectAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return false, err
	}
	worktreeAbs, err := filepath.Abs(worktreePath)
	if err != nil {
		return false, err
	}

	projectClean := filepath.Clean(projectAbs)
	worktreeClean := filepath.Clean(worktreeAbs)
	if projectClean == worktreeClean {
		return true, nil
	}

	projectResolved, projectResolveErr := workflowEvalSymlinks(projectClean)
	worktreeResolved, worktreeResolveErr := workflowEvalSymlinks(worktreeClean)
	switch {
	case projectResolveErr == nil && worktreeResolveErr == nil:
		if filepath.Clean(projectResolved) == filepath.Clean(worktreeResolved) {
			return true, nil
		}
	case projectResolveErr != nil || worktreeResolveErr != nil:
		// Fail closed: if alias resolution is ambiguous, skip on-disk env mutation.
		return true, nil
	}

	projectInfo, projectStatErr := workflowStat(projectClean)
	worktreeInfo, worktreeStatErr := workflowStat(worktreeClean)
	if projectStatErr != nil || worktreeStatErr != nil {
		// Existing roots should stat. If they do not, equality cannot be safely ruled out.
		return true, nil
	}
	return os.SameFile(projectInfo, worktreeInfo), nil
}

func configuredHookOutputMode(cfg *config.HooksConfig) hooks.OutputMode {
	if cfg == nil {
		return hooks.OutputSummary
	}
	switch cfg.Output {
	case "stream":
		return hooks.OutputStream
	case "quiet":
		return hooks.OutputQuiet
	default:
		return hooks.OutputSummary
	}
}

func effectiveTmuxConfig(cfg *config.Config) *config.TmuxConfig {
	tmuxCfg := cfg.Tmux
	if tmuxCfg == nil {
		tmuxCfg = &config.TmuxConfig{}
	}
	return tmuxCfg
}
