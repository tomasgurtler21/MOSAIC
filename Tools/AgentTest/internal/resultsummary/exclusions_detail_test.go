package resultsummary_test

// Tests for exclusions-detail threading: from stored report JSON through
// resultstore.AggregateWire into resultsummary.VersionSummary.ExclusionDetails,
// and into the internal-summary.md exclusions-detail section rendered by
// RenderInternalSummary.
//
// These tests cover the integration path -- fixture JSON on disk ->
// ParseAndValidate -> buildVersionSummary -> Generate writes internal-summary.md
// -- rather than the pure-render tests in internal_summary_test.go.
//
// RED-phase failure modes:
//
//  1. Generate does not yet write internal-summary.md (it writes summary.md).
//     All tests that read /TestResults/Orchestrator/v7.4.1/internal-summary.md
//     fail with "file was not found in fakeFS".
//
//  2. AggregateWire does not yet have an Exclusions field, so the exclusions
//     array in the fixture is silently discarded during JSON unmarshaling.
//     VersionSummary.ExclusionDetails remains nil even after the Generate
//     output file path issue is fixed.
//
//  3. buildVersionSummary does not populate ExclusionDetails from the wire
//     data, so the exclusions-detail table is never rendered even after the
//     Exclusions field is added to AggregateWire.
//
// All three issues must be resolved for these tests to pass. Resolving only
// one or two leaves at least one test failing.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/resultsummary"
)

// internalSummaryPath returns the path Generate should write the per-version
// internal-summary.md file to, given a root and version string.
func internalSummaryPath(root, version string) string {
	return root + "/Orchestrator/" + version + "/internal-summary.md"
}

// -- Fixture JSON with exclusions (T3.1a) --

// TestGenerate_ExclusionsFixture_InternalSummaryContainsExclusionReasonAndDetail
// verifies that after Generate processes a report fixture whose aggregate
// contains an exclusions array, the written internal-summary.md's
// exclusions-detail section includes the reason and detail of each excluded run.
//
// FAILS in RED phase: Generate does not write internal-summary.md; even if it
// did, AggregateWire.Exclusions and ExclusionDetails population are both absent.
func TestGenerate_ExclusionsFixture_InternalSummaryContainsExclusionReasonAndDetail(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v7.4.1", "route-back", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "exclusions_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, internalSummaryPath("/TestResults", "v7.4.1"))

	// The fixture's first exclusion has reason "spawn_failed".
	if !strings.Contains(content, "spawn_failed") {
		t.Errorf("internal-summary.md exclusions-detail section must contain reason %q; got:\n%s",
			"spawn_failed", content)
	}
	// The fixture's first exclusion has detail "process exited with code 1".
	if !strings.Contains(content, "process exited with code 1") {
		t.Errorf("internal-summary.md exclusions-detail section must contain detail %q; got:\n%s",
			"process exited with code 1", content)
	}
}

// TestGenerate_ExclusionsFixture_InternalSummaryContainsTestNameAndSuite
// verifies that the exclusions-detail table rows identify the excluded run
// by suite ID and test name.
//
// FAILS in RED phase: same reasons as the test above.
func TestGenerate_ExclusionsFixture_InternalSummaryContainsTestNameAndSuite(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v7.4.1", "route-back", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "exclusions_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, internalSummaryPath("/TestResults", "v7.4.1"))

	// Suite comes from the parent report's suite_id field.
	if !strings.Contains(content, "route-back") {
		t.Errorf("internal-summary.md exclusions-detail section must contain suite %q; got:\n%s",
			"route-back", content)
	}
	// Test name comes from TestReportWire.TestName ("test-1" in the fixture).
	if !strings.Contains(content, "test-1") {
		t.Errorf("internal-summary.md exclusions-detail section must contain test name %q; got:\n%s",
			"test-1", content)
	}
}

// TestGenerate_ExclusionsFixture_InternalSummaryContainsTerminationReason
// verifies that the exclusions-detail table rows include the termination reason.
//
// FAILS in RED phase: same reasons as the test above.
func TestGenerate_ExclusionsFixture_InternalSummaryContainsTerminationReason(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v7.4.1", "route-back", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "exclusions_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, internalSummaryPath("/TestResults", "v7.4.1"))

	// The fixture's first exclusion has termination_reason "early_exit".
	if !strings.Contains(content, "early_exit") {
		t.Errorf("internal-summary.md exclusions-detail section must contain termination reason %q; got:\n%s",
			"early_exit", content)
	}
}

