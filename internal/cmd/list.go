package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/lukemelnik/grove/internal/config"
	"github.com/lukemelnik/grove/internal/ports"
	"github.com/lukemelnik/grove/internal/worktree"

	"github.com/spf13/cobra"
)

// listEntry is a single worktree entry for list output.
type listEntry struct {
	Branch   string         `json:"branch"`
	Worktree string         `json:"worktree"`
	Ports    map[string]int `json:"ports"`
}

type listPickerAction string

const (
	listActionOpen          listPickerAction = "open"
	listActionEnter         listPickerAction = "enter"
	listActionOpenNewWindow listPickerAction = "open-new-window"
	listActionDelete        listPickerAction = "delete"
)

type listPickerSelection struct {
	Action listPickerAction
	Branch string
}

var fzfLookPath = exec.LookPath
var listPicker = defaultListPicker
var listActionDispatcher = dispatchListAction

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active worktrees with their branch, path, and ports",
		Long: `List all active grove-managed worktrees with their branch names, paths, and port assignments.

In an interactive terminal, Grove uses fzf when available:
  Enter   open the full tmux workspace
  Ctrl-P  enter the worktree in the current pane
  Ctrl-W  open an additional tmux window
  Ctrl-D  confirm and delete the worktree

Agents and scripts should use grove list --json and avoid the interactive picker.`,
		Args: cobra.NoArgs,
		RunE: runList,
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

	entries := buildListEntries(cfg, wtMgr, worktrees)

	// Step 4: Output
	if jsonOutput {
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return outputError(cmd, fmt.Errorf("marshaling JSON: %w", err))
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	if canUseInteractiveList(cmd) {
		selection, err := listPicker(entries)
		if err != nil {
			return outputError(cmd, err)
		}
		if selection != nil {
			return listActionDispatcher(cmd, selection.Action, selection.Branch)
		}
	}

	printListText(cmd, entries)
	return nil
}

func buildListEntries(cfg *config.Config, wtMgr *worktree.Manager, worktrees []worktree.Info) []listEntry {
	// Compute ports for each worktree and build entries.
	// Filter out bare repos and detached worktrees.
	defaultBranch := wtMgr.DefaultBranch()
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
		}

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Branch < entries[j].Branch
	})
	return entries
}

func printListText(cmd *cobra.Command, entries []listEntry) {
	w := cmd.OutOrStdout()
	if len(entries) == 0 {
		fmt.Fprintln(w, "No active worktrees")
		return
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
	}
}

func canUseInteractiveList(cmd *cobra.Command) bool {
	if !isTerminal(int(os.Stdout.Fd())) {
		return false
	}
	return cmd.OutOrStdout() == os.Stdout
}

func defaultListPicker(entries []listEntry) (*listPickerSelection, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if _, err := fzfLookPath("fzf"); err != nil {
		return nil, nil
	}

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.Branch+"\t"+entry.Worktree)
	}

	fzf := exec.Command("fzf", "--expect=enter,ctrl-p,ctrl-w,ctrl-d", "--prompt=Grove> ", "--header=Enter open | Ctrl-P enter | Ctrl-W new window | Ctrl-D delete")
	fzf.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	fzf.Stderr = os.Stderr
	out, err := fzf.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, nil
		}
		return nil, fmt.Errorf("running fzf: %w", err)
	}

	return parseListPickerOutput(string(out))
}

func parseListPickerOutput(output string) (*listPickerSelection, error) {
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return nil, nil
	}

	lines := strings.Split(output, "\n")
	key := "enter"
	selected := lines[0]
	if len(lines) >= 2 {
		key = strings.TrimSpace(lines[0])
		selected = lines[1]
	}
	branch := strings.SplitN(selected, "\t", 2)[0]
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil, nil
	}

	action := listActionOpen
	switch key {
	case "", "enter":
		action = listActionOpen
	case "ctrl-p":
		action = listActionEnter
	case "ctrl-w":
		action = listActionOpenNewWindow
	case "ctrl-d":
		action = listActionDelete
	default:
		return nil, fmt.Errorf("unsupported picker action %q", key)
	}

	return &listPickerSelection{Action: action, Branch: branch}, nil
}

func dispatchListAction(cmd *cobra.Command, action listPickerAction, branch string) error {
	switch action {
	case listActionOpen:
		return openBranch(cmd, branch, false)
	case listActionEnter:
		return enterBranch(cmd, branch)
	case listActionOpenNewWindow:
		return openBranch(cmd, branch, true)
	case listActionDelete:
		fmt.Fprintf(cmd.OutOrStdout(), "Delete worktree for branch %q? [y/N] ", branch)
		scanner := bufio.NewScanner(stdinReader)
		if !scanYesNo(scanner, false) {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
		deleteCmd := newDeleteCmd()
		deleteCmd.SetOut(cmd.OutOrStdout())
		deleteCmd.SetErr(cmd.ErrOrStderr())
		return runDelete(deleteCmd, []string{branch})
	default:
		return fmt.Errorf("unsupported list action %q", action)
	}
}
