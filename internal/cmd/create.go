package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/env"
	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/tmux"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

// createOutput is the structured JSON output for grove create --json.
type createOutput struct {
	Worktree string         `json:"worktree"`
	Branch   string         `json:"branch"`
	Ports    map[string]int `json:"ports"`
}

func newCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <branch>",
		Short: "Create a worktree for the given branch",
		Long: `Create a git worktree with deterministic port assignment and optional
tmux workspace. If a tmux config is present in .grove.yml, sets up
a tmux session/window with panes and environment variables.

Branch resolution:
  1. Branch exists locally — use it
  2. Branch exists on remote — fetch and create local tracking branch
  3. Branch is new — create from --from ref (default: origin/main)

Optional panes:
  Panes marked with "optional: true" in .grove.yml are skipped by default.
  Include them with --all (all optional panes) or --with <name> (by name).

  Example .grove.yml:
    tmux:
      panes:
        - nvim
        - cmd: pnpm dev
          name: dev
          optional: true

Run 'grove schema' for the full .grove.yml configuration reference.`,
		Args: cobra.ExactArgs(1),
		RunE: runCreate,
	}

	cmd.Flags().String("from", "", "base branch for new branches (default: origin/main)")
	cmd.Flags().Bool("no-tmux", false, "skip tmux, just create worktree and print info")
	cmd.Flags().Bool("all", false, "include optional panes")
	cmd.Flags().StringArray("with", nil, "include specific optional pane(s)")
	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")
	cmd.Flags().Bool("attach", true, "auto-attach to the tmux session/window")

	return cmd
}

// tmuxRunnerFactory creates a tmux runner. It is a var so tests can override it.
var tmuxRunnerFactory = func() tmux.Runner {
	return tmux.NewRunner()
}

func runCreate(cmd *cobra.Command, args []string) error {
	branch := args[0]

	// Parse flags
	fromRef, _ := cmd.Flags().GetString("from")
	noTmux, _ := cmd.Flags().GetBool("no-tmux")
	explicitJSON, _ := cmd.Flags().GetBool("json")
	jsonOutput := shouldOutputJSON(cmd)
	includeAll, _ := cmd.Flags().GetBool("all")
	includeWith, _ := cmd.Flags().GetStringArray("with")
	attach, _ := cmd.Flags().GetBool("attach")
	if jsonOutput && !explicitJSON && !cmd.Flags().Changed("attach") {
		// In auto-JSON mode we are typically being driven by another program, so
		// set up tmux but avoid switching the caller into an interactive session.
		attach = false
	}

	// Step 1: Discover and load config
	cwd, err := getWorkingDir()
	if err != nil {
		return outputError(cmd, fmt.Errorf("getting working directory: %w", err))
	}

	configPath, projectRoot, err := config.Discover(cwd)
	if err != nil {
		return outputError(cmd, err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return outputError(cmd, err)
	}

	// Step 2: Detect default branch and assign ports
	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)
	defaultBranch := wtMgr.DefaultBranch()

	var portAssignment *ports.Assignment
	if len(cfg.Services) > 0 {
		portAssignment, err = ports.Assign(cfg.Services, branch, ports.DefaultMaxOffset, defaultBranch)
		if err != nil {
			return outputError(cmd, fmt.Errorf("assigning ports: %w", err))
		}
	} else {
		portAssignment = &ports.Assignment{Ports: map[string]int{}}
	}

	// Step 3: Resolve managed env before making any filesystem changes so a bad
	// template fails fast instead of creating a partial worktree.
	managed, err := env.BuildManagedEnv(cfg, portAssignment.Ports, branch)
	if err != nil {
		return outputError(cmd, fmt.Errorf("resolving managed environment: %w", err))
	}

	// Step 4: Create worktree with branch resolution
	result, err := wtMgr.Create(branch, fromRef)
	if err != nil {
		return outputError(cmd, fmt.Errorf("creating worktree: %w", err))
	}

	// Step 5: Output worktree details before attach so interactive users still
	// see the workspace info before tmux takes over.
	if jsonOutput {
		if err := outputJSON(cmd, result, portAssignment.Ports); err != nil {
			return outputError(cmd, err)
		}
	} else if err := outputText(cmd, result, portAssignment.Ports); err != nil {
		return outputError(cmd, err)
	}

	// Step 6: Symlink .env files and write .env.local with managed vars
	allEnvFiles := cfg.AllEnvFiles()
	if len(allEnvFiles) > 0 {
		if err := env.SymlinkEnvFiles(allEnvFiles, projectRoot, result.Path); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not symlink env files: %v\n", err)
		}
		mappings, err := managed.EnvLocalMappings(cfg, projectRoot)
		if err != nil {
			return outputError(cmd, fmt.Errorf("building .env.local mappings: %w", err))
		}
		if len(mappings) > 0 {
			if err := env.WriteEnvLocals(mappings, result.Path); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not write .env.local files: %v\n", err)
			}
		}
	}

	// Step 7: Tmux workspace setup (default to empty config if not specified)
	if !noTmux {
		tmuxCfg := cfg.Tmux
		if tmuxCfg == nil {
			tmuxCfg = &config.TmuxConfig{}
		}

		tmuxMgr := tmux.NewManager(tmuxRunnerFactory())
		tmuxOpts := tmux.Options{
			Branch:       branch,
			WorktreePath: result.Path,
			Env:          managed.SessionEnv(),
			TmuxConfig:   tmuxCfg,
			IncludeAll:   includeAll,
			IncludeWith:  includeWith,
			Attach:       attach,
		}
		if err := tmuxMgr.Create(tmuxOpts); err != nil {
			return outputError(cmd, fmt.Errorf("setting up tmux workspace: %w", err))
		}
	} else {
		if !jsonOutput {
			fmt.Fprintf(cmd.OutOrStdout(), "\n  cd %s\n", result.Path)
		}
	}

	return nil
}

func outputJSON(cmd *cobra.Command, result *worktree.CreateResult, assignedPorts map[string]int) error {
	out := createOutput{
		Worktree: result.Path,
		Branch:   result.Branch,
		Ports:    assignedPorts,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func outputText(cmd *cobra.Command, result *worktree.CreateResult, assignedPorts map[string]int) error {
	w := cmd.OutOrStdout()

	if result.Created {
		fmt.Fprintf(w, "Created worktree for branch %q (%s)\n", result.Branch, result.Resolution)
	} else {
		fmt.Fprintf(w, "Reusing existing worktree for branch %q\n", result.Branch)
	}

	fmt.Fprintf(w, "Worktree: %s\n", result.Path)

	if len(assignedPorts) > 0 {
		fmt.Fprintln(w, "Ports:")
		names := make([]string, 0, len(assignedPorts))
		for name := range assignedPorts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(w, "  %s: %d\n", name, assignedPorts[name])
		}
	}

	return nil
}

// getWorkingDir returns the current working directory.
// This is a var so tests can override it.
var getWorkingDir = os.Getwd
