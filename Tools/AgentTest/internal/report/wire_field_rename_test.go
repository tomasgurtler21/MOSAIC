package report_test

// Tests verifying the wire JSON output shape after the test_id (string) ->
// test_name rename and the addition of a numeric test_id, test_version.
//
// These tests drive the TDD RED phase: they compile against the current code
// and fail because the current wire structs still emit a string test_id where
// test_name is expected, and do not yet emit a numeric test_id or test_version
// at all. Once I2.4-I2.8 update the wire structs and conversion functions, the
// tests will pass without modification (except renaming TestID->TestName in
// the fixture once I2.1 lands).
//
// Each test decodes the rendered JSON into a generic map rather than a typed
// struct, so the assertions target the wire field names directly — exactly the
// contract a downstream JSON consumer (resultstore, resultsummary) depends on.

import (
	"encoding/json"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// wireRenameFixture builds a minimal report.Result that exercises every wire
// path the rename and addition tests need to observe: one test with one run,
// one exclusion, a numeric ID, and a content version.
func wireRenameFixture() report.Result {
	return report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "rename-suite",
		StartedAt:     time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC),
		FinishedAt:    time.Date(2026, 8, 26, 10, 5, 0, 0, time.UTC),
		Tests: []report.TestReport{
			{
				TestName:    "my-test", // will become TestName after I2.1
				NumericID: 42,
				Layer:     domain.LayerSubagent,
				Aggregate: domain.AggregateResult{
					TestName:    "my-test", // will become TestName after I2.1
					NumericID: 42,
					Verdict:   domain.VerdictPass,
					Counted:   1,
					Passed:    1,
					PassRate:  1.0,
					TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
					Exclusions: []domain.ExcludedRun{
						{
							Key:    domain.RunKey{RunID: "run-1", TestName: "my-test", RunNumber: 2},
							Reason: domain.ExclusionStateIntegrity,
							Detail: "lock was reclaimed",
						},
					},
					Excluded: 1,
				},
				Runs: []report.RunReport{
					{
						Key:         domain.RunKey{RunID: "run-1", TestName: "my-test", RunNumber: 1},
						Verdict:     domain.VerdictPass,
						Duration:    2 * time.Second,
						Cost:        domain.CostReport{Attribution: domain.AttributionAttributed},
						TestVersion: 3,
						NumericID:   42,
					},
				},
			},
		},
		Counts: map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}
}

// decodeJSON renders result to JSON and decodes it into a generic map, so
// assertions target actual wire field names rather than Go struct field names.
func decodeWireJSON(t *testing.T, result report.Result) map[string]any {
	t.Helper()
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("rendered output is not valid JSON: %v", err)
	}
	return decoded
}

// firstTest returns tests[0] from the decoded JSON document.
func firstTest(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	tests, ok := doc["tests"].([]any)
	if !ok || len(tests) == 0 {
		t.Fatal("decoded JSON has no tests array")
	}
	tr, ok := tests[0].(map[string]any)
	if !ok {
		t.Fatal("tests[0] is not a JSON object")
	}
	return tr
}

// firstRun returns tests[0].runs[0] from the decoded JSON document.
func firstRun(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	tr := firstTest(t, doc)
	runs, ok := tr["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatal("tests[0] has no runs array")
	}
	rr, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatal("tests[0].runs[0] is not a JSON object")
	}
	return rr
}

// firstExclusion returns tests[0].aggregate.exclusions[0] from the decoded JSON.
func firstExclusion(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	tr := firstTest(t, doc)
	agg, ok := tr["aggregate"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].aggregate is not a JSON object")
	}
	excls, ok := agg["exclusions"].([]any)
	if !ok || len(excls) == 0 {
		t.Fatal("tests[0].aggregate.exclusions is empty or not an array")
	}
	ex, ok := excls[0].(map[string]any)
	if !ok {
		t.Fatal("exclusions[0] is not a JSON object")
	}
	return ex
}

// TestWireTestReport_HasTestNameField verifies that the wireTestReport emits a
// test_name field (string) instead of a string test_id field. The downstream
// consumer (resultstore) reads test_name to identify which test a report
// describes; a missing field silently yields an empty string.
func TestWireTestReport_HasTestNameField(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)

	if _, ok := tr["test_name"]; !ok {
		t.Error("wireTestReport is missing test_name field: the string identifier must be renamed from test_id to test_name in the wire output")
	}
}

// TestWireTestReport_TestNameCarriesDisplayName verifies that the test_name
// field in wireTestReport carries the human-readable display name, not an
// empty string or a numeric value.
func TestWireTestReport_TestNameCarriesDisplayName(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)

	name, _ := tr["test_name"].(string)
	if name != "my-test" {
		t.Errorf("wireTestReport.test_name = %q, want %q", name, "my-test")
	}
}

