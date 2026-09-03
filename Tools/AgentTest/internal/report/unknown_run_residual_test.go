package report_test

// Tests for Stage 4: report-level unknown-run residual error field.
//
// Design specifies:
//   - report.Build scans each test's Aggregate.TotalCost.UnknownRunResidual
//     and, when the sum > 0, appends a ReportError with Kind ==
//     ErrorUnknownRunResidual and Count == the sum.
//   - Result.Errors is always [] (never null) on the wire.
//   - NewSession aggregates Errors across suites.
//   - Session.Errors is always [] (never null).
//   - RenderJSON includes "errors" as a top-level JSON field.
//
// TDD RED phase: all tests here FAIL until the corresponding implementation
// tasks (I4.3, I4.4) add the logic to Build, NewSession, and toWireResult.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

var buildTime = time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)

// costReportWithResidual returns a CostReport whose UnknownRunResidual is set.
func costReportWithResidual(residual int) domain.CostReport {
	return domain.CostReport{
		TotalUSD:           0.50,
		Attribution:        domain.AttributionAttributed,
		UnknownRunResidual: residual,
	}
}

// testReportWithResidual builds a minimal TestReport whose aggregate carries
// the given UnknownRunResidual in TotalCost.
func testReportWithResidual(name string, residual int) report.TestReport {
	return report.TestReport{
		TestName: name,
		Aggregate: domain.AggregateResult{
			TestName:  name,
			Verdict:   domain.VerdictPass,
			Counted:   1,
			Passed:    1,
			PassRate:  1.0,
			TotalCost: costReportWithResidual(residual),
		},
	}
}

// testReportNoResidual builds a minimal TestReport with no unknown-run residual.
func testReportNoResidual(name string) report.TestReport {
	return report.TestReport{
		TestName: name,
		Aggregate: domain.AggregateResult{
			TestName:  name,
			Verdict:   domain.VerdictPass,
			Counted:   1,
			Passed:    1,
			PassRate:  1.0,
			TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
		},
	}
}

// --- report.Build: Errors field ---

// TestBuild_NoResidual_ErrorsIsEmptySlice verifies that when no test has an
// unknown-run residual, Result.Errors is an empty slice (never nil). An empty
// nil slice would render as "null" in JSON instead of "[]", breaking the
// machine-readable contract.
//
// FAILS until Build initializes Errors to []ReportError{} when no residual
// is found (I4.3).
func TestBuild_NoResidual_ErrorsIsEmptySlice(t *testing.T) {
	// Arrange: two tests with no residual
	tests := []report.TestReport{
		testReportNoResidual("checkout-happy"),
		testReportNoResidual("checkout-reject"),
	}

	// Act
	result := report.Build("suite-no-residual", buildTime, buildTime, tests, "")

	// Assert: Errors is non-nil (empty slice, not nil)
	if result.Errors == nil {
		t.Error("Result.Errors is nil, want [] (empty slice) -- nil renders as JSON null, not []")
	}
	if len(result.Errors) != 0 {
		t.Errorf("len(Result.Errors) = %d, want 0 when no test has unknown-run residual", len(result.Errors))
	}
}

// TestBuild_WithResidual_ErrorsContainsUnknownRunResidualEntry verifies that
// when at least one test has UnknownRunResidual > 0, Build appends a
// ReportError with Kind == ErrorUnknownRunResidual.
//
// FAILS until Build scans TotalCost.UnknownRunResidual and populates Errors
// (I4.3).
func TestBuild_WithResidual_ErrorsContainsUnknownRunResidualEntry(t *testing.T) {
	// Arrange: one test with residual = 3
	tests := []report.TestReport{
		testReportWithResidual("agent-test-one", 3),
	}

	// Act
	result := report.Build("suite-with-residual", buildTime, buildTime, tests, "")

	// Assert: exactly one error entry of the expected kind
	if len(result.Errors) == 0 {
		t.Fatal("Result.Errors is empty, want one entry with Kind == ErrorUnknownRunResidual")
	}
	if len(result.Errors) != 1 {
		t.Errorf("len(Result.Errors) = %d, want 1 -- Build must aggregate all residuals into a single entry, not one entry per test", len(result.Errors))
	}
	if result.Errors[0].Kind != report.ErrorUnknownRunResidual {
		t.Errorf("Result.Errors[0].Kind = %q, want %q", result.Errors[0].Kind, report.ErrorUnknownRunResidual)
	}
}

