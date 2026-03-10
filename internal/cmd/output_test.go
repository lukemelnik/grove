package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestShouldOutputJSON_FlagSet(t *testing.T) {
	// When --json flag is set, shouldOutputJSON should return true
	// regardless of TTY status.
	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return true } // simulate TTY
	defer func() { isTerminal = origIsTerminal }()

	rootCmd := NewRootCmd()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"list", "--json"})

	// Walk the command tree to find the list subcommand with flags parsed.
	listCmd, _, err := rootCmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("failed to find list command: %v", err)
	}
	listCmd.Flags().Set("json", "true")

	if !shouldOutputJSON(listCmd) {
		t.Error("expected shouldOutputJSON to return true when --json is set")
	}
}

func TestShouldOutputJSON_NonTTY(t *testing.T) {
	// When stdout is not a TTY, shouldOutputJSON should return true.
	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return false } // simulate non-TTY
	defer func() { isTerminal = origIsTerminal }()

	rootCmd := NewRootCmd()
	listCmd, _, err := rootCmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("failed to find list command: %v", err)
	}

	if !shouldOutputJSON(listCmd) {
		t.Error("expected shouldOutputJSON to return true when not a TTY")
	}
}

func TestShouldOutputJSON_TTYNoFlag(t *testing.T) {
	// When stdout IS a TTY and --json is not set, should return false.
	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return true }
	defer func() { isTerminal = origIsTerminal }()

	rootCmd := NewRootCmd()
	listCmd, _, err := rootCmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("failed to find list command: %v", err)
	}

	if shouldOutputJSON(listCmd) {
		t.Error("expected shouldOutputJSON to return false in TTY without --json")
	}
}

func TestOutputError_JSONMode(t *testing.T) {
	// In JSON mode, outputError should write structured JSON to stderr.
	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return false } // simulate non-TTY (JSON mode)
	defer func() { isTerminal = origIsTerminal }()

	rootCmd := NewRootCmd()
	listCmd, _, err := rootCmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("failed to find list command: %v", err)
	}

	var errBuf bytes.Buffer
	listCmd.SetErr(&errBuf)

	testErr := errors.New("something went wrong")
	retErr := outputError(listCmd, testErr)

	if retErr == nil {
		t.Fatal("expected non-nil error in JSON mode")
	}
	if !ErrorAlreadyReported(retErr) {
		t.Errorf("expected structured error marker, got %v", retErr)
	}
	if !errors.Is(retErr, testErr) {
		t.Errorf("expected returned error to wrap original error, got %v", retErr)
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, `"error"`) {
		t.Errorf("expected JSON error on stderr, got: %s", stderr)
	}
	if !strings.Contains(stderr, "something went wrong") {
		t.Errorf("expected error message in JSON, got: %s", stderr)
	}
}

func TestOutputError_HumanMode(t *testing.T) {
	// In human mode (TTY, no --json), outputError should just return the error.
	origIsTerminal := isTerminal
	isTerminal = func(int) bool { return true }
	defer func() { isTerminal = origIsTerminal }()

	rootCmd := NewRootCmd()
	listCmd, _, err := rootCmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("failed to find list command: %v", err)
	}

	var errBuf bytes.Buffer
	listCmd.SetErr(&errBuf)

	testErr := errors.New("something went wrong")
	retErr := outputError(listCmd, testErr)

	if retErr != testErr {
		t.Errorf("expected original error returned, got %v", retErr)
	}
	if ErrorAlreadyReported(retErr) {
		t.Errorf("did not expect structured error marker in human mode, got %v", retErr)
	}

	if errBuf.Len() != 0 {
		t.Errorf("expected no stderr output in human mode, got: %s", errBuf.String())
	}
}
