package ports

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lukemelnik/grove/internal/config"
)

const registryVersion = 1

var (
	// ErrConfigDrift indicates an existing branch record no longer matches service/base configuration.
	ErrConfigDrift = errors.New("port registry config drift")
	// ErrReallocationRequired indicates a saved offset can no longer produce valid concrete ports.
	ErrReallocationRequired = errors.New("port registry repair or reallocation required")
	// ErrPortCollision indicates no non-colliding assignment could be found.
	ErrPortCollision = errors.New("port registry port collision")
)

// Store persists branch port assignments under an explicit Git common-state directory.
type Store struct {
	path string
	fs   atomicWriter
}

// StorePath returns the registry file path for a Git common-state directory.
func StorePath(commonStateDir string) string {
	return filepath.Join(commonStateDir, "grove", "ports.json")
}

// NewStore creates a registry store rooted at an explicit Git common-state directory.
func NewStore(commonStateDir string) *Store {
	return &Store{path: StorePath(commonStateDir), fs: osAtomicWriter{}}
}

func newStoreWithWriter(commonStateDir string, fs atomicWriter) *Store {
	return &Store{path: StorePath(commonStateDir), fs: fs}
}

// Registry is the durable JSON representation of branch port assignments.
type Registry struct {
	Version  int                     `json:"version"`
	Branches map[string]BranchRecord `json:"branches"`
}

