package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lukemelnik/grove/internal/config"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// stdinReader is the reader used for interactive input.
// It is a var so tests can override it with mock input.
var stdinReader io.Reader = os.Stdin

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new .grove.yml configuration",
		Long: `Create a .grove.yml in the current directory.

By default, runs interactively. Use flags for non-interactive setup
(useful for agents and scripts).

Non-interactive examples:
  grove init --service api:4000:PORT --service web:3000:WEB_PORT
  grove init --service api:4000:PORT --env-file .env --pane nvim --pane "pnpm dev"
  grove init --service api:4000:PORT --pane nvim --pane "pnpm dev:dev:optional"

Service format:  name:port:ENV_VAR
Pane format:     command[:name[:optional]]

If --worktree-dir is omitted, Grove defaults to ../.grove-worktrees/<repo-name>.
Set it only when you want a different location.

Use --env-file for shared/root-level env symlinks (top-level env_files in .grove.yml),
for example .env or .env.apple. Service-scoped env files like apps/api/.env
belong under services.<name>.env_file in YAML.

The --pane flag only creates a flat pane list. For nested tmux split layouts,
edit the generated .grove.yml (or start from 'grove schema').

Tmux explicit split rules in .grove.yml:
  split: horizontal => children go left-to-right
  split: vertical   => children go top-to-bottom
  Child order matters: first child is left/top, second is right/bottom.
  size applies along the split axis (width for horizontal, height for vertical).

  Example: two side-by-side pi panes with a small full-width terminal on the bottom:
    tmux:
      panes:
        - split: vertical
          panes:
            - split: horizontal
              panes:
                - pi
                - pi
            - cmd: ""
              name: terminal
              size: "20%"

Run 'grove schema' to see the full .grove.yml configuration reference.`,
		Args: cobra.NoArgs,
		RunE: runInit,
	}

	cmd.Flags().StringArray("service", nil, `add a service (format: name:port:ENV_VAR, repeatable)`)
	cmd.Flags().StringArray("env-file", nil, "add a shared/root env file path for top-level env_files (repeatable)")
	cmd.Flags().StringArray("pane", nil, `add a tmux pane (format: command[:name[:optional]], repeatable)`)
	cmd.Flags().String("worktree-dir", "", "worktree directory override (default when omitted: ../.grove-worktrees/<repo-name>)")
	cmd.Flags().String("tmux-mode", "", `tmux mode: "window" or "session"`)
	cmd.Flags().String("tmux-layout", "", "tmux layout preset or raw string")
	cmd.Flags().StringArray("env", nil, "add an env var (format: KEY=VALUE or KEY={{service.port}}, repeatable)")
	cmd.Flags().Bool("force", false, "overwrite existing .grove.yml without prompting")

	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
	// Detect non-interactive mode: any config flag was provided
	services, _ := cmd.Flags().GetStringArray("service")
	envFiles, _ := cmd.Flags().GetStringArray("env-file")
	panes, _ := cmd.Flags().GetStringArray("pane")
	wtDir, _ := cmd.Flags().GetString("worktree-dir")
	tmuxMode, _ := cmd.Flags().GetString("tmux-mode")
	tmuxLayout, _ := cmd.Flags().GetString("tmux-layout")
	envVars, _ := cmd.Flags().GetStringArray("env")
	force, _ := cmd.Flags().GetBool("force")

	nonInteractive := len(services) > 0 || len(envFiles) > 0 || len(panes) > 0 ||
		wtDir != "" || tmuxMode != "" || tmuxLayout != "" || len(envVars) > 0

	if nonInteractive {
		return runInitNonInteractive(cmd, services, envFiles, panes, wtDir, tmuxMode, tmuxLayout, envVars, force)
	}

	return runInitInteractive(cmd, force)
}

