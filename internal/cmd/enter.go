package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

type shellLaunchOptions struct {
	Shell string
	Dir   string
	Env   []string
}

var shellLauncher = func(opts shellLaunchOptions) error {
	cmd := exec.Command(opts.Shell)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func newEnterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enter <branch>",
		Short: "Enter a worktree in the current pane",
		Long: `Start an interactive subshell in an existing Grove worktree without
creating or switching tmux sessions/windows. The shell receives Grove-managed
environment variables and GROVE_BRANCH, GROVE_WORKTREE, and GROVE_PROJECT_ROOT
markers. Type exit or press Ctrl-D to return.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return enterBranch(cmd, args[0])
		},
	}
}

func enterBranch(cmd *cobra.Command, branch string) error {
	ctx, err := loadProjectContext()
	if err != nil {
		return outputError(cmd, err)
	}

	found, err := findWorktreeByBranch(ctx, branch)
	if err != nil {
		return outputError(cmd, err)
	}

	_, managed, err := resolveRuntimeEnv(ctx, branch)
	if err != nil {
		return outputError(cmd, err)
	}

	if err := syncWorktreeEnv(cmd, ctx.Config, ctx.ProjectRoot, found.Path, managed); err != nil {
		return outputError(cmd, err)
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	envVars := append([]string{}, os.Environ()...)
	for key, val := range managed.SessionEnv() {
		envVars = append(envVars, key+"="+val)
	}
	envVars = append(envVars,
		"GROVE_BRANCH="+branch,
		"GROVE_WORKTREE="+found.Path,
		"GROVE_PROJECT_ROOT="+ctx.ProjectRoot,
	)

	fmt.Fprintf(cmd.OutOrStdout(), "Entering Grove worktree %q at %s — type exit or Ctrl-D to return.\n", branch, found.Path)
	if err := shellLauncher(shellLaunchOptions{Shell: shell, Dir: found.Path, Env: envVars}); err != nil {
		return outputError(cmd, fmt.Errorf("launching shell: %w", err))
	}
	return nil
}
