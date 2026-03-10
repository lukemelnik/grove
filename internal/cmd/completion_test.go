package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionCmd_Bash(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"completion", "bash"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("completion bash failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "bash") && !strings.Contains(output, "grove") {
		t.Errorf("expected bash completion output, got:\n%s", output[:min(200, len(output))])
	}
	if len(output) < 100 {
		t.Errorf("expected substantial completion output, got %d bytes", len(output))
	}
}

func TestCompletionCmd_Zsh(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"completion", "zsh"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("completion zsh failed: %v", err)
	}

	output := buf.String()
	if len(output) < 100 {
		t.Errorf("expected substantial completion output, got %d bytes", len(output))
	}
}

func TestCompletionCmd_Fish(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"completion", "fish"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("completion fish failed: %v", err)
	}

	output := buf.String()
	if len(output) < 100 {
		t.Errorf("expected substantial completion output, got %d bytes", len(output))
	}
}

func TestCompletionCmd_InvalidShell(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"completion", "powershell"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for invalid shell")
	}
}

func TestCompletionCmd_NoArgs(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"completion"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing shell argument")
	}
}

func TestVersionFlag(t *testing.T) {
	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"--version"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("--version failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "dev") {
		t.Errorf("expected version output to contain 'dev', got:\n%s", output)
	}
}
