package tmux

import (
	"fmt"
	"strings"
	"testing"

	"grove/internal/config"
)

// mockRunner records all tmux commands for verification.
type mockRunner struct {
	commands [][]string
	// outputs maps command key to output (optional)
	outputs map[string]string
	// errors maps command key to error (optional)
	errors map[string]error
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		outputs: make(map[string]string),
		errors:  make(map[string]error),
	}
}

func (m *mockRunner) Run(args ...string) (string, error) {
	m.commands = append(m.commands, args)

	key := strings.Join(args, " ")
	if err, ok := m.errors[key]; ok {
		return "", err
	}
	if out, ok := m.outputs[key]; ok {
		return out, nil
	}

	// Special handling: display-message for session name queries
	if len(args) >= 3 && args[0] == "display-message" && args[1] == "-p" {
		return "my-session", nil
	}

	return "", nil
}

// findCommand returns the index of the first command matching the given prefix args,
// or -1 if not found.
func (m *mockRunner) findCommand(prefix ...string) int {
	for i, cmd := range m.commands {
		if len(cmd) >= len(prefix) {
			match := true
			for j, p := range prefix {
				if cmd[j] != p {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
	}
	return -1
}

// findAllCommands returns all commands matching the given prefix.
func (m *mockRunner) findAllCommands(prefix ...string) [][]string {
	var result [][]string
	for _, cmd := range m.commands {
		if len(cmd) >= len(prefix) {
			match := true
			for j, p := range prefix {
				if cmd[j] != p {
					match = false
					break
				}
			}
			if match {
				result = append(result, cmd)
			}
		}
	}
	return result
}

// commandString returns a human-readable representation of all commands.
func (m *mockRunner) commandString() string {
	var lines []string
	for i, cmd := range m.commands {
		lines = append(lines, fmt.Sprintf("  [%d] tmux %s", i, strings.Join(cmd, " ")))
	}
	return strings.Join(lines, "\n")
}

func TestSessionName(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"main", "main"},
		{"feat/auth", "feat-auth"},
		{"fix/login-bug", "fix-login-bug"},
		{"feat/nested/deep/branch", "feat-nested-deep-branch"},
	}
	for _, tt := range tests {
		got := SessionName(tt.branch)
		if got != tt.want {
			t.Errorf("SessionName(%q) = %q, want %q", tt.branch, got, tt.want)
		}
	}
}

func TestIsPresetLayout(t *testing.T) {
	presets := []string{"even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled"}
	for _, p := range presets {
		if !IsPresetLayout(p) {
			t.Errorf("IsPresetLayout(%q) should be true", p)
		}
	}

	nonPresets := []string{"custom", "a]180x50,0,0{120x50,0,0", "", "vertical"}
	for _, p := range nonPresets {
		if IsPresetLayout(p) {
			t.Errorf("IsPresetLayout(%q) should be false", p)
		}
	}
}

func TestDetectTier(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.TmuxConfig
		want layoutTier
	}{
		{
			name: "tier 1: preset name",
			cfg: config.TmuxConfig{
				Layout: "main-vertical",
				Panes:  []config.Pane{{Cmd: "nvim"}},
			},
			want: tierPreset,
		},
		{
			name: "tier 1: empty layout defaults to main-vertical",
			cfg: config.TmuxConfig{
				Panes: []config.Pane{{Cmd: "nvim"}},
			},
			want: tierPreset,
		},
		{
			name: "tier 2: preset with size",
			cfg: config.TmuxConfig{
				Layout:   "main-vertical",
				MainSize: "70%",
				Panes:    []config.Pane{{Cmd: "nvim"}},
			},
			want: tierPresetWithSize,
		},
		{
			name: "tier 3: explicit splits",
			cfg: config.TmuxConfig{
				Panes: []config.Pane{
					{Cmd: "nvim", Size: "70%"},
					{Split: "vertical", Panes: []config.Pane{
						{Cmd: "claude"},
						{Cmd: "pnpm dev"},
					}},
				},
			},
			want: tierExplicitSplits,
		},
		{
			name: "tier 4: raw layout string",
			cfg: config.TmuxConfig{
				Layout: "a]180x50,0,0{120x50,0,0,0,59x50,121,0[59x25,121,0,1,59x24,121,26,2]}",
				Panes:  []config.Pane{{Cmd: "nvim"}},
			},
			want: tierRawLayout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectTier(&tt.cfg)
			if got != tt.want {
				t.Errorf("detectTier() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFilterPanes(t *testing.T) {
	panes := []config.Pane{
		{Cmd: "nvim"},
		{Cmd: "claude"},
		{Cmd: "pnpm dev", Optional: true, Name: "dev"},
		{Cmd: "lazygit", Optional: true, Name: "git"},
	}

	t.Run("default: skips optional", func(t *testing.T) {
		result := filterPanes(panes, false, nil)
		if len(result) != 2 {
			t.Fatalf("expected 2 panes, got %d", len(result))
		}
		if result[0].Cmd != "nvim" || result[1].Cmd != "claude" {
			t.Errorf("expected nvim and claude, got %v", result)
		}
	})

	t.Run("--all: includes everything", func(t *testing.T) {
		result := filterPanes(panes, true, nil)
		if len(result) != 4 {
			t.Fatalf("expected 4 panes, got %d", len(result))
		}
	})

	t.Run("--with name: includes specific optional", func(t *testing.T) {
		result := filterPanes(panes, false, []string{"dev"})
		if len(result) != 3 {
			t.Fatalf("expected 3 panes, got %d", len(result))
		}
		found := false
		for _, p := range result {
			if p.Cmd == "pnpm dev" {
				found = true
			}
		}
		if !found {
			t.Error("expected 'pnpm dev' pane to be included")
		}
	})

	t.Run("--with index: includes by index", func(t *testing.T) {
		result := filterPanes(panes, false, []string{"3"})
		if len(result) != 3 {
			t.Fatalf("expected 3 panes, got %d", len(result))
		}
		found := false
		for _, p := range result {
			if p.Cmd == "lazygit" {
				found = true
			}
		}
		if !found {
			t.Error("expected 'lazygit' pane to be included")
		}
	})

	t.Run("--with multiple: includes multiple optional", func(t *testing.T) {
		result := filterPanes(panes, false, []string{"dev", "git"})
		if len(result) != 4 {
			t.Fatalf("expected 4 panes, got %d", len(result))
		}
	})

	t.Run("nested split with optional", func(t *testing.T) {
		nested := []config.Pane{
			{Cmd: "nvim"},
			{Split: "vertical", Panes: []config.Pane{
				{Cmd: "claude"},
				{Cmd: "pnpm dev", Optional: true, Name: "dev"},
			}},
		}
		result := filterPanes(nested, false, nil)
		if len(result) != 2 {
			t.Fatalf("expected 2 panes, got %d", len(result))
		}
		// The split container should still be there with only claude
		if result[1].Split != "vertical" {
			t.Error("expected split container")
		}
		if len(result[1].Panes) != 1 {
			t.Fatalf("expected 1 nested pane, got %d", len(result[1].Panes))
		}
		if result[1].Panes[0].Cmd != "claude" {
			t.Errorf("expected claude, got %s", result[1].Panes[0].Cmd)
		}
	})
}

func TestCreate_SessionMode(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "feat/auth",
		WorktreePath: "/path/to/worktree",
		Env: map[string]string{
			"PORT":     "4045",
			"WEB_PORT": "3045",
		},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
				{Cmd: "pnpm dev"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify session creation
	idx := runner.findCommand("new-session", "-d", "-s", "feat-auth", "-c", "/path/to/worktree")
	if idx < 0 {
		t.Errorf("expected new-session command, got:\n%s", runner.commandString())
	}

	// Verify env injection happens after session creation
	envCmds := runner.findAllCommands("set-environment")
	if len(envCmds) != 2 {
		t.Errorf("expected 2 set-environment commands, got %d:\n%s", len(envCmds), runner.commandString())
	}
	// Env should be on session "feat-auth"
	for _, cmd := range envCmds {
		if cmd[2] != "feat-auth" {
			t.Errorf("expected set-environment on session feat-auth, got target %s", cmd[2])
		}
	}

	// Verify env injection is before pane creation
	firstEnvIdx := runner.findCommand("set-environment")
	firstSplitIdx := runner.findCommand("split-window")
	if firstEnvIdx > firstSplitIdx && firstSplitIdx >= 0 {
		t.Error("env injection should happen before pane creation")
	}

	// Verify pane commands
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 3 {
		t.Errorf("expected 3 send-keys commands, got %d:\n%s", len(sendKeys), runner.commandString())
	}

	// Verify layout is applied
	layoutIdx := runner.findCommand("select-layout", "-t", "feat-auth", "main-vertical")
	if layoutIdx < 0 {
		t.Errorf("expected select-layout command, got:\n%s", runner.commandString())
	}

	// Verify split-window commands (2 additional panes)
	splits := runner.findAllCommands("split-window")
	if len(splits) != 2 {
		t.Errorf("expected 2 split-window commands, got %d:\n%s", len(splits), runner.commandString())
	}
}

func TestCreate_WindowMode(t *testing.T) {
	// Simulate being inside tmux
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "feat/auth",
		WorktreePath: "/path/to/worktree",
		Env: map[string]string{
			"PORT": "4045",
		},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "window",
			Layout: "even-horizontal",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify window creation (not session)
	idx := runner.findCommand("new-window")
	if idx < 0 {
		t.Errorf("expected new-window command, got:\n%s", runner.commandString())
	}
	sessionIdx := runner.findCommand("new-session")
	if sessionIdx >= 0 {
		t.Errorf("should not create a session in window mode, got:\n%s", runner.commandString())
	}

	// Verify env is set on the parent session (my-session from mock)
	envCmds := runner.findAllCommands("set-environment")
	if len(envCmds) != 1 {
		t.Fatalf("expected 1 set-environment command, got %d:\n%s", len(envCmds), runner.commandString())
	}
	if envCmds[0][2] != "my-session" {
		t.Errorf("expected env on parent session 'my-session', got %s", envCmds[0][2])
	}
}

func TestCreate_WindowModeFallsBackToSession(t *testing.T) {
	// Not inside tmux
	t.Setenv("TMUX", "")

	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "feat/auth",
		WorktreePath: "/path/to/worktree",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode: "window",
			Panes: []config.Pane{
				{Cmd: "nvim"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should fall back to creating a session
	idx := runner.findCommand("new-session")
	if idx < 0 {
		t.Errorf("expected new-session fallback, got:\n%s", runner.commandString())
	}
}

func TestCreate_Tier1_Preset(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "tiled",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
				{Cmd: "pnpm dev"},
				{Cmd: "lazygit"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 3 split-window commands for 4 panes
	splits := runner.findAllCommands("split-window")
	if len(splits) != 3 {
		t.Errorf("expected 3 splits for 4 panes, got %d:\n%s", len(splits), runner.commandString())
	}

	// All 4 send-keys
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 4 {
		t.Errorf("expected 4 send-keys, got %d:\n%s", len(sendKeys), runner.commandString())
	}

	// Layout should be "tiled"
	layoutIdx := runner.findCommand("select-layout", "-t", "test-branch", "tiled")
	if layoutIdx < 0 {
		t.Errorf("expected select-layout tiled:\n%s", runner.commandString())
	}

	// No set-option for main-pane-width/height (Tier 1, no size)
	setOpts := runner.findAllCommands("set-option")
	if len(setOpts) != 0 {
		t.Errorf("Tier 1 should not set-option, got:\n%s", runner.commandString())
	}
}

func TestCreate_Tier2_PresetWithSize(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:     "session",
			Layout:   "main-vertical",
			MainSize: "70%",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
				{Cmd: "pnpm dev"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify set-option for main-pane-width
	setOptIdx := runner.findCommand("set-option", "-t", "test-branch", "main-pane-width", "70%")
	if setOptIdx < 0 {
		t.Errorf("expected set-option main-pane-width 70%%:\n%s", runner.commandString())
	}

	// set-option should be BEFORE select-layout
	layoutIdx := runner.findCommand("select-layout")
	if setOptIdx >= layoutIdx {
		t.Error("set-option should come before select-layout")
	}
}

func TestCreate_Tier2_MainHorizontal(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:     "session",
			Layout:   "main-horizontal",
			MainSize: "60%",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should use main-pane-height for horizontal layout
	setOptIdx := runner.findCommand("set-option", "-t", "test-branch", "main-pane-height", "60%")
	if setOptIdx < 0 {
		t.Errorf("expected set-option main-pane-height 60%%:\n%s", runner.commandString())
	}
}

func TestCreate_Tier3_ExplicitSplits(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode: "session",
			Panes: []config.Pane{
				{Cmd: "nvim", Size: "70%"},
				{Split: "vertical", Panes: []config.Pane{
					{Cmd: "claude", Size: "60%"},
					{Cmd: "pnpm dev"},
				}},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify split-window commands with correct directions and sizes
	splits := runner.findAllCommands("split-window")
	if len(splits) != 2 {
		t.Fatalf("expected 2 split-window commands, got %d:\n%s", len(splits), runner.commandString())
	}

	// First split: horizontal (for the container), then vertical for nested panes
	// The container split should create a horizontal split
	// Then the second pane inside the container is a vertical split

	// All 3 commands should be sent
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 3 {
		t.Errorf("expected 3 send-keys, got %d:\n%s", len(sendKeys), runner.commandString())
	}

	// No select-layout for Tier 3
	layoutCmds := runner.findAllCommands("select-layout")
	if len(layoutCmds) != 0 {
		t.Errorf("Tier 3 should not use select-layout, got:\n%s", runner.commandString())
	}
}

func TestCreate_Tier4_RawLayout(t *testing.T) {
	rawLayout := "a]180x50,0,0{120x50,0,0,0,59x50,121,0[59x25,121,0,1,59x24,121,26,2]}"
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: rawLayout,
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
				{Cmd: "pnpm dev"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify raw layout is applied
	layoutIdx := runner.findCommand("select-layout", "-t", "test-branch", rawLayout)
	if layoutIdx < 0 {
		t.Errorf("expected select-layout with raw layout string:\n%s", runner.commandString())
	}

	// 2 split-window for 3 panes
	splits := runner.findAllCommands("split-window")
	if len(splits) != 2 {
		t.Errorf("expected 2 splits for 3 panes, got %d:\n%s", len(splits), runner.commandString())
	}

	// 3 send-keys
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 3 {
		t.Errorf("expected 3 send-keys, got %d:\n%s", len(sendKeys), runner.commandString())
	}
}

func TestCreate_OptionalPanes_Default(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
				{Cmd: "pnpm dev", Optional: true, Name: "dev"},
			},
		},
		IncludeAll: false,
		Attach:     false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Only 1 split-window (nvim + claude = 2 panes, 1 split)
	splits := runner.findAllCommands("split-window")
	if len(splits) != 1 {
		t.Errorf("expected 1 split (optional skipped), got %d:\n%s", len(splits), runner.commandString())
	}

	// Only 2 send-keys
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 2 {
		t.Errorf("expected 2 send-keys, got %d:\n%s", len(sendKeys), runner.commandString())
	}
}

func TestCreate_OptionalPanes_All(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
				{Cmd: "pnpm dev", Optional: true, Name: "dev"},
			},
		},
		IncludeAll: true,
		Attach:     false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 2 splits for 3 panes
	splits := runner.findAllCommands("split-window")
	if len(splits) != 2 {
		t.Errorf("expected 2 splits (all included), got %d:\n%s", len(splits), runner.commandString())
	}
}

func TestCreate_OptionalPanes_WithName(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
				{Cmd: "pnpm dev", Optional: true, Name: "dev"},
				{Cmd: "lazygit", Optional: true, Name: "git"},
			},
		},
		IncludeWith: []string{"dev"},
		Attach:      false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 2 splits for 3 panes (nvim, claude, pnpm dev)
	splits := runner.findAllCommands("split-window")
	if len(splits) != 2 {
		t.Errorf("expected 2 splits (dev included, git skipped), got %d:\n%s", len(splits), runner.commandString())
	}

	// 3 send-keys: nvim, claude, pnpm dev
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 3 {
		t.Errorf("expected 3 send-keys, got %d:\n%s", len(sendKeys), runner.commandString())
	}
}

func TestCreate_EnvInjectionOrder(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	env := map[string]string{
		"PORT":         "4045",
		"WEB_PORT":     "3045",
		"VITE_API_URL": "http://localhost:4045",
	}

	opts := Options{
		Branch:       "feat/auth",
		WorktreePath: "/work",
		Env:          env,
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "pnpm dev"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// All env vars should be set
	envCmds := runner.findAllCommands("set-environment")
	if len(envCmds) != 3 {
		t.Fatalf("expected 3 set-environment commands, got %d:\n%s", len(envCmds), runner.commandString())
	}

	// Env injection should be alphabetically sorted (deterministic)
	if envCmds[0][3] != "PORT" {
		t.Errorf("expected first env var to be PORT, got %s", envCmds[0][3])
	}
	if envCmds[1][3] != "VITE_API_URL" {
		t.Errorf("expected second env var to be VITE_API_URL, got %s", envCmds[1][3])
	}
	if envCmds[2][3] != "WEB_PORT" {
		t.Errorf("expected third env var to be WEB_PORT, got %s", envCmds[2][3])
	}

	// Env should come BEFORE any split-window or send-keys
	lastEnvIdx := -1
	for i, cmd := range runner.commands {
		if len(cmd) > 0 && cmd[0] == "set-environment" {
			lastEnvIdx = i
		}
	}
	firstPaneIdx := -1
	for i, cmd := range runner.commands {
		if len(cmd) > 0 && (cmd[0] == "send-keys" || cmd[0] == "split-window") {
			firstPaneIdx = i
			break
		}
	}
	if lastEnvIdx >= firstPaneIdx && firstPaneIdx >= 0 {
		t.Error("all env injection should happen before pane creation")
	}
}

func TestCreate_AttachSession(t *testing.T) {
	// Not inside tmux
	t.Setenv("TMUX", "")

	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes:  []config.Pane{{Cmd: "nvim"}},
		},
		Attach: true,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should use attach for session mode outside tmux
	idx := runner.findCommand("attach", "-t", "test-branch")
	if idx < 0 {
		t.Errorf("expected tmux attach:\n%s", runner.commandString())
	}
}

func TestCreate_AttachSwitchClient(t *testing.T) {
	// Inside tmux
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes:  []config.Pane{{Cmd: "nvim"}},
		},
		Attach: true,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should use switch-client when already inside tmux
	idx := runner.findCommand("switch-client", "-t", "test-branch")
	if idx < 0 {
		t.Errorf("expected tmux switch-client:\n%s", runner.commandString())
	}
}

func TestCreate_NoAttach(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes:  []config.Pane{{Cmd: "nvim"}},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// No attach/switch commands
	attachIdx := runner.findCommand("attach")
	switchIdx := runner.findCommand("switch-client")
	selectIdx := runner.findCommand("select-window")
	if attachIdx >= 0 || switchIdx >= 0 || selectIdx >= 0 {
		t.Errorf("should not attach/switch when Attach=false:\n%s", runner.commandString())
	}
}

func TestCreate_DefaultLayout(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode: "session",
			// No layout specified — should default to main-vertical
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	layoutIdx := runner.findCommand("select-layout", "-t", "test-branch", "main-vertical")
	if layoutIdx < 0 {
		t.Errorf("expected default layout main-vertical:\n%s", runner.commandString())
	}
}

func TestCreate_NoPanes(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{"PORT": "4000"},
		TmuxConfig: &config.TmuxConfig{
			Mode:  "session",
			Panes: []config.Pane{},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should still create session and inject env, just no pane commands
	sessionIdx := runner.findCommand("new-session")
	if sessionIdx < 0 {
		t.Error("should still create session")
	}
	envCmds := runner.findAllCommands("set-environment")
	if len(envCmds) != 1 {
		t.Errorf("expected 1 set-environment, got %d", len(envCmds))
	}
	splits := runner.findAllCommands("split-window")
	if len(splits) != 0 {
		t.Error("should have no split-window commands")
	}
}

func TestCreate_EmptyCommand(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "even-horizontal",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: ""}, // empty command pane
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Should create 1 split-window but only 1 send-keys (for nvim)
	splits := runner.findAllCommands("split-window")
	if len(splits) != 1 {
		t.Errorf("expected 1 split, got %d", len(splits))
	}
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 1 {
		t.Errorf("expected 1 send-keys (only nvim), got %d:\n%s", len(sendKeys), runner.commandString())
	}
}

func TestCreate_SinglePane(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes: []config.Pane{
				{Cmd: "nvim"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// No splits needed for a single pane
	splits := runner.findAllCommands("split-window")
	if len(splits) != 0 {
		t.Errorf("expected 0 splits for 1 pane, got %d", len(splits))
	}

	// 1 send-keys for the command
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 1 {
		t.Errorf("expected 1 send-keys, got %d", len(sendKeys))
	}
}

func TestDestroy_Session(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	cfg := &config.TmuxConfig{Mode: "session"}
	err := mgr.Destroy("feat/auth", cfg)
	if err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	idx := runner.findCommand("kill-session", "-t", "feat-auth")
	if idx < 0 {
		t.Errorf("expected kill-session:\n%s", runner.commandString())
	}
}

func TestDestroy_Window(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	cfg := &config.TmuxConfig{Mode: "window"}
	err := mgr.Destroy("feat/auth", cfg)
	if err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	idx := runner.findCommand("kill-window", "-t", "feat-auth")
	if idx < 0 {
		t.Errorf("expected kill-window:\n%s", runner.commandString())
	}
}

func TestCreate_FullCommandSequence_Session(t *testing.T) {
	// This test verifies the exact command sequence from the spec
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "feat/auth",
		WorktreePath: "/path/to/worktree",
		Env: map[string]string{
			"PORT":         "4045",
			"WEB_PORT":     "3045",
			"VITE_API_URL": "http://localhost:4045",
		},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "session",
			Layout: "main-vertical",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Cmd: "claude --model sonnet"},
				{Cmd: "pnpm dev"},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify the command order matches the spec
	expected := []struct {
		prefix []string
	}{
		// 1. Create session
		{[]string{"new-session", "-d", "-s", "feat-auth", "-c", "/path/to/worktree"}},
		// 2. Set environment (sorted alphabetically)
		{[]string{"set-environment", "-t", "feat-auth", "PORT", "4045"}},
		{[]string{"set-environment", "-t", "feat-auth", "VITE_API_URL", "http://localhost:4045"}},
		{[]string{"set-environment", "-t", "feat-auth", "WEB_PORT", "3045"}},
		// 3. First pane command
		{[]string{"send-keys", "-t", "feat-auth", "nvim", "Enter"}},
		// 4. Second pane
		{[]string{"split-window", "-h", "-t", "feat-auth", "-c", "/path/to/worktree"}},
		{[]string{"send-keys", "-t", "feat-auth", "claude --model sonnet", "Enter"}},
		// 5. Third pane
		{[]string{"split-window", "-h", "-t", "feat-auth", "-c", "/path/to/worktree"}},
		{[]string{"send-keys", "-t", "feat-auth", "pnpm dev", "Enter"}},
		// 6. Apply layout
		{[]string{"select-layout", "-t", "feat-auth", "main-vertical"}},
		// 7. Select first pane
		{[]string{"select-pane", "-t", "feat-auth.0"}},
	}

	if len(runner.commands) != len(expected) {
		t.Fatalf("expected %d commands, got %d:\n%s", len(expected), len(runner.commands), runner.commandString())
	}

	for i, exp := range expected {
		if i >= len(runner.commands) {
			t.Errorf("missing command at index %d: expected %v", i, exp.prefix)
			continue
		}
		cmd := runner.commands[i]
		for j, p := range exp.prefix {
			if j >= len(cmd) || cmd[j] != p {
				t.Errorf("command[%d]: expected %v, got %v", i, exp.prefix, cmd)
				break
			}
		}
	}
}

func TestParseSizePercent(t *testing.T) {
	tests := []struct {
		input string
		want  int
		err   bool
	}{
		{"70%", 70, false},
		{"50%", 50, false},
		{"100%", 100, false},
		{"70", 70, false},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		got, err := parseSizePercent(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("parseSizePercent(%q) error = %v, want error = %v", tt.input, err, tt.err)
		}
		if got != tt.want {
			t.Errorf("parseSizePercent(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestCreate_Tier3_SizeOnSplit(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode: "session",
			Panes: []config.Pane{
				{Cmd: "nvim", Size: "70%"},
				{Split: "vertical", Panes: []config.Pane{
					{Cmd: "claude", Size: "60%"},
					{Cmd: "pnpm dev"},
				}},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// The vertical container gets a horizontal split-window (from parent direction "h").
	// Inside the container, claude is the first child and reuses the pane from the
	// container's split (no split-window created for it). pnpm dev is split off
	// vertically. Since pnpm dev has no Size, no -p flag is generated for it.
	// Note: claude's Size "60%" only applies if a split-window is created for it;
	// since it reuses the existing pane, the size is not used here.
	splits := runner.findAllCommands("split-window")
	if len(splits) != 2 {
		t.Fatalf("expected 2 split-window commands, got %d:\n%s", len(splits), runner.commandString())
	}

	// Verify all 3 commands were sent
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 3 {
		t.Errorf("expected 3 send-keys, got %d:\n%s", len(sendKeys), runner.commandString())
	}

	// Verify the second split (pnpm dev) uses -v (vertical direction from the container)
	foundVerticalSplit := false
	for _, split := range splits {
		for _, arg := range split {
			if arg == "-v" {
				foundVerticalSplit = true
			}
		}
	}
	if !foundVerticalSplit {
		t.Errorf("expected a vertical split (-v) for the nested container:\n%s", runner.commandString())
	}
}

func TestCreate_Tier3_SizeOnNonFirstPane(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)

	// Tier 3 with a size on a non-first pane that gets its own split-window.
	// nvim is first (uses existing pane), claude gets split off with -p 40.
	opts := Options{
		Branch:       "test-branch",
		WorktreePath: "/work",
		Env:          map[string]string{},
		TmuxConfig: &config.TmuxConfig{
			Mode: "session",
			Panes: []config.Pane{
				{Cmd: "nvim"},
				{Split: "vertical", Panes: []config.Pane{
					{Cmd: "claude"},
					{Cmd: "pnpm dev", Size: "40%"},
				}},
			},
		},
		Attach: false,
	}

	err := mgr.Create(opts)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Find the split-window command that has the -p flag
	splits := runner.findAllCommands("split-window")
	foundSizeFlag := false
	for _, split := range splits {
		for i, arg := range split {
			if arg == "-p" && i+1 < len(split) && split[i+1] == "40" {
				foundSizeFlag = true
			}
		}
	}
	if !foundSizeFlag {
		t.Errorf("expected a split-window with -p 40 for pnpm dev:\n%s", runner.commandString())
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]string{
		"ZEBRA":    "1",
		"APPLE":    "2",
		"MANGO":    "3",
		"BANANA":   "4",
	}
	keys := sortedKeys(m)
	expected := []string{"APPLE", "BANANA", "MANGO", "ZEBRA"}
	if len(keys) != len(expected) {
		t.Fatalf("expected %d keys, got %d", len(expected), len(keys))
	}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("keys[%d] = %q, want %q", i, k, expected[i])
		}
	}
}
