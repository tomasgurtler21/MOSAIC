package report_test

// Tests for the required_pass_rate field appearing in the machine-readable
// (JSON) report's aggregate object. Both renderings must surface the required
// threshold once it exists, so they do not diverge: the human-readable report
// shows "required N%" per test and the machine-readable report carries
// "required_pass_rate": <float> in the aggregate object.
//
// Adding required_pass_rate is additive — no existing field is removed or
// retyped — so the schema version does not change and existing consumers keep
// working.
//
// These are TDD RED phase tests: they fail until the JSON renderer is extended
// to include required_pass_rate in the wireAggregate type.

import (
	"encoding/json"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// TestRenderJSON_RequiredPassRateAppearsInAggregate asserts that the
// machine-readable report's per-test aggregate object carries a
// "required_pass_rate" field matching the AggregateResult's value. A consumer
// of the machine-readable report must be able to compare the achieved pass rate
// against the required threshold without re-deriving it from the test's
// repetition policy.
func TestRenderJSON_RequiredPassRateAppearsInAggregate(t *testing.T) {
	// Arrange: a test with a 0.8 required pass rate
	const wantRequired = 0.8
	result := fixtureResultWithRepetitionStats(4, 5, 0.8, wantRequired, domain.VerdictPass)

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: "required_pass_rate" key is present in the JSON output
	if !strings.Contains(string(out), `"required_pass_rate"`) {
		t.Errorf("JSON output has no 'required_pass_rate' key in the aggregate;\n"+
			"want the machine-readable report to carry the required threshold so a "+
			"consumer can compare achieved vs required without knowing the policy.\n"+
			"--- got ---\n%s", out)
	}

	// Assert: the value matches the AggregateResult field
	gotRequired := decodeJSONAggregateRequiredPassRate(t, out)
	if gotRequired != wantRequired {
		t.Errorf("required_pass_rate in JSON aggregate = %v, want %v;\n"+
			"the wire value must match AggregateResult.RequiredPassRate exactly",
			gotRequired, wantRequired)
	}
}

// TestRenderJSON_RequiredPassRateWithFailingTest_IsRecordedVerbatim asserts
// that the required pass rate appears in the JSON aggregate even when the test
// failed to meet it. The value is a statement of the declared policy, not of
// whether the test passed.
func TestRenderJSON_RequiredPassRateWithFailingTest_IsRecordedVerbatim(t *testing.T) {
	// Arrange: 3/5 passed, required 80% — test fails
	result := fixtureResultWithRepetitionStats(3, 5, 0.6, 0.8, domain.VerdictFail)

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: "required_pass_rate" key present
	if !strings.Contains(string(out), `"required_pass_rate"`) {
		t.Errorf("JSON output has no 'required_pass_rate' key for a failing test;\n"+
			"the required threshold must appear in the JSON aggregate regardless of "+
			"whether the test passed or failed.\n"+
			"--- got ---\n%s", out)
	}

	// Assert: the value is 0.8 (from AggregateResult.RequiredPassRate)
	gotRequired := decodeJSONAggregateRequiredPassRate(t, out)
	if gotRequired != 0.8 {
		t.Errorf("required_pass_rate in JSON aggregate = %v, want 0.8", gotRequired)
	}
}

// TestRenderJSON_RequiredPassRateIsAdditive_ExistingAggregateFieldsPreserved
// asserts that adding required_pass_rate to the aggregate wire object does not
// displace or rename any field that was already present. The JSON schema is
// documented as additive-only from SchemaVersion "1".
func TestRenderJSON_RequiredPassRateIsAdditive_ExistingAggregateFieldsPreserved(t *testing.T) {
	// Arrange
	result := fixtureResultWithRepetitionStats(3, 5, 0.6, 0.8, domain.VerdictFail)

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: every existing aggregate field from the original schema is still present
	existingAggregateFields := []string{
		`"test_id"`,
		`"verdict"`,
		`"reasons"`,
		`"counted"`,
		`"passed"`,
		`"excluded"`,
		`"pass_rate"`,
		`"infrastructure_failure"`,
		`"total_cost"`,
	}
	jsonStr := string(out)
	for _, field := range existingAggregateFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("existing aggregate field %s is absent from the JSON output;\n"+
				"adding required_pass_rate must be a purely additive change — "+
				"no existing field may be removed or renamed",
				field)
		}
	}
}

// --- helpers ---

// decodeJSONAggregateRequiredPassRate decodes the required_pass_rate field from
// the first test's aggregate in the JSON output. Returns 0 when the field is
// absent, which is what causes the RED test to fail: 0 != the expected non-zero value.
func decodeJSONAggregateRequiredPassRate(t *testing.T, data []byte) float64 {
	t.Helper()
	var doc struct {
		Tests []struct {
			Aggregate struct {
				RequiredPassRate float64 `json:"required_pass_rate"`
			} `json:"aggregate"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding JSON report: %v", err)
	}
	if len(doc.Tests) == 0 {
		t.Fatal("JSON report has no tests to decode required_pass_rate from")
	}
	return doc.Tests[0].Aggregate.RequiredPassRate
}
