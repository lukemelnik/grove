package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

// statusOutput is the structured JSON output for grove status --json.
type statusOutput struct {
	Branch   string         `json:"branch"`
	Worktree string         `json:"worktree"`
	Ports    map[string]int `json:"ports"`
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

	cwd, err := getWorkingDir()
	if err != nil {
		return outputError(cmd, fmt.Errorf("getting working directory: %w", err))
	}
	ctx, err := loadProjectContext()
	if err != nil {
		return outputError(cmd, err)
	}
	wtInfo, err := ctx.Worktrees.FindByPath(cwd)
	if err != nil {
		return outputError(cmd, fmt.Errorf("not inside a grove worktree: %w", err))
	}

	assignment, err := readPortAssignment(ctx, wtInfo.Branch)
	if err != nil {
		return outputError(cmd, fmt.Errorf("assigning ports: %w", err))
	}
	assignedPorts := assignment.Ports

	// Step 4: Output
	if jsonOutput {
		out := statusOutput{
			Branch:   wtInfo.Branch,
			Worktree: wtInfo.Path,
			Ports:    assignedPorts,
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

	return nil
}
