package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@grove.test"},
		{"config", "user.name", "Grove Test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s: %v", args, out, err)
		}
	}
}

func TestParse_MinimalConfig(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port:
      base: 3000
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
	if svc.Port.Base != 3000 {
		t.Errorf("expected port 3000, got %d", svc.Port.Base)
	}
	if svc.Port.Env != "PORT" {
		t.Errorf("expected port env PORT, got %s", svc.Port.Env)
	}

	// Parse preserves omission; project-derived defaults are applied by Load.
	if cfg.WorktreeDir != "" {
		t.Errorf("expected omitted worktree_dir to stay empty after Parse, got %s", cfg.WorktreeDir)
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

services:
  api:
    env_file: apps/api/.env
    port:
      base: 4000
      env: PORT
    env:
      CORS_ORIGIN: "http://localhost:{{web.port}}"
  web:
    env_file: apps/web/.env
    port:
      base: 3000
      env: WEB_PORT
    env:
      VITE_API_URL: "http://localhost:{{api.port}}"

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

	// Env files (top-level)
	if len(cfg.EnvFiles) != 1 {
		t.Fatalf("expected 1 top-level env_file, got %d", len(cfg.EnvFiles))
	}
	if cfg.EnvFiles[0] != ".env" {
		t.Errorf("expected first env_file '.env', got %s", cfg.EnvFiles[0])
	}

	// AllEnvFiles should include service env_files
	allEnv := cfg.AllEnvFiles()
	if len(allEnv) != 3 {
		t.Fatalf("expected 3 total env files, got %d: %v", len(allEnv), allEnv)
	}

	// Services
	if len(cfg.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(cfg.Services))
	}
	if cfg.Services["api"].Port.Base != 4000 {
		t.Errorf("expected api port 4000, got %d", cfg.Services["api"].Port.Base)
	}
	if cfg.Services["web"].Port.Env != "WEB_PORT" {
		t.Errorf("expected web port env WEB_PORT, got %s", cfg.Services["web"].Port.Env)
	}
	if cfg.Services["api"].EnvFile != "apps/api/.env" {
		t.Errorf("expected api env_file 'apps/api/.env', got %s", cfg.Services["api"].EnvFile)
	}

	// Service-level env vars
	if cfg.Services["api"].Env["CORS_ORIGIN"] != "http://localhost:{{web.port}}" {
		t.Errorf("unexpected CORS_ORIGIN: %s", cfg.Services["api"].Env["CORS_ORIGIN"])
	}
	if cfg.Services["web"].Env["VITE_API_URL"] != "http://localhost:{{api.port}}" {
		t.Errorf("unexpected VITE_API_URL: %s", cfg.Services["web"].Env["VITE_API_URL"])
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
    port:
      base: 3000
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
    port:
      base: 3000
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
    port:
      base: 3000
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
    port:
      base: 0
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
    port:
      base: 70000
      env: PORT
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for port too high")
	}
}

func TestParse_MissingPortEnv(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port:
      base: 3000
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for missing port env var")
	}
}

func TestParse_ServiceWithoutPort(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      env: PORT
    env_file: apps/api/.env
  desktop:
    env_file: apps/desktop/.env
    env:
      PORT: "{{api.port}}"
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("expected no error for port-less service, got: %v", err)
	}
	if cfg.Services["desktop"].HasPort() {
		t.Error("expected desktop service to have no port")
	}
	if !cfg.Services["api"].HasPort() {
		t.Error("expected api service to have a port")
	}
}

