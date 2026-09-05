package resultstore_test

// Tests for the ToolVersion field on resultstore.ReportWire.
//
// These tests cover:
//   - ReportWire.ToolVersion is populated when a report JSON includes "tool_version".
//   - ReportWire.ToolVersion is empty string (not an error) when a report JSON
//     lacks "tool_version" -- backward compatibility with pre-feature reports.
//   - The field is distinct from SchemaVersion in the parsed wire struct.
//
// TDD RED state: these tests will fail to compile until resultstore.ReportWire gains
// a ToolVersion field, because they reference parsed.Raw.ToolVersion directly.

import (
	"encoding/json"
	"testing"
	"time"

	"mosaic-agent-test/internal/resultstore"
)

// minimalValidReportJSON returns a JSON byte slice representing the minimum valid
// report that ParseAndValidate accepts. The caller can set toolVersion to a non-empty
// string to include the "tool_version" field, or leave it empty to omit the field
// (simulating a pre-feature report).
//
// The fixture satisfies all four invariants that ParseAndValidate enforces:
//   - non-empty schema_version
//   - non-empty suite_id
//   - non-empty tests array (at least one test entry)
//   - at least one run in tests[0]
func minimalValidReportJSON(toolVersion string) []byte {
	oneRun := map[string]interface{}{
		"subject_version":    "v0.0.1",
		"subject_model":      "test-model",
		"harness_id":         "test-harness",
		"verdict":            "pass",
		"duration_ms":        1000,
		"cost":               map[string]interface{}{"total_usd": 0.0, "attribution": "attributed"},
		"termination_reason": "completed",
		"conditions":         []interface{}{},
		"test_version":       1,
	}
	oneTest := map[string]interface{}{
		"test_name":   "fixture-test",
		"test_id":     1,
		"description": "fixture test for tool_version tests",
		"layer":       "orchestrator",
		"aggregate": map[string]interface{}{
			"verdict":                "pass",
			"counted":                1,
			"passed":                 1,
			"pass_rate":              1.0,
			"infrastructure_failure": false,
			"total_cost":             map[string]interface{}{"total_usd": 0.0, "attribution": "attributed"},
		},
		"runs": []interface{}{oneRun},
	}
	base := map[string]interface{}{
		"schema_version":          "1",
		"suite_id":                "suite-tool-version-test",
		"started_at":              time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"finished_at":             time.Date(2026, 9, 5, 14, 5, 0, 0, time.UTC).Format(time.RFC3339),
		"tests":                   []interface{}{oneTest},
		"counts":                  map[string]int{"pass": 1},
		"total_cost":              map[string]interface{}{"total_usd": 0.0, "attribution": "attributed"},
		"infrastructure_failures": 0,
	}
	if toolVersion != "" {
		base["tool_version"] = toolVersion
	}
	data, err := json.Marshal(base)
	if err != nil {
		panic("minimalValidReportJSON: json.Marshal failed: " + err.Error())
	}
	return data
}

// ---------------------------------------------------------------------------
// ReportWire.ToolVersion populated from JSON
// ---------------------------------------------------------------------------

// TestReportWire_ToolVersion_PopulatedFromJSON verifies that when a report JSON
// includes a "tool_version" field, ParseAndValidate populates ReportWire.ToolVersion
// with that value. This is the primary data-flow assertion for the new field.
func TestReportWire_ToolVersion_PopulatedFromJSON(t *testing.T) {
	const version = "1.0.0"
	data := minimalValidReportJSON(version)

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("ParseAndValidate returned unexpected error: %v", err)
	}

	// parsed.Raw.ToolVersion references the new field on ReportWire.
	// This line fails to compile until ReportWire gains a ToolVersion field.
	if parsed.Raw.ToolVersion != version {
		t.Errorf("ReportWire.ToolVersion = %q, want %q (the field must be populated from the \"tool_version\" JSON key)",
			parsed.Raw.ToolVersion, version)
	}
}

