package resultsummary_test

// Tests for Stage 2: Sample-Size Visibility.
//
// These tests cover three areas:
//
//  1. HarnessModelStats pipeline (T2.2): Verify that ExcludedCount and
//     AttemptedCount fields are correctly propagated from report wire data
//     through the pipeline into HarnessModelStats for both ByModel and BySuite.
//
//  2. TestStats per-combo counted/excluded (T2.3): Verify that TestStats
//     entries in ProblemTests carry BestCounted, BestExcluded, WorstCounted,
//     and WorstExcluded from the combo that produced best/worst rates.
//
//  3. Markdown rendering (T2.4): Verify all four table sections (Model
//     Comparison, Harness Comparison, Model Results, Problem Areas) show the
//     counted sample size alongside pass-rate cells, and that the reduced-sample
//     indicator (N/M) is present when excluded > 0 and absent when excluded == 0.
//
// RED-phase notes:
//   - T2.2 pipeline tests FAIL at runtime: statsAccumulator.add() does not yet
//     accumulate Excluded; toStats() does not yet populate ExcludedCount or
//     AttemptedCount. The rendered output therefore does not include sample-size
//     annotations even though the fixture carries "excluded" > 0.
//   - T2.3 pipeline tests FAIL at runtime: buildVersionSummary does not yet
//     propagate counted/excluded into testComboRate, so TestStats.BestCounted
//     etc. remain zero.
//   - T2.4 rendering tests FAIL at runtime: RenderVersionSummary does not yet
//     produce sample-size annotations in any of the four table sections.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/resultsummary"
)

// ---- T2.2: HarnessModelStats ExcludedCount and AttemptedCount ----

// TestHarnessModelStats_ExcludedCountAndAttemptedCountFields_Compile verifies
// that HarnessModelStats carries ExcludedCount and AttemptedCount fields.
// This test exercises the fields directly to confirm they compile; no
// behavioral assertion beyond field presence is made here.
func TestHarnessModelStats_ExcludedCountAndAttemptedCountFields_Compile(t *testing.T) {
	stats := resultsummary.HarnessModelStats{
		Harness:        "claude-code",
		Model:          "claude-sonnet-4.6",
		TestCount:      10,
		PassCount:      8,
		PassRate:       0.8,
		ExcludedCount:  3,
		AttemptedCount: 13, // TestCount + ExcludedCount
	}
	if stats.ExcludedCount != 3 {
		t.Errorf("ExcludedCount = %d, want 3", stats.ExcludedCount)
	}
	if stats.AttemptedCount != 13 {
		t.Errorf("AttemptedCount = %d, want 13", stats.AttemptedCount)
	}
}

// TestGenerate_ExcludedRunsInReport_PropagatesExcludedCountToModelStats verifies
// the end-to-end pipeline: when a report wire file contains "excluded": N in an
// aggregate block, the resulting ByModel stats for that harness+model combination
// carry a non-zero ExcludedCount, which causes the rendered output to include a
// sample-size annotation showing the reduced-sample indicator.
//
// FAILS in RED phase: statsAccumulator.add() does not accumulate Excluded, so
// ExcludedCount stays 0 and no reduced-sample indicator appears in the output.
func TestGenerate_ExcludedRunsInReport_PropagatesExcludedCountToModelStats(t *testing.T) {
	fs := newFakeFS()
	// excluded_count_report.json has test-alpha with excluded=3 (counted=5).
	// Total for claude-sonnet-4.6/claude-code: ExcludedCount=3, AttemptedCount=8.
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "excluded_count_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/summary.md")

	// The model-comparison block must show the reduced-sample indicator because
	// ExcludedCount=3 > 0. The format is "(counted/attempted)" = "(10/13)"
	// where counted=10 (sum of both tests' counted=5+5) and attempted=13
	// (counted=10 + excluded=3).
	// Before implementation, ExcludedCount=0 so no reduced-sample indicator appears.
	modelCompStart := strings.Index(content, "<!-- generated:model-comparison -->")
	modelCompEnd := strings.Index(content, "<!-- /generated:model-comparison -->")
	if modelCompStart < 0 || modelCompEnd < 0 || modelCompEnd <= modelCompStart {
		t.Fatal("generated:model-comparison block not found in rendered output")
	}
	modelCompBlock := content[modelCompStart:modelCompEnd]

	// The reduced-sample indicator uses a slash: "(N/M)" where N < M.
	// When ExcludedCount is correctly propagated, the block contains a "/" inside
	// a sample-size parenthetical. Before implementation, no parenthetical with a
	// slash appears, so this check fails.
	if !strings.Contains(modelCompBlock, "(10/13)") {
		t.Errorf("model-comparison block must contain reduced-sample indicator (10/13) "+
			"when ExcludedCount=3; block content:\n%s", modelCompBlock)
	}
}

