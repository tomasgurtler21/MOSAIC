package session

// Unit tests for the buildActiveAgentsFilter and hasCheckpointClassAgent
// pure functions.
//
// These tests use package session (internal) rather than session_test to access
// the unexported functions.
//
// buildActiveAgentsFilter coverage:
//
//   Empty declared set:
//   - Empty declared agents → nil (no filtering needed).
//
//   Single agent per gated class:
//   - Single checkpoint agent, no selection → nil (auto-selected, no filtering needed).
//   - Single checkpoint + single commit, no selection → nil.
//   - Single restore agent, no selection → nil (restore is a gated class).
//
//   Multiple agents of same gated class:
//   - Two checkpoint agents, selection provided → non-nil map with only selected.
//   - Non-selected checkpoint agent excluded from filter.
//   - Two commit agents, selection provided → non-nil map with only selected.
//   - Two restore agents, selection provided → non-nil map with only selected.
//   - Non-selected restore agent excluded from filter.
//
//   Non-gated (review) class:
//   - Review agents are included unconditionally whenever filter is non-nil.
//   - Two review agents, no gated agents → nil (no filtering needed).
//
//   Mixed gated and non-gated:
//   - Two checkpoints + one review, checkpoint selected → selected checkpoint +
//     review agent included; non-selected checkpoint excluded.
//
// hasCheckpointClassAgent coverage:
//
//   - Empty/nil declared agents → false.
//   - Only restore-class agents → false (restore does not satisfy checkpoint precondition).
//   - Checkpoint-class agent present → true.
//   - Both restore and checkpoint agents present → true (checkpoint agent satisfies check).

import (
	"testing"

	"mosaic-run/internal/domain"
)

// TestBuildActiveAgentsFilter_NilDeclared_ReturnsNil verifies that when the
// declared infrastructure agent slice is nil, buildActiveAgentsFilter returns
// nil (no filtering needed for an empty deployment).
func TestBuildActiveAgentsFilter_NilDeclared_ReturnsNil(t *testing.T) {
	result := buildActiveAgentsFilter(nil, nil)
	if result != nil {
		t.Errorf("want nil (empty declared set, no filtering needed), got %v", result)
	}
}

// TestBuildActiveAgentsFilter_EmptyDeclared_ReturnsNil verifies that when the
// declared infrastructure agent slice is empty, buildActiveAgentsFilter returns
// nil.
func TestBuildActiveAgentsFilter_EmptyDeclared_ReturnsNil(t *testing.T) {
	result := buildActiveAgentsFilter([]domain.DeclaredInfraAgent{}, nil)
	if result != nil {
		t.Errorf("want nil (empty declared slice, no filtering needed), got %v", result)
	}
}

// TestBuildActiveAgentsFilter_SingleCheckpointAgent_ReturnsNil verifies that
// when exactly one checkpoint-class agent is declared, buildActiveAgentsFilter
// returns nil — the single agent is auto-selected and no filter is needed.
func TestBuildActiveAgentsFilter_SingleCheckpointAgent_ReturnsNil(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
	}
	result := buildActiveAgentsFilter(declared, nil)
	if result != nil {
		t.Errorf("want nil (single checkpoint agent, auto-selected, no filtering needed), got %v", result)
	}
}

// TestBuildActiveAgentsFilter_SingleAgentPerGatedClass_ReturnsNil verifies
// that when each gated class (checkpoint, commit) has at most one declared
// agent, no filter is needed and the function returns nil.
func TestBuildActiveAgentsFilter_SingleAgentPerGatedClass_ReturnsNil(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
		{Name: "commit-manager-git", Class: "commit"},
	}
	result := buildActiveAgentsFilter(declared, nil)
	if result != nil {
		t.Errorf("want nil (single agent per gated class, no filtering needed), got %v", result)
	}
}