// TestReportWire_ToolVersion_RoundTripsValue verifies that the exact string value
// of "tool_version" in the report JSON is preserved without transformation in
// ReportWire.ToolVersion. No normalisation, truncation, or case change may occur.
func TestReportWire_ToolVersion_RoundTripsValue(t *testing.T) {
	const version = "2.3.14"
	data := minimalValidReportJSON(version)

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("ParseAndValidate returned unexpected error: %v", err)
	}

	if parsed.Raw.ToolVersion != version {
		t.Errorf("ReportWire.ToolVersion = %q, want %q (value must round-trip from JSON without transformation)",
			parsed.Raw.ToolVersion, version)
	}
}

// ---------------------------------------------------------------------------
// Backward compatibility: pre-feature reports (no "tool_version" key)
// ---------------------------------------------------------------------------

// TestReportWire_ToolVersion_AbsentInJSON_EmptyString verifies that when a report
// JSON does NOT include a "tool_version" field -- as all pre-feature reports do not --
// ParseAndValidate succeeds and ReportWire.ToolVersion is the empty string. An empty
// string is the correct sentinel for "not recorded", not an error.
//
// This test verifies the backward-compatibility contract: existing stored reports
// must continue to be readable after the field is added.
func TestReportWire_ToolVersion_AbsentInJSON_EmptyString(t *testing.T) {
	// Build JSON without "tool_version" key -- simulates a pre-feature stored report.
	data := minimalValidReportJSON("") // empty string causes the key to be omitted

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("ParseAndValidate returned error for pre-feature report (no tool_version key): %v\nThis breaks backward compatibility -- old reports must still parse without error", err)
	}

	// ToolVersion must be empty string, not any default or fallback value.
	if parsed.Raw.ToolVersion != "" {
		t.Errorf("ReportWire.ToolVersion = %q for pre-feature report, want empty string (absent field must decode to zero value, not a default)",
			parsed.Raw.ToolVersion)
	}
}

// TestReportWire_ToolVersion_AbsentInJSON_NoParseError verifies specifically that
// ParseAndValidate does not return an error when "tool_version" is absent from the
// report JSON. The field is optional and additive-only; its absence is not an error
// condition. Pre-feature reports should parse cleanly.
func TestReportWire_ToolVersion_AbsentInJSON_NoParseError(t *testing.T) {
	data := minimalValidReportJSON("")

	_, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Errorf("ParseAndValidate returned error %v for pre-feature report lacking tool_version; want nil error (backward compatibility requires pre-feature reports to parse cleanly)", err)
	}
}

// ---------------------------------------------------------------------------
// ToolVersion is distinct from SchemaVersion in the wire struct
// ---------------------------------------------------------------------------

// TestReportWire_ToolVersion_DistinctFromSchemaVersion verifies that ReportWire.ToolVersion
// and ReportWire.SchemaVersion are separate fields populated from separate JSON keys.
// Setting "tool_version" in JSON must not affect SchemaVersion, and vice versa.
func TestReportWire_ToolVersion_DistinctFromSchemaVersion(t *testing.T) {
	const toolVer = "1.0.0"
	data := minimalValidReportJSON(toolVer)

	parsed, err := resultstore.ParseAndValidate(data)
	if err != nil {
		t.Fatalf("ParseAndValidate returned unexpected error: %v", err)
	}

	if parsed.Raw.SchemaVersion == parsed.Raw.ToolVersion {
		t.Errorf("ReportWire.SchemaVersion (%q) equals ReportWire.ToolVersion (%q); they must be distinct fields from distinct JSON keys",
			parsed.Raw.SchemaVersion, parsed.Raw.ToolVersion)
	}
	if parsed.Raw.SchemaVersion == "" {
		t.Errorf("ReportWire.SchemaVersion is empty after parsing; it must be populated from \"schema_version\" regardless of \"tool_version\" presence")
	}
	if parsed.Raw.ToolVersion != toolVer {
		t.Errorf("ReportWire.ToolVersion = %q, want %q", parsed.Raw.ToolVersion, toolVer)
	}
}
