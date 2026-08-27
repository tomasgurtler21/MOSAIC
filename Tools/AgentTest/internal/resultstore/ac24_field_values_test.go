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
