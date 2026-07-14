package env

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/config"
)

func TestParseEnvContent_Basic(t *testing.T) {
	content := `
KEY1=value1
KEY2=value2
`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["KEY1"] != "value1" {
		t.Errorf("expected KEY1=value1, got %s", result["KEY1"])
	}
	if result["KEY2"] != "value2" {
		t.Errorf("expected KEY2=value2, got %s", result["KEY2"])
	}
}

func TestParseEnvContent_Comments(t *testing.T) {
	content := `
# This is a comment
KEY1=value1
# Another comment
KEY2=value2
`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 vars, got %d", len(result))
	}
	if result["KEY1"] != "value1" {
		t.Errorf("expected KEY1=value1, got %s", result["KEY1"])
	}
}

func TestParseEnvContent_EmptyLines(t *testing.T) {
	content := `

KEY1=value1

KEY2=value2

`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 vars, got %d", len(result))
	}
}

func TestParseEnvContent_QuotedValues(t *testing.T) {
	content := `
DOUBLE_QUOTED="hello world"
SINGLE_QUOTED='hello world'
UNQUOTED=hello
`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["DOUBLE_QUOTED"] != "hello world" {
		t.Errorf("expected 'hello world', got %q", result["DOUBLE_QUOTED"])
	}
	if result["SINGLE_QUOTED"] != "hello world" {
		t.Errorf("expected 'hello world', got %q", result["SINGLE_QUOTED"])
	}
	if result["UNQUOTED"] != "hello" {
		t.Errorf("expected 'hello', got %q", result["UNQUOTED"])
	}
}

func TestParseEnvContent_EmptyValue(t *testing.T) {
	content := `KEY=`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val, ok := result["KEY"]; !ok || val != "" {
		t.Errorf("expected KEY with empty value, got %q (ok=%v)", val, ok)
	}
}

func TestParseEnvContent_ValueWithEquals(t *testing.T) {
	content := `DATABASE_URL=postgres://user:pass@host/db?sslmode=require`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "postgres://user:pass@host/db?sslmode=require"
	if result["DATABASE_URL"] != expected {
		t.Errorf("expected %q, got %q", expected, result["DATABASE_URL"])
	}
}

func TestParseEnvContent_WhitespaceAroundKey(t *testing.T) {
	content := `  KEY  =  value  `
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["KEY"] != "value" {
		t.Errorf("expected KEY=value, got KEY=%q", result["KEY"])
	}
}

func TestParseEnvContent_NoEqualsSign(t *testing.T) {
	content := `
KEY1=value1
THIS_HAS_NO_EQUALS
KEY2=value2
`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 vars, got %d", len(result))
	}
}

