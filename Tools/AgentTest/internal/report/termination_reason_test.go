package report_test

// Tests for the termination reason carried in the report model and surfaced
// by both renderings.
//
// The model carries why a run ended as RunReport.TerminationReason — a
// string matching domain.RunDisposition values. Both the terminal and
// machine-readable renderings surface it so a clean cutoff is
// distinguishable from a timeout, a turn-limit stop, a spawn failure,
// and a normal completion.
//
// A new OutcomeClass (ClassCutoff) is derived from TerminationReason by
// Classify, following the same single-derivation rule that keeps the
// terminal report, the machine-readable report and the TUI detail view
// from disagreeing.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// fixtureRunWithTerminationReason builds a minimal single-test, single-run
// Result whose only run carries the given termination reason. Tests use this
// to assert on each disposition value in isolation without depending on the
// full fixtureFullResult shape.
func fixtureRunWithTerminationReason(reason string) report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "termination-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestID:      "termination-test",
				Description: "a run with a known termination reason",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:   "termination-test",
					Verdict:  domain.VerdictPass,
					Counted:  1,
					Passed:   1,
					PassRate: 1,
					TotalCost: domain.CostReport{
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:               domain.RunKey{RunID: "run-001", TestID: "termination-test", RunNumber: 1},
						Verdict:           domain.VerdictPass,
						Duration:          2 * time.Second,
						Cost:              domain.CostReport{Attribution: domain.AttributionAttributed},
						TerminationReason: reason,
					},
				},
			},
		},
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}
}

