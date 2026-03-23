// Package config handles parsing and discovery of .grove.yml configuration files.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	// WorktreeDir optionally overrides where worktrees are created, relative to
	// the project root. When omitted, Grove derives a default of
	// "../.grove-worktrees/<repo-name>".
	WorktreeDir string `yaml:"worktree_dir,omitempty"`

	// EnvFiles lists .env files to read (all vars pass through automatically).
	EnvFiles []string `yaml:"env_files,omitempty"`

	// Services defines services with port assignments.
	Services map[string]Service `yaml:"services,omitempty"`

	// Env defines additional environment variables (can reference {{service.port}}).
	Env map[string]string `yaml:"env,omitempty"`

	// Tmux defines optional tmux workspace configuration.
	Tmux *TmuxConfig `yaml:"tmux,omitempty"`

	// Hooks defines lifecycle hooks (scripts to run at specific points).
	Hooks *HooksConfig `yaml:"hooks,omitempty"`

	// Proxy defines optional HTTPS reverse proxy configuration.
	Proxy *ProxyConfig `yaml:"proxy,omitempty"`
}

// HooksConfig defines lifecycle hooks.
type HooksConfig struct {
	// PostCreate lists scripts to run after worktree creation.
	// Paths are relative to the project root and cannot escape it.
	PostCreate []string `yaml:"post_create,omitempty"`
}

// ProxyConfig defines the HTTPS reverse proxy settings.
type ProxyConfig struct {
	// Name is the project name used in hostnames (defaults to repo directory name).
	Name string `yaml:"name,omitempty"`

	// Port is the proxy listen port (default: 1355).
	Port int `yaml:"port,omitempty"`

	// HTTPS enables TLS termination (default: true).
	HTTPS bool `yaml:"https,omitempty"`

	disabled bool
}

// IsDisabled returns true when the proxy was explicitly set to false.
func (pc *ProxyConfig) IsDisabled() bool {
	return pc != nil && pc.disabled
}

// UnmarshalYAML supports both boolean and object forms for proxy config.
//
//	proxy: true       → enabled with defaults
//	proxy: false      → disabled (nil pointer in Config)
//	proxy:            → enabled with overrides
//	  name: myapp
func (pc *ProxyConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var enabled bool
		if err := value.Decode(&enabled); err != nil {
			return fmt.Errorf("proxy must be a boolean or an object: %w", err)
		}
		if !enabled {
			pc.disabled = true
			return nil
		}
		pc.HTTPS = true
		return nil
	}

	type proxyAlias ProxyConfig
	alias := proxyAlias{HTTPS: true}
	if err := value.Decode(&alias); err != nil {
		return fmt.Errorf("parsing proxy config: %w", err)
	}
	*pc = ProxyConfig(alias)
	return nil
}

// Service represents a service with a base port, an env file, and optional env vars.
type Service struct {
	// Port defines the base port and which env var receives the assigned port.
	Port ServicePort `yaml:"port"`

	// EnvFile is the .env file associated with this service (e.g. "apps/api/.env").
	// Grove symlinks it from the main repo and writes service-scoped vars to
	// its .env.local companion.
	EnvFile string `yaml:"env_file,omitempty"`

	// Env defines additional environment variables scoped to this service.
	// Values can reference {{service.port}} and {{branch}} templates.
	// These are written to the service's .env.local file.
	Env map[string]string `yaml:"env,omitempty"`
}

// ServicePort defines the base port and which env var receives the assigned port.
type ServicePort struct {
	// Base is the base port number for this service.
	Base int `yaml:"base"`

	// Env is the environment variable name to set for this service's port.
	// Accepts both "var" (preferred) and "env" (deprecated) in YAML.
	Env string `yaml:"var"`
}

