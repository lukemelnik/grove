// Package hosts manages a grove-owned block in /etc/hosts that maps
// *.localhost hostnames to 127.0.0.1.
//
// Chrome, Firefox, and Edge resolve *.localhost to loopback automatically
// (RFC 6761). Safari does not. This package keeps /etc/hosts in sync so
// that all browsers and CLI tools work.
//
// The managed block is delimited by markers:
//
//	# grove-start
//	127.0.0.1 api.myapp.localhost
//	127.0.0.1 web.myapp.localhost
//	# grove-end
//
// Only the block between the markers is touched; the rest of the file
// is left untouched.
package hosts

import (
	"os"
	"sort"
	"strings"
)

const (
	hostsPath   = "/etc/hosts"
	markerStart = "# grove-start"
	markerEnd   = "# grove-end"
)

// Sync updates /etc/hosts so the grove-managed block contains exactly the
// given hostnames, each pointing to 127.0.0.1. If hostnames is empty, any
// existing block is removed.
//
// Returns true on success. Returns false if the write fails (typically
// because the process lacks root privileges).
func Sync(hostnames []string) bool {
	content := readFile()
	cleaned := removeBlock(content)

	var updated string
	if len(hostnames) == 0 {
		updated = cleaned
	} else {
		block := buildBlock(hostnames)
		updated = strings.TrimRight(cleaned, "\n") + "\n\n" + block + "\n"
	}

	return writeFile(updated)
}

// Clean removes the grove-managed block from /etc/hosts.
// Returns true on success.
func Clean() bool {
	content := readFile()
	if !strings.Contains(content, markerStart) {
		return true
	}
	return writeFile(removeBlock(content))
}

// ManagedHostnames returns the hostnames currently in the grove-managed
// block of /etc/hosts.
func ManagedHostnames() []string {
	content := readFile()
	lines := extractBlock(content)
	hostnames := make([]string, 0, len(lines))
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hostnames = append(hostnames, parts[1])
		}
	}
	return hostnames
}

// NeedsSync returns true if the managed block doesn't match the given hostnames.
func NeedsSync(hostnames []string) bool {
	current := ManagedHostnames()
	if len(current) != len(hostnames) {
		return true
	}

	sorted := make([]string, len(hostnames))
	copy(sorted, hostnames)
	sort.Strings(sorted)

	currentSorted := make([]string, len(current))
	copy(currentSorted, current)
	sort.Strings(currentSorted)

	for i := range sorted {
		if sorted[i] != currentSorted[i] {
			return true
		}
	}
	return false
}

func buildBlock(hostnames []string) string {
	sorted := make([]string, len(hostnames))
	copy(sorted, hostnames)
	sort.Strings(sorted)

	var sb strings.Builder
	sb.WriteString(markerStart)
	sb.WriteByte('\n')
	for _, h := range sorted {
		sb.WriteString("127.0.0.1 ")
		sb.WriteString(h)
		sb.WriteByte('\n')
	}
	sb.WriteString(markerEnd)
	return sb.String()
}

func extractBlock(content string) []string {
	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return nil
	}
	block := content[startIdx+len(markerStart) : endIdx]
	var lines []string
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func removeBlock(content string) string {
	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)
	if startIdx == -1 || endIdx == -1 {
		return content
	}
	before := content[:startIdx]
	after := content[endIdx+len(markerEnd):]
	// Collapse excess blank lines at the join
	result := before + after
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return strings.TrimRight(result, "\n") + "\n"
}

// readFile and writeFile are package-level variables so tests can override them.
var readFile = func() string {
	data, err := os.ReadFile(hostsPath)
	if err != nil {
		return ""
	}
	return string(data)
}

var writeFile = func(content string) bool {
	err := os.WriteFile(hostsPath, []byte(content), 0644)
	return err == nil
}
