package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"grove/internal/config"
	"grove/internal/env"
	"grove/internal/ports"
	"grove/internal/worktree"

	"github.com/spf13/cobra"
)

// statusOutput is the structured JSON output for grove status --json.
type statusOutput struct {
	Branch   string            `json:"branch"`
	Worktree string            `json:"worktree"`
	Ports    map[string]int    `json:"ports"`
	Env      map[string]string `json:"env"`
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show info for the current worktree",
		Long:  `Show branch, worktree path, port assignments, and environment variables for the current worktree (detected from cwd).`,
		Args:  cobra.NoArgs,
		RunE:  runStatus,
	}

	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	jsonOutput := shouldOutputJSON(cmd)

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

	// Step 2: Detect current worktree from cwd
	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)

	wtInfo, err := wtMgr.FindByPath(cwd)
	if err != nil {
		return outputError(cmd, fmt.Errorf("not inside a grove worktree: %w", err))
	}

	// Step 3: Compute ports for this branch
	var assignedPorts map[string]int
	if len(cfg.Services) > 0 {
		assignment, err := ports.Assign(cfg.Services, wtInfo.Branch, ports.DefaultMaxOffset)
		if err != nil {
			return outputError(cmd, fmt.Errorf("assigning ports: %w", err))
		}
		assignedPorts = assignment.Ports
	} else {
		assignedPorts = map[string]int{}
	}

	// Step 4: Resolve environment variables
	resolvedEnv, err := env.Resolve(cfg, assignedPorts, projectRoot, nil)
	if err != nil {
		return outputError(cmd, fmt.Errorf("resolving environment: %w", err))
	}

	// Step 5: Output
	if jsonOutput {
		out := statusOutput{
			Branch:   wtInfo.Branch,
			Worktree: wtInfo.Path,
			Ports:    assignedPorts,
			Env:      resolvedEnv,
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return outputError(cmd, fmt.Errorf("marshaling JSON: %w", err))
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Branch:   %s\n", wtInfo.Branch)
	fmt.Fprintf(w, "Worktree: %s\n", wtInfo.Path)

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

	if len(resolvedEnv) > 0 {
		fmt.Fprintln(w, "Env:")
		keys := make([]string, 0, len(resolvedEnv))
		for k := range resolvedEnv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := resolvedEnv[k]
			if len(v) > 100 {
				v = v[:97] + "..."
			}
			fmt.Fprintf(w, "  %s=%s\n", k, v)
		}
	}

	return nil
}
