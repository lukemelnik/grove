package env

import (
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
		result, err := ResolveTemplates(tt.input, ports, "", nil)
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

	_, err := ResolveTemplates("{{unknown.port}}", ports, "", nil)
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func TestResolveTemplates_EmptyPorts(t *testing.T) {
	result, err := ResolveTemplates("no templates", map[string]int{}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "no templates" {
		t.Errorf("expected unchanged string, got %q", result)
	}
}

func TestResolveTemplates_Branch(t *testing.T) {
	result, err := ResolveTemplates("{{branch}}", map[string]int{}, "feat/auth", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "feat/auth" {
		t.Errorf("expected branch template to resolve, got %q", result)
	}
}

func TestResolveTemplates_ProxyURL(t *testing.T) {
	ports := map[string]int{"api": 4045}
	pi := &ProxyInfo{
		ProjectName:   "myapp",
		Port:          1355,
		HTTPS:         true,
		DefaultBranch: "main",
	}

	result, err := ResolveTemplates("{{api.url}}", ports, "feat/auth", pi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "https://api.feat-auth.myapp.localhost:1355" {
		t.Errorf("got %q, want %q", result, "https://api.feat-auth.myapp.localhost:1355")
	}
}

func TestResolveTemplates_ProxyURL_DefaultBranch(t *testing.T) {
	ports := map[string]int{"api": 4000}
	pi := &ProxyInfo{
		ProjectName:   "myapp",
		Port:          1355,
		HTTPS:         true,
		DefaultBranch: "main",
	}

	result, err := ResolveTemplates("{{api.url}}", ports, "main", pi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "https://api.myapp.localhost:1355" {
		t.Errorf("got %q, want %q", result, "https://api.myapp.localhost:1355")
	}
}

func TestResolveTemplates_ProxyURL_DefaultPort(t *testing.T) {
	ports := map[string]int{"api": 4000}
	pi := &ProxyInfo{
		ProjectName:   "myapp",
		Port:          443,
		HTTPS:         true,
		DefaultBranch: "main",
	}

	result, err := ResolveTemplates("{{api.url}}", ports, "main", pi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "https://api.myapp.localhost" {
		t.Errorf("got %q, want %q", result, "https://api.myapp.localhost")
	}
}

func TestResolveTemplates_ProxyURL_HTTP(t *testing.T) {
	ports := map[string]int{"api": 4000}
	pi := &ProxyInfo{
		ProjectName:   "myapp",
		Port:          80,
		HTTPS:         false,
		DefaultBranch: "main",
	}

	result, err := ResolveTemplates("{{api.url}}", ports, "main", pi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "http://api.myapp.localhost" {
		t.Errorf("got %q, want %q", result, "http://api.myapp.localhost")
	}
}

func TestResolveTemplates_ProxyHost(t *testing.T) {
	ports := map[string]int{"api": 4045}
	pi := &ProxyInfo{
		ProjectName:   "myapp",
		Port:          1355,
		HTTPS:         true,
		DefaultBranch: "main",
	}

	result, err := ResolveTemplates("{{api.host}}", ports, "feat/auth", pi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "api.feat-auth.myapp.localhost" {
		t.Errorf("got %q, want %q", result, "api.feat-auth.myapp.localhost")
	}
}

func TestResolveTemplates_ProxyURL_NoProxyInfo(t *testing.T) {
	ports := map[string]int{"api": 4045}

	_, err := ResolveTemplates("{{api.url}}", ports, "main", nil)
	if err == nil {
		t.Fatal("expected error when using proxy template without proxy info")
	}
	if !strings.Contains(err.Error(), "requires proxy configuration") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolveTemplates_ProxyHost_NoProxyInfo(t *testing.T) {
	ports := map[string]int{"api": 4045}

	_, err := ResolveTemplates("{{api.host}}", ports, "main", nil)
	if err == nil {
		t.Fatal("expected error when using host template without proxy info")
	}
	if !strings.Contains(err.Error(), "requires proxy configuration") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProxyInfoFromConfig_Nil(t *testing.T) {
	pi := ProxyInfoFromConfig(nil, "/path/to/project", "main")
	if pi != nil {
		t.Error("expected nil for nil proxy config")
	}
}

func TestProxyInfoFromConfig_Defaults(t *testing.T) {
	cfg := &config.ProxyConfig{HTTPS: true}
	pi := ProxyInfoFromConfig(cfg, "/path/to/myproject", "main")
	if pi == nil {
		t.Fatal("expected non-nil ProxyInfo")
	}
	if pi.ProjectName != "myproject" {
		t.Errorf("ProjectName = %q, want %q", pi.ProjectName, "myproject")
	}
	if pi.Port != 1355 {
		t.Errorf("Port = %d, want 1355", pi.Port)
	}
	if !pi.HTTPS {
		t.Error("expected HTTPS=true")
	}
	if pi.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", pi.DefaultBranch, "main")
	}
}

func TestProxyInfoFromConfig_Overrides(t *testing.T) {
	cfg := &config.ProxyConfig{Name: "custom", Port: 443, HTTPS: false}
	pi := ProxyInfoFromConfig(cfg, "/path/to/myproject", "master")
	if pi.ProjectName != "custom" {
		t.Errorf("ProjectName = %q, want %q", pi.ProjectName, "custom")
	}
	if pi.Port != 443 {
		t.Errorf("Port = %d, want 443", pi.Port)
	}
	if pi.HTTPS {
		t.Error("expected HTTPS=false")
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

	result, err := Resolve(cfg, ports, "", dir, nil)
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

	result, err := Resolve(cfg, ports, "", dir, nil)
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

	result, err := Resolve(cfg, ports, "", "", nil)
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

	_, err := Resolve(cfg, map[string]int{}, "", "/tmp", nil)
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

	_, err := Resolve(cfg, map[string]int{}, "", "", nil)
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

	result, err := Resolve(cfg, map[string]int{}, "", dir, nil)
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
	result, err := Resolve(cfg, map[string]int{}, "", "", nil)
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
	managed, err := BuildManagedEnv(cfg, ports, "feat/auth", nil)
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

	managed, err := BuildManagedEnv(cfg, ports, "feat/auth", nil)
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

	managed, err := BuildManagedEnv(cfg, map[string]int{"api": 4045}, "feat/auth", nil)
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
