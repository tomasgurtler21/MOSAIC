package report_test

// Tests for the harness identity appearing in the human-readable (text) report
// (TDD RED phase).
//
// The text rendering prints a "harness:" line per run, following the same
// pattern as "subject version:", "subject model:", and "stub model:". When
// RunReport.HarnessID is empty the text value is "unknown" — the same mapping
// applied to all other identity fields, so the renderings are consistent and a
// reader never mistakes an absent value for a real one.

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// TestRenderText_HarnessIDAppearsInOutput asserts that when a RunReport
// carries a non-empty HarnessID, the human-readable report includes a
// "harness:" line with that exact value. A developer reading the terminal
// output must be able to identify which harness produced the run.
func TestRenderText_HarnessIDAppearsInOutput(t *testing.T) {
	// Arrange
	const wantID = "claude-code"
	result := fixtureResultWithHarnessID(wantID)

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "harness: claude-code" appears somewhere in the output
	wantSubstr := "harness: " + wantID
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("terminal report has no %q line;\n"+
			"want the human-readable report to carry the harness identity so a "+
			"developer can identify which harness produced the run.\n"+
			"--- got ---\n%s", wantSubstr, out)
	}
}

// TestRenderText_AbsentHarnessIDRendersAsUnknown asserts that when a
// RunReport's HarnessID is empty — meaning no harness identity was recorded
// — the human-readable report prints "harness: unknown" rather than an empty
// or missing line. An absent identity must be visibly distinct from a real one.
func TestRenderText_AbsentHarnessIDRendersAsUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithHarnessID("") // no harness identity recorded

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "harness: unknown" appears, never a blank
	const wantSubstr = "harness: unknown"
	if !strings.Contains(out, wantSubstr) {
		t.Errorf("terminal report does not contain %q for an absent harness identity;\n"+
			"an absent harness identity must render as 'unknown', never as blank "+
			"or omitted, so a reader does not mistake its absence for a real value.\n"+
			"--- got ---\n%s", wantSubstr, out)
	}
}

// TestRenderText_HarnessIDNotBlankWhenAbsent asserts that neither an empty
// "harness: " line nor a completely missing harness line appears when no
// harness identity was recorded. Only "unknown" is acceptable.
func TestRenderText_HarnessIDNotBlankWhenAbsent(t *testing.T) {
	// Arrange
	result := fixtureResultWithHarnessID("")

	// Act
	out, err := renderText(t, result)
	if err != nil {
		t.Fatalf("RenderText returned an error: %v", err)
	}

	// Assert: "harness: " must not be followed immediately by a newline or
	// only whitespace, which would indicate a blank value.
	blankLine := regexp.MustCompile(`harness:\s*\n`)
	if blankLine.MatchString(out) {
		t.Errorf("terminal report has a blank 'harness:' line for an absent harness identity;\n"+
			"want 'harness: unknown', never a blank value that reads like a real one.\n"+
			"--- got ---\n%s", out)
	}
}

// TestReportsAgree_HarnessIDConsistent asserts that both the terminal and
// machine-readable reports carry the same harness identity for the same run.
// Both renderings must apply the identical "" → "unknown" mapping, so a
// developer reading the terminal output and a tool consuming the JSON report
// both see the identical value.
func TestReportsAgree_HarnessIDConsistent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		harnessID string
		wantValue string // the agreed value both renderings must show
	}{
		{"declared harness", "claude-code", "claude-code"},
		{"absent harness", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			result := fixtureResultWithHarnessID(tc.harnessID)

			// Act
			textOut, textErr := renderText(t, result)
			jsonOut, jsonErr := renderJSON(t, result)

			// Assert no render errors
			if textErr != nil {
				t.Fatalf("RenderText returned an error: %v", textErr)
			}
			if jsonErr != nil {
				t.Fatalf("RenderJSON returned an error: %v", jsonErr)
			}

			// Assert: JSON carries the expected agreed value
			jsonHarnessID := decodeJSONRunHarnessID(t, jsonOut)
			if jsonHarnessID != tc.wantValue {
				t.Errorf("JSON harness_id = %q, want %q for case %q",
					jsonHarnessID, tc.wantValue, tc.name)
			}

			// Assert: text carries the expected agreed value
			wantTextLine := "harness: " + tc.wantValue
			if !strings.Contains(textOut, wantTextLine) {
				t.Errorf("terminal report does not contain %q for case %q;\n"+
					"both renderings must agree on the harness identity value.\n"+
					"--- got ---\n%s", wantTextLine, tc.name, textOut)
			}
		})
	}
}

// --- helpers ---

// fixtureResultWithHarnessID builds a minimal report.Result containing exactly
// one test with one run whose HarnessID is set to harnessID. It is the minimal
// fixture needed to exercise the harness identity rendering path without pulling
// in the full fixtureFullResult fixture.
func fixtureResultWithHarnessID(harnessID string) report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "harness-identity-test-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:      "harness-identity-test",
				Description: "harness identity appears in the report",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:   "harness-identity-test",
					Verdict:  domain.VerdictPass,
					Counted:  1,
					Passed:   1,
					PassRate: 1,
					TotalCost: domain.CostReport{
						TotalUSD:    0.01,
						Attribution: domain.AttributionAttributed,
					},
				},
				Runs: []report.RunReport{
					{
						Key:       domain.RunKey{RunID: "run-001", TestName: "harness-identity-test", RunNumber: 1},
						Verdict:   domain.VerdictPass,
						Duration:  1 * time.Second,
						HarnessID: harnessID,
						Cost: domain.CostReport{
							TotalUSD:    0.01,
							Attribution: domain.AttributionAttributed,
						},
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{
			domain.VerdictPass: 1,
		},
		TotalCost: domain.CostReport{
			TotalUSD:    0.01,
			Attribution: domain.AttributionAttributed,
		},
	}
}
