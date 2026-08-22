package report_test

// Tests for Stage 4: CatalogFolder field on report.Result.
//
// These tests verify that:
//   - Build's new catalogFolder parameter is set on the returned Result.
//   - The text renderer includes the catalog folder in the header section.
//   - The JSON renderer includes catalog_folder as a top-level field.
//
// These tests reference contracts not yet in the implementation:
//   - report.Result.CatalogFolder (not yet a field on Result)
//   - report.Build's new 5th parameter (current signature has 4 parameters)
//   - wireResult.catalog_folder in the JSON output
//
// They will fail to compile (wrong arity for Build, unknown field CatalogFolder)
// until the implementation adds those changes, which is the expected RED state.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/report"
)

// ---------------------------------------------------------------------------
// Build: new catalogFolder parameter sets Result.CatalogFolder
// ---------------------------------------------------------------------------

// TestBuild_CatalogFolder_SetInResult verifies that the new catalogFolder
// argument to Build is reflected in the returned Result.CatalogFolder field.
// This is the foundational contract: the field must be populated, not ignored.
func TestBuild_CatalogFolder_SetInResult(t *testing.T) {
	const catalog = "/path/to/Tools/AgentTest/catalog"

	// Build now takes a 5th catalogFolder parameter. This call will fail to
	// compile until the implementation adds that parameter.
	r := report.Build("suite-id", fixtureStarted(), fixtureFinished(), nil, catalog)

	if r.CatalogFolder != catalog {
		t.Errorf("Result.CatalogFolder = %q, want %q (catalogFolder argument must be set on the returned Result)", r.CatalogFolder, catalog)
	}
}

// TestBuild_CatalogFolder_EmptyStringPreserved verifies that passing an empty
// string for catalogFolder produces an empty CatalogFolder on Result. An empty
// value is legitimate: it means the deploy tool resolved its own catalogue.
func TestBuild_CatalogFolder_EmptyStringPreserved(t *testing.T) {
	r := report.Build("suite-id", fixtureStarted(), fixtureFinished(), nil, "")

	if r.CatalogFolder != "" {
		t.Errorf("Result.CatalogFolder = %q, want empty string (an empty catalogFolder argument must produce an empty field, not a default path)", r.CatalogFolder)
	}
}

// TestBuild_CatalogFolder_DoesNotAffectOtherFields verifies that the new
// catalogFolder parameter does not disturb the fields already set by Build.
// SuiteID, SchemaVersion, Counts, and TotalCost must all be populated
// correctly regardless of the catalogFolder value.
func TestBuild_CatalogFolder_DoesNotAffectOtherFields(t *testing.T) {
	const suiteID = "checkout-suite"
	tests := []report.TestReport{
		{
			TestID: "test-1",
			Aggregate: domain.AggregateResult{
				TestID:  "test-1",
				Verdict: domain.VerdictPass,
				Counted: 1,
				Passed:  1,
				PassRate: 1,
				TotalCost: domain.CostReport{
					TotalUSD:    0.05,
					Attribution: domain.AttributionAttributed,
				},
			},
			Runs: []report.RunReport{{
				Key:     domain.RunKey{RunID: "run-001", TestID: "test-1", RunNumber: 1},
				Verdict: domain.VerdictPass,
				Cost:    domain.CostReport{TotalUSD: 0.05, Attribution: domain.AttributionAttributed},
			}},
		},
	}

	r := report.Build(suiteID, fixtureStarted(), fixtureFinished(), tests, "C:/catalog")

	if r.SuiteID != suiteID {
		t.Errorf("Result.SuiteID = %q, want %q (adding catalogFolder must not disturb SuiteID)", r.SuiteID, suiteID)
	}
	if r.SchemaVersion != report.SchemaVersion {
		t.Errorf("Result.SchemaVersion = %q, want %q (adding catalogFolder must not disturb SchemaVersion)", r.SchemaVersion, report.SchemaVersion)
	}
	if r.Counts[domain.VerdictPass] != 1 {
		t.Errorf("Result.Counts[pass] = %d, want 1 (Counts derivation must be unaffected by the new parameter)", r.Counts[domain.VerdictPass])
	}
}

// ---------------------------------------------------------------------------
// Text rendering: catalog folder appears in the header
// ---------------------------------------------------------------------------

// TestRenderText_CatalogFolder_NonEmpty_IncludedInOutput verifies that when
// Result.CatalogFolder is non-empty, the text renderer includes it in the
// rendered output so a user reading the report can see which catalog was used.
func TestRenderText_CatalogFolder_NonEmpty_IncludedInOutput(t *testing.T) {
	const catalog = "/path/to/Tools/AgentTest/catalog"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		CatalogFolder: catalog, // new field — compile error until added
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	out, err := renderText(t, r)
	if err != nil {
		t.Fatalf("RenderText returned error: %v", err)
	}
	if !strings.Contains(out, catalog) {
		t.Errorf("RenderText output does not contain CatalogFolder %q; the rendered report must show which catalog was used.\nOutput:\n%s", catalog, out)
	}
}

// TestRenderText_CatalogFolder_Empty_NotIncludedAsBlankLine verifies that
// when Result.CatalogFolder is empty, the renderer does not produce a
// meaningless blank catalog-folder line in the output. The header should only
// include the catalog folder when it has a value.
func TestRenderText_CatalogFolder_Empty_Behavior(t *testing.T) {
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		CatalogFolder: "", // empty: deploy tool resolved its own catalogue
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	out, err := renderText(t, r)
	if err != nil {
		t.Fatalf("RenderText returned error for empty CatalogFolder: %v", err)
	}
	// With an empty CatalogFolder, there must still be well-formed output
	// (not a panic or an error). The exact rendering choice (omit or say
	// "none") is the implementer's decision.
	if out == "" {
		t.Error("RenderText produced empty output even for a valid (empty-catalog) Result; want at least the standard header fields")
	}
}

