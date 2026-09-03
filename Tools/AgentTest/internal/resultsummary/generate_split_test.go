package resultsummary_test

// Integration tests for the Generate two-file output behavior introduced in
// Stage 4 (user-summary.md + internal-summary.md per version directory).
//
// These tests are targeted specifically at the split-file contract:
//
//   - Generate writes BOTH user-summary.md and internal-summary.md for each
//     version directory (not a single summary.md).
//   - SummaryResult.FilesWritten / FilesUpdated reports both paths.
//   - No per-version summary.md is written (old filename is gone).
//   - user-summary.md contains percentage-only pass rates (no (N/M) annotations
//     in model-comparison, harness-comparison, or model-results sections).
//   - internal-summary.md contains the problem-areas and infrastructure-failures
//     sections.
//
// Complementary coverage in other files:
//   - exclusions-detail rendering and exclusion threading: exclusions_detail_test.go
//   - infra test routing to InfraTests: infra_routing_test.go
//   - sample-size annotation suppression in user-facing sections: sample_size_test.go
//   - marker preservation and determinism: resultsummary_test.go

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/resultsummary"
)

// TestGenerate_SplitFiles_WritesBothUserSummaryAndInternalSummary verifies that
// for a single version directory with one report, Generate writes both
// user-summary.md and internal-summary.md and reports both paths in
// SummaryResult.
func TestGenerate_SplitFiles_WritesBothUserSummaryAndInternalSummary(t *testing.T) {
	// Arrange
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// Act
	result, err := resultsummary.Generate(fs, req)

	// Assert
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	allFiles := append(result.FilesWritten, result.FilesUpdated...)

	userPath := "/TestResults/Orchestrator/v1.0.0/user-summary.md"
	if !containsPath(allFiles, userPath) {
		t.Errorf("SummaryResult must include %q; got written=%v updated=%v",
			userPath, result.FilesWritten, result.FilesUpdated)
	}

	internalPath := "/TestResults/Orchestrator/v1.0.0/internal-summary.md"
	if !containsPath(allFiles, internalPath) {
		t.Errorf("SummaryResult must include %q; got written=%v updated=%v",
			internalPath, result.FilesWritten, result.FilesUpdated)
	}
}

// TestGenerate_SplitFiles_NoPerVersionSummaryMdWritten verifies that Generate
// does not write a file named summary.md inside any version directory. The old
// per-version summary.md has been replaced by user-summary.md and
// internal-summary.md. Writing the old file alongside the new ones would cause
// stale output to persist on disk and confuse consumers.
//
// The cross-version Orchestrator/summary.md (at the Orchestrator root, not in a
// version subdirectory) is still expected and is not checked here.
func TestGenerate_SplitFiles_NoPerVersionSummaryMdWritten(t *testing.T) {
	// Arrange
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// Act
	_, err := resultsummary.Generate(fs, req)

	// Assert
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	// Inspect the fake filesystem directly: no file named summary.md should be
	// present under any version directory path.
	perVersionSummaryMd := "/TestResults/Orchestrator/v1.0.0/summary.md"
	if _, ok := fs.files[perVersionSummaryMd]; ok {
		t.Errorf("Generate must not write per-version %q (old filename replaced by user-summary.md + internal-summary.md); "+
			"file was found in fakeFS with content:\n%s",
			perVersionSummaryMd, string(fs.files[perVersionSummaryMd]))
	}
}

