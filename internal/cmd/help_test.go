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
