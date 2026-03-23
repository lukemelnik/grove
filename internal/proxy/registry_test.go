package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func createTestProject(t *testing.T, dir, name string, withProxy bool) string {
	t.Helper()
	projectDir := filepath.Join(dir, name)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}

	var yamlContent string
	if withProxy {
		yamlContent = `services:
  api:
    port:
      base: 4000
      var: PORT
proxy:
  name: ` + name + "\n"
	} else {
		yamlContent = `services:
  api:
    port:
      base: 4000
      var: PORT
`
	}

	if err := os.WriteFile(filepath.Join(projectDir, ".grove.yml"), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	return projectDir
}

func TestRegisterProject_Basic(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	projectDir := createTestProject(t, dir, "myapp", true)

	reg := NewRegistry(stateDir)
	if err := reg.RegisterProject(projectDir); err != nil {
		t.Fatalf("RegisterProject failed: %v", err)
	}

	entries, err := reg.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].Name != "myapp" {
		t.Errorf("entry name = %q, want %q", entries[0].Name, "myapp")
	}
}

func TestRegisterProject_Deduplicates(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	projectDir := createTestProject(t, dir, "myapp", true)

	reg := NewRegistry(stateDir)
	if err := reg.RegisterProject(projectDir); err != nil {
		t.Fatalf("first RegisterProject failed: %v", err)
	}
	if err := reg.RegisterProject(projectDir); err != nil {
		t.Fatalf("second RegisterProject failed: %v", err)
	}

	entries, err := reg.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 entry after dedup, got %d", len(entries))
	}
}

func TestRegisterProject_NameCollision(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	project1 := filepath.Join(dir, "path1", "myapp")
	if err := os.MkdirAll(project1, 0755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project1, ".grove.yml"), []byte("services:\n  api:\n    port:\n      base: 4000\n      var: PORT\nproxy:\n  name: myapp\n"), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	project2 := filepath.Join(dir, "path2", "myapp")
	if err := os.MkdirAll(project2, 0755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project2, ".grove.yml"), []byte("services:\n  api:\n    port:\n      base: 4000\n      var: PORT\nproxy:\n  name: myapp\n"), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	reg := NewRegistry(stateDir)
	if err := reg.RegisterProject(project1); err != nil {
		t.Fatalf("first RegisterProject failed: %v", err)
	}

	err := reg.RegisterProject(project2)
	if err == nil {
		t.Fatal("expected name collision error")
	}
}

func TestRegisterProject_MultipleProjects(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	project1 := createTestProject(t, dir, "app1", true)
	project2 := createTestProject(t, dir, "app2", true)

	reg := NewRegistry(stateDir)
	if err := reg.RegisterProject(project1); err != nil {
		t.Fatalf("RegisterProject(app1) failed: %v", err)
	}
	if err := reg.RegisterProject(project2); err != nil {
		t.Fatalf("RegisterProject(app2) failed: %v", err)
	}

	entries, err := reg.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestLoadAndPrune_RemovesMissing(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	project1 := createTestProject(t, dir, "exists", true)
	project2 := createTestProject(t, dir, "willdelete", true)

	reg := NewRegistry(stateDir)
	if err := reg.RegisterProject(project1); err != nil {
		t.Fatalf("RegisterProject(exists) failed: %v", err)
	}
	if err := reg.RegisterProject(project2); err != nil {
		t.Fatalf("RegisterProject(willdelete) failed: %v", err)
	}

	if err := os.RemoveAll(project2); err != nil {
		t.Fatalf("removing project dir: %v", err)
	}

	entries, err := reg.LoadAndPrune()
	if err != nil {
		t.Fatalf("LoadAndPrune failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after pruning, got %d", len(entries))
	}

	if entries[0].Name != "exists" {
		t.Errorf("remaining entry = %q, want %q", entries[0].Name, "exists")
	}
}

func TestLoadAndPrune_RemovesNoProxyConfig(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	project := createTestProject(t, dir, "myapp", true)

	reg := NewRegistry(stateDir)
	if err := reg.RegisterProject(project); err != nil {
		t.Fatalf("RegisterProject failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(project, ".grove.yml"), []byte("services:\n  api:\n    port:\n      base: 4000\n      var: PORT\n"), 0644); err != nil {
		t.Fatalf("rewriting config: %v", err)
	}

	entries, err := reg.LoadAndPrune()
	if err != nil {
		t.Fatalf("LoadAndPrune failed: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries after pruning project without proxy config, got %d", len(entries))
	}
}

func TestUnregisterProject(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	project := createTestProject(t, dir, "myapp", true)

	reg := NewRegistry(stateDir)
	if err := reg.RegisterProject(project); err != nil {
		t.Fatalf("RegisterProject failed: %v", err)
	}

	if err := reg.UnregisterProject(project); err != nil {
		t.Fatalf("UnregisterProject failed: %v", err)
	}

	entries, err := reg.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries after unregister, got %d", len(entries))
	}
}

func TestUnregisterProject_NotRegistered(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	reg := NewRegistry(stateDir)
	err := reg.UnregisterProject("/nonexistent")
	if err == nil {
		t.Error("expected error for unregistering non-existent project")
	}
}

func TestUnregisterByName(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	project := createTestProject(t, dir, "myapp", true)

	reg := NewRegistry(stateDir)
	if err := reg.RegisterProject(project); err != nil {
		t.Fatalf("RegisterProject failed: %v", err)
	}

	if err := reg.UnregisterByName("myapp"); err != nil {
		t.Fatalf("UnregisterByName failed: %v", err)
	}

	entries, err := reg.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries after unregister by name, got %d", len(entries))
	}
}

func TestRegistry_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	reg := NewRegistry(stateDir)
	entries, err := reg.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty registry, got %d", len(entries))
	}
}
