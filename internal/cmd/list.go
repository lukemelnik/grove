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

// listEntry is a single worktree entry for list output.
type listEntry struct {
	Branch    string            `json:"branch"`
	Worktree  string            `json:"worktree"`
	Ports     map[string]int    `json:"ports"`
	ProxyURLs map[string]string `json:"proxy_urls,omitempty"`
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active worktrees with their branch, path, and ports",
		Long:  `List all active grove-managed worktrees with their branch names, paths, and port assignments.`,
		Args:  cobra.NoArgs,
		RunE:  runList,
	}

	cmd.Flags().Bool("json", false, "output as JSON (agent-friendly)")

	return cmd
}

func runList(cmd *cobra.Command, args []string) error {
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

	// Step 2: List worktrees
	git := worktree.NewGitRunner(projectRoot)
	wtMgr := worktree.NewManager(git, projectRoot, cfg.WorktreeDir)

	worktrees, err := wtMgr.List()
	if err != nil {
		return outputError(cmd, fmt.Errorf("listing worktrees: %w", err))
	}

	// Step 3: Compute ports for each worktree and build entries
	// Filter out bare repos and the main worktree (same as project root)
	defaultBranch := wtMgr.DefaultBranch()
	proxyInfo := env.ProxyInfoFromConfig(cfg.Proxy, projectRoot, defaultBranch)
	var entries []listEntry
	for _, wt := range worktrees {
		if wt.IsBare || wt.Branch == "" {
			continue
		}

		entry := listEntry{
			Branch:   wt.Branch,
			Worktree: wt.Path,
			Ports:    map[string]int{},
		}

		if len(cfg.Services) > 0 {
			assignment, err := ports.Assign(cfg.Services, wt.Branch, ports.DefaultMaxOffset, defaultBranch)
			if err == nil {
				entry.Ports = assignment.Ports
			}

			if proxyInfo != nil {
				proxyURLs := make(map[string]string)
				for name := range assignment.Ports {
					if urlStr, urlErr := proxyInfo.BuildProxyURL(name, wt.Branch); urlErr == nil {
						proxyURLs[name] = urlStr
					}
				}
				if len(proxyURLs) > 0 {
					entry.ProxyURLs = proxyURLs
				}
			}
		}

		entries = append(entries, entry)
	}

	// Sort entries by branch name for deterministic output
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Branch < entries[j].Branch
	})

	// Step 4: Output
	if jsonOutput {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return outputError(cmd, fmt.Errorf("marshaling JSON: %w", err))
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	w := cmd.OutOrStdout()
	if len(entries) == 0 {
		fmt.Fprintln(w, "No active worktrees")
		return nil
	}

	for i, entry := range entries {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "Branch:   %s\n", entry.Branch)
		fmt.Fprintf(w, "Worktree: %s\n", entry.Worktree)
		if len(entry.Ports) > 0 {
			fmt.Fprintln(w, "Ports:")
			names := make([]string, 0, len(entry.Ports))
			for name := range entry.Ports {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				fmt.Fprintf(w, "  %s: %d\n", name, entry.Ports[name])
			}
		}
		if len(entry.ProxyURLs) > 0 {
			fmt.Fprintln(w, "Proxy:")
			names := make([]string, 0, len(entry.ProxyURLs))
			for name := range entry.ProxyURLs {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				fmt.Fprintf(w, "  %s: %s\n", name, entry.ProxyURLs[name])
			}
		}
	}

	return nil
}
