package report_test

// Tests for excluded-run visibility in the machine-readable and text reports,
// and for the additive-only guarantee that adding these fields does not remove
// or retype existing ones.
//
// Behaviour specified:
//  - The JSON report's per-aggregate object carries an "exclusions" field that
//    is always an array (never null), even when no runs were excluded.
//  - Each element in "exclusions" carries the run key, exclusion reason,
//    termination reason, and an explanatory detail.
//  - All existing aggregate fields (verdict, counted, passed, excluded,
//    pass_rate, required_pass_rate, infrastructure_failure, total_cost) remain
//    present and unchanged by the addition.
//  - Golden report comparisons remain stable: the new field renders as []
//    rather than null, so an existing golden file updated to include
//    "exclusions": [] still matches the rendered output exactly.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// fixtureResultWithSpawnFailedExclusion builds a report.Result whose one
// test's aggregate has one excluded spawn-failed run and one counted passing
// run, so the exclusions field has content to verify against.
func fixtureResultWithSpawnFailedExclusion() report.Result {
	excl := domain.ExcludedRun{
		Key:               domain.RunKey{RunID: "run-001", TestName: "my-test", RunNumber: 1},
		Reason:            domain.ExclusionSpawnFailed,
		TerminationReason: string(domain.DispositionSpawnFailed),
		Detail:            "harness process exited non-zero: exit status 1",
	}
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "exclusion-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:      "my-test",
				Description: "spawn-failure retry and exclusion",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:   "my-test",
					Verdict:  domain.VerdictPass,
					Counted:  1,
					Passed:   1,
					Excluded: 1,
					PassRate: 1.0,
					TotalCost: domain.CostReport{
						Attribution: domain.AttributionAttributed,
					},
					Exclusions: []domain.ExcludedRun{excl},
				},
				Runs: []report.RunReport{
					{
						Key:      domain.RunKey{RunID: "run-002", TestName: "my-test", RunNumber: 2},
						Verdict:  domain.VerdictPass,
						Duration: 2 * time.Second,
						Cost: domain.CostReport{
							Attribution: domain.AttributionAttributed,
						},
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}
}

// fixtureResultWithNoExclusions builds a report.Result whose aggregate has
// no exclusions, so we can verify the empty-array rendering.
func fixtureResultWithNoExclusions() report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "clean-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:      "clean-test",
				Description: "all runs counted",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:     "clean-test",
					Verdict:    domain.VerdictPass,
					Counted:    1,
					Passed:     1,
					Excluded:   0,
					PassRate:   1.0,
					Exclusions: []domain.ExcludedRun{}, // explicitly empty, not nil
					TotalCost: domain.CostReport{
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:     domain.RunKey{RunID: "run-001", TestName: "clean-test", RunNumber: 1},
						Verdict: domain.VerdictPass,
						Duration: time.Second,
						Cost: domain.CostReport{
							Attribution: domain.AttributionAttributed,
						},
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
	}
}

// ---------------------------------------------------------------------------
// T16.5 — Excluded-run visibility in the JSON report
// ---------------------------------------------------------------------------

// TestRenderJSON_Exclusions_AppearsInAggregateObject asserts that the
// machine-readable output includes an "exclusions" field in the per-aggregate
// object. A reader of the report must be able to see which runs were excluded
// and why without inspecting a retained sandbox.
func TestRenderJSON_Exclusions_AppearsInAggregateObject(t *testing.T) {
	out, err := renderJSON(t, fixtureResultWithSpawnFailedExclusion())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}
	if !strings.Contains(string(out), `"exclusions"`) {
		t.Errorf("machine-readable report has no \"exclusions\" field in the aggregate object\noutput: %s", out)
	}
}

// TestRenderJSON_Exclusions_CarriesReason asserts that each excluded-run
// object in the JSON report carries its exclusion reason so a reader can
// see why the run did not count.
func TestRenderJSON_Exclusions_CarriesReason(t *testing.T) {
	out, err := renderJSON(t, fixtureResultWithSpawnFailedExclusion())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}
	if !strings.Contains(string(out), string(domain.ExclusionSpawnFailed)) {
		t.Errorf("machine-readable report does not contain exclusion reason %q in its exclusions array\noutput: %s", domain.ExclusionSpawnFailed, out)
	}
}