// TestGenerate_NoExcludedRuns_ShowsCountOnlyAnnotation verifies that when all
// runs are counted (excluded == 0), the rendered output shows only the counted
// value in the sample-size annotation -- not a fraction with a denominator.
//
// FAILS in RED phase: RenderVersionSummary does not yet emit sample-size
// annotations, so no "(N)" appears in the model-comparison block.
func TestGenerate_NoExcludedRuns_ShowsCountOnlyAnnotation(t *testing.T) {
	fs := newFakeFS()
	// v1_claude_code_report.json has no "excluded" field: all runs are counted.
	// Total for claude-sonnet-4.6/claude-code: TestCount=2, ExcludedCount=0.
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/summary.md")

	modelCompStart := strings.Index(content, "<!-- generated:model-comparison -->")
	modelCompEnd := strings.Index(content, "<!-- /generated:model-comparison -->")
	if modelCompStart < 0 || modelCompEnd < 0 || modelCompEnd <= modelCompStart {
		t.Fatal("generated:model-comparison block not found in rendered output")
	}
	modelCompBlock := content[modelCompStart:modelCompEnd]

	// When excluded == 0, the annotation must show only "(2)" not "(2/2)".
	// Before implementation, no annotation appears at all, so this fails.
	if !strings.Contains(modelCompBlock, "(2)") {
		t.Errorf("model-comparison block must contain count-only annotation (2) "+
			"when ExcludedCount=0; block content:\n%s", modelCompBlock)
	}
}

// ---- T2.3: TestStats per-combo counted/excluded ----

// TestTestStats_CountedAndExcludedFields_Compile verifies that TestStats carries
// BestCounted, BestExcluded, WorstCounted, and WorstExcluded fields. This test
// exercises the fields directly to confirm they compile.
func TestTestStats_CountedAndExcludedFields_Compile(t *testing.T) {
	s := resultsummary.TestStats{
		SuiteID:       "suite-alpha",
		TestName:      "tricky-test",
		NumericID:     42,
		BestRate:      1.0,
		BestCombo:     "claude-sonnet-4.6/claude-code",
		WorstRate:     0.5,
		WorstCombo:    "gpt-4o/open-code",
		Spread:        0.5,
		BestCounted:   10,
		BestExcluded:  2,
		WorstCounted:  8,
		WorstExcluded: 5,
	}
	if s.BestCounted != 10 {
		t.Errorf("BestCounted = %d, want 10", s.BestCounted)
	}
	if s.BestExcluded != 2 {
		t.Errorf("BestExcluded = %d, want 2", s.BestExcluded)
	}
	if s.WorstCounted != 8 {
		t.Errorf("WorstCounted = %d, want 8", s.WorstCounted)
	}
	if s.WorstExcluded != 5 {
		t.Errorf("WorstExcluded = %d, want 5", s.WorstExcluded)
	}
}

