package artifact_test

// Round-trip tests for Render -> Parse -> re-Parse: configuration fields,
// commits, commit_branch, inputs, and infrastructure overrides.

import (
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// ---- Round-trip (Render -> Parse): configuration fields ----

func TestRoundTrip_AllConfigFields_ParsedBack(t *testing.T) {
	// All six configuration fields must survive a Render → Parse round-trip
	// and be read back with their original values.
	original := domain.ArtifactState{
		Type:            "orchestration-artifact",
		Workflow:        "test",
		WorkflowVersion: "1.0",
		Task:            "round-trip task",
		Started:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LastUpdated:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RunSettings: domain.RunSettings{
			Mode:                domain.ExecutionModeAutoReview,
			Checkpoints:         true,
			Commits:             true,
			CommitBranchVariant: domain.CommitBranchUserOwn,
			CommitBranch:        "mosaic/run/20260816T000724Z-3b68",
			PreConsultation:     true,
			ManualResolution:    true,
		},
	}

	rendered, err := artifact.Render(original)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	parsed, err := artifact.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse rendered bytes: unexpected error: %v", err)
	}

	if parsed.Mode != original.Mode {
		t.Errorf("Mode: want %q, got %q", original.Mode, parsed.Mode)
	}
	if parsed.Commits != original.Commits {
		t.Errorf("Commits: want %v, got %v", original.Commits, parsed.Commits)
	}
	if parsed.CommitBranchVariant != original.CommitBranchVariant {
		t.Errorf("CommitBranchVariant: want %q, got %q",
			original.CommitBranchVariant, parsed.CommitBranchVariant)
	}
	if parsed.CommitBranch != original.CommitBranch {
		t.Errorf("CommitBranch: want %q, got %q", original.CommitBranch, parsed.CommitBranch)
	}
	if parsed.PreConsultation != original.PreConsultation {
		t.Errorf("PreConsultation: want %v, got %v", original.PreConsultation, parsed.PreConsultation)
	}
	if parsed.ManualResolution != original.ManualResolution {
		t.Errorf("ManualResolution: want %v, got %v", original.ManualResolution, parsed.ManualResolution)
	}
}

func TestRoundTrip_CommitsDisabled_CommitBranchVariantPreservedAsEmpty(t *testing.T) {
	// Render an ArtifactState with Commits=false and empty CommitBranchVariant,
	// then Parse the output back. CommitBranchVariant must come back as empty
	// (not defaulted to mosaic-owned). This validates the resumed-run scenario
	// where a commits-disabled orchestration file is re-parsed.
	// RED: current Parse defaults absent key to mosaic-owned regardless of Commits.
	original := domain.ArtifactState{
		Type:     "orchestration-artifact",
		Workflow: "test",
		RunSettings: domain.RunSettings{
			Commits:             false,
			CommitBranchVariant: "",
		},
	}

	rendered, err := artifact.Render(original)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	parsed, err := artifact.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse rendered bytes: unexpected error: %v", err)
	}

	if parsed.CommitBranchVariant != "" {
		t.Errorf("CommitBranchVariant after round-trip: want %q (empty, commits disabled), got %q",
			"", parsed.CommitBranchVariant)
	}
	if parsed.Commits {
		t.Error("Commits after round-trip: want false, got true")
	}
}

func TestRoundTrip_CommitBranch_Empty_SurvivesRoundTrip(t *testing.T) {
	// commit_branch is omitted when empty; re-parsing must yield "" not an error.
	original := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{CommitBranch: ""},
	}

	rendered, err := artifact.Render(original)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	parsed, err := artifact.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse rendered bytes: unexpected error: %v", err)
	}

	if parsed.CommitBranch != "" {
		t.Errorf("CommitBranch: want %q (empty) after round-trip, got %q", "", parsed.CommitBranch)
	}
}

func TestRoundTrip_InputsValue_Preserved(t *testing.T) {
	const wantInputs = "Plan.md, Design.md"
	original := minimalArtifactWithExecutionRow(wantInputs)

	state, err := artifact.Parse(original)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	rendered, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	state2, err := artifact.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse rendered bytes: %v", err)
	}

	if len(state2.ExecutionLog) == 0 {
		t.Fatal("ExecutionLog after round-trip: want at least 1 row, got 0")
	}
	if state2.ExecutionLog[0].Inputs != wantInputs {
		t.Errorf("ExecutionLog[0].Inputs after round-trip: want %q, got %q", wantInputs, state2.ExecutionLog[0].Inputs)
	}
}

func TestRoundTrip_InfrastructureOverrides_AgentNamePreserved(t *testing.T) {
	const wantAgent = "checkpoint-manager-git"
	data := minimalArtifactWithSingleOverrideBytes()

	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Verify parse produced the expected override before exercising the
	// round-trip. A failure here is an artifact of the RED phase, not a
	// round-trip bug.
	if len(state.InfrastructureOverrides) != 1 {
		t.Fatalf("InfrastructureOverrides count: want 1, got %d (parser not yet implemented)", len(state.InfrastructureOverrides))
	}

	rendered, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	state2, err := artifact.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse rendered bytes: %v", err)
	}

	if len(state2.InfrastructureOverrides) != 1 {
		t.Errorf("InfrastructureOverrides count after round-trip: want 1, got %d", len(state2.InfrastructureOverrides))
	}
	if len(state2.InfrastructureOverrides) > 0 && state2.InfrastructureOverrides[0].AgentName != wantAgent {
		t.Errorf("InfrastructureOverrides[0].AgentName after round-trip: want %q, got %q",
			wantAgent, state2.InfrastructureOverrides[0].AgentName)
	}
}
