package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"grove/internal/config"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// stdinReader is the reader used for interactive input.
// It is a var so tests can override it with mock input.
var stdinReader io.Reader = os.Stdin

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a new .grove.yml configuration",
		Long:  `Interactively create a .grove.yml in the current directory by asking about services, ports, and tmux preferences.`,
		Args:  cobra.NoArgs,
		RunE:  runInit,
	}
}

func runInit(cmd *cobra.Command, args []string) error {
	w := cmd.OutOrStdout()
	scanner := bufio.NewScanner(stdinReader)

	// Check if .grove.yml already exists in cwd
	cwd, err := getWorkingDir()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	configPath := filepath.Join(cwd, config.ConfigFileName)
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(w, "A %s already exists in this directory. Overwrite? [y/N] ", config.ConfigFileName)
		if !scanYesNo(scanner, false) {
			fmt.Fprintln(w, "Aborted.")
			return nil
		}
	}

	cfg := config.Config{}

	// --- Worktree directory ---
	fmt.Fprintf(w, "\nWorktree directory (relative to project root) [../.grove-worktrees]: ")
	wtDir := scanLine(scanner)
	if wtDir == "" {
		wtDir = "../.grove-worktrees"
	}
	cfg.WorktreeDir = wtDir

	// --- Env files ---
	fmt.Fprintf(w, "\nEnv files to include (comma or space separated, e.g. .env apps/api/.env) [.env]: ")
	envFilesInput := scanLine(scanner)
	if envFilesInput == "" {
		// Default: include .env if it exists
		if _, err := os.Stat(filepath.Join(cwd, ".env")); err == nil {
			cfg.EnvFiles = []string{".env"}
		}
	} else {
		// Split on commas, spaces, or both
		parts := strings.FieldsFunc(envFilesInput, func(r rune) bool {
			return r == ',' || r == ' '
		})
		for _, p := range parts {
			if p != "" {
				cfg.EnvFiles = append(cfg.EnvFiles, p)
			}
		}
	}

	// --- Services ---
	fmt.Fprintf(w, "\n--- Services ---\n")
	fmt.Fprintf(w, "Define services with base ports for deterministic port assignment.\n")

	cfg.Services = map[string]config.Service{}
	for {
		fmt.Fprintf(w, "\nAdd a service? [Y/n] ")
		if !scanYesNo(scanner, true) {
			break
		}

		fmt.Fprintf(w, "  Service name (e.g. api, web): ")
		name := scanLine(scanner)
		if name == "" {
			fmt.Fprintln(w, "  Skipped (empty name).")
			continue
		}

		fmt.Fprintf(w, "  Base port for %s (e.g. 4000): ", name)
		portStr := scanLine(scanner)
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			fmt.Fprintf(w, "  Invalid port %q, skipping service.\n", portStr)
			continue
		}

		// Default env var name: uppercase service name + "_PORT" or just "PORT" for single service
		defaultEnv := strings.ToUpper(name) + "_PORT"
		if len(cfg.Services) == 0 {
			defaultEnv = "PORT"
		}
		fmt.Fprintf(w, "  Env var name for port [%s]: ", defaultEnv)
		envName := scanLine(scanner)
		if envName == "" {
			envName = defaultEnv
		}

		cfg.Services[name] = config.Service{
			Port: port,
			Env:  envName,
		}
		fmt.Fprintf(w, "  Added service %q (port %d, env %s)\n", name, port, envName)
	}

	if len(cfg.Services) == 0 {
		cfg.Services = nil
	}

	// --- Tmux configuration ---
	fmt.Fprintf(w, "\n--- Tmux Configuration ---\n")
	fmt.Fprintf(w, "Include tmux workspace configuration? [y/N] ")
	if scanYesNo(scanner, false) {
		tmuxCfg := &config.TmuxConfig{}

		fmt.Fprintf(w, "  Mode (session/window) [window]: ")
		mode := scanLine(scanner)
		if mode == "" {
			mode = "window"
		}
		if mode != "session" && mode != "window" {
			fmt.Fprintf(w, "  Invalid mode %q, using \"window\".\n", mode)
			mode = "window"
		}
		tmuxCfg.Mode = mode

		fmt.Fprintf(w, "  Layout preset (even-horizontal, even-vertical, main-horizontal, main-vertical, tiled) [main-vertical]: ")
		layout := scanLine(scanner)
		if layout == "" {
			layout = "main-vertical"
		}
		tmuxCfg.Layout = layout

		fmt.Fprintf(w, "  Main pane size (e.g. 70%%) []: ")
		mainSize := scanLine(scanner)
		if mainSize != "" {
			tmuxCfg.MainSize = mainSize
		}

		// Panes
		fmt.Fprintf(w, "\n  Define panes (commands to run in each pane).\n")
		for {
			fmt.Fprintf(w, "  Add a pane? [Y/n] ")
			if !scanYesNo(scanner, true) {
				break
			}

			fmt.Fprintf(w, "    Command (e.g. nvim, pnpm dev): ")
			paneCmd := scanLine(scanner)
			if paneCmd == "" {
				fmt.Fprintln(w, "    Skipped (empty command).")
				continue
			}

			fmt.Fprintf(w, "    Optional pane? [y/N] ")
			optional := scanYesNo(scanner, false)

			pane := config.Pane{Cmd: paneCmd}
			if optional {
				pane.Optional = true
			}
			tmuxCfg.Panes = append(tmuxCfg.Panes, pane)
		}

		cfg.Tmux = tmuxCfg
	}

	// --- Write config ---
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	// Add a header comment
	header := "# Grove configuration — see https://github.com/grovewtm/grove\n\n"
	content := header + string(data)

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", config.ConfigFileName, err)
	}

	fmt.Fprintf(w, "\nWrote %s\n", configPath)
	return nil
}

// scanLine reads a single line from the scanner and trims whitespace.
func scanLine(scanner *bufio.Scanner) string {
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// scanYesNo reads a yes/no answer. Returns the default if input is empty.
func scanYesNo(scanner *bufio.Scanner, defaultYes bool) bool {
	input := scanLine(scanner)
	switch strings.ToLower(input) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultYes
	}
}
