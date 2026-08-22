package tui

// catalog_folder_test.go covers the Stage 4 TUI catalog-folder feature:
//
//   (a) The TUI suite-select screen offers a catalog-folder field, showing the
//       current value before a run starts so the user can see and edit it.
//   (b) When the user sets a catalog-folder override, it flows through
//       preflight.Overrides.CatalogFolder in startSelectedSuite() to the
//       preflight call.
//   (d) The TUI displays the resolved catalog folder before a run starts
//       (the editable field doubles as display).
//
// T4.4 (detail screen): The TUI detail screen reads Result.CatalogFolder
//       (suite-level) and shows it so the user can confirm which catalog was
//       actually used for the completed run.
//
// All tests are in package tui (not tui_test) so they can access the Model's
// unexported fields and use the shared helpers from testhelpers_test.go.
//
// NOTE: These tests are in TDD RED phase. They reference:
//   - Options.CatalogFolder (not yet on Options)
//   - Model.catalogFolder (unexported field, not yet on Model)
//   - Model.CatalogFolder() (accessor, not yet on Model)
//   - preflight.Overrides.CatalogFolder (not yet on Overrides)
//   - report.Result.CatalogFolder (not yet on Result)
//
// They will not compile against the current code. That is the expected RED
// state — they specify the target behaviour for the implementation tasks.

import (
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
	"mosaic-agent-test/internal/report"
)

// stringPtr is a local helper for non-nil *string overrides.
func stringPtr(v string) *string { return &v }

// ---------------------------------------------------------------------------
// Options.CatalogFolder: initial value propagation
// ---------------------------------------------------------------------------

// TestNewModel_CatalogFolder_StartsAtOptionsValue verifies that a freshly
// constructed Model's catalog folder starts at the value supplied in Options,
// before the user makes any change — analogous to SelectedHarness() starting
// at Options.Harness.
func TestNewModel_CatalogFolder_StartsAtOptionsValue(t *testing.T) {
	const initialCatalog = "C:/Tools/AgentTest/catalog"
	o := newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = initialCatalog // new field — compile error until added

	m := NewModel(o)

	if got := m.CatalogFolder(); got != initialCatalog {
		t.Errorf("CatalogFolder() = %q, want %q (must start at Options.CatalogFolder before any user change)", got, initialCatalog)
	}
}

// TestNewModel_CatalogFolder_EmptyDefault_StartsEmpty verifies that when
// Options.CatalogFolder is the empty string, the model's catalog folder also
// starts empty. An empty default is valid: it means the deploy tool resolves
// its own catalogue.
func TestNewModel_CatalogFolder_EmptyDefault_StartsEmpty(t *testing.T) {
	o := newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = "" // empty default

	m := NewModel(o)

	if got := m.CatalogFolder(); got != "" {
		t.Errorf("CatalogFolder() = %q, want empty string (empty Options.CatalogFolder must propagate to model)", got)
	}
}

// ---------------------------------------------------------------------------
// Suite-select screen: catalog folder displayed (T4.3a, T4.3d)
// ---------------------------------------------------------------------------

// TestSuiteSelect_CatalogFolder_DisplayedOnScreen verifies that the suite-select
// screen shows the current catalog folder so a user can see it before starting
// a run. This doubles as the display affordance: the field is visible whether
// or not the user has edited it.
func TestSuiteSelect_CatalogFolder_DisplayedOnScreen(t *testing.T) {
	const catalog = "C:/Tools/AgentTest/catalog"
	o := newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = catalog

	m := NewModel(o)
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	if !strings.Contains(view, catalog) {
		t.Errorf("suite-select View() does not contain the catalog folder %q; the user must be able to see it before starting a run.\nView:\n%s", catalog, view)
	}
}

// TestSuiteSelect_CatalogFolder_ChangedValue_DisplayedOnScreen verifies that
// when the model's catalogFolder field is changed (e.g. through user editing),
// the updated value appears in the suite-select View.
func TestSuiteSelect_CatalogFolder_ChangedValue_DisplayedOnScreen(t *testing.T) {
	const initial = "C:/default/catalog"
	const edited = "C:/custom/catalog"
	o := newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = initial

	m := NewModel(o)
	m.catalogFolder = edited // directly set the unexported field (same-package test)

	view := safeView(t, m)
	if !strings.Contains(view, edited) {
		t.Errorf("suite-select View() does not contain the edited catalog folder %q.\nView:\n%s", edited, view)
	}
}

// ---------------------------------------------------------------------------
// preflight.Overrides.CatalogFolder: threading through startSelectedSuite (T4.3b)
// ---------------------------------------------------------------------------

// TestSuiteSelect_CatalogFolder_SameAsDefault_OverridesNil verifies that when
// the model's catalog folder matches the initial Options.CatalogFolder (the
// user made no change), the preflight call receives a nil Overrides.CatalogFolder.
// A nil means "use the process-wide default" — no per-run override is needed.
func TestSuiteSelect_CatalogFolder_SameAsDefault_OverridesNil(t *testing.T) {
	const catalog = "C:/Tools/AgentTest/catalog"
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.CatalogFolder = catalog
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	// catalogFolder starts at opts.CatalogFolder (unchanged): no override.
	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.CatalogFolder != nil {
		t.Errorf("preflight.Input.Overrides.CatalogFolder = %v, want nil (no override when catalog folder equals the default)", captured.Overrides.CatalogFolder)
	}
}

