package artifact_test

// Tests for fileStore.SetCommitBranch: set-once recording of the commit branch
// reported by commit setup, leaving every other part of the artifact alone.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// commitBranchFixture creates an artifact with commits enabled and one
// recorded infrastructure row (the commit setup row), and returns the store
// and the state returned by the last write.
func commitBranchFixture(t *testing.T, commits bool) (domain.ArtifactStore, domain.ArtifactState) {
	t.Helper()
	store := artifact.NewFileStore(filepath.Join(t.TempDir(), "Orchestration.md"))
	ctx := context.Background()
	settings := domain.RunSettings{Mode: domain.ExecutionModeAuto, Commits: commits}
	state, err := store.Create(ctx, domain.WorkflowInfo{ID: "quick-fix", Version: "1.0"}, "task", settings,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), testRunID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	step := newTestStep(1, "commit-manager-git#1", "", "", domain.StatusSUCCESS, time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), nil)
	step.IsInfrastructure = true
	state, err = store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return store, state
}

func TestSetCommitBranch_RecordsBranchAndPersistsIt(t *testing.T) {
	store, _ := commitBranchFixture(t, true)
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	updated, err := store.SetCommitBranch(ctx, "mosaic/run/"+testRunID, now)

	if err != nil {
		t.Fatalf("SetCommitBranch: unexpected error: %v", err)
	}
	if updated.CommitBranch != "mosaic/run/"+testRunID {
		t.Errorf("returned CommitBranch = %q, want %q", updated.CommitBranch, "mosaic/run/"+testRunID)
	}
	reread, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read after SetCommitBranch: %v", err)
	}
	if reread.CommitBranch != "mosaic/run/"+testRunID {
		t.Errorf("persisted CommitBranch = %q, want %q", reread.CommitBranch, "mosaic/run/"+testRunID)
	}
	if !reread.LastUpdated.Equal(now) {
		t.Errorf("LastUpdated = %v, want %v", reread.LastUpdated, now)
	}
}

func TestSetCommitBranch_LeavesLogSequenceAndCurrentStateUnchanged(t *testing.T) {
	store, before := commitBranchFixture(t, true)
	ctx := context.Background()

	after, err := store.SetCommitBranch(ctx, "feature/mine", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))

	if err != nil {
		t.Fatalf("SetCommitBranch: unexpected error: %v", err)
	}
	if after.GlobalSequence != before.GlobalSequence {
		t.Errorf("GlobalSequence = %d, want unchanged %d", after.GlobalSequence, before.GlobalSequence)
	}
	if len(after.ExecutionLog) != len(before.ExecutionLog) {
		t.Errorf("ExecutionLog has %d rows, want unchanged %d", len(after.ExecutionLog), len(before.ExecutionLog))
	}
	if after.CurrentState != before.CurrentState {
		t.Errorf("CurrentState = %+v, want unchanged %+v", after.CurrentState, before.CurrentState)
	}
}

func TestSetCommitBranch_SecondCall_RefusedAndKeepsFirstBranch(t *testing.T) {
	store, _ := commitBranchFixture(t, true)
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, err := store.SetCommitBranch(ctx, "first-branch", now); err != nil {
		t.Fatalf("first SetCommitBranch: %v", err)
	}

	_, err := store.SetCommitBranch(ctx, "second-branch", now.Add(time.Hour))

	if err == nil {
		t.Fatal("want a refusal when commit_branch is already present, got nil")
	}
	asRefusalError(t, err)
	reread, readErr := store.Read(ctx)
	if readErr != nil {
		t.Fatalf("Read: %v", readErr)
	}
	if reread.CommitBranch != "first-branch" {
		t.Errorf("CommitBranch = %q, want the first value kept", reread.CommitBranch)
	}
}

func TestSetCommitBranch_CommitsDisabled_Refused(t *testing.T) {
	store, _ := commitBranchFixture(t, false)

	_, err := store.SetCommitBranch(context.Background(), "some-branch", time.Now())

	if err == nil {
		t.Fatal("want a refusal when commits are disabled in the artifact, got nil")
	}
	asRefusalError(t, err)
}

func TestSetCommitBranch_EmptyBranch_Refused(t *testing.T) {
	store, _ := commitBranchFixture(t, true)

	_, err := store.SetCommitBranch(context.Background(), "", time.Now())

	if err == nil {
		t.Fatal("want a refusal for an empty branch name, got nil")
	}
	asRefusalError(t, err)
}