// TestBuild_WithResidual_ErrorCountIsSummedAcrossTests verifies that when
// multiple tests have residuals, the single ReportError entry carries their
// combined count.
//
// FAILS until Build sums residuals from all tests (I4.3).
func TestBuild_WithResidual_ErrorCountIsSummedAcrossTests(t *testing.T) {
	// Arrange: two tests with residuals 2 and 5 -> total 7
	tests := []report.TestReport{
		testReportWithResidual("agent-alpha", 2),
		testReportNoResidual("agent-normal"),
		testReportWithResidual("agent-beta", 5),
	}

	// Act
	result := report.Build("suite-multi-residual", buildTime, buildTime, tests, "")

	// Assert: one error entry with count = 7
	if len(result.Errors) == 0 {
		t.Fatal("Result.Errors is empty, want one entry with summed residual count")
	}
	if len(result.Errors) != 1 {
		t.Errorf("len(Result.Errors) = %d, want 1 -- Build must aggregate all residuals into a single entry, not one entry per test", len(result.Errors))
	}
	if result.Errors[0].Count != 7 {
		t.Errorf("Result.Errors[0].Count = %d, want 7 (2+5 residuals summed across all affected tests)", result.Errors[0].Count)
	}
}

// TestBuild_WithResidual_ErrorDetailIsHumanReadable verifies that the
// ReportError entry includes a non-empty Detail string describing the
// condition in human-readable terms.
//
// FAILS until Build populates the Detail field (I4.3).
func TestBuild_WithResidual_ErrorDetailIsHumanReadable(t *testing.T) {
	// Arrange
	tests := []report.TestReport{
		testReportWithResidual("agent-test", 2),
	}

	// Act
	result := report.Build("suite-detail-check", buildTime, buildTime, tests, "")

	// Assert
	if len(result.Errors) == 0 {
		t.Fatal("Result.Errors is empty, want one entry")
	}
	if result.Errors[0].Detail == "" {
		t.Error("Result.Errors[0].Detail is empty, want a human-readable description of the unknown-run residual condition")
	}
}

// TestBuild_WithResidual_ErrorDetailMentionsCount verifies that the error
// detail names the residual count so a human reader does not have to parse
// the Count field to understand the magnitude.
//
// FAILS until Build populates the Detail field with the count (I4.3).
func TestBuild_WithResidual_ErrorDetailMentionsCount(t *testing.T) {
	// Arrange: residual = 9 -> detail should mention "9"
	tests := []report.TestReport{
		testReportWithResidual("agent-test", 9),
	}

	// Act
	result := report.Build("suite-detail-count", buildTime, buildTime, tests, "")

	// Assert
	if len(result.Errors) == 0 {
		t.Fatal("Result.Errors is empty, want one entry")
	}
	detail := result.Errors[0].Detail
	if detail == "" {
		t.Fatal("Detail is empty")
	}
	// The detail string must contain the count so a human reader gets the number.
	if !strings.Contains(detail, "9") {
		t.Errorf("Result.Errors[0].Detail = %q, want it to name the residual count (9)", detail)
	}
}

// --- report.NewSession: Errors aggregation ---

// TestNewSession_NoErrors_ErrorsIsEmptySlice verifies that when no suite has
// errors, Session.Errors is [] (not nil). Same nil-vs-empty rule as
// Result.Errors.
//
// FAILS until NewSession initializes Errors to []ReportError{} when empty
// (I4.3).
func TestNewSession_NoErrors_ErrorsIsEmptySlice(t *testing.T) {
	// Arrange: two suites with no errors
	suiteA := report.Result{
		SuiteID:   "a",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
		Errors:    []report.ReportError{},
	}
	suiteB := report.Result{
		SuiteID:   "b",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
		Errors:    []report.ReportError{},
	}

	// Act
	session := report.NewSession([]report.Result{suiteA, suiteB}, nil, false)

	// Assert
	if session.Errors == nil {
		t.Error("Session.Errors is nil, want [] (empty slice) -- nil renders as JSON null, not []")
	}
	if len(session.Errors) != 0 {
		t.Errorf("len(Session.Errors) = %d, want 0 when no suite has errors", len(session.Errors))
	}
}

// TestNewSession_ErrorsFromMultipleSuites_AreAggregated verifies that NewSession
// concatenates the Errors from all executed suites into Session.Errors, so a
// caller can inspect all report-level errors in one place.
//
// FAILS until NewSession aggregates errors from suite Results (I4.3).
func TestNewSession_ErrorsFromMultipleSuites_AreAggregated(t *testing.T) {
	// Arrange: suite A has one residual error, suite B has one residual error
	errorA := report.ReportError{
		Kind:   report.ErrorUnknownRunResidual,
		Detail: "3 unknown-run usage record(s) could not be attributed after merging",
		Count:  3,
	}
	errorB := report.ReportError{
		Kind:   report.ErrorUnknownRunResidual,
		Detail: "5 unknown-run usage record(s) could not be attributed after merging",
		Count:  5,
	}

	suiteA := report.Result{
		SuiteID:   "suite-a",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
		Errors:    []report.ReportError{errorA},
	}
	suiteB := report.Result{
		SuiteID:   "suite-b",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
		Errors:    []report.ReportError{errorB},
	}
	suiteC := report.Result{
		SuiteID:   "suite-c",
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
		Errors:    []report.ReportError{},
	}

	// Act
	session := report.NewSession([]report.Result{suiteA, suiteB, suiteC}, nil, false)

	// Assert: two errors combined (one from A, one from B)
	if len(session.Errors) != 2 {
		t.Errorf("len(Session.Errors) = %d, want 2 (one from each suite with an error)", len(session.Errors))
	}
}

