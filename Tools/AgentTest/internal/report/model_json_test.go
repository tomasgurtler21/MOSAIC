package report_test

// Tests for the subject model and stub model appearing in the machine-readable
// (JSON) report, including the absent-model case and the additive-only schema
// guarantee (TDD RED phase).
//
// The JSON wire type carries "subject_model" and "stub_model" in every run
// object. When RunReport.SubjectModel or RunReport.StubModel is empty the wire
// value is "unknown" — never an empty string that would read like a real
// value. When it is non-empty the wire value is that exact string.
//
// The fields are additive: every field present before this change keeps its
// name, position in the model, and meaning.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// TestRenderJSON_SubjectModelAppearsInRunReport asserts that when a RunReport
// carries a non-empty SubjectModel, the machine-readable output includes a
// "subject_model" field in the per-run object with that exact value. A reader
// of the machine-readable report must be able to attribute a result to the
// model the subject ran on.
func TestRenderJSON_SubjectModelAppearsInRunReport(t *testing.T) {
	// Arrange
	const wantModel = "claude-opus-4-5"
	result := fixtureResultWithModels(wantModel, "claude-haiku-4-5")

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: subject_model field present with correct value
	gotModel := decodeJSONRunSubjectModel(t, out)
	if gotModel != wantModel {
		t.Errorf("subject_model in JSON run = %q, want %q; "+
			"the machine-readable report must carry the subject model so a "+
			"result can be attributed to the model the subject ran on",
			gotModel, wantModel)
	}
}

// TestRenderJSON_StubModelAppearsInRunReport asserts that when a RunReport
// carries a non-empty StubModel, the machine-readable output includes a
// "stub_model" field in the per-run object with that exact value.
func TestRenderJSON_StubModelAppearsInRunReport(t *testing.T) {
	// Arrange
	const wantStubModel = "claude-haiku-4-5"
	result := fixtureResultWithModels("claude-opus-4-5", wantStubModel)

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: stub_model field present with correct value
	gotModel := decodeJSONRunStubModel(t, out)
	if gotModel != wantStubModel {
		t.Errorf("stub_model in JSON run = %q, want %q; "+
			"the machine-readable report must carry the stub model so a "+
			"result can be attributed to the models the stubs ran on",
			gotModel, wantStubModel)
	}
}

// TestRenderJSON_AbsentSubjectModelRendersAsUnknown asserts that when a
// RunReport's SubjectModel is empty — meaning no model was recorded for this
// run — the machine-readable output carries "unknown" rather than an empty
// string. An empty string reads like a real value and must never appear.
func TestRenderJSON_AbsentSubjectModelRendersAsUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithModels("", "") // no model recorded

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: absent subject model must render as "unknown", not as empty string
	gotModel := decodeJSONRunSubjectModel(t, out)
	if gotModel != "unknown" {
		t.Errorf("subject_model in JSON run = %q, want %q; "+
			"an absent subject model must render as 'unknown' in the JSON report, "+
			"never as an empty string that reads like a real model identifier",
			gotModel, "unknown")
	}
}

// TestRenderJSON_AbsentStubModelRendersAsUnknown asserts that when a
// RunReport's StubModel is empty the machine-readable output carries "unknown"
// rather than an empty string.
func TestRenderJSON_AbsentStubModelRendersAsUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithModels("", "") // no model recorded

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: absent stub model must render as "unknown", not as empty string
	gotModel := decodeJSONRunStubModel(t, out)
	if gotModel != "unknown" {
		t.Errorf("stub_model in JSON run = %q, want %q; "+
			"an absent stub model must render as 'unknown' in the JSON report, "+
			"never as an empty string that reads like a real model identifier",
			gotModel, "unknown")
	}
}

// TestRenderJSON_ModelFieldsAlwaysPresent asserts that both "subject_model"
// and "stub_model" keys are present in every run object, whether models were
// recorded or not. A caller cannot distinguish "field absent" from "this tool
// version did not include the field" without schema metadata.
func TestRenderJSON_ModelFieldsAlwaysPresent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		subjectModel string
		stubModel    string
	}{
		{"both models recorded", "claude-opus-4-5", "claude-haiku-4-5"},
		{"no models recorded", "", ""},
		{"only subject model", "claude-opus-4-5", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			result := fixtureResultWithModels(tc.subjectModel, tc.stubModel)

			// Act
			out, err := renderJSON(t, result)
			if err != nil {
				t.Fatalf("RenderJSON returned an error: %v", err)
			}

			// Assert: both keys must always be present
			jsonStr := string(out)
			for _, key := range []string{`"subject_model"`, `"stub_model"`} {
				if !strings.Contains(jsonStr, key) {
					t.Errorf("JSON output has no %s key for case %q; "+
						"model fields must always be present so a caller does not have to "+
						"distinguish absence from an old schema version",
						key, tc.name)
				}
			}
		})
	}
}

// TestRenderJSON_ModelFieldsAreAdditive_ExistingRunFieldsPreserved asserts
// that adding subject_model and stub_model to the per-run shape does not
// displace or rename any field that was already present. This is the per-run
// instance of the additive-only contract that governs the entire JSON report
// schema.
func TestRenderJSON_ModelFieldsAreAdditive_ExistingRunFieldsPreserved(t *testing.T) {
	// Arrange
	result := fixtureResultWithModels("claude-opus-4-5", "claude-haiku-4-5")

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: well-known per-run fields from the original schema must all
	// still be present. Adding subject_model and stub_model must not rename,
	// repurpose, or remove any existing field.
	existingRunFields := []string{
		`"run"`,
		`"verdict"`,
		`"reasons"`,
		`"assertions"`,
		`"conditions"`,
		`"duration_ms"`,
		`"cost"`,
		`"negative_applied"`,
	}
	jsonStr := string(out)
	for _, field := range existingRunFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("existing run field %s is absent from the JSON output; "+
				"adding subject_model and stub_model must be a purely additive change — "+
				"no existing field may be removed or renamed",
				field)
		}
	}
}

// --- helpers ---

// fixtureResultWithModels builds a minimal report.Result containing exactly
// one test with one run whose SubjectModel and StubModel are set to the given
// values. It is the minimal fixture needed to exercise the model rendering path.
func fixtureResultWithModels(subjectModel, stubModel string) report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "model-attribution-test-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:      "model-attribution-test",
				Description: "models used appear in the report",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:   "model-attribution-test",
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
						Key:          domain.RunKey{RunID: "run-001", TestName: "model-attribution-test", RunNumber: 1},
						Verdict:      domain.VerdictPass,
						Duration:     1 * time.Second,
						SubjectModel: subjectModel,
						StubModel:    stubModel,
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

// decodeJSONRunSubjectModel decodes the subject_model field from the first
// run of the first test in the JSON output.
func decodeJSONRunSubjectModel(t *testing.T, data []byte) string {
	t.Helper()
	var doc struct {
		Tests []struct {
			Runs []struct {
				SubjectModel string `json:"subject_model"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding JSON report: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("JSON report has no tests or no runs to decode subject_model from")
	}
	return doc.Tests[0].Runs[0].SubjectModel
}

// decodeJSONRunStubModel decodes the stub_model field from the first run of
// the first test in the JSON output.
func decodeJSONRunStubModel(t *testing.T, data []byte) string {
	t.Helper()
	var doc struct {
		Tests []struct {
			Runs []struct {
				StubModel string `json:"stub_model"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding JSON report: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("JSON report has no tests or no runs to decode stub_model from")
	}
	return doc.Tests[0].Runs[0].StubModel
}