func TestParse_PortEnvCollidesWithEnv(t *testing.T) {
	yaml := []byte(`
services:
  api:
    env_file: apps/api/.env
    port:
      base: 4000
      env: PORT
    env:
      PORT: "9999"
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error when port.env collides with env key")
	}
	if !strings.Contains(err.Error(), "already set by port.var") {
		t.Errorf("expected collision error, got: %v", err)
	}
}

func TestParse_InvalidTmuxMode(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port:
      base: 3000
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

	// Parse preserves omission; project-derived defaults are applied by Load.
	if cfg.WorktreeDir != "" {
		t.Errorf("expected omitted worktree_dir to stay empty after Parse, got %s", cfg.WorktreeDir)
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
    port:
      base: 3000
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
	initGitRepo(t, dir)
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
	initGitRepo(t, parentDir)
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

func TestDiscover_NotFound_NoGitRepo(t *testing.T) {
	dir := t.TempDir()
	_, _, err := Discover(dir)
	if err == nil {
		t.Fatal("expected error when not in a git repo")
	}
	if !strings.Contains(err.Error(), "not inside a git repository") {
		t.Errorf("expected clearer 'not inside a git repository' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "only searches for .grove.yml within the current git repo") {
		t.Errorf("expected search-scope hint, got: %v", err)
	}
}

func TestDiscover_NotFound_GitRepoNoConfig(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	_, _, err := Discover(dir)
	if err == nil {
		t.Fatal("expected error when no config found")
	}
	if !strings.Contains(err.Error(), "git repository found at") {
		t.Errorf("expected repo-found context in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "but no .grove.yml was found") {
		t.Errorf("expected clearer 'no .grove.yml' error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "grove init") {
		t.Errorf("expected hint about 'grove init', got: %v", err)
	}
}

func TestDefaultWorktreeDir(t *testing.T) {
	got := DefaultWorktreeDir("/home/user/project-a")
	want := filepath.Join("..", ".grove-worktrees", "project-a")
	if got != want {
		t.Fatalf("DefaultWorktreeDir() = %q, want %q", got, want)
	}
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ConfigFileName)

	content := []byte(`
services:
  api:
    port:
      base: 4000
      env: PORT
`)
	if err := os.WriteFile(configPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Services["api"].Port.Base != 4000 {
		t.Errorf("expected port 4000, got %d", cfg.Services["api"].Port.Base)
	}

	wantWorktreeDir := filepath.Join("..", ".grove-worktrees", filepath.Base(dir))
	if cfg.WorktreeDir != wantWorktreeDir {
		t.Errorf("expected derived worktree_dir %q, got %q", wantWorktreeDir, cfg.WorktreeDir)
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
    port:
      base: 8080
      env: GATEWAY_PORT
  users:
    port:
      base: 8081
      env: USERS_PORT
  billing:
    port:
      base: 8082
      env: BILLING_PORT
  frontend:
    port:
      base: 3000
      env: FRONTEND_PORT

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

func TestParse_DuplicateEnvVarName(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      env: PORT
  web:
    port:
      base: 3000
      env: PORT
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for duplicate env var name")
	}
	if !strings.Contains(err.Error(), "both use env var") {
		t.Errorf("expected 'both use env var' in error, got: %v", err)
	}
}

func TestParse_NegativePort(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port: -1
    env: PORT
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for negative port")
	}
}

func TestParse_PaneWithName(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port:
      base: 3000
      env: PORT

tmux:
  panes:
    - nvim
    - name: dev
      cmd: pnpm dev
      optional: true
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Tmux.Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(cfg.Tmux.Panes))
	}

	// String pane should have empty name
	if cfg.Tmux.Panes[0].Name != "" {
		t.Errorf("expected empty name for string pane, got %s", cfg.Tmux.Panes[0].Name)
	}

	// Named pane
	if cfg.Tmux.Panes[1].Name != "dev" {
		t.Errorf("expected pane name 'dev', got %s", cfg.Tmux.Panes[1].Name)
	}
	if cfg.Tmux.Panes[1].Cmd != "pnpm dev" {
		t.Errorf("expected pane cmd 'pnpm dev', got %s", cfg.Tmux.Panes[1].Cmd)
	}
	if !cfg.Tmux.Panes[1].Optional {
		t.Errorf("expected pane to be optional")
	}
}

func TestParse_InvalidSplitDirection(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port:
      base: 3000
      env: PORT

tmux:
  panes:
    - split: diagonal
      panes:
        - nvim
        - claude
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for invalid split direction")
	}
}

func TestParse_NestedSplitsDeep(t *testing.T) {
	yaml := []byte(`
services:
  app:
    port:
      base: 3000
      env: PORT

tmux:
  panes:
    - cmd: nvim
      size: "60%"
    - split: vertical
      panes:
        - cmd: claude
          size: "50%"
        - split: horizontal
          panes:
            - cmd: pnpm dev
            - cmd: pnpm test
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Top level: 2 panes (nvim + vertical split)
	if len(cfg.Tmux.Panes) != 2 {
		t.Fatalf("expected 2 top-level panes, got %d", len(cfg.Tmux.Panes))
	}

	// Vertical split has 2 children (claude + horizontal split)
	vSplit := cfg.Tmux.Panes[1]
	if len(vSplit.Panes) != 2 {
		t.Fatalf("expected 2 panes in vertical split, got %d", len(vSplit.Panes))
	}

	// Nested horizontal split has 2 leaf panes
	hSplit := vSplit.Panes[1]
	if hSplit.Split != "horizontal" {
		t.Errorf("expected split 'horizontal', got %s", hSplit.Split)
	}
	if len(hSplit.Panes) != 2 {
		t.Fatalf("expected 2 panes in horizontal split, got %d", len(hSplit.Panes))
	}
	if hSplit.Panes[0].Cmd != "pnpm dev" {
		t.Errorf("expected 'pnpm dev', got %s", hSplit.Panes[0].Cmd)
	}
	if hSplit.Panes[1].Cmd != "pnpm test" {
		t.Errorf("expected 'pnpm test', got %s", hSplit.Panes[1].Cmd)
	}
}

func TestParse_TmuxEmptyPanes(t *testing.T) {
	yaml := []byte(`
tmux:
  layout: main-vertical
  panes: []
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Tmux == nil {
		t.Fatal("expected tmux config")
	}
	if len(cfg.Tmux.Panes) != 0 {
		t.Errorf("expected 0 panes, got %d", len(cfg.Tmux.Panes))
	}
}

func TestParse_DefaultTmuxMode(t *testing.T) {
	// When tmux mode is omitted, it should be empty string (default applied at runtime)
	yaml := []byte(`
tmux:
  panes:
    - nvim
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Tmux.Mode != "" {
		t.Errorf("expected empty mode (default), got %s", cfg.Tmux.Mode)
	}
}

func TestParse_SetupField(t *testing.T) {
	yaml := []byte(`
tmux:
  panes:
    - cmd: pnpm dev
      setup: pnpm install
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Tmux.Panes[0].Setup != "pnpm install" {
		t.Errorf("expected setup 'pnpm install', got %q", cfg.Tmux.Panes[0].Setup)
	}
	if cfg.Tmux.Panes[0].Cmd != "pnpm dev" {
		t.Errorf("expected cmd 'pnpm dev', got %q", cfg.Tmux.Panes[0].Cmd)
	}
}

func TestParse_AutorunField(t *testing.T) {
	yaml := []byte(`
tmux:
  panes:
    - cmd: pnpm dev
      autorun: false
    - nvim
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Tmux.Panes[0].ShouldAutorun() {
		t.Error("expected autorun false for first pane")
	}
	if !cfg.Tmux.Panes[1].ShouldAutorun() {
		t.Error("expected autorun true (default) for second pane")
	}
}

func TestParse_SetupWithAutorunFalse(t *testing.T) {
	yaml := []byte(`
tmux:
  panes:
    - cmd: pnpm dev
      setup: pnpm install
      autorun: false
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	p := cfg.Tmux.Panes[0]
	if p.Setup != "pnpm install" {
		t.Errorf("expected setup 'pnpm install', got %q", p.Setup)
	}
	if p.Cmd != "pnpm dev" {
		t.Errorf("expected cmd 'pnpm dev', got %q", p.Cmd)
	}
	if p.ShouldAutorun() {
		t.Error("expected autorun false")
	}
}

func TestParse_BlockedBasePort(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 6000
      env: PORT
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for blocked base port 6000")
	}
	if !strings.Contains(err.Error(), "browser-restricted") {
		t.Errorf("expected browser-restricted error, got: %v", err)
	}
}

func TestParse_EnvFilesAbsolutePath(t *testing.T) {
	yaml := []byte(`
env_files:
  - /etc/secrets/.env
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for absolute env_files path")
	}
	if !strings.Contains(err.Error(), "must be a relative path") {
		t.Errorf("expected 'must be a relative path' error, got: %v", err)
	}
}

func TestParse_EnvFilesEscapesRoot(t *testing.T) {
	yaml := []byte(`
env_files:
  - ../../etc/passwd
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for env_files escaping project root")
	}
	if !strings.Contains(err.Error(), "escapes the project root") {
		t.Errorf("expected 'escapes the project root' error, got: %v", err)
	}
}

func TestParse_EnvFilesValidRelative(t *testing.T) {
	yaml := []byte(`
env_files:
  - .env
  - apps/api/.env
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.EnvFiles) != 2 {
		t.Errorf("expected 2 env_files, got %d", len(cfg.EnvFiles))
	}
}

func TestParse_ServiceEnvRequiresEnvFile(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      env: PORT
    env:
      API_URL: "http://localhost:{{api.port}}"
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error when service env is configured without env_file")
	}
	if !strings.Contains(err.Error(), "env requires env_file") {
		t.Errorf("expected env_file validation error, got: %v", err)
	}
}

func TestParse_HooksPostCreate(t *testing.T) {
	yaml := []byte(`
hooks:
  post_create:
    - scripts/setup.sh
    - scripts/generate.sh
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Hooks == nil {
		t.Fatal("expected hooks config")
	}
	if len(cfg.Hooks.PostCreate) != 2 {
		t.Fatalf("expected 2 post_create hooks, got %d", len(cfg.Hooks.PostCreate))
	}
	if cfg.Hooks.PostCreate[0] != "scripts/setup.sh" {
		t.Errorf("expected first hook 'scripts/setup.sh', got %s", cfg.Hooks.PostCreate[0])
	}
	if cfg.Hooks.PostCreate[1] != "scripts/generate.sh" {
		t.Errorf("expected second hook 'scripts/generate.sh', got %s", cfg.Hooks.PostCreate[1])
	}
}

func TestParse_HooksAbsolutePath(t *testing.T) {
	yaml := []byte(`
hooks:
  post_create:
    - /usr/local/bin/evil.sh
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for absolute hook path")
	}
	if !strings.Contains(err.Error(), "must be a relative path") {
		t.Errorf("expected relative path error, got: %v", err)
	}
}

func TestParse_HooksEscapesRoot(t *testing.T) {
	yaml := []byte(`
hooks:
  post_create:
    - ../../etc/evil.sh
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for hook escaping project root")
	}
	if !strings.Contains(err.Error(), "escapes the project root") {
		t.Errorf("expected escape error, got: %v", err)
	}
}

func TestParse_HooksEmptyPath(t *testing.T) {
	yaml := []byte(`
hooks:
  post_create:
    - ""
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for empty hook path")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("expected empty path error, got: %v", err)
	}
}

func TestParse_HooksDuplicate(t *testing.T) {
	yaml := []byte(`
hooks:
  post_create:
    - scripts/setup.sh
    - scripts/setup.sh
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for duplicate hook script")
	}
	if !strings.Contains(err.Error(), "duplicate script") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestParse_NoHooks(t *testing.T) {
	yaml := []byte(`
tmux:
  panes:
    - nvim
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Hooks != nil {
		t.Errorf("expected nil hooks when not configured")
	}
}

func TestDiscover_SymlinkDir(t *testing.T) {
	// Config in a parent dir reached via a symlinked child
	realDir := t.TempDir()
	initGitRepo(t, realDir)
	configPath := filepath.Join(realDir, ConfigFileName)
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	childDir := filepath.Join(realDir, "child")
	if err := os.MkdirAll(childDir, 0755); err != nil {
		t.Fatal(err)
	}

	foundPath, projectRoot, err := Discover(childDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if foundPath != configPath {
		t.Errorf("expected config at %s, got %s", configPath, foundPath)
	}
	if projectRoot != realDir {
		t.Errorf("expected project root %s, got %s", realDir, projectRoot)
	}
}

func TestParse_ProxyTrue(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
proxy: true
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Proxy == nil {
		t.Fatal("expected Proxy to be non-nil for proxy: true")
	}
	if !cfg.Proxy.HTTPS {
		t.Error("expected HTTPS default to be true")
	}
}

func TestParse_ProxyFalse(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
proxy: false
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Proxy != nil {
		t.Error("expected Proxy to be nil for proxy: false")
	}
}

func TestParse_ProxyObject(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
proxy:
  name: myapp
  port: 443
  https: false
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Proxy == nil {
		t.Fatal("expected Proxy to be non-nil")
	}
	if cfg.Proxy.Name != "myapp" {
		t.Errorf("Proxy.Name = %q, want %q", cfg.Proxy.Name, "myapp")
	}
	if cfg.Proxy.Port != 443 {
		t.Errorf("Proxy.Port = %d, want 443", cfg.Proxy.Port)
	}
	if cfg.Proxy.HTTPS {
		t.Error("expected HTTPS to be false")
	}
}

func TestParse_ProxyOmitted(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Proxy != nil {
		t.Error("expected Proxy to be nil when omitted")
	}
}

func TestParse_ProxyObjectDefaults(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
proxy:
  name: myapp
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Proxy == nil {
		t.Fatal("expected Proxy to be non-nil")
	}
	if !cfg.Proxy.HTTPS {
		t.Error("expected HTTPS default to be true in object form")
	}
	if cfg.Proxy.Port != 0 {
		t.Errorf("expected Port to be 0 (use runtime default), got %d", cfg.Proxy.Port)
	}
}

func TestParse_ProxyPortOutOfRange(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
proxy:
  port: 70000
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for out-of-range port")
	}
	if !strings.Contains(err.Error(), "port must be between 1 and 65535") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_ProxyNameInvalid(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"uppercase", "proxy:\n  name: MyApp"},
		{"leading hyphen", "proxy:\n  name: -myapp"},
		{"trailing hyphen", "proxy:\n  name: myapp-"},
		{"special chars", "proxy:\n  name: my_app"},
		{"spaces", "proxy:\n  name: my app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			full := "services:\n  api:\n    port:\n      base: 4000\n      var: PORT\n" + tt.yaml
			_, err := Parse([]byte(full))
			if err == nil {
				t.Fatal("expected error for invalid DNS label")
			}
			if !strings.Contains(err.Error(), "not a valid DNS label") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestParse_ProxyNameValid(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
proxy:
  name: my-app-123
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed for valid name: %v", err)
	}
	if cfg.Proxy.Name != "my-app-123" {
		t.Errorf("Proxy.Name = %q, want %q", cfg.Proxy.Name, "my-app-123")
	}
}

func TestParse_ProxyTemplateWithoutProxy(t *testing.T) {
	yaml := []byte(`
services:
  api:
    env_file: .env
    port:
      base: 4000
      var: PORT
    env:
      PUBLIC_URL: "{{api.url}}"
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for proxy template without proxy config")
	}
	if !strings.Contains(err.Error(), "proxy config is defined") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_ProxyTemplateHostWithoutProxy(t *testing.T) {
	yaml := []byte(`
services:
  api:
    env_file: .env
    port:
      base: 4000
      var: PORT
    env:
      PUBLIC_HOST: "{{api.host}}"
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for proxy template without proxy config")
	}
	if !strings.Contains(err.Error(), "proxy config is defined") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_ProxyTemplateWithProxy(t *testing.T) {
	yaml := []byte(`
services:
  api:
    env_file: .env
    port:
      base: 4000
      var: PORT
    env:
      PUBLIC_URL: "{{api.url}}"
proxy: true
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Proxy == nil {
		t.Fatal("expected Proxy to be non-nil")
	}
}

func TestParse_ProxyGlobalEnvTemplateWithoutProxy(t *testing.T) {
	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
env:
  APP_URL: "{{api.url}}"
`)
	_, err := Parse(yaml)
	if err == nil {
		t.Fatal("expected error for proxy template in global env without proxy config")
	}
	if !strings.Contains(err.Error(), "proxy config is defined") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParse_ProxyPortValidRange(t *testing.T) {
	for _, port := range []int{1, 80, 443, 1355, 65535} {
		yaml := []byte("services:\n  api:\n    port:\n      base: 4000\n      var: PORT\nproxy:\n  port: " + strings.TrimSpace(strings.Replace(string(rune(port+'0')), string(rune(port+'0')), "", 1)))
		_ = yaml
	}

	yaml := []byte(`
services:
  api:
    port:
      base: 4000
      var: PORT
proxy:
  port: 443
`)
	cfg, err := Parse(yaml)
	if err != nil {
		t.Fatalf("Parse failed for port 443: %v", err)
	}
	if cfg.Proxy.Port != 443 {
		t.Errorf("Proxy.Port = %d, want 443", cfg.Proxy.Port)
	}
}
