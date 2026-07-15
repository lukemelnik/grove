package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestFormatProgressFrame(t *testing.T) {
	got := formatProgressFrame(0, "Removing Git worktree and branch")
	want := progressClear + "⠋ Removing Git worktree and branch"
	if got != want {
		t.Fatalf("formatProgressFrame() = %q, want %q", got, want)
	}
}

func TestTerminalProgressRendersAndClears(t *testing.T) {
	var buf bytes.Buffer
	p := &terminalProgress{writer: &buf, enabled: true}
	p.Update("Checking branch safety")

	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	p.render(started.Add(progressDelay))
	if got := buf.String(); !strings.Contains(got, "⠋ Checking branch safety") {
		t.Fatalf("rendered progress = %q", got)
	}

	p.Clear()
	if !strings.HasSuffix(buf.String(), progressClear) {
		t.Fatalf("cleared progress = %q, want clear-line suffix", buf.String())
	}
}

func TestTerminalProgressDisabledIsSilent(t *testing.T) {
	var buf bytes.Buffer
	p := newTerminalProgress(&buf, false)
	p.Update("Removing Git worktree and branch")
	p.Clear()
	p.Stop()
	if buf.Len() != 0 {
		t.Fatalf("disabled progress wrote %q", buf.String())
	}
}
