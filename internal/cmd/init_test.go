package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grove/internal/config"
)

func TestInitCmd_FullInteractive(t *testing.T) {
	tmpDir := t.TempDir()

	// Override getWorkingDir
	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return tmpDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Simulate user input:
	// - Worktree dir: (accept default)
	// - Env files: .env
	// - Add a service? yes
	//   - Name: api
	//   - Port: 4000
	//   - Env var: PORT
	// - Add another service? yes
	//   - Name: web
	//   - Port: 3000
	//   - Env var: WEB_PORT
	// - Add another service? no
	// - Include tmux? yes
	//   - Mode: session
	//   - Layout: main-vertical
	//   - Main size: 70%
	//   - Add pane? yes
	//     - Command: nvim
	//     - Optional: no
	//   - Add pane? yes
	//     - Command: pnpm dev
	//     - Optional: yes
	//   - Add pane? no
	input := strings.Join([]string{
		"",          // worktree dir (accept default)
		".env",      // env files
		"y",         // add a service?
		"api",       // service name
		"4000",      // port
		"PORT",      // env var
		"y",         // add another service?
		"web",       // service name
		"3000",      // port
		"WEB_PORT",  // env var
		"n",         // add another service?
		"y",         // include tmux?
		"session",   // mode
		"main-vertical", // layout
		"70%",       // main size
		"y",         // add pane?
		"nvim",      // command
		"n",         // optional?
		"y",         // add pane?
		"pnpm dev",  // command
		"y",         // optional?
		"n",         // add pane?
	}, "\n")

	origStdin := stdinReader
	stdinReader = strings.NewReader(input)
	defer func() { stdinReader = origStdin }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("init command failed: %v\nOutput: %s", err, buf.String())
	}

	// Verify .grove.yml was written
	configPath := filepath.Join(tmpDir, ".grove.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .grove.yml: %v", err)
	}

	content := string(data)

	// Verify content contains expected sections
	if !strings.Contains(content, "worktree_dir: ../.grove-worktrees") {
		t.Errorf("expected worktree_dir in config, got:\n%s", content)
	}
	if !strings.Contains(content, "env_files:") {
		t.Errorf("expected env_files in config, got:\n%s", content)
	}
	if !strings.Contains(content, ".env") {
		t.Errorf("expected .env in env_files, got:\n%s", content)
	}
	if !strings.Contains(content, "api:") {
		t.Errorf("expected api service in config, got:\n%s", content)
	}
	if !strings.Contains(content, "port: 4000") {
		t.Errorf("expected port 4000 in config, got:\n%s", content)
	}
	if !strings.Contains(content, "env: PORT") {
		t.Errorf("expected env PORT in config, got:\n%s", content)
	}
	if !strings.Contains(content, "web:") {
		t.Errorf("expected web service in config, got:\n%s", content)
	}
	if !strings.Contains(content, "port: 3000") {
		t.Errorf("expected port 3000 in config, got:\n%s", content)
	}
	if !strings.Contains(content, "env: WEB_PORT") {
		t.Errorf("expected env WEB_PORT in config, got:\n%s", content)
	}
	if !strings.Contains(content, "tmux:") {
		t.Errorf("expected tmux section in config, got:\n%s", content)
	}
	if !strings.Contains(content, "mode: session") {
		t.Errorf("expected mode: session in config, got:\n%s", content)
	}
	if !strings.Contains(content, "layout: main-vertical") {
		t.Errorf("expected layout: main-vertical in config, got:\n%s", content)
	}
	if !strings.Contains(content, "main_size: \"70%\"") && !strings.Contains(content, "main_size: 70%") {
		t.Errorf("expected main_size: 70%% in config, got:\n%s", content)
	}
	if !strings.Contains(content, "nvim") {
		t.Errorf("expected nvim pane in config, got:\n%s", content)
	}
	if !strings.Contains(content, "pnpm dev") {
		t.Errorf("expected pnpm dev pane in config, got:\n%s", content)
	}
	if !strings.Contains(content, "optional: true") {
		t.Errorf("expected optional pane in config, got:\n%s", content)
	}

	// Verify output mentions the file was written
	if !strings.Contains(buf.String(), "Wrote") {
		t.Errorf("output should mention writing the file, got:\n%s", buf.String())
	}

	// Verify the generated YAML round-trips through config.Parse
	parsed, err := config.Parse(data)
	if err != nil {
		t.Fatalf("generated .grove.yml failed to parse: %v\nContent:\n%s", err, content)
	}
	if parsed.WorktreeDir != "../.grove-worktrees" {
		t.Errorf("parsed WorktreeDir = %q, want %q", parsed.WorktreeDir, "../.grove-worktrees")
	}
	if len(parsed.Services) != 2 {
		t.Errorf("parsed %d services, want 2", len(parsed.Services))
	}
	if parsed.Tmux == nil {
		t.Fatal("parsed Tmux config is nil, want non-nil")
	}
	if parsed.Tmux.Mode != "session" {
		t.Errorf("parsed Tmux.Mode = %q, want %q", parsed.Tmux.Mode, "session")
	}
	if len(parsed.Tmux.Panes) != 2 {
		t.Errorf("parsed %d panes, want 2", len(parsed.Tmux.Panes))
	}
}

