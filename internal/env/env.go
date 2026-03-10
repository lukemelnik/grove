// Package env handles environment variable resolution for Grove workspaces.
//
// Resolution order (last wins):
//  1. .env files — all vars from listed files pass through automatically
//  2. env block — project-level overrides and templates from .grove.yml
//  3. services ports — computed port vars always win
//  4. -e flags — one-off per-worktree overrides at create time
package env

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"grove/internal/config"
)

// templatePattern matches {{service.port}} references in env values.
var templatePattern = regexp.MustCompile(`\{\{(\w+)\.port\}\}`)

// ParseEnvFile reads a .env file and returns a map of key-value pairs.
// It handles:
//   - Empty lines (skipped)
//   - Comment lines starting with # (skipped)
//   - KEY=VALUE format
//   - Quoted values (single and double quotes are stripped)
//   - Inline comments after unquoted values
//   - Lines with no = sign (skipped)
func ParseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading env file %s: %w", path, err)
	}
	return ParseEnvContent(string(data))
}

// ParseEnvContent parses the content of a .env file (as a string) into key-value pairs.
func ParseEnvContent(content string) (map[string]string, error) {
	result := make(map[string]string)
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Find the first = sign
		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			// No = sign, skip this line
			continue
		}

		key := strings.TrimSpace(line[:eqIdx])
		value := strings.TrimSpace(line[eqIdx+1:])

		if key == "" {
			continue
		}

		// Handle quoted values — preserve content inside quotes verbatim.
		// For unquoted values, strip inline comments (e.g., "value # comment").
		quoted := false
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
				quoted = true
			}
		}
		if !quoted {
			if idx := strings.Index(value, " #"); idx >= 0 {
				value = strings.TrimRight(value[:idx], " ")
			}
		}

		result[key] = value
	}

	return result, nil
}

// ResolveTemplates replaces {{service.port}} references in a string
// using the provided port map (service name -> port number).
// Returns the resolved string and an error if any referenced service is not found.
func ResolveTemplates(value string, ports map[string]int) (string, error) {
	var resolveErr error

	resolved := templatePattern.ReplaceAllStringFunc(value, func(match string) string {
		// Extract the service name from {{service.port}}
		submatch := templatePattern.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		serviceName := submatch[1]
		port, ok := ports[serviceName]
		if !ok {
			resolveErr = fmt.Errorf("unknown service %q in template %q", serviceName, match)
			return match
		}
		return fmt.Sprintf("%d", port)
	})

	return resolved, resolveErr
}

// Resolve computes the final set of environment variables for a worktree.
//
// Parameters:
//   - cfg: the parsed .grove.yml configuration
//   - ports: map of service name to assigned port (from port hashing)
//   - projectRoot: the project root directory (for resolving relative env file paths)
//   - overrides: -e flag overrides (KEY=VALUE from CLI)
//
// Resolution order (last wins):
//  1. .env files from cfg.EnvFiles
//  2. env block from cfg.Env (with template resolution)
//  3. service port vars (e.g., PORT=4045)
//  4. overrides from -e flags
func Resolve(cfg *config.Config, ports map[string]int, projectRoot string, overrides map[string]string) (map[string]string, error) {
	result := make(map[string]string)

	// Step 1: Read all .env files (order matters, later files override earlier ones)
	for _, envFile := range cfg.EnvFiles {
		path := envFile
		if projectRoot != "" && !strings.HasPrefix(path, "/") {
			path = projectRoot + "/" + path
		}
		vars, err := ParseEnvFile(path)
		if err != nil {
			return nil, fmt.Errorf("loading env file: %w", err)
		}
		for k, v := range vars {
			result[k] = v
		}
	}

	// Step 2: Apply env block from config (with template resolution)
	for k, v := range cfg.Env {
		resolved, err := ResolveTemplates(v, ports)
		if err != nil {
			return nil, fmt.Errorf("resolving template in env var %s: %w", k, err)
		}
		result[k] = resolved
	}

	// Step 3: Apply service port vars (always win over env block)
	for name, svc := range cfg.Services {
		port, ok := ports[name]
		if !ok {
			continue
		}
		result[svc.Env] = fmt.Sprintf("%d", port)
	}

	// Step 4: Apply -e flag overrides (highest priority)
	for k, v := range overrides {
		result[k] = v
	}

	return result, nil
}

// ParseOverrides parses a slice of "KEY=VALUE" strings into a map.
// This is used to parse -e flag values from the CLI.
func ParseOverrides(pairs []string) (map[string]string, error) {
	result := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			return nil, fmt.Errorf("invalid override %q: must be KEY=VALUE format", pair)
		}
		key := pair[:eqIdx]
		value := pair[eqIdx+1:]
		if key == "" {
			return nil, fmt.Errorf("invalid override %q: key cannot be empty", pair)
		}
		result[key] = value
	}
	return result, nil
}