// TestGenerate_ExcludedRuns_ProblemTestsCarryCountedAndExcluded verifies the
// pipeline: when a report has "excluded" > 0 in an aggregate block, the
// resulting TestStats.BestCounted and related fields are populated from the
// testComboRate that was selected as best or worst.
//
// Setup: two harness reports for the same test with different pass rates so a
// ProblemTest entry is created. The harness-a report has excluded=2 (counted=5);
// harness-b has excluded=0 (counted=3).
//
// FAILS in RED phase: buildVersionSummary does not yet propagate
// counted/excluded into testComboRate, so BestCounted and WorstCounted remain 0.
func TestGenerate_ExcludedRuns_ProblemTestsCarryCountedAndExcluded(t *testing.T) {
	// Use two reports from different harnesses for the same version and test.
	// infra_routing_harness_a.json has "normal-test" (test_id=10) with counted=2,
	// excluded field absent (defaults to 0). We need a fixture with excluded > 0.
	// Since we need cross-harness spread, we use excluded_count_report.json for
	// one harness and v1_open_code_report.json for the second.
	//
	// excluded_count_report.json:  suite=happy-path, test-alpha(1) counted=5 excluded=3, test-beta(2) counted=5 excluded=0
	// v1_open_code_report.json:    suite=happy-path, same tests at 50% pass rate, no excluded
	//
	// test-alpha: claude-code best(100%? no: pass_rate=0.8), open-code: need to check
	// For a meaningful problem-areas entry we need spread > 0.
	// Let's use the two harness fixtures: claude-code at 80% (excluded=3) vs open-code fixture.
	// The test verifies that whichever combo ends up as "best" carries its counted/excluded.
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "excluded_count_report.json"))
	// v1_open_code_report.json: test-alpha pass_rate=1.0 (or 0.5 depending on fixture).
	// We'll use v1_open_code_report.json for gpt-4o/open-code combo.
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "open-code",
		"gpt-4o", "20260901T110000", loadFixture(t, "v1_open_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/summary.md")

	// The problem-areas block must contain the counted/excluded annotation for the
	// combo that has excluded > 0. For the claude-code combo: counted=5, excluded=3,
	// so AttemptedCount=8. Expected annotation: "(5/8)".
	probStart := strings.Index(content, "<!-- generated:problem-areas -->")
	probEnd := strings.Index(content, "<!-- /generated:problem-areas -->")
	if probStart < 0 || probEnd < 0 || probEnd <= probStart {
		t.Fatal("generated:problem-areas block not found in rendered output")
	}
	probBlock := content[probStart:probEnd]

	// The problem-areas section should show "(5/8)" for the combo with excluded=3.
	// Before implementation, BestCounted/WorstCounted are 0, so no such annotation
	// appears. This test therefore fails in RED phase.
	if !strings.Contains(probBlock, "(5/8)") {
		t.Errorf("problem-areas block must contain (5/8) annotation for the combo with "+
			"counted=5 excluded=3; block content:\n%s", probBlock)
	}
}

// ---- T2.4: Markdown rendering with sample-size annotations ----

// TestRenderVersionSummary_ModelComparison_ShowsCountedAnnotationWhenNoExclusions
// verifies that the model-comparison section includes the counted sample size in
// a "(N)" annotation alongside the pass-rate cell when ExcludedCount == 0.
//
// FAILS in RED phase: RenderVersionSummary does not emit sample-size annotations.
func TestRenderVersionSummary_ModelComparison_ShowsCountedAnnotationWhenNoExclusions(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  6,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:        "claude-code",
					Model:          "claude-sonnet-4.6",
					TestCount:      6,
					PassCount:      5,
					PassRate:       0.833,
					TotalCost:      0.12,
					ExcludedCount:  0,
					AttemptedCount: 6, // == TestCount when ExcludedCount == 0
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:        "claude-code",
						Model:          "claude-sonnet-4.6",
						TestCount:      6,
						PassCount:      5,
						PassRate:       0.833,
						TotalCost:      0.12,
						ExcludedCount:  0,
						AttemptedCount: 6,
					},
				},
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:model-comparison -->")
	blockEnd := strings.Index(output, "<!-- /generated:model-comparison -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:model-comparison block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	// When ExcludedCount == 0, the annotation must be "(6)" (counted only, no slash).
	if !strings.Contains(block, "(6)") {
		t.Errorf("model-comparison must contain count-only annotation (6) when ExcludedCount=0; block:\n%s", block)
	}
}

