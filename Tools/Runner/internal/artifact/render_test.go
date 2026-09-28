package artifact_test

// Tests for artifact.Render, artifact.TruncateSummary, and round-trip
// (Parse -> Render -> re-Parse) behavior.

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// ---- Round-trip ----

func TestRoundTrip_CanonicalFile_IdenticalBytes(t *testing.T) {
	// Parse the canonical fixture, render it back to bytes, and verify that
	// the result is byte-identical to the original file.
	//
	// This test verifies that:
	// - Parse reads all fields correctly.
	// - Render produces exactly the same bytes for an unmodified state.
	// - No content is silently dropped or reformatted.
	original, err := os.ReadFile(fixturePath("canonical.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	state, err := artifact.Parse(original)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !bytes.Equal(original, got) {
		// Show a human-readable diff.
		origLines := strings.Split(string(original), "\n")
		gotLines := strings.Split(string(got), "\n")
		for i := 0; i < len(origLines) || i < len(gotLines); i++ {
			var origLine, gotLine string
			if i < len(origLines) {
				origLine = origLines[i]
			}
			if i < len(gotLines) {
				gotLine = gotLines[i]
			}
			if origLine != gotLine {
				t.Errorf("line %d differs:\n  original: %q\n       got: %q", i+1, origLine, gotLine)
				if i > 3 {
					t.Log("... (further differences omitted)")
					break
				}
			}
		}
		t.Fail()
	}
}

// ---- TruncateSummary ----

func TestTruncateSummary_ShortMessage_ReturnedUnchanged(t *testing.T) {
	input := "Short message"
	got := artifact.TruncateSummary(input)
	if got != input {
		t.Errorf("TruncateSummary(%q): want unchanged %q, got %q", input, input, got)
	}
}

func TestTruncateSummary_ExactlyHundredChars_NotTruncated(t *testing.T) {
	input := strings.Repeat("a", 100)
	got := artifact.TruncateSummary(input)
	if got != input {
		t.Errorf("TruncateSummary(100 chars): want unchanged (100 chars), got %d chars", len(got))
	}
}

func TestTruncateSummary_LongMessage_HeadFiftyEllipsisTailFifty(t *testing.T) {
	// Messages longer than 100 characters are truncated to head-50 + " … " + tail-50.
	// This ensures verbose messages retain both the opening context and final conclusion.
	head := strings.Repeat("h", 50)
	tail := strings.Repeat("t", 50)
	input := head + strings.Repeat("m", 20) + tail // 120 chars total

	got := artifact.TruncateSummary(input)

	want := head + " … " + tail
	if got != want {
		t.Errorf("TruncateSummary(120 chars):\n  want: %q\n   got: %q", want, got)
	}
}

func TestTruncateSummary_PipeCharacter_Stripped(t *testing.T) {
	// Pipe characters must be stripped because they would break the markdown table.
	input := "status: OK | count: 5"
	got := artifact.TruncateSummary(input)
	if strings.Contains(got, "|") {
		t.Errorf("TruncateSummary(%q): result must not contain pipe, got %q", input, got)
	}
}

func TestTruncateSummary_Newline_Stripped(t *testing.T) {
	// Newlines must be stripped because multi-line content breaks the markdown table.
	input := "first line\nsecond line"
	got := artifact.TruncateSummary(input)
	if strings.Contains(got, "\n") {
		t.Errorf("TruncateSummary(%q): result must not contain newline, got %q", input, got)
	}
}

func TestTruncateSummary_StripBeforeTruncation_HeadTailFromCleanString(t *testing.T) {
	// Stripping (pipe/newline removal) happens before truncation so that the
	// 50/50 split is counted on the clean string, not the raw input.
	// Input: 100 clean chars preceded by a pipe that would be stripped.
	clean := strings.Repeat("c", 100)
	input := "|" + clean // 101 chars raw, but 100 chars after strip
	got := artifact.TruncateSummary(input)
	// After stripping the pipe, we have exactly 100 chars — no truncation.
	if got != clean {
		t.Errorf("TruncateSummary(%q): want %q (stripped, no truncation), got %q", input, clean, got)
	}
}

// ---- Render: run_id handling ----

func TestRender_WithRunID_EmitsRunIDInFrontmatter(t *testing.T) {
	// When ArtifactState.RunID is non-empty, Render must emit
	// "run_id: <value>" in the frontmatter block.
	const runID = "20260727T170000Z-a3f9"
	state := domain.ArtifactState{
		RunID:    runID,
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	if !strings.Contains(string(got), "run_id: "+runID) {
		t.Errorf("Render: want frontmatter to contain %q, but it was absent.\nOutput:\n%s", "run_id: "+runID, got)
	}
}

func TestRender_RunIDPosition_AfterTypeBeforeWorkflow(t *testing.T) {
	// When run_id is present, it must appear in the frontmatter after "type:"
	// and before "workflow:", matching the field ordering specified in the design.
	const runID = "20260727T170000Z-a3f9"
	state := domain.ArtifactState{
		RunID:    runID,
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	output := string(got)
	typeIdx := strings.Index(output, "type:")
	runIDIdx := strings.Index(output, "run_id:")
	workflowIdx := strings.Index(output, "\nworkflow:")

	if typeIdx < 0 {
		t.Fatal("rendered output is missing the type: field")
	}
	if runIDIdx < 0 {
		t.Fatal("rendered output is missing the run_id: field")
	}
	if workflowIdx < 0 {
		t.Fatal("rendered output is missing the workflow: field")
	}
	if !(typeIdx < runIDIdx && runIDIdx < workflowIdx) {
		t.Errorf("run_id is not positioned after type: and before workflow:. type@%d, run_id@%d, workflow@%d",
			typeIdx, runIDIdx, workflowIdx)
	}
}

func TestRender_EmptyRunID_NotEmittedInFrontmatter(t *testing.T) {
	// When ArtifactState.RunID is empty (pre-v1.8 artifact), Render must NOT
	// emit a run_id line. This preserves backward compatibility: rendering a
	// pre-v1.8 artifact must not add a spurious run_id field.
	state := domain.ArtifactState{
		RunID:    "", // empty
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	if strings.Contains(string(got), "run_id:") {
		t.Errorf("Render: want no run_id: field when RunID is empty, but output contained it.\nOutput:\n%s", got)
	}
}

// ---- Round-trip: run_id ----

func TestRoundTrip_WithRunID_PreservesRunID(t *testing.T) {
	// Parse an artifact that has run_id in its frontmatter, render the resulting
	// state back to bytes, then re-parse. The RunID must survive the round-trip
	// unchanged.
	const runID = "20260727T170000Z-a3f9"
	data := minimalArtifactBytes(runID)

	state, err := artifact.Parse(data)
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

	if state2.RunID != runID {
		t.Errorf("RunID after round-trip: want %q, got %q", runID, state2.RunID)
	}
}

// ---- Render / Parse: commits frontmatter field ----

func TestRender_ContainsCommitsDisabled(t *testing.T) {
	// Render must emit "commits: disabled" in the frontmatter when Commits is
	// false (the zero value). The commits: field reflects the actual
	// RunSettings.Commits value; a separate test (TestRender_CommitsEnabled_EmitsEnabled)
	// covers the enabled case.
	state := domain.ArtifactState{
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	if !strings.Contains(string(got), "commits: disabled") {
		t.Errorf("Render: want frontmatter to contain %q, but it was absent.\nOutput:\n%s", "commits: disabled", got)
	}
}

func TestRender_CommitsPosition_AfterCheckpointsBeforeCurrentState(t *testing.T) {
	// commits: disabled must appear immediately after the checkpoints: line
	// and before the current_state: block, so the two related toggles read
	// together.
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{Checkpoints: true},
	}

	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}

	output := string(got)
	checkpointsIdx := strings.Index(output, "checkpoints:")
	commitsIdx := strings.Index(output, "commits:")
	currentStateIdx := strings.Index(output, "current_state:")

	if checkpointsIdx < 0 {
		t.Fatal("rendered output is missing the checkpoints: field")
	}
	if commitsIdx < 0 {
		t.Fatal("rendered output is missing the commits: field")
	}
	if currentStateIdx < 0 {
		t.Fatal("rendered output is missing the current_state: field")
	}
	if !(checkpointsIdx < commitsIdx && commitsIdx < currentStateIdx) {
		t.Errorf("commits is not positioned after checkpoints: and before current_state:. checkpoints@%d, commits@%d, current_state@%d",
			checkpointsIdx, commitsIdx, currentStateIdx)
	}
}

// ---- Render: new configuration keys ----

func TestRender_ModeKey_Emitted(t *testing.T) {
	// When Mode is non-empty, Render must emit a "mode:" key with the correct value.
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{Mode: domain.ExecutionModeAutoReview},
	}

	got, err := artifact.Render(state)

	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "mode: auto-review") {
		t.Errorf("Render: want frontmatter to contain %q, got:\n%s", "mode: auto-review", got)
	}
}

func TestRender_CommitsEnabled_EmitsEnabled(t *testing.T) {
	// When Commits is true, Render must emit "commits: enabled" (not the
	// former hardcoded "commits: disabled").
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{Commits: true},
	}

	got, err := artifact.Render(state)

	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "commits: enabled") {
		t.Errorf("Render: want frontmatter to contain %q when Commits=true, got:\n%s", "commits: enabled", got)
	}
}

