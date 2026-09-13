package resultsummary_test

// Tests for RenderInternalSummary rendering behavior.
//
// These tests verify that RenderInternalSummary produces an internal/test-owner-
// facing summary document with the following characteristics:
//
//  1. Contains a generated:internal-overview section with version, report count,
//     suites, models, and harnesses.
//
//  2. Contains generated:problem-areas and generated:infrastructure-failures
//     sections, both with sample-size annotations (formatSampleSize output)
//     retained alongside pass-rate values.
//
//  3. Contains a generated:exclusions-detail section. When ExclusionDetails
//     is empty the section renders a placeholder message. When populated it
//     renders a table with one row per ExclusionDetail.
//
//  4. Does NOT include generated:model-comparison, generated:harness-comparison,
//     or generated:model-results sections (those belong to the user-facing report).
//
//  5. Includes the analysis:internal-analysis placeholder markers for
//     test-owner commentary.
//
// RED-phase notes:
//   - All tests that assert section presence FAIL at runtime: RenderInternalSummary
//     returns "" (stub) with no implementation.
//   - Tests asserting section absence pass vacuously in RED phase (stub returns "")
//     but provide real protection once implementation exists.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/resultsummary"
)

// internalSummaryFixture builds a VersionSummary with non-trivial ProblemTests
// and InfraTests (both with ExcludedCount > 0 so sample-size annotation checks
// are meaningful) and a non-empty ExclusionDetails slice for exclusion-table
// rendering checks.
func internalSummaryFixture() resultsummary.VersionSummary {
	statsA := resultsummary.HarnessModelStats{
		Harness:        "claude-code",
		Model:          "claude-sonnet-4.6",
		TestCount:      8,
		PassCount:      6,
		PassRate:       0.75,
		TotalCost:      0.16,
		ExcludedCount:  2,
		AttemptedCount: 10,
	}
	statsB := resultsummary.HarnessModelStats{
		Harness:        "open-code",
		Model:          "gpt-4o",
		TestCount:      8,
		PassCount:      5,
		PassRate:       0.625,
		TotalCost:      0.14,
		ExcludedCount:  1,
		AttemptedCount: 9,
	}
	return resultsummary.VersionSummary{
		Version:     "7.4.1",
		ReportCount: 3,
		Suites:      []string{"deploy-check", "route-back"},
		Models:      []string{"claude-sonnet-4.6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  16,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {"claude-code": statsA},
			"gpt-4o":            {"open-code": statsB},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"route-back": {
				"claude-sonnet-4.6": {"claude-code": statsA},
				"gpt-4o":            {"open-code": statsB},
			},
			"deploy-check": {
				"claude-sonnet-4.6": {"claude-code": statsA},
			},
		},
		ProblemTests: []resultsummary.TestStats{
			{
				SuiteID:       "route-back",
				TestName:      "flaky-route-test",
				NumericID:     5,
				BestRate:      0.75,
				BestCombo:     "claude-sonnet-4.6/claude-code",
				WorstRate:     0.625,
				WorstCombo:    "gpt-4o/open-code",
				Spread:        0.125,
				BestCounted:   8,
				BestExcluded:  2,
				WorstCounted:  8,
				WorstExcluded: 1,
			},
		},
		InfraTests: []resultsummary.TestStats{
			{
				SuiteID:       "deploy-check",
				TestName:      "infra-broken-test",
				NumericID:     3,
				BestRate:      0.0,
				BestCombo:     "claude-sonnet-4.6/claude-code",
				WorstRate:     0.0,
				WorstCombo:    "claude-sonnet-4.6/claude-code",
				Spread:        0.0,
				BestCounted:   4,
				BestExcluded:  3,
				WorstCounted:  4,
				WorstExcluded: 3,
			},
		},
		ExclusionDetails: []resultsummary.ExclusionDetail{
			{
				Suite:             "route-back",
				TestName:          "test-1",
				Reason:            "spawn_failed",
				TerminationReason: "early_exit",
				Detail:            "process exited with code 1",
			},
			{
				Suite:             "route-back",
				TestName:          "test-1",
				Reason:            "spawn_failed",
				TerminationReason: "early_exit",
				Detail:            "process exited with code 1",
			},
		},
	}
}

// internalSummaryFixtureNoExclusions builds a VersionSummary with an empty
// ExclusionDetails slice to test the placeholder rendering path.
func internalSummaryFixtureNoExclusions() resultsummary.VersionSummary {
	v := internalSummaryFixture()
	v.ExclusionDetails = nil
	return v
}

// -- Overview section tests --

// TestRenderInternalSummary_ContainsInternalOverviewSection verifies that the
// rendered output includes a generated:internal-overview section wrapped in
// marker comments.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_ContainsInternalOverviewSection(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:internal-overview -->"
	closeMarker := "<!-- /generated:internal-overview -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderInternalSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderInternalSummary_InternalOverviewSection_ContainsVersion verifies