// UnmarshalYAML supports both "var" (preferred) and "env" (deprecated alias)
// for the port env var name field.
func (sp *ServicePort) UnmarshalYAML(value *yaml.Node) error {
	// Decode into a raw map to handle both field names.
	var raw struct {
		Base int    `yaml:"base"`
		Var  string `yaml:"var"`
		Env  string `yaml:"env"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	sp.Base = raw.Base
	switch {
	case raw.Var != "" && raw.Env != "":
		return fmt.Errorf("port: use either \"var\" or \"env\", not both (\"env\" is deprecated, prefer \"var\")")
	case raw.Var != "":
		sp.Env = raw.Var
	default:
		sp.Env = raw.Env
	}
	return nil
}

// HasPort returns true if the service has a port block defined.
// A service with no port block (Base == 0 and Env == "") is an env-only
// service that skips port assignment and validation.
func (s Service) HasPort() bool {
	return s.Port.Base != 0 || s.Port.Env != ""
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

	// Setup is a command that runs before Cmd (e.g. "pnpm install").
	// Always executes regardless of Autorun. If Setup succeeds and Cmd is set,
	// the behavior depends on Autorun: when true, Cmd runs immediately;
	// when false, Cmd is typed but not executed.
	Setup string `yaml:"setup,omitempty"`

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

// DefaultConfig returns a Config with static defaults applied.
// Project-derived defaults (like worktree_dir) are applied later when the
// project root is known.
func DefaultConfig() Config {
	return Config{}
}

// DefaultWorktreeDir returns the derived default worktree directory for a
// project root: ../.grove-worktrees/<repo-name>.
func DefaultWorktreeDir(projectRoot string) string {
	repoName := filepath.Base(filepath.Clean(projectRoot))
	if repoName == "" || repoName == "." || repoName == string(filepath.Separator) {
		repoName = "repo"
	}
	return filepath.Join("..", ".grove-worktrees", repoName)
}

// ApplyProjectDefaults fills in defaults that depend on the project root.
func (cfg *Config) ApplyProjectDefaults(projectRoot string) {
	if cfg.WorktreeDir == "" {
		cfg.WorktreeDir = DefaultWorktreeDir(projectRoot)
	}
}

// Load reads and parses a .grove.yml file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}
	cfg.ApplyProjectDefaults(filepath.Dir(path))
	return cfg, nil
}

// LoadNoValidate reads and parses a .grove.yml file without running validation.
// Use this for operations (like delete) that don't need fully valid service config.
func LoadNoValidate(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg.normalizeProxy()
	cfg.ApplyProjectDefaults(filepath.Dir(path))
	return &cfg, nil
}

// Parse parses raw YAML bytes into a Config.
func Parse(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.normalizeProxy()

	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// normalizeProxy converts "proxy: false" (disabled flag) into a nil pointer.
func (cfg *Config) normalizeProxy() {
	if cfg.Proxy.IsDisabled() {
		cfg.Proxy = nil
	}
}

// AllEnvFiles returns the union of top-level env_files and service-level env_file
// values, in order (top-level first, then service env files not already listed).
func (cfg *Config) AllEnvFiles() []string {
	seen := make(map[string]bool)
	var result []string

	for _, f := range cfg.EnvFiles {
		if !seen[f] {
			seen[f] = true
			result = append(result, f)
		}
	}

	names := make([]string, 0, len(cfg.Services))
	for name := range cfg.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := cfg.Services[name]
		if svc.EnvFile != "" && !seen[svc.EnvFile] {
			seen[svc.EnvFile] = true
			result = append(result, svc.EnvFile)
		}
	}

	return result
}

// Validate checks that the config is well-formed.
func Validate(cfg *Config) error {
	// Validate services
	envSeen := make(map[string]string) // env var name -> service name
	for name, svc := range cfg.Services {
		if svc.HasPort() {
			// Partial port block: env set but base missing (or vice versa)
			if svc.Port.Base <= 0 {
				return fmt.Errorf("service %q: port.base is required when port is defined — either add a base port or remove the port block entirely (services without port are valid for env-only use)", name)
			}
			if svc.Port.Base > 65535 {
				return fmt.Errorf("service %q: port must be between 1 and 65535, got %d", name, svc.Port.Base)
			}
			if IsPortBlocked(svc.Port.Base) {
				return fmt.Errorf("service %q: port %d is browser-restricted (blocked by Chromium/Firefox) — pick a different base port", name, svc.Port.Base)
			}
			if svc.Port.Env == "" {
				return fmt.Errorf("service %q: port.var is required when port is defined — either add an env var name or remove the port block entirely", name)
			}
			if other, exists := envSeen[svc.Port.Env]; exists {
				return fmt.Errorf("services %q and %q both use env var %q", other, name, svc.Port.Env)
			}
			envSeen[svc.Port.Env] = name
		}
		// Catch collision: port.env names the var that receives the computed
		// port — if the same key also appears in env, the env value silently
		// wins, which is almost certainly a mistake.
		if svc.HasPort() && len(svc.Env) > 0 {
			if _, exists := svc.Env[svc.Port.Env]; exists {
				return fmt.Errorf("service %q: env var %q is already set by port.var — remove it from env to avoid a silent override", name, svc.Port.Env)
			}
		}
		if len(svc.Env) > 0 && svc.EnvFile == "" {
			return fmt.Errorf("service %q: env requires env_file so Grove knows where to write service-scoped vars", name)
		}

		// Validate service env_file path
		if svc.EnvFile != "" {
			if filepath.IsAbs(svc.EnvFile) {
				return fmt.Errorf("service %q: env_file %q must be a relative path", name, svc.EnvFile)
			}
			cleaned := filepath.Clean(svc.EnvFile)
			if strings.HasPrefix(cleaned, "..") {
				return fmt.Errorf("service %q: env_file %q escapes the project root", name, svc.EnvFile)
			}
		}
	}

	// Check for env var key collisions across services sharing the same env_file.
	// Maps env_file -> env var key -> service name that writes it.
	envFileKeys := make(map[string]map[string]string)
	for name, svc := range cfg.Services {
		if svc.EnvFile == "" {
			continue
		}
		if envFileKeys[svc.EnvFile] == nil {
			envFileKeys[svc.EnvFile] = make(map[string]string)
		}
		keys := envFileKeys[svc.EnvFile]

		if svc.HasPort() {
			if other, exists := keys[svc.Port.Env]; exists {
				return fmt.Errorf("services %q and %q both write env var %q to %s", other, name, svc.Port.Env, svc.EnvFile+".local")
			}
			keys[svc.Port.Env] = name
		}
		for key := range svc.Env {
			if other, exists := keys[key]; exists {
				return fmt.Errorf("services %q and %q both write env var %q to %s", other, name, key, svc.EnvFile+".local")
			}
			keys[key] = name
		}
	}

	// Validate env_files paths stay within project root
	for _, envFile := range cfg.EnvFiles {
		if filepath.IsAbs(envFile) {
			return fmt.Errorf("env_files: %q must be a relative path", envFile)
		}
		cleaned := filepath.Clean(envFile)
		if strings.HasPrefix(cleaned, "..") {
			return fmt.Errorf("env_files: %q escapes the project root", envFile)
		}
	}

	// Validate hooks
	if cfg.Hooks != nil {
		seen := make(map[string]bool)
		for _, script := range cfg.Hooks.PostCreate {
			if script == "" {
				return fmt.Errorf("hooks.post_create: script path must not be empty")
			}
			if filepath.IsAbs(script) {
				return fmt.Errorf("hooks.post_create: %q must be a relative path", script)
			}
			cleaned := filepath.Clean(script)
			if strings.HasPrefix(cleaned, "..") {
				return fmt.Errorf("hooks.post_create: %q escapes the project root", script)
			}
			if seen[cleaned] {
				return fmt.Errorf("hooks.post_create: duplicate script %q", script)
			}
			seen[cleaned] = true
		}
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

	// Check that git is available
	if _, err := exec.LookPath("git"); err != nil {
		return "", "", fmt.Errorf("git is not installed or not in PATH — grove requires git")
	}

	// Find the git repo root to bound our search
	repoRoot, err := gitRepoRoot(dir)
	if err != nil {
		return "", "", fmt.Errorf(
			"not inside a git repository (started at %s) — grove only searches for %s within the current git repo; run 'grove init' inside a git repo",
			dir,
			ConfigFileName,
		)
	}

	// Walk up the directory tree, stopping at the git repo root
	cur := dir
	for {
		candidate := filepath.Join(cur, ConfigFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, cur, nil
		}

		if cur == repoRoot {
			break
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

	return "", "", fmt.Errorf(
		"git repository found at %s, but no %s was found from %s up to that repo root (or in the main repo root if this is a linked worktree) — run 'grove init' to create one",
		repoRoot,
		ConfigFileName,
		dir,
	)
}

// gitRepoRoot returns the top-level directory of the current git repository.
func gitRepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
