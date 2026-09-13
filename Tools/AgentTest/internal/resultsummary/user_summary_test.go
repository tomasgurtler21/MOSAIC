package resultsummary_test

// Tests for RenderUserSummary rendering behavior.
//
// These tests verify that RenderUserSummary produces a user-facing summary
// document with the following characteristics:
//
//  1. Contains the four user-facing generated sections (overview,
//     model-comparison, harness-comparison, model-results) each wrapped in
//     generated marker comments.
//
//  2. Pass-rate cells show percentage only (e.g. "83%") with NO parenthetical
//     sample-size annotation -- no "(N)" or "(N/M)" suffix alongside any
//     pass-rate value, even when ExcludedCount > 0.
//
//  3. Does NOT include generated:problem-areas or
//     generated:infrastructure-failures sections.
//
//  4. Includes the overall-analysis analysis placeholder marker.
//
// RED-phase notes:
//   - All tests in this file FAIL at runtime: RenderUserSummary returns ""
//     (stub) and has no implementation. Tests asserting section presence
//     therefore fail. Tests asserting section absence pass vacuously in RED
//     phase but will provide real protection once the implementation exists.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/resultsummary"
)

// userSummaryFixture builds a VersionSummary with ExcludedCount > 0 so that
// sample-size-absence checks are meaningful. The fixture has two models and
// two harnesses so section rendering is non-trivial.
func userSummaryFixture() resultsummary.VersionSummary {
	statsA := resultsummary.HarnessModelStats{
		Harness:        "claude-code",
		Model:          "claude-sonnet-4.6",
		TestCount:      6,
		PassCount:      5,
		PassRate:       0.833,
		TotalCost:      0.12,
		ExcludedCount:  2,
		AttemptedCount: 8,
	}
	statsB := resultsummary.HarnessModelStats{
		Harness:        "open-code",
		Model:          "gpt-4o",
		TestCount:      6,
		PassCount:      4,
		PassRate:       0.667,
		TotalCost:      0.10,
		ExcludedCount:  1,
		AttemptedCount: 7,
	}
	return resultsummary.VersionSummary{
		Version:     "v2.0.0",
		ReportCount: 2,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  12,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {"claude-code": statsA},
			"gpt-4o":            {"open-code": statsB},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {"claude-code": statsA},
				"gpt-4o":            {"open-code": statsB},
			},
		},
		ProblemTests: []resultsummary.TestStats{
			{
				SuiteID:       "happy-path",
				TestName:      "problematic-test",
				NumericID:     3,
				BestRate:      0.833,
				BestCombo:     "claude-sonnet-4.6/claude-code",
				WorstRate:     0.667,
				WorstCombo:    "gpt-4o/open-code",
				Spread:        0.166,
				BestCounted:   6,
				BestExcluded:  2,
				WorstCounted:  6,
				WorstExcluded: 1,
			},
		},
	}
}