// BranchRecord records a branch assignment and the service configuration it was computed from.
type BranchRecord struct {
	Branch        string                   `json:"branch"`
	Offset        int                      `json:"offset"`
	Ports         map[string]int           `json:"ports"`
	Services      map[string]ServiceRecord `json:"services"`
	MaxOffset     int                      `json:"max_offset"`
	DefaultBranch string                   `json:"default_branch,omitempty"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

// ServiceRecord is the non-secret port configuration needed to detect drift.
type ServiceRecord struct {
	Base int    `json:"base"`
	Env  string `json:"env,omitempty"`
}

// Load reads the registry. Missing files return an empty registry; malformed files fail closed.
func (s *Store) Load() (*Registry, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyRegistry(), nil
	}
	if err != nil {
		return nil, err
	}
	var r Registry
	dec := json.NewDecoder(stringReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("load port registry: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing JSON value")
		}
		return nil, fmt.Errorf("load port registry: %w", err)
	}
	if r.Version != registryVersion {
		return nil, fmt.Errorf("load port registry: unsupported version %d", r.Version)
	}
	if r.Branches == nil {
		r.Branches = map[string]BranchRecord{}
	}
	return &r, nil
}

// Save atomically writes the registry using same-directory temp file, fsync, and rename.
func (s *Store) Save(r *Registry) error {
	if r == nil {
		r = emptyRegistry()
	}
	r.Version = registryVersion
	if r.Branches == nil {
		r.Branches = map[string]BranchRecord{}
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return s.fs.WriteFileAtomic(s.path, b, 0o644)
}

// AssignDurable loads, allocates or validates a branch assignment, and saves the registry.
func (s *Store) AssignDurable(services map[string]config.Service, branch, defaultBranch string, maxOffset int) (*Assignment, error) {
	r, err := s.Load()
	if err != nil {
		return nil, err
	}
	rec, a, err := Allocate(r, services, branch, defaultBranch, maxOffset)
	if err != nil {
		return nil, err
	}
	r.Branches[branch] = rec
	if err := s.Save(r); err != nil {
		return nil, err
	}
	return a, nil
}

// Allocate returns a collision-free assignment without doing any locking or IO.
func Allocate(r *Registry, services map[string]config.Service, branch, defaultBranch string, maxOffset int) (BranchRecord, *Assignment, error) {
	if maxOffset <= 0 {
		maxOffset = DefaultMaxOffset
	}
	if r == nil {
		r = emptyRegistry()
	}
	if r.Branches == nil {
		r.Branches = map[string]BranchRecord{}
	}
	sig := serviceSignature(services)
	if existing, ok := r.Branches[branch]; ok {
		if existing.MaxOffset != maxOffset || existing.DefaultBranch != defaultBranch || !sameServices(existing.Services, sig) {
			return BranchRecord{}, nil, fmt.Errorf("%w for branch %q", ErrConfigDrift, branch)
		}
		ports, ok := concretePorts(services, existing.Offset)
		if !ok || !samePorts(ports, existing.Ports) || !validAgainstRegistry(r, branch, ports) {
			return BranchRecord{}, nil, fmt.Errorf("%w for branch %q", ErrReallocationRequired, branch)
		}
		existing.UpdatedAt = time.Now().UTC()
		return existing, &Assignment{Ports: ports, Offset: existing.Offset}, nil
	}
	baseOffset := 0
	if !(defaultBranch != "" && branch == defaultBranch) {
		baseOffset = HashOffset(branch, maxOffset)
	}
	attempts := maxOffset
	if defaultBranch != "" && branch == defaultBranch {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		offset := (baseOffset + i) % maxOffset
		ports, ok := concretePorts(services, offset)
		if !ok {
			continue
		}
		if validAgainstRegistry(r, branch, ports) {
			rec := BranchRecord{Branch: branch, Offset: offset, Ports: ports, Services: sig, MaxOffset: maxOffset, DefaultBranch: defaultBranch, UpdatedAt: time.Now().UTC()}
			return rec, &Assignment{Ports: ports, Offset: offset}, nil
		}
	}
	return BranchRecord{}, nil, fmt.Errorf("%w for branch %q", ErrPortCollision, branch)
}

// Release removes one branch record, if present.
func (s *Store) Release(branch string) error {
	r, err := s.Load()
	if err != nil {
		return err
	}
	delete(r.Branches, branch)
	return s.Save(r)
}

// Reconcile removes records whose branch is not in activeBranches.
func (s *Store) Reconcile(activeBranches []string) error {
	r, err := s.Load()
	if err != nil {
		return err
	}
	active := map[string]bool{}
	for _, b := range activeBranches {
		active[b] = true
	}
	for b := range r.Branches {
		if !active[b] {
			delete(r.Branches, b)
		}
	}
	return s.Save(r)
}

func emptyRegistry() *Registry {
	return &Registry{Version: registryVersion, Branches: map[string]BranchRecord{}}
}

func serviceSignature(services map[string]config.Service) map[string]ServiceRecord {
	out := map[string]ServiceRecord{}
	for name, svc := range services {
		if svc.HasPort() {
			out[name] = ServiceRecord{Base: svc.Port.Base, Env: svc.Port.Env}
		}
	}
	return out
}

func concretePorts(services map[string]config.Service, offset int) (map[string]int, bool) {
	names := make([]string, 0, len(services))
	for n := range services {
		names = append(names, n)
	}
	sort.Strings(names)
	ports := map[string]int{}
	seen := map[int]string{}
	for _, name := range names {
		svc := services[name]
		if !svc.HasPort() {
			continue
		}
		p := svc.Port.Base + offset
		if p <= 0 || p > 65535 || IsPortBlocked(p) {
			return nil, false
		}
		if seen[p] != "" {
			return nil, false
		}
		seen[p] = name
		ports[name] = p
	}
	return ports, true
}

func validAgainstRegistry(r *Registry, branch string, ports map[string]int) bool {
	used := map[int]string{}
	for b, rec := range r.Branches {
		if b != branch {
			for _, p := range rec.Ports {
				used[p] = b
			}
		}
	}
	for _, p := range ports {
		if used[p] != "" {
			return false
		}
	}
	return true
}

func sameServices(a, b map[string]ServiceRecord) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if b[k] != av {
			return false
		}
	}
	return true
}
func samePorts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if b[k] != av {
			return false
		}
	}
	return true
}

type byteReader []byte

func stringReader(b []byte) io.Reader { return (*byteReader)(&b) }
func (r *byteReader) Read(p []byte) (int, error) {
	if len(*r) == 0 {
		return 0, io.EOF
	}
	n := copy(p, *r)
	*r = (*r)[n:]
	return n, nil
}

type atomicWriter interface {
	WriteFileAtomic(path string, data []byte, perm os.FileMode) error
}
type osAtomicWriter struct{}

func (osAtomicWriter) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ports-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	ok = true
	return nil
}
