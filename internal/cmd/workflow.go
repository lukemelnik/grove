package cmd

import (
	"fmt"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/env"
	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

type projectContext struct {
	ConfigPath  string
	ProjectRoot string
	Config      *config.Config
	Worktrees   *worktree.Manager
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

	return &projectContext{
		ConfigPath:  configPath,
		ProjectRoot: projectRoot,
		Config:      cfg,
		Worktrees:   wtMgr,
	}, nil
}

func resolveRuntimeEnv(ctx *projectContext, branch string) (*ports.Assignment, *env.ManagedEnv, error) {
	defaultBranch := ctx.Worktrees.DefaultBranch()

	var portAssignment *ports.Assignment
	var err error
	if len(ctx.Config.Services) > 0 {
		portAssignment, err = ports.Assign(ctx.Config.Services, branch, ports.DefaultMaxOffset, defaultBranch)
		if err != nil {
			return nil, nil, fmt.Errorf("assigning ports: %w", err)
		}
	} else {
		portAssignment = &ports.Assignment{Ports: map[string]int{}}
	}

	managed, err := env.BuildManagedEnv(ctx.Config, portAssignment.Ports, branch)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving managed environment: %w", err)
	}

	return portAssignment, managed, nil
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

func syncWorktreeEnv(cmd *cobra.Command, cfg *config.Config, projectRoot, worktreePath string, managed *env.ManagedEnv) error {
	allEnvFiles := cfg.AllEnvFiles()
	if len(allEnvFiles) == 0 {
		return nil
	}

	if err := env.SymlinkEnvFiles(allEnvFiles, projectRoot, worktreePath); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not symlink env files: %v\n", err)
	}

	mappings, err := managed.EnvLocalMappings(cfg, projectRoot)
	if err != nil {
		return fmt.Errorf("building .env.local mappings: %w", err)
	}
	if len(mappings) > 0 {
		if err := env.WriteEnvLocals(mappings, worktreePath); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not write .env.local files: %v\n", err)
		}
	}

	return nil
}

func effectiveTmuxConfig(cfg *config.Config) *config.TmuxConfig {
	tmuxCfg := cfg.Tmux
	if tmuxCfg == nil {
		tmuxCfg = &config.TmuxConfig{}
	}
	return tmuxCfg
}