// decodeFirstRunTerminationReason extracts termination_reason from the first
// run of the first test in a JSON-encoded report.
func decodeFirstRunTerminationReason(t *testing.T, data []byte) string {
	t.Helper()
	var doc struct {
		Tests []struct {
			Runs []struct {
				TerminationReason string `json:"termination_reason"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding termination_reason from report JSON: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("decoded report has no tests or runs to read termination_reason from")
	}
	return doc.Tests[0].Runs[0].TerminationReason
}

// allKnownDispositions lists every RunDisposition value the supervisor can
// decide, so table-driven tests cover all distinguishable reasons without
// hard-coding each as a separate function.
var allKnownDispositions = []domain.RunDisposition{
	domain.DispositionCompleted,
	domain.DispositionEarlyExit,
	domain.DispositionTimedOut,
	domain.DispositionTurnLimit,
	domain.DispositionSpawnFailed,
}

// ---- Terminal rendering surfaces termination reason (T3.1) ----

// TestRenderText_ShowsTerminationReason asserts the terminal report includes
// the run's disposition string for each known termination reason, so a
// reader can tell why each run ended without consulting the machine-readable
// report.
func TestRenderText_ShowsTerminationReason(t *testing.T) {
	for _, d := range allKnownDispositions {
		d := d
		t.Run(string(d), func(t *testing.T) {
			out, err := renderText(t, fixtureRunWithTerminationReason(string(d)))
			if err != nil {
				t.Fatalf("RenderText returned an error: %v", err)
			}
			if !strings.Contains(out, string(d)) {
				t.Errorf("terminal report does not contain termination reason %q.\n--- got ---\n%s", d, out)
			}
		})
	}
}

// TestRenderText_ShowsUnknown_WhenTerminationReasonEmpty asserts the terminal
// report shows "unknown" rather than a blank when the run's TerminationReason
// is empty, so a reader never mistakes an absent reason for a real disposition.
func TestRenderText_ShowsUnknown_WhenTerminationReasonEmpty(t *testing.T) {
	out, err := renderText(t, fixtureRunWithTerminationReason(""))
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}
	if !strings.Contains(out, "termination reason: unknown") {
		t.Errorf("terminal report does not show %q for an empty TerminationReason; want the specific label and fallback value together.\n--- got ---\n%s", "termination reason: unknown", out)
	}
}

// ---- Machine-readable rendering surfaces termination reason (T3.1) ----

// TestRenderJSON_IncludesTerminationReasonField asserts the machine-readable
// report carries a "termination_reason" field in every per-run object whose
// value equals the run's exact disposition string, so a consumer can tell
// why each run ended by reading one field.
func TestRenderJSON_IncludesTerminationReasonField(t *testing.T) {
	for _, d := range allKnownDispositions {
		d := d
		t.Run(string(d), func(t *testing.T) {
			out, err := renderJSON(t, fixtureRunWithTerminationReason(string(d)))
			if err != nil {
				t.Fatalf("RenderJSON returned an error: %v", err)
			}
			got := decodeFirstRunTerminationReason(t, out)
			if got != string(d) {
				t.Errorf("termination_reason = %q, want %q", got, d)
			}
		})
	}
}

// TestRenderJSON_ShowsUnknown_WhenTerminationReasonEmpty asserts the
// machine-readable report carries "unknown" for termination_reason rather
// than an empty string when no disposition was recorded, mirroring the same
// treatment subject_version and subject_model receive.
func TestRenderJSON_ShowsUnknown_WhenTerminationReasonEmpty(t *testing.T) {
	out, err := renderJSON(t, fixtureRunWithTerminationReason(""))
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}
	got := decodeFirstRunTerminationReason(t, out)
	if got != "unknown" {
		t.Errorf("termination_reason = %q for empty TerminationReason, want %q", got, "unknown")
	}
}

// ---- All five dispositions produce distinguishable output (T3.1) ----

// TestTerminationReasons_AllDistinguishable asserts each known disposition
// produces a unique termination_reason value in the machine-readable report,
// so a clean cutoff, a timeout, a turn-limit stop, a spawn failure, and a
// normal completion are each unambiguously identifiable by value.
func TestTerminationReasons_AllDistinguishable(t *testing.T) {
	seen := map[string]domain.RunDisposition{}
	for _, d := range allKnownDispositions {
		out, err := renderJSON(t, fixtureRunWithTerminationReason(string(d)))
		if err != nil {
			t.Fatalf("RenderJSON(%s) returned an error: %v", d, err)
		}
		got := decodeFirstRunTerminationReason(t, out)
		if prior, clash := seen[got]; clash {
			t.Errorf("dispositions %q and %q both produce termination_reason %q; they must be distinguishable", prior, d, got)
		}
		seen[got] = d
	}
}

// ---- Both renderings agree on the termination reason (T3.2) ----

// TestBothRenderings_AgreeOnTerminationReason renders both the terminal and
// machine-readable reports from the same Result and asserts they carry the
// same termination reason for the same run. The single-result-model rule
// requires agreement: two reports from two traversals that disagree are
// worse than one report.
func TestBothRenderings_AgreeOnTerminationReason(t *testing.T) {
	for _, d := range allKnownDispositions {
		d := d
		t.Run(string(d), func(t *testing.T) {
			result := fixtureRunWithTerminationReason(string(d))

			textOut, err := renderText(t, result)
			if err != nil {
				t.Fatalf("RenderText returned an error: %v", err)
			}
			jsonOut, err := renderJSON(t, result)
			if err != nil {
				t.Fatalf("RenderJSON returned an error: %v", err)
			}

			if !strings.Contains(textOut, string(d)) {
				t.Errorf("terminal report does not contain disposition %q; renderings disagree.\n--- text ---\n%s", d, textOut)
			}
			if got := decodeFirstRunTerminationReason(t, jsonOut); got != string(d) {
				t.Errorf("machine-readable termination_reason = %q, want %q; renderings disagree", got, d)
			}
		})
	}
}

// ---- Additive schema: existing fields are preserved (T3.2) ----

// TestRenderJSON_TerminationReason_IsAdditive asserts that adding
// "termination_reason" to the per-run JSON object does not remove or retype
// any field that was present before the change. An existing consumer that
// does not read termination_reason must still parse all other fields without
// error.
func TestRenderJSON_TerminationReason_IsAdditive(t *testing.T) {
	out, err := renderJSON(t, fixtureRunWithTerminationReason(string(domain.DispositionCompleted)))
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	var doc struct {
		Tests []struct {
			Runs []json.RawMessage `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("no runs in decoded report to inspect for field presence")
	}

	var runObj map[string]interface{}
	if err := json.Unmarshal(doc.Tests[0].Runs[0], &runObj); err != nil {
		t.Fatalf("decoding run object: %v", err)
	}

	// Every field that existed before this change must still be present with
	// its original key name. The schema is additive-only: a field is never
	// removed or retyped.
	mustHave := []string{
		"run", "verdict", "reasons", "assertions", "conditions",
		"duration_ms", "cost", "negative_applied",
		"subject_version", "subject_model", "stub_model",
	}
	for _, key := range mustHave {
		if _, ok := runObj[key]; !ok {
			t.Errorf("run object is missing pre-existing field %q after adding termination_reason; schema changes must be additive", key)
		}
	}

	// The new field must also be present.
	if _, ok := runObj["termination_reason"]; !ok {
		t.Errorf("run object is missing new field %q", "termination_reason")
	}
}

// ---- ClassCutoff is derived from TerminationReason (T3.2) ----

// TestClassify_IncludesClassCutoff_WhenTerminationReasonIsEarlyExit asserts
// that Classify returns ClassCutoff when the run's TerminationReason is the
// early-exit disposition, so the TUI and both renderings can surface a cutoff
// as a visible, distinct outcome class.
func TestClassify_IncludesClassCutoff_WhenTerminationReasonIsEarlyExit(t *testing.T) {
	run := report.RunReport{
		Verdict:           domain.VerdictPass,
		TerminationReason: string(domain.DispositionEarlyExit),
	}

	classes := report.Classify(run)

	found := false
	for _, c := range classes {
		if c == report.ClassCutoff {
			found = true
		}
	}
	if !found {
		t.Errorf("Classify(%+v) = %v, want ClassCutoff present when TerminationReason is %q", run, classes, domain.DispositionEarlyExit)
	}
}

// TestClassify_ExcludesClassCutoff_WhenTerminationReasonIsNotEarlyExit
// asserts Classify never returns ClassCutoff for any disposition other than
// early_exit, so a timed-out, turn-limited, spawn-failed or normally
// completed run is never misidentified as a cutoff.
func TestClassify_ExcludesClassCutoff_WhenTerminationReasonIsNotEarlyExit(t *testing.T) {
	for _, d := range []domain.RunDisposition{
		domain.DispositionCompleted,
		domain.DispositionTimedOut,
		domain.DispositionTurnLimit,
		domain.DispositionSpawnFailed,
	} {
		d := d
		t.Run(string(d), func(t *testing.T) {
			run := report.RunReport{
				Verdict:           domain.VerdictPass,
				TerminationReason: string(d),
			}
			classes := report.Classify(run)
			for _, c := range classes {
				if c == report.ClassCutoff {
					t.Errorf("Classify(%+v) = %v, want ClassCutoff absent when TerminationReason is %q", run, classes, d)
				}
			}
		})
	}
}

// TestClassify_ExcludesClassCutoff_WhenTerminationReasonEmpty asserts
// Classify never emits ClassCutoff when TerminationReason is empty, so a
// run whose disposition was not recorded is never misidentified as a cutoff.
func TestClassify_ExcludesClassCutoff_WhenTerminationReasonEmpty(t *testing.T) {
	run := report.RunReport{Verdict: domain.VerdictPass}

	classes := report.Classify(run)

	for _, c := range classes {
		if c == report.ClassCutoff {
			t.Errorf("Classify(%+v) = %v, want ClassCutoff absent when TerminationReason is empty", run, classes)
		}
	}
}

// TestClassify_ClassCutoff_OnlyFromTerminationReason asserts ClassCutoff is
// never derived from anything other than TerminationReason == "early_exit".
// A run that passes with no termination reason must not acquire ClassCutoff
// from any other path.
func TestClassify_ClassCutoff_OnlyFromTerminationReason(t *testing.T) {
	// A passing run with conditions, assertions, and reasons — but no
	// TerminationReason — must not include ClassCutoff.
	run := report.RunReport{
		Verdict: domain.VerdictPass,
		Conditions: []domain.RunCondition{
			{Kind: domain.ConditionCostUnattributed, Detail: "log root missing"},
		},
	}

	classes := report.Classify(run)
	for _, c := range classes {
		if c == report.ClassCutoff {
			t.Errorf("Classify(%+v) = %v, ClassCutoff present but TerminationReason is empty; ClassCutoff must derive only from TerminationReason", run, classes)
		}
	}
}

// ---- Cutoff run's duration and cost are carried unchanged (T3.1 / AC3.5) ----

// TestRenderJSON_CutoffRun_CarriesDurationUnchanged asserts a run with the
// early-exit termination reason carries its exact Duration in the
// machine-readable report, unmodified. The report's job is to surface what
// the runner measured; it does not recalculate or extend the value.
func TestRenderJSON_CutoffRun_CarriesDurationUnchanged(t *testing.T) {
	const wantMS = int64(3500)
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "cutoff-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestID: "cutoff-test",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:    "cutoff-test",
					Verdict:   domain.VerdictPass,
					Counted:   1,
					Passed:    1,
					PassRate:  1,
					TotalCost: domain.CostReport{TotalUSD: 0.03, Attribution: domain.AttributionAttributed},
				},
				Runs: []report.RunReport{
					{
						Key:               domain.RunKey{RunID: "cutoff-001", TestID: "cutoff-test", RunNumber: 1},
						Verdict:           domain.VerdictPass,
						Duration:          time.Duration(wantMS) * time.Millisecond,
						Cost:              domain.CostReport{TotalUSD: 0.03, Attribution: domain.AttributionAttributed},
						TerminationReason: string(domain.DispositionEarlyExit),
					},
				},
			},
		},
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{TotalUSD: 0.03, Attribution: domain.AttributionAttributed},
	}

	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	var doc struct {
		Tests []struct {
			Runs []struct {
				DurationMS int64 `json:"duration_ms"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decoding report JSON: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("no runs in decoded report")
	}
	if got := doc.Tests[0].Runs[0].DurationMS; got != wantMS {
		t.Errorf("cutoff run duration_ms = %d, want %d; the report must carry the measured duration unchanged", got, wantMS)
	}
}

// TestRenderJSON_CutoffRun_CarriesCostUnchanged asserts a run with the
// early-exit termination reason carries its exact Cost.TotalUSD in the
// machine-readable report, unmodified. The report's job is to surface what
// the runner measured; it does not recalculate or cap the cost figure.
func TestRenderJSON_CutoffRun_CarriesCostUnchanged(t *testing.T) {
	const wantCostUSD = float64(0.027)
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "cutoff-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestID: "cutoff-cost-test",
				Layer:  domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestID:    "cutoff-cost-test",
					Verdict:   domain.VerdictPass,
					Counted:   1,
					Passed:    1,
					PassRate:  1,
					TotalCost: domain.CostReport{TotalUSD: wantCostUSD, Attribution: domain.AttributionAttributed},
				},
				Runs: []report.RunReport{
					{
						Key:               domain.RunKey{RunID: "cutoff-cost-001", TestID: "cutoff-cost-test", RunNumber: 1},
						Verdict:           domain.VerdictPass,
						Duration:          3500 * time.Millisecond,
						Cost:              domain.CostReport{TotalUSD: wantCostUSD, Attribution: domain.AttributionAttributed},
						TerminationReason: string(domain.DispositionEarlyExit),
					},
				},
			},
		},
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{TotalUSD: wantCostUSD, Attribution: domain.AttributionAttributed},
	}

	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	var doc struct {
		Tests []struct {
			Runs []struct {
				Cost struct {
					TotalUSD float64 `json:"total_usd"`
				} `json:"cost"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decoding report JSON: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("no runs in decoded report")
	}
	if got := doc.Tests[0].Runs[0].Cost.TotalUSD; got != wantCostUSD {
		t.Errorf("cutoff run cost.total_usd = %v, want %v; the report must carry the measured cost unchanged", got, wantCostUSD)
	}
}
