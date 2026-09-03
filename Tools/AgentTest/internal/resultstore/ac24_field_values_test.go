package resultstore_test

// Tests for resultstore wire field value assertions (AC2.4).
//
// These tests verify that TestReportWire.TestName, TestReportWire.TestID (numeric),
// and RunReportWire.TestVersion carry correct values when parsed from report JSON.
// They are TDD RED-phase tests: each currently fails because the wire struct field
// types and testdata fixtures have not yet been updated to the new wire format.
//
// Expected failure state before wire struct and fixture updates:
//   - TestResultStore_TestReportWire_TestName_CarriesDisplayName: fails because
//     valid_report.json uses "test_id" (not "test_name") for the string display name.
//     TestName is empty until the fixture is updated to use the "test_name" JSON key.
//   - TestResultStore_TestReportWire_NumericTestID_CarriesValue: fails because
//     TestReportWire.TestID is currently a string field. json.Unmarshal rejects a
//     numeric "test_id" value for a string receiver, causing ParseAndValidate to error.
//   - TestResultStore_RunReportWire_TestVersion_CarriesValue: fails because
//     valid_report.json runs have no "test_version" field. TestVersion is zero until
//     the fixture is updated to include that key.
//
// After the wire struct field type changes and testdata fixture updates are applied,
// all three tests pass.

import (
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/resultstore"
)

// TestResultStore_TestReportWire_TestName_CarriesDisplayName verifies that
// TestReportWire.TestName is populated from the "test_name" JSON key.
//
// Currently fails because testdata/valid_report.json has the string test identity
// under "test_id" rather than "test_name", so TestName is empty after parsing.
// After testdata/valid_report.json is updated to use "test_name": "test-one",
// TestName carries the display name and this test passes.
func TestResultStore_TestReportWire_TestName_CarriesDisplayName(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected ParseAndValidate error: %v", err)
	}

	// valid_report.json identifies the test as "test-one". After the fixture
	// is updated to carry the display name under "test_name" instead of "test_id",
	// TestName carries this value. Currently zero because the fixture uses the
	// old "test_id" key, which is not mapped to TestReportWire.TestName.
	const wantName = "test-one"
	if parsed.Raw.Tests[0].TestName != wantName {
		t.Errorf("Tests[0].TestName = %q, want %q -- testdata fixture must use \"test_name\" JSON key to populate TestReportWire.TestName",
			parsed.Raw.Tests[0].TestName, wantName)
	}
}

// TestResultStore_TestReportWire_NumericTestID_CarriesValue verifies that
// TestReportWire.TestID carries a numeric identity when "test_id" in the report
// JSON holds an integer.
//
// Currently fails because TestReportWire.TestID is declared as string. Parsing a
// JSON document whose "test_id" key holds an integer (42) into a string field causes
// json.Unmarshal to return an UnmarshalTypeError, and ParseAndValidate propagates it.
// After TestReportWire.TestID is changed to int, parsing succeeds and the numeric
// value round-trips correctly through the struct.
func TestResultStore_TestReportWire_NumericTestID_CarriesValue(t *testing.T) {
	// Inline fixture using the post-rename wire format:
	//   "test_name" carries the string display name
	//   "test_id"   carries the numeric identity (42)
	// Parsing this document currently fails because TestReportWire.TestID is string.
	data := []byte(`{
		"schema_version": "1",
		"suite_id": "wire-fields",
		"started_at": "2026-08-26T00:00:00Z",
		"finished_at": "2026-08-26T00:01:00Z",
		"tests": [{
			"test_name": "my-test",
			"test_id": 42,
			"description": "numeric test_id fixture",
			"layer": "orchestrator",
			"aggregate": {
				"verdict": "pass",
				"counted": 1,
				"passed": 1,
				"pass_rate": 1.0,
				"infrastructure_failure": false,
				"total_cost": {"total_usd": 0, "attribution": ""}
			},
			"runs": [{
				"subject_version": "v1.0.0",
				"subject_model": "provider/model",
				"harness_id": "test-harness",
				"verdict": "pass",
				"duration_ms": 100,
				"cost": {"total_usd": 0, "attribution": ""},
				"termination_reason": "completed",
				"conditions": []
			}]
		}],
		"counts": {"pass": 1},
		"total_cost": {"total_usd": 0, "attribution": ""},
		"infrastructure_failures": 0
	}`)

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		// Red phase: TestReportWire.TestID is string; parsing a numeric "test_id"
		// value causes json.Unmarshal to return a type error. Changing TestID to int
		// lets this fixture parse successfully.
		t.Fatalf("ParseAndValidate returned error for numeric test_id fixture -- TestReportWire.TestID must be changed to int: %v", err)
	}

	// Verify the numeric test_id value round-trips through the wire struct.
	// We marshal Tests[0] back to JSON and decode into map[string]any so the
	// assertion does not depend on the specific Go field name in TestReportWire.
	rawJSON, marshalErr := json.Marshal(parsed.Raw.Tests[0])
	if marshalErr != nil {
		t.Fatalf("failed to re-marshal Tests[0]: %v", marshalErr)
	}
	var m map[string]any
	if decodeErr := json.Unmarshal(rawJSON, &m); decodeErr != nil {
		t.Fatalf("failed to decode re-marshaled Tests[0]: %v", decodeErr)
	}

	rawTestID, present := m["test_id"]
	if !present {
		t.Fatal("test_id field absent from re-marshaled TestReportWire -- the numeric TestID int field must be present with json:\"test_id\" tag")
	}
	numericID, isFloat := rawTestID.(float64) // JSON numbers decode to float64 in map[string]any
	if !isFloat {
		t.Fatalf("test_id must be numeric (float64 in map[string]any), got %T: %v -- TestReportWire.TestID must be int, not string", rawTestID, rawTestID)
	}
	if int(numericID) != 42 {
		t.Errorf("TestReportWire test_id = %d, want 42", int(numericID))
	}
}

