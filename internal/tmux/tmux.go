// Package tmux handles tmux session/window creation, pane layouts,
// and environment injection for Grove workspaces.
package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"grove/internal/config"
	"grove/internal/worktree"
)

// Runner executes tmux commands. This interface exists for testability.
type Runner interface {
	// Run executes a tmux command and returns its combined output.
	Run(args ...string) (string, error)
}

// realRunner executes tmux commands via os/exec.
type realRunner struct{}

// NewRunner creates a Runner that executes real tmux commands.
func NewRunner() Runner {
	return &realRunner{}
}

func (r *realRunner) Run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("tmux %s: %s: %w", strings.Join(args, " "), output, err)
	}
	return output, nil
}

// presetLayouts lists all valid tmux preset layout names.
var presetLayouts = map[string]bool{
	"even-horizontal": true,
	"even-vertical":   true,
	"main-horizontal": true,
	"main-vertical":   true,
	"tiled":           true,
}

// IsPresetLayout returns true if the layout string is a tmux preset name.
func IsPresetLayout(layout string) bool {
	return presetLayouts[layout]
}

// layoutTier determines which layout tier the config uses.
type layoutTier int

const (
	tierPreset        layoutTier = iota + 1 // Tier 1: preset name
	tierPresetWithSize                      // Tier 2: preset + main_size
	tierExplicitSplits                      // Tier 3: explicit split objects
	tierRawLayout                           // Tier 4: raw tmux layout string
)

// detectTier determines the layout tier from tmux config.
func detectTier(cfg *config.TmuxConfig) layoutTier {
	// Tier 3: any pane has a Split field set
	if hasExplicitSplits(cfg.Panes) {
		return tierExplicitSplits
	}

	// Tier 4: layout is set but is not a known preset
	if cfg.Layout != "" && !IsPresetLayout(cfg.Layout) {
		return tierRawLayout
	}

	// Tier 2: preset with size hint
	if cfg.MainSize != "" {
		return tierPresetWithSize
	}

	// Tier 1: preset name (or default main-vertical)
	return tierPreset
}

// hasExplicitSplits checks if any pane in the slice has a Split field.
func hasExplicitSplits(panes []config.Pane) bool {
	for _, p := range panes {
		if p.Split != "" {
			return true
		}
	}
	return false
}

// SessionName converts a branch name into a tmux-safe session/window name.
// Replaces slashes with dashes, same as worktree sanitization.
func SessionName(branch string) string {
	return worktree.SanitizeBranchName(branch)
}

// Options controls tmux workspace creation behavior.
type Options struct {
	// IncludeAll includes all optional panes.
	IncludeAll bool

	// IncludeWith lists specific optional pane names to include.
	IncludeWith []string

	// Attach controls whether to attach/switch after creation.
	Attach bool

	// WorktreePath is the directory for the worktree (used as -c for panes).
	WorktreePath string

	// Branch is the branch name (used for naming).
	Branch string

	// Env is the resolved environment variables to inject.
	Env map[string]string

	// TmuxConfig is the tmux configuration from .grove.yml.
	TmuxConfig *config.TmuxConfig
}

// Manager orchestrates tmux workspace creation.
type Manager struct {
	runner Runner
}

// NewManager creates a new tmux Manager.
func NewManager(runner Runner) *Manager {
	return &Manager{runner: runner}
}

// Create sets up the full tmux workspace: session/window, env, panes, and attach.
func (m *Manager) Create(opts Options) error {
	name := SessionName(opts.Branch)
	cfg := opts.TmuxConfig

	mode := cfg.Mode
	if mode == "" {
		mode = "window"
	}

	// parentSession tracks the parent session name when in window mode,
	// so we can set environment variables on it without querying tmux twice.
	var parentSession string

	// Step 1: Create session or window
	switch mode {
	case "session":
		if err := m.createSession(name, opts.WorktreePath); err != nil {
			return fmt.Errorf("creating tmux session: %w", err)
		}
	case "window":
		session, err := m.currentSession()
		if err != nil {
			// Fallback to session mode if not inside tmux
			if err := m.createSession(name, opts.WorktreePath); err != nil {
				return fmt.Errorf("creating tmux session (fallback): %w", err)
			}
			mode = "session"
			break
		}
		parentSession = session
		if err := m.createWindow(session, name, opts.WorktreePath); err != nil {
			return fmt.Errorf("creating tmux window: %w", err)
		}
	}

	// Step 2: Inject environment variables BEFORE creating panes.
	// Use the session name for set-environment (windows share session env).
	sessionTarget := name
	if mode == "window" {
		// For window mode, env is set on the parent session.
		sessionTarget = parentSession
	}
	if err := m.injectEnv(sessionTarget, opts.Env); err != nil {
		return fmt.Errorf("injecting environment: %w", err)
	}

	// Step 3: Filter panes (handle optional)
	panes := filterPanes(cfg.Panes, opts.IncludeAll, opts.IncludeWith)

	// Step 4: Create panes with the appropriate layout tier
	target := name // session or window name
	if err := m.createPanes(target, opts.WorktreePath, panes, cfg); err != nil {
		return fmt.Errorf("creating panes: %w", err)
	}

	// Step 5: Attach/switch if requested
	if opts.Attach {
		if err := m.doAttach(name, mode); err != nil {
			return fmt.Errorf("attaching to tmux: %w", err)
		}
	}

	return nil
}

