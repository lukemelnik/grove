// Package ports implements deterministic port assignment based on branch name hashing.
package ports

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net"
	"sort"

	"grove/internal/config"
)

// DefaultMaxOffset is the default modulus for the port offset hash.
// Keeps assigned ports under 65535 for most base ports.
const DefaultMaxOffset = 3000

// MaxCollisionAttempts is the maximum number of offset increments to try
// when a computed port is unavailable or blocked.
const MaxCollisionAttempts = 100

// blockedPorts contains browser-restricted ports that should never be assigned.
// See https://fetch.spec.whatwg.org/#bad-port (Chromium/Firefox blocked ports).
var blockedPorts = map[int]bool{
	1:     true,
	7:     true,
	9:     true,
	11:    true,
	13:    true,
	15:    true,
	17:    true,
	19:    true,
	20:    true,
	21:    true,
	22:    true,
	23:    true,
	25:    true,
	37:    true,
	42:    true,
	43:    true,
	53:    true,
	77:    true,
	79:    true,
	87:    true,
	95:    true,
	101:   true,
	102:   true,
	103:   true,
	104:   true,
	109:   true,
	110:   true,
	111:   true,
	113:   true,
	115:   true,
	117:   true,
	119:   true,
	123:   true,
	135:   true,
	139:   true,
	143:   true,
	179:   true,
	389:   true,
	427:   true,
	465:   true,
	512:   true,
	513:   true,
	514:   true,
	515:   true,
	526:   true,
	530:   true,
	531:   true,
	532:   true,
	540:   true,
	548:   true,
	556:   true,
	563:   true,
	587:   true,
	601:   true,
	636:   true,
	993:   true,
	995:   true,
	2049:  true,
	3659:  true,
	4045:  true,
	4190:  true,
	5060:  true,
	5061:  true,
	6000:  true,
	6566:  true,
	6665:  true,
	6666:  true,
	6667:  true,
	6668:  true,
	6669:  true,
	6697:  true,
	10080: true,
}

// PortChecker is a function that checks whether a port is available.
// Returns true if the port is available, false otherwise.
type PortChecker func(port int) bool

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
	return blockedPorts[port]
}

// CheckPortAvailable attempts to bind to the given TCP port on localhost.
// Returns true if the port is available, false if it is in use.
func CheckPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// Assignment holds the result of port assignment for all services.
type Assignment struct {
	// Ports maps service name to assigned port.
	Ports map[string]int

	// Offset is the final offset used (may differ from initial hash if collisions occurred).
	Offset int
}

// Assign computes deterministic port assignments for all services defined in the config.
// It hashes the branch name to produce an offset, then adds that offset to each service's
// base port. If any resulting port is blocked or unavailable, the offset is incremented
// and all ports are recomputed, up to MaxCollisionAttempts times.
//
// The checker parameter controls port availability checking. Pass nil to use the
// default CheckPortAvailable (real TCP bind check). Pass a custom function for testing.
func Assign(services map[string]config.Service, branchName string, maxOffset int, checker PortChecker) (*Assignment, error) {
	if maxOffset <= 0 {
		maxOffset = DefaultMaxOffset
	}
	if checker == nil {
		checker = CheckPortAvailable
	}

	baseOffset := HashOffset(branchName, maxOffset)

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
			port := svc.Port + offset

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

			// Check port availability
			if !checker(port) {
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
