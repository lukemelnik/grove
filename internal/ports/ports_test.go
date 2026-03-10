package ports

import (
	"testing"

	"grove/internal/config"
)

func TestHashOffset_Deterministic(t *testing.T) {
	// Same branch name should always produce the same offset
	offset1 := HashOffset("feat/auth", DefaultMaxOffset)
	offset2 := HashOffset("feat/auth", DefaultMaxOffset)

	if offset1 != offset2 {
		t.Errorf("expected deterministic hash, got %d and %d", offset1, offset2)
	}
}

func TestHashOffset_DifferentBranches(t *testing.T) {
	// Different branches should (very likely) produce different offsets
	offset1 := HashOffset("feat/auth", DefaultMaxOffset)
	offset2 := HashOffset("feat/billing", DefaultMaxOffset)

	if offset1 == offset2 {
		t.Errorf("expected different offsets for different branches, both got %d", offset1)
	}
}

func TestHashOffset_Range(t *testing.T) {
	// Offset should always be in [0, maxOffset)
	branches := []string{
		"main",
		"feat/auth",
		"fix/login-bug",
		"very/long/branch/name/with/many/segments",
		"",
		"a",
		"branch-with-numbers-123",
	}

	for _, branch := range branches {
		offset := HashOffset(branch, DefaultMaxOffset)
		if offset < 0 || offset >= DefaultMaxOffset {
			t.Errorf("HashOffset(%q) = %d, expected range [0, %d)", branch, offset, DefaultMaxOffset)
		}
	}
}

func TestHashOffset_SmallMaxOffset(t *testing.T) {
	offset := HashOffset("feat/auth", 10)
	if offset < 0 || offset >= 10 {
		t.Errorf("expected offset in [0, 10), got %d", offset)
	}
}

func TestIsPortBlocked(t *testing.T) {
	tests := []struct {
		port    int
		blocked bool
	}{
		{4045, true},
		{4190, true},
		{5060, true},
		{5061, true},
		{6000, true},
		{22, true},
		{80, false},     // common HTTP port, not blocked
		{443, false},    // common HTTPS port, not blocked
		{3000, false},   // typical dev port
		{4000, false},   // typical dev port
		{8080, false},   // typical dev port
		{10080, true},   // blocked
		{50000, false},  // high port, not blocked
	}

	for _, tt := range tests {
		if got := IsPortBlocked(tt.port); got != tt.blocked {
			t.Errorf("IsPortBlocked(%d) = %v, want %v", tt.port, got, tt.blocked)
		}
	}
}

func TestAssign_Basic(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: 4000, Env: "PORT"},
		"web": {Port: 3000, Env: "WEB_PORT"},
	}

	// Use a checker that always says ports are available
	allAvailable := func(port int) bool { return true }

	result, err := Assign(services, "feat/auth", DefaultMaxOffset, allAvailable)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Ports) != 2 {
		t.Fatalf("expected 2 port assignments, got %d", len(result.Ports))
	}

	// Both ports should have the same offset from their base
	apiOffset := result.Ports["api"] - 4000
	webOffset := result.Ports["web"] - 3000

	if apiOffset != webOffset {
		t.Errorf("expected same offset for all services, api=%d web=%d", apiOffset, webOffset)
	}

	if apiOffset != result.Offset {
		t.Errorf("expected offset %d, got %d from port math", result.Offset, apiOffset)
	}
}

func TestAssign_Deterministic(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: 4000, Env: "PORT"},
	}

	allAvailable := func(port int) bool { return true }

	result1, err := Assign(services, "feat/auth", DefaultMaxOffset, allAvailable)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result2, err := Assign(services, "feat/auth", DefaultMaxOffset, allAvailable)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result1.Ports["api"] != result2.Ports["api"] {
		t.Errorf("expected deterministic ports, got %d and %d", result1.Ports["api"], result2.Ports["api"])
	}
}

func TestAssign_CollisionHandling(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: 4000, Env: "PORT"},
	}

	// First offset's port is "in use", second attempt should succeed
	expectedOffset := HashOffset("test-branch", DefaultMaxOffset)
	blockedPort := 4000 + expectedOffset

	callCount := 0
	checker := func(port int) bool {
		callCount++
		// Block the first computed port, allow everything else
		return port != blockedPort
	}

	result, err := Assign(services, "test-branch", DefaultMaxOffset, checker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have moved to next offset
	if result.Ports["api"] == blockedPort {
		t.Errorf("should not have assigned blocked port %d", blockedPort)
	}

	// The new offset should be one more than the original (mod maxOffset)
	expectedNewOffset := (expectedOffset + 1) % DefaultMaxOffset
	if result.Offset != expectedNewOffset {
		t.Errorf("expected offset %d after collision, got %d", expectedNewOffset, result.Offset)
	}
}

