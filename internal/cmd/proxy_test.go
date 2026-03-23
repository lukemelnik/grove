package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/certs"
	"github.com/lukemelnik/grove/internal/proxy"
)

func TestProxyStatusCmd_NotRunning(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "status", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy status failed: %v\nOutput: %s", err, buf.String())
	}

	var result proxyStatusOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if result.Running {
		t.Error("expected running=false")
	}
	if len(result.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(result.Projects))
	}
}

func TestProxyStatusCmd_Text(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	mockTerminal(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "status"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy status failed: %v\nOutput: %s", err, buf.String())
	}

	output := buf.String()
	if !strings.Contains(output, "not running") {
		t.Errorf("expected 'not running' in output, got:\n%s", output)
	}
}

func TestProxyProjectsCmd_Empty(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	mockTerminal(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "projects"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy projects failed: %v\nOutput: %s", err, buf.String())
	}

	if !strings.Contains(buf.String(), "No projects registered") {
		t.Errorf("expected 'No projects registered', got:\n%s", buf.String())
	}
}

func TestProxyProjectsCmd_JSON(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "projects", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy projects --json failed: %v\nOutput: %s", err, buf.String())
	}

	var entries []interface{}
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if len(entries) != 0 {
		t.Errorf("expected empty array, got %d entries", len(entries))
	}
}

func TestProxyStopCmd_NotRunning(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	mockTerminal(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "stop"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy stop failed: %v\nOutput: %s", err, buf.String())
	}

	if !strings.Contains(buf.String(), "not running") {
		t.Errorf("expected 'not running', got:\n%s", buf.String())
	}
}

func TestProxyCleanCmd(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	os.WriteFile(filepath.Join(stateDir, "test-file"), []byte("data"), 0644)

	mockTerminal(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "clean"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy clean failed: %v\nOutput: %s", err, buf.String())
	}

	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Errorf("expected state directory to be removed")
	}
}

func TestProxyCleanCmd_JSON(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "clean", "--json"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy clean --json failed: %v\nOutput: %s", err, buf.String())
	}

	var result map[string]string
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nRaw: %s", err, buf.String())
	}

	if result["action"] != "cleaned" {
		t.Errorf("expected action=cleaned, got %q", result["action"])
	}
}

func TestProxyUnregisterCmd_ByName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      var: PORT
proxy: true
`
	repoDir := setupCreateTestRepo(t, groveYML)

	registry := proxy.NewRegistry(stateDir)
	if err := registry.RegisterProject(repoDir); err != nil {
		t.Fatalf("RegisterProject failed: %v", err)
	}

	entries, _ := registry.ListProjects()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	mockTerminal(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "unregister", entries[0].Name})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy unregister failed: %v\nOutput: %s", err, buf.String())
	}

	remaining, _ := registry.ListProjects()
	if len(remaining) != 0 {
		t.Errorf("expected 0 entries after unregister, got %d", len(remaining))
	}
}

func TestProxyStartCmd_AlreadyRunning(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	os.WriteFile(filepath.Join(stateDir, pidFileName), []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)
	os.WriteFile(filepath.Join(stateDir, portFileName), []byte("1355\n"), 0644)

	mockTerminal(t)

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"proxy", "start"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("proxy start failed: %v\nOutput: %s", err, buf.String())
	}

	if !strings.Contains(buf.String(), "already running") {
		t.Errorf("expected 'already running', got:\n%s", buf.String())
	}
}

func TestProxyStartCmd_StalePIDFile(t *testing.T) {
	stateDir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return stateDir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	os.WriteFile(filepath.Join(stateDir, pidFileName), []byte("99999999\n"), 0644)

	running, _ := isProxyRunning(stateDir)
	if running {
		t.Skip("PID 99999999 unexpectedly exists")
	}
}

func TestReadPIDFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, pidFileName), []byte("12345\n"), 0644)

	pid, err := readPIDFile(dir)
	if err != nil {
		t.Fatalf("readPIDFile failed: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected PID 12345, got %d", pid)
	}
}

func TestReadPIDFile_Missing(t *testing.T) {
	dir := t.TempDir()
	_, err := readPIDFile(dir)
	if err == nil {
		t.Fatal("expected error for missing PID file")
	}
}

func TestReadPortFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, portFileName), []byte("1355\n"), 0644)

	port, err := readPortFile(dir)
	if err != nil {
		t.Fatalf("readPortFile failed: %v", err)
	}
	if port != 1355 {
		t.Errorf("expected port 1355, got %d", port)
	}
}

func TestWriteAndReadPIDFile(t *testing.T) {
	dir := t.TempDir()
	if err := writePIDFile(dir, 42); err != nil {
		t.Fatalf("writePIDFile failed: %v", err)
	}
	pid, err := readPIDFile(dir)
	if err != nil {
		t.Fatalf("readPIDFile failed: %v", err)
	}
	if pid != 42 {
		t.Errorf("expected PID 42, got %d", pid)
	}
}

func TestComputeWatchHash_EmptyDir(t *testing.T) {
	stateDir := t.TempDir()
	registry := proxy.NewRegistry(stateDir)

	hash := computeWatchHash(stateDir, registry)
	_ = hash
}

func TestComputeWatchHash_ChangesOnRegistryUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	stateDir := t.TempDir()
	registry := proxy.NewRegistry(stateDir)

	hash1 := computeWatchHash(stateDir, registry)

	worktreeDir := t.TempDir()
	groveYML := `worktree_dir: ` + worktreeDir + `
services:
  api:
    port:
      base: 4000
      var: PORT
proxy: true
`
	repoDir := setupCreateTestRepo(t, groveYML)
	registry.RegisterProject(repoDir)

	hash2 := computeWatchHash(stateDir, registry)
	if hash1 == hash2 {
		t.Error("expected hash to change after project registration")
	}
}
