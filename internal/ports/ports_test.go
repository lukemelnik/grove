package ports

import (
	"testing"

	"github.com/lukemelnik/grove/internal/config"
)

func TestHashOffset_Deterministic(t *testing.T) {
	offset1 := HashOffset("feat/auth", DefaultMaxOffset)
	offset2 := HashOffset("feat/auth", DefaultMaxOffset)

	if offset1 != offset2 {
		t.Errorf("expected deterministic hash, got %d and %d", offset1, offset2)
	}
}

func TestHashOffset_DifferentBranches(t *testing.T) {
	offset1 := HashOffset("feat/auth", DefaultMaxOffset)
	offset2 := HashOffset("feat/billing", DefaultMaxOffset)

	if offset1 == offset2 {
		t.Errorf("expected different offsets for different branches, both got %d", offset1)
	}
}

func TestHashOffset_Range(t *testing.T) {
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
		{80, false},
		{443, false},
		{3000, false},
		{4000, false},
		{8080, false},
		{10080, true},
		{50000, false},
	}

	for _, tt := range tests {
		if got := IsPortBlocked(tt.port); got != tt.blocked {
			t.Errorf("IsPortBlocked(%d) = %v, want %v", tt.port, got, tt.blocked)
		}
	}
}

func TestAssign_Basic(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
		"web": {Port: config.ServicePort{Base: 3000, Env: "WEB_PORT"}},
	}

	result, err := Assign(services, "feat/auth", DefaultMaxOffset, "")
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
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
	}

	result1, err := Assign(services, "feat/auth", DefaultMaxOffset, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result2, err := Assign(services, "feat/auth", DefaultMaxOffset, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result1.Ports["api"] != result2.Ports["api"] {
		t.Errorf("expected deterministic ports, got %d and %d", result1.Ports["api"], result2.Ports["api"])
	}
}

func TestAssign_BlockedPortSkipped(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
	}

	if !IsPortBlocked(4045) {
		t.Fatal("4045 should be blocked")
	}

	// Run assignment — the important thing is it doesn't return a blocked port
	result, err := Assign(services, "any-branch", DefaultMaxOffset, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if IsPortBlocked(result.Ports["api"]) {
		t.Errorf("assigned blocked port %d", result.Ports["api"])
	}
}

func TestAssign_PortOverflow(t *testing.T) {
	// Base port near 65535 — offset could push it over
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 65000, Env: "PORT"}},
	}

	result, err := Assign(services, "feat/auth", DefaultMaxOffset, "")
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

	result, err := Assign(services, "feat/auth", DefaultMaxOffset, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Ports) != 0 {
		t.Errorf("expected empty port map, got %d entries", len(result.Ports))
	}
}

func TestAssign_DefaultMaxOffset(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
	}

	// Pass 0 for maxOffset to use default
	result, err := Assign(services, "feat/auth", 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Offset < 0 || result.Offset >= DefaultMaxOffset {
		t.Errorf("expected offset in [0, %d), got %d", DefaultMaxOffset, result.Offset)
	}
}

func TestAssign_ConsistentAcrossCommands(t *testing.T) {
	// Verify that the same branch always gets the same ports,
	// regardless of how many times Assign is called
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
		"web": {Port: config.ServicePort{Base: 3000, Env: "WEB_PORT"}},
	}

	results := make([]*Assignment, 5)
	for i := range results {
		var err error
		results[i], err = Assign(services, "feat/consistent", DefaultMaxOffset, "")
		if err != nil {
			t.Fatalf("attempt %d: unexpected error: %v", i, err)
		}
	}

	for i := 1; i < len(results); i++ {
		if results[i].Ports["api"] != results[0].Ports["api"] {
			t.Errorf("attempt %d: api port %d != %d", i, results[i].Ports["api"], results[0].Ports["api"])
		}
		if results[i].Ports["web"] != results[0].Ports["web"] {
			t.Errorf("attempt %d: web port %d != %d", i, results[i].Ports["web"], results[0].Ports["web"])
		}
	}
}

func TestAssign_DefaultBranchUsesBasePorts(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
		"web": {Port: config.ServicePort{Base: 3000, Env: "WEB_PORT"}},
	}

	result, err := Assign(services, "main", DefaultMaxOffset, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Offset != 0 {
		t.Errorf("expected offset 0 for default branch, got %d", result.Offset)
	}

	if result.Ports["api"] != 4000 {
		t.Errorf("expected api port 4000, got %d", result.Ports["api"])
	}

	if result.Ports["web"] != 3000 {
		t.Errorf("expected web port 3000, got %d", result.Ports["web"])
	}
}

func TestAssign_DefaultBranchMaster(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
	}

	result, err := Assign(services, "master", DefaultMaxOffset, "master")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Offset != 0 {
		t.Errorf("expected offset 0 for default branch, got %d", result.Offset)
	}

	if result.Ports["api"] != 4000 {
		t.Errorf("expected api port 4000, got %d", result.Ports["api"])
	}
}

func TestAssign_NonDefaultBranchStillGetsOffset(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
	}

	result, err := Assign(services, "feat/auth", DefaultMaxOffset, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Offset == 0 {
		t.Error("expected non-zero offset for non-default branch")
	}

	if result.Ports["api"] == 4000 {
		t.Error("expected non-default branch to NOT use base port")
	}
}

func TestAssign_EmptyDefaultBranchFallsBackToHash(t *testing.T) {
	services := map[string]config.Service{
		"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
	}

	// When defaultBranch is empty, "main" should get a hash-based offset (not 0)
	result, err := Assign(services, "main", DefaultMaxOffset, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Offset == 0 {
		t.Error("expected hash-based offset when defaultBranch is empty, got 0")
	}
}

func TestAssign_PortlessServiceSkipped(t *testing.T) {
	services := map[string]config.Service{
		"api":     {Port: config.ServicePort{Base: 4000, Env: "PORT"}},
		"desktop": {EnvFile: "apps/desktop/.env"},
	}

	result, err := Assign(services, "main", DefaultMaxOffset, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.Ports["api"]; !ok {
		t.Error("expected api to have an assigned port")
	}
	if _, ok := result.Ports["desktop"]; ok {
		t.Error("expected desktop (no port block) to be skipped in port assignment")
	}
}