// TestRenderUserSummary_ContainsOverviewSection verifies that the rendered
// output includes the generated:overview section wrapped in marker comments.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub).
func TestRenderUserSummary_ContainsOverviewSection(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- generated:overview -->"
	closeMarker := "<!-- /generated:overview -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderUserSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderUserSummary_ContainsModelComparisonSection verifies that the
// rendered output includes the generated:model-comparison section.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub).
func TestRenderUserSummary_ContainsModelComparisonSection(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- generated:model-comparison -->"
	closeMarker := "<!-- /generated:model-comparison -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderUserSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderUserSummary_ContainsHarnessComparisonSection verifies that the
// rendered output includes the generated:harness-comparison section.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub).
func TestRenderUserSummary_ContainsHarnessComparisonSection(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- generated:harness-comparison -->"
	closeMarker := "<!-- /generated:harness-comparison -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderUserSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderUserSummary_ContainsModelResultsSection verifies that the
// rendered output includes the generated:model-results section.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub).
func TestRenderUserSummary_ContainsModelResultsSection(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- generated:model-results -->"
	closeMarker := "<!-- /generated:model-results -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderUserSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderUserSummary_DoesNotContainProblemAreasSection verifies that the
// rendered output does NOT include the generated:problem-areas section, even
// when ProblemTests is non-empty.
//
// Passes vacuously in RED phase (stub returns ""). Provides real protection
// once implementation exists.
func TestRenderUserSummary_DoesNotContainProblemAreasSection(t *testing.T) {
	v := userSummaryFixture() // ProblemTests is non-empty
	output := resultsummary.RenderUserSummary(v)

	if strings.Contains(output, "<!-- generated:problem-areas -->") {
		t.Errorf("RenderUserSummary output must NOT contain generated:problem-areas section; got:\n%s", output)
	}
	if strings.Contains(output, "<!-- /generated:problem-areas -->") {
		t.Errorf("RenderUserSummary output must NOT contain generated:problem-areas close marker; got:\n%s", output)
	}
}

// TestRenderUserSummary_DoesNotContainInfrastructureFailuresSection verifies
// that the rendered output does NOT include the generated:infrastructure-failures
// section.
//
// Passes vacuously in RED phase (stub returns ""). Provides real protection
// once implementation exists.
func TestRenderUserSummary_DoesNotContainInfrastructureFailuresSection(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	if strings.Contains(output, "<!-- generated:infrastructure-failures -->") {
		t.Errorf("RenderUserSummary output must NOT contain generated:infrastructure-failures section; got:\n%s", output)
	}
	if strings.Contains(output, "<!-- /generated:infrastructure-failures -->") {
		t.Errorf("RenderUserSummary output must NOT contain generated:infrastructure-failures close marker; got:\n%s", output)
	}
}

// TestRenderUserSummary_ContainsOverallAnalysisPlaceholder verifies that the
// rendered output includes the analysis:overall-analysis placeholder markers
// for human-authored commentary.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub).
func TestRenderUserSummary_ContainsOverallAnalysisPlaceholder(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- analysis:overall-analysis -->"
	closeMarker := "<!-- /analysis:overall-analysis -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Errorf("RenderUserSummary output must contain %q and %q markers; got:\n%s",
			openMarker, closeMarker, output)
	}
}

// TestRenderUserSummary_ModelComparisonSection_NoParentheticalAnnotation
// verifies that the model-comparison section contains pass-rate values as
// percentages only, with no parenthetical sample-size annotation alongside
// them. The fixture has ExcludedCount=2 for claude-sonnet-4.6/claude-code,
// so a "(6/8)" or "(6)" annotation would appear if sample-size rendering
// leaked into RenderUserSummary.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub) so the section
// marker check fails first.
func TestRenderUserSummary_ModelComparisonSection_NoParentheticalAnnotation(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- generated:model-comparison -->"
	closeMarker := "<!-- /generated:model-comparison -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:model-comparison block not found in RenderUserSummary output; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	// No parenthetical annotation of any form should appear -- not "(N)" nor
	// "(N/M)". The fixture has ExcludedCount=2 on claude-sonnet-4.6, so any
	// sample-size rendering would produce "(6/8)" or similar. Assert neither
	// form appears.
	if strings.Contains(block, "(6/8)") {
		t.Errorf("model-comparison must not contain reduced-sample indicator (6/8); "+
			"RenderUserSummary shows percentage only; block:\n%s", block)
	}
	if strings.Contains(block, "(6)") {
		t.Errorf("model-comparison must not contain sample-size annotation (6); "+
			"RenderUserSummary shows percentage only; block:\n%s", block)
	}
	// Additionally assert the pass rate is present as a bare percentage.
	if !strings.Contains(block, "83%") {
		t.Errorf("model-comparison must contain pass rate as bare percentage \"83%%\"; block:\n%s", block)
	}
}

// TestRenderUserSummary_HarnessComparisonSection_NoParentheticalAnnotation
// verifies that the harness-comparison section shows percentage-only pass
// rates with no sample-size parenthetical. The fixture has ExcludedCount > 0
// for both harnesses.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub) so the section
// marker check fails first.
func TestRenderUserSummary_HarnessComparisonSection_NoParentheticalAnnotation(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- generated:harness-comparison -->"
	closeMarker := "<!-- /generated:harness-comparison -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:harness-comparison block not found in RenderUserSummary output; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	// claude-code: TestCount=6, ExcludedCount=2 -> "(6/8)" would appear if leaked.
	if strings.Contains(block, "(6/8)") {
		t.Errorf("harness-comparison must not contain reduced-sample indicator (6/8); "+
			"RenderUserSummary shows percentage only; block:\n%s", block)
	}
	if strings.Contains(block, "(6)") {
		t.Errorf("harness-comparison must not contain sample-size annotation (6); "+
			"RenderUserSummary shows percentage only; block:\n%s", block)
	}
	// open-code: TestCount=6, ExcludedCount=1 -> "(6/7)" would appear if leaked.
	if strings.Contains(block, "(6/7)") {
		t.Errorf("harness-comparison must not contain reduced-sample indicator (6/7); "+
			"RenderUserSummary shows percentage only; block:\n%s", block)
	}
}

// TestRenderUserSummary_ModelResultsSection_NoParentheticalAnnotation verifies
// that the model-results section shows percentage-only pass rates with no
// sample-size parenthetical for any row.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub) so the section
// marker check fails first.
func TestRenderUserSummary_ModelResultsSection_NoParentheticalAnnotation(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- generated:model-results -->"
	closeMarker := "<!-- /generated:model-results -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:model-results block not found in RenderUserSummary output; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	// claude-sonnet-4.6 BySuite entry: TestCount=6, ExcludedCount=2.
	// Sample-size render would produce "(6/8)".
	if strings.Contains(block, "(6/8)") {
		t.Errorf("model-results must not contain reduced-sample indicator (6/8); "+
			"RenderUserSummary shows percentage only; block:\n%s", block)
	}
	if strings.Contains(block, "(6)") {
		t.Errorf("model-results must not contain sample-size annotation (6); "+
			"RenderUserSummary shows percentage only; block:\n%s", block)
	}
	// gpt-4o BySuite entry: TestCount=6, ExcludedCount=1.
	// Sample-size render would produce "(6/7)".
	if strings.Contains(block, "(6/7)") {
		t.Errorf("model-results must not contain reduced-sample indicator (6/7); "+
			"RenderUserSummary shows percentage only; block:\n%s", block)
	}
}

// TestRenderUserSummary_NoParentheticalAnywhere verifies that no parenthetical
// annotation of the form "(N)" or "(N/M)" (where N and M are digits) appears
// anywhere in the RenderUserSummary output. This is a catch-all complement to
// the per-section checks: if any section accidentally emits a sample-size
// annotation, this test catches it.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub) so the sections
// check fails first; this test passes vacuously on stub output.
func TestRenderUserSummary_NoParentheticalAnywhere(t *testing.T) {
	// Use a fixture with ExcludedCount=5 to make any leaked annotation distinctive.
	stats := resultsummary.HarnessModelStats{
		Harness:        "claude-code",
		Model:          "claude-sonnet-4.6",
		TestCount:      10,
		PassCount:      8,
		PassRate:       0.8,
		TotalCost:      0.20,
		ExcludedCount:  5,
		AttemptedCount: 15,
	}
	v := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"suite-a"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  10,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {"claude-code": stats},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-a": {"claude-sonnet-4.6": {"claude-code": stats}},
		},
	}

	output := resultsummary.RenderUserSummary(v)

	// ExcludedCount=5: sample-size annotation would be "(10/15)" or "(10)".
	if strings.Contains(output, "(10/15)") {
		t.Errorf("RenderUserSummary output must not contain reduced-sample indicator (10/15); "+
			"got:\n%s", output)
	}
	if strings.Contains(output, "(10)") {
		t.Errorf("RenderUserSummary output must not contain sample-size annotation (10); "+
			"got:\n%s", output)
	}
}

