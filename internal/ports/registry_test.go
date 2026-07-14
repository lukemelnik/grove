package ports

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lukemelnik/grove/internal/config"
)

func testServices() map[string]config.Service {
	return map[string]config.Service{"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}}, "web": {Port: config.ServicePort{Base: 3000, Env: "WEB_PORT"}}}
}

func TestRegistryFirstAllocationAndStability(t *testing.T) {
	r := emptyRegistry()
	rec, a, err := Allocate(r, testServices(), "feat/a", "main", 10)
	if err != nil {
		t.Fatal(err)
	}
	r.Branches["feat/a"] = rec
	_, b, err := Allocate(r, testServices(), "feat/b", "main", 10)
	if err != nil {
		t.Fatal(err)
	}
	_, again, err := Allocate(r, testServices(), "feat/a", "main", 10)
	if err != nil {
		t.Fatal(err)
	}
	if again.Offset != a.Offset || again.Ports["api"] != a.Ports["api"] {
		t.Fatal("existing assignment changed")
	}
	if b.Ports["api"] == a.Ports["api"] || b.Ports["web"] == a.Ports["web"] {
		t.Fatal("concrete port collision")
	}
}

func TestRegistryRejectsTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(StorePath(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"version":1,"branches":{}} {"version":1,"branches":{}}`)
	if err := os.WriteFile(StorePath(dir), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(dir).Load(); err == nil {
		t.Fatal("trailing JSON did not fail closed")
	}
}

func TestRegistrySearchesBeyondLegacyCollisionAttempts(t *testing.T) {
	r := emptyRegistry()
	services := map[string]config.Service{"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}}}
	for offset := 0; offset < MaxCollisionAttempts+5; offset++ {
		r.Branches[fmt.Sprintf("taken-%03d", offset)] = BranchRecord{Ports: map[string]int{"api": 4000 + offset}}
	}
	_, a, err := Allocate(r, services, "main", "", MaxCollisionAttempts+10)
	if err != nil {
		t.Fatal(err)
	}
	if a.Offset < MaxCollisionAttempts+5 {
		t.Fatalf("offset = %d, want beyond legacy attempt cap", a.Offset)
	}
}

func TestRegistryDeliberateHashCollision(t *testing.T) {
	r := emptyRegistry()
	services := map[string]config.Service{"api": {Port: config.ServicePort{Base: 4100, Env: "PORT"}}}
	rec, a, err := Allocate(r, services, "one", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	r.Branches["one"] = rec
	_, _, err = Allocate(r, services, "two", "", 1)
	if !errors.Is(err, ErrPortCollision) {
		t.Fatalf("got %v, want collision", err)
	}
	if a.Offset != 0 {
		t.Fatal("expected only offset 0")
	}
}

func TestRegistryConcreteCrossServiceCollision(t *testing.T) {
	services := map[string]config.Service{"a": {Port: config.ServicePort{Base: 4000, Env: "A"}}, "b": {Port: config.ServicePort{Base: 4000, Env: "B"}}}
	_, _, err := Allocate(emptyRegistry(), services, "feat", "", 10)
	if !errors.Is(err, ErrPortCollision) {
		t.Fatalf("got %v, want collision", err)
	}
}

func TestRegistryDefaultCollision(t *testing.T) {
	r := emptyRegistry()
	r.Branches["other"] = BranchRecord{Ports: map[string]int{"api": 4000}}
	_, _, err := Allocate(r, map[string]config.Service{"api": {Port: config.ServicePort{Base: 4000, Env: "PORT"}}}, "main", "main", 10)
	if !errors.Is(err, ErrPortCollision) {
		t.Fatalf("got %v, want default collision", err)
	}
}

func TestRegistryConfigDrift(t *testing.T) {
	r := emptyRegistry()
	rec, _, err := Allocate(r, testServices(), "feat", "main", 10)
	if err != nil {
		t.Fatal(err)
	}
	r.Branches["feat"] = rec
	changed := testServices()
	s := changed["api"]
	s.Port.Base = 5000
	changed["api"] = s
	_, _, err = Allocate(r, changed, "feat", "main", 10)
	if !errors.Is(err, ErrConfigDrift) {
		t.Fatalf("got %v, want drift", err)
	}
}

func TestStoreReleaseReconcileMalformedAndAtomicFailure(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if _, err := s.AssignDurable(testServices(), "feat/a", "main", 10); err != nil {
		t.Fatal(err)
	}
	if err := s.Release("feat/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(StorePath(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignDurable(testServices(), "feat/a", "main", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignDurable(testServices(), "feat/b", "main", 10); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile([]string{"feat/b"}); err != nil {
		t.Fatal(err)
	}
	r, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Branches["feat/a"]; ok {
		t.Fatal("inactive branch preserved")
	}
	badDir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(StorePath(badDir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StorePath(badDir), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(badDir).Load(); err == nil {
		t.Fatal("malformed registry did not fail")
	}
	fail := newStoreWithWriter(dir, failingWriter{})
	before, _ := os.ReadFile(StorePath(dir))
	if err := fail.Save(&Registry{Branches: map[string]BranchRecord{"x": {Branch: "x"}}}); err == nil {
		t.Fatal("expected failure")
	}
	after, _ := os.ReadFile(StorePath(dir))
	if string(before) != string(after) {
		t.Fatal("failed atomic write changed existing file")
	}
}

type failingWriter struct{}

func (failingWriter) WriteFileAtomic(string, []byte, os.FileMode) error { return errors.New("boom") }
