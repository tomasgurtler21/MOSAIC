package report_test

// Tests for the subject version appearing in the machine-readable (JSON)
// report, including the absent-version case and the additive-only schema
// guarantee (TDD RED phase).
//
// The JSON wire type (wireRunReport) carries "subject_version" in every run
// report. When RunReport.SubjectVersion is empty the wire value is "unknown"
// — never an empty string that would read like a real value. When it is
// non-empty the wire value is that exact string.
//
// The field is additive: every field present before this change keeps its
// name, position in the model, and meaning. These tests cover the field's
// presence and value but do not re-test the existing golden shape; those
// tests live in json_test.go.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// TestRenderJSON_SubjectVersionAppearsInRunReport asserts that when a
// RunReport carries a non-empty SubjectVersion, the machine-readable output
// includes a "subject_version" field in the per-run object with that exact
// value. A reader of the machine-readable report must be able to attribute a
// result to a specific version of the agent under test.
func TestRenderJSON_SubjectVersionAppearsInRunReport(t *testing.T) {
	// Arrange
	const wantVersion = "v2.5.0"
	result := fixtureResultWithSubjectVersion(wantVersion)

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: subject_version field present with correct value
	runVersion := decodeJSONRunSubjectVersion(t, out)
	if runVersion != wantVersion {
		t.Errorf("subject_version in JSON run = %q, want %q; "+
			"the machine-readable report must carry the subject version so a "+
			"result can be attributed to a specific version of the agent under test",
			runVersion, wantVersion)
	}
}

// TestRenderJSON_AbsentSubjectVersionRendersAsUnknown asserts that when a
// RunReport's SubjectVersion is empty — meaning the source declared no version
// — the machine-readable output carries "unknown" rather than an empty string.
// An empty string in the report reads like a real value and must never appear.
func TestRenderJSON_AbsentSubjectVersionRendersAsUnknown(t *testing.T) {
	// Arrange
	result := fixtureResultWithSubjectVersion("") // source declared no version

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: absent version must render as "unknown", not as empty string
	runVersion := decodeJSONRunSubjectVersion(t, out)
	if runVersion != "unknown" {
		t.Errorf("subject_version in JSON run = %q, want %q; "+
			"an absent source version must render as 'unknown' in the JSON report, "+
			"never as an empty string that reads like a real version value",
			runVersion, "unknown")
	}
}

// TestRenderJSON_SubjectVersionFieldAlwaysPresent asserts that the
// "subject_version" key is present in every run object, whether the source
// declared a version or not. The field must not be omitted when the version
// is unknown, because a caller cannot distinguish "field absent" from "this
// tool version did not include the field" without schema metadata.
func TestRenderJSON_SubjectVersionFieldAlwaysPresent(t *testing.T) {
	for _, tc := range []struct {
		name           string
		subjectVersion string
	}{
		{"declared version", "v1.0.0"},
		{"absent version", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			result := fixtureResultWithSubjectVersion(tc.subjectVersion)

			// Act
			out, err := renderJSON(t, result)
			if err != nil {
				t.Fatalf("RenderJSON returned an error: %v", err)
			}

			// Assert: "subject_version" key must be present
			if !strings.Contains(string(out), `"subject_version"`) {
				t.Errorf("JSON output has no 'subject_version' key for case %q; "+
					"the field must always be present so a caller does not have to "+
					"distinguish absence from an old schema version",
					tc.name)
			}
		})
	}
}

// TestRenderJSON_SubjectVersionIsAdditive_ExistingRunFieldsPreserved asserts
// that adding subject_version to the per-run shape does not displace or
// rename any field that was already present. This is the per-run instance
// of the additive-only contract that governs the entire JSON report schema.
func TestRenderJSON_SubjectVersionIsAdditive_ExistingRunFieldsPreserved(t *testing.T) {
	// Arrange
	result := fixtureResultWithSubjectVersion("v1.0.0")

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: well-known per-run fields from the original schema must all
	// still be present. These are the fields wireRunReport had before this
	// stage; adding subject_version must not rename, repurpose, or remove any.
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
				"adding subject_version must be a purely additive change — "+
				"no existing field may be removed or renamed",
				field)
		}
	}
}

// --- helpers ---

// fixtureResultWithSubjectVersion builds a minimal report.Result containing
// exactly one test with one run whose SubjectVersion is set to version. It is
// the minimal fixture needed to exercise the subject-version rendering path
// without pulling in the full fixtureFullResult fixture.
func fixtureResultWithSubjectVersion(version string) report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "subject-version-test-suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Tests: []report.TestReport{
			{
				TestName:      "version-attribution-test",
				Description: "subject version appears in the report",
				Layer:       domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:   "version-attribution-test",
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
						Key:            domain.RunKey{RunID: "run-001", TestName: "version-attribution-test", RunNumber: 1},
						Verdict:        domain.VerdictPass,
						Duration:       1 * time.Second,
						SubjectVersion: version,
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

// decodeJSONRunSubjectVersion decodes the subject_version field from the
// first run of the first test in the JSON output, so an assertion can check
// the wire value without depending on the entire document shape.
func decodeJSONRunSubjectVersion(t *testing.T, data []byte) string {
	t.Helper()
	var doc struct {
		Tests []struct {
			Runs []struct {
				SubjectVersion string `json:"subject_version"`
			} `json:"runs"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding JSON report: %v", err)
	}
	if len(doc.Tests) == 0 || len(doc.Tests[0].Runs) == 0 {
		t.Fatal("JSON report has no tests or no runs to decode subject_version from")
	}
	return doc.Tests[0].Runs[0].SubjectVersion
}
