package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_MinimalConfig(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: 3000
    env: PORT
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(cfg.Services))
	}

	svc := cfg.Services["app"]
	if svc.Port != 3000 {
		t.Errorf("expected port 3000, got %d", svc.Port)
	}
	if svc.Env != "PORT" {
		t.Errorf("expected env PORT, got %s", svc.Env)
	}

	// Default worktree_dir should be applied
	if cfg.WorktreeDir != "../.grove-worktrees" {
		t.Errorf("expected default worktree_dir, got %s", cfg.WorktreeDir)
	}

	// No tmux config
	if cfg.Tmux != nil {
		t.Errorf("expected nil tmux config")
	}
}

func TestParse_FullConfig(t *testing.T) {
	yaml := []byte(`
worktree_dir: ../.grove-worktrees

env_files:
  - .env
  - apps/api/.env

services:
  api:
    port: 4000
    env: PORT
  web:
    port: 3000
    env: WEB_PORT

env:
  VITE_API_URL: "http://localhost:{{api.port}}"
  CORS_ORIGIN: "http://localhost:{{web.port}}"

tmux:
  mode: window
  layout: main-vertical
  main_size: "70%"
  panes:
    - nvim
    - claude --model sonnet
    - cmd: pnpm dev
      optional: true
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Worktree dir
	if cfg.WorktreeDir != "../.grove-worktrees" {
		t.Errorf("expected worktree_dir '../.grove-worktrees', got %s", cfg.WorktreeDir)
	}

	// Env files
	if len(cfg.EnvFiles) != 2 {
		t.Fatalf("expected 2 env_files, got %d", len(cfg.EnvFiles))
	}
	if cfg.EnvFiles[0] != ".env" {
		t.Errorf("expected first env_file '.env', got %s", cfg.EnvFiles[0])
	}

	// Services
	if len(cfg.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(cfg.Services))
	}
	if cfg.Services["api"].Port != 4000 {
		t.Errorf("expected api port 4000, got %d", cfg.Services["api"].Port)
	}
	if cfg.Services["web"].Env != "WEB_PORT" {
		t.Errorf("expected web env WEB_PORT, got %s", cfg.Services["web"].Env)
	}

	// Env vars
	if len(cfg.Env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(cfg.Env))
	}
	if cfg.Env["VITE_API_URL"] != "http://localhost:{{api.port}}" {
		t.Errorf("unexpected VITE_API_URL: %s", cfg.Env["VITE_API_URL"])
	}

	// Tmux
	if cfg.Tmux == nil {
		t.Fatal("expected tmux config")
	}
	if cfg.Tmux.Mode != "window" {
		t.Errorf("expected mode 'window', got %s", cfg.Tmux.Mode)
	}
	if cfg.Tmux.Layout != "main-vertical" {
		t.Errorf("expected layout 'main-vertical', got %s", cfg.Tmux.Layout)
	}
	if cfg.Tmux.MainSize != "70%" {
		t.Errorf("expected main_size '70%%', got %s", cfg.Tmux.MainSize)
	}

	// Panes
	if len(cfg.Tmux.Panes) != 3 {
		t.Fatalf("expected 3 panes, got %d", len(cfg.Tmux.Panes))
	}
	if cfg.Tmux.Panes[0].Cmd != "nvim" {
		t.Errorf("expected pane 0 cmd 'nvim', got %s", cfg.Tmux.Panes[0].Cmd)
	}
	if cfg.Tmux.Panes[1].Cmd != "claude --model sonnet" {
		t.Errorf("expected pane 1 cmd 'claude --model sonnet', got %s", cfg.Tmux.Panes[1].Cmd)
	}
	if cfg.Tmux.Panes[2].Cmd != "pnpm dev" {
		t.Errorf("expected pane 2 cmd 'pnpm dev', got %s", cfg.Tmux.Panes[2].Cmd)
	}
	if !cfg.Tmux.Panes[2].Optional {
		t.Errorf("expected pane 2 to be optional")
	}
	if cfg.Tmux.Panes[0].Optional {
		t.Errorf("expected pane 0 to not be optional")
	}
}

func TestParse_Tier3ExplicitSplits(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: 3000
    env: PORT

tmux:
  panes:
    - cmd: nvim
      size: "70%"
    - split: vertical
      panes:
        - cmd: claude
          size: "60%"
        - cmd: pnpm dev
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Tmux.Panes) != 2 {
		t.Fatalf("expected 2 top-level panes, got %d", len(cfg.Tmux.Panes))
	}

	// First pane: regular with size
	if cfg.Tmux.Panes[0].Cmd != "nvim" {
		t.Errorf("expected first pane cmd 'nvim', got %s", cfg.Tmux.Panes[0].Cmd)
	}
	if cfg.Tmux.Panes[0].Size != "70%" {
		t.Errorf("expected first pane size '70%%', got %s", cfg.Tmux.Panes[0].Size)
	}

	// Second pane: split container
	split := cfg.Tmux.Panes[1]
	if split.Split != "vertical" {
		t.Errorf("expected split 'vertical', got %s", split.Split)
	}
	if len(split.Panes) != 2 {
		t.Fatalf("expected 2 nested panes, got %d", len(split.Panes))
	}
	if split.Panes[0].Cmd != "claude" {
		t.Errorf("expected nested pane 0 cmd 'claude', got %s", split.Panes[0].Cmd)
	}
	if split.Panes[1].Cmd != "pnpm dev" {
		t.Errorf("expected nested pane 1 cmd 'pnpm dev', got %s", split.Panes[1].Cmd)
	}
}

func TestParse_Tier4RawLayoutString(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: 3000
    env: PORT

tmux:
  layout: "a]180x50,0,0{120x50,0,0,0,59x50,121,0[59x25,121,0,1,59x24,121,26,2]}"
  panes:
    - nvim
    - claude
    - pnpm dev
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "a]180x50,0,0{120x50,0,0,0,59x50,121,0[59x25,121,0,1,59x24,121,26,2]}"
	if cfg.Tmux.Layout != expected {
		t.Errorf("expected raw layout string, got %s", cfg.Tmux.Layout)
	}
}

func TestParse_SessionMode(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: 3000
    env: PORT

tmux:
  mode: session
  panes:
    - nvim
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Tmux.Mode != "session" {
		t.Errorf("expected mode 'session', got %s", cfg.Tmux.Mode)
	}
}

func TestParse_InvalidPort(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: 0
    env: PORT
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestParse_PortTooHigh(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: 70000
    env: PORT
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for port too high")
	}
}

func TestParse_MissingEnvVar(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: 3000
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}

func TestParse_InvalidTmuxMode(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: 3000
    env: PORT

tmux:
  mode: invalid
  panes:
    - nvim
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for invalid tmux mode")
	}
}

func TestParse_EmptyConfig(t *testing.T) {
	yaml := []byte(``)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get defaults
	if cfg.WorktreeDir != "../.grove-worktrees" {
		t.Errorf("expected default worktree_dir, got %s", cfg.WorktreeDir)
	}
	if cfg.Services != nil {
		t.Errorf("expected nil services, got %v", cfg.Services)
	}
}

func TestParse_CustomWorktreeDir(t *testing.T) {
	yaml := []byte(`
worktree_dir: /tmp/worktrees
services:
  app:
    port: 3000
    env: PORT
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.WorktreeDir != "/tmp/worktrees" {
		t.Errorf("expected custom worktree_dir, got %s", cfg.WorktreeDir)
	}
}

func TestDiscover_FindsConfigInCurrentDir(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	foundPath, projectRoot, err := Discover(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if foundPath != configPath {
		t.Errorf("expected config at %s, got %s", configPath, foundPath)
	}
	if projectRoot != dir {
		t.Errorf("expected project root %s, got %s", dir, projectRoot)
	}
}

func TestDiscover_FindsConfigInParentDir(t *testing.T) {
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "subdir", "deep")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(parentDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	foundPath, projectRoot, err := Discover(childDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if foundPath != configPath {
		t.Errorf("expected config at %s, got %s", configPath, foundPath)
	}
	if projectRoot != parentDir {
		t.Errorf("expected project root %s, got %s", parentDir, projectRoot)
	}
}

func TestDiscover_NotFound(t *testing.T) {
	// Use a temp dir that definitely has no .grove.yml anywhere up
	dir := t.TempDir()
	_, _, err := Discover(dir)
	if err == nil {
		t.Fatal("expected error when no config found")
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ConfigFileName)

	content := []byte(`
services:
  api:
    port: 4000
    env: PORT
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Services["api"].Port != 4000 {
		t.Errorf("expected port 4000, got %d", cfg.Services["api"].Port)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/.grove.yml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ConfigFileName)

	if err := os.WriteFile(configPath, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParse_MicroservicesConfig(t *testing.T) {
	yaml := []byte(`
services:
  gateway:
    port: 8080
    env: PORT
  users:
    port: 8081
    env: PORT
  billing:
    port: 8082
    env: PORT
  frontend:
    port: 3000
    env: PORT

env:
  API_URL: "http://localhost:{{gateway.port}}"

tmux:
  layout: tiled
  panes:
    - nvim
    - docker compose up
    - claude
    - lazygit
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Services) != 4 {
		t.Fatalf("expected 4 services, got %d", len(cfg.Services))
	}
	if cfg.Tmux.Layout != "tiled" {
		t.Errorf("expected layout 'tiled', got %s", cfg.Tmux.Layout)
	}
	if len(cfg.Tmux.Panes) != 4 {
		t.Fatalf("expected 4 panes, got %d", len(cfg.Tmux.Panes))
	}
}

func TestParse_NoServices(t *testing.T) {
	// Config with tmux but no services should be valid
	yaml := []byte(`
tmux:
  panes:
    - nvim
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tmux == nil {
		t.Fatal("expected tmux config")
	}
	if len(cfg.Tmux.Panes) != 1 {
		t.Fatalf("expected 1 pane, got %d", len(cfg.Tmux.Panes))
	}
}