// HasSession checks if a tmux session with the given name exists.
func (m *Manager) HasSession(name string) bool {
	_, err := m.runner.Run("has-session", "-t", name)
	return err == nil
}

// HasWindow checks if a tmux window with the given name exists.
// It uses list-windows to search across all sessions.
func (m *Manager) HasWindow(name string) bool {
	out, err := m.runner.Run("list-windows", "-a", "-F", "#{window_name}", "-f", "#{==:#{window_name},"+name+"}")
	if err != nil {
		return false
	}
	// Some tmux versions return exit 0 with empty output when no windows match.
	return strings.TrimSpace(out) != ""
}

// Destroy kills the tmux session or window for a branch.
func (m *Manager) Destroy(branch string, cfg *config.TmuxConfig) error {
	name := SessionName(branch)
	mode := cfg.Mode
	if mode == "" {
		mode = "window"
	}

	switch mode {
	case "session":
		_, err := m.runner.Run("kill-session", "-t", name)
		if err != nil {
			return fmt.Errorf("killing tmux session %q: %w", name, err)
		}
	case "window":
		_, err := m.runner.Run("kill-window", "-t", name)
		if err != nil {
			return fmt.Errorf("killing tmux window %q: %w", name, err)
		}
	}
	return nil
}

// createSession creates a new tmux session.
func (m *Manager) createSession(name, workdir string) error {
	_, err := m.runner.Run("new-session", "-d", "-s", name, "-c", workdir)
	return err
}

// createWindow creates a new window in the current session.
func (m *Manager) createWindow(session, name, workdir string) error {
	_, err := m.runner.Run("new-window", "-t", session+":"+name, "-c", workdir)
	return err
}

