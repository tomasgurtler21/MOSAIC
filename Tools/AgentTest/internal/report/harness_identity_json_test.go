package report_test

// Tests for the harness identity appearing in the machine-readable (JSON)
// report (TDD RED phase).
//
// The JSON wire type (wireRunReport) carries "harness_id" in every run report,
// following the same pattern as "subject_version", "subject_model", and
// "stub_model". When RunReport.HarnessID is empty the wire value is "unknown"
// — never an empty string that would read like a real value. When it is
// non-empty the wire value is that exact string.
//
// The field is additive: every field present before this change keeps its
// name, position in the model, and meaning.

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderJSON_HarnessIDAppearsInRunReport asserts that when a RunReport
// carries a non-empty HarnessID, the machine-readable output includes a
// "harness_id" field in the per-run object with that exact value. A reader of
// the machine-readable report must be able to identify which harness produced
// the run.
func TestRenderJSON_HarnessIDAppearsInRunReport(t *testing.T) {
	// Arrange
	const wantID = "claude-code"
	result := fixtureResultWithHarnessID(wantID)

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: harness_id field present with correct value
	runHarnessID := decodeJSONRunHarnessID(t, out)
	if runHarnessID != wantID {
		t.Errorf("harness_id in JSON run = %q, want %q; "+
			"the machine-readable report must carry the harness identity so a "+
			"result can be attributed to the harness that produced it",
			runHarnessID, wantID)
	}
}

// TestRenderJSON_AbsentHarnessIDRendersAsUnknown asserts that when a
// RunReport's HarnessID is empty — meaning no harness identity was recorded
// — the machine-readable output carries "unknown" rather than an empty string.
// An empty string in the report reads like a real value and must never appear.
func TestRenderJSON_AbsentHarnessIDRendersAsUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithHarnessID("") // no harness identity recorded

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: absent harness identity must render as "unknown", not as empty string
	runHarnessID := decodeJSONRunHarnessID(t, out)
	if runHarnessID != "unknown" {
		t.Errorf("harness_id in JSON run = %q, want %q; "+
			"an absent harness identity must render as 'unknown' in the JSON "+
			"report, never as an empty string that reads like a real value",
			runHarnessID, "unknown")
	}
}

// TestRenderJSON_HarnessIDFieldAlwaysPresent asserts that the "harness_id" key
// is present in every run object, whether a harness identity was recorded or
// not. The field must not be omitted when the identity is unknown, because a
// caller cannot distinguish "field absent" from "this tool version did not
// include the field" without schema metadata.
func TestRenderJSON_HarnessIDFieldAlwaysPresent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		harnessID string
	}{
		{"declared harness", "claude-code"},
		{"absent harness", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			result := fixtureResultWithHarnessID(tc.harnessID)

			// Act
			out, err := renderJSON(t, result)
			if err != nil {
				t.Fatalf("RenderJSON returned an error: %v", err)
			}

			// Assert: "harness_id" key must be present
			if !strings.Contains(string(out), `"harness_id"`) {
				t.Errorf("JSON output has no 'harness_id' key for case %q; "+
					"the field must always be present so a caller does not have to "+
					"distinguish absence from an old schema version",
					tc.name)
			}
		})
	}
}

// TestRenderJSON_HarnessIDIsAdditive_ExistingRunFieldsPreserved asserts that
// adding harness_id to the per-run shape does not displace or rename any field
// that was already present. This is the per-run instance of the additive-only
// contract that governs the entire JSON report schema.
func TestRenderJSON_HarnessIDIsAdditive_ExistingRunFieldsPreserved(t *testing.T) {
	// Arrange
	result := fixtureResultWithHarnessID("claude-code")

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: well-known per-run fields from the original schema must all
	// still be present. Adding harness_id must not rename, repurpose, or
	// remove any of them.
	existingRunFields := []string{
		`"run"`,
		`"verdict"`,
		`"reasons"`,
		`"assertions"`,
		`"conditions"`,
		`"duration_ms"`,
		`"cost"`,
		`"negative_applied"`,
		`"subject_version"`,
		`"subject_model"`,
		`"stub_model"`,
		`"termination_reason"`,
	}
	jsonStr := string(out)
	for _, field := range existingRunFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("existing run field %s is absent from the JSON output; "+
				"adding harness_id must be a purely additive change — "+
				"no existing field may be removed or renamed",
				field)
		}
	}
}

// --- helpers ---

// decodeJSONRunHarnessID decodes the harness_id field from the first run of
// the first test in the JSON output, so an assertion can check the wire value
// without depending on the entire document shape.
func decodeJSONRunHarnessID(t *testing.T, data []byte) string {
	t.Helper()
	var doc struct {
		Tests []struct {
			Runs []struct {
				HarnessID string `json:"harness_id"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding JSON report: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("JSON report has no tests or no runs to decode harness_id from")
	}
	return doc.Tests[0].Runs[0].HarnessID
}