// TestGenerate_SplitFiles_TwoVersions_BothGetBothFiles verifies that when
// two version directories are present, Generate writes both user-summary.md
// and internal-summary.md for each, and none of the version directories gets
// a per-version summary.md.
func TestGenerate_SplitFiles_TwoVersions_BothGetBothFiles(t *testing.T) {
	// Arrange
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	seedReport(fs, "/TestResults", "v2.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260825T100000", loadFixture(t, "v2_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// Act
	result, err := resultsummary.Generate(fs, req)

	// Assert
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	allFiles := append(result.FilesWritten, result.FilesUpdated...)

	for _, version := range []string{"v1.0.0", "v2.0.0"} {
		base := "/TestResults/Orchestrator/" + version
		userPath := base + "/user-summary.md"
		internalPath := base + "/internal-summary.md"
		legacyPath := base + "/summary.md"

		if !containsPath(allFiles, userPath) {
			t.Errorf("version %s: SummaryResult must include %q; got: %v", version, userPath, allFiles)
		}
		if !containsPath(allFiles, internalPath) {
			t.Errorf("version %s: SummaryResult must include %q; got: %v", version, internalPath, allFiles)
		}
		if _, ok := fs.files[legacyPath]; ok {
			t.Errorf("version %s: Generate must not write per-version %q; found in fakeFS", version, legacyPath)
		}
	}
}

// TestGenerate_SplitFiles_UserSummaryHasNoSampleSizeAnnotationInModelComparison
// verifies that user-summary.md does not carry (N/M) sample-size annotations in
// its model-comparison block. This is the key user-facing requirement: pass rates
// show as percentages only.
func TestGenerate_SplitFiles_UserSummaryHasNoSampleSizeAnnotationInModelComparison(t *testing.T) {
	// Arrange: use a report fixture with excluded > 0 so that if the
	// implementation erroneously includes sample-size annotations they would appear.
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "excluded_count_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// Act
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	// Assert: inspect the model-comparison block in user-summary.md only.
	userContent := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")

	blockStart := strings.Index(userContent, "<!-- generated:model-comparison -->")
	blockEnd := strings.Index(userContent, "<!-- /generated:model-comparison -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:model-comparison block not found in user-summary.md; implementation required")
	}
	block := userContent[blockStart:blockEnd]

	// The (N/M) pattern must not appear in this block.
	if strings.Contains(block, "(/") || strings.ContainsRune(block, '(') && strings.ContainsRune(block, '/') {
		// Narrow check: parenthetical fractions like (5/8) are the disallowed pattern.
		// Walk through looking for the specific (digits/digits) shape.
		for i := 0; i < len(block)-4; i++ {
			if block[i] == '(' {
				// Find closing paren.
				j := strings.IndexRune(block[i+1:], ')')
				if j < 0 {
					break
				}
				inner := block[i+1 : i+1+j]
				if strings.ContainsRune(inner, '/') {
					t.Errorf("user-summary.md model-comparison must not contain sample-size fraction %q (N/M format); "+
						"user-facing output shows percentage only.\nBlock content:\n%s", "("+inner+")", block)
					break
				}
			}
		}
	}
}

// TestGenerate_SplitFiles_InternalSummaryHasProblemAreasSection verifies that
// internal-summary.md contains a generated:problem-areas block when the version
// has tests with spread > 0 across harness+model combinations.
func TestGenerate_SplitFiles_InternalSummaryHasProblemAreasSection(t *testing.T) {
	// Arrange: two harnesses with different pass rates produce a problem-area entry.
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "open-code",
		"gpt-4o", "20260820T110000", loadFixture(t, "v1_open_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// Act
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	// Assert
	internalContent := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/internal-summary.md")

	if !strings.Contains(internalContent, "<!-- generated:problem-areas -->") {
		t.Error("internal-summary.md must contain <!-- generated:problem-areas --> marker")
	}
	if !strings.Contains(internalContent, "<!-- /generated:problem-areas -->") {
		t.Error("internal-summary.md must contain <!-- /generated:problem-areas --> closing marker")
	}
}

// TestGenerate_SplitFiles_InternalSummaryHasInfrastructureFailuresSection
// verifies that internal-summary.md contains a generated:infrastructure-failures
// block. This section belongs in the internal report only.
func TestGenerate_SplitFiles_InternalSummaryHasInfrastructureFailuresSection(t *testing.T) {
	// Arrange: use any report; the infrastructure-failures section is always
	// rendered (possibly empty) in the internal summary.
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// Act
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	// Assert
	internalContent := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/internal-summary.md")

	if !strings.Contains(internalContent, "<!-- generated:infrastructure-failures -->") {
		t.Error("internal-summary.md must contain <!-- generated:infrastructure-failures --> marker")
	}
	if !strings.Contains(internalContent, "<!-- /generated:infrastructure-failures -->") {
		t.Error("internal-summary.md must contain <!-- /generated:infrastructure-failures --> closing marker")
	}
}

// TestGenerate_SplitFiles_UserSummaryDoesNotContainProblemAreasOrInfraFailures
// verifies that the problem-areas and infrastructure-failures sections are absent
// from user-summary.md. Those sections belong exclusively in the internal report.
func TestGenerate_SplitFiles_UserSummaryDoesNotContainProblemAreasOrInfraFailures(t *testing.T) {
	// Arrange: use two harnesses so problem-areas data is available (and would
	// appear in user-summary.md if the implementation incorrectly includes it).
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "open-code",
		"gpt-4o", "20260820T110000", loadFixture(t, "v1_open_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// Act
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	// Assert
	userContent := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")

	if strings.Contains(userContent, "<!-- generated:problem-areas -->") {
		t.Error("user-summary.md must NOT contain <!-- generated:problem-areas --> marker; " +
			"problem-areas belongs in internal-summary.md only")
	}
	if strings.Contains(userContent, "<!-- generated:infrastructure-failures -->") {
		t.Error("user-summary.md must NOT contain <!-- generated:infrastructure-failures --> marker; " +
			"infrastructure-failures belongs in internal-summary.md only")
	}
}

// TestGenerate_SplitFiles_SummaryResultReportsBothPathsAsSingleVersionOutput
// is the canonical AC4.4 test: SummaryResult.FilesWritten and FilesUpdated
// together must include exactly the user-summary.md and internal-summary.md
// paths for a single-version, fresh-run scenario (no pre-existing files).
// A cross-version summary.md at the Orchestrator root is also expected.
func TestGenerate_SplitFiles_SummaryResultReportsBothPathsAsSingleVersionOutput(t *testing.T) {
	// Arrange: fresh filesystem, no pre-existing summary files.
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	// Act
	result, err := resultsummary.Generate(fs, req)

	// Assert
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	// All files written must appear in FilesWritten on a fresh run.
	userPath := "/TestResults/Orchestrator/v1.0.0/user-summary.md"
	internalPath := "/TestResults/Orchestrator/v1.0.0/internal-summary.md"

	if !containsPath(result.FilesWritten, userPath) {
		t.Errorf("SummaryResult.FilesWritten must include %q on a fresh run; got FilesWritten=%v FilesUpdated=%v",
			userPath, result.FilesWritten, result.FilesUpdated)
	}
	if !containsPath(result.FilesWritten, internalPath) {
		t.Errorf("SummaryResult.FilesWritten must include %q on a fresh run; got FilesWritten=%v FilesUpdated=%v",
			internalPath, result.FilesWritten, result.FilesUpdated)
	}
}

// TestGenerate_SplitFiles_Regeneration_BothFilesMovedToFilesUpdated verifies
// that when user-summary.md and internal-summary.md already exist on disk from
// a prior Generate call, a second Generate call reports both paths in
// FilesUpdated (not FilesWritten), because the files were updated rather than
// created fresh.
func TestGenerate_SplitFiles_Regeneration_BothFilesMovedToFilesUpdated(t *testing.T) {
	// Arrange: first run to write the files.
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}

	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("first Generate returned unexpected error: %v", err)
	}

	// Act: second run with the same inputs (files now exist on disk).
	result, err := resultsummary.Generate(fs, req)

	// Assert
	if err != nil {
		t.Fatalf("second Generate returned unexpected error: %v", err)
	}

	userPath := "/TestResults/Orchestrator/v1.0.0/user-summary.md"
	internalPath := "/TestResults/Orchestrator/v1.0.0/internal-summary.md"

	// Both must appear somewhere in the result (written or updated).
	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	if !containsPath(allFiles, userPath) {
		t.Errorf("second Generate must include %q in result; got written=%v updated=%v",
			userPath, result.FilesWritten, result.FilesUpdated)
	}
	if !containsPath(allFiles, internalPath) {
		t.Errorf("second Generate must include %q in result; got written=%v updated=%v",
			internalPath, result.FilesWritten, result.FilesUpdated)
	}

	// On re-generation the files existed beforehand, so they should appear in
	// FilesUpdated rather than FilesWritten.
	if containsPath(result.FilesWritten, userPath) {
		t.Errorf("%q was re-written but should be in FilesUpdated on regeneration; "+
			"FilesWritten=%v FilesUpdated=%v", userPath, result.FilesWritten, result.FilesUpdated)
	}
	if containsPath(result.FilesWritten, internalPath) {
		t.Errorf("%q was re-written but should be in FilesUpdated on regeneration; "+
			"FilesWritten=%v FilesUpdated=%v", internalPath, result.FilesWritten, result.FilesUpdated)
	}
}
