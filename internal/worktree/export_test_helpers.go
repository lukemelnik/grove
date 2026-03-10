package worktree

// ParseGoneBranchesForTest exports parseGoneBranches for use in tests.
func ParseGoneBranchesForTest(output string) []string {
	return parseGoneBranches(output)
}

// ParseMergedBranchesForTest exports parseMergedBranches for use in tests.
func ParseMergedBranchesForTest(output string) []string {
	return parseMergedBranches(output)
}