// TestWireTestReport_HasNumericTestID verifies that wireTestReport emits a
// numeric test_id field (integer) separate from the renamed string test_name
// field. The numeric test_id is the stable identity for cross-rename tracking.
func TestWireTestReport_HasNumericTestID(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)

	raw, ok := tr["test_id"]
	if !ok {
		t.Error("wireTestReport is missing test_id field: a numeric test_id must be present alongside test_name")
		return
	}
	// JSON numbers decode as float64. The field must be numeric, not a string.
	if _, ok := raw.(float64); !ok {
		t.Errorf("wireTestReport.test_id has type %T, want a numeric (float64) value — the string identifier was renamed to test_name; test_id must now be the integer numeric identity", raw)
	}
}

// TestWireTestReport_NumericTestIDCarriesValue verifies the numeric test_id in
// wireTestReport carries the definition's NumericID, not zero.
func TestWireTestReport_NumericTestIDCarriesValue(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)

	id, _ := tr["test_id"].(float64)
	if id != 42 {
		t.Errorf("wireTestReport.test_id = %v, want 42", id)
	}
}

// TestWireAggregate_HasTestNameField verifies that wireAggregate emits
// test_name (string) instead of a string test_id, mirroring wireTestReport.
func TestWireAggregate_HasTestNameField(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)
	agg, ok := tr["aggregate"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].aggregate is not a JSON object")
	}

	if _, ok := agg["test_name"]; !ok {
		t.Error("wireAggregate is missing test_name field: the string identifier must be renamed from test_id to test_name")
	}
}

// TestWireAggregate_HasNumericTestID verifies that wireAggregate emits a
// numeric test_id field alongside test_name.
func TestWireAggregate_HasNumericTestID(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)
	agg, ok := tr["aggregate"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].aggregate is not a JSON object")
	}

	raw, ok := agg["test_id"]
	if !ok {
		t.Error("wireAggregate is missing test_id field: a numeric test_id must be present alongside test_name")
		return
	}
	if _, ok := raw.(float64); !ok {
		t.Errorf("wireAggregate.test_id has type %T, want numeric (float64)", raw)
	}
}

// TestWireAggregate_TestNameCarriesDisplayName verifies that the test_name
// field in wireAggregate carries the human-readable display name, not an empty
// string. The existence check in TestWireAggregate_HasTestNameField does not
// catch a conversion function that maps the wrong source field (e.g., an empty
// string or an unrelated domain field).
func TestWireAggregate_TestNameCarriesDisplayName(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)
	agg, ok := tr["aggregate"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].aggregate is not a JSON object")
	}

	name, _ := agg["test_name"].(string)
	if name != "my-test" {
		t.Errorf("wireAggregate.test_name = %q, want %q", name, "my-test")
	}
}

// TestWireAggregate_NumericTestIDCarriesValue verifies the numeric test_id in
// wireAggregate carries the AggregateResult.NumericID value, not zero. A
// conversion function that emits a zero-value numeric test_id would pass the
// type check in TestWireAggregate_HasNumericTestID but silently discard the
// actual identity value.
func TestWireAggregate_NumericTestIDCarriesValue(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)
	agg, ok := tr["aggregate"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].aggregate is not a JSON object")
	}

	id, _ := agg["test_id"].(float64)
	if id != 42 {
		t.Errorf("wireAggregate.test_id = %v, want 42", id)
	}
}

// TestWireNoStringTestIDInAggregate verifies that the string test_id field no
// longer appears at the wireAggregate level. AC2.1 requires no string test_id
// anywhere in report JSON; the aggregate level must be consistent with the
// wireTestReport and wireRunKey levels where equivalent checks already exist.
func TestWireNoStringTestIDInAggregate(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)
	agg, ok := tr["aggregate"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].aggregate is not a JSON object")
	}

	// If test_id is present and is a string, it is the old field that was not
	// renamed. A numeric test_id is allowed (the new field reusing the key).
	if raw, ok := agg["test_id"]; ok {
		if _, isString := raw.(string); isString {
			t.Errorf("wireAggregate.test_id is a string %q: the string identifier must be renamed to test_name; test_id must be the integer numeric identity only", raw)
		}
	}
}

// TestWireRunKey_HasTestNameField verifies that wireRunKey emits test_name
// (string) instead of test_id. Every run key in the wire output — both in
// runs and in exclusions — must use test_name.
func TestWireRunKey_HasTestNameField(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	run := firstRun(t, doc)

	runKey, ok := run["run"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].runs[0].run is not a JSON object")
	}

	if _, ok := runKey["test_name"]; !ok {
		t.Error("wireRunKey is missing test_name field: the run key's test identifier must be renamed from test_id to test_name")
	}
}