// TestSuiteSelect_CatalogFolder_ChangedValue_FlowsThroughOverrides verifies that
// when the model's catalogFolder differs from the initial Options.CatalogFolder
// (the user edited it), the preflight call receives a non-nil
// Overrides.CatalogFolder pointing to the edited value.
func TestSuiteSelect_CatalogFolder_ChangedValue_FlowsThroughOverrides(t *testing.T) {
	const defaultCatalog = "C:/default/catalog"
	const overrideCatalog = "C:/custom/catalog"
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.CatalogFolder = defaultCatalog
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	m.catalogFolder = overrideCatalog // user changed the catalog folder

	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.CatalogFolder == nil {
		t.Fatalf("preflight.Input.Overrides.CatalogFolder = nil; want a non-nil pointer to %q (override must be set when catalog folder differs from default)", overrideCatalog)
	}
	if *captured.Overrides.CatalogFolder != overrideCatalog {
		t.Errorf("preflight.Input.Overrides.CatalogFolder = %q, want %q", *captured.Overrides.CatalogFolder, overrideCatalog)
	}
}

// TestSuiteSelect_CatalogFolder_OverridePreservesOtherOverrides verifies that
// threading the catalog-folder override does not clobber SubjectModel,
// StubModel, or Repetitions — all overrides must be present simultaneously
// in the preflight input.
func TestSuiteSelect_CatalogFolder_OverridePreservesOtherOverrides(t *testing.T) {
	const defaultCatalog = "C:/default/catalog"
	const overrideCatalog = "C:/custom/catalog"
	const wantSubject = "claude-opus-4-5"
	wantReps := 3
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.CatalogFolder = defaultCatalog
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	m.catalogFolder = overrideCatalog
	m.selectedSubjectModel = wantSubject
	m.repetitions = intPtr(wantReps)

	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.CatalogFolder == nil || *captured.Overrides.CatalogFolder != overrideCatalog {
		t.Errorf("Overrides.CatalogFolder = %v, want *%q", captured.Overrides.CatalogFolder, overrideCatalog)
	}
	if captured.Overrides.SubjectModel != wantSubject {
		t.Errorf("Overrides.SubjectModel = %q, want %q (must not be clobbered when CatalogFolder override is set)", captured.Overrides.SubjectModel, wantSubject)
	}
	if captured.Overrides.Repetitions == nil || *captured.Overrides.Repetitions != wantReps {
		t.Errorf("Overrides.Repetitions = %v, want *%d (must not be clobbered when CatalogFolder override is set)", captured.Overrides.Repetitions, wantReps)
	}
}

// ---------------------------------------------------------------------------
// TUI detail screen: catalog folder shown post-run (T4.4)
// ---------------------------------------------------------------------------

// modelWithCatalogFolderResult builds a Model already on the detail screen
// for a completed suite whose Result carries a specific CatalogFolder. The
// result is constructed directly as a struct literal (not via Build) so
// this helper does not depend on Build's changing arity.
func modelWithCatalogFolderResult(t *testing.T, catalog string) Model {
	t.Helper()

	// Construct the result directly. CatalogFolder is a new field on Result;
	// this compile error confirms the RED state.
	result := report.Result{
		SchemaVersion: report.SchemaVersion,
		SuiteID:       "suite-under-test",
		StartedAt:     time.Now(),
		FinishedAt:    time.Now(),
		CatalogFolder: catalog, // new field — compile error until implementation adds it
		Tests: []report.TestReport{
			{
				TestID: "catalog-test",
				Aggregate: domain.AggregateResult{
					TestID:  "catalog-test",
					Verdict: domain.VerdictPass,
					Counted: 1,
					Passed:  1,
					PassRate: 1,
					TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
				},
				Runs: []report.RunReport{{
					Key:     domain.RunKey{TestID: "catalog-test", RunNumber: 1},
					Verdict: domain.VerdictPass,
					Cost:    domain.CostReport{Attribution: domain.AttributionAttributed},
				}},
			},
		},
		Counts:    map[domain.Verdict]int{domain.VerdictPass: 1},
		TotalCost: domain.CostReport{Attribution: domain.AttributionAttributed},
	}

	runner := newFakeSuiteRunner().withResult(result)
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = runSuiteToCompletion(t, m, runner)

	// Navigate into the (only) finished test's detail.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	return m
}

// TestDetail_CatalogFolder_ShownInDetailView verifies that the TUI detail
// screen includes the Result.CatalogFolder value, so a user reviewing a
// completed run's detail can see which catalog was deployed without inspecting
// a retained sandbox.
func TestDetail_CatalogFolder_ShownInDetailView(t *testing.T) {
	const catalog = "C:/Tools/AgentTest/catalog"

	m := modelWithCatalogFolderResult(t, catalog)

	if m.Screen() != ScreenDetail {
		t.Fatalf("Screen() after navigating to detail = %q, want %q (test setup failed to reach ScreenDetail)", m.Screen(), ScreenDetail)
	}

	view := safeView(t, m)
	if !strings.Contains(view, catalog) {
		t.Errorf("detail view does not show CatalogFolder %q; a user reviewing a run's detail must be able to see which catalog was used.\nView:\n%s", catalog, view)
	}
}

// TestDetail_CatalogFolder_EmptyValue_DoesNotPanic verifies that the detail
// screen renders without error or panic when CatalogFolder is empty (no
// catalog override was used). This guards against nil-dereference or empty-
// label rendering errors in the implementation.
func TestDetail_CatalogFolder_EmptyValue_DoesNotPanic(t *testing.T) {
	m := modelWithCatalogFolderResult(t, "")

	if m.Screen() != ScreenDetail {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenDetail)
	}

	// Just calling View must not panic; the exact content is implementation-
	// defined (omit the label, show "none", etc.).
	view := safeView(t, m)
	if view == "" {
		t.Error("detail View() returned empty string for a valid (empty-catalog) completed run; want non-empty output")
	}
}