func TestRender_CommitBranchVariantKey_EmittedWhenCommitsEnabled(t *testing.T) {
	// When commits is enabled, commit_branch_variant must appear in the rendered
	// output with the correct value.
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{Commits: true, CommitBranchVariant: domain.CommitBranchUserOwn},
	}

	got, err := artifact.Render(state)

	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "commit_branch_variant: user-own") {
		t.Errorf("Render: want frontmatter to contain %q when Commits=true, got:\n%s",
			"commit_branch_variant: user-own", got)
	}
}

func TestRender_CommitBranchVariantKey_OmittedWhenCommitsDisabled(t *testing.T) {
	// When commits is disabled, commit_branch_variant must NOT appear in the
	// rendered output, regardless of the CommitBranchVariant field value.
	// RED: current implementation unconditionally emits the key.
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{Commits: false, CommitBranchVariant: domain.CommitBranchUserOwn},
	}

	got, err := artifact.Render(state)

	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if strings.Contains(string(got), "commit_branch_variant:") {
		t.Errorf("Render: want frontmatter to NOT contain %q when Commits=false, got:\n%s",
			"commit_branch_variant:", got)
	}
}

func TestRender_CommitBranchKey_EmittedWhenNonEmpty(t *testing.T) {
	// commit_branch must appear in the rendered output when non-empty.
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{CommitBranch: "mosaic/run/20260816T000724Z-3b68"},
	}

	got, err := artifact.Render(state)

	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "commit_branch: mosaic/run/20260816T000724Z-3b68") {
		t.Errorf("Render: want frontmatter to contain %q when CommitBranch non-empty, got:\n%s",
			"commit_branch: mosaic/run/20260816T000724Z-3b68", got)
	}
}

func TestRender_CommitBranchKey_OmittedWhenEmpty(t *testing.T) {
	// commit_branch must be omitted entirely when empty (not emitted as "commit_branch: ").
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{CommitBranch: ""},
	}

	got, err := artifact.Render(state)

	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if strings.Contains(string(got), "commit_branch:") {
		t.Errorf("Render: want frontmatter to NOT contain %q when CommitBranch empty, got:\n%s",
			"commit_branch:", got)
	}
}

func TestRender_PreConsultationKey_Emitted(t *testing.T) {
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{PreConsultation: true},
	}

	got, err := artifact.Render(state)

	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "pre_consultation: enabled") {
		t.Errorf("Render: want frontmatter to contain %q when PreConsultation=true, got:\n%s",
			"pre_consultation: enabled", got)
	}
}

func TestRender_ManualResolutionKey_Emitted(t *testing.T) {
	state := domain.ArtifactState{
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{ManualResolution: true},
	}

	got, err := artifact.Render(state)

	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	if !strings.Contains(string(got), "manual_resolution: enabled") {
		t.Errorf("Render: want frontmatter to contain %q when ManualResolution=true, got:\n%s",
			"manual_resolution: enabled", got)
	}
}