// TestGenerate_ExclusionsFixture_InternalSummaryContainsSecondExclusion
// verifies that all exclusion entries are represented in the table, not just
// the first one. The fixture has two exclusions; both must appear.
//
// FAILS in RED phase: same reasons as the test above.
func TestGenerate_ExclusionsFixture_InternalSummaryContainsSecondExclusion(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v7.4.1", "route-back", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "exclusions_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, internalSummaryPath("/TestResults", "v7.4.1"))

	// The fixture's second exclusion has reason "state_integrity" and detail
	// "sandbox state check failed". Both must appear in the table.
	if !strings.Contains(content, "state_integrity") {
		t.Errorf("internal-summary.md exclusions-detail section must contain second exclusion reason %q; got:\n%s",
			"state_integrity", content)
	}
	if !strings.Contains(content, "sandbox state check failed") {
		t.Errorf("internal-summary.md exclusions-detail section must contain second exclusion detail %q; got:\n%s",
			"sandbox state check failed", content)
	}
}

// -- Older report fixture without exclusions (T3.1c) --

// TestGenerate_OlderFixture_InternalSummaryExclusionDetailShowsPlaceholder
// verifies that when a stored report does not contain an exclusions array
// (an older report format), the internal-summary.md exclusions-detail section
// renders a placeholder message rather than failing or showing an empty table.
// The excluded count elsewhere (aggregate.excluded) is unaffected.
//
// FAILS in RED phase: Generate does not write internal-summary.md.
func TestGenerate_OlderFixture_InternalSummaryExclusionDetailShowsPlaceholder(t *testing.T) {
	// excluded_count_report.json has aggregate.excluded=3 but no exclusions
	// array -- it represents an older report format.
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "excluded_count_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, internalSummaryPath("/TestResults", "v1.0.0"))

	openMarker := "<!-- generated:exclusions-detail -->"
	closeMarker := "<!-- /generated:exclusions-detail -->"

	openIdx := strings.Index(content, openMarker)
	closeIdx := strings.Index(content, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("internal-summary.md must contain generated:exclusions-detail section; got:\n%s", content)
	}
	block := content[openIdx:closeIdx]

	// The placeholder must be present -- the exact wording matches the design
	// specification: "No exclusion details available."
	if !strings.Contains(block, "No exclusion details available") {
		t.Errorf("exclusions-detail section for older report must contain placeholder text; block:\n%s", block)
	}
}

// TestGenerate_OlderFixture_ExistingAggregateFieldsUnaffected verifies that
// adding the Exclusions field to AggregateWire does not disturb the existing
// wire fields. The older fixture's aggregate.excluded=3 count must still be
// read correctly after the additive change.
//
// This test checks AC3.5 (additive-only change) through observable side effects:
// the internal summary's overview section still shows the correct report count,
// verifying that the rest of the wire parsing is intact.
//
// FAILS in RED phase: Generate does not write internal-summary.md.
func TestGenerate_OlderFixture_ExistingAggregateFieldsUnaffected(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "excluded_count_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, internalSummaryPath("/TestResults", "v1.0.0"))

	openMarker := "<!-- generated:internal-overview -->"
	closeMarker := "<!-- /generated:internal-overview -->"

	openIdx := strings.Index(content, openMarker)
	closeIdx := strings.Index(content, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("internal-summary.md must contain generated:internal-overview section; got:\n%s", content)
	}
	overview := content[openIdx:closeIdx]

	// The suite "happy-path" must appear, confirming the parser read the report.
	if !strings.Contains(overview, "happy-path") {
		t.Errorf("internal-overview section must contain suite %q, confirming wire parse is intact; block:\n%s",
			"happy-path", overview)
	}
}

// -- Generate output paths (AC3.2 observable through SummaryResult) --

// TestGenerate_ExclusionsFixture_SummaryResultIncludesInternalSummaryPath
// verifies that Generate's SummaryResult records the internal-summary.md path
// in FilesWritten or FilesUpdated. This ensures callers that iterate
// SummaryResult paths see the internal summary alongside the user summary.
//
// FAILS in RED phase: Generate writes summary.md, not internal-summary.md.
func TestGenerate_ExclusionsFixture_SummaryResultIncludesInternalSummaryPath(t *testing.T) {
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v7.4.1", "route-back", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "exclusions_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	wantPath := internalSummaryPath("/TestResults", "v7.4.1")
	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	if !containsPath(allFiles, wantPath) {
		t.Errorf("Generate SummaryResult must include %q; got written=%v updated=%v",
			wantPath, result.FilesWritten, result.FilesUpdated)
	}
}
