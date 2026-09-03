package resultsummary_test

// Tests for Sample-Size Visibility.
//
// These tests cover three areas:
//
//  1. HarnessModelStats pipeline: Verify that ExcludedCount and
//     AttemptedCount fields are correctly propagated from report wire data
//     through the pipeline into HarnessModelStats for both ByModel and BySuite.
//
//  2. TestStats per-combo counted/excluded: Verify that TestStats
//     entries in ProblemTests carry BestCounted, BestExcluded, WorstCounted,
//     and WorstExcluded from the combo that produced best/worst rates.
//
//  3. Markdown rendering: Sample-size annotations (N/M format) appear only in
//     internal-summary.md (problem-areas section). user-summary.md intentionally
//     omits sample-size annotations from model-comparison, harness-comparison,
//     and model-results sections.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/resultsummary"
)

// ---- HarnessModelStats ExcludedCount and AttemptedCount ----

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
// aggregate block, Generate produces user-summary.md without sample-size
// annotations (user-facing) and internal-summary.md with exclusion detail.
// The ExcludedCount is propagated through the data model even though
// user-summary.md intentionally omits the (N/M) annotation.
func TestGenerate_ExcludedRunsInReport_PropagatesExcludedCountToModelStats(t *testing.T) {
	fs := newFakeFS()
	// excluded_count_report.json has test-alpha with excluded=3 (counted=5).
	// Total for claude-sonnet-4.6/claude-code: ExcludedCount=3, AttemptedCount=8.
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "excluded_count_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	result, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	// Both user-summary.md and internal-summary.md must be produced.
	allFiles := append(result.FilesWritten, result.FilesUpdated...)
	if !containsPath(allFiles, "/TestResults/Orchestrator/v1.0.0/user-summary.md") {
		t.Errorf("Generate must write user-summary.md; got: %v", allFiles)
	}
	if !containsPath(allFiles, "/TestResults/Orchestrator/v1.0.0/internal-summary.md") {
		t.Errorf("Generate must write internal-summary.md; got: %v", allFiles)
	}

	// user-summary.md must NOT contain (N/M) annotations in model-comparison --
	// user-facing output shows percentage-only pass rates.
	userContent := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")
	modelCompStart := strings.Index(userContent, "<!-- generated:model-comparison -->")
	modelCompEnd := strings.Index(userContent, "<!-- /generated:model-comparison -->")
	if modelCompStart < 0 || modelCompEnd < 0 || modelCompEnd <= modelCompStart {
		t.Fatal("generated:model-comparison block not found in user-summary.md")
	}
	modelCompBlock := userContent[modelCompStart:modelCompEnd]

	// User summary must not show (N/M) sample-size annotations.
	if strings.Contains(modelCompBlock, "(10/13)") || strings.Contains(modelCompBlock, "/13)") {
		t.Errorf("user-summary.md model-comparison must not contain sample-size annotations (N/M); block:\n%s", modelCompBlock)
	}
}

// TestGenerate_NoExcludedRuns_NoSampleSizeAnnotation verifies that when all
// runs are counted (excluded == 0), the user-facing summary does not include
// any sample-size annotation in the model-comparison block.
func TestGenerate_NoExcludedRuns_NoSampleSizeAnnotation(t *testing.T) {
	fs := newFakeFS()
	// v1_claude_code_report.json has no "excluded" field: all runs are counted.
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260820T100000", loadFixture(t, "v1_claude_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/user-summary.md")

	modelCompStart := strings.Index(content, "<!-- generated:model-comparison -->")
	modelCompEnd := strings.Index(content, "<!-- /generated:model-comparison -->")
	if modelCompStart < 0 || modelCompEnd < 0 || modelCompEnd <= modelCompStart {
		t.Fatal("generated:model-comparison block not found in user-summary.md")
	}
	modelCompBlock := content[modelCompStart:modelCompEnd]

	// User-summary must not have parenthetical sample-size annotations like "(2)" or "(2/2)".
	// The model-comparison rows show pass rate percentage only.
	if strings.Contains(modelCompBlock, "(2)") {
		t.Errorf("user-summary.md model-comparison must not contain sample-size annotation (2); block:\n%s", modelCompBlock)
	}
}

// ---- TestStats per-combo counted/excluded ----

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
// testComboRate that was selected as best or worst. The counted/excluded
// annotation appears in the internal-summary.md problem-areas section.
//
// Setup: two harness reports for the same test with different pass rates so a
// ProblemTest entry is created. The harness-a report has excluded=2 (counted=5);
// harness-b has excluded=0 (counted=3).
func TestGenerate_ExcludedRuns_ProblemTestsCarryCountedAndExcluded(t *testing.T) {
	// Use two reports from different harnesses for the same version and test.
	// excluded_count_report.json:  suite=happy-path, test-alpha(1) counted=5 excluded=3, test-beta(2) counted=5 excluded=0
	// v1_open_code_report.json:    suite=happy-path, same tests at 50% pass rate, no excluded
	//
	// The test verifies that whichever combo ends up as "best" carries its counted/excluded.
	fs := newFakeFS()
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "claude-code",
		"claude-sonnet-4.6", "20260901T100000", loadFixture(t, "excluded_count_report.json"))
	seedReport(fs, "/TestResults", "v1.0.0", "happy-path", "open-code",
		"gpt-4o", "20260901T110000", loadFixture(t, "v1_open_code_report.json"))

	req := resultsummary.SummaryRequest{TestResultsRoot: "/TestResults"}
	_, err := resultsummary.Generate(fs, req)
	if err != nil {
		t.Fatalf("Generate returned unexpected error: %v", err)
	}

	content := readWrittenFile(t, fs, "/TestResults/Orchestrator/v1.0.0/internal-summary.md")

	// The problem-areas block must contain the counted/excluded annotation for the
	// combo that has excluded > 0. For the claude-code combo: counted=5, excluded=3,
	// so AttemptedCount=8. Expected annotation: "(5/8)".
	probStart := strings.Index(content, "<!-- generated:problem-areas -->")
	probEnd := strings.Index(content, "<!-- /generated:problem-areas -->")
	if probStart < 0 || probEnd < 0 || probEnd <= probStart {
		t.Fatal("generated:problem-areas block not found in internal-summary.md")
	}
	probBlock := content[probStart:probEnd]

	// The problem-areas section should show "(5/8)" for the combo with excluded=3.
	if !strings.Contains(probBlock, "(5/8)") {
		t.Errorf("problem-areas block must contain (5/8) annotation for the combo with "+
			"counted=5 excluded=3; block content:\n%s", probBlock)
	}
}

// ---- Markdown rendering with sample-size annotations (problem-areas only) ----

// TestRenderInternalSummary_ProblemAreas_ShowsCountedAnnotationForBestCombo
// verifies that the problem-areas section shows a sample-size annotation for
// the best-combo rate cell. When BestExcluded == 0, shows "(N)" only.
func TestRenderInternalSummary_ProblemAreas_ShowsCountedAnnotationForBestCombo(t *testing.T) {
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

	output := resultsummary.RenderInternalSummary(vs)

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

// TestRenderInternalSummary_ProblemAreas_ShowsReducedSampleIndicatorWhenBestComboHasExclusions
// verifies the problem-areas section shows "(N/M)" in the best-combo rate cell
// when BestExcluded > 0.
func TestRenderInternalSummary_ProblemAreas_ShowsReducedSampleIndicatorWhenBestComboHasExclusions(t *testing.T) {
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

	output := resultsummary.RenderInternalSummary(vs)

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