// TestResultStore_AggregateWire_Excluded_ParsesFromJSON verifies that
// AggregateWire.Excluded is populated when the "excluded" key is present in
// the aggregate object.
//
// Fails before AggregateWire gains an Excluded int field with json:"excluded".
// After that field is added, parsing a report containing "excluded": 5 in the
// aggregate block populates AggregateWire.Excluded == 5.
func TestResultStore_AggregateWire_Excluded_ParsesFromJSON(t *testing.T) {
	// Inline fixture with "excluded": 5 in the aggregate block.
	data := []byte(`{
		"schema_version": "1",
		"suite_id": "excluded-field",
		"started_at": "2026-09-01T00:00:00Z",
		"finished_at": "2026-09-01T00:01:00Z",
		"tests": [{
			"test_name": "test-excl",
			"test_id": 1,
			"description": "excluded field test",
			"layer": "orchestrator",
			"aggregate": {
				"verdict": "pass",
				"counted": 10,
				"passed": 8,
				"pass_rate": 0.8,
				"infrastructure_failure": false,
				"total_cost": {"total_usd": 0, "attribution": ""},
				"excluded": 5
			},
			"runs": [{
				"subject_version": "v1.0.0",
				"subject_model": "provider/model",
				"harness_id": "test-harness",
				"verdict": "pass",
				"duration_ms": 100,
				"cost": {"total_usd": 0, "attribution": ""},
				"termination_reason": "completed",
				"conditions": []
			}]
		}],
		"counts": {"pass": 1},
		"total_cost": {"total_usd": 0, "attribution": ""},
		"infrastructure_failures": 0
	}`)

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("ParseAndValidate returned unexpected error: %v", err)
	}

	const wantExcluded = 5
	if parsed.Raw.Tests[0].Aggregate.Excluded != wantExcluded {
		t.Errorf("AggregateWire.Excluded = %d, want %d -- AggregateWire must have Excluded int field with json:\"excluded\" tag",
			parsed.Raw.Tests[0].Aggregate.Excluded, wantExcluded)
	}
}

// TestResultStore_AggregateWire_Excluded_DefaultsToZeroWhenAbsent verifies
// backward compatibility: when "excluded" is absent from the aggregate JSON
// object (as in all pre-Stage-2 report files), AggregateWire.Excluded decodes
// to 0 without error.
func TestResultStore_AggregateWire_Excluded_DefaultsToZeroWhenAbsent(t *testing.T) {
	// Use valid_report.json which predates the Excluded field and has no
	// "excluded" key in any aggregate block.
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("ParseAndValidate returned error for legacy fixture without excluded field: %v", err)
	}

	for i, tr := range parsed.Raw.Tests {
		if tr.Aggregate.Excluded != 0 {
			t.Errorf("Tests[%d].Aggregate.Excluded = %d, want 0 for legacy fixture without excluded field",
				i, tr.Aggregate.Excluded)
		}
	}
}

