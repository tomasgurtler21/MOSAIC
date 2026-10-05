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
	// Messages longer than 100 characters are truncated to head-50 + " ... " + tail-50.
	// This ensures verbose messages retain both the opening context and final conclusion.
	head := strings.Repeat("h", 50)
	tail := strings.Repeat("t", 50)
	input := head + strings.Repeat("m", 20) + tail // 120 chars total

	got := artifact.TruncateSummary(input)

	want := head + " ... " + tail
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

func TestRender_EmptyOrMalformedRunID_ReturnsError(t *testing.T) {
	// run_id is required: Render never writes an artifact without a valid one.
	for name, runID := range map[string]string{
		"empty":     "",
		"malformed": "not-a-run-id",
	} {
		t.Run(name, func(t *testing.T) {
			state := domain.ArtifactState{
				RunID:    runID,
				Type:     "orchestration-artifact",
				Workflow: "test",
			}

			got, err := artifact.Render(state)

			if err == nil {
				t.Fatalf("Render with RunID %q: want error, got nil and output:\n%s", runID, got)
			}
			asRefusalError(t, err)
		})
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
		RunID:    testRunID,
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
		RunID:       testRunID,
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

// ---- Render: runner-owned configuration keys ----

// renderState renders state and returns the output as a string, failing the
// test on error.
func renderState(t *testing.T, state domain.ArtifactState) string {
	t.Helper()
	got, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: unexpected error: %v", err)
	}
	return string(got)
}

func TestRender_RunnerKeys_EmittedUnderPrefixedNames(t *testing.T) {
	state := domain.ArtifactState{
		RunID:    testRunID,
		Type:     "orchestration-artifact",
		Workflow: "test",
		RunSettings: domain.RunSettings{
			Mode:             domain.ExecutionModeAutoReview,
			PreConsultation:  true,
			ManualResolution: false,
		},
	}

	out := renderState(t, state)

	for _, want := range []string{
		"runner_mode: auto-review\n",
		"runner_pre_consultation: enabled\n",
		"runner_manual_resolution: disabled\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render: want frontmatter to contain %q, got:\n%s", want, out)
		}
	}
}

func TestRender_NeverEmitsUnprefixedRunnerKeys(t *testing.T) {
	state := domain.ArtifactState{
		RunID:    testRunID,
		Type:     "orchestration-artifact",
		Workflow: "test",
		RunSettings: domain.RunSettings{
			Mode:             domain.ExecutionModeAuto,
			PreConsultation:  true,
			ManualResolution: true,
		},
	}

	out := renderState(t, state)

	for _, line := range strings.Split(out, "\n") {
		for _, legacy := range []string{"mode:", "pre_consultation:", "manual_resolution:"} {
			if strings.HasPrefix(line, legacy) {
				t.Errorf("Render must not write legacy key %q, found line %q", legacy, line)
			}
		}
	}
}