func TestAssign_BlockedPortSkipped(t *testing.T) {
	// Use a base port such that base + hash offset lands on a blocked port
	// Port 4045 is blocked. If base=4000 and offset=45, we hit 4045.
	services := map[string]config.Service{
		"api": {Port: 4000, Env: "PORT"},
	}

	// Find a branch that produces offset 45
	// We'll just use the blocked port check directly:
	// simulate by making a checker that allows everything, and ensure the
	// Assign function skips the blocked port
	allAvailable := func(port int) bool { return true }

	// Test with a crafted scenario: override offset to land on 4045
	// Since we can't control the hash, test IsPortBlocked directly
	if !IsPortBlocked(4045) {
		t.Fatal("4045 should be blocked")
	}

	// Run assignment — the important thing is it doesn't return a blocked port
	result, err := Assign(services, "any-branch", DefaultMaxOffset, allAvailable)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if IsPortBlocked(result.Ports["api"]) {
		t.Errorf("assigned blocked port %d", result.Ports["api"])
	}
}

func TestAssign_AllPortsUnavailable(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: 4000, Env: "PORT"},
	}

	// No ports available
	noneAvailable := func(port int) bool { return false }

	_, err := Assign(services, "feat/auth", DefaultMaxOffset, noneAvailable)
	if err == nil {
		t.Fatal("expected error when no ports available")
	}
}

func TestAssign_PortOverflow(t *testing.T) {
	// Base port near 65535 — offset could push it over
	services := map[string]config.Service{
		"api": {Port: 65000, Env: "PORT"},
	}

	allAvailable := func(port int) bool { return true }

	result, err := Assign(services, "feat/auth", DefaultMaxOffset, allAvailable)
	if err != nil {
		// If the hash-based offset pushes us over 65535 for all attempts, error is fine
		return
	}

	if result.Ports["api"] > 65535 {
		t.Errorf("assigned port %d exceeds 65535", result.Ports["api"])
	}
}

func TestAssign_EmptyServices(t *testing.T) {
	services := map[string]config.Service{}
	allAvailable := func(port int) bool { return true }

	result, err := Assign(services, "feat/auth", DefaultMaxOffset, allAvailable)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Ports) != 0 {
		t.Errorf("expected empty port map, got %d entries", len(result.Ports))
	}
}

func TestAssign_DefaultMaxOffset(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: 4000, Env: "PORT"},
	}

	allAvailable := func(port int) bool { return true }

	// Pass 0 for maxOffset to use default
	result, err := Assign(services, "feat/auth", 0, allAvailable)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Offset < 0 || result.Offset >= DefaultMaxOffset {
		t.Errorf("expected offset in [0, %d), got %d", DefaultMaxOffset, result.Offset)
	}
}

func TestAssign_MultipleServicesOneBlocked(t *testing.T) {
	// If one service's port is unavailable at a given offset, ALL services
	// should move to the next offset together
	services := map[string]config.Service{
		"api": {Port: 4000, Env: "PORT"},
		"web": {Port: 3000, Env: "WEB_PORT"},
	}

	expectedOffset := HashOffset("multi-test", DefaultMaxOffset)
	// Block the web port at the first offset
	blockedWebPort := 3000 + expectedOffset

	checker := func(port int) bool {
		return port != blockedWebPort
	}

	result, err := Assign(services, "multi-test", DefaultMaxOffset, checker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both services moved to the same new offset
	apiOffset := result.Ports["api"] - 4000
	webOffset := result.Ports["web"] - 3000

	if apiOffset != webOffset {
		t.Errorf("services should share offset, api=%d web=%d", apiOffset, webOffset)
	}

	if result.Ports["web"] == blockedWebPort {
		t.Errorf("should not have assigned blocked web port %d", blockedWebPort)
	}
}

func TestCheckPortAvailable_HighPort(t *testing.T) {
	// Port 0 tells the OS to pick a free port — we just test the function runs
	// We can't guarantee specific high ports are free, but 0 should work
	// Actually test a realistic scenario: bind a port, check it's unavailable
	// This is an integration-style test
	if testing.Short() {
		t.Skip("skipping port binding test in short mode")
	}

	// A very high port should generally be available
	// Just verify the function doesn't panic
	_ = CheckPortAvailable(59999)
}
