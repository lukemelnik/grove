package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/env"
	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

// statusOutput is the structured JSON output for grove status --json.
type statusOutput struct {
	Branch    string            `json:"branch"`
	Worktree  string            `json:"worktree"`
	Ports     map[string]int    `json:"ports"`
	ProxyURLs map[string]string `json:"proxy_urls,omitempty"`
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show info for the current worktree",
		Long:  `Show branch, worktree path, and port assignments for the current worktree (detected from cwd).`,
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
	defaultBranch := wtMgr.DefaultBranch()
	var assignedPorts map[string]int
	if len(cfg.Services) > 0 {
		assignment, err := ports.Assign(cfg.Services, wtInfo.Branch, ports.DefaultMaxOffset, defaultBranch)
		if err != nil {
			return outputError(cmd, fmt.Errorf("assigning ports: %w", err))
		}
		assignedPorts = assignment.Ports
	} else {
		assignedPorts = map[string]int{}
	}

	// Step 4: Compute proxy URLs if proxy is configured
	proxyInfo := env.ProxyInfoFromConfig(cfg.Proxy, projectRoot, defaultBranch)
	var proxyURLs map[string]string
	if proxyInfo != nil && len(assignedPorts) > 0 {
		proxyURLs = make(map[string]string)
		for name := range assignedPorts {
			if urlStr, urlErr := proxyInfo.BuildProxyURL(name, wtInfo.Branch); urlErr == nil {
				proxyURLs[name] = urlStr
			}
		}
	}

	// Step 5: Output
	if jsonOutput {
		out := statusOutput{
			Branch:    wtInfo.Branch,
			Worktree:  wtInfo.Path,
			Ports:     assignedPorts,
			ProxyURLs: proxyURLs,
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

	if len(proxyURLs) > 0 {
		fmt.Fprintln(w, "Proxy:")
		names := make([]string, 0, len(proxyURLs))
		for name := range proxyURLs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(w, "  %s: %s\n", name, proxyURLs[name])
		}
	}

	return nil
}
