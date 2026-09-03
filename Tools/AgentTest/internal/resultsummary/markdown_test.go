package resultsummary_test

// Tests for Stage 1 Report Content changes to RenderUserSummary.
//
// These tests specify the desired rendering behavior after the bug-fix batch:
//   - No [partial] suffix appears in any table output.
//   - No Avg Duration column in Model Results.
//   - Section order is Model Comparison, then Harness Comparison, then Model Results.
//   - Cost cells show per-100-tests format ($X.XX/100t).
//
// All tests in this file are in TDD RED phase: they compile and fail against
// the current implementation, and pass once the implementation is corrected.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/resultsummary"
)

// stage1VersionSummary builds a VersionSummary with known values for
// asserting Stage 1 rendering changes. HasPartial=true and AvgDuration=2s
// are set so that the [partial] and duration removal tests can assert their
// absence.
func stage1VersionSummary() resultsummary.VersionSummary {
	stats := resultsummary.HarnessModelStats{
		Harness:     "claude-code",
		Model:       "claude-sonnet-4-6",
		TestCount:   10,
		PassCount:   8,
		PassRate:    0.8,
		AvgDuration: 2 * time.Second,
		TotalCost:   1.50,
		CostWarning: false,
		HasPartial:  true,
	}
	return resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 3,
		Suites:      []string{"suite-a"},
		Models:      []string{"claude-sonnet-4-6"},
		Harnesses:   []string{"claude-code"},
		TotalTests:  10,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4-6": {
				"claude-code": stats,
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-a": {
				"claude-sonnet-4-6": {
					"claude-code": stats,
				},
			},
		},
	}
}

// TestRenderUserSummary_NoPartialSuffix asserts that the rendered output
// contains no "[partial]" substring anywhere, even when HasPartial is true.
// This fails against the current implementation which calls partialSuffix and
// appends " [partial]" to pass-rate cells in all three section renderers.
func TestRenderUserSummary_NoPartialSuffix(t *testing.T) {
	v := stage1VersionSummary()
	out := resultsummary.RenderUserSummary(v)

	if strings.Contains(out, "[partial]") {
		t.Errorf("output contains [partial] suffix; it must not appear in any table.\ngot:\n%s", out)
	}
}

// TestRenderUserSummary_NoAvgDurationColumn asserts that the Model Results
// table has no "Avg Duration" column header and no duration cell value. The
// fixture's AvgDuration is 2s; both the column header and any "2s" cell must
// be absent after the fix.
func TestRenderUserSummary_NoAvgDurationColumn(t *testing.T) {
	v := stage1VersionSummary()
	out := resultsummary.RenderUserSummary(v)

	if strings.Contains(out, "Avg Duration") {
		t.Errorf("output contains \"Avg Duration\" column header; it must be removed from Model Results.\ngot:\n%s", out)
	}
	// AvgDuration=2s in fixture; the formatted value "2s" should not appear as
	// a duration cell once the column is removed.
	if strings.Contains(out, " 2s ") {
		t.Errorf("output contains duration cell value \" 2s \"; Avg Duration column must be removed.\ngot:\n%s", out)
	}
}

// TestRenderUserSummary_SectionHeaderOrder asserts that the three comparison
// section headers appear in the required order: Model Comparison before Harness
// Comparison, and Harness Comparison before Model Results. The current
// implementation emits Model Results first; this test fails against it.
func TestRenderUserSummary_SectionHeaderOrder(t *testing.T) {
	v := stage1VersionSummary()
	out := resultsummary.RenderUserSummary(v)

	mcIdx := strings.Index(out, "## Model Comparison")
	hcIdx := strings.Index(out, "## Harness Comparison")
	mrIdx := strings.Index(out, "## Model Results")

	if mcIdx == -1 {
		t.Fatal("output missing \"## Model Comparison\" section")
	}
	if hcIdx == -1 {
		t.Fatal("output missing \"## Harness Comparison\" section")
	}
	if mrIdx == -1 {
		t.Fatal("output missing \"## Model Results\" section")
	}

	if !(mcIdx < hcIdx) {
		t.Errorf("Model Comparison (at %d) must appear before Harness Comparison (at %d)", mcIdx, hcIdx)
	}
	if !(hcIdx < mrIdx) {
		t.Errorf("Harness Comparison (at %d) must appear before Model Results (at %d)", hcIdx, mrIdx)
	}
}