// TestBuildActiveAgentsFilter_TwoCheckpointAgents_SelectedIncluded verifies that
// when two checkpoint-class agents are declared and a selection is provided, the
// returned filter contains the selected agent with value true.
func TestBuildActiveAgentsFilter_TwoCheckpointAgents_SelectedIncluded(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
		{Name: "checkpoint-manager-alt", Class: "checkpoint"},
	}
	selections := map[string]string{"checkpoint": "checkpoint-manager-git"}

	result := buildActiveAgentsFilter(declared, selections)

	if result == nil {
		t.Fatal("want non-nil filter (multiple checkpoint agents require selection filtering), got nil")
	}
	if !result["checkpoint-manager-git"] {
		t.Errorf("want checkpoint-manager-git included in filter (selected agent), got absent")
	}
}

// TestBuildActiveAgentsFilter_TwoCheckpointAgents_NonSelectedExcluded verifies
// that when a selection is provided for a gated class, the non-selected agent
// of that class is absent from (or false in) the returned filter map.
func TestBuildActiveAgentsFilter_TwoCheckpointAgents_NonSelectedExcluded(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
		{Name: "checkpoint-manager-alt", Class: "checkpoint"},
	}
	selections := map[string]string{"checkpoint": "checkpoint-manager-git"}

	result := buildActiveAgentsFilter(declared, selections)

	if result == nil {
		t.Fatal("want non-nil filter (multiple checkpoint agents require selection filtering), got nil")
	}
	if result["checkpoint-manager-alt"] {
		t.Errorf("want checkpoint-manager-alt excluded from filter (not the selected agent), got included")
	}
}

// TestBuildActiveAgentsFilter_TwoCommitAgents_SelectedIncluded verifies that the
// selection filter works for commit-class agents the same way it does for
// checkpoint-class agents (commit is also a gated class).
func TestBuildActiveAgentsFilter_TwoCommitAgents_SelectedIncluded(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "commit-manager-git", Class: "commit"},
		{Name: "commit-manager-alt", Class: "commit"},
	}
	selections := map[string]string{"commit": "commit-manager-git"}

	result := buildActiveAgentsFilter(declared, selections)

	if result == nil {
		t.Fatal("want non-nil filter (multiple commit agents require selection filtering), got nil")
	}
	if !result["commit-manager-git"] {
		t.Errorf("want commit-manager-git included in filter (selected agent), got absent")
	}
	if result["commit-manager-alt"] {
		t.Errorf("want commit-manager-alt excluded from filter (not selected), got included")
	}
}

// TestBuildActiveAgentsFilter_OnlyReviewAgents_ReturnsNil verifies that when all
// declared agents are non-gated (review class), no filter is needed and the
// function returns nil. Review agents are never gated; the "at most one" rule
// applies only to checkpoint and commit classes.
func TestBuildActiveAgentsFilter_OnlyReviewAgents_ReturnsNil(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "review-agent-a", Class: "review"},
		{Name: "review-agent-b", Class: "review"},
	}
	result := buildActiveAgentsFilter(declared, nil)
	if result != nil {
		t.Errorf("want nil (all declared agents are non-gated review class, no filtering needed), got %v", result)
	}
}

// TestBuildActiveAgentsFilter_ReviewIncludedWhenFilterNonNil verifies that when
// the filter is non-nil (because multiple gated-class agents require selection
// filtering), review-class agents are unconditionally included in the map. If
// review agents were absent from a non-nil activeAgents map, evaluateTriggers
// would skip them — incorrect behaviour.
func TestBuildActiveAgentsFilter_ReviewIncludedWhenFilterNonNil(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
		{Name: "checkpoint-manager-alt", Class: "checkpoint"},
		{Name: "review-agent", Class: "review"},
	}
	selections := map[string]string{"checkpoint": "checkpoint-manager-git"}

	result := buildActiveAgentsFilter(declared, selections)

	if result == nil {
		t.Fatal("want non-nil filter (multiple checkpoint agents present), got nil")
	}
	if !result["review-agent"] {
		t.Errorf("want review-agent unconditionally included in non-nil filter (non-gated class), got absent")
	}
}

