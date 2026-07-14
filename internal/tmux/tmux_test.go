package tmux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemelnik/grove/internal/config"
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
	if len(args) >= 3 && args[0] == "new-window" && args[1] == "-P" {
		return "@mock-window", nil
	}
	if len(args) > 0 && args[0] == "has-session" {
		return "", errors.New("session not found")
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

func TestRealRunnerRedactsSetEnvironmentErrors(t *testing.T) {
	installFailingTmux(t)

	secretValue := "super-secret-managed-env-value"
	out, err := (&realRunner{}).Run("set-environment", "-t", "safe-session", "API_TOKEN", secretValue)
	if err == nil {
		t.Fatal("expected failing tmux error")
	}
	if out != "" {
		t.Fatalf("sensitive tmux output was returned: %q", out)
	}

	errText := err.Error()
	for _, want := range []string{"set-environment", "safe-session", "API_TOKEN", redactedTmuxArg} {
		if !strings.Contains(errText, want) {
			t.Fatalf("expected sanitized error to contain %q, got %q", want, errText)
		}
	}
	for _, leaked := range []string{secretValue, out} {
		if leaked != "" && strings.Contains(errText, leaked) {
			t.Fatalf("sanitized error leaked %q in %q", leaked, errText)
		}
	}
}

func TestRealRunnerRedactsSendKeysErrors(t *testing.T) {
	installFailingTmux(t)

	setupPayload := "export API_TOKEN=setup-secret"
	commandPayload := "pnpm dev --password command-secret"
	out, err := (&realRunner{}).Run("send-keys", "-t", "%7", setupPayload+" && "+commandPayload, "Enter")
	if err == nil {
		t.Fatal("expected failing tmux error")
	}
	if out != "" {
		t.Fatalf("sensitive tmux output was returned: %q", out)
	}

	errText := err.Error()
	for _, want := range []string{"send-keys", "%7", redactedTmuxArg} {
		if !strings.Contains(errText, want) {
			t.Fatalf("expected sanitized error to contain %q, got %q", want, errText)
		}
	}
	for _, leaked := range []string{setupPayload, commandPayload, "Enter", out} {
		if leaked != "" && strings.Contains(errText, leaked) {
			t.Fatalf("sanitized error leaked %q in %q", leaked, errText)
		}
	}
}

func installFailingTmux(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf 'synthetic tmux echoed args: %s\\n' \"$*\" >&2\nexit 42\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSessionName(t *testing.T) {
	if got := SessionName("main"); got != "main" {
		t.Fatalf("SessionName(main) = %q, want main", got)
	}

	slash := SessionName("feat/auth")
	dash := SessionName("feat-auth")
	if slash == dash {
		t.Fatalf("expected distinct session names for feat/auth and feat-auth, got %q", slash)
	}
	if !strings.HasPrefix(slash, "feat-auth-") {
		t.Fatalf("expected readable slash-branch prefix, got %q", slash)
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
	name := SessionName("feat/auth")

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

	// Verify session creation (no -e flags on new-session)
	idx := runner.findCommand("new-session", "-d", "-s", name)
	if idx < 0 {
		t.Errorf("expected new-session command, got:\n%s", runner.commandString())
	}

	// Session mode uses set-environment for managed env vars
	envCmds := runner.findAllCommands("set-environment")
	if len(envCmds) != 2 {
		t.Errorf("expected 2 set-environment commands, got %d:\n%s", len(envCmds), runner.commandString())
	}
	for _, cmd := range envCmds {
		if cmd[2] != name {
			t.Errorf("expected set-environment on session %s, got target %s", name, cmd[2])
		}
	}

	// Verify env injection is before pane creation
	firstEnvIdx := runner.findCommand("set-environment")
	firstSplitIdx := runner.findCommand("split-window")
	if firstEnvIdx > firstSplitIdx && firstSplitIdx >= 0 {
		t.Error("env injection should happen before pane creation")
	}

	// Verify split-window commands (no -e flags)
	splits := runner.findAllCommands("split-window")
	if len(splits) != 2 {
		t.Errorf("expected 2 split-window commands, got %d:\n%s", len(splits), runner.commandString())
	}

	// Verify pane commands
	sendKeys := runner.findAllCommands("send-keys")
	if len(sendKeys) != 3 {
		t.Errorf("expected 3 send-keys commands, got %d:\n%s", len(sendKeys), runner.commandString())
	}

	// Verify layout is applied
	layoutIdx := runner.findCommand("select-layout", "-t", name, "main-vertical")
	if layoutIdx < 0 {
		t.Errorf("expected select-layout command, got:\n%s", runner.commandString())
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

	// Window mode should NOT use set-environment (avoids env leaking between windows)
	envCmds := runner.findAllCommands("set-environment")
	if len(envCmds) != 0 {
		t.Errorf("window mode should not use set-environment (env leak), got %d:\n%s", len(envCmds), runner.commandString())
	}
}

func TestCreate_WindowModeOutsideTmuxReturnsError(t *testing.T) {
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
	if err == nil {
		t.Fatal("expected error when window mode is used outside tmux")
	}
	if !strings.Contains(err.Error(), "window mode requires running inside tmux") {
		t.Fatalf("expected explicit window mode error, got: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("expected no tmux commands on early window-mode failure, got:\n%s", runner.commandString())
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

	// Session mode: set-environment for manually created panes/windows
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

	// set-environment should come BEFORE any split-window or send-keys
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

	// Should still create session and use set-environment
	sessionIdx := runner.findCommand("new-session")
	if sessionIdx < 0 {
		t.Error("should still create session")
	}
	// set-environment for session mode
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

func TestCreate_FullCommandSequence_Session(t *testing.T) {
	// This test verifies the exact command sequence for session mode
	runner := newMockRunner()
	mgr := NewManager(runner)
	name := SessionName("feat/auth")

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

	// Verify the command order:
	// 1. new-session (no -e flags)
	// 2. Grove labels
	// 3. set-environment (session mode, for managed env vars)
	// 4. send-keys for first pane
	// 5. split-window + send-keys for each additional pane (no -e flags)
	// 6. select-layout
	// 7. select-pane
	expected := []struct {
		prefix []string
	}{
		// 1. Fail closed before creating a session with an existing unlabeled name
		{[]string{"has-session", "-t", name}},
		// 2. Create session (no -e flags)
		{[]string{"new-session", "-d", "-s", name, "-c", "/path/to/worktree"}},
		// 2. Label the Grove target
		{[]string{"set", "-t", name, "@grove.project_root", ""}},
		{[]string{"set", "-t", name, "@grove.branch", "feat/auth"}},
		{[]string{"set", "-t", name, "@grove.worktree_path", "/path/to/worktree"}},
		{[]string{"set", "-t", name, "@grove.role", "canonical"}},
		// 3. Set environment (sorted alphabetically)
		{[]string{"set-environment", "-t", name, "PORT", "4045"}},
		{[]string{"set-environment", "-t", name, "VITE_API_URL", "http://localhost:4045"}},
		{[]string{"set-environment", "-t", name, "WEB_PORT", "3045"}},
		// 3. First pane command
		{[]string{"send-keys", "-t", name, "nvim", "Enter"}},
		// 4. Second pane (no -e flags)
		{[]string{"split-window", "-h", "-t", name, "-c", "/path/to/worktree"}},
		{[]string{"send-keys", "-t", name, "claude --model sonnet", "Enter"}},
		// 5. Third pane (no -e flags)
		{[]string{"split-window", "-h", "-t", name, "-c", "/path/to/worktree"}},
		{[]string{"send-keys", "-t", name, "pnpm dev", "Enter"}},
		// 6. Apply layout
		{[]string{"select-layout", "-t", name, "main-vertical"}},
		// 7. Select first pane
		{[]string{"select-pane", "-t", name + ".0"}},
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
		if len(cmd) != len(exp.prefix) {
			t.Errorf("command[%d]: expected %v, got %v", i, exp.prefix, cmd)
			continue
		}
		for j, p := range exp.prefix {
			if j >= len(cmd) || cmd[j] != p {
				t.Errorf("command[%d]: expected %v, got %v", i, exp.prefix, cmd)
				break
			}
		}
	}
}

func TestCreate_WindowMode_EnvOnSplitWindow(t *testing.T) {
	// Verify that window mode does NOT use -e flags or set-environment
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	runner := newMockRunner()
	mgr := NewManager(runner)

	opts := Options{
		Branch:       "feat/auth",
		WorktreePath: "/work",
		Env: map[string]string{
			"PORT":     "4045",
			"WEB_PORT": "3045",
		},
		TmuxConfig: &config.TmuxConfig{
			Mode:   "window",
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

	// No set-environment in window mode
	envCmds := runner.findAllCommands("set-environment")
	if len(envCmds) != 0 {
		t.Errorf("window mode should not use set-environment, got %d:\n%s", len(envCmds), runner.commandString())
	}

	// new-window should NOT have -e flags (env no longer injected per-pane)
	newWindowIdx := runner.findCommand("new-window")
	newWindowCmd := runner.commands[newWindowIdx]
	for _, arg := range newWindowCmd {
		if arg == "-e" {
			t.Errorf("new-window should not have -e flags: %v", newWindowCmd)
			break
		}
	}

	// split-window should NOT have -e flags
	splits := runner.findAllCommands("split-window")
	if len(splits) != 2 {
		t.Fatalf("expected 2 split-window, got %d", len(splits))
	}
	for _, split := range splits {
		for _, arg := range split {
			if arg == "-e" {
				t.Errorf("split-window should not have -e flags: %v", split)
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

func TestSendPaneCommands_Setup(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name     string
		pane     config.Pane
		wantKeys [][]string // expected send-keys commands
	}{
		{
			name: "cmd only with autorun",
			pane: config.Pane{Cmd: "pnpm dev"},
			wantKeys: [][]string{
				{"send-keys", "-t", "test", "pnpm dev", "Enter"},
			},
		},
		{
			name: "cmd only without autorun",
			pane: config.Pane{Cmd: "pnpm dev", Autorun: boolPtr(false)},
			wantKeys: [][]string{
				{"send-keys", "-t", "test", "pnpm dev"},
			},
		},
		{
			name: "setup only",
			pane: config.Pane{Setup: "pnpm install"},
			wantKeys: [][]string{
				{"send-keys", "-t", "test", "pnpm install", "Enter"},
			},
		},
		{
			name: "setup + cmd with autorun",
			pane: config.Pane{Setup: "pnpm install", Cmd: "pnpm dev"},
			wantKeys: [][]string{
				{"send-keys", "-t", "test", "pnpm install && pnpm dev", "Enter"},
			},
		},
		{
			name: "setup + cmd without autorun",
			pane: config.Pane{Setup: "pnpm install", Cmd: "pnpm dev", Autorun: boolPtr(false)},
			wantKeys: [][]string{
				{"send-keys", "-t", "test", "pnpm install", "Enter"},
				{"send-keys", "-t", "test", "pnpm dev"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := newMockRunner()
			mgr := NewManager(runner)

			err := mgr.sendPaneCommands("test", tt.pane)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			sendKeys := runner.findAllCommands("send-keys")
			if len(sendKeys) != len(tt.wantKeys) {
				t.Fatalf("expected %d send-keys commands, got %d:\n%s", len(tt.wantKeys), len(sendKeys), runner.commandString())
			}

			for i, want := range tt.wantKeys {
				got := sendKeys[i]
				if len(got) != len(want) {
					t.Errorf("send-keys[%d]: expected %v, got %v", i, want, got)
					continue
				}
				for j := range want {
					if got[j] != want[j] {
						t.Errorf("send-keys[%d][%d]: expected %q, got %q", i, j, want[j], got[j])
					}
				}
			}
		})
	}
}

func TestHasSession(t *testing.T) {
	t.Run("session exists", func(t *testing.T) {
		runner := newMockRunner()
		runner.outputs["has-session -t test-session"] = ""
		mgr := NewManager(runner)

		got := mgr.HasSession("test-session")
		if !got {
			t.Error("expected HasSession to return true when no error")
		}
	})

	t.Run("session does not exist", func(t *testing.T) {
		runner := newMockRunner()
		runner.errors["has-session -t missing-session"] = fmt.Errorf("session not found")
		mgr := NewManager(runner)

		got := mgr.HasSession("missing-session")
		if got {
			t.Error("expected HasSession to return false when error")
		}
	})
}

func TestCreateWindow_DoesNotAdoptSameNameFromAnySession(t *testing.T) {
	runner := newMockRunner()
	runner.outputs["new-window -P -F #{window_id} -t current -n shared-name -c /worktree"] = "@42"
	mgr := NewManager(runner)

	target, err := mgr.createWindow("current", "shared-name", "/worktree")
	if err != nil {
		t.Fatalf("createWindow failed: %v", err)
	}
	if target != "@42" {
		t.Fatalf("target = %q, want newly created window id @42", target)
	}
	if runner.findCommand("new-window", "-P", "-F", "#{window_id}", "-t", "current") < 0 {
		t.Fatalf("expected a new window in current session, got:\n%s", runner.commandString())
	}
	if runner.findCommand("list-windows") >= 0 {
		t.Fatalf("createWindow must not inspect/adopt existing windows: %s", runner.commandString())
	}
}

func TestAttach_OutsideTmux_Session(t *testing.T) {
	t.Setenv("TMUX", "")

	runner := newMockRunner()
	mgr := NewManager(runner)

	err := mgr.Attach("test-session", "session")
	if err != nil {
		t.Fatalf("Attach failed: %v", err)
	}

	idx := runner.findCommand("attach", "-t", "test-session")
	if idx < 0 {
		t.Errorf("expected tmux attach:\n%s", runner.commandString())
	}
}

func TestAttach_InsideTmux_SwitchClient(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	runner := newMockRunner()
	mgr := NewManager(runner)

	err := mgr.Attach("test-session", "session")
	if err != nil {
		t.Fatalf("Attach failed: %v", err)
	}

	idx := runner.findCommand("switch-client", "-t", "test-session")
	if idx < 0 {
		t.Errorf("expected switch-client:\n%s", runner.commandString())
	}
}

func TestAttach_InsideTmux_WindowMode_SelectsWindowNotSession(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	runner := newMockRunner()
	// Simulate a separate session named "test" existing elsewhere. The attach
	// path must still target the window "test" in the current tmux workflow.
	runner.outputs["list-windows -a -F #{session_name} -f #{==:#{window_name},test}"] = "my-session"
	mgr := NewManager(runner)

	err := mgr.Attach("test", "window")
	if err != nil {
		t.Fatalf("Attach failed: %v", err)
	}

	selectIdx := runner.findCommand("select-window", "-t", "my-session:test")
	if selectIdx < 0 {
		t.Errorf("expected select-window with explicit session:window target:\n%s", runner.commandString())
	}
	switchIdx := runner.findCommand("switch-client", "-t", "test")
	if switchIdx >= 0 {
		t.Errorf("window mode inside tmux should not switch-client by branch/session name:\n%s", runner.commandString())
	}
}

func TestAttach_WindowMode(t *testing.T) {
	t.Setenv("TMUX", "")

	runner := newMockRunner()
	// findWindowSession needs list-windows to return a session name
	runner.outputs["list-windows -a -F #{session_name} -f #{==:#{window_name},test-window}"] = "my-session"
	mgr := NewManager(runner)

	err := mgr.Attach("test-window", "window")
	if err != nil {
		t.Fatalf("Attach failed: %v", err)
	}

	// Should select the window and then attach to the parent session
	selectIdx := runner.findCommand("select-window", "-t", "my-session:test-window")
	if selectIdx < 0 {
		t.Errorf("expected select-window with session:window target:\n%s", runner.commandString())
	}
	attachIdx := runner.findCommand("attach", "-t", "my-session")
	if attachIdx < 0 {
		t.Errorf("expected tmux attach to parent session:\n%s", runner.commandString())
	}
}

func TestAttach_WindowMode_Fallback(t *testing.T) {
	t.Setenv("TMUX", "")

	runner := newMockRunner()
	// findWindowSession fails — no matching window found
	runner.errors["list-windows -a -F #{session_name} -f #{==:#{window_name},test-window}"] = fmt.Errorf("no windows")
	mgr := NewManager(runner)

	err := mgr.Attach("test-window", "window")
	if err != nil {
		t.Fatalf("Attach failed: %v", err)
	}

	// Should fall back to direct attach with the window name
	idx := runner.findCommand("attach", "-t", "test-window")
	if idx < 0 {
		t.Errorf("expected fallback tmux attach:\n%s", runner.commandString())
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]string{
		"ZEBRA":  "1",
		"APPLE":  "2",
		"MANGO":  "3",
		"BANANA": "4",
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

func TestCreate_LabelsSessionTarget(t *testing.T) {
	runner := newMockRunner()
	mgr := NewManager(runner)
	name := SessionName("feat/labels")

	err := mgr.Create(Options{
		ProjectRoot:  "/repo",
		Branch:       "feat/labels",
		WorktreePath: "/repo-wt/feat-labels",
		Env:          map[string]string{},
		TmuxConfig:   &config.TmuxConfig{Mode: "session"},
		Attach:       false,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for _, want := range [][]string{
		{"set", "-t", name, "@grove.project_root", "/repo"},
		{"set", "-t", name, "@grove.branch", "feat/labels"},
		{"set", "-t", name, "@grove.worktree_path", "/repo-wt/feat-labels"},
		{"set", "-t", name, "@grove.role", "canonical"},
	} {
		if runner.findCommand(want...) < 0 {
			t.Fatalf("expected label command %v, got:\n%s", want, runner.commandString())
		}
	}
}

func TestFindCanonicalUsesLabelsAndProjectRoot(t *testing.T) {
	runner := newMockRunner()
	runner.outputs["list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = ""
	runner.outputs["list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = strings.Join([]string{
		"@1\tdev\trenamed\t/repo-a\tfeat/shared\t/repo-a-wt\tcanonical",
		"@2\tdev\trenamed-again\t/repo-b\tfeat/shared\t/repo-b-wt\tcanonical",
	}, "\n")
	mgr := NewManager(runner)

	target, ok, err := mgr.FindCanonical("/repo-b", "feat/shared", "/repo-b-wt", "window")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected canonical target")
	}
	if target.Target != "@2" || target.Name != "renamed-again" || target.Session != "dev" {
		t.Fatalf("unexpected target: %+v", target)
	}
}

func TestFindCanonicalFailsClosedOnUnexpectedDiscoveryError(t *testing.T) {
	runner := newMockRunner()
	command := "list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"
	runner.errors[command] = errors.New("permission denied")
	mgr := NewManager(runner)
	if _, _, err := mgr.FindCanonical("/repo", "feat/x", "/wt", "window"); err == nil {
		t.Fatal("expected unexpected discovery error")
	}
}

func TestFindCanonicalTreatsNoTmuxServerAsNoTarget(t *testing.T) {
	runner := newMockRunner()
	command := "list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"
	runner.errors[command] = errors.New("no server running on /tmp/tmux.sock")
	mgr := NewManager(runner)
	if _, ok, err := mgr.FindCanonical("/repo", "feat/x", "/wt", "window"); err != nil || ok {
		t.Fatalf("FindCanonical = ok %t, err %v; want no target", ok, err)
	}
}

func TestDestroyLabeledKillsMatchingTargets(t *testing.T) {
	runner := newMockRunner()
	runner.outputs["list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = "grove-session\t/repo\tfeat/destroy\t/wt\tcanonical"
	runner.outputs["list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = strings.Join([]string{
		"@1\tgrove-session\textra\t/repo\tfeat/destroy\t/wt\textra",
		"@2\tdev\textra\t/repo\tfeat/destroy\t/wt\textra",
		"@3\tdev\tother\t/repo\tfeat/other\t/other\tcanonical",
	}, "\n")
	mgr := NewManager(runner)

	killed, err := mgr.DestroyLabeled("/repo", "feat/destroy", "/wt")
	if err != nil {
		t.Fatalf("DestroyLabeled failed: %v", err)
	}
	if !killed {
		t.Fatal("expected labeled targets to be killed")
	}
	if runner.findCommand("kill-session", "-t", "grove-session") < 0 {
		t.Fatalf("expected labeled session kill, got:\n%s", runner.commandString())
	}
	if runner.findCommand("kill-window", "-t", "@2") < 0 {
		t.Fatalf("expected labeled extra window kill, got:\n%s", runner.commandString())
	}
	if runner.findCommand("kill-window", "-t", "@3") >= 0 {
		t.Fatalf("should not kill labels for other branches, got:\n%s", runner.commandString())
	}
}

func TestCreate_SessionModeRefusesUnlabeledExistingSession(t *testing.T) {
	runner := newMockRunner()
	runner.outputs["has-session -t "+SessionName("feat/collision")] = ""
	mgr := NewManager(runner)

	err := mgr.Create(Options{
		ProjectRoot:  "/repo",
		Branch:       "feat/collision",
		WorktreePath: "/wt",
		TmuxConfig:   &config.TmuxConfig{Mode: "session"},
		Attach:       false,
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to adopt") {
		t.Fatalf("expected fail-closed adoption error, got %v", err)
	}
	if runner.findCommand("new-session") >= 0 {
		t.Fatalf("must not create after detecting existing session, got:\n%s", runner.commandString())
	}
}

func TestCreate_WindowModeAlwaysCreatesExactWindowID(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")
	runner := newMockRunner()
	name := SessionName("feat/collision")
	runner.outputs["new-window -P -F #{window_id} -t my-session -n "+name+" -c /wt"] = "@77"
	mgr := NewManager(runner)

	err := mgr.Create(Options{
		ProjectRoot:  "/repo",
		Branch:       "feat/collision",
		WorktreePath: "/wt",
		TmuxConfig:   &config.TmuxConfig{Mode: "window"},
		Attach:       false,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if runner.findCommand("list-windows", "-t", "my-session") >= 0 {
		t.Fatalf("window creation must not check/adopt same-name windows, got:\n%s", runner.commandString())
	}
	if runner.findCommand("setw", "-t", "@77", "@grove.role", "canonical") < 0 {
		t.Fatalf("expected exact created window id to be labeled, got:\n%s", runner.commandString())
	}
}

func TestCreate_RollbacksCreatedSessionOnEnvFailure(t *testing.T) {
	runner := newMockRunner()
	name := SessionName("feat/rollback-session")
	runner.errors["set-environment -t "+name+" TOKEN secret"] = errors.New("boom secret")
	mgr := NewManager(runner)

	err := mgr.Create(Options{
		ProjectRoot:  "/repo",
		Branch:       "feat/rollback-session",
		WorktreePath: "/wt",
		Env:          map[string]string{"TOKEN": "secret"},
		TmuxConfig:   &config.TmuxConfig{Mode: "session"},
		Attach:       false,
	})
	if err == nil || !strings.Contains(err.Error(), "injecting environment") {
		t.Fatalf("expected original env error, got %v", err)
	}
	if runner.findCommand("kill-session", "-t", name) < 0 {
		t.Fatalf("expected created session rollback, got:\n%s", runner.commandString())
	}
}

func TestCreate_RollbacksCreatedWindowOnPaneFailure(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")
	runner := newMockRunner()
	name := SessionName("feat/rollback-window")
	runner.outputs["new-window -P -F #{window_id} -t my-session -n "+name+" -c /wt"] = "@88"
	runner.errors["send-keys -t @88 nvim Enter"] = errors.New("send failed secret")
	mgr := NewManager(runner)

	err := mgr.Create(Options{
		ProjectRoot:  "/repo",
		Branch:       "feat/rollback-window",
		WorktreePath: "/wt",
		TmuxConfig:   &config.TmuxConfig{Mode: "window", Panes: []config.Pane{{Cmd: "nvim"}}},
		Attach:       false,
	})
	if err == nil || !strings.Contains(err.Error(), "creating panes") {
		t.Fatalf("expected original pane error, got %v", err)
	}
	if runner.findCommand("kill-window", "-t", "@88") < 0 {
		t.Fatalf("expected created window rollback by exact id, got:\n%s", runner.commandString())
	}
}

func TestCreate_RollbackReportsCleanupFailure(t *testing.T) {
	runner := newMockRunner()
	name := SessionName("feat/rollback-cleanup")
	runner.errors["set -t "+name+" @grove.branch feat/rollback-cleanup"] = errors.New("label failed")
	runner.errors["kill-session -t "+name] = errors.New("cleanup failed")
	mgr := NewManager(runner)

	err := mgr.Create(Options{
		ProjectRoot:  "/repo",
		Branch:       "feat/rollback-cleanup",
		WorktreePath: "/wt",
		TmuxConfig:   &config.TmuxConfig{Mode: "session"},
		Attach:       false,
	})
	if err == nil || !strings.Contains(err.Error(), "labeling tmux target") || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("expected original and rollback errors, got %v", err)
	}
}

func TestDestroyLabeledPreservesLastWindowWithPlaceholderBeforeKill(t *testing.T) {
	runner := newMockRunner()
	runner.outputs["list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = ""
	runner.outputs["list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = "@1\tdev\tfeat\t/repo\tfeat/last\t/wt\tcanonical"
	runner.outputs["list-windows -t dev -F #{window_id}"] = "@1"
	mgr := NewManager(runner)

	killed, err := mgr.DestroyLabeled("/repo", "feat/last", "/wt")
	if err != nil || !killed {
		t.Fatalf("DestroyLabeled = %v, %v", killed, err)
	}
	placeholderIdx := runner.findCommand("new-window", "-d", "-t", "dev", "-n", "grove-placeholder")
	killIdx := runner.findCommand("kill-window", "-t", "@1")
	if placeholderIdx < 0 || killIdx < 0 || placeholderIdx > killIdx {
		t.Fatalf("expected placeholder before exact kill, got:\n%s", runner.commandString())
	}
}

func TestDestroyLabeledPlaceholderFailureDoesNotKillLastWindow(t *testing.T) {
	runner := newMockRunner()
	runner.outputs["list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = ""
	runner.outputs["list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = "@1\tdev\tfeat\t/repo\tfeat/last\t/wt\tcanonical"
	runner.outputs["list-windows -t dev -F #{window_id}"] = "@1"
	runner.errors["new-window -d -t dev -n grove-placeholder"] = errors.New("placeholder failed")
	mgr := NewManager(runner)

	killed, err := mgr.DestroyLabeled("/repo", "feat/last", "/wt")
	if err == nil || killed {
		t.Fatalf("expected reported placeholder failure without successful kill, got killed=%v err=%v", killed, err)
	}
	if runner.findCommand("kill-window", "-t", "@1") >= 0 {
		t.Fatalf("must not kill last Grove window when placeholder fails, got:\n%s", runner.commandString())
	}
}

func TestDestroyLabeledWindowCountFailureDoesNotKill(t *testing.T) {
	runner := newMockRunner()
	runner.outputs["list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = ""
	runner.outputs["list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = "@1\tdev\tfeat\t/repo\tfeat/count\t/wt\tcanonical"
	runner.errors["list-windows -t dev -F #{window_id}"] = errors.New("count failed")
	mgr := NewManager(runner)

	killed, err := mgr.DestroyLabeled("/repo", "feat/count", "/wt")
	if err == nil || killed {
		t.Fatalf("expected count failure without successful kill, got killed=%v err=%v", killed, err)
	}
	if runner.findCommand("kill-window", "-t", "@1") >= 0 {
		t.Fatalf("must not kill when window count cannot be queried, got:\n%s", runner.commandString())
	}
}

func TestCreate_RollbacksCreatedWindowOnAttachFailure(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")
	runner := newMockRunner()
	name := SessionName("feat/attach-fail")
	runner.outputs["new-window -P -F #{window_id} -t my-session -n "+name+" -c /wt"] = "@91"
	runner.errors["select-window -t @91"] = errors.New("attach failed")
	mgr := NewManager(runner)

	err := mgr.Create(Options{
		ProjectRoot:  "/repo",
		Branch:       "feat/attach-fail",
		WorktreePath: "/wt",
		TmuxConfig:   &config.TmuxConfig{Mode: "window"},
		Attach:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "attaching to tmux") {
		t.Fatalf("expected attach error, got %v", err)
	}
	if runner.findCommand("kill-window", "-t", "@91") < 0 {
		t.Fatalf("expected exact created window rollback, got:\n%s", runner.commandString())
	}
}

func TestCreate_WindowModeEmptyWindowIDFailsClosed(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")
	runner := newMockRunner()
	name := SessionName("feat/no-id")
	runner.outputs["new-window -P -F #{window_id} -t my-session -n "+name+" -c /wt"] = ""
	mgr := NewManager(runner)

	err := mgr.Create(Options{
		ProjectRoot:  "/repo",
		Branch:       "feat/no-id",
		WorktreePath: "/wt",
		TmuxConfig:   &config.TmuxConfig{Mode: "window"},
		Attach:       false,
	})
	if err == nil || !strings.Contains(err.Error(), "did not return an exact window id") {
		t.Fatalf("expected empty id error, got %v", err)
	}
	if runner.findCommand("kill-window") >= 0 || runner.findCommand("setw") >= 0 {
		t.Fatalf("must not guess target or mutate without exact window id, got:\n%s", runner.commandString())
	}
}

func TestDestroyLabeledKilledBoolRequiresSuccessfulExactKill(t *testing.T) {
	runner := newMockRunner()
	runner.outputs["list-sessions -F #{session_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = ""
	runner.outputs["list-windows -a -F #{window_id}\t#{session_name}\t#{window_name}\t#{@grove.project_root}\t#{@grove.branch}\t#{@grove.worktree_path}\t#{@grove.role}"] = "@1\tdev\tfeat\t/repo\tfeat/kill-fail\t/wt\tcanonical"
	runner.outputs["list-windows -t dev -F #{window_id}"] = "@1\n@2"
	runner.errors["kill-window -t @1"] = errors.New("kill failed")
	mgr := NewManager(runner)

	killed, err := mgr.DestroyLabeled("/repo", "feat/kill-fail", "/wt")
	if err == nil || killed {
		t.Fatalf("expected kill failure with killed=false, got killed=%v err=%v", killed, err)
	}
}

func TestCreate_ForceNewWindowLabelsExtra(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux/default,1,0")
	runner := newMockRunner()
	name := SessionName("feat/extra")
	runner.outputs["new-window -P -F #{window_id} -t my-session -n "+name+" -c /wt"] = "@42"
	mgr := NewManager(runner)

	err := mgr.Create(Options{
		ProjectRoot:    "/repo",
		Branch:         "feat/extra",
		WorktreePath:   "/wt",
		Env:            map[string]string{},
		TmuxConfig:     &config.TmuxConfig{Mode: "window"},
		Attach:         false,
		Role:           RoleExtra,
		ForceNewWindow: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if runner.findCommand("new-window", "-P", "-F", "#{window_id}", "-t", "my-session") < 0 {
		t.Fatalf("expected forced new-window, got:\n%s", runner.commandString())
	}
	if runner.findCommand("setw", "-t", "@42", "@grove.role", "extra") < 0 {
		t.Fatalf("expected extra role label on new window, got:\n%s", runner.commandString())
	}
}