func TestParseEnvContent_EmptyContent(t *testing.T) {
	result, err := ParseEnvContent("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	content := []byte("API_KEY=secret123\nPORT=3000\n")
	if err := os.WriteFile(envPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ParseEnvFile(envPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["API_KEY"] != "secret123" {
		t.Errorf("expected API_KEY=secret123, got %s", result["API_KEY"])
	}
	if result["PORT"] != "3000" {
		t.Errorf("expected PORT=3000, got %s", result["PORT"])
	}
}

func TestParseEnvFile_NotFound(t *testing.T) {
	_, err := ParseEnvFile("/nonexistent/.env")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestResolveTemplates_Basic(t *testing.T) {
	ports := map[string]int{
		"api": 4045,
		"web": 3045,
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"http://localhost:{{api.port}}", "http://localhost:4045"},
		{"http://localhost:{{web.port}}", "http://localhost:3045"},
		{"{{api.port}}", "4045"},
		{"no templates here", "no templates here"},
		{"api={{api.port}}&web={{web.port}}", "api=4045&web=3045"},
	}

	for _, tt := range tests {
		result, err := ResolveTemplates(tt.input, ports, "")
		if err != nil {
			t.Errorf("ResolveTemplates(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if result != tt.expected {
			t.Errorf("ResolveTemplates(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestResolveTemplates_UnknownService(t *testing.T) {
	ports := map[string]int{"api": 4045}

	_, err := ResolveTemplates("{{unknown.port}}", ports, "")
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func TestResolveTemplates_HyphenatedService(t *testing.T) {
	result, err := ResolveTemplates("http://localhost:{{web-app.port}}", map[string]int{"web-app": 3045}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "http://localhost:3045" {
		t.Fatalf("resolved template = %q, want %q", result, "http://localhost:3045")
	}
}

func TestResolveTemplates_UnsupportedSyntaxDoesNotPassThrough(t *testing.T) {
	value := "prefix {{web app.port}} suffix"
	result, err := ResolveTemplates(value, map[string]int{"web": 3045}, "")
	if err == nil {
		t.Fatal("expected unsupported template syntax error")
	}
	if result != value {
		t.Fatalf("result = %q, want unchanged input on error", result)
	}
	if strings.Contains(err.Error(), value) {
		t.Fatalf("error exposed environment value: %v", err)
	}
}

func TestResolveTemplates_EmptyPorts(t *testing.T) {
	result, err := ResolveTemplates("no templates", map[string]int{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "no templates" {
		t.Errorf("expected unchanged string, got %q", result)
	}
}

func TestResolveTemplates_Branch(t *testing.T) {
	result, err := ResolveTemplates("{{branch}}", map[string]int{}, "feat/auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "feat/auth" {
		t.Errorf("expected branch template to resolve, got %q", result)
	}
}

func TestResolveTemplates_BranchHash(t *testing.T) {
	result, err := ResolveTemplates("db_{{branch.hash}}", map[string]int{}, "feat/auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "db_3a9c5546d5c1" {
		t.Errorf("expected branch hash template to resolve, got %q", result)
	}
}

func TestResolve_FullPipeline(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	envContent := []byte("EXISTING_VAR=from_env_file\nPORT=9999\n")
	if err := os.WriteFile(envPath, envContent, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		EnvFiles: []string{".env"},
		Services: map[string]config.Service{
			"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
			"web": {Port: config.ServicePort{Base: 3000, Env: "WEB_PORT"}},
		},
		Env: map[string]string{
			"VITE_API_URL": "http://localhost:{{api.port}}",
			"CORS_ORIGIN":  "http://localhost:{{web.port}}",
			"CUSTOM":       "static_value",
		},
	}

	ports := map[string]int{
		"api": 4045,
		"web": 3045,
	}

	result, err := Resolve(cfg, ports, "", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["EXISTING_VAR"] != "from_env_file" {
		t.Errorf("expected EXISTING_VAR from env file, got %q", result["EXISTING_VAR"])
	}
	if result["VITE_API_URL"] != "http://localhost:4045" {
		t.Errorf("expected VITE_API_URL with resolved port, got %q", result["VITE_API_URL"])
	}
	if result["CORS_ORIGIN"] != "http://localhost:3045" {
		t.Errorf("expected CORS_ORIGIN with resolved port, got %q", result["CORS_ORIGIN"])
	}
	if result["CUSTOM"] != "static_value" {
		t.Errorf("expected CUSTOM=static_value, got %q", result["CUSTOM"])
	}
	if result["PORT"] != "4045" {
		t.Errorf("expected PORT=4045 (from service), got %q", result["PORT"])
	}
	if result["WEB_PORT"] != "3045" {
		t.Errorf("expected WEB_PORT=3045, got %q", result["WEB_PORT"])
	}
}

func TestResolve_Precedence(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	envContent := []byte("PORT=1111\nCUSTOM=from_file\n")
	if err := os.WriteFile(envPath, envContent, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		EnvFiles: []string{".env"},
		Services: map[string]config.Service{
			"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
		},
		Env: map[string]string{
			"CUSTOM": "from_env_block",
		},
	}

	ports := map[string]int{"api": 4045}

	result, err := Resolve(cfg, ports, "", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["CUSTOM"] != "from_env_block" {
		t.Errorf("expected CUSTOM=from_env_block, got %q", result["CUSTOM"])
	}
	if result["PORT"] != "4045" {
		t.Errorf("expected PORT=4045 (service wins over env file), got %q", result["PORT"])
	}
}

func TestResolve_NoEnvFiles(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.Service{
			"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
		},
	}

	ports := map[string]int{"api": 4050}

	result, err := Resolve(cfg, ports, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["PORT"] != "4050" {
		t.Errorf("expected PORT=4050, got %q", result["PORT"])
	}
}

func TestResolve_MissingEnvFile(t *testing.T) {
	cfg := &config.Config{
		EnvFiles: []string{".env.nonexistent"},
	}

	_, err := Resolve(cfg, map[string]int{}, "", "/tmp")
	if err == nil {
		t.Fatal("expected error for missing env file")
	}
}

func TestResolve_TemplateError(t *testing.T) {
	cfg := &config.Config{
		Env: map[string]string{
			"URL": "http://localhost:{{unknown.port}}",
		},
	}

	_, err := Resolve(cfg, map[string]int{}, "", "")
	if err == nil {
		t.Fatal("expected error for unknown service in template")
	}
}

func TestResolve_MultipleEnvFiles(t *testing.T) {
	dir := t.TempDir()

	env1 := filepath.Join(dir, ".env")
	if err := os.WriteFile(env1, []byte("KEY1=from_first\nSHARED=first\n"), 0644); err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(dir, "apps", "api")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	env2 := filepath.Join(subdir, ".env")
	if err := os.WriteFile(env2, []byte("KEY2=from_second\nSHARED=second\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		EnvFiles: []string{".env", "apps/api/.env"},
	}

	result, err := Resolve(cfg, map[string]int{}, "", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["KEY1"] != "from_first" {
		t.Errorf("expected KEY1=from_first, got %q", result["KEY1"])
	}
	if result["KEY2"] != "from_second" {
		t.Errorf("expected KEY2=from_second, got %q", result["KEY2"])
	}
	if result["SHARED"] != "second" {
		t.Errorf("expected SHARED=second (last env file wins), got %q", result["SHARED"])
	}
}

func TestResolve_EmptyConfig(t *testing.T) {
	cfg := &config.Config{}
	result, err := Resolve(cfg, map[string]int{}, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

func TestParseEnvContent_QuotedWithSpaces(t *testing.T) {
	content := `MSG="hello   world"`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["MSG"] != "hello   world" {
		t.Errorf("expected 'hello   world', got %q", result["MSG"])
	}
}

func TestParseEnvContent_InlineComments(t *testing.T) {
	content := `
KEY1=value1 # this is a comment
KEY2="value2 # not a comment"
KEY3='value3 # not a comment'
KEY4=value4
KEY5=no_comment_here
`
	result, err := ParseEnvContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["KEY1"] != "value1" {
		t.Errorf("expected KEY1=value1, got %q", result["KEY1"])
	}
	if result["KEY2"] != "value2 # not a comment" {
		t.Errorf("expected KEY2='value2 # not a comment', got %q", result["KEY2"])
	}
	if result["KEY3"] != "value3 # not a comment" {
		t.Errorf("expected KEY3='value3 # not a comment', got %q", result["KEY3"])
	}
	if result["KEY4"] != "value4" {
		t.Errorf("expected KEY4=value4, got %q", result["KEY4"])
	}
	if result["KEY5"] != "no_comment_here" {
		t.Errorf("expected KEY5=no_comment_here, got %q", result["KEY5"])
	}
}

func TestBuildManagedEnv_SessionEnv(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.Service{
			"api": {
				Port:    config.ServicePort{Base: 4000, Env: "PORT"},
				EnvFile: "apps/api/.env",
				Env: map[string]string{
					"API_URL": "http://localhost:{{api.port}}",
				},
			},
			"web": {
				Port:    config.ServicePort{Base: 3000, Env: "WEB_PORT"},
				EnvFile: "apps/web/.env",
				Env: map[string]string{
					"API_URL": "http://localhost:{{web.port}}",
				},
			},
		},
		Env: map[string]string{
			"GLOBAL_URL": "http://localhost:{{api.port}}",
		},
	}

	ports := map[string]int{"api": 4045, "web": 3045}
	managed, err := BuildManagedEnv(cfg, ports, "feat/auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := managed.SessionEnv()

	if result["PORT"] != "4045" {
		t.Errorf("expected PORT=4045, got %q", result["PORT"])
	}
	if result["WEB_PORT"] != "3045" {
		t.Errorf("expected WEB_PORT=3045, got %q", result["WEB_PORT"])
	}
	if result["GLOBAL_URL"] != "http://localhost:4045" {
		t.Errorf("expected GLOBAL_URL resolved, got %q", result["GLOBAL_URL"])
	}
	if _, exists := result["API_URL"]; exists {
		t.Fatalf("service-scoped env vars should not be injected into session env, got %v", result)
	}
}

func TestManagedEnv_EnvLocalMappings(t *testing.T) {
	dir := t.TempDir()

	apiDir := filepath.Join(dir, "apps", "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, ".env"), []byte("PORT=4000\nDB_URL=postgres://...\n"), 0644); err != nil {
		t.Fatal(err)
	}

	webDir := filepath.Join(dir, "apps", "web")
	if err := os.MkdirAll(webDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, ".env"), []byte("WEB_PORT=3000\nVITE_API_URL=http://localhost:4000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		EnvFiles: []string{".env"},
		Services: map[string]config.Service{
			"api": {
				Port:    config.ServicePort{Base: 4000, Env: "PORT"},
				EnvFile: "apps/api/.env",
				Env: map[string]string{
					"API_URL": "http://localhost:{{api.port}}",
				},
			},
			"web": {
				Port:    config.ServicePort{Base: 3000, Env: "WEB_PORT"},
				EnvFile: "apps/web/.env",
				Env: map[string]string{
					"API_URL":            "http://localhost:{{web.port}}",
					"VITE_WORKTREE_NAME": "{{branch}}",
				},
			},
		},
		Env: map[string]string{
			"GLOBAL_FLAG": "enabled",
		},
	}

	rootEnv := filepath.Join(dir, ".env")
	if err := os.WriteFile(rootEnv, []byte("ROOT_ONLY=yes\nORPHAN_PORT=1234\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ports := map[string]int{
		"api": 4045,
		"web": 3045,
	}

	managed, err := BuildManagedEnv(cfg, ports, "feat/auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mappings, err := managed.EnvLocalMappings(cfg, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rootLocal := findMapping(mappings, ".env.local")
	if rootLocal == nil {
		t.Fatal("expected .env.local mapping")
	}
	if rootLocal.Vars["GLOBAL_FLAG"] != "enabled" {
		t.Errorf("expected global env in root .env.local, got %v", rootLocal.Vars)
	}

	apiLocal := findMapping(mappings, "apps/api/.env.local")
	if apiLocal == nil {
		t.Fatal("expected apps/api/.env.local mapping")
	}
	if apiLocal.Vars["GLOBAL_FLAG"] != "enabled" {
		t.Errorf("expected global env in api .env.local, got %v", apiLocal.Vars)
	}
	if apiLocal.Vars["PORT"] != "4045" {
		t.Errorf("expected PORT in api .env.local, got %v", apiLocal.Vars)
	}
	if apiLocal.Vars["API_URL"] != "http://localhost:4045" {
		t.Errorf("expected API_URL in api .env.local, got %v", apiLocal.Vars)
	}

	webLocal := findMapping(mappings, "apps/web/.env.local")
	if webLocal == nil {
		t.Fatal("expected apps/web/.env.local mapping")
	}
	if webLocal.Vars["GLOBAL_FLAG"] != "enabled" {
		t.Errorf("expected global env in web .env.local, got %v", webLocal.Vars)
	}
	if webLocal.Vars["WEB_PORT"] != "3045" {
		t.Errorf("expected WEB_PORT in web .env.local")
	}
	if webLocal.Vars["API_URL"] != "http://localhost:3045" {
		t.Errorf("expected service-specific API_URL in web .env.local, got %v", webLocal.Vars)
	}
	if webLocal.Vars["VITE_WORKTREE_NAME"] != "feat/auth" {
		t.Errorf("expected branch template in web .env.local, got %v", webLocal.Vars)
	}
}

func TestManagedEnv_EnvLocalMappings_RoutesOrphanPortByTopLevelEnvFile(t *testing.T) {
	dir := t.TempDir()

	rootEnv := filepath.Join(dir, ".env")
	if err := os.WriteFile(rootEnv, []byte("PORT=3000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		EnvFiles: []string{".env"},
		Services: map[string]config.Service{
			"api": {
				Port: config.ServicePort{Base: 4000, Env: "PORT"},
			},
		},
	}

	managed, err := BuildManagedEnv(cfg, map[string]int{"api": 4045}, "feat/auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mappings, err := managed.EnvLocalMappings(cfg, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rootLocal := findMapping(mappings, ".env.local")
	if rootLocal == nil {
		t.Fatal("expected .env.local mapping")
	}
	if rootLocal.Vars["PORT"] != "4045" {
		t.Errorf("expected orphan port to fall back to top-level env file, got %v", rootLocal.Vars)
	}
}

func findMapping(mappings []EnvLocalMapping, relPath string) *EnvLocalMapping {
	for i := range mappings {
		if mappings[i].RelPath == relPath {
			return &mappings[i]
		}
	}
	return nil
}

func TestWriteEnvLocals(t *testing.T) {
	dir := t.TempDir()

	mappings := []EnvLocalMapping{
		{
			RelPath: "apps/api/.env.local",
			Vars:    map[string]string{"PORT": "4045", "DEBUG": "true"},
		},
	}

	if err := WriteEnvLocals(mappings, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "apps", "api", ".env.local"))
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "# Generated by grove") {
		t.Error("expected header comment")
	}
	if !strings.Contains(s, "PORT=4045") {
		t.Error("expected PORT=4045")
	}
	if !strings.Contains(s, "DEBUG=true") {
		t.Error("expected DEBUG=true")
	}
}

func TestSymlinkEnvFiles(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()

	apiDir := filepath.Join(mainRepo, "apps", "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatal(err)
	}
	envContent := []byte("PORT=4000\n")
	if err := os.WriteFile(filepath.Join(apiDir, ".env"), envContent, 0644); err != nil {
		t.Fatal(err)
	}

	envFiles := []string{"apps/api/.env"}

	if err := SymlinkEnvFiles(envFiles, mainRepo, worktree); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wtEnv := filepath.Join(worktree, "apps", "api", ".env")
	info, err := os.Lstat(wtEnv)
	if err != nil {
		t.Fatalf("symlink not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink, got regular file")
	}

	target, err := os.Readlink(wtEnv)
	if err != nil {
		t.Fatalf("failed to read symlink: %v", err)
	}

	expectedRel, _ := filepath.Rel(filepath.Join(worktree, "apps", "api"), filepath.Join(mainRepo, "apps", "api", ".env"))
	if target != expectedRel {
		t.Errorf("expected relative symlink %q, got %q", expectedRel, target)
	}

	// Reading through the symlink should work
	data, err := os.ReadFile(wtEnv)
	if err != nil {
		t.Fatalf("failed to read through symlink: %v", err)
	}
	if string(data) != "PORT=4000\n" {
		t.Errorf("expected PORT=4000, got %q", string(data))
	}
}

func TestSymlinkEnvFiles_SkipsMissing(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()

	envFiles := []string{"nonexistent/.env"}

	if err := SymlinkEnvFiles(envFiles, mainRepo, worktree); err != nil {
		t.Fatalf("expected no error for missing env file, got: %v", err)
	}
}

func TestSymlinkEnvFiles_MissingSourceLeavesExistingDestinationUntouched(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	destination := filepath.Join(worktree, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(destination))
	want := []byte("user data stays\n")
	if err := os.WriteFile(destination, want, 0600); err != nil {
		t.Fatal(err)
	}

	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree); err != nil {
		t.Fatalf("missing optional source returned error: %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(want) {
		t.Fatalf("destination changed for missing source: got=%q err=%v", got, err)
	}
}

func TestSymlinkEnvFiles_Idempotent(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()

	if err := os.WriteFile(filepath.Join(mainRepo, ".env"), []byte("KEY=val\n"), 0644); err != nil {
		t.Fatal(err)
	}

	envFiles := []string{".env"}

	if err := SymlinkEnvFiles(envFiles, mainRepo, worktree); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if err := SymlinkEnvFiles(envFiles, mainRepo, worktree); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}

func TestSymlinkEnvFiles_SameRootRegularNoMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("TOKEN=kept\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	oldRename := renameFile
	renameFile = func(_, _ string) error { return errors.New("rename must not run for the main worktree") }
	if err := SymlinkEnvFiles([]string{"config/secrets"}, root, root); err != nil {
		renameFile = oldRename
		t.Fatalf("unexpected error: %v", err)
	}
	renameFile = oldRename

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "TOKEN=kept\n" || after.Mode() != before.Mode() {
		t.Fatalf("regular source/destination was mutated: data=%q mode=%v", data, after.Mode())
	}
}

func TestSymlinkEnvFiles_SameValidExternalSymlinkPreserved(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(t.TempDir(), "actual")
	if err := os.WriteFile(external, []byte("TOKEN=external\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(link))
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}

	if err := SymlinkEnvFiles([]string{"config/secrets"}, root, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if got != external {
		t.Fatalf("symlink target changed: got %q want %q", got, external)
	}
}

func TestSymlinkEnvFiles_SameBrokenSymlinkErrorsUnchanged(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(link))
	if err := os.Symlink("missing", link); err != nil {
		t.Fatal(err)
	}

	if err := SymlinkEnvFiles([]string{"config/secrets"}, root, root); err == nil {
		t.Fatal("expected broken source error")
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if got != "missing" {
		t.Fatalf("broken symlink changed: %q", got)
	}
}

func TestSymlinkEnvFiles_SameSelfSymlinkErrorsUnchanged(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(link))
	if err := os.Symlink("secrets", link); err != nil {
		t.Fatal(err)
	}

	err := SymlinkEnvFiles([]string{"config/secrets"}, root, root)
	if err == nil {
		t.Fatal("expected self-referential canonical source error")
	}
	if !strings.Contains(err.Error(), "cyclic") && !strings.Contains(err.Error(), "broken") {
		t.Fatalf("expected actionable corrupt-source error, got %v", err)
	}
	got, readErr := os.Readlink(link)
	if readErr != nil || got != "secrets" {
		t.Fatalf("self-link changed: target=%q err=%v", got, readErr)
	}
}

func TestSymlinkEnvFiles_DestinationSymlinkBehaviors(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	src := filepath.Join(mainRepo, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(src))
	if err := os.WriteFile(src, []byte("TOKEN=main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(worktree, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(dst))

	if err := os.Symlink("missing", dst); err != nil {
		t.Fatal(err)
	}
	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree); err != nil {
		t.Fatalf("repair missing relative link: %v", err)
	}
	assertSymlinkResolvesTo(t, dst, src)

	before, _ := os.Readlink(dst)
	oldRename := renameFile
	renameFile = func(_, _ string) error { return errors.New("rename must not be called for a correct link") }
	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree); err != nil {
		renameFile = oldRename
		t.Fatalf("idempotent correct link: %v", err)
	}
	renameFile = oldRename
	after, _ := os.Readlink(dst)
	if after != before {
		t.Fatalf("correct symlink was replaced: before=%q after=%q", before, after)
	}

	other := filepath.Join(mainRepo, "config", "other")
	if err := os.WriteFile(other, []byte("OTHER=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dst); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, dst); err != nil {
		t.Fatal(err)
	}
	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree); err != nil {
		t.Fatalf("repair incorrect link: %v", err)
	}
	assertSymlinkResolvesTo(t, dst, src)

	if err := os.Remove(dst); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("secrets", dst); err != nil {
		t.Fatal(err)
	}
	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree); err != nil {
		t.Fatalf("repair self link: %v", err)
	}
	assertSymlinkResolvesTo(t, dst, src)
}

func TestSymlinkEnvFiles_PreflightFailuresLeaveDestinationsUntouched(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	firstSrc := filepath.Join(mainRepo, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(firstSrc))
	if err := os.WriteFile(firstSrc, []byte("TOKEN=main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	firstDst := filepath.Join(worktree, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(firstDst))
	if err := os.Symlink("old", firstDst); err != nil {
		t.Fatal(err)
	}
	collisionSrc := filepath.Join(mainRepo, "services", "api", "config", "secrets")
	mustMkdirAll(t, filepath.Dir(collisionSrc))
	if err := os.WriteFile(collisionSrc, []byte("API=main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	collisionDst := filepath.Join(worktree, "services", "api", "config", "secrets")
	mustMkdirAll(t, filepath.Dir(collisionDst))
	if err := os.WriteFile(collisionDst, []byte("user\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := SymlinkEnvFiles([]string{"config/secrets", "services/api/config/secrets"}, mainRepo, worktree)
	if err == nil {
		t.Fatal("expected preflight collision error")
	}
	got, _ := os.Readlink(firstDst)
	if got != "old" {
		t.Fatalf("first mapping mutated despite second preflight failure: %q", got)
	}
	data, _ := os.ReadFile(collisionDst)
	if string(data) != "user\n" {
		t.Fatalf("regular destination changed: %q", data)
	}
}

func TestSymlinkEnvFiles_UnreadableSourceFailsPreflight(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	src := filepath.Join(mainRepo, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(src))
	if err := os.WriteFile(src, []byte("TOKEN=main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(src, 0o600) })
	if f, err := os.Open(src); err == nil {
		_ = f.Close()
		t.Skip("runtime bypasses file permissions")
	}

	err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree)
	if err == nil {
		t.Fatal("expected unreadable source error")
	}
	if !strings.Contains(err.Error(), "opening canonical source config/secrets read-only") {
		t.Fatalf("expected read validation error, got: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(worktree, "config", "secrets")); !os.IsNotExist(err) {
		t.Fatalf("destination was created despite unreadable source: %v", err)
	}
}

func TestSymlinkEnvFiles_InvalidSourceAndDestinationParentSymlink(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	src := filepath.Join(mainRepo, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(src))
	if err := os.Symlink("missing", src); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(worktree, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(dst))
	if err := os.Symlink("old", dst); err != nil {
		t.Fatal(err)
	}
	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree); err == nil {
		t.Fatal("expected dangling source error")
	}
	got, _ := os.Readlink(dst)
	if got != "old" {
		t.Fatalf("destination changed after invalid source: %q", got)
	}

	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("TOKEN=main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	worktree2 := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(worktree2, "config")); err != nil {
		t.Fatal(err)
	}
	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree2); err == nil {
		t.Fatal("expected nested destination parent symlink rejection")
	}
	if _, err := os.Lstat(filepath.Join(outside, "secrets")); !os.IsNotExist(err) {
		t.Fatalf("outside destination was touched: %v", err)
	}
}

func TestSymlinkEnvFiles_DanglingSourceParentIsNotTreatedAsOptional(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	if err := os.Symlink("missing-directory", filepath.Join(mainRepo, "config")); err != nil {
		t.Fatal(err)
	}
	oldSource := filepath.Join(worktree, "old-source")
	if err := os.WriteFile(oldSource, []byte("old remains usable\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(worktree, "config-destination")
	if err := os.Symlink("old-source", dst); err != nil {
		t.Fatal(err)
	}

	err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree)
	if err == nil {
		t.Fatal("expected dangling canonical parent error")
	}
	got, readErr := os.ReadFile(dst)
	if readErr != nil || string(got) != "old remains usable\n" {
		t.Fatalf("unrelated destination changed or broke: got=%q err=%v", got, readErr)
	}
}

func TestSymlinkEnvFiles_PreflightFailureDoesNotCreateEarlierParents(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	firstSource := filepath.Join(mainRepo, "nested", "first", "secrets")
	mustMkdirAll(t, filepath.Dir(firstSource))
	if err := os.WriteFile(firstSource, []byte("first\n"), 0644); err != nil {
		t.Fatal(err)
	}
	secondSource := filepath.Join(mainRepo, "second", "secrets")
	mustMkdirAll(t, filepath.Dir(secondSource))
	if err := os.Symlink("secrets", secondSource); err != nil {
		t.Fatal(err)
	}

	if err := SymlinkEnvFiles([]string{"nested/first/secrets", "second/secrets"}, mainRepo, worktree); err == nil {
		t.Fatal("expected second source preflight failure")
	}
	if _, err := os.Lstat(filepath.Join(worktree, "nested")); !os.IsNotExist(err) {
		t.Fatalf("preflight created an earlier destination parent: %v", err)
	}
}

func TestSymlinkEnvFiles_RenameFailurePreservesOldLinkAndCleansTemp(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	src := filepath.Join(mainRepo, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(src))
	if err := os.WriteFile(src, []byte("TOKEN=main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(worktree, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(dst))
	oldSource := filepath.Join(worktree, "old-source")
	if err := os.WriteFile(oldSource, []byte("old remains usable\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "old-source"), dst); err != nil {
		t.Fatal(err)
	}
	oldRename := renameFile
	renameFile = func(_, _ string) error { return os.ErrPermission }
	defer func() { renameFile = oldRename }()

	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree); err == nil {
		t.Fatal("expected rename failure")
	}
	got, readErr := os.ReadFile(dst)
	if readErr != nil || string(got) != "old remains usable\n" {
		t.Fatalf("old symlink not preserved and usable: got=%q err=%v", got, readErr)
	}
	assertNoTempEntries(t, filepath.Dir(dst))
}

func TestSymlinkEnvFiles_ApplyStopsAfterAtomicFailure(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	for _, rel := range []string{"config/first", "services/api/config"} {
		source := filepath.Join(mainRepo, rel)
		mustMkdirAll(t, filepath.Dir(source))
		if err := os.WriteFile(source, []byte("canonical "+rel+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(worktree, rel)
		mustMkdirAll(t, filepath.Dir(destination))
		oldSource := destination + ".old"
		if err := os.WriteFile(oldSource, []byte("old "+rel+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Base(oldSource), destination); err != nil {
			t.Fatal(err)
		}
	}

	oldRename := renameFile
	renameCalls := 0
	renameFile = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("synthetic second rename failure")
		}
		return oldRename(oldPath, newPath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	err := SymlinkEnvFiles([]string{"config/first", "services/api/config"}, mainRepo, worktree)
	if err == nil {
		t.Fatal("expected second atomic replacement to fail")
	}
	assertSymlinkResolvesTo(t, filepath.Join(worktree, "config", "first"), filepath.Join(mainRepo, "config", "first"))
	second, readErr := os.ReadFile(filepath.Join(worktree, "services", "api", "config"))
	if readErr != nil || string(second) != "old services/api/config\n" {
		t.Fatalf("failed mapping did not preserve its old destination: got=%q err=%v", second, readErr)
	}
	assertNoTempEntries(t, filepath.Join(worktree, "config"))
	assertNoTempEntries(t, filepath.Join(worktree, "services", "api"))
}

func TestSymlinkEnvFiles_RelativeAndCleanedAbsoluteRoots(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	src := filepath.Join(mainRepo, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(src))
	if err := os.WriteFile(src, []byte("synthetic\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relMain, err := filepath.Rel(cwd, mainRepo)
	if err != nil {
		t.Fatal(err)
	}
	relWorktree, err := filepath.Rel(cwd, worktree)
	if err != nil {
		t.Fatal(err)
	}
	if err := SymlinkEnvFiles([]string{"config/secrets"}, relMain, relWorktree); err != nil {
		t.Fatalf("relative roots failed: %v", err)
	}
	assertSymlinkResolvesTo(t, filepath.Join(worktree, "config", "secrets"), src)

	secondWorktree := t.TempDir()
	cleanedMain := filepath.Join(mainRepo, "missing-component", "..")
	cleanedWorktree := filepath.Join(secondWorktree, ".")
	if err := SymlinkEnvFiles([]string{"config/secrets"}, cleanedMain, cleanedWorktree); err != nil {
		t.Fatalf("cleaned absolute roots failed: %v", err)
	}
	assertSymlinkResolvesTo(t, filepath.Join(secondWorktree, "config", "secrets"), src)
}

func TestSymlinkEnvFiles_AllowsCanonicalParentSymlink(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "secrets"), []byte("external synthetic\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(mainRepo, "config")); err != nil {
		t.Fatal(err)
	}

	if err := SymlinkEnvFiles([]string{"config/secrets"}, mainRepo, worktree); err != nil {
		t.Fatalf("canonical parent symlink should be supported: %v", err)
	}
	assertSymlinkResolvesTo(t, filepath.Join(worktree, "config", "secrets"), filepath.Join(external, "secrets"))
}

func TestSymlinkEnvFiles_PathValidationAndRootAlias(t *testing.T) {
	mainRepo := t.TempDir()
	worktree := t.TempDir()
	src := filepath.Join(mainRepo, "config", "secrets")
	mustMkdirAll(t, filepath.Dir(src))
	if err := os.WriteFile(src, []byte("TOKEN=main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rootAlias := filepath.Join(t.TempDir(), "main-alias")
	if err := os.Symlink(mainRepo, rootAlias); err != nil {
		t.Fatal(err)
	}
	if err := SymlinkEnvFiles([]string{"config/../config/secrets"}, rootAlias, worktree); err != nil {
		t.Fatalf("expected cleaned nested relative path through root alias: %v", err)
	}
	assertSymlinkResolvesTo(t, filepath.Join(worktree, "config", "secrets"), src)

	for _, bad := range []string{"", "/abs/secrets", "../secrets", "config/../../secrets"} {
		if err := SymlinkEnvFiles([]string{bad}, mainRepo, worktree); err == nil {
			t.Fatalf("expected invalid path error for %q", bad)
		}
	}
}

func TestWriteEnvLocals_HardenedBehaviors(t *testing.T) {
	worktree := t.TempDir()
	mapping := EnvLocalMapping{RelPath: "config/secrets.local", Vars: map[string]string{"B": "2", "A": "1"}}
	path := filepath.Join(worktree, "config", "secrets.local")
	if err := WriteEnvLocals([]EnvLocalMapping{mapping}, worktree); err != nil {
		t.Fatalf("create generated file: %v", err)
	}
	content := mustReadFile(t, path)
	if content != generatedEnvLocalMarker+"A=1\nB=2\n" {
		t.Fatalf("unexpected generated content: %q", content)
	}

	mapping.Vars["A"] = "updated"
	if err := WriteEnvLocals([]EnvLocalMapping{mapping}, worktree); err != nil {
		t.Fatalf("update generated file: %v", err)
	}
	if got := mustReadFile(t, path); got != generatedEnvLocalMarker+"A=updated\nB=2\n" {
		t.Fatalf("generated file not replaced: %q", got)
	}

	userPath := filepath.Join(worktree, "config", "user.local")
	if err := os.WriteFile(userPath, []byte("USER=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvLocals([]EnvLocalMapping{{RelPath: "config/user.local", Vars: map[string]string{"A": "secret"}}}, worktree); err == nil {
		t.Fatal("expected user regular rejection")
	}
	if got := mustReadFile(t, userPath); got != "USER=1\n" {
		t.Fatalf("user file changed: %q", got)
	}

	symlinkPath := filepath.Join(worktree, "config", "link.local")
	if err := os.Symlink(path, symlinkPath); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvLocals([]EnvLocalMapping{{RelPath: "config/link.local", Vars: map[string]string{"A": "secret"}}}, worktree); err == nil {
		t.Fatal("expected symlink rejection")
	}
	if got := mustReadFile(t, path); got != generatedEnvLocalMarker+"A=updated\nB=2\n" {
		t.Fatalf("symlink target was followed or changed: %q", got)
	}
}

func TestWriteEnvLocals_RejectsUnusualFilesystemObject(t *testing.T) {
	worktree := t.TempDir()
	path := filepath.Join(worktree, "config", "generated.local")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	err := WriteEnvLocals([]EnvLocalMapping{{
		RelPath: "config/generated.local",
		Vars:    map[string]string{"SAFE": "value"},
	}}, worktree)
	if err == nil {
		t.Fatal("expected directory destination to be rejected")
	}
	info, statErr := os.Stat(path)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("unusual destination changed: info=%v err=%v", info, statErr)
	}
}

func TestWriteEnvLocals_PreflightAndRenameFailure(t *testing.T) {
	worktree := t.TempDir()
	firstPath := filepath.Join(worktree, "config", "secrets.local")
	mustMkdirAll(t, filepath.Dir(firstPath))
	if err := os.WriteFile(firstPath, []byte(generatedEnvLocalMarker+"OLD=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(worktree, "config", "user.local")
	if err := os.WriteFile(userPath, []byte("USER=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mappings := []EnvLocalMapping{
		{RelPath: "config/secrets.local", Vars: map[string]string{"NEW": "2"}},
		{RelPath: "config/user.local", Vars: map[string]string{"SECRET": "redacted"}},
	}
	if err := WriteEnvLocals(mappings, worktree); err == nil {
		t.Fatal("expected second preflight failure")
	}
	if got := mustReadFile(t, firstPath); got != generatedEnvLocalMarker+"OLD=1\n" {
		t.Fatalf("first file changed despite preflight failure: %q", got)
	}

	oldRename := renameFile
	renameFile = func(_, _ string) error { return os.ErrPermission }
	defer func() { renameFile = oldRename }()
	if err := WriteEnvLocals([]EnvLocalMapping{{RelPath: "config/secrets.local", Vars: map[string]string{"NEW": "2"}}}, worktree); err == nil {
		t.Fatal("expected rename failure")
	}
	if got := mustReadFile(t, firstPath); got != generatedEnvLocalMarker+"OLD=1\n" {
		t.Fatalf("old generated file not preserved: %q", got)
	}
	assertNoTempEntries(t, filepath.Dir(firstPath))
}

func TestWriteEnvLocals_ApplyStopsAfterAtomicFailure(t *testing.T) {
	worktree := t.TempDir()
	paths := []string{"config/first.local", "services/api/config.local"}
	for _, rel := range paths {
		path := filepath.Join(worktree, rel)
		mustMkdirAll(t, filepath.Dir(path))
		if err := os.WriteFile(path, []byte(generatedEnvLocalMarker+"OLD=1\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	oldRename := renameFile
	renameCalls := 0
	renameFile = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("synthetic second rename failure")
		}
		return oldRename(oldPath, newPath)
	}
	t.Cleanup(func() { renameFile = oldRename })

	err := WriteEnvLocals([]EnvLocalMapping{
		{RelPath: paths[0], Vars: map[string]string{"FIRST": "updated"}},
		{RelPath: paths[1], Vars: map[string]string{"SECOND": "updated"}},
	}, worktree)
	if err == nil {
		t.Fatal("expected second atomic write to fail")
	}
	if got := mustReadFile(t, filepath.Join(worktree, paths[0])); got != generatedEnvLocalMarker+"FIRST=updated\n" {
		t.Fatalf("first atomic write did not remain applied: %q", got)
	}
	if got := mustReadFile(t, filepath.Join(worktree, paths[1])); got != generatedEnvLocalMarker+"OLD=1\n" {
		t.Fatalf("failed atomic write did not preserve prior contents: %q", got)
	}
	assertNoTempEntries(t, filepath.Join(worktree, "config"))
	assertNoTempEntries(t, filepath.Join(worktree, "services", "api"))
}

func TestWriteEnvLocals_RejectsAssignmentInjectionBeforeMutation(t *testing.T) {
	worktree := t.TempDir()
	secretValue := "synthetic-secret-value\nINJECTED=1"
	mappings := []EnvLocalMapping{{
		RelPath: "new/generated.local",
		Vars:    map[string]string{"VALID_NAME": secretValue},
	}}

	err := WriteEnvLocals(mappings, worktree)
	if err == nil {
		t.Fatal("expected newline-containing value to be rejected")
	}
	if strings.Contains(err.Error(), "synthetic-secret-value") {
		t.Fatalf("error exposed environment value: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(worktree, "new")); !os.IsNotExist(statErr) {
		t.Fatalf("validation mutated destination parents: %v", statErr)
	}
}

func TestWriteEnvLocals_RejectsInvalidVariableName(t *testing.T) {
	worktree := t.TempDir()
	err := WriteEnvLocals([]EnvLocalMapping{{
		RelPath: "config/generated.local",
		Vars:    map[string]string{"BAD=NAME": "synthetic"},
	}}, worktree)
	if err == nil {
		t.Fatal("expected invalid environment variable name to be rejected")
	}
	if _, statErr := os.Lstat(filepath.Join(worktree, "config")); !os.IsNotExist(statErr) {
		t.Fatalf("validation mutated destination parents: %v", statErr)
	}
}

func TestWriteEnvLocals_PreflightFailureDoesNotCreateEarlierParents(t *testing.T) {
	worktree := t.TempDir()
	userPath := filepath.Join(worktree, "existing", "user.local")
	mustMkdirAll(t, filepath.Dir(userPath))
	if err := os.WriteFile(userPath, []byte("USER=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mappings := []EnvLocalMapping{
		{RelPath: "new/nested/generated.local", Vars: map[string]string{"A": "1"}},
		{RelPath: "existing/user.local", Vars: map[string]string{"B": "2"}},
	}

	if err := WriteEnvLocals(mappings, worktree); err == nil {
		t.Fatal("expected second mapping ownership failure")
	}
	if _, err := os.Lstat(filepath.Join(worktree, "new")); !os.IsNotExist(err) {
		t.Fatalf("preflight created an earlier parent: %v", err)
	}
}

func TestWriteEnvLocals_PathValidationAndNestedParentSymlink(t *testing.T) {
	worktree := t.TempDir()
	for _, bad := range []string{"", "/abs.local", "../x.local", "config/../../x.local"} {
		if err := WriteEnvLocals([]EnvLocalMapping{{RelPath: bad, Vars: map[string]string{"A": "1"}}}, worktree); err == nil {
			t.Fatalf("expected invalid path error for %q", bad)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(worktree, "config")); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvLocals([]EnvLocalMapping{{RelPath: "config/secrets.local", Vars: map[string]string{"A": "1"}}}, worktree); err == nil {
		t.Fatal("expected nested parent symlink rejection")
	}
	if _, err := os.Lstat(filepath.Join(outside, "secrets.local")); !os.IsNotExist(err) {
		t.Fatalf("outside path touched: %v", err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertSymlinkResolvesTo(t *testing.T, link, want string) {
	t.Helper()
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("reading symlink: %v", err)
	}
	abs := target
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(link), target)
	}
	got, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("resolving symlink target %q: %v", target, err)
	}
	wantEval, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolving expected target: %v", err)
	}
	if got != wantEval {
		t.Fatalf("symlink resolves to %q, want %q", got, wantEval)
	}
}

func assertNoTempEntries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(name, ".tmp-") || strings.Contains(name, ".grove-link-") || strings.Contains(name, ".grove-write-") {
			t.Fatalf("temp artifact left behind: %s", name)
		}
	}
}

func TestRenderEnvLocal_QuotesUnsafeValuesAndRoundTrips(t *testing.T) {
	vars := map[string]string{
		"SAFE":  "abc_123-./:",
		"SPACE": "hello world",
		"HASH":  "value # not comment",
		"QUOTE": `say "hi"`,
		"SHELL": `$(echo bad) $PATH ` + "`whoami`" + ` \`,
		"META":  `a;b&c|d<e>*?[x]{y}!~`,
		"EMPTY": "",
	}
	content := renderEnvLocal(vars)
	if strings.Contains(content, "SPACE=hello world\n") {
		t.Fatalf("unsafe value left unquoted: %s", content)
	}
	parsed, err := ParseEnvContent(content)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range vars {
		if parsed[k] != want {
			t.Fatalf("%s roundtrip = %q, want %q\ncontent:\n%s", k, parsed[k], want, content)
		}
	}
}