// that the internal-overview section includes the version string.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_InternalOverviewSection_ContainsVersion(t *testing.T) {
	v := internalSummaryFixture() // Version: "7.4.1"
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:internal-overview -->"
	closeMarker := "<!-- /generated:internal-overview -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:internal-overview block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	if !strings.Contains(block, "7.4.1") {
		t.Errorf("internal-overview section must contain version \"7.4.1\"; block:\n%s", block)
	}
}

// TestRenderInternalSummary_InternalOverviewSection_ContainsReportCount verifies
// that the internal-overview section includes the report count.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_InternalOverviewSection_ContainsReportCount(t *testing.T) {
	v := internalSummaryFixture() // ReportCount: 3
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:internal-overview -->"
	closeMarker := "<!-- /generated:internal-overview -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:internal-overview block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	if !strings.Contains(block, "3") {
		t.Errorf("internal-overview section must contain report count \"3\"; block:\n%s", block)
	}
}

// TestRenderInternalSummary_InternalOverviewSection_ContainsSuites verifies
// that the internal-overview section lists the suites from VersionSummary.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_InternalOverviewSection_ContainsSuites(t *testing.T) {
	v := internalSummaryFixture() // Suites: ["deploy-check", "route-back"]
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:internal-overview -->"
	closeMarker := "<!-- /generated:internal-overview -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:internal-overview block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	if !strings.Contains(block, "deploy-check") {
		t.Errorf("internal-overview section must contain suite \"deploy-check\"; block:\n%s", block)
	}
	if !strings.Contains(block, "route-back") {
		t.Errorf("internal-overview section must contain suite \"route-back\"; block:\n%s", block)
	}
}

// TestRenderInternalSummary_InternalOverviewSection_ContainsModels verifies
// that the internal-overview section lists the models from VersionSummary.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_InternalOverviewSection_ContainsModels(t *testing.T) {
	v := internalSummaryFixture() // Models: ["claude-sonnet-4.6", "gpt-4o"]
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:internal-overview -->"
	closeMarker := "<!-- /generated:internal-overview -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:internal-overview block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	if !strings.Contains(block, "claude-sonnet-4.6") {
		t.Errorf("internal-overview section must contain model \"claude-sonnet-4.6\"; block:\n%s", block)
	}
	if !strings.Contains(block, "gpt-4o") {
		t.Errorf("internal-overview section must contain model \"gpt-4o\"; block:\n%s", block)
	}
}

// TestRenderInternalSummary_InternalOverviewSection_ContainsHarnesses verifies
// that the internal-overview section lists the harnesses from VersionSummary.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_InternalOverviewSection_ContainsHarnesses(t *testing.T) {
	v := internalSummaryFixture() // Harnesses: ["claude-code", "open-code"]
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:internal-overview -->"
	closeMarker := "<!-- /generated:internal-overview -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:internal-overview block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	if !strings.Contains(block, "claude-code") {
		t.Errorf("internal-overview section must contain harness \"claude-code\"; block:\n%s", block)
	}
	if !strings.Contains(block, "open-code") {
		t.Errorf("internal-overview section must contain harness \"open-code\"; block:\n%s", block)
	}
}

// -- Problem areas section tests --

// TestRenderInternalSummary_ContainsProblemAreasSection verifies that the
// rendered output includes the generated:problem-areas section wrapped in
// marker comments.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_ContainsProblemAreasSection(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:problem-areas -->"
	closeMarker := "<!-- /generated:problem-areas -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderInternalSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderInternalSummary_ProblemAreasSection_ContainsSampleSizeAnnotation
// verifies that the problem-areas section retains the formatSampleSize annotation
// alongside pass-rate values. The fixture has BestExcluded=2 and WorstExcluded=1,
// so formatSampleSize produces "(8/10)" and "(8/9)" respectively.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_ProblemAreasSection_ContainsSampleSizeAnnotation(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:problem-areas -->"
	closeMarker := "<!-- /generated:problem-areas -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:problem-areas block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	// BestCounted=8, BestExcluded=2 -> formatSampleSize produces "(8/10)"
	if !strings.Contains(block, "(8/10)") {
		t.Errorf("problem-areas section must contain sample-size annotation \"(8/10)\" "+
			"(BestCounted=8, BestExcluded=2); block:\n%s", block)
	}
	// WorstCounted=8, WorstExcluded=1 -> formatSampleSize produces "(8/9)"
	if !strings.Contains(block, "(8/9)") {
		t.Errorf("problem-areas section must contain sample-size annotation \"(8/9)\" "+
			"(WorstCounted=8, WorstExcluded=1); block:\n%s", block)
	}
}

