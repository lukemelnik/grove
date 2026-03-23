package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %s: %v", args, out, err)
		}
	}
}

func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	cmds := [][]string{
		{"git", "add", "."},
		{"git", "commit", "--allow-empty", "-m", msg},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %s: %v", args, out, err)
		}
	}
}

func createGitProject(t *testing.T, baseDir, name string, proxyName string) string {
	t.Helper()
	projectDir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}

	initGitRepo(t, projectDir)

	yaml := `services:
  api:
    port:
      base: 4000
      var: PORT
  web:
    port:
      base: 3000
      var: WEB_PORT
proxy:
  name: ` + proxyName + `
worktree_dir: ../.grove-worktrees/` + name + "\n"

	if err := os.WriteFile(filepath.Join(projectDir, ".grove.yml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	gitCommit(t, projectDir, "initial")
	return projectDir
}

func TestComputeAllRoutes_SingleProject(t *testing.T) {
	dir := t.TempDir()
	projectDir := createGitProject(t, dir, "myapp", "myapp")

	entries := []ProjectEntry{
		{Path: projectDir, Name: "myapp"},
	}

	routes, err := ComputeAllRoutes(entries)
	if err != nil {
		t.Fatalf("ComputeAllRoutes failed: %v", err)
	}

	if len(routes) == 0 {
		t.Fatal("expected at least one route")
	}

	foundAPI := false
	foundWeb := false
	for _, r := range routes {
		if r.Service == "api" && r.Hostname == "api.myapp.localhost" {
			foundAPI = true
			if r.Project != "myapp" {
				t.Errorf("api route project = %q, want %q", r.Project, "myapp")
			}
		}
		if r.Service == "web" && r.Hostname == "web.myapp.localhost" {
			foundWeb = true
		}
	}

	if !foundAPI {
		t.Error("expected route for api.myapp.localhost (default branch)")
	}
	if !foundWeb {
		t.Error("expected route for web.myapp.localhost (default branch)")
	}
}

func TestComputeAllRoutes_FeatureBranch(t *testing.T) {
	dir := t.TempDir()
	projectDir := createGitProject(t, dir, "myapp", "myapp")

	wtDir := filepath.Join(dir, ".grove-worktrees", "myapp", "feat-auth")
	cmd := exec.Command("git", "worktree", "add", "-b", "feat/auth", wtDir)
	cmd.Dir = projectDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creating worktree: %s: %v", out, err)
	}

	entries := []ProjectEntry{
		{Path: projectDir, Name: "myapp"},
	}

	routes, err := ComputeAllRoutes(entries)
	if err != nil {
		t.Fatalf("ComputeAllRoutes failed: %v", err)
	}

	foundFeatureAPI := false
	foundDefaultAPI := false
	for _, r := range routes {
		if r.Service == "api" && r.Branch == "feat/auth" {
			foundFeatureAPI = true
			if r.Hostname != "api.feat-auth.myapp.localhost" {
				t.Errorf("feature branch api hostname = %q, want %q", r.Hostname, "api.feat-auth.myapp.localhost")
			}
		}
		if r.Service == "api" && r.Branch == "main" {
			foundDefaultAPI = true
			if r.Hostname != "api.myapp.localhost" {
				t.Errorf("default branch api hostname = %q, want %q", r.Hostname, "api.myapp.localhost")
			}
		}
	}

	if !foundFeatureAPI {
		t.Error("expected route for feature branch api")
	}
	if !foundDefaultAPI {
		t.Error("expected route for default branch api")
	}
}

func TestComputeAllRoutes_MultiProject(t *testing.T) {
	dir := t.TempDir()
	project1 := createGitProject(t, dir, "app1", "app1")
	project2 := createGitProject(t, dir, "app2", "app2")

	entries := []ProjectEntry{
		{Path: project1, Name: "app1"},
		{Path: project2, Name: "app2"},
	}

	routes, err := ComputeAllRoutes(entries)
	if err != nil {
		t.Fatalf("ComputeAllRoutes failed: %v", err)
	}

	projects := make(map[string]bool)
	for _, r := range routes {
		projects[r.Project] = true
	}

	if !projects["app1"] {
		t.Error("expected routes from app1")
	}
	if !projects["app2"] {
		t.Error("expected routes from app2")
	}
}

func TestComputeAllRoutes_SkipsEnvOnlyServices(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "envonly")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}

	initGitRepo(t, projectDir)

	yaml := `services:
  api:
    port:
      base: 4000
      var: PORT
  desktop:
    env_file: apps/desktop/.env
proxy:
  name: envonly
`
	if err := os.WriteFile(filepath.Join(projectDir, ".grove.yml"), []byte(yaml), 0644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	gitCommit(t, projectDir, "initial")

	entries := []ProjectEntry{
		{Path: projectDir, Name: "envonly"},
	}

	routes, err := ComputeAllRoutes(entries)
	if err != nil {
		t.Fatalf("ComputeAllRoutes failed: %v", err)
	}

	for _, r := range routes {
		if r.Service == "desktop" {
			t.Error("env-only service 'desktop' should not have a route")
		}
	}
}

func TestComputeAllRoutes_EmptyEntries(t *testing.T) {
	routes, err := ComputeAllRoutes(nil)
	if err != nil {
		t.Fatalf("ComputeAllRoutes failed: %v", err)
	}
	if len(routes) != 0 {
		t.Errorf("expected 0 routes for empty entries, got %d", len(routes))
	}
}

func TestRouteTable_LookupAndUpdate(t *testing.T) {
	rt := NewRouteTable()

	routes := []Route{
		{Hostname: "api.myapp.localhost", Target: "127.0.0.1:4000", Project: "myapp", Service: "api", Branch: "main"},
		{Hostname: "web.myapp.localhost", Target: "127.0.0.1:3000", Project: "myapp", Service: "web", Branch: "main"},
	}

	rt.Update(routes)

	r, ok := rt.Lookup("api.myapp.localhost")
	if !ok {
		t.Fatal("expected route for api.myapp.localhost")
	}
	if r.Target != "127.0.0.1:4000" {
		t.Errorf("target = %q, want %q", r.Target, "127.0.0.1:4000")
	}

	_, ok = rt.Lookup("nonexistent.localhost")
	if ok {
		t.Error("expected no route for nonexistent.localhost")
	}

	all := rt.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d routes, want 2", len(all))
	}
}

func TestRouteTable_UpdateReplacesAll(t *testing.T) {
	rt := NewRouteTable()

	rt.Update([]Route{
		{Hostname: "old.localhost", Target: "127.0.0.1:4000"},
	})

	rt.Update([]Route{
		{Hostname: "new.localhost", Target: "127.0.0.1:5000"},
	})

	_, ok := rt.Lookup("old.localhost")
	if ok {
		t.Error("old route should be removed after full update")
	}

	r, ok := rt.Lookup("new.localhost")
	if !ok {
		t.Fatal("new route should exist")
	}
	if r.Target != "127.0.0.1:5000" {
		t.Errorf("target = %q, want %q", r.Target, "127.0.0.1:5000")
	}
}
