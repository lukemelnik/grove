package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lukemelnik/grove/internal/certs"
)

func setupTrustTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	_, err := certs.EnsureCerts(dir)
	if err != nil {
		t.Fatalf("EnsureCerts failed: %v", err)
	}

	return dir
}

func TestTrust_Check_Trusted(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	setupTrustTest(t)

	origCheck := trustCheckCA
	trustCheckCA = func() bool { return true }
	t.Cleanup(func() { trustCheckCA = origCheck })

	origTerm := isTerminal
	isTerminal = func(int) bool { return true }
	t.Cleanup(func() { isTerminal = origTerm })

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"trust", "--check"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected no error for trusted CA, got: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("trusted")) {
		t.Errorf("expected 'trusted' in output, got: %s", buf.String())
	}
}

func TestTrust_Check_NotTrusted(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	setupTrustTest(t)

	origCheck := trustCheckCA
	trustCheckCA = func() bool { return false }
	t.Cleanup(func() { trustCheckCA = origCheck })

	origTerm := isTerminal
	isTerminal = func(int) bool { return true }
	t.Cleanup(func() { isTerminal = origTerm })

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"trust", "--check"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for untrusted CA")
	}
}

func TestTrust_Check_JSON_Trusted(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	setupTrustTest(t)

	origCheck := trustCheckCA
	trustCheckCA = func() bool { return true }
	t.Cleanup(func() { trustCheckCA = origCheck })

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"trust", "--check", "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out trustOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}

	if out.Trusted == nil || !*out.Trusted {
		t.Error("expected trusted=true in JSON output")
	}
}

func TestTrust_Check_JSON_NotTrusted(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	setupTrustTest(t)

	origCheck := trustCheckCA
	trustCheckCA = func() bool { return false }
	t.Cleanup(func() { trustCheckCA = origCheck })

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"trust", "--check", "--json"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for untrusted CA")
	}

	if !ErrorAlreadyReported(err) {
		t.Error("expected error to be already reported")
	}

	var out trustOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, stdout.String())
	}

	if out.Trusted == nil || *out.Trusted {
		t.Error("expected trusted=false in JSON output")
	}
}

func TestTrust_Add(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	setupTrustTest(t)

	var addedPath string
	origAdd := trustAddCA
	trustAddCA = func(certPath string) error {
		addedPath = certPath
		return nil
	}
	t.Cleanup(func() { trustAddCA = origAdd })

	origTerm := isTerminal
	isTerminal = func(int) bool { return true }
	t.Cleanup(func() { isTerminal = origTerm })

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"trust"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stateDir, _ := certs.DefaultStateDir()
	expectedPath := filepath.Join(stateDir, certs.CACertFile)
	if addedPath != expectedPath {
		t.Errorf("added cert path = %q, want %q", addedPath, expectedPath)
	}

	if !bytes.Contains(buf.Bytes(), []byte("added")) {
		t.Errorf("expected 'added' in output, got: %s", buf.String())
	}
}

func TestTrust_Remove(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	setupTrustTest(t)

	var removedPath string
	origRemove := trustRemoveCA
	trustRemoveCA = func(certPath string) error {
		removedPath = certPath
		return nil
	}
	t.Cleanup(func() { trustRemoveCA = origRemove })

	origTerm := isTerminal
	isTerminal = func(int) bool { return true }
	t.Cleanup(func() { isTerminal = origTerm })

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"trust", "--remove"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stateDir, _ := certs.DefaultStateDir()
	expectedPath := filepath.Join(stateDir, certs.CACertFile)
	if removedPath != expectedPath {
		t.Errorf("removed cert path = %q, want %q", removedPath, expectedPath)
	}
}

func TestTrust_MissingCA(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	dir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	origTerm := isTerminal
	isTerminal = func(int) bool { return true }
	t.Cleanup(func() { isTerminal = origTerm })

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"trust"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing CA")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("does not exist")) {
		t.Errorf("expected 'does not exist' in error, got: %v", err)
	}
}

func TestTrust_MissingCA_JSON(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	dir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	cmd := NewRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"trust", "--json"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing CA")
	}

	if !ErrorAlreadyReported(err) {
		t.Error("expected error to be already reported")
	}

	var out trustOutput
	if jsonErr := json.Unmarshal(stderr.Bytes(), &out); jsonErr != nil {
		t.Fatalf("invalid JSON on stderr: %v\n%s", jsonErr, stderr.String())
	}

	if !bytes.Contains([]byte(out.Message), []byte("does not exist")) {
		t.Errorf("expected 'does not exist' in JSON message, got: %s", out.Message)
	}
}

func TestTrust_NonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("test only applicable on non-macOS platforms")
	}

	dir := t.TempDir()
	origStateDir := certs.DefaultStateDir
	certs.DefaultStateDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { certs.DefaultStateDir = origStateDir })

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, certs.CACertFile), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	origTerm := isTerminal
	isTerminal = func(int) bool { return true }
	t.Cleanup(func() { isTerminal = origTerm })

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"trust"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on non-macOS platform")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("only supported on macOS")) {
		t.Errorf("expected platform error, got: %v", err)
	}
}

func TestTrust_Add_JSON(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("trust tests require macOS")
	}

	setupTrustTest(t)

	origAdd := trustAddCA
	trustAddCA = func(string) error { return nil }
	t.Cleanup(func() { trustAddCA = origAdd })

	cmd := NewRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"trust", "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out trustOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}

	if out.Action != "added" {
		t.Errorf("expected action 'added', got %q", out.Action)
	}
}