// TestNewSession_FromNoSuites_ErrorsIsEmptySlice verifies that an empty
// session has Session.Errors == [] (not nil).
//
// FAILS until NewSession initializes Errors to []ReportError{} (I4.3).
func TestNewSession_FromNoSuites_ErrorsIsEmptySlice(t *testing.T) {
	// Act
	session := report.NewSession(nil, nil, false)

	// Assert
	if session.Errors == nil {
		t.Error("Session.Errors is nil for an empty session, want [] (empty slice)")
	}
}

// --- RenderJSON: "errors" field in wire output ---

// TestRenderJSON_ErrorsFieldIsPresent verifies that the JSON output includes
// a top-level "errors" field. This is the machine-readable contract per FR-9.
//
// FAILS until toWireResult and wireResult include the errors field (I4.4).
func TestRenderJSON_ErrorsFieldIsPresent(t *testing.T) {
	// Arrange: a result with no errors
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "test-suite",
		StartedAt:     buildTime,
		FinishedAt:    buildTime,
		Tests:         []report.TestReport{},
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		Errors:        []report.ReportError{},
	}

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: "errors" key is present in the JSON document
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("JSON output could not be decoded: %v", err)
	}
	if _, ok := decoded["errors"]; !ok {
		t.Error(`JSON output has no "errors" field, want a top-level "errors" array per FR-9`)
	}
}

// TestRenderJSON_ErrorsIsEmptyArrayNotNull verifies that when Result.Errors is
// empty, the JSON output renders as "errors":[] rather than "errors":null.
// A consumer that checks for the key must always get an array, never null.
//
// FAILS until toWireResult ensures the errors slice is never nil (I4.4).
func TestRenderJSON_ErrorsIsEmptyArrayNotNull(t *testing.T) {
	// Arrange: result with empty Errors
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "test-suite",
		StartedAt:     buildTime,
		FinishedAt:    buildTime,
		Tests:         []report.TestReport{},
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		Errors:        []report.ReportError{},
	}

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: the "errors" value is [] not null
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("JSON output could not be decoded: %v", err)
	}
	raw, ok := decoded["errors"]
	if !ok {
		t.Fatal(`JSON output has no "errors" field`)
	}
	if string(raw) == "null" {
		t.Error(`"errors" JSON value is null, want [] -- consumers must always receive an array, never null`)
	}
	// Verify it is a JSON array
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Errorf(`"errors" value %s is not a JSON array: %v`, string(raw), err)
	}
}

// TestRenderJSON_ErrorsFieldContainsPopulatedEntries verifies that when
// Result.Errors has entries, they appear in the JSON output with the expected
// fields: kind, detail, count.
//
// FAILS until toWireResult encodes errors into wireReportError entries (I4.4).
func TestRenderJSON_ErrorsFieldContainsPopulatedEntries(t *testing.T) {
	// Arrange: result with one error entry
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "test-suite",
		StartedAt:     buildTime,
		FinishedAt:    buildTime,
		Tests:         []report.TestReport{},
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		Errors: []report.ReportError{
			{
				Kind:   report.ErrorUnknownRunResidual,
				Detail: "2 unknown-run usage record(s) could not be attributed after merging",
				Count:  2,
			},
		},
	}

	// Act
	out, err := renderJSON(t, result)
	if err != nil {
		t.Fatalf("RenderJSON returned an error: %v", err)
	}

	// Assert: decode and inspect the errors array
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("JSON output could not be decoded: %v", err)
	}
	raw, ok := decoded["errors"]
	if !ok {
		t.Fatal(`JSON output has no "errors" field`)
	}

	var errors []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &errors); err != nil {
		t.Fatalf(`"errors" value could not be decoded as array: %v`, err)
	}
	if len(errors) != 1 {
		t.Fatalf("len(errors) = %d, want 1", len(errors))
	}

	entry := errors[0]
	if _, ok := entry["kind"]; !ok {
		t.Error(`errors[0] missing "kind" field`)
	}
	if _, ok := entry["detail"]; !ok {
		t.Error(`errors[0] missing "detail" field`)
	}
	if _, ok := entry["count"]; !ok {
		t.Error(`errors[0] missing "count" field`)
	}

	var kind string
	if err := json.Unmarshal(entry["kind"], &kind); err == nil {
		if kind != string(report.ErrorUnknownRunResidual) {
			t.Errorf(`errors[0].kind = %q, want %q`, kind, report.ErrorUnknownRunResidual)
		}
	}

	var count int
	if err := json.Unmarshal(entry["count"], &count); err == nil {
		if count != 2 {
			t.Errorf(`errors[0].count = %d, want 2`, count)
		}
	}
}
