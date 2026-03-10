// Package config handles parsing and discovery of .grove.yml configuration files.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFileName is the name of the Grove configuration file.
const ConfigFileName = ".grove.yml"

// BlockedPorts contains browser-restricted ports that should never be assigned.
// See https://fetch.spec.whatwg.org/#bad-port (Chromium/Firefox blocked ports).
var BlockedPorts = map[int]bool{
	1: true, 7: true, 9: true, 11: true, 13: true, 15: true, 17: true,
	19: true, 20: true, 21: true, 22: true, 23: true, 25: true, 37: true,
	42: true, 43: true, 53: true, 77: true, 79: true, 87: true, 95: true,
	101: true, 102: true, 103: true, 104: true, 109: true, 110: true,
	111: true, 113: true, 115: true, 117: true, 119: true, 123: true,
	135: true, 139: true, 143: true, 179: true, 389: true, 427: true,
	465: true, 512: true, 513: true, 514: true, 515: true, 526: true,
	530: true, 531: true, 532: true, 540: true, 548: true, 556: true,
	563: true, 587: true, 601: true, 636: true, 993: true, 995: true,
	2049: true, 3659: true, 4045: true, 4190: true, 5060: true, 5061: true,
	6000: true, 6566: true, 6665: true, 6666: true, 6667: true, 6668: true,
	6669: true, 6697: true, 10080: true,
}

// IsPortBlocked returns true if the port is in the browser-restricted list.
func IsPortBlocked(port int) bool {
	return BlockedPorts[port]
}

// Config represents the top-level .grove.yml configuration.
type Config struct {
	// WorktreeDir is where worktrees are created, relative to the project root.
	// Defaults to "../.grove-worktrees".
	WorktreeDir string `yaml:"worktree_dir,omitempty"`

	// EnvFiles lists .env files to read (all vars pass through automatically).
	EnvFiles []string `yaml:"env_files,omitempty"`

	// Services defines services with port assignments.
	Services map[string]Service `yaml:"services,omitempty"`

	// Env defines additional environment variables (can reference {{service.port}}).
	Env map[string]string `yaml:"env,omitempty"`

	// Tmux defines optional tmux workspace configuration.
	Tmux *TmuxConfig `yaml:"tmux,omitempty"`
}

// Service represents a service with a base port and an env var name.
type Service struct {
	// Port is the base port number for this service.
	Port int `yaml:"port"`

	// Env is the environment variable name to set for this service's port.
	Env string `yaml:"env"`
}

// TmuxConfig represents the tmux workspace configuration.
type TmuxConfig struct {
	// Mode is either "window" (default) or "session".
	Mode string `yaml:"mode,omitempty"`

	// Layout is a tmux layout preset name or a raw layout string.
	// Presets: even-horizontal, even-vertical, main-horizontal, main-vertical, tiled.
	// Defaults to "main-vertical" when using preset layouts.
	Layout string `yaml:"layout,omitempty"`

	// MainSize is the size of the main pane (e.g., "70%").
	MainSize string `yaml:"main_size,omitempty"`

	// Panes defines the panes in the tmux workspace.
	Panes []Pane `yaml:"panes,omitempty"`
}

// Pane represents a single pane in a tmux layout.
// It supports multiple forms:
//   - Simple string: "nvim" (just a command)
//   - Map with cmd: {cmd: "pnpm dev", optional: true}
//   - Map with split: {split: "vertical", panes: [...]} (Tier 3 explicit splits)
type Pane struct {
	// Name is an optional identifier for this pane, used with --with <name>.
	Name string `yaml:"name,omitempty"`

	// Cmd is the command to run in this pane.
	Cmd string `yaml:"cmd,omitempty"`

	// Optional marks this pane as optional (skipped unless --all or --with).
	Optional bool `yaml:"optional,omitempty"`

	// Size is the size of this pane (e.g., "70%"), used in Tier 3 explicit splits.
	Size string `yaml:"size,omitempty"`

	// Autorun controls whether the command is executed automatically.
	// Defaults to true. When false, the command is typed but not executed,
	// letting the user press Enter when ready.
	Autorun *bool `yaml:"autorun,omitempty"`

	// Split defines a nested split direction ("vertical" or "horizontal").
	// When set, this pane is a container with sub-panes.
	Split string `yaml:"split,omitempty"`

	// Panes are the nested panes inside a split container.
	Panes []Pane `yaml:"panes,omitempty"`
}

