// Package config handles parsing and discovery of .grove.yml configuration files.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ConfigFileName is the name of the Grove configuration file.
const ConfigFileName = ".grove.yml"

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
	// Cmd is the command to run in this pane.
	Cmd string `yaml:"cmd,omitempty"`

	// Optional marks this pane as optional (skipped unless --all or --with).
	Optional bool `yaml:"optional,omitempty"`

	// Size is the size of this pane (e.g., "70%"), used in Tier 3 explicit splits.
	Size string `yaml:"size,omitempty"`

	// Split defines a nested split direction ("vertical" or "horizontal").
	// When set, this pane is a container with sub-panes.
	Split string `yaml:"split,omitempty"`

	// Panes are the nested panes inside a split container.
	Panes []Pane `yaml:"panes,omitempty"`
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

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validate checks that the config is well-formed.
func validate(cfg *Config) error {
	// Validate services
	for name, svc := range cfg.Services {
		if svc.Port <= 0 || svc.Port > 65535 {
			return fmt.Errorf("service %q: port must be between 1 and 65535, got %d", name, svc.Port)
		}
		if svc.Env == "" {
			return fmt.Errorf("service %q: env var name is required", name)
		}
	}

	// Validate tmux config
	if cfg.Tmux != nil {
		if cfg.Tmux.Mode != "" && cfg.Tmux.Mode != "window" && cfg.Tmux.Mode != "session" {
			return fmt.Errorf("tmux mode must be \"window\" or \"session\", got %q", cfg.Tmux.Mode)
		}
	}

	return nil
}

// Discover walks up from startDir looking for a .grove.yml file.
// Returns the path to the config file and the project root directory.
// If no config file is found, returns an error.
func Discover(startDir string) (configPath string, projectRoot string, err error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", "", fmt.Errorf("resolving absolute path: %w", err)
	}

	for {
		candidate := filepath.Join(dir, ConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding config
			return "", "", fmt.Errorf("no %s found (searched from %s to filesystem root)", ConfigFileName, startDir)
		}
		dir = parent
	}
}
