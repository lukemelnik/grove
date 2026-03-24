package hosts

import (
	"strings"
	"testing"
)

func TestBuildBlock(t *testing.T) {
	block := buildBlock([]string{"web.myapp.localhost", "api.myapp.localhost"})
	if !strings.Contains(block, markerStart) {
		t.Error("missing start marker")
	}
	if !strings.Contains(block, markerEnd) {
		t.Error("missing end marker")
	}
	if !strings.Contains(block, "127.0.0.1 api.myapp.localhost") {
		t.Error("missing api entry")
	}
	if !strings.Contains(block, "127.0.0.1 web.myapp.localhost") {
		t.Error("missing web entry")
	}
	// Should be sorted
	apiIdx := strings.Index(block, "api.myapp")
	webIdx := strings.Index(block, "web.myapp")
	if apiIdx > webIdx {
		t.Error("entries should be sorted alphabetically")
	}
}

func TestExtractBlock(t *testing.T) {
	content := "127.0.0.1 localhost\n\n# grove-start\n127.0.0.1 api.myapp.localhost\n127.0.0.1 web.myapp.localhost\n# grove-end\n"
	lines := extractBlock(content)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "127.0.0.1 api.myapp.localhost" {
		t.Errorf("line 0: got %q", lines[0])
	}
}

func TestExtractBlock_NoBlock(t *testing.T) {
	lines := extractBlock("127.0.0.1 localhost\n")
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestRemoveBlock(t *testing.T) {
	content := "127.0.0.1 localhost\n\n# grove-start\n127.0.0.1 api.myapp.localhost\n# grove-end\n\n::1 localhost\n"
	result := removeBlock(content)
	if strings.Contains(result, "grove-start") {
		t.Error("start marker should be removed")
	}
	if strings.Contains(result, "api.myapp") {
		t.Error("managed entry should be removed")
	}
	if !strings.Contains(result, "127.0.0.1 localhost") {
		t.Error("unmanaged entry should be preserved")
	}
	if !strings.Contains(result, "::1 localhost") {
		t.Error("unmanaged entry after block should be preserved")
	}
}

func TestRemoveBlock_NoBlock(t *testing.T) {
	content := "127.0.0.1 localhost\n"
	result := removeBlock(content)
	if result != content {
		t.Errorf("expected unchanged content, got %q", result)
	}
}

func TestSync_WithMock(t *testing.T) {
	// Mock the file system
	var stored string
	origRead := readFile
	origWrite := writeFile
	readFile = func() string { return stored }
	writeFile = func(content string) bool { stored = content; return true }
	t.Cleanup(func() { readFile = origRead; writeFile = origWrite })

	// Initial state
	stored = "127.0.0.1 localhost\n::1 localhost\n"

	// Sync some hostnames
	ok := Sync([]string{"web.myapp.localhost", "api.myapp.localhost"})
	if !ok {
		t.Fatal("Sync returned false")
	}
	if !strings.Contains(stored, markerStart) {
		t.Error("missing start marker")
	}
	if !strings.Contains(stored, "127.0.0.1 api.myapp.localhost") {
		t.Error("missing api entry")
	}
	if !strings.Contains(stored, "127.0.0.1 web.myapp.localhost") {
		t.Error("missing web entry")
	}
	if !strings.Contains(stored, "127.0.0.1 localhost") {
		t.Error("original entries should be preserved")
	}
}

func TestSync_UpdatesExistingBlock(t *testing.T) {
	var stored string
	origRead := readFile
	origWrite := writeFile
	readFile = func() string { return stored }
	writeFile = func(content string) bool { stored = content; return true }
	t.Cleanup(func() { readFile = origRead; writeFile = origWrite })

	stored = "127.0.0.1 localhost\n\n# grove-start\n127.0.0.1 old.myapp.localhost\n# grove-end\n"

	Sync([]string{"new.myapp.localhost"})
	if strings.Contains(stored, "old.myapp") {
		t.Error("old entry should be replaced")
	}
	if !strings.Contains(stored, "127.0.0.1 new.myapp.localhost") {
		t.Error("new entry should be present")
	}
}

func TestSync_EmptyRemovesBlock(t *testing.T) {
	var stored string
	origRead := readFile
	origWrite := writeFile
	readFile = func() string { return stored }
	writeFile = func(content string) bool { stored = content; return true }
	t.Cleanup(func() { readFile = origRead; writeFile = origWrite })

	stored = "127.0.0.1 localhost\n\n# grove-start\n127.0.0.1 api.myapp.localhost\n# grove-end\n"

	Sync([]string{})
	if strings.Contains(stored, "grove-start") {
		t.Error("block should be removed when hostnames is empty")
	}
}

func TestClean_WithMock(t *testing.T) {
	var stored string
	origRead := readFile
	origWrite := writeFile
	readFile = func() string { return stored }
	writeFile = func(content string) bool { stored = content; return true }
	t.Cleanup(func() { readFile = origRead; writeFile = origWrite })

	stored = "127.0.0.1 localhost\n\n# grove-start\n127.0.0.1 api.myapp.localhost\n# grove-end\n"

	ok := Clean()
	if !ok {
		t.Fatal("Clean returned false")
	}
	if strings.Contains(stored, "grove-start") {
		t.Error("block should be removed")
	}
}

func TestManagedHostnames(t *testing.T) {
	origRead := readFile
	readFile = func() string {
		return "127.0.0.1 localhost\n\n# grove-start\n127.0.0.1 api.myapp.localhost\n127.0.0.1 web.myapp.localhost\n# grove-end\n"
	}
	t.Cleanup(func() { readFile = origRead })

	hostnames := ManagedHostnames()
	if len(hostnames) != 2 {
		t.Fatalf("expected 2 hostnames, got %d", len(hostnames))
	}
	if hostnames[0] != "api.myapp.localhost" {
		t.Errorf("hostname 0: got %q", hostnames[0])
	}
	if hostnames[1] != "web.myapp.localhost" {
		t.Errorf("hostname 1: got %q", hostnames[1])
	}
}

func TestNeedsSync_Matching(t *testing.T) {
	origRead := readFile
	readFile = func() string {
		return "# grove-start\n127.0.0.1 api.myapp.localhost\n127.0.0.1 web.myapp.localhost\n# grove-end\n"
	}
	t.Cleanup(func() { readFile = origRead })

	if NeedsSync([]string{"web.myapp.localhost", "api.myapp.localhost"}) {
		t.Error("should not need sync when hostnames match")
	}
}

func TestNeedsSync_Different(t *testing.T) {
	origRead := readFile
	readFile = func() string {
		return "# grove-start\n127.0.0.1 api.myapp.localhost\n# grove-end\n"
	}
	t.Cleanup(func() { readFile = origRead })

	if !NeedsSync([]string{"api.myapp.localhost", "web.myapp.localhost"}) {
		t.Error("should need sync when hostnames differ")
	}
}

func TestNeedsSync_NoBlock(t *testing.T) {
	origRead := readFile
	readFile = func() string { return "127.0.0.1 localhost\n" }
	t.Cleanup(func() { readFile = origRead })

	if !NeedsSync([]string{"api.myapp.localhost"}) {
		t.Error("should need sync when no block exists")
	}
}
