package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const maxDNSLabelLength = 63

func SanitizeBranch(branch string) (string, error) {
	var b strings.Builder
	for _, r := range branch {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}

	label := strings.ToLower(b.String())

	label = collapseHyphens(label)
	label = strings.Trim(label, "-")

	if label == "" {
		return "", fmt.Errorf("branch name %q produces an empty DNS label after sanitization", branch)
	}

	if len(label) > maxDNSLabelLength {
		hash := sha256.Sum256([]byte(branch))
		suffix := hex.EncodeToString(hash[:4])
		truncated := label[:maxDNSLabelLength-len(suffix)-1]
		truncated = strings.TrimRight(truncated, "-")
		label = truncated + "-" + suffix
	}

	return label, nil
}

func collapseHyphens(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range s {
		if r == '-' {
			if !prev {
				b.WriteByte('-')
			}
			prev = true
		} else {
			b.WriteRune(r)
			prev = false
		}
	}
	return b.String()
}

func BuildHostname(service, branch, project, defaultBranch string) (string, error) {
	if defaultBranch != "" && branch == defaultBranch {
		return service + "." + project + ".localhost", nil
	}

	sanitized, err := SanitizeBranch(branch)
	if err != nil {
		return "", err
	}

	return service + "." + sanitized + "." + project + ".localhost", nil
}