// TestWireRunKey_TestNameCarriesDisplayName verifies the test_name field in a
// wireRunKey carries the display name, not an empty string.
func TestWireRunKey_TestNameCarriesDisplayName(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	run := firstRun(t, doc)
	runKey, ok := run["run"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].runs[0].run is not a JSON object")
	}

	name, _ := runKey["test_name"].(string)
	if name != "my-test" {
		t.Errorf("wireRunKey.test_name = %q, want %q", name, "my-test")
	}
}

// TestWireRunReport_HasTestVersion verifies that wireRunReport emits a
// test_version field (integer). The per-run content version lets a downstream
// consumer detect whether a stored result was produced by an older version of
// the test definition.
func TestWireRunReport_HasTestVersion(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	run := firstRun(t, doc)

	raw, ok := run["test_version"]
	if !ok {
		t.Error("wireRunReport is missing test_version field: the content version must be present at the per-run level in the wire output")
		return
	}
	if _, ok := raw.(float64); !ok {
		t.Errorf("wireRunReport.test_version has type %T, want numeric (float64)", raw)
	}
}

// TestWireRunReport_TestVersionCarriesValue verifies the test_version field in
// wireRunReport carries the TestVersion from the RunReport, not zero.
func TestWireRunReport_TestVersionCarriesValue(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	run := firstRun(t, doc)

	v, _ := run["test_version"].(float64)
	if v != 3 {
		t.Errorf("wireRunReport.test_version = %v, want 3", v)
	}
}

// TestWireRunReport_HasNumericTestID verifies that wireRunReport emits a
// numeric test_id field at the per-run level. A downstream consumer reading
// the per-run wire output can identify which test definition the run belongs
// to by stable numeric identity, not only by display name.
func TestWireRunReport_HasNumericTestID(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	run := firstRun(t, doc)

	raw, ok := run["test_id"]
	if !ok {
		t.Error("wireRunReport is missing test_id field: a numeric test_id must be present in the per-run wire output")
		return
	}
	if _, ok := raw.(float64); !ok {
		t.Errorf("wireRunReport.test_id has type %T, want numeric (float64)", raw)
	}
}

// TestWireRunReport_NumericTestIDCarriesValue verifies the numeric test_id in
// wireRunReport carries the RunReport.NumericID, not zero.
func TestWireRunReport_NumericTestIDCarriesValue(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	run := firstRun(t, doc)

	id, _ := run["test_id"].(float64)
	if id != 42 {
		t.Errorf("wireRunReport.test_id = %v, want 42", id)
	}
}

// TestWireExcludedRun_RunKey_HasTestNameField verifies that the run key inside
// a wireExcludedRun also emits test_name rather than test_id. Exclusion
// entries share the same wireRunKey shape as run entries, so the rename must
// be consistent across all key occurrences in the wire output.
func TestWireExcludedRun_RunKey_HasTestNameField(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	ex := firstExclusion(t, doc)

	key, ok := ex["key"].(map[string]any)
	if !ok {
		t.Fatal("exclusions[0].key is not a JSON object")
	}

	if _, ok := key["test_name"]; !ok {
		t.Error("wireExcludedRun run key is missing test_name field: exclusion run keys must use test_name, not test_id, consistent with all other wireRunKey occurrences")
	}
}

// TestWireNoStringTestIDInTestReport verifies that the string test_id field no
// longer appears at the wireTestReport level — it has been renamed to test_name
// and must not be present under both names simultaneously.
func TestWireNoStringTestIDInTestReport(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	tr := firstTest(t, doc)

	// If test_id is present and is a string, it is the old field that was not
	// renamed. A numeric test_id is allowed (the new field reusing the key).
	if raw, ok := tr["test_id"]; ok {
		if _, isString := raw.(string); isString {
			t.Errorf("wireTestReport.test_id is a string %q: the string identifier must be renamed to test_name; test_id must be the integer numeric identity only", raw)
		}
	}
}

// TestWireNoStringTestIDInRunKey verifies that no string test_id field appears
// in the wireRunKey — it must have been renamed to test_name.
func TestWireNoStringTestIDInRunKey(t *testing.T) {
	doc := decodeWireJSON(t, wireRenameFixture())
	run := firstRun(t, doc)
	runKey, ok := run["run"].(map[string]any)
	if !ok {
		t.Fatal("tests[0].runs[0].run is not a JSON object")
	}

	if raw, ok := runKey["test_id"]; ok {
		if _, isString := raw.(string); isString {
			t.Errorf("wireRunKey.test_id is a string %q: the string test identifier must be renamed to test_name; no string test_id must remain in wireRunKey", raw)
		}
	}
}
