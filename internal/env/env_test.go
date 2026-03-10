package env

import (
	"os"
	"path/filepath"
	"testing"

	"grove/internal/config"
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
		result, err := ResolveTemplates(tt.input, ports)
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

	_, err := ResolveTemplates("{{unknown.port}}", ports)
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func TestResolveTemplates_EmptyPorts(t *testing.T) {
	result, err := ResolveTemplates("no templates", map[string]int{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "no templates" {
		t.Errorf("expected unchanged string, got %q", result)
	}
}

func TestResolve_FullPipeline(t *testing.T) {
	// Set up env files
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	envContent := []byte("EXISTING_VAR=from_env_file\nPORT=9999\n")
	if err := os.WriteFile(envPath, envContent, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		EnvFiles: []string{".env"},
		Services: map[string]config.Service{
			"api": {Port: 4000, Env: "PORT"},
			"web": {Port: 3000, Env: "WEB_PORT"},
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

	overrides := map[string]string{
		"OVERRIDE_VAR": "from_cli",
	}

	result, err := Resolve(cfg, ports, dir, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Step 1: .env file vars should be present
	if result["EXISTING_VAR"] != "from_env_file" {
		t.Errorf("expected EXISTING_VAR from env file, got %q", result["EXISTING_VAR"])
	}

	// Step 2: env block vars with templates resolved
	if result["VITE_API_URL"] != "http://localhost:4045" {
		t.Errorf("expected VITE_API_URL with resolved port, got %q", result["VITE_API_URL"])
	}
	if result["CORS_ORIGIN"] != "http://localhost:3045" {
		t.Errorf("expected CORS_ORIGIN with resolved port, got %q", result["CORS_ORIGIN"])
	}
	if result["CUSTOM"] != "static_value" {
		t.Errorf("expected CUSTOM=static_value, got %q", result["CUSTOM"])
	}

	// Step 3: service port vars override .env file PORT=9999
	if result["PORT"] != "4045" {
		t.Errorf("expected PORT=4045 (from service), got %q", result["PORT"])
	}
	if result["WEB_PORT"] != "3045" {
		t.Errorf("expected WEB_PORT=3045, got %q", result["WEB_PORT"])
	}

	// Step 4: -e overrides present
	if result["OVERRIDE_VAR"] != "from_cli" {
		t.Errorf("expected OVERRIDE_VAR=from_cli, got %q", result["OVERRIDE_VAR"])
	}
}

func TestResolve_OverridePrecedence(t *testing.T) {
	// Test that resolution order is enforced: env_files < env block < services < overrides
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	// .env file sets PORT=1111 and CUSTOM=from_file
	envContent := []byte("PORT=1111\nCUSTOM=from_file\n")
	if err := os.WriteFile(envPath, envContent, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		EnvFiles: []string{".env"},
		Services: map[string]config.Service{
			"api": {Port: 4000, Env: "PORT"},
		},
		Env: map[string]string{
			"CUSTOM": "from_env_block", // overrides .env file value
		},
	}

	ports := map[string]int{"api": 4045}

	overrides := map[string]string{
		"PORT": "9999", // overrides even the service port
	}

	result, err := Resolve(cfg, ports, dir, overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// CUSTOM should come from env block (step 2 overrides step 1)
	if result["CUSTOM"] != "from_env_block" {
		t.Errorf("expected CUSTOM=from_env_block, got %q", result["CUSTOM"])
	}

	// PORT: service sets it to 4045 (step 3), but -e override sets 9999 (step 4)
	if result["PORT"] != "9999" {
		t.Errorf("expected PORT=9999 (from override), got %q", result["PORT"])
	}
}

func TestResolve_NoEnvFiles(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.Service{
			"api": {Port: 4000, Env: "PORT"},
		},
	}

	ports := map[string]int{"api": 4050}

	result, err := Resolve(cfg, ports, "", nil)
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

	_, err := Resolve(cfg, map[string]int{}, "/tmp", nil)
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

	_, err := Resolve(cfg, map[string]int{}, "", nil)
	if err == nil {
		t.Fatal("expected error for unknown service in template")
	}
}

func TestResolve_MultipleEnvFiles(t *testing.T) {
	dir := t.TempDir()

	// First env file
	env1 := filepath.Join(dir, ".env")
	if err := os.WriteFile(env1, []byte("KEY1=from_first\nSHARED=first\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Second env file (later in list, should override SHARED)
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

	result, err := Resolve(cfg, map[string]int{}, dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["KEY1"] != "from_first" {
		t.Errorf("expected KEY1=from_first, got %q", result["KEY1"])
	}
	if result["KEY2"] != "from_second" {
		t.Errorf("expected KEY2=from_second, got %q", result["KEY2"])
	}
	// SHARED should come from second file (last wins)
	if result["SHARED"] != "second" {
		t.Errorf("expected SHARED=second (last env file wins), got %q", result["SHARED"])
	}
}

func TestParseOverrides_Valid(t *testing.T) {
	pairs := []string{"KEY1=value1", "KEY2=value2", "KEY3=val=ue3"}
	result, err := ParseOverrides(pairs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["KEY1"] != "value1" {
		t.Errorf("expected KEY1=value1, got %q", result["KEY1"])
	}
	if result["KEY2"] != "value2" {
		t.Errorf("expected KEY2=value2, got %q", result["KEY2"])
	}
	if result["KEY3"] != "val=ue3" {
		t.Errorf("expected KEY3=val=ue3, got %q", result["KEY3"])
	}
}

func TestParseOverrides_Empty(t *testing.T) {
	result, err := ParseOverrides(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestParseOverrides_NoEquals(t *testing.T) {
	_, err := ParseOverrides([]string{"INVALID"})
	if err == nil {
		t.Fatal("expected error for missing =")
	}
}

func TestParseOverrides_EmptyKey(t *testing.T) {
	_, err := ParseOverrides([]string{"=value"})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestParseOverrides_EmptyValue(t *testing.T) {
	result, err := ParseOverrides([]string{"KEY="})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["KEY"] != "" {
		t.Errorf("expected empty value, got %q", result["KEY"])
	}
}

func TestResolve_NilOverrides(t *testing.T) {
	cfg := &config.Config{
		Services: map[string]config.Service{
			"api": {Port: 4000, Env: "PORT"},
		},
	}

	ports := map[string]int{"api": 4050}

	result, err := Resolve(cfg, ports, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["PORT"] != "4050" {
		t.Errorf("expected PORT=4050, got %q", result["PORT"])
	}
}

func TestResolve_EmptyConfig(t *testing.T) {
	cfg := &config.Config{}
	result, err := Resolve(cfg, map[string]int{}, "", nil)
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