func TestRender_ModeUnset_OmitsAllRunnerKeys(t *testing.T) {
	// A native-created artifact carries no runner_* keys; rendering it must
	// not invent them.
	state := domain.ArtifactState{
		RunID:    testRunID,
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	out := renderState(t, state)

	if strings.Contains(out, "runner_") {
		t.Errorf("Render with Mode unset: want no runner_* keys, got:\n%s", out)
	}
}

func TestRender_RunnerKeys_PositionedAfterCommitBranchBeforeCurrentState(t *testing.T) {
	state := domain.ArtifactState{
		RunID:       testRunID,
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{Mode: domain.ExecutionModeAuto, Commits: true, CommitBranch: "feature/x"},
	}

	out := renderState(t, state)

	commitsIdx := strings.Index(out, "\ncommits:")
	branchIdx := strings.Index(out, "\ncommit_branch:")
	modeIdx := strings.Index(out, "\nrunner_mode:")
	manualIdx := strings.Index(out, "\nrunner_manual_resolution:")
	currentIdx := strings.Index(out, "\ncurrent_state:")
	if commitsIdx < 0 || branchIdx < 0 || modeIdx < 0 || manualIdx < 0 || currentIdx < 0 {
		t.Fatalf("missing expected keys in:\n%s", out)
	}
	if !(commitsIdx < branchIdx && branchIdx < modeIdx && modeIdx < manualIdx && manualIdx < currentIdx) {
		t.Errorf("key order wrong: commits@%d commit_branch@%d runner_mode@%d runner_manual_resolution@%d current_state@%d",
			commitsIdx, branchIdx, modeIdx, manualIdx, currentIdx)
	}
}

func TestRender_CommitsEnabled_EmitsEnabled(t *testing.T) {
	state := domain.ArtifactState{
		RunID:       testRunID,
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{Commits: true},
	}

	out := renderState(t, state)

	if !strings.Contains(out, "commits: enabled") {
		t.Errorf("Render: want frontmatter to contain %q when Commits=true, got:\n%s", "commits: enabled", out)
	}
}

func TestRender_CommitBranchVariant_NeverWritten(t *testing.T) {
	// The variant is derived from commit_branch on read; no key is ever written,
	// whatever the in-memory variant or commits setting.
	for _, variant := range []domain.CommitBranchVariant{"", domain.CommitBranchMOSAICOwned, domain.CommitBranchUserOwn} {
		for _, commits := range []bool{false, true} {
			state := domain.ArtifactState{
				RunID:    testRunID,
				Type:     "orchestration-artifact",
				Workflow: "test",
				RunSettings: domain.RunSettings{
					Commits:             commits,
					CommitBranchVariant: variant,
					CommitBranch:        domain.MOSAICRunBranchName(testRunID),
				},
			}

			out := renderState(t, state)

			if strings.Contains(out, "commit_branch_variant") {
				t.Errorf("variant=%q commits=%v: commit_branch_variant must never be written, got:\n%s", variant, commits, out)
			}
		}
	}
}

func TestRender_CommitBranchKey_EmittedWhenNonEmpty(t *testing.T) {
	state := domain.ArtifactState{
		RunID:       testRunID,
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{CommitBranch: "mosaic/run/20260816T000724Z-3b68"},
	}

	out := renderState(t, state)

	if !strings.Contains(out, "commit_branch: mosaic/run/20260816T000724Z-3b68") {
		t.Errorf("Render: want commit_branch line, got:\n%s", out)
	}
}

func TestRender_CommitBranchKey_OmittedWhenEmpty(t *testing.T) {
	state := domain.ArtifactState{
		RunID:    testRunID,
		Type:     "orchestration-artifact",
		Workflow: "test",
	}

	out := renderState(t, state)

	if strings.Contains(out, "commit_branch:") {
		t.Errorf("Render: want no commit_branch key when empty, got:\n%s", out)
	}
}

// ---- Render: review_loop_limit and infrastructure_selections ----

func TestRender_ReviewLoopLimit_EmittedWhenPositive(t *testing.T) {
	state := domain.ArtifactState{
		RunID:       testRunID,
		Type:        "orchestration-artifact",
		Workflow:    "test",
		RunSettings: domain.RunSettings{ReviewLoopLimit: 3},
	}

	out := renderState(t, state)

	if !strings.Contains(out, "review_loop_limit: 3\n") {
		t.Errorf("Render: want %q, got:\n%s", "review_loop_limit: 3", out)
	}
}

func TestRender_ReviewLoopLimit_OmittedWhenZero(t *testing.T) {
	state := domain.ArtifactState{RunID: testRunID, Type: "orchestration-artifact", Workflow: "test"}

	out := renderState(t, state)

	if strings.Contains(out, "review_loop_limit") {
		t.Errorf("Render: want no review_loop_limit when no limit, got:\n%s", out)
	}
}

func TestRender_InfrastructureSelections_BlockMappingInClassOrder(t *testing.T) {
	state := domain.ArtifactState{
		RunID:    testRunID,
		Type:     "orchestration-artifact",
		Workflow: "test",
		RunSettings: domain.RunSettings{
			InfraClassSelections: map[string]string{
				"restore":    "restore-manager-git",
				"commit":     "commit-manager-git",
				"checkpoint": "checkpoint-manager-git",
			},
		},
	}

	out := renderState(t, state)

	want := "infrastructure_selections:\n" +
		"  checkpoint: checkpoint-manager-git\n" +
		"  commit: commit-manager-git\n" +
		"  restore: restore-manager-git\n"
	if !strings.Contains(out, want) {
		t.Errorf("Render: want block mapping in class order %q, got:\n%s", want, out)
	}
}

func TestRender_InfrastructureSelections_OmittedWhenEmpty(t *testing.T) {
	for name, sel := range map[string]map[string]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			state := domain.ArtifactState{
				RunID:       testRunID,
				Type:        "orchestration-artifact",
				Workflow:    "test",
				RunSettings: domain.RunSettings{InfraClassSelections: sel},
			}

			out := renderState(t, state)

			if strings.Contains(out, "infrastructure_selections") {
				t.Errorf("Render: want no infrastructure_selections key, got:\n%s", out)
			}
		})
	}
}

// ---- Render: unknown frontmatter preservation ----

func TestRender_UnknownFrontmatter_EmittedVerbatimBeforeCurrentStateInOrder(t *testing.T) {
	state := domain.ArtifactState{
		RunID:    testRunID,
		Type:     "orchestration-artifact",
		Workflow: "test",
		UnknownFrontmatter: []domain.FrontmatterEntry{
			{Key: "zeta", Lines: []string{"zeta: 1"}},
			{Key: "alpha", Lines: []string{"alpha:", "  nested: value", "  other: 2"}},
		},
	}

	out := renderState(t, state)

	want := "zeta: 1\nalpha:\n  nested: value\n  other: 2\ncurrent_state:\n"
	if !strings.Contains(out, want) {
		t.Errorf("Render: want unknown keys verbatim in file order before current_state, got:\n%s", out)
	}
}