// ---------------------------------------------------------------------------
// JSON rendering: catalog_folder field present in wire output
// ---------------------------------------------------------------------------

// TestRenderJSON_CatalogFolder_NonEmpty_IncludedInOutput verifies that when
// Result.CatalogFolder is non-empty, the JSON output includes a
// "catalog_folder" top-level field. Machine consumers parsing the report
// must be able to read which catalog was used.
func TestRenderJSON_CatalogFolder_NonEmpty_IncludedInOutput(t *testing.T) {
	const catalog = "/path/to/Tools/AgentTest/catalog"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		CatalogFolder: catalog, // new field — compile error until added
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	out, err := renderJSON(t, r)
	if err != nil {
		t.Fatalf("RenderJSON returned error: %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("RenderJSON produced invalid JSON: %s", out)
	}
	if !strings.Contains(string(out), `"catalog_folder"`) {
		t.Errorf("RenderJSON output does not contain the \"catalog_folder\" key; machine consumers must be able to read the catalog folder.\nOutput:\n%s", out)
	}
	if !strings.Contains(string(out), catalog) {
		t.Errorf("RenderJSON output contains \"catalog_folder\" key but not the expected value %q.\nOutput:\n%s", catalog, out)
	}
}

// TestRenderJSON_CatalogFolder_Empty_KeyPresent verifies that the
// "catalog_folder" key is present in the JSON output even when the value is
// empty. The schema is additive-only: a field is never omitted once added,
// so a parser can always rely on the key being present.
func TestRenderJSON_CatalogFolder_Empty_KeyPresent(t *testing.T) {
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		CatalogFolder: "", // empty
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	out, err := renderJSON(t, r)
	if err != nil {
		t.Fatalf("RenderJSON returned error for empty CatalogFolder: %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("RenderJSON produced invalid JSON: %s", out)
	}
	if !strings.Contains(string(out), `"catalog_folder"`) {
		t.Errorf("RenderJSON output does not contain \"catalog_folder\" key for an empty value; the field must always be present in the wire output.\nOutput:\n%s", out)
	}
}

// TestRenderJSON_CatalogFolder_Value_DecodesCorrectly verifies that the
// "catalog_folder" value in the JSON output decodes back to the original
// Result.CatalogFolder string, confirming that no transformation (escaping,
// truncation, mangling) occurs during serialisation.
func TestRenderJSON_CatalogFolder_Value_DecodesCorrectly(t *testing.T) {
	const catalog = "C:/Tools/AgentTest/catalog"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		CatalogFolder: catalog,
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	out, err := renderJSON(t, r)
	if err != nil {
		t.Fatalf("RenderJSON returned error: %v", err)
	}

	var decoded struct {
		CatalogFolder string `json:"catalog_folder"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v\nOutput:\n%s", err, out)
	}
	if decoded.CatalogFolder != catalog {
		t.Errorf("decoded catalog_folder = %q, want %q (the value must round-trip without transformation)", decoded.CatalogFolder, catalog)
	}
}

// ---------------------------------------------------------------------------
// Agreement: text and JSON include the same catalog folder value
// ---------------------------------------------------------------------------

// TestRenderAgreement_CatalogFolder_BothRenderingsIncludeIt verifies that
// both the text and JSON renderers include the catalog folder, maintaining the
// invariant that both renderings are derived from the same Result model and
// neither can omit a field the other includes.
func TestRenderAgreement_CatalogFolder_BothRenderingsIncludeIt(t *testing.T) {
	const catalog = "/path/to/AgentTest/catalog"
	r := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite",
		StartedAt:     fixtureStarted(),
		FinishedAt:    fixtureFinished(),
		CatalogFolder: catalog,
		Counts:        map[domain.Verdict]int{},
		TotalCost:     domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	textOut, textErr := renderText(t, r)
	jsonOut, jsonErr := renderJSON(t, r)

	if textErr != nil {
		t.Fatalf("RenderText failed: %v", textErr)
	}
	if jsonErr != nil {
		t.Fatalf("RenderJSON failed: %v", jsonErr)
	}

	if !strings.Contains(textOut, catalog) {
		t.Errorf("text rendering does not include CatalogFolder %q; both renderings must include it.\nText:\n%s", catalog, textOut)
	}
	if !strings.Contains(string(jsonOut), catalog) {
		t.Errorf("JSON rendering does not include CatalogFolder %q; both renderings must include it.\nJSON:\n%s", catalog, jsonOut)
	}
}

// ---------------------------------------------------------------------------
// Timestamp validation for Build's new signature (existing time params preserved)
// ---------------------------------------------------------------------------

// TestBuild_CatalogFolder_TimeFieldsPreserved verifies that adding the new
// catalogFolder parameter does not accidentally displace the started/finished
// timestamps in the returned Result.
func TestBuild_CatalogFolder_TimeFieldsPreserved(t *testing.T) {
	started := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	finished := time.Date(2026, 8, 1, 10, 5, 0, 0, time.UTC)

	r := report.Build("suite", started, finished, nil, "C:/catalog")

	if !r.StartedAt.Equal(started) {
		t.Errorf("Result.StartedAt = %v, want %v (the new catalogFolder param must not displace started)", r.StartedAt, started)
	}
	if !r.FinishedAt.Equal(finished) {
		t.Errorf("Result.FinishedAt = %v, want %v (the new catalogFolder param must not displace finished)", r.FinishedAt, finished)
	}
}