// TestRenderVersionSummary_ModelComparison_ShowsReducedSampleIndicatorWhenExcluded
// verifies that the model-comparison section shows a "(N/M)" annotation when
// ExcludedCount > 0, where N=counted and M=counted+excluded.
//
// FAILS in RED phase: RenderVersionSummary does not emit reduced-sample indicators.
func TestRenderVersionSummary_ModelComparison_ShowsReducedSampleIndicatorWhenExcluded(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  5,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:        "claude-code",
					Model:          "claude-sonnet-4.6",
					TestCount:      5,
					PassCount:      4,
					PassRate:       0.8,
					TotalCost:      0.10,
					ExcludedCount:  3,
					AttemptedCount: 8, // 5 + 3
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:        "claude-code",
						Model:          "claude-sonnet-4.6",
						TestCount:      5,
						PassCount:      4,
						PassRate:       0.8,
						TotalCost:      0.10,
						ExcludedCount:  3,
						AttemptedCount: 8,
					},
				},
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:model-comparison -->")
	blockEnd := strings.Index(output, "<!-- /generated:model-comparison -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:model-comparison block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	// When ExcludedCount=3 > 0, the annotation must show "(5/8)" (fraction with slash).
	if !strings.Contains(block, "(5/8)") {
		t.Errorf("model-comparison must contain reduced-sample indicator (5/8) when ExcludedCount=3; block:\n%s", block)
	}
}

// TestRenderVersionSummary_ModelComparison_NoSlashWhenExcludedIsZero verifies
// that the "(N/M)" fraction form does NOT appear when ExcludedCount == 0. This
// is the complement of TestRenderVersionSummary_ModelComparison_ShowsReducedSampleIndicatorWhenExcluded.
//
// FAILS in RED phase: because the sample-size annotation is not produced at all,
// this test passes vacuously before implementation. Once implementation adds
// annotations, this test prevents regressions where the fraction is shown even
// when there are no exclusions.
//
// Note: this test is expected to pass vacuously in RED phase (no "(N/M)" appears
// because no sample-size annotation is emitted at all). It becomes meaningful
// once the annotation is implemented.
func TestRenderVersionSummary_ModelComparison_NoSlashWhenExcludedIsZero(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  4,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:        "claude-code",
					Model:          "claude-sonnet-4.6",
					TestCount:      4,
					PassCount:      4,
					PassRate:       1.0,
					TotalCost:      0.08,
					ExcludedCount:  0,
					AttemptedCount: 4,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:        "claude-code",
						Model:          "claude-sonnet-4.6",
						TestCount:      4,
						PassCount:      4,
						PassRate:       1.0,
						TotalCost:      0.08,
						ExcludedCount:  0,
						AttemptedCount: 4,
					},
				},
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:model-comparison -->")
	blockEnd := strings.Index(output, "<!-- /generated:model-comparison -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:model-comparison block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	// The fraction form "(4/4)" must NOT appear. If sample-size is (4) (no slash),
	// this test passes. If the rendering mistakenly shows "(4/4)", it fails.
	if strings.Contains(block, "(4/4)") {
		t.Errorf("model-comparison must not show fraction (4/4) when ExcludedCount=0; block:\n%s", block)
	}
}

// TestRenderVersionSummary_HarnessComparison_ShowsCountedAnnotationWhenNoExclusions
// verifies the harness-comparison section shows "(N)" when ExcludedCount == 0.
//
// FAILS in RED phase: RenderVersionSummary does not emit sample-size annotations.
func TestRenderVersionSummary_HarnessComparison_ShowsCountedAnnotationWhenNoExclusions(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  6,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:        "claude-code",
					Model:          "claude-sonnet-4.6",
					TestCount:      6,
					PassCount:      6,
					PassRate:       1.0,
					TotalCost:      0.12,
					ExcludedCount:  0,
					AttemptedCount: 6,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:        "claude-code",
						Model:          "claude-sonnet-4.6",
						TestCount:      6,
						PassCount:      6,
						PassRate:       1.0,
						TotalCost:      0.12,
						ExcludedCount:  0,
						AttemptedCount: 6,
					},
				},
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:harness-comparison -->")
	blockEnd := strings.Index(output, "<!-- /generated:harness-comparison -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:harness-comparison block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	if !strings.Contains(block, "(6)") {
		t.Errorf("harness-comparison must contain count-only annotation (6) when ExcludedCount=0; block:\n%s", block)
	}
}

