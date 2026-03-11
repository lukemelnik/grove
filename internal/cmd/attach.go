package cmd

import (
	"fmt"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/env"
	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

func newAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <branch>",
		Short: "Attach to an existing worktree's tmux session/window",
		Long: `Jump back to an existing worktree. If a tmux session/window is running,
attach to it. If the worktree exists but no tmux session/window is active,
create one and attach.`,
		Args: cobra.ExactArgs(1),
		RunE: runAttach,
	}
}

func runAttach(cmd *cobra.Command, args []string) error {
	branch := args[0]

	// Step 1: Discover and load config
	cwd, err := getWorkingDir()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	configPath, projectRoot, err := config.Discover(cwd)
	if err != nil {
		return err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Step 2: Check if worktree exists for this branch
	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)

	worktrees, err := wtMgr.List()
	if err != nil {
		return fmt.Errorf("listing worktrees: %w", err)
	}

	var found *worktree.Info
	for _, wt := range worktrees {
		if wt.Branch == branch {
			found = &wt
			break
		}
	}

	if found == nil {
		return fmt.Errorf("no worktree found for branch %q — did you mean `grove create %s`?", branch, branch)
	}

	// Step 3: Default to empty tmux config if not specified
	tmuxCfg := cfg.Tmux
	if tmuxCfg == nil {
		tmuxCfg = &config.TmuxConfig{}
	}

	// Step 4: Check if tmux session/window already exists
	tmuxRunner := tmuxRunnerFactory()
	tmuxMgr := tmux.NewManager(tmuxRunner)

	mode := tmuxCfg.Mode
	if mode == "" {
		mode = "window"
	}

	name := tmuxMgr.ResolveName(branch, mode)
	sessionExists := tmuxMgr.HasSession(name)
	windowExists := tmuxMgr.HasWindow(name)

	if (mode == "session" && sessionExists) || (mode == "window" && windowExists) {
		// Session/window is running — just attach/switch
		return tmuxMgr.Attach(name, mode)
	}

	// Worktree exists but no tmux session/window — create one

	// Resolve ports and env for this branch
	defaultBranch := wtMgr.DefaultBranch()
	var portAssignment *ports.Assignment
	if len(cfg.Services) > 0 {
		portAssignment, err = ports.Assign(cfg.Services, branch, ports.DefaultMaxOffset, defaultBranch)
		if err != nil {
			return fmt.Errorf("assigning ports: %w", err)
		}
	} else {
		portAssignment = &ports.Assignment{Ports: map[string]int{}}
	}

	// Ensure .env files are symlinked and .env.local is up to date
	if len(cfg.EnvFiles) > 0 {
		if err := env.SymlinkEnvFiles(cfg.EnvFiles, projectRoot, found.Path); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not symlink env files: %v\n", err)
		}
		managed := env.ManagedVars(cfg, portAssignment.Ports)
		if len(managed) > 0 {
			mappings := env.MapManagedToEnvFiles(cfg, managed, projectRoot)
			if err := env.WriteEnvLocals(mappings, found.Path); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not write .env.local files: %v\n", err)
			}
		}
	}

	managed := env.ManagedVars(cfg, portAssignment.Ports)

	tmuxOpts := tmux.Options{
		Branch:       branch,
		WorktreePath: found.Path,
		Env:          managed,
		TmuxConfig:   tmuxCfg,
		IncludeAll:   false,
		IncludeWith:  nil,
		Attach:       true,
	}
	if err := tmuxMgr.Create(tmuxOpts); err != nil {
		return fmt.Errorf("setting up tmux workspace: %w", err)
	}

	return nil
}