// TestRenderUserSummary_MarkerOrder asserts that the generated marker
// pairs appear in the required order matching the corrected section order.
// Both opening markers and closing markers must be present and correctly
// sequenced. This fails against the current implementation because the
// generated:model-results marker currently precedes generated:model-comparison.
func TestRenderUserSummary_MarkerOrder(t *testing.T) {
	v := stage1VersionSummary()
	out := resultsummary.RenderUserSummary(v)

	mcMarker := strings.Index(out, "<!-- generated:model-comparison -->")
	hcMarker := strings.Index(out, "<!-- generated:harness-comparison -->")
	mrMarker := strings.Index(out, "<!-- generated:model-results -->")

	if mcMarker == -1 {
		t.Fatal("output missing <!-- generated:model-comparison --> marker")
	}
	if hcMarker == -1 {
		t.Fatal("output missing <!-- generated:harness-comparison --> marker")
	}
	if mrMarker == -1 {
		t.Fatal("output missing <!-- generated:model-results --> marker")
	}

	if !(mcMarker < hcMarker) {
		t.Errorf("model-comparison marker (at %d) must appear before harness-comparison marker (at %d)", mcMarker, hcMarker)
	}
	if !(hcMarker < mrMarker) {
		t.Errorf("harness-comparison marker (at %d) must appear before model-results marker (at %d)", hcMarker, mrMarker)
	}

	// Closing markers must also be present.
	if !strings.Contains(out, "<!-- /generated:model-comparison -->") {
		t.Error("output missing <!-- /generated:model-comparison --> closing marker")
	}
	if !strings.Contains(out, "<!-- /generated:harness-comparison -->") {
		t.Error("output missing <!-- /generated:harness-comparison --> closing marker")
	}
	if !strings.Contains(out, "<!-- /generated:model-results -->") {
		t.Error("output missing <!-- /generated:model-results --> closing marker")
	}
}

// TestRenderUserSummary_CostPerHundredTests asserts that cost cells in
// all tables show the per-100-tests format (suffix "/100t"). The fixture has
// TotalCost=1.50 and TestCount=10, so the normalized value is $15.00/100t.
// This fails against the current implementation which shows "$1.50" without
// normalization.
func TestRenderUserSummary_CostPerHundredTests(t *testing.T) {
	v := stage1VersionSummary()
	out := resultsummary.RenderUserSummary(v)

	if !strings.Contains(out, "/100t") {
		t.Errorf("output has no \"/100t\" suffix in cost cells; all cost cells must use per-100-tests format.\ngot:\n%s", out)
	}
	// The raw absolute total $1.50 must not appear as a standalone cost value.
	if strings.Contains(out, "$1.50") {
		t.Errorf("output shows absolute cost $1.50 without per-100-tests normalization; cost must be normalized.\ngot:\n%s", out)
	}
}

// TestRenderUserSummary_CostPerHundredTests_CorrectValue asserts that the
// normalized cost value is computed correctly. TotalCost=1.50, TestCount=10 ->
// 1.50/10*100 = $15.00/100t.
func TestRenderUserSummary_CostPerHundredTests_CorrectValue(t *testing.T) {
	v := stage1VersionSummary()
	out := resultsummary.RenderUserSummary(v)

	if !strings.Contains(out, "$15.00/100t") {
		t.Errorf("expected normalized cost \"$15.00/100t\" (1.50 / 10 tests * 100) in output.\ngot:\n%s", out)
	}
}

