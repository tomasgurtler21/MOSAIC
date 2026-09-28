package artifact

import (
	"context"
	"fmt"
	"time"

	"mosaic-run/internal/domain"
)

// SetCommitBranch records the commit branch reported by commit setup. It
// re-reads the artifact from disk, sets commit_branch and last_updated, and
// writes atomically. The execution log, registry, current_state,
// global_sequence and notes are unchanged.
//
// It returns a *domain.RefusalError when branch is empty, when commits are
// disabled in the artifact, or when commit_branch is already present.
func (f *fileStore) SetCommitBranch(_ context.Context, branch string, now time.Time) (domain.ArtifactState, error) {
	refuse := func(reason string) error {
		return &domain.RefusalError{Component: "artifact", Resource: f.path, Reason: reason}
	}
	if branch == "" {
		return domain.ArtifactState{}, refuse("cannot record an empty commit branch")
	}

	data, err := readFile(f.path)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("SetCommitBranch: read: %w", err)
	}
	current, err := Parse(data)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("SetCommitBranch: parse: %w", err)
	}
	if !current.Commits {
		return domain.ArtifactState{}, refuse("cannot record a commit branch: commits are disabled in this artifact")
	}
	if current.CommitBranch != "" {
		return domain.ArtifactState{}, refuse(fmt.Sprintf("commit_branch is already set to %q", current.CommitBranch))
	}

	current.CommitBranch = branch
	current.CommitBranchVariant = domain.DeriveCommitBranchVariant(branch, current.RunID)
	current.LastUpdated = now.UTC()

	rendered, err := Render(current)
	if err != nil {
		return domain.ArtifactState{}, fmt.Errorf("SetCommitBranch: render: %w", err)
	}
	if err := atomicWrite(f.path, rendered); err != nil {
		return domain.ArtifactState{}, fmt.Errorf("SetCommitBranch: write: %w", err)
	}
	return current, nil
}
