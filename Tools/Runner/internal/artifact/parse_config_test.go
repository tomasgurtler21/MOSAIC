package artifact_test

// Tests for artifact.Parse: run configuration frontmatter fields (mode, commits,
// commit_branch_variant, commit_branch, pre_consultation, manual_resolution).
// Covers defaults when keys are absent, explicit valid values, and refusals on
// bad values.

import (
	"os"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// ---- Parse: defaults when configuration keys are absent ----

func TestParse_AbsentModeKey_DefaultsToUnset(t *testing.T) {
	// When the mode key is absent, the parsed Mode must equal ExecutionModeUnset.
	// This is not a refusal — absence is a valid state that the caller resolves.
	data := minimalArtifactWithConfigBytes(standardConfigLines)

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.Mode != domain.ExecutionModeUnset {
		t.Errorf("Mode: want %q (unset), got %q", domain.ExecutionModeUnset, state.Mode)
	}
}

func TestParse_AbsentCommitsKey_DefaultsToFalse(t *testing.T) {
	// When the commits key is absent, Commits defaults to false.
	data := minimalArtifactWithConfigBytes("checkpoints: disabled\n")

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.Commits {
		t.Error("Commits: want false (default when key absent), got true")
	}
}

func TestParse_AbsentCommitBranchVariantKey_WhenCommitsEnabled_DefaultsToMOSAICOwned(t *testing.T) {
	// When commits is enabled and the commit_branch_variant key is absent,
	// CommitBranchVariant defaults to CommitBranchMOSAICOwned — the documented
	// recommended variant for commits-enabled runs.
	data := minimalArtifactWithConfigBytes("checkpoints: disabled\ncommits: enabled\n")

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant: want %q (default when commits enabled and key absent), got %q",
			domain.CommitBranchMOSAICOwned, state.CommitBranchVariant)
	}
}

func TestParse_AbsentCommitBranchVariantKey_WhenCommitsDisabled_YieldsEmpty(t *testing.T) {
	// When commits is disabled and the commit_branch_variant key is absent,
	// CommitBranchVariant must be the zero value (empty string). The mosaic-owned
	// default only applies when commits are enabled.
	// RED: current implementation defaults to mosaic-owned regardless of commits.
	data := minimalArtifactWithConfigBytes(standardConfigLines) // commits: disabled

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.CommitBranchVariant != "" {
		t.Errorf("CommitBranchVariant: want %q (zero value when commits disabled and key absent), got %q",
			"", state.CommitBranchVariant)
	}
}

func TestParse_AbsentCommitBranchKey_DefaultsToEmpty(t *testing.T) {
	// When the commit_branch key is absent, CommitBranch is "".
	data := minimalArtifactWithConfigBytes(standardConfigLines)

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.CommitBranch != "" {
		t.Errorf("CommitBranch: want %q (default when key absent), got %q", "", state.CommitBranch)
	}
}

func TestParse_AbsentPreConsultationKey_DefaultsToFalse(t *testing.T) {
	// When the pre_consultation key is absent, PreConsultation defaults to false.
	data := minimalArtifactWithConfigBytes(standardConfigLines)

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.PreConsultation {
		t.Error("PreConsultation: want false (default when key absent), got true")
	}
}

func TestParse_AbsentManualResolutionKey_DefaultsToFalse(t *testing.T) {
	// When the manual_resolution key is absent, ManualResolution defaults to false.
	data := minimalArtifactWithConfigBytes(standardConfigLines)

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.ManualResolution {
		t.Error("ManualResolution: want false (default when key absent), got true")
	}
}

// ---- Parse: explicit configuration key values ----

func TestParse_ModeAuto_ParsedCorrectly(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		"mode: auto\n" +
			standardConfigLines)

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.Mode != domain.ExecutionModeAuto {
		t.Errorf("Mode: want %q, got %q", domain.ExecutionModeAuto, state.Mode)
	}
}