// -- Infrastructure failures section tests --

// TestRenderInternalSummary_ContainsInfrastructureFailuresSection verifies that
// the rendered output includes the generated:infrastructure-failures section
// wrapped in marker comments.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_ContainsInfrastructureFailuresSection(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:infrastructure-failures -->"
	closeMarker := "<!-- /generated:infrastructure-failures -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderInternalSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderInternalSummary_InfrastructureFailuresSection_ContainsSampleSizeAnnotation
// verifies that the infrastructure-failures section retains the formatSampleSize
// annotation. The infra fixture has BestExcluded=3 and WorstExcluded=3, so
// formatSampleSize produces "(4/7)".
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_InfrastructureFailuresSection_ContainsSampleSizeAnnotation(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:infrastructure-failures -->"
	closeMarker := "<!-- /generated:infrastructure-failures -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:infrastructure-failures block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	// BestCounted=4, BestExcluded=3 -> formatSampleSize produces "(4/7)"
	if !strings.Contains(block, "(4/7)") {
		t.Errorf("infrastructure-failures section must contain sample-size annotation \"(4/7)\" "+
			"(BestCounted=4, BestExcluded=3); block:\n%s", block)
	}
}

// -- Exclusions-detail section tests --

// TestRenderInternalSummary_ContainsExclusionsDetailSection verifies that the
// rendered output includes the generated:exclusions-detail section wrapped in
// marker comments, regardless of whether ExclusionDetails is populated.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_ContainsExclusionsDetailSection(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:exclusions-detail -->"
	closeMarker := "<!-- /generated:exclusions-detail -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderInternalSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderInternalSummary_ExclusionsDetailSection_PlaceholderWhenEmpty
// verifies that when ExclusionDetails is empty (or nil), the exclusions-detail
// section renders a placeholder message rather than an empty block.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_ExclusionsDetailSection_PlaceholderWhenEmpty(t *testing.T) {
	v := internalSummaryFixtureNoExclusions() // ExclusionDetails: nil
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:exclusions-detail -->"
	closeMarker := "<!-- /generated:exclusions-detail -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:exclusions-detail block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	// The section must not be completely empty; a placeholder must be present.
	// The design specifies "No exclusion details available." but the exact wording
	// is an implementation choice -- test only that non-empty content is present.
	trimmed := strings.TrimSpace(strings.Replace(
		strings.Replace(block, openMarker, "", 1),
		closeMarker, "", 1,
	))
	if trimmed == "" {
		t.Errorf("exclusions-detail section must contain placeholder text when ExclusionDetails is empty; block:\n%s", block)
	}
}

// TestRenderInternalSummary_ExclusionsDetailSection_RendersTableWhenPopulated
// verifies that when ExclusionDetails is non-empty, the exclusions-detail section
// renders table rows containing the suite, test name, reason, termination reason,
// and detail for each entry.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_ExclusionsDetailSection_RendersTableWhenPopulated(t *testing.T) {
	v := internalSummaryFixture() // ExclusionDetails has two entries for "route-back" / "test-1"
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- generated:exclusions-detail -->"
	closeMarker := "<!-- /generated:exclusions-detail -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:exclusions-detail block not found; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	// Suite and test name from the fixture entries
	if !strings.Contains(block, "route-back") {
		t.Errorf("exclusions-detail section must contain suite \"route-back\"; block:\n%s", block)
	}
	if !strings.Contains(block, "test-1") {
		t.Errorf("exclusions-detail section must contain test name \"test-1\"; block:\n%s", block)
	}
	// Reason from the fixture entries
	if !strings.Contains(block, "spawn_failed") {
		t.Errorf("exclusions-detail section must contain reason \"spawn_failed\"; block:\n%s", block)
	}
	// TerminationReason from the fixture entries
	if !strings.Contains(block, "early_exit") {
		t.Errorf("exclusions-detail section must contain termination reason \"early_exit\"; block:\n%s", block)
	}
	// Detail from the fixture entries
	if !strings.Contains(block, "process exited with code 1") {
		t.Errorf("exclusions-detail section must contain detail \"process exited with code 1\"; block:\n%s", block)
	}
}

// -- Absent section tests --

// TestRenderInternalSummary_DoesNotContainModelComparisonSection verifies that
// the rendered output does NOT include the generated:model-comparison section,
// even when ByModel data is present.
//
// Passes vacuously in RED phase (stub returns ""). Provides real protection once
// implementation exists.
func TestRenderInternalSummary_DoesNotContainModelComparisonSection(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	if strings.Contains(output, "<!-- generated:model-comparison -->") {
		t.Errorf("RenderInternalSummary output must NOT contain generated:model-comparison section; got:\n%s", output)
	}
	if strings.Contains(output, "<!-- /generated:model-comparison -->") {
		t.Errorf("RenderInternalSummary output must NOT contain generated:model-comparison close marker; got:\n%s", output)
	}
}

