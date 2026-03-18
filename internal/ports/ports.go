// Package ports implements deterministic port assignment based on branch name hashing.
package ports

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/lukemelnik/grove/internal/config"
)

// DefaultMaxOffset is the default modulus for the port offset hash.
// Keeps assigned ports under 65535 for most base ports.
const DefaultMaxOffset = 3000

// MaxCollisionAttempts is the maximum number of offset increments to try
// when a computed port is unavailable or blocked.
const MaxCollisionAttempts = 100

// HashOffset computes a deterministic offset from a branch name.
// offset = md5(branchName) mod maxOffset
func HashOffset(branchName string, maxOffset int) int {
	hash := md5.Sum([]byte(branchName))
	// Use the first 8 bytes of the hash as a uint64
	n := binary.BigEndian.Uint64(hash[:8])
	return int(n % uint64(maxOffset))
}

// IsPortBlocked returns true if the port is in the browser-restricted list.
func IsPortBlocked(port int) bool {
	return config.IsPortBlocked(port)
}

// Assignment holds the result of port assignment for all services.
type Assignment struct {
	// Ports maps service name to assigned port.
	Ports map[string]int

	// Offset is the final offset used (may differ from initial hash if blocked ports were hit).
	Offset int
}

// Assign computes deterministic port assignments for all services defined in the config.
// It hashes the branch name to produce an offset, then adds that offset to each service's
// base port. If any resulting port is blocked or exceeds 65535, the offset is incremented
// and all ports are recomputed, up to MaxCollisionAttempts times.
//
// The default branch (e.g. "main") uses offset 0, so its services run on the base ports
// defined in the config. All other branches get a hash-based offset.
//
// Port assignment is purely deterministic — the same branch name always produces the
// same ports. No runtime availability checking is performed.
func Assign(services map[string]config.Service, branchName string, maxOffset int, defaultBranch string) (*Assignment, error) {
	if maxOffset <= 0 {
		maxOffset = DefaultMaxOffset
	}

	var baseOffset int
	if defaultBranch != "" && branchName == defaultBranch {
		baseOffset = 0
	} else {
		baseOffset = HashOffset(branchName, maxOffset)
	}

	// Sort service names for deterministic iteration order
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)

	for attempt := 0; attempt < MaxCollisionAttempts; attempt++ {
		offset := (baseOffset + attempt) % maxOffset
		ports := make(map[string]int, len(services))
		valid := true

		for _, name := range names {
			svc := services[name]

			// Skip services without a port block (env-only services)
			if !svc.HasPort() {
				continue
			}

			port := svc.Port.Base + offset

			// Check port is in valid range
			if port > 65535 {
				valid = false
				break
			}

			// Check port is not browser-restricted
			if IsPortBlocked(port) {
				valid = false
				break
			}

			ports[name] = port
		}

		if valid {
			return &Assignment{
				Ports:  ports,
				Offset: offset,
			}, nil
		}
	}

	return nil, fmt.Errorf("could not find available ports after %d attempts for branch %q", MaxCollisionAttempts, branchName)
}