func TestParse_ModeOrchestrated_ParsedCorrectly(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		"mode: orchestrated\n" +
			standardConfigLines)

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.Mode != domain.ExecutionModeOrchestrated {
		t.Errorf("Mode: want %q, got %q", domain.ExecutionModeOrchestrated, state.Mode)
	}
}

func TestParse_ModeAutoReview_ParsedCorrectly(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		"mode: auto-review\n" +
			standardConfigLines)

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.Mode != domain.ExecutionModeAutoReview {
		t.Errorf("Mode: want %q, got %q", domain.ExecutionModeAutoReview, state.Mode)
	}
}

func TestParse_CommitsEnabled_ParsedCorrectly(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		"checkpoints: disabled\n" +
			"commits: enabled\n")

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if !state.Commits {
		t.Error("Commits: want true (commits: enabled), got false")
	}
}

func TestParse_CommitBranchVariantUserOwn_ParsedCorrectly(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		standardConfigLines +
			"commit_branch_variant: user-own\n")

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.CommitBranchVariant != domain.CommitBranchUserOwn {
		t.Errorf("CommitBranchVariant: want %q, got %q", domain.CommitBranchUserOwn, state.CommitBranchVariant)
	}
}

func TestParse_CommitBranch_ParsedCorrectly(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		standardConfigLines +
			"commit_branch: mosaic/run/testrun\n")

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if state.CommitBranch != "mosaic/run/testrun" {
		t.Errorf("CommitBranch: want %q, got %q", "mosaic/run/testrun", state.CommitBranch)
	}
}

func TestParse_PreConsultationEnabled_ParsedCorrectly(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		standardConfigLines +
			"pre_consultation: enabled\n")

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if !state.PreConsultation {
		t.Error("PreConsultation: want true (pre_consultation: enabled), got false")
	}
}

func TestParse_ManualResolutionEnabled_ParsedCorrectly(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		standardConfigLines +
			"manual_resolution: enabled\n")

	state, err := artifact.Parse(data)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if !state.ManualResolution {
		t.Error("ManualResolution: want true (manual_resolution: enabled), got false")
	}
}

// ---- Parse: refusals on bad configuration values ----

func TestParse_UnknownModeValue_ReturnsRefusalError(t *testing.T) {
	// A mode value that is not one of the three valid modes is a refusal.
	// The user supplied a bad value; silence is worse than rejection.
	data := minimalArtifactWithConfigBytes(
		"mode: quick\n" +
			standardConfigLines)

	_, err := artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse with unknown mode value: want error, got nil")
	}
	asRefusalError(t, err)
}

func TestParse_UnknownModeValue_RefusalErrorNamesOffendingValue(t *testing.T) {
	data := minimalArtifactWithConfigBytes(
		"mode: quick\n" +
			standardConfigLines)

	_, err := artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse with unknown mode value: want error, got nil")
	}
	if !strings.Contains(err.Error(), "quick") {
		t.Errorf("error message must name the offending value %q, got: %q", "quick", err.Error())
	}
}

func TestParse_UnknownCommitBranchVariantValue_ReturnsRefusalError(t *testing.T) {
	// A commit_branch_variant value that is not "mosaic-owned" or "user-own"
	// is a refusal.
	data := minimalArtifactWithConfigBytes(
		standardConfigLines +
			"commit_branch_variant: weekly\n")

	_, err := artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse with unknown commit_branch_variant value: want error, got nil")
	}
	asRefusalError(t, err)
}

func TestParse_MalformedCheckpointsValue_ReturnsRefusalError(t *testing.T) {
	// A checkpoints value that is neither "enabled" nor "disabled" must now be
	// refused. Previously it was silently treated as false; this tightening
	// ensures typos like "enbaled" are caught rather than silently disabling
	// checkpoints.
	//
	// This test uses the malformed-checkpoints.md fixture which has
	// "checkpoints: enbaled" (deliberate typo).
	data, err := os.ReadFile(fixturePath("malformed-checkpoints.md"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	_, err = artifact.Parse(data)

	if err == nil {
		t.Fatal("Parse with malformed checkpoints value: want error, got nil")
	}
	asRefusalError(t, err)
}
