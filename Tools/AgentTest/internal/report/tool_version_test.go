package report_test

// Tests for the ToolVersion field on report.Result.
//
// These tests cover:
//   - Result.ToolVersion flows through to the "tool_version" JSON key in RenderJSON output.
//   - RenderText includes a "Tool version:" line in the header when ToolVersion is non-empty.
//   - RenderText omits the "Tool version:" line when ToolVersion is empty (omit-when-empty
//     convention, same as CatalogFolder).
//   - The ToolVersion field is structurally separate from SchemaVersion and SubjectVersion.
//   - report.Build signature is unchanged: ToolVersion is set on the returned Result at the
//     call site, not passed as a Build parameter.
//
// TDD RED state: these tests will fail to compile until report.Result gains a ToolVersion
// field, because they reference r.ToolVersion directly. They will begin to pass only once
// the implementation adds that field and wires it through RenderJSON and RenderText.

import (
	"encoding/json"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// ---------------------------------------------------------------------------
// report.Result.ToolVersion field existence and distinctness
// ---------------------------------------------------------------------------

// TestResult_ToolVersion_FieldExists verifies that report.Result has a ToolVersion
// field and that it can be set and read. This is the foundational compile-time
// assertion: the field must exist before any renderer can consume it.
func TestResult_ToolVersion_FieldExists(t *testing.T) {
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		ToolVersion:   "1.0.0", // NEW field -- compile error until report.Result gains ToolVersion
	}

	if r.ToolVersion != "1.0.0" {
		t.Errorf("Result.ToolVersion = %q, want %q (the field must carry the value that was assigned)", r.ToolVersion, "1.0.0")
	}
}