// TestResultStore_AggregateWire_ExcludedJSONFieldName_IsExcluded locks down
// the JSON key name for AggregateWire.Excluded. Marshalling an AggregateWire
// with a non-zero Excluded back to JSON must produce a key named exactly
// "excluded" -- not "Excluded", "excluded_count", or any other variant.
//
// This guards against accidental field renames that would break the wire format.
func TestResultStore_AggregateWire_ExcludedJSONFieldName_IsExcluded(t *testing.T) {
	// Inline fixture with "excluded": 7 in the aggregate block.
	data := []byte(`{
		"schema_version": "1",
		"suite_id": "field-lockdown",
		"started_at": "2026-09-01T00:00:00Z",
		"finished_at": "2026-09-01T00:01:00Z",
		"tests": [{
			"test_name": "lockdown-test",
			"test_id": 1,
			"description": "field name lockdown",
			"layer": "orchestrator",
			"aggregate": {
				"verdict": "pass",
				"counted": 4,
				"passed": 4,
				"pass_rate": 1.0,
				"infrastructure_failure": false,
				"total_cost": {"total_usd": 0, "attribution": ""},
				"excluded": 7
			},
			"runs": [{
				"subject_version": "v1.0.0",
				"subject_model": "provider/model",
				"harness_id": "test-harness",
				"verdict": "pass",
				"duration_ms": 100,
				"cost": {"total_usd": 0, "attribution": ""},
				"termination_reason": "completed",
				"conditions": []
			}]
		}],
		"counts": {"pass": 1},
		"total_cost": {"total_usd": 0, "attribution": ""},
		"infrastructure_failures": 0
	}`)

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("ParseAndValidate returned unexpected error: %v", err)
	}

	// Re-marshal the aggregate and decode into map[string]any so we can assert
	// on the JSON key name without depending on any specific Go field name.
	rawJSON, marshalErr := json.Marshal(parsed.Raw.Tests[0].Aggregate)
	if marshalErr != nil {
		t.Fatalf("failed to re-marshal AggregateWire: %v", marshalErr)
	}
	var m map[string]any
	if decodeErr := json.Unmarshal(rawJSON, &m); decodeErr != nil {
		t.Fatalf("failed to decode re-marshaled AggregateWire: %v", decodeErr)
	}

	rawExcluded, present := m["excluded"]
	if !present {
		t.Fatal("\"excluded\" key absent from re-marshaled AggregateWire -- the Excluded field must carry json:\"excluded\" tag")
	}
	numericVal, isFloat := rawExcluded.(float64)
	if !isFloat {
		t.Fatalf("\"excluded\" must be numeric (float64 in map[string]any), got %T: %v", rawExcluded, rawExcluded)
	}
	if int(numericVal) != 7 {
		t.Errorf("AggregateWire \"excluded\" round-trips as %d, want 7", int(numericVal))
	}
}

// TestResultStore_RunReportWire_TestVersion_CarriesValue verifies that
// RunReportWire.TestVersion is populated from the "test_version" JSON key.
//
// Currently fails because testdata/valid_report.json runs have no "test_version"
// field, so TestVersion defaults to zero. After valid_report.json is updated to
// include "test_version": 1 in each run, TestVersion carries the correct value.
//
// Note: the expected value 1 must match the "test_version" value added to
// testdata/valid_report.json when fixtures are updated to the new wire format.
func TestResultStore_RunReportWire_TestVersion_CarriesValue(t *testing.T) {
	data := loadFixture(t, "valid_report.json")

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("unexpected ParseAndValidate error: %v", err)
	}

	// valid_report.json runs must carry "test_version": 1 after fixtures are updated.
	// Currently fails because the existing fixture has no "test_version" field and
	// TestVersion defaults to zero.
	const wantVersion = 1
	if parsed.Raw.Tests[0].Runs[0].TestVersion != wantVersion {
		t.Errorf("Tests[0].Runs[0].TestVersion = %d, want %d -- testdata fixture run must include \"test_version\": %d to populate RunReportWire.TestVersion",
			parsed.Raw.Tests[0].Runs[0].TestVersion, wantVersion, wantVersion)
	}
}
