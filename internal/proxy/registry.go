package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/lukemelnik/grove/internal/config"
)

const (
	registryFileName = "projects.json"
	DefaultProxyPort = 1355
)

type ProjectEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type Registry struct {
	mu       sync.Mutex
	stateDir string
}

func NewRegistry(stateDir string) *Registry {
	return &Registry{stateDir: stateDir}
}

func (r *Registry) filePath() string {
	return filepath.Join(r.stateDir, registryFileName)
}

func (r *Registry) RegisterProject(projectRoot string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolving absolute path: %w", err)
	}

	entries, err := r.loadLocked()
	if err != nil {
		return err
	}

	cfg, err := loadProxyConfig(absPath)
	if err != nil {
		return fmt.Errorf("loading config for %s: %w", absPath, err)
	}

	name := projectName(cfg, absPath)

	for _, entry := range entries {
		if entry.Path == absPath {
			return nil
		}
		if entry.Name == name {
			return fmt.Errorf("project name %q is already registered by %s", name, entry.Path)
		}
	}

	entries = append(entries, ProjectEntry{Path: absPath, Name: name})
	return r.saveLocked(entries)
}

func (r *Registry) UnregisterProject(projectRoot string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolving absolute path: %w", err)
	}

	entries, err := r.loadLocked()
	if err != nil {
		return err
	}

	filtered := make([]ProjectEntry, 0, len(entries))
	found := false
	for _, entry := range entries {
		if entry.Path == absPath {
			found = true
			continue
		}
		filtered = append(filtered, entry)
	}

	if !found {
		return fmt.Errorf("project at %s is not registered", absPath)
	}

	return r.saveLocked(filtered)
}

func (r *Registry) UnregisterByName(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.loadLocked()
	if err != nil {
		return err
	}

	filtered := make([]ProjectEntry, 0, len(entries))
	found := false
	for _, entry := range entries {
		if entry.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, entry)
	}

	if !found {
		return fmt.Errorf("no project named %q is registered", name)
	}

	return r.saveLocked(filtered)
}

func (r *Registry) ListProjects() ([]ProjectEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadLocked()
}

func (r *Registry) LoadAndPrune() ([]ProjectEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := r.loadLocked()
	if err != nil {
		return nil, err
	}

	valid := make([]ProjectEntry, 0, len(entries))
	for _, entry := range entries {
		if _, statErr := os.Stat(entry.Path); statErr != nil {
			continue
		}

		configPath := filepath.Join(entry.Path, config.ConfigFileName)
		if _, statErr := os.Stat(configPath); statErr != nil {
			continue
		}

		cfg, err := loadProxyConfig(entry.Path)
		if err != nil || cfg == nil {
			continue
		}

		valid = append(valid, entry)
	}

	if len(valid) != len(entries) {
		_ = r.saveLocked(valid)
	}

	return valid, nil
}

func (r *Registry) Clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveLocked(nil)
}

func (r *Registry) loadLocked() ([]ProjectEntry, error) {
	data, err := os.ReadFile(r.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading registry: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	var entries []ProjectEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing registry: %w", err)
	}

	return entries, nil
}

func (r *Registry) saveLocked(entries []ProjectEntry) error {
	if err := os.MkdirAll(r.stateDir, 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling registry: %w", err)
	}

	return os.WriteFile(r.filePath(), data, 0644)
}

type ProxyConfig struct {
	Name  string
	Port  int
	HTTPS bool
}

func loadProxyConfig(projectRoot string) (*ProxyConfig, error) {
	configPath := filepath.Join(projectRoot, config.ConfigFileName)
	cfg, err := config.LoadNoValidate(configPath)
	if err != nil {
		return nil, err
	}

	if cfg.Proxy == nil {
		return nil, nil
	}

	name := cfg.Proxy.Name
	if name == "" {
		name = filepath.Base(filepath.Clean(projectRoot))
	}

	port := cfg.Proxy.Port
	if port == 0 {
		port = DefaultProxyPort
	}

	return &ProxyConfig{
		Name:  name,
		Port:  port,
		HTTPS: cfg.Proxy.HTTPS,
	}, nil
}

func projectName(cfg *ProxyConfig, projectRoot string) string {
	if cfg != nil && cfg.Name != "" {
		return cfg.Name
	}
	return filepath.Base(filepath.Clean(projectRoot))
}