// TestRenderUserSummary_Deterministic verifies that calling RenderUserSummary
// twice with the same input produces byte-identical output.
//
// FAILS in RED phase: stub returns "" for both calls, which are equal, so
// this test passes vacuously. It provides real protection once implementation
// exists.
func TestRenderUserSummary_Deterministic(t *testing.T) {
	v := userSummaryFixture()
	first := resultsummary.RenderUserSummary(v)
	second := resultsummary.RenderUserSummary(v)
	if first != second {
		t.Errorf("RenderUserSummary is not deterministic: two calls with same input produced different output.\nFirst:\n%s\nSecond:\n%s",
			first, second)
	}
}

// TestRenderUserSummary_SectionOrder verifies that the four user-facing sections
// appear in the correct order: overview, then model-comparison, then
// harness-comparison, then model-results.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub) so index lookups
// return -1 and the ordering assertion fails.
func TestRenderUserSummary_SectionOrder(t *testing.T) {
	v := userSummaryFixture()
	output := resultsummary.RenderUserSummary(v)

	overviewIdx := strings.Index(output, "<!-- generated:overview -->")
	modelCompIdx := strings.Index(output, "<!-- generated:model-comparison -->")
	harnessCompIdx := strings.Index(output, "<!-- generated:harness-comparison -->")
	modelResultsIdx := strings.Index(output, "<!-- generated:model-results -->")

	if overviewIdx < 0 {
		t.Fatal("generated:overview marker not found in RenderUserSummary output")
	}
	if modelCompIdx < 0 {
		t.Fatal("generated:model-comparison marker not found in RenderUserSummary output")
	}
	if harnessCompIdx < 0 {
		t.Fatal("generated:harness-comparison marker not found in RenderUserSummary output")
	}
	if modelResultsIdx < 0 {
		t.Fatal("generated:model-results marker not found in RenderUserSummary output")
	}

	if !(overviewIdx < modelCompIdx) {
		t.Errorf("overview section must come before model-comparison: got overview=%d, model-comparison=%d",
			overviewIdx, modelCompIdx)
	}
	if !(modelCompIdx < harnessCompIdx) {
		t.Errorf("model-comparison section must come before harness-comparison: got model-comparison=%d, harness-comparison=%d",
			modelCompIdx, harnessCompIdx)
	}
	if !(harnessCompIdx < modelResultsIdx) {
		t.Errorf("harness-comparison section must come before model-results: got harness-comparison=%d, model-results=%d",
			harnessCompIdx, modelResultsIdx)
	}
}

// TestRenderUserSummary_OverviewSection_ContainsVersionInfo verifies that the
// overview section includes the version string from the input VersionSummary.
//
// FAILS in RED phase: RenderUserSummary returns "" (stub).
func TestRenderUserSummary_OverviewSection_ContainsVersionInfo(t *testing.T) {
	v := userSummaryFixture() // Version: "v2.0.0"
	output := resultsummary.RenderUserSummary(v)

	openMarker := "<!-- generated:overview -->"
	closeMarker := "<!-- /generated:overview -->"

	openIdx := strings.Index(output, openMarker)
	closeIdx := strings.Index(output, closeMarker)
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		t.Fatalf("generated:overview block not found in RenderUserSummary output; got:\n%s", output)
	}
	block := output[openIdx:closeIdx]

	if !strings.Contains(block, "v2.0.0") {
		t.Errorf("overview section must contain version string \"v2.0.0\"; block:\n%s", block)
	}
}