// ShouldAutorun returns whether the pane command should be executed automatically.
// Defaults to true when Autorun is not set.
func (p *Pane) ShouldAutorun() bool {
	if p.Autorun == nil {
		return true
	}
	return *p.Autorun
}

// UnmarshalYAML implements custom unmarshaling for Pane to support both
// simple string form ("nvim") and map form ({cmd: "nvim", optional: true}).
func (p *Pane) UnmarshalYAML(value *yaml.Node) error {
	// Simple string form: "nvim"
	if value.Kind == yaml.ScalarNode {
		p.Cmd = value.Value
		return nil
	}

	// Map form: use an alias type to avoid infinite recursion
	if value.Kind == yaml.MappingNode {
		type paneAlias Pane
		var alias paneAlias
		if err := value.Decode(&alias); err != nil {
			return fmt.Errorf("decoding pane: %w", err)
		}
		*p = Pane(alias)
		return nil
	}

	return fmt.Errorf("pane must be a string or a map, got %v", value.Kind)
}

// DefaultConfig returns a Config with default values applied.
func DefaultConfig() Config {
	return Config{
		WorktreeDir: "../.grove-worktrees",
	}
}

// Load reads and parses a .grove.yml file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return Parse(data)
}

// Parse parses raw YAML bytes into a Config.
func Parse(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks that the config is well-formed.
func Validate(cfg *Config) error {
	// Validate services
	envSeen := make(map[string]string) // env var name -> service name
	for name, svc := range cfg.Services {
		if svc.Port <= 0 || svc.Port > 65535 {
			return fmt.Errorf("service %q: port must be between 1 and 65535, got %d", name, svc.Port)
		}
		if IsPortBlocked(svc.Port) {
			return fmt.Errorf("service %q: port %d is browser-restricted (blocked by Chromium/Firefox) — pick a different base port", name, svc.Port)
		}
		if svc.Env == "" {
			return fmt.Errorf("service %q: env var name is required", name)
		}
		if other, exists := envSeen[svc.Env]; exists {
			return fmt.Errorf("services %q and %q both use env var %q", other, name, svc.Env)
		}
		envSeen[svc.Env] = name
	}

	// Validate tmux config
	if cfg.Tmux != nil {
		if cfg.Tmux.Mode != "" && cfg.Tmux.Mode != "window" && cfg.Tmux.Mode != "session" {
			return fmt.Errorf("tmux mode must be \"window\" or \"session\", got %q", cfg.Tmux.Mode)
		}
		if err := validatePanes(cfg.Tmux.Panes); err != nil {
			return err
		}
	}

	return nil
}

// validatePanes recursively validates pane definitions.
func validatePanes(panes []Pane) error {
	for _, p := range panes {
		if p.Split != "" {
			if p.Split != "vertical" && p.Split != "horizontal" {
				return fmt.Errorf("split direction must be \"vertical\" or \"horizontal\", got %q", p.Split)
			}
			if err := validatePanes(p.Panes); err != nil {
				return err
			}
		}
	}
	return nil
}

// Discover walks up from startDir looking for a .grove.yml file.
// Returns the path to the config file and the project root directory.
// If no config file is found in the directory tree, it checks whether
// we're inside a git worktree and looks in the main repo root. This
// lets you run grove commands from any worktree even if .grove.yml is
// untracked (only exists in the main repo).
func Discover(startDir string) (configPath string, projectRoot string, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", fmt.Errorf("resolving absolute path: %w", err)
	}

	// First: walk up the directory tree
	cur := dir
	for {
		candidate := filepath.Join(cur, ConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, cur, nil
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	// Fallback: if we're in a git worktree, check the main repo root.
	// git rev-parse --git-common-dir returns the main repo's .git dir.
	mainRoot, err := gitMainRepoRoot(startDir)
	if err == nil && mainRoot != "" {
		candidate := filepath.Join(mainRoot, ConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, mainRoot, nil
		}
	}

	return "", "", fmt.Errorf("no %s found (searched from %s to filesystem root)", ConfigFileName, startDir)
}

// gitMainRepoRoot returns the root directory of the main git repo
// (not a worktree) by resolving --git-common-dir.
func gitMainRepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(string(out))
	if gitDir == "" {
		return "", fmt.Errorf("empty git-common-dir")
	}
	// --git-common-dir may return a relative path
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(dir, gitDir)
	}
	// The repo root is the parent of the .git directory
	root := filepath.Dir(gitDir)
	return filepath.Clean(root), nil
}