// TestBuildActiveAgentsFilter_InvalidSelectionName_NonExistentAgentAbsent verifies
// that when the selections map names an agent that does not exist among the declared
// agents for that class, the function does not panic and does not include the
// non-existent agent in any returned map. This pins the contract for callers that
// pass a stale or typo'd selection: the function must behave safely.
//
// The design contract specifies that the caller (the session) is responsible for
// validation, so the function is not required to return an error. It must simply
// not panic and must not include the non-existent name in the result.
//
// This test passes in both RED and GREEN: in RED the stub returns nil (the
// non-existent agent is trivially absent from nil). In GREEN the implementation
// must also ensure the non-existent agent does not appear in any returned map.
func TestBuildActiveAgentsFilter_InvalidSelectionName_NonExistentAgentAbsent(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
		{Name: "checkpoint-manager-alt", Class: "checkpoint"},
	}
	selections := map[string]string{"checkpoint": "no-such-agent"}

	// Must not panic.
	result := buildActiveAgentsFilter(declared, selections)

	// Whether nil or non-nil, the non-existent agent must not appear in the map.
	if result != nil && result["no-such-agent"] {
		t.Errorf("non-existent agent %q must not appear in the active-agents filter map", "no-such-agent")
	}
}

// TestBuildActiveAgentsFilter_MixedGatedAndNonGated_CorrectFilter verifies the
// combined case: two checkpoint agents (gated, filtered by selection) plus a
// review agent (non-gated, always active). Only the selected checkpoint agent
// and the review agent should appear in the filter.
func TestBuildActiveAgentsFilter_MixedGatedAndNonGated_CorrectFilter(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
		{Name: "checkpoint-manager-alt", Class: "checkpoint"},
		{Name: "review-agent", Class: "review"},
	}
	selections := map[string]string{"checkpoint": "checkpoint-manager-alt"}

	result := buildActiveAgentsFilter(declared, selections)

	if result == nil {
		t.Fatal("want non-nil filter (multiple checkpoint agents require selection filtering), got nil")
	}
	// Non-selected checkpoint agent must be excluded.
	if result["checkpoint-manager-git"] {
		t.Errorf("want checkpoint-manager-git excluded (not the selected checkpoint agent), got included")
	}
	// Selected checkpoint agent must be included.
	if !result["checkpoint-manager-alt"] {
		t.Errorf("want checkpoint-manager-alt included (selected for checkpoint class), got absent")
	}
	// Review agent must always be included.
	if !result["review-agent"] {
		t.Errorf("want review-agent unconditionally included (non-gated class), got absent")
	}
}

// ---------------------------------------------------------------------------
// Restore class as gated class (T8.1b)
// ---------------------------------------------------------------------------

// TestBuildActiveAgentsFilter_SingleRestoreAgent_ReturnsNil verifies that
// when exactly one restore-class agent is declared, buildActiveAgentsFilter
// returns nil — the single agent is auto-selected and no filter is needed.
// This confirms that "restore" is treated as a gated class (same as
// "checkpoint" and "commit"), so single-agent deployments require no prompt.
func TestBuildActiveAgentsFilter_SingleRestoreAgent_ReturnsNil(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-restore-git", Class: "restore"},
	}
	result := buildActiveAgentsFilter(declared, nil)
	if result != nil {
		t.Errorf("want nil (single restore agent, auto-selected, no filtering needed), got %v", result)
	}
}