func runInitNonInteractive(cmd *cobra.Command, services, envFiles, panes []string, wtDir, tmuxMode, tmuxLayout string, envVars []string, force bool) error {
	cwd, err := getWorkingDir()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	configPath := filepath.Join(cwd, config.ConfigFileName)
	if !force {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("%s already exists — use --force to overwrite", config.ConfigFileName)
		}
	}

	cfg := config.Config{}

	// Worktree dir override
	if wtDir != "" {
		cfg.WorktreeDir = wtDir
	}

	// Env files
	cfg.EnvFiles = envFiles

	// Services: format is name:port:ENV_VAR
	if len(services) > 0 {
		cfg.Services = map[string]config.Service{}
		for _, s := range services {
			parts := strings.SplitN(s, ":", 3)
			if len(parts) != 3 {
				return fmt.Errorf("invalid service format %q — expected name:port:ENV_VAR", s)
			}
			name := parts[0]
			port, err := strconv.Atoi(parts[1])
			if err != nil || port <= 0 || port > 65535 {
				return fmt.Errorf("invalid port %q for service %q", parts[1], name)
			}
			cfg.Services[name] = config.Service{Port: config.ServicePort{Base: port, Env: parts[2]}}
		}
	}

	// Env vars: format is KEY=VALUE
	if len(envVars) > 0 {
		cfg.Env = map[string]string{}
		for _, e := range envVars {
			k, v, ok := strings.Cut(e, "=")
			if !ok {
				return fmt.Errorf("invalid env format %q — expected KEY=VALUE", e)
			}
			cfg.Env[k] = v
		}
	}

	// Tmux config: built from --pane, --tmux-mode, --tmux-layout
	if len(panes) > 0 || tmuxMode != "" || tmuxLayout != "" {
		tmuxCfg := &config.TmuxConfig{}
		if tmuxMode != "" {
			tmuxCfg.Mode = tmuxMode
		}
		if tmuxLayout != "" {
			tmuxCfg.Layout = tmuxLayout
		}

		// Panes: format is command[:name[:optional]]
		for _, p := range panes {
			parts := strings.SplitN(p, ":", 3)
			pane := config.Pane{Cmd: parts[0]}
			if len(parts) >= 2 && parts[1] != "" {
				pane.Name = parts[1]
			}
			if len(parts) >= 3 && parts[2] == "optional" {
				pane.Optional = true
			}
			tmuxCfg.Panes = append(tmuxCfg.Panes, pane)
		}

		cfg.Tmux = tmuxCfg
	}

	return writeConfig(cmd, configPath, &cfg)
}

func runInitInteractive(cmd *cobra.Command, force bool) error {
	w := cmd.OutOrStdout()
	scanner := bufio.NewScanner(stdinReader)

	// Check if .grove.yml already exists in cwd
	cwd, err := getWorkingDir()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	configPath := filepath.Join(cwd, config.ConfigFileName)
	if _, err := os.Stat(configPath); err == nil {
		if force {
			// Skip prompt
		} else {
			fmt.Fprintf(w, "A %s already exists in this directory. Overwrite? [y/N] ", config.ConfigFileName)
			if !scanYesNo(scanner, false) {
				fmt.Fprintln(w, "Aborted.")
				return nil
			}
		}
	}

	cfg := config.Config{}

	// --- Worktree directory ---
	defaultWorktreeDir := config.DefaultWorktreeDir(cwd)
	fmt.Fprintf(w, "\nWorktree directory override (relative to project root, leave empty for default %s): ", defaultWorktreeDir)
	wtDir := scanLine(scanner)
	if wtDir != "" && filepath.Clean(wtDir) != filepath.Clean(defaultWorktreeDir) {
		cfg.WorktreeDir = wtDir
	}

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
			Port: config.ServicePort{Base: port, Env: envName},
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

		fmt.Fprintf(w, "  Main pane size (e.g. 70%%%%) []: ")
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

			fmt.Fprintf(w, "    Pane name (for --with flag, leave empty to skip): ")
			paneName := scanLine(scanner)

			fmt.Fprintf(w, "    Optional pane? [y/N] ")
			optional := scanYesNo(scanner, false)

			pane := config.Pane{Cmd: paneCmd}
			if paneName != "" {
				pane.Name = paneName
			}
			if optional {
				pane.Optional = true
			}
			tmuxCfg.Panes = append(tmuxCfg.Panes, pane)
		}

		cfg.Tmux = tmuxCfg
	}

	return writeConfig(cmd, configPath, &cfg)
}

func writeConfig(cmd *cobra.Command, configPath string, cfg *config.Config) error {
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	header := "# Grove configuration — run 'grove schema' for full reference\n\n"
	content := header + string(data)

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", config.ConfigFileName, err)
	}

	projectRoot := filepath.Dir(configPath)
	effective := *cfg
	effective.ApplyProjectDefaults(projectRoot)
	resolvedWorktreeDir := effective.WorktreeDir
	if !filepath.IsAbs(resolvedWorktreeDir) {
		resolvedWorktreeDir = filepath.Join(projectRoot, resolvedWorktreeDir)
	}
	resolvedWorktreeDir = filepath.Clean(resolvedWorktreeDir)

	fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", configPath)
	if cfg.WorktreeDir == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Default worktree base dir: %s\n", resolvedWorktreeDir)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Worktree base dir: %s\n", resolvedWorktreeDir)
	}
	return nil
}

// scanLine reads a single line from the scanner and trims whitespace.
// Returns the text and true if a line was read, or "" and false on EOF.
func scanLine(scanner *bufio.Scanner) string {
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}

// scannerAtEOF returns true if the scanner has reached EOF.
func scannerAtEOF(scanner *bufio.Scanner) bool {
	return scanner.Err() == nil && !scanner.Scan()
}

// scanYesNo reads a yes/no answer. Returns the default if input is empty.
// On EOF, always returns false to prevent infinite loops in interactive mode.
func scanYesNo(scanner *bufio.Scanner, defaultYes bool) bool {
	if !scanner.Scan() {
		return false // EOF — stop looping
	}
	input := strings.TrimSpace(scanner.Text())
	switch strings.ToLower(input) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultYes
	}
}