func TestInitCmd_MinimalConfig(t *testing.T) {
	tmpDir := t.TempDir()

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return tmpDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Minimal: accept defaults, no services, no tmux
	input := strings.Join([]string{
		"",  // worktree dir (accept default)
		"",  // env files (default .env — but won't exist)
		"n", // add a service? no
		"n", // include tmux? no
	}, "\n")

	origStdin := stdinReader
	stdinReader = strings.NewReader(input)
	defer func() { stdinReader = origStdin }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("init command failed: %v\nOutput: %s", err, buf.String())
	}

	// Verify .grove.yml was written
	configPath := filepath.Join(tmpDir, ".grove.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .grove.yml: %v", err)
	}

	content := string(data)

	// Should have worktree_dir
	if !strings.Contains(content, "worktree_dir: ../.grove-worktrees") {
		t.Errorf("expected worktree_dir in config, got:\n%s", content)
	}

	// Should NOT have services or tmux
	if strings.Contains(content, "services:") {
		t.Errorf("expected no services section in minimal config, got:\n%s", content)
	}
	if strings.Contains(content, "tmux:") {
		t.Errorf("expected no tmux section in minimal config, got:\n%s", content)
	}
}

func TestInitCmd_OverwriteAbort(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an existing .grove.yml
	existingConfig := filepath.Join(tmpDir, ".grove.yml")
	if err := os.WriteFile(existingConfig, []byte("existing: true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return tmpDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// User says no to overwrite
	input := "n\n"

	origStdin := stdinReader
	stdinReader = strings.NewReader(input)
	defer func() { stdinReader = origStdin }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("init command failed: %v", err)
	}

	// Verify file was NOT overwritten
	data, _ := os.ReadFile(existingConfig)
	if !strings.Contains(string(data), "existing: true") {
		t.Errorf("existing .grove.yml should not be overwritten, got:\n%s", string(data))
	}

	// Output should mention abort
	if !strings.Contains(buf.String(), "Aborted") {
		t.Errorf("output should mention abort, got:\n%s", buf.String())
	}
}

func TestInitCmd_OverwriteConfirm(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an existing .grove.yml
	existingConfig := filepath.Join(tmpDir, ".grove.yml")
	if err := os.WriteFile(existingConfig, []byte("existing: true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return tmpDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// User says yes to overwrite, then minimal config
	input := strings.Join([]string{
		"y",  // overwrite
		"",   // worktree dir
		"",   // env files
		"n",  // add service?
		"n",  // include tmux?
	}, "\n")

	origStdin := stdinReader
	stdinReader = strings.NewReader(input)
	defer func() { stdinReader = origStdin }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("init command failed: %v\nOutput: %s", err, buf.String())
	}

	// Verify file WAS overwritten
	data, _ := os.ReadFile(existingConfig)
	if strings.Contains(string(data), "existing: true") {
		t.Errorf("existing .grove.yml should have been overwritten, got:\n%s", string(data))
	}
}

func TestInitCmd_InvalidPort(t *testing.T) {
	tmpDir := t.TempDir()

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return tmpDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Try to add a service with invalid port, then skip adding more
	input := strings.Join([]string{
		"",       // worktree dir
		"",       // env files
		"y",      // add a service?
		"api",    // service name
		"notaport", // invalid port
		"n",      // add another service? (after skip)
		"n",      // include tmux?
	}, "\n")

	origStdin := stdinReader
	stdinReader = strings.NewReader(input)
	defer func() { stdinReader = origStdin }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("init command failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "Invalid port") {
		t.Errorf("expected invalid port message, got:\n%s", output)
	}

	// Config should still be written (without the invalid service)
	configPath := filepath.Join(tmpDir, ".grove.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .grove.yml: %v", err)
	}
	if strings.Contains(string(data), "api:") {
		t.Errorf("config should not contain invalid service, got:\n%s", string(data))
	}
}

func TestInitCmd_DefaultEnvVarName(t *testing.T) {
	tmpDir := t.TempDir()

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return tmpDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	// Add one service accepting default env var name (PORT for first)
	// Then add another accepting default env var name (WEB_PORT for second)
	input := strings.Join([]string{
		"",     // worktree dir
		"",     // env files
		"y",    // add a service?
		"api",  // service name
		"4000", // port
		"",     // env var name (accept default: PORT for first service)
		"y",    // add another service?
		"web",  // service name
		"3000", // port
		"",     // env var name (accept default: WEB_PORT for second)
		"n",    // add another service?
		"n",    // include tmux?
	}, "\n")

	origStdin := stdinReader
	stdinReader = strings.NewReader(input)
	defer func() { stdinReader = origStdin }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("init command failed: %v\nOutput: %s", err, buf.String())
	}

	configPath := filepath.Join(tmpDir, ".grove.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .grove.yml: %v", err)
	}

	content := string(data)
	// First service should get PORT as default
	if !strings.Contains(content, "env: PORT") {
		t.Errorf("expected first service to have env: PORT, got:\n%s", content)
	}
	// Second service should get WEB_PORT as default
	if !strings.Contains(content, "env: WEB_PORT") {
		t.Errorf("expected second service to have env: WEB_PORT, got:\n%s", content)
	}
}

func TestInitCmd_EnvFileExistsDefault(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a .env file so the default picks it up
	if err := os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("FOO=bar\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origGetWd := getWorkingDir
	getWorkingDir = func() (string, error) { return tmpDir, nil }
	defer func() { getWorkingDir = origGetWd }()

	input := strings.Join([]string{
		"",  // worktree dir
		"",  // env files (default — .env exists)
		"n", // add service?
		"n", // include tmux?
	}, "\n")

	origStdin := stdinReader
	stdinReader = strings.NewReader(input)
	defer func() { stdinReader = origStdin }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"init"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("init command failed: %v\nOutput: %s", err, buf.String())
	}

	configPath := filepath.Join(tmpDir, ".grove.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .grove.yml: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "env_files:") || !strings.Contains(content, ".env") {
		t.Errorf("expected env_files with .env when .env exists, got:\n%s", content)
	}
}