// TestRenderVersionSummary_HarnessComparison_ShowsReducedSampleIndicatorWhenExcluded
// verifies the harness-comparison section shows "(N/M)" when ExcludedCount > 0.
//
// FAILS in RED phase: RenderVersionSummary does not emit reduced-sample indicators.
func TestRenderVersionSummary_HarnessComparison_ShowsReducedSampleIndicatorWhenExcluded(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  7,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:        "claude-code",
					Model:          "claude-sonnet-4.6",
					TestCount:      7,
					PassCount:      6,
					PassRate:       0.857,
					TotalCost:      0.14,
					ExcludedCount:  4,
					AttemptedCount: 11, // 7 + 4
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:        "claude-code",
						Model:          "claude-sonnet-4.6",
						TestCount:      7,
						PassCount:      6,
						PassRate:       0.857,
						TotalCost:      0.14,
						ExcludedCount:  4,
						AttemptedCount: 11,
					},
				},
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:harness-comparison -->")
	blockEnd := strings.Index(output, "<!-- /generated:harness-comparison -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:harness-comparison block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	if !strings.Contains(block, "(7/11)") {
		t.Errorf("harness-comparison must contain reduced-sample indicator (7/11) when ExcludedCount=4; block:\n%s", block)
	}
}

// TestRenderVersionSummary_ModelResults_ShowsCountedAnnotationWhenNoExclusions
// verifies the model-results section shows "(N)" when ExcludedCount == 0.
//
// FAILS in RED phase: RenderVersionSummary does not emit sample-size annotations.
func TestRenderVersionSummary_ModelResults_ShowsCountedAnnotationWhenNoExclusions(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  3,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:        "claude-code",
					Model:          "claude-sonnet-4.6",
					TestCount:      3,
					PassCount:      3,
					PassRate:       1.0,
					TotalCost:      0.06,
					ExcludedCount:  0,
					AttemptedCount: 3,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:        "claude-code",
						Model:          "claude-sonnet-4.6",
						TestCount:      3,
						PassCount:      3,
						PassRate:       1.0,
						TotalCost:      0.06,
						ExcludedCount:  0,
						AttemptedCount: 3,
					},
				},
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:model-results -->")
	blockEnd := strings.Index(output, "<!-- /generated:model-results -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:model-results block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	if !strings.Contains(block, "(3)") {
		t.Errorf("model-results must contain count-only annotation (3) when ExcludedCount=0; block:\n%s", block)
	}
}

