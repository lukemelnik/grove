package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func executeCommandForHelp(t *testing.T, args ...string) string {
	t.Helper()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("command %v failed: %v\nOutput: %s", args, err, buf.String())
	}

	return buf.String()
}

func TestRootHelp_IncludesTmuxLayoutDiscoveryHints(t *testing.T) {
	output := executeCommandForHelp(t, "--help")

	for _, want := range []string{
		"Config defaults:",
		"worktree_dir is optional; if omitted Grove uses ../.grove-worktrees/<repo-name>",
		"Set worktree_dir only when you want a different location",
		"Env files in .grove.yml:",
		"env_files is for shared root-level env symlinks like .env.apple",
		"services.<name>.env_file is for service-scoped env files like apps/api/.env",
		"Tmux layout quick rules for .grove.yml:",
		"split: horizontal => children go left-to-right",
		"split: vertical   => children go top-to-bottom",
		"Full-width pane on the bottom/top => outer split should be vertical",
		"Full-height pane on the left/right => outer split should be horizontal",
		"Need help translating a pane layout into .grove.yml?",
		"grove create --help  Tmux split direction rules + nested layout example",
		"grove init --help    Notes on flat --pane flags vs nested YAML layouts",
		"grove schema         Full annotated config reference with tmux examples",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("root help should contain %q, got:\n%s", want, output)
		}
	}
}

func TestCreateHelp_IncludesTmuxSplitRules(t *testing.T) {
	output := executeCommandForHelp(t, "create", "--help")

	for _, want := range []string{
		"Tmux explicit split rules in .grove.yml:",
		"split: horizontal => children go left-to-right",
		"split: vertical   => children go top-to-bottom",
		"size applies along the split axis (width for horizontal, height for vertical).",
		"Example: two side-by-side pi panes with a small full-width terminal on the bottom:",
		"name: terminal",
		`size: "20%"`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("create help should contain %q, got:\n%s", want, output)
		}
	}
}

func TestInitHelp_ExplainsFlatPaneFlagAndNestedLayouts(t *testing.T) {
	output := executeCommandForHelp(t, "init", "--help")

	for _, want := range []string{
		"If --worktree-dir is omitted, Grove defaults to ../.grove-worktrees/<repo-name>.",
		"Set it only when you want a different location.",
		"Use --env-file for shared/root-level env symlinks (top-level env_files in .grove.yml),",
		"Service-scoped env files like apps/api/.env",
		"The --pane flag only creates a flat pane list.",
		"edit the generated .grove.yml (or start from 'grove schema').",
		"split: horizontal => children go left-to-right",
		"split: vertical   => children go top-to-bottom",
		"Example: two side-by-side pi panes with a small full-width terminal on the bottom:",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("init help should contain %q, got:\n%s", want, output)
		}
	}
}

func TestSchemaOutput_IncludesTmuxSplitExample(t *testing.T) {
	output := executeCommandForHelp(t, "schema")

	for _, want := range []string{
		"# Default when omitted: \"../.grove-worktrees/<repo-name>\"",
		"# worktree_dir: ../.grove-worktrees/shared",
		"# Split form — nested pane layout (Tier 3):",
		"#   split: horizontal => children go left-to-right",
		"#   split: vertical   => children go top-to-bottom",
		"# Example: two side-by-side pi panes with a small full-width terminal on the bottom:",
		"#         name: terminal",
		`#         size: "20%"`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("schema output should contain %q, got:\n%s", want, output)
		}
	}
}

func TestWorkflowHelp_IncludesOpenEnterAndAgentGuidance(t *testing.T) {
	rootOutput := executeCommandForHelp(t, "--help")
	for _, want := range []string{
		"grove create <branch> --no-open",
		"grove open <branch>",
		"grove enter <branch>",
		"Use grove create <branch> --no-open --json to provision without stealing tmux focus",
		"Use grove list --json to discover worktrees and use the returned worktree path as cwd",
		"Use grove open <branch> only when asked to open or restore the full tmux UI",
		"Avoid grove enter and interactive grove list unless explicitly asked",
		"Do not pass --force to delete/clean without explicit user approval",
	} {
		if !strings.Contains(rootOutput, want) {
			t.Errorf("root help should contain %q, got:\n%s", want, rootOutput)
		}
	}

	createOutput := executeCommandForHelp(t, "create", "--help")
	for _, want := range []string{
		"--no-open",
		"grove open <branch>",
		"grove enter <branch>",
	} {
		if !strings.Contains(createOutput, want) {
			t.Errorf("create help should contain %q, got:\n%s", want, createOutput)
		}
	}

	openOutput := executeCommandForHelp(t, "open", "--help")
	for _, want := range []string{
		"Open or restore the full Grove tmux workspace",
		"--new-window",
	} {
		if !strings.Contains(openOutput, want) {
			t.Errorf("open help should contain %q, got:\n%s", want, openOutput)
		}
	}

	enterOutput := executeCommandForHelp(t, "enter", "--help")
	for _, want := range []string{
		"Start an interactive subshell",
		"GROVE_BRANCH",
		"GROVE_WORKTREE",
	} {
		if !strings.Contains(enterOutput, want) {
			t.Errorf("enter help should contain %q, got:\n%s", want, enterOutput)
		}
	}
}