// TestRenderUserSummary_CostWarningPreserved_StillShowsMarker asserts
// that when CostWarning is true, the "[cost?]" warning marker is still used
// instead of any computed per-100-tests value. The warning takes priority.
func TestRenderUserSummary_CostWarningPreserved_StillShowsMarker(t *testing.T) {
	v := stage1VersionSummary()

	// Set CostWarning on both ByModel and BySuite entries.
	warningStats := resultsummary.HarnessModelStats{
		Harness:     "claude-code",
		Model:       "claude-sonnet-4-6",
		TestCount:   10,
		PassCount:   8,
		PassRate:    0.8,
		AvgDuration: 2 * time.Second,
		TotalCost:   0.0,
		CostWarning: true,
		HasPartial:  false,
	}
	v.ByModel["claude-sonnet-4-6"]["claude-code"] = warningStats
	v.BySuite["suite-a"]["claude-sonnet-4-6"]["claude-code"] = warningStats

	out := resultsummary.RenderUserSummary(v)

	if !strings.Contains(out, "[cost?]") {
		t.Errorf("output missing [cost?] warning marker when CostWarning=true; warning must take priority over per-100-tests computation.\ngot:\n%s", out)
	}
	if strings.Contains(out, "/100t") {
		t.Errorf("output should not show /100t format when CostWarning=true; [cost?] must be shown instead.\ngot:\n%s", out)
	}
}

// TestRenderUserSummary_NoDurationInOutput_Regression asserts that the
// text "Avg Duration" does not appear in the rendered Markdown, guarding
// against re-introduction of the removed column. Companion to
// TestRenderUserSummary_NoAvgDurationColumn but checks all harness/model
// stat combinations, including multi-model summaries.
func TestRenderUserSummary_NoDurationInOutput_MultiModel(t *testing.T) {
	v := resultsummary.VersionSummary{
		Version:     "v1.0.0",
		ReportCount: 2,
		Suites:      []string{"suite-a"},
		Models:      []string{"claude-sonnet-4-6", "gpt-4o"},
		Harnesses:   []string{"claude-code", "open-code"},
		TotalTests:  20,
		ByModel: map[string]map[string]resultsummary.HarnessModelStats{
			"claude-sonnet-4-6": {
				"claude-code": {
					Harness:     "claude-code",
					Model:       "claude-sonnet-4-6",
					TestCount:   10,
					PassCount:   9,
					PassRate:    0.9,
					AvgDuration: 3 * time.Second,
					TotalCost:   2.00,
				},
			},
			"gpt-4o": {
				"open-code": {
					Harness:     "open-code",
					Model:       "gpt-4o",
					TestCount:   10,
					PassCount:   7,
					PassRate:    0.7,
					AvgDuration: 5 * time.Second,
					TotalCost:   1.00,
				},
			},
		},
		BySuite: map[string]map[string]map[string]resultsummary.HarnessModelStats{
			"suite-a": {
				"claude-sonnet-4-6": {
					"claude-code": {
						Harness:     "claude-code",
						Model:       "claude-sonnet-4-6",
						TestCount:   10,
						PassCount:   9,
						PassRate:    0.9,
						AvgDuration: 3 * time.Second,
						TotalCost:   2.00,
					},
				},
				"gpt-4o": {
					"open-code": {
						Harness:     "open-code",
						Model:       "gpt-4o",
						TestCount:   10,
						PassCount:   7,
						PassRate:    0.7,
						AvgDuration: 5 * time.Second,
						TotalCost:   1.00,
					},
				},
			},
		},
	}

	out := resultsummary.RenderUserSummary(v)

	if strings.Contains(out, "Avg Duration") {
		t.Errorf("output contains \"Avg Duration\" column header in multi-model summary; it must be removed from all Model Results tables.\ngot:\n%s", out)
	}
}