// TestBuildActiveAgentsFilter_TwoRestoreAgents_SelectedIncluded verifies that
// when two restore-class agents are declared and a selection is provided, the
// returned filter contains the selected agent. This confirms that "restore" is
// a gated class subject to at-most-one-active enforcement, so the selection
// filter applies to restore agents the same way it does to checkpoint and commit.
func TestBuildActiveAgentsFilter_TwoRestoreAgents_SelectedIncluded(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-restore-git", Class: "restore"},
		{Name: "checkpoint-restore-s3", Class: "restore"},
	}
	selections := map[string]string{"restore": "checkpoint-restore-git"}

	result := buildActiveAgentsFilter(declared, selections)

	if result == nil {
		t.Fatal("want non-nil filter (multiple restore agents require selection filtering), got nil")
	}
	if !result["checkpoint-restore-git"] {
		t.Errorf("want checkpoint-restore-git included in filter (selected restore agent), got absent")
	}
}

// TestBuildActiveAgentsFilter_TwoRestoreAgents_NonSelectedExcluded verifies
// that when a selection is provided for the restore gated class, the
// non-selected restore agent is absent from the returned filter map.
func TestBuildActiveAgentsFilter_TwoRestoreAgents_NonSelectedExcluded(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-restore-git", Class: "restore"},
		{Name: "checkpoint-restore-s3", Class: "restore"},
	}
	selections := map[string]string{"restore": "checkpoint-restore-git"}

	result := buildActiveAgentsFilter(declared, selections)

	if result == nil {
		t.Fatal("want non-nil filter (multiple restore agents require selection filtering), got nil")
	}
	if result["checkpoint-restore-s3"] {
		t.Errorf("want checkpoint-restore-s3 excluded from filter (not the selected restore agent), got included")
	}
}

// ---------------------------------------------------------------------------
// hasCheckpointClassAgent (T8.1c)
// ---------------------------------------------------------------------------

// TestHasCheckpointClassAgent_EmptySlice_ReturnsFalse verifies that
// hasCheckpointClassAgent returns false for a nil (empty) declared-agent slice.
func TestHasCheckpointClassAgent_EmptySlice_ReturnsFalse(t *testing.T) {
	result := hasCheckpointClassAgent(nil)
	if result {
		t.Error("want false for nil declared agents, got true")
	}
}

// TestHasCheckpointClassAgent_OnlyRestoreClass_ReturnsFalse verifies that
// hasCheckpointClassAgent returns false when only restore-class agents are
// declared. A restore-class agent does not satisfy the checkpoint precondition:
// Class=="restore" is distinct from Class=="checkpoint". This confirms that
// the false positive (where restore was misclassified as checkpoint and
// incorrectly satisfied the precondition) is eliminated by the distinct class.
func TestHasCheckpointClassAgent_OnlyRestoreClass_ReturnsFalse(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-restore-git", Class: "restore"},
	}
	result := hasCheckpointClassAgent(declared)
	if result {
		t.Errorf("want false (restore-class agent does not satisfy checkpoint precondition), got true")
	}
}

// TestHasCheckpointClassAgent_CheckpointClass_ReturnsTrue verifies that
// hasCheckpointClassAgent returns true when a checkpoint-class agent is declared.
func TestHasCheckpointClassAgent_CheckpointClass_ReturnsTrue(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
	}
	result := hasCheckpointClassAgent(declared)
	if !result {
		t.Error("want true (checkpoint-class agent satisfies checkpoint precondition), got false")
	}
}

// TestHasCheckpointClassAgent_RestoreAndCheckpoint_ReturnsTrue verifies that
// hasCheckpointClassAgent returns true when both restore and checkpoint agents
// are declared — the checkpoint agent satisfies the precondition regardless of
// co-declared restore agents.
func TestHasCheckpointClassAgent_RestoreAndCheckpoint_ReturnsTrue(t *testing.T) {
	declared := []domain.DeclaredInfraAgent{
		{Name: "checkpoint-restore-git", Class: "restore"},
		{Name: "checkpoint-manager-git", Class: "checkpoint"},
	}
	result := hasCheckpointClassAgent(declared)
	if !result {
		t.Error("want true (checkpoint-class agent present alongside restore agent), got false")
	}
}
