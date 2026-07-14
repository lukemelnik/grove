// Package hooks runs lifecycle scripts defined in .grove.yml.
package hooks

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultTimeout = 2 * time.Minute

// OutputMode controls hook child process output handling.
type OutputMode int

const (
	OutputSummary OutputMode = iota
	OutputStream
	OutputQuiet
)

// RunOpts contains the parameters for hook execution.
type RunOpts struct {
	Branch         string
	WorktreePath   string
	ProjectRoot    string
	Ports          map[string]int // service name -> assigned port
	Stdout         io.Writer      // used only when OutputMode is OutputStream
	Stderr         io.Writer      // used only when OutputMode is OutputStream
	EnvPassthrough []string
	OutputMode     OutputMode
	Context        context.Context
	Timeout        time.Duration
}

func RunPostCreate(scripts []string, opts RunOpts) []string {
	var warnings []string
	env := buildEnv(opts)
	for _, script := range scripts {
		if err := runScript("post_create", script, opts, env, true); err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	return warnings
}

func RunPreDelete(scripts []string, opts RunOpts) error {
	env := buildEnv(opts)
	for _, script := range scripts {
		if err := runScript("pre_delete", script, opts, env, false); err != nil {
			return err
		}
	}
	return nil
}

func runScript(lifecycle, script string, opts RunOpts, env []string, skipMissing bool) error {
	absScript, err := resolveScript(opts.ProjectRoot, script)
	if err != nil {
		if os.IsNotExist(err) && skipMissing {
			return fmt.Errorf("hooks.%s: script %q not found, skipping", lifecycle, script)
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("hooks.%s: script %q not found", lifecycle, script)
		}
		return fmt.Errorf("hooks.%s: invalid script %q: %w", lifecycle, script, err)
	}

	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, absScript)
	configureProcessTree(cmd)
	cmd.Cancel = func() error { return killProcessTree(cmd) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = opts.WorktreePath
	cmd.Env = env
	if opts.OutputMode == OutputStream {
		cmd.Stdout = opts.Stdout
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("hooks.%s: %q canceled or timed out: %w", lifecycle, script, ctx.Err())
		}
		return fmt.Errorf("hooks.%s: %q failed: %w", lifecycle, script, err)
	}
	return nil
}

func resolveScript(projectRoot, script string) (string, error) {
	rootAbs, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(rootAbs))
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(script) {
		return "", fmt.Errorf("must be a relative path")
	}
	clean := filepath.Clean(script)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("escapes project root")
	}
	joined := filepath.Join(root, clean)
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("resolves outside project root")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("must resolve to a regular file")
	}
	return resolved, nil
}

func buildEnv(opts RunOpts) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true, "TMPDIR": true, "TEMP": true, "TMP": true, "LANG": true, "TERM": true}
	for _, k := range opts.EnvPassthrough {
		allowed[k] = true
	}
	vals := make(map[string]string)
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if allowed[k] || strings.HasPrefix(k, "LC_") {
			vals[k] = v
		}
	}
	vals["GROVE_BRANCH"] = opts.Branch
	vals["GROVE_WORKTREE"] = opts.WorktreePath
	vals["GROVE_PROJECT_ROOT"] = opts.ProjectRoot
	names := make([]string, 0, len(opts.Ports))
	for name := range opts.Ports {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		vals["GROVE_PORT_"+strings.ToUpper(name)] = fmt.Sprintf("%d", opts.Ports[name])
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+vals[k])
	}
	return env
}
