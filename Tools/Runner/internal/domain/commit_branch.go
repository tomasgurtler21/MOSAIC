package domain

import (
	"fmt"
	"strings"
)

// CommitBranchVariant selects which branch a commits-enabled run commits to.
type CommitBranchVariant string

const (
	// CommitBranchMOSAICOwned creates a branch for this run named
	// "mosaic/run/{run_id}". It is the recommended variant: an abandoned
	// attempt is discarded by deleting the branch.
	CommitBranchMOSAICOwned CommitBranchVariant = "mosaic-owned"

	// CommitBranchUserOwn commits to the branch the user is already on. A
	// failed attempt and its undo both stay in that branch's history.
	CommitBranchUserOwn CommitBranchVariant = "user-own"
)

// CommitBranchVariants returns the valid variants in presentation order,
// recommended first.
func CommitBranchVariants() []CommitBranchVariant {
	return []CommitBranchVariant{
		CommitBranchMOSAICOwned,
		CommitBranchUserOwn,
	}
}

// ParseCommitBranchVariant mirrors ParseExecutionMode: "" yields
// CommitBranchMOSAICOwned (the documented default) with a nil error; any other
// unrecognised value yields a *RefusalError naming it and listing the valid values.
func ParseCommitBranchVariant(s string) (CommitBranchVariant, error) {
	switch CommitBranchVariant(s) {
	case CommitBranchVariant(""):
		return CommitBranchMOSAICOwned, nil
	case CommitBranchMOSAICOwned, CommitBranchUserOwn:
		return CommitBranchVariant(s), nil
	}
	variants := CommitBranchVariants()
	names := make([]string, len(variants))
	for i, v := range variants {
		names[i] = string(v)
	}
	return CommitBranchMOSAICOwned, &RefusalError{
		Component: "domain",
		Resource:  s,
		Reason:    fmt.Sprintf("unknown commit branch variant; valid values: %s", strings.Join(names, ", ")),
	}
}

// MOSAICRunBranchName returns the MOSAIC-owned branch name for a run id:
// "mosaic/run/" + runID.
func MOSAICRunBranchName(runID string) string {
	return "mosaic/run/" + runID
}