// currentSession returns the name of the current tmux session, or error if not inside tmux.
func (m *Manager) currentSession() (string, error) {
	// Check TMUX env var first
	if os.Getenv("TMUX") == "" {
		return "", fmt.Errorf("not inside tmux")
	}
	out, err := m.runner.Run("display-message", "-p", "#{session_name}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// injectEnv sets environment variables on the tmux session.
func (m *Manager) injectEnv(session string, env map[string]string) error {
	// Sort keys for deterministic ordering
	keys := sortedKeys(env)
	for _, k := range keys {
		_, err := m.runner.Run("set-environment", "-t", session, k, env[k])
		if err != nil {
			return fmt.Errorf("setting env var %s: %w", k, err)
		}
	}
	return nil
}

// createPanes creates panes according to the layout tier.
func (m *Manager) createPanes(target, workdir string, panes []config.Pane, cfg *config.TmuxConfig) error {
	if len(panes) == 0 {
		return nil
	}

	tier := detectTier(cfg)

	switch tier {
	case tierPreset:
		return m.createPanesPreset(target, workdir, panes, effectiveLayout(cfg), "")
	case tierPresetWithSize:
		return m.createPanesPreset(target, workdir, panes, effectiveLayout(cfg), cfg.MainSize)
	case tierExplicitSplits:
		return m.createPanesExplicit(target, workdir, panes)
	case tierRawLayout:
		return m.createPanesRaw(target, workdir, panes, cfg.Layout)
	}
	return nil
}

// effectiveLayout returns the layout name, defaulting to "main-vertical".
func effectiveLayout(cfg *config.TmuxConfig) string {
	if cfg.Layout == "" {
		return "main-vertical"
	}
	return cfg.Layout
}

// createPanesPreset handles Tier 1 and Tier 2 layouts.
func (m *Manager) createPanesPreset(target, workdir string, panes []config.Pane, layout, mainSize string) error {
	// First pane already exists in the session/window. Send command to it.
	if panes[0].Cmd != "" {
		_, err := m.runner.Run("send-keys", "-t", target, panes[0].Cmd, "Enter")
		if err != nil {
			return fmt.Errorf("sending command to first pane: %w", err)
		}
	}

	// Create additional panes via split-window
	for i := 1; i < len(panes); i++ {
		_, err := m.runner.Run("split-window", "-h", "-t", target, "-c", workdir)
		if err != nil {
			return fmt.Errorf("splitting pane %d: %w", i, err)
		}
		if panes[i].Cmd != "" {
			_, err = m.runner.Run("send-keys", "-t", target, panes[i].Cmd, "Enter")
			if err != nil {
				return fmt.Errorf("sending command to pane %d: %w", i, err)
			}
		}
	}

	// Tier 2: set main pane size before applying layout.
	// tmux accepts percentage strings (e.g. "70%") directly for these options.
	if mainSize != "" {
		if layout == "main-vertical" || layout == "main-horizontal" {
			var optionName string
			if layout == "main-vertical" {
				optionName = "main-pane-width"
			} else {
				optionName = "main-pane-height"
			}
			_, err := m.runner.Run("set-option", "-t", target, optionName, mainSize)
			if err != nil {
				return fmt.Errorf("setting %s: %w", optionName, err)
			}
		}
	}

	// Apply the preset layout
	_, err := m.runner.Run("select-layout", "-t", target, layout)
	if err != nil {
		return fmt.Errorf("applying layout %q: %w", layout, err)
	}

	// Select the first pane
	_, _ = m.runner.Run("select-pane", "-t", target+".0")

	return nil
}

// createPanesExplicit handles Tier 3: explicit split objects.
func (m *Manager) createPanesExplicit(target, workdir string, panes []config.Pane) error {
	// Build layout from the pane tree.
	// The first pane is already created with the session/window.
	// We walk the pane tree depth-first, creating splits as needed.
	first := true
	return m.walkPanes(target, workdir, panes, "h", &first, true)
}

// walkPanes recursively creates panes from a split tree.
// splitDir is "h" for horizontal (left-right) or "v" for vertical (top-bottom).
// first tracks whether the very first pane has been used (it already exists in the
// session/window). useExisting means the current pane was just created by a parent
// container's split-window and should be used directly for the first child.
func (m *Manager) walkPanes(target, workdir string, panes []config.Pane, splitDir string, first *bool, useExisting bool) error {
	for i, p := range panes {
		if p.Split != "" {
			// This is a container — recurse into nested panes
			nestedDir := "v"
			if p.Split == "horizontal" {
				nestedDir = "h"
			}

			if *first {
				// First pane in the tree — the container's first child uses the existing pane.
				if err := m.walkPanes(target, workdir, p.Panes, nestedDir, first, true); err != nil {
					return err
				}
			} else if useExisting && i == 0 {
				// Reuse the pane just created by a parent container split.
				if err := m.walkPanes(target, workdir, p.Panes, nestedDir, first, true); err != nil {
					return err
				}
			} else {
				// Create a split for this container, then recurse.
				splitArgs := []string{"split-window", "-" + splitDir, "-t", target, "-c", workdir}
				if p.Size != "" {
					pct, err := parseSizePercent(p.Size)
					if err == nil {
						splitArgs = append(splitArgs, "-p", strconv.Itoa(pct))
					}
				}
				_, err := m.runner.Run(splitArgs...)
				if err != nil {
					return fmt.Errorf("creating split container: %w", err)
				}
				// The first child of this container reuses the pane we just created.
				if err := m.walkPanes(target, workdir, p.Panes, nestedDir, first, true); err != nil {
					return err
				}
			}
			continue
		}

		// Leaf pane
		if *first {
			// First pane already exists in the session/window. Just send command.
			*first = false
			if p.Cmd != "" {
				_, err := m.runner.Run("send-keys", "-t", target, p.Cmd, "Enter")
				if err != nil {
					return fmt.Errorf("sending command to first pane: %w", err)
				}
			}
		} else if useExisting && i == 0 {
			// Reuse the pane just created by a parent container split.
			if p.Cmd != "" {
				_, err := m.runner.Run("send-keys", "-t", target, p.Cmd, "Enter")
				if err != nil {
					return fmt.Errorf("sending command to reused pane: %w", err)
				}
			}
		} else {
			// Create a new pane via split
			splitArgs := []string{"split-window", "-" + splitDir, "-t", target, "-c", workdir}
			if p.Size != "" {
				pct, err := parseSizePercent(p.Size)
				if err == nil {
					splitArgs = append(splitArgs, "-p", strconv.Itoa(pct))
				}
			}
			_, err := m.runner.Run(splitArgs...)
			if err != nil {
				return fmt.Errorf("splitting pane %d: %w", i, err)
			}
			if p.Cmd != "" {
				_, err = m.runner.Run("send-keys", "-t", target, p.Cmd, "Enter")
				if err != nil {
					return fmt.Errorf("sending command to pane %d: %w", i, err)
				}
			}
		}
	}
	return nil
}

// createPanesRaw handles Tier 4: raw tmux layout string.
func (m *Manager) createPanesRaw(target, workdir string, panes []config.Pane, rawLayout string) error {
	// First pane already exists. Send command.
	if len(panes) > 0 && panes[0].Cmd != "" {
		_, err := m.runner.Run("send-keys", "-t", target, panes[0].Cmd, "Enter")
		if err != nil {
			return fmt.Errorf("sending command to first pane: %w", err)
		}
	}

	// Create additional panes
	for i := 1; i < len(panes); i++ {
		_, err := m.runner.Run("split-window", "-h", "-t", target, "-c", workdir)
		if err != nil {
			return fmt.Errorf("splitting pane %d: %w", i, err)
		}
		if panes[i].Cmd != "" {
			_, err = m.runner.Run("send-keys", "-t", target, panes[i].Cmd, "Enter")
			if err != nil {
				return fmt.Errorf("sending command to pane %d: %w", i, err)
			}
		}
	}

	// Apply the raw layout string
	_, err := m.runner.Run("select-layout", "-t", target, rawLayout)
	if err != nil {
		return fmt.Errorf("applying raw layout: %w", err)
	}

	// Select first pane
	_, _ = m.runner.Run("select-pane", "-t", target+".0")

	return nil
}

// Attach attaches to or switches to the tmux session/window.
// This is the public version of the attach method.
func (m *Manager) Attach(name, mode string) error {
	return m.doAttach(name, mode)
}

// doAttach implements the attach/switch logic.
func (m *Manager) doAttach(name, mode string) error {
	if os.Getenv("TMUX") != "" {
		// Already inside tmux — switch client
		_, err := m.runner.Run("switch-client", "-t", name)
		return err
	}

	if mode == "session" {
		_, err := m.runner.Run("attach", "-t", name)
		return err
	}

	// Window mode outside tmux — find the session containing the window,
	// select the window, then attach to the session.
	// list-windows -a -F gives us "session_name:window_name" style info.
	sessionName, err := m.findWindowSession(name)
	if err != nil {
		// Fallback: try attaching with the window name directly via target syntax
		_, attachErr := m.runner.Run("attach", "-t", name)
		return attachErr
	}
	// Select the window first, then attach to the parent session.
	_, _ = m.runner.Run("select-window", "-t", sessionName+":"+name)
	_, err = m.runner.Run("attach", "-t", sessionName)
	return err
}

// findWindowSession returns the session name that contains the given window.
func (m *Manager) findWindowSession(windowName string) (string, error) {
	out, err := m.runner.Run("list-windows", "-a", "-F", "#{session_name}", "-f", "#{==:#{window_name},"+windowName+"}")
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("window %q not found", windowName)
	}
	// Take the first line in case multiple sessions have a window with the same name.
	lines := strings.SplitN(out, "\n", 2)
	return strings.TrimSpace(lines[0]), nil
}

// filterPanes returns only the panes that should be created, based on optional flags.
func filterPanes(panes []config.Pane, includeAll bool, includeWith []string) []config.Pane {
	if includeAll {
		return panes
	}

	// Build a set of names/indices to include
	includeSet := make(map[string]bool, len(includeWith))
	for _, w := range includeWith {
		includeSet[w] = true
	}

	var result []config.Pane
	for i, p := range panes {
		if p.Split != "" {
			// Split containers: recurse to filter nested panes
			filtered := filterPanes(p.Panes, includeAll, includeWith)
			if len(filtered) > 0 {
				cp := p
				cp.Panes = filtered
				result = append(result, cp)
			}
			continue
		}

		if !p.Optional {
			result = append(result, p)
			continue
		}

		// Optional pane: include if name or index matches --with
		if p.Name != "" && includeSet[p.Name] {
			result = append(result, p)
			continue
		}
		if includeSet[strconv.Itoa(i)] {
			result = append(result, p)
		}
	}
	return result
}

// parseSizePercent parses a size string like "70%" into an integer percentage.
func parseSizePercent(s string) (int, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		s = s[:len(s)-1]
	}
	return strconv.Atoi(s)
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