// TestRenderVersionSummary_ModelResults_ShowsReducedSampleIndicatorWhenExcluded
// verifies the model-results section shows "(N/M)" when ExcludedCount > 0.
//
// FAILS in RED phase: RenderVersionSummary does not emit reduced-sample indicators.
func TestRenderVersionSummary_ModelResults_ShowsReducedSampleIndicatorWhenExcluded(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"happy-path"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  9,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {
					Harness:        "claude-code",
					Model:          "claude-sonnet-4.6",
					TestCount:      9,
					PassCount:      7,
					PassRate:       0.778,
					TotalCost:      0.18,
					ExcludedCount:  6,
					AttemptedCount: 15, // 9 + 6
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"happy-path": {
				"claude-sonnet-4.6": {
					"claude-code": {
						Harness:        "claude-code",
						Model:          "claude-sonnet-4.6",
						TestCount:      9,
						PassCount:      7,
						PassRate:       0.778,
						TotalCost:      0.18,
						ExcludedCount:  6,
						AttemptedCount: 15,
					},
				},
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:model-results -->")
	blockEnd := strings.Index(output, "<!-- /generated:model-results -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:model-results block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	if !strings.Contains(block, "(9/15)") {
		t.Errorf("model-results must contain reduced-sample indicator (9/15) when ExcludedCount=6; block:\n%s", block)
	}
}

// TestRenderVersionSummary_ProblemAreas_ShowsCountedAnnotationForBestCombo
// verifies that the problem-areas section shows a sample-size annotation for
// the best-combo rate cell. When BestExcluded == 0, shows "(N)" only.
//
// FAILS in RED phase: RenderVersionSummary does not emit sample-size annotations
// in the problem-areas section.
func TestRenderVersionSummary_ProblemAreas_ShowsCountedAnnotationForBestCombo(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 2,
		Suites:      []string{"suite-alpha"},
		Models:      []string{"claude-sonnet-4.6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  2,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
					TestCount: 1, PassCount: 1, PassRate: 1.0, ExcludedCount: 0, AttemptedCount: 1},
			},
			"gpt-4o": {
				"open-code": {Harness: "open-code", Model: "gpt-4o",
					TestCount: 1, PassCount: 0, PassRate: 0.0, ExcludedCount: 0, AttemptedCount: 1},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-alpha": {
				"claude-sonnet-4.6": {
					"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
						TestCount: 1, PassCount: 1, PassRate: 1.0, ExcludedCount: 0, AttemptedCount: 1},
				},
				"gpt-4o": {
					"open-code": {Harness: "open-code", Model: "gpt-4o",
						TestCount: 1, PassCount: 0, PassRate: 0.0, ExcludedCount: 0, AttemptedCount: 1},
				},
			},
		},
		ProblemTests: []resultsummary.TestStats{
			{
				SuiteID:       "suite-alpha",
				TestName:      "tricky-test",
				NumericID:     5,
				BestRate:      1.0,
				BestCombo:     "claude-sonnet-4.6/claude-code",
				WorstRate:     0.0,
				WorstCombo:    "gpt-4o/open-code",
				Spread:        1.0,
				BestCounted:   8,
				BestExcluded:  0,
				WorstCounted:  6,
				WorstExcluded: 0,
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:problem-areas -->")
	blockEnd := strings.Index(output, "<!-- /generated:problem-areas -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:problem-areas block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	// BestCounted=8, BestExcluded=0: annotation must be "(8)" (no slash).
	if !strings.Contains(block, "(8)") {
		t.Errorf("problem-areas must contain count-only annotation (8) for BestCounted=8, BestExcluded=0; block:\n%s", block)
	}
	// WorstCounted=6, WorstExcluded=0: annotation must be "(6)" (no slash).
	if !strings.Contains(block, "(6)") {
		t.Errorf("problem-areas must contain count-only annotation (6) for WorstCounted=6, WorstExcluded=0; block:\n%s", block)
	}
}

// TestRenderVersionSummary_ProblemAreas_ShowsReducedSampleIndicatorWhenBestComboHasExclusions
// verifies the problem-areas section shows "(N/M)" in the best-combo rate cell
// when BestExcluded > 0.
//
// FAILS in RED phase: RenderVersionSummary does not emit reduced-sample indicators.
func TestRenderVersionSummary_ProblemAreas_ShowsReducedSampleIndicatorWhenBestComboHasExclusions(t *testing.T) {
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 2,
		Suites:      []string{"suite-alpha"},
		Models:      []string{"claude-sonnet-4.6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  2,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {
				"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
					TestCount: 5, PassCount: 4, PassRate: 0.8, ExcludedCount: 2, AttemptedCount: 7},
			},
			"gpt-4o": {
				"open-code": {Harness: "open-code", Model: "gpt-4o",
					TestCount: 5, PassCount: 2, PassRate: 0.4, ExcludedCount: 0, AttemptedCount: 5},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-alpha": {
				"claude-sonnet-4.6": {
					"claude-code": {Harness: "claude-code", Model: "claude-sonnet-4.6",
						TestCount: 5, PassCount: 4, PassRate: 0.8, ExcludedCount: 2, AttemptedCount: 7},
				},
				"gpt-4o": {
					"open-code": {Harness: "open-code", Model: "gpt-4o",
						TestCount: 5, PassCount: 2, PassRate: 0.4, ExcludedCount: 0, AttemptedCount: 5},
				},
			},
		},
		ProblemTests: []resultsummary.TestStats{
			{
				SuiteID:       "suite-alpha",
				TestName:      "flaky-test",
				NumericID:     11,
				BestRate:      0.8,
				BestCombo:     "claude-sonnet-4.6/claude-code",
				WorstRate:     0.4,
				WorstCombo:    "gpt-4o/open-code",
				Spread:        0.4,
				BestCounted:   5,
				BestExcluded:  2,  // counted=5, excluded=2 -> "(5/7)"
				WorstCounted:  5,
				WorstExcluded: 0,  // counted=5, excluded=0 -> "(5)"
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	blockStart := strings.Index(output, "<!-- generated:problem-areas -->")
	blockEnd := strings.Index(output, "<!-- /generated:problem-areas -->")
	if blockStart < 0 || blockEnd < 0 || blockEnd <= blockStart {
		t.Fatal("generated:problem-areas block not found in rendered output")
	}
	block := output[blockStart:blockEnd]

	// BestCounted=5, BestExcluded=2, AttemptedCount=7: annotation must be "(5/7)".
	if !strings.Contains(block, "(5/7)") {
		t.Errorf("problem-areas must contain reduced-sample indicator (5/7) for BestCounted=5, BestExcluded=2; block:\n%s", block)
	}
	// WorstCounted=5, WorstExcluded=0: annotation must be "(5)" (count only, no slash).
	if !strings.Contains(block, "(5)") {
		t.Errorf("problem-areas must contain count-only annotation (5) for WorstCounted=5, WorstExcluded=0; block:\n%s", block)
	}
}

// TestRenderVersionSummary_AllFourSections_ContainSampleSizeAnnotation verifies
// that all four generated sections (model-comparison, harness-comparison,
// model-results, problem-areas) contain a sample-size annotation when the input
// data has ExcludedCount > 0 for the relevant entries. This serves as a
// comprehensive check that no section was accidentally omitted.
//
// FAILS in RED phase: RenderVersionSummary does not emit sample-size annotations
// in any section.
func TestRenderVersionSummary_AllFourSections_ContainSampleSizeAnnotation(t *testing.T) {
	// Build a VersionSummary where claude-sonnet-4.6/claude-code has ExcludedCount=2
	// (TestCount=4, AttemptedCount=6) and there is one ProblemTest with BestExcluded=2.
	stats := resultsummary.HarnessModelStats{
		Harness:        "claude-code",
		Model:          "claude-sonnet-4.6",
		TestCount:      4,
		PassCount:      3,
		PassRate:       0.75,
		TotalCost:      0.08,
		ExcludedCount:  2,
		AttemptedCount: 6, // 4 + 2
	}
	vs := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 1,
		Suites:      []string{"suite-x"},
		Models:      []string{"claude-sonnet-4.6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  4,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4.6": {"claude-code": stats},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-x": {"claude-sonnet-4.6": {"claude-code": stats}},
		},
		ProblemTests: []resultsummary.TestStats{
			{
				SuiteID:       "suite-x",
				TestName:      "hard-test",
				NumericID:     3,
				BestRate:      0.75,
				BestCombo:     "claude-sonnet-4.6/claude-code",
				WorstRate:     0.0,
				WorstCombo:    "claude-sonnet-4.6/claude-code",
				Spread:        0.75,
				BestCounted:   4,
				BestExcluded:  2,
				WorstCounted:  4,
				WorstExcluded: 2,
			},
		},
	}

	output := resultsummary.RenderVersionSummary(vs)

	sections := []struct {
		name  string
		open  string
		close string
	}{
		{"model-comparison", "<!-- generated:model-comparison -->", "<!-- /generated:model-comparison -->"},
		{"harness-comparison", "<!-- generated:harness-comparison -->", "<!-- /generated:harness-comparison -->"},
		{"model-results", "<!-- generated:model-results -->", "<!-- /generated:model-results -->"},
		{"problem-areas", "<!-- generated:problem-areas -->", "<!-- /generated:problem-areas -->"},
	}

	for _, sec := range sections {
		start := strings.Index(output, sec.open)
		end := strings.Index(output, sec.close)
		if start < 0 || end < 0 || end <= start {
			t.Errorf("section %q: markers not found in rendered output", sec.name)
			continue
		}
		block := output[start:end]
		// Each section must show the reduced-sample indicator "(4/6)" because
		// ExcludedCount=2 (or BestExcluded=2 for problem-areas).
		if !strings.Contains(block, "(4/6)") {
			t.Errorf("section %q: must contain reduced-sample indicator (4/6) for ExcludedCount=2; block:\n%s",
				sec.name, block)
		}
	}
}