// TestRenderInternalSummary_DoesNotContainHarnessComparisonSection verifies that
// the rendered output does NOT include the generated:harness-comparison section.
//
// Passes vacuously in RED phase (stub returns ""). Provides real protection once
// implementation exists.
func TestRenderInternalSummary_DoesNotContainHarnessComparisonSection(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	if strings.Contains(output, "<!-- generated:harness-comparison -->") {
		t.Errorf("RenderInternalSummary output must NOT contain generated:harness-comparison section; got:\n%s", output)
	}
	if strings.Contains(output, "<!-- /generated:harness-comparison -->") {
		t.Errorf("RenderInternalSummary output must NOT contain generated:harness-comparison close marker; got:\n%s", output)
	}
}

// TestRenderInternalSummary_DoesNotContainModelResultsSection verifies that the
// rendered output does NOT include the generated:model-results section.
//
// Passes vacuously in RED phase (stub returns ""). Provides real protection once
// implementation exists.
func TestRenderInternalSummary_DoesNotContainModelResultsSection(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	if strings.Contains(output, "<!-- generated:model-results -->") {
		t.Errorf("RenderInternalSummary output must NOT contain generated:model-results section; got:\n%s", output)
	}
	if strings.Contains(output, "<!-- /generated:model-results -->") {
		t.Errorf("RenderInternalSummary output must NOT contain generated:model-results close marker; got:\n%s", output)
	}
}

// -- Analysis placeholder test --

// TestRenderInternalSummary_ContainsInternalAnalysisPlaceholder verifies that
// the rendered output includes the analysis:internal-analysis placeholder markers
// for test-owner commentary.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub).
func TestRenderInternalSummary_ContainsInternalAnalysisPlaceholder(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	openMarker := "<!-- analysis:internal-analysis -->"
	closeMarker := "<!-- /analysis:internal-analysis -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderInternalSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// -- Determinism test --

// TestRenderInternalSummary_Deterministic verifies that calling
// RenderInternalSummary twice with the same input produces byte-identical output.
//
// Passes vacuously in RED phase (stub returns "" for both calls). Provides real
// protection once implementation exists.
func TestRenderInternalSummary_Deterministic(t *testing.T) {
	v := internalSummaryFixture()
	first := resultsummary.RenderInternalSummary(v)
	second := resultsummary.RenderInternalSummary(v)
	if first != second {
		t.Errorf("RenderInternalSummary is not deterministic: two calls with same input produced different output.\nFirst:\n%s\nSecond:\n%s",
			first, second)
	}
}

// -- Section order test --

// TestRenderInternalSummary_SectionOrder verifies that the four generated sections
// appear in the expected order: internal-overview, then problem-areas, then
// infrastructure-failures, then exclusions-detail.
//
// FAILS in RED phase: RenderInternalSummary returns "" (stub) so index lookups
// return -1 and the ordering assertion fails.
func TestRenderInternalSummary_SectionOrder(t *testing.T) {
	v := internalSummaryFixture()
	output := resultsummary.RenderInternalSummary(v)

	overviewIdx := strings.Index(output, "<!-- generated:internal-overview -->")
	problemIdx := strings.Index(output, "<!-- generated:problem-areas -->")
	infraIdx := strings.Index(output, "<!-- generated:infrastructure-failures -->")
	exclusionsIdx := strings.Index(output, "<!-- generated:exclusions-detail -->")

	if overviewIdx < 0 {
		t.Fatal("generated:internal-overview marker not found in RenderInternalSummary output")
	}
	if problemIdx < 0 {
		t.Fatal("generated:problem-areas marker not found in RenderInternalSummary output")
	}
	if infraIdx < 0 {
		t.Fatal("generated:infrastructure-failures marker not found in RenderInternalSummary output")
	}
	if exclusionsIdx < 0 {
		t.Fatal("generated:exclusions-detail marker not found in RenderInternalSummary output")
	}

	if !(overviewIdx < problemIdx) {
		t.Errorf("internal-overview section must come before problem-areas: got internal-overview=%d, problem-areas=%d",
			overviewIdx, problemIdx)
	}
	if !(problemIdx < infraIdx) {
		t.Errorf("problem-areas section must come before infrastructure-failures: got problem-areas=%d, infrastructure-failures=%d",
			problemIdx, infraIdx)
	}
	if !(infraIdx < exclusionsIdx) {
		t.Errorf("infrastructure-failures section must come before exclusions-detail: got infrastructure-failures=%d, exclusions-detail=%d",
			infraIdx, exclusionsIdx)
	}
}