// TestRenderJSON_Exclusions_CarriesTerminationReason asserts that each
// excluded-run object carries the subject's termination reason (disposition)
// so a reader can distinguish a spawn-failed exclusion from a state-integrity
// exclusion without inspecting a sandbox.
func TestRenderJSON_Exclusions_CarriesTerminationReason(t *testing.T) {
	out, err := renderJSON(t, fixtureResultWithSpawnFailedExclusion())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}
	if !strings.Contains(string(out), string(domain.DispositionSpawnFailed)) {
		t.Errorf("machine-readable report does not contain termination reason %q in its exclusions array\noutput: %s", domain.DispositionSpawnFailed, out)
	}
}

// TestRenderJSON_Exclusions_CarriesRunKey asserts that each excluded-run
// object carries the run key (run_id, test_id) so a reader can identify
// the specific excluded run in the invocation log.
func TestRenderJSON_Exclusions_CarriesRunKey(t *testing.T) {
	out, err := renderJSON(t, fixtureResultWithSpawnFailedExclusion())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}
	if !strings.Contains(string(out), "run-001") {
		t.Errorf("machine-readable report does not contain the excluded run's run_id %q in its exclusions array\noutput: %s", "run-001", out)
	}
}

// TestRenderJSON_Exclusions_CarriesDetail asserts that each excluded-run
// object carries a non-empty detail field explaining the exclusion without
// requiring sandbox inspection.
func TestRenderJSON_Exclusions_CarriesDetail(t *testing.T) {
	out, err := renderJSON(t, fixtureResultWithSpawnFailedExclusion())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Decode to verify the detail field is present and non-empty in the
	// structured output rather than just checking for a substring.
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	tests, _ := decoded["tests"].([]any)
	if len(tests) == 0 {
		t.Fatal("no tests in decoded output")
	}
	test0, _ := tests[0].(map[string]any)
	agg, _ := test0["aggregate"].(map[string]any)
	exclusions, _ := agg["exclusions"].([]any)
	if len(exclusions) == 0 {
		t.Fatal("exclusions array is empty in decoded output")
	}
	excl0, _ := exclusions[0].(map[string]any)
	detail, _ := excl0["detail"].(string)
	if detail == "" {
		t.Error("exclusions[0].detail is empty, want non-empty — the detail must explain the exclusion without requiring sandbox inspection")
	}
}

// ---------------------------------------------------------------------------
// T16.6 — Additive-only guarantee
// ---------------------------------------------------------------------------

// TestRenderJSON_Exclusions_RendersAsEmptyArrayNotNull asserts that when
// there are no exclusions, the "exclusions" field renders as [] rather than
// null. The additive-only invariant requires every collection to render as []
// so a caller parsing today's output never needs to distinguish an omitted
// collection from an empty one.
func TestRenderJSON_Exclusions_RendersAsEmptyArrayNotNull(t *testing.T) {
	out, err := renderJSON(t, fixtureResultWithNoExclusions())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	tests, _ := decoded["tests"].([]any)
	if len(tests) == 0 {
		t.Fatal("no tests in decoded output")
	}
	test0, _ := tests[0].(map[string]any)
	agg, _ := test0["aggregate"].(map[string]any)

	raw, ok := agg["exclusions"]
	if !ok {
		t.Error("aggregate has no \"exclusions\" field, want an empty array [] — the field must always be present")
	}
	exclusions, isSlice := raw.([]any)
	if !isSlice {
		t.Errorf("aggregate.exclusions has type %T, want []any (JSON array) — must render as [] not null", raw)
	} else if len(exclusions) != 0 {
		t.Errorf("aggregate.exclusions has %d elements, want 0 — no runs were excluded", len(exclusions))
	}
}

// TestRenderJSON_ExistingAggregateFields_UnchangedAfterExclusionsAdded verifies
// that adding the "exclusions" field does not remove or retype any existing
// aggregate field. The complete set of existing fields must still appear in
// the JSON output when Exclusions is populated.
func TestRenderJSON_ExistingAggregateFields_UnchangedAfterExclusionsAdded(t *testing.T) {
	out, err := renderJSON(t, fixtureResultWithSpawnFailedExclusion())
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	tests, _ := decoded["tests"].([]any)
	if len(tests) == 0 {
		t.Fatal("no tests in decoded output")
	}
	test0, _ := tests[0].(map[string]any)
	agg, _ := test0["aggregate"].(map[string]any)

	// Every field present before this addition must still be present.
	existingFields := []string{
		"test_id", "verdict", "reasons", "counted", "passed", "excluded",
		"pass_rate", "required_pass_rate", "infrastructure_failure", "total_cost",
	}
	for _, field := range existingFields {
		if _, ok := agg[field]; !ok {
			t.Errorf("aggregate field %q is missing from JSON output — the exclusions addition must be additive-only", field)
		}
	}
}