// TestResult_ToolVersion_DistinctFromSchemaVersion verifies that ToolVersion and
// SchemaVersion are separate fields: setting one does not affect the other. They
// serve fundamentally different purposes -- SchemaVersion identifies the wire-format
// version, ToolVersion identifies the binary that produced the report.
func TestResult_ToolVersion_DistinctFromSchemaVersion(t *testing.T) {
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		ToolVersion:   "1.0.0",
	}

	if r.SchemaVersion == r.ToolVersion {
		t.Errorf("Result.SchemaVersion (%q) equals Result.ToolVersion (%q); they must be distinct fields serving distinct purposes",
			r.SchemaVersion, r.ToolVersion)
	}
	if r.SchemaVersion != report.SchemaVersion {
		t.Errorf("Result.SchemaVersion = %q, want %q (setting ToolVersion must not disturb SchemaVersion)",
			r.SchemaVersion, report.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// report.Build signature is unchanged (ToolVersion is set post-Build)
// ---------------------------------------------------------------------------

// TestBuild_SignatureUnchanged verifies that report.Build still accepts exactly
// the five parameters it has always taken: suiteID, started, finished, tests,
// catalogFolder. ToolVersion must NOT be added as a Build parameter; it is set
// on the returned Result at the call site.
//
// This test compiles if and only if Build's signature matches
//   Build(suiteID string, started, finished time.Time, tests []TestReport, catalogFolder string) Result
// Any addition or removal of parameters causes a compile error that breaks this test.
func TestBuild_SignatureUnchanged(t *testing.T) {
	// Call Build with the original five parameters. If the signature changes this
	// line will fail to compile, which is the intended gate.
	r := report.Build("suite-id", fixtureStarted(), fixtureFinished(), nil, "")

	// ToolVersion is empty because it has not been set at the call site.
	// The test verifies that Build returns a zero-valued ToolVersion (not a default).
	if r.ToolVersion != "" {
		t.Errorf("Build() with no post-Build ToolVersion assignment returned ToolVersion = %q, want empty string (ToolVersion must default to empty, not to any built-in value)", r.ToolVersion)
	}
}

// TestBuild_ToolVersion_SetPostBuild verifies that setting ToolVersion on the
// Result returned by Build is the intended wiring pattern: Build returns a Result,
// the caller sets ToolVersion, and the Result carries the value correctly.
func TestBuild_ToolVersion_SetPostBuild(t *testing.T) {
	r := report.Build("suite-id", fixtureStarted(), fixtureFinished(), nil, "")
	r.ToolVersion = "1.0.0" // post-Build assignment -- the wiring pattern documented in plan

	if r.ToolVersion != "1.0.0" {
		t.Errorf("Result.ToolVersion after post-Build assignment = %q, want %q", r.ToolVersion, "1.0.0")
	}
	// Verify Build's other fields are still correct after the post-Build field set.
	if r.SuiteID != "suite-id" {
		t.Errorf("Result.SuiteID = %q, want %q (post-Build ToolVersion assignment must not disturb SuiteID)", r.SuiteID, "suite-id")
	}
	if r.SchemaVersion != report.SchemaVersion {
		t.Errorf("Result.SchemaVersion = %q, want %q (post-Build ToolVersion assignment must not disturb SchemaVersion)", r.SchemaVersion, report.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// JSON rendering: tool_version field
// ---------------------------------------------------------------------------

// TestRenderJSON_ToolVersion_NonEmpty_KeyPresent verifies that when
// Result.ToolVersion is non-empty, the JSON output produced by RenderJSON
// includes a "tool_version" top-level field with the correct value.
// Machine consumers that need to know which binary produced the report must
// be able to read this field.
func TestRenderJSON_ToolVersion_NonEmpty_KeyPresent(t *testing.T) {
	const version = "1.0.0"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		ToolVersion:   version,
	}

	out, err := renderJSON(t, r)
	if err != nil {
		t.Fatalf("RenderJSON returned error: %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("RenderJSON produced invalid JSON: %s", out)
	}
	if !strings.Contains(string(out), `"tool_version"`) {
		t.Errorf("RenderJSON output does not contain the \"tool_version\" key when ToolVersion is %q; the field must appear in JSON output when non-empty.\nOutput:\n%s", version, out)
	}
	if !strings.Contains(string(out), version) {
		t.Errorf("RenderJSON output contains \"tool_version\" key but not the expected value %q.\nOutput:\n%s", version, out)
	}
}

// TestRenderJSON_ToolVersion_NonEmpty_ValueRoundTrips verifies that the value of
// "tool_version" in the JSON output decodes back to the original ToolVersion string,
// confirming no transformation occurs during serialisation.
func TestRenderJSON_ToolVersion_NonEmpty_ValueRoundTrips(t *testing.T) {
	const version = "1.0.0"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		ToolVersion:   version,
	}

	out, err := renderJSON(t, r)
	if err != nil {
		t.Fatalf("RenderJSON returned error: %v", err)
	}

	var decoded struct {
		ToolVersion string `json:"tool_version"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\nOutput:\n%s", err, out)
	}
	if decoded.ToolVersion != version {
		t.Errorf("decoded tool_version = %q, want %q (the value must round-trip without transformation)", decoded.ToolVersion, version)
	}
}

// TestRenderJSON_ToolVersion_Empty_KeyAbsent verifies that when Result.ToolVersion
// is empty, the JSON output does NOT include a "tool_version" key. The field uses
// omitempty so that pre-feature report consumers do not see an unexpected empty field,
// and older parsers that ignore unknown fields continue to work unmodified.
func TestRenderJSON_ToolVersion_Empty_KeyAbsent(t *testing.T) {
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		// ToolVersion intentionally zero -- not set
	}

	out, err := renderJSON(t, r)
	if err != nil {
		t.Fatalf("RenderJSON returned error for empty ToolVersion: %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("RenderJSON produced invalid JSON: %s", out)
	}
	if strings.Contains(string(out), `"tool_version"`) {
		t.Errorf("RenderJSON output contains \"tool_version\" key when ToolVersion is empty; the field must be omitted (omitempty) when not set.\nOutput:\n%s", out)
	}
}

// TestRenderJSON_ToolVersion_DistinctFromSchemaVersion verifies that the JSON
// output carries both "schema_version" and "tool_version" as separate keys and
// that they hold independent values.
func TestRenderJSON_ToolVersion_DistinctFromSchemaVersion(t *testing.T) {
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		ToolVersion:   "1.0.0",
	}

	out, err := renderJSON(t, r)
	if err != nil {
		t.Fatalf("RenderJSON returned error: %v", err)
	}

	var decoded struct {
		SchemaVersion string `json:"schema_version"`
		ToolVersion   string `json:"tool_version"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\nOutput:\n%s", err, out)
	}
	if decoded.SchemaVersion == decoded.ToolVersion {
		t.Errorf("schema_version (%q) equals tool_version (%q) in JSON output; they must be distinct fields", decoded.SchemaVersion, decoded.ToolVersion)
	}
	if decoded.SchemaVersion != report.SchemaVersion {
		t.Errorf("schema_version = %q in JSON output, want %q (setting ToolVersion must not affect SchemaVersion in JSON)", decoded.SchemaVersion, report.SchemaVersion)
	}
	if decoded.ToolVersion != "1.0.0" {
		t.Errorf("tool_version = %q in JSON output, want %q", decoded.ToolVersion, "1.0.0")
	}
}

// ---------------------------------------------------------------------------
// Text rendering: Tool version header line
// ---------------------------------------------------------------------------

// TestRenderText_ToolVersion_NonEmpty_IncludedInHeader verifies that when
// Result.ToolVersion is non-empty, the text rendering produced by RenderText
// includes the tool version string in its output. A user reading the report
// must be able to identify which binary produced it.
func TestRenderText_ToolVersion_NonEmpty_IncludedInHeader(t *testing.T) {
	const version = "1.0.0"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		ToolVersion:   version,
	}

	out, err := renderText(t, r)
	if err != nil {
		t.Fatalf("RenderText returned error: %v", err)
	}
	if !strings.Contains(out, version) {
		t.Errorf("RenderText output does not contain ToolVersion %q; the report header must show which binary version produced this report.\nOutput:\n%s", version, out)
	}
}

// TestRenderText_ToolVersion_NonEmpty_HeaderLineFormat verifies that the tool
// version appears in the header as a "Tool version: X.Y.Z" line, following the
// convention used by other header fields. The exact prefix "Tool version:" must
// be present so a reader can identify the field unambiguously.
func TestRenderText_ToolVersion_NonEmpty_HeaderLineFormat(t *testing.T) {
	const version = "1.0.0"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		ToolVersion:   version,
	}

	out, err := renderText(t, r)
	if err != nil {
		t.Fatalf("RenderText returned error: %v", err)
	}
	const wantPrefix = "Tool version: "
	if !strings.Contains(out, wantPrefix+version) {
		t.Errorf("RenderText output does not contain %q; want a header line in the form \"Tool version: 1.0.0\".\nOutput:\n%s",
			wantPrefix+version, out)
	}
}

// TestRenderText_ToolVersion_Empty_LineOmitted verifies that when Result.ToolVersion
// is empty, the text rendering does NOT include a "Tool version:" line. Omitting the
// line when the value is empty follows the omit-when-empty convention used by
// CatalogFolder, which is the correct model for report-level header fields.
// (Per-run fields like SubjectVersion use "unknown" because every run always has a
// subject; report-level fields use omit-when-empty because absence is legitimate.)
func TestRenderText_ToolVersion_Empty_LineOmitted(t *testing.T) {
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		// ToolVersion intentionally zero
	}

	out, err := renderText(t, r)
	if err != nil {
		t.Fatalf("RenderText returned error for empty ToolVersion: %v", err)
	}
	if out == "" {
		t.Fatal("RenderText produced empty output for a valid Result; want at least the standard header fields")
	}
	if strings.Contains(out, "Tool version:") {
		t.Errorf("RenderText output contains \"Tool version:\" when ToolVersion is empty; the line must be omitted when ToolVersion is not set.\nOutput:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Agreement: both renderings reflect ToolVersion consistently
// ---------------------------------------------------------------------------

// TestRenderAgreement_ToolVersion_BothRenderingsIncludeIt verifies that when
// ToolVersion is non-empty, both the text and JSON renderings include the version
// string, maintaining the invariant that both renderings are derived from the same
// Result model and neither omits information the other includes.
func TestRenderAgreement_ToolVersion_BothRenderingsIncludeIt(t *testing.T) {
	const version = "1.0.0"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
		ToolVersion:   version,
	}

	textOut, textErr := renderText(t, r)
	jsonOut, jsonErr := renderJSON(t, r)

	if textErr != nil {
		t.Fatalf("RenderText failed: %v", textErr)
	}
	if jsonErr != nil {
		t.Fatalf("RenderJSON failed: %v", jsonErr)
	}

	if !strings.Contains(textOut, version) {
		t.Errorf("text rendering does not include ToolVersion %q; both renderings must include it.\nText:\n%s", version, textOut)
	}
	if !strings.Contains(string(jsonOut), version) {
		t.Errorf("JSON rendering does not include ToolVersion %q; both renderings must include it.\nJSON:\n%s", version, jsonOut)
	}
}