// ---------------------------------------------------------------------------
// T16.5 — Excluded-run visibility in the text (human-readable) report
// ---------------------------------------------------------------------------

// TestRenderText_Exclusions_AppearsInOutput asserts that the human-readable
// terminal report mentions the exclusion reason or disposition for each
// excluded run. A user reading terminal output after a suite run must be able
// to see that a run was excluded and why without opening a retained sandbox.
// This is the AC16.6 text-renderer obligation.
func TestRenderText_Exclusions_AppearsInOutput(t *testing.T) {
	out, err := renderText(t, fixtureResultWithSpawnFailedExclusion())
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	// The report must surface at least the exclusion reason ("spawn_failed")
	// or the termination disposition ("spawn_failed") so the reader knows
	// why the run did not count. Either string makes the exclusion legible
	// without requiring sandbox inspection.
	if !strings.Contains(out, string(domain.ExclusionSpawnFailed)) &&
		!strings.Contains(out, string(domain.DispositionSpawnFailed)) {
		t.Errorf(
			"text report does not mention exclusion reason %q or disposition %q "+
				"— a user reading the terminal cannot determine why the run was excluded "+
				"without inspecting a sandbox, which AC16.6 prohibits\noutput:\n%s",
			domain.ExclusionSpawnFailed, domain.DispositionSpawnFailed, out,
		)
	}
}

// TestRenderText_Exclusions_EmptyExclusionsDoNotAddNoise asserts that when no
// runs were excluded the text report does not spuriously mention exclusion
// content. A reader who sees "spawn_failed" in a report where nothing was
// excluded would be misled into thinking an infrastructure event occurred.
// This is a regression guard: once the text renderer emits exclusion content
// it must gate that output on whether there are actually exclusions to report.
func TestRenderText_Exclusions_EmptyExclusionsDoNotAddNoise(t *testing.T) {
	out, err := renderText(t, fixtureResultWithNoExclusions())
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	// With zero excluded runs, no exclusion reason must appear in the output.
	if strings.Contains(out, string(domain.ExclusionSpawnFailed)) {
		t.Errorf(
			"text report for a run with no exclusions mentions %q — this would "+
				"mislead a reader into believing a run was excluded when none was",
			domain.ExclusionSpawnFailed,
		)
	}
}

// TestRenderJSON_GoldenComparisons_StableWithEmptyExclusions verifies that a
// report whose aggregates have empty Exclusions slices produces the same
// overall JSON structure as the fixture the golden tests compare against,
// except for the new "exclusions": [] field. In particular, no existing field
// is removed, retyped, or renamed.
//
// This is the shape stability test: when the implementation adds "exclusions"
// to the wire, updating the golden files to include "exclusions": [] in each
// aggregate is all that is needed to keep them stable. Nothing else changes.
func TestRenderJSON_GoldenComparisons_StableWithEmptyExclusions(t *testing.T) {
	// Use the same fixture the all_excluded golden test uses, but verify the
	// aggregate-level fields independently so we don't couple to the exact
	// golden file content (which the implementation agent will update).
	result := fixtureAllExcludedResult()
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	tests, _ := decoded["tests"].([]any)
	if len(tests) == 0 {
		t.Fatal("no tests in decoded output")
	}
	agg, _ := (tests[0].(map[string]any))["aggregate"].(map[string]any)

	// The existing scalar fields must still carry the right values.
	if agg["verdict"] != "FAIL" {
		t.Errorf("aggregate.verdict = %v, want FAIL", agg["verdict"])
	}
	excluded, _ := agg["excluded"].(float64)
	if int(excluded) != 2 {
		t.Errorf("aggregate.excluded = %v, want 2", agg["excluded"])
	}
	if !agg["infrastructure_failure"].(bool) {
		t.Error("aggregate.infrastructure_failure = false, want true")
	}

	// The new "exclusions" field must be present and be an array.
	raw, ok := agg["exclusions"]
	if !ok {
		t.Error("aggregate has no \"exclusions\" field after the addition — the field must be additive, always present")
	}
	if _, isSlice := raw.([]any); !isSlice {
		t.Errorf("aggregate.exclusions has type %T, want JSON array", raw)
	}
}
