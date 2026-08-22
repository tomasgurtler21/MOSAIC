package tui

// catalog_override_liveness_test.go verifies T10.2: a changed catalog folder
// reaches run start as an override, and no override branch is left unreachable.
//
// The catalog-folder override branch in startSelectedSuite() fires when
// m.catalogFolder != m.opts.CatalogFolder. Before Stage 9's settings screen,
// the model's catalogFolder could only be set at construction (from
// Options.CatalogFolder), making the branch permanently dead. Stage 9 made
// the field editable via the settings screen; Stage 10 confirms both branches
// are live by exercising them through the full UI path.
//
// These tests pin AC10.4: "A changed catalog folder reaches run start; no
// unreachable override branch remains."

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// Branch 1: changed catalog folder → override fires (override branch live)
// ---------------------------------------------------------------------------

// TestCatalogFolder_OverrideBranch_IsLive_WhenEditedViaSettingsScreen verifies
// that when the user edits the catalog folder through the settings screen and
// the new value differs from the initial Options.CatalogFolder, startSelectedSuite
// fires the override branch and passes a non-nil CatalogFolder override to
// preflight. This proves the branch is not dead: a setting change drives a
// real code path.
func TestCatalogFolder_OverrideBranch_IsLive_WhenEditedViaSettingsScreen(t *testing.T) {
	const defaultCatalog = "/default/catalog"
	const newCatalog = "/override/catalog"
	var captured preflight.Input

	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.CatalogFolder = defaultCatalog
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)

	// Navigate to the settings screen.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() after Tab = %q, want %q (test setup: settings screen must be reachable from suite-select)", m.Screen(), ScreenSettings)
	}

	// Find the catalog entry and position the cursor on it.
	entries := m.SettingsEntries()
	catIdx := -1
	for i, e := range entries {
		if e.Kind == SettingCatalog {
			catIdx = i
			break
		}
	}
	if catIdx < 0 {
		t.Fatalf("SettingsEntries() contains no SettingCatalog entry; cannot navigate to it for editing")
	}
	for i := 0; i < catIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}

	// Open the inline editor and type the new catalog path.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	for _, ch := range newCatalog {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter)) // confirm the edit

	// Return to suite-select and start the run.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after Esc from settings = %q, want %q (Esc must return to suite-select)", m.Screen(), ScreenSuiteSelect)
	}
	m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
	_ = m
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	// The override branch must have fired.
	if captured.Overrides.CatalogFolder == nil {
		t.Fatalf("preflight.Input.Overrides.CatalogFolder = nil after editing via settings screen; the override branch must fire when the user changes the catalog folder (branch must be live, not dead)")
	}
	if *captured.Overrides.CatalogFolder != newCatalog {
		t.Errorf("preflight.Input.Overrides.CatalogFolder = %q, want %q (the edited value must reach preflight)", *captured.Overrides.CatalogFolder, newCatalog)
	}
}

// ---------------------------------------------------------------------------
// Branch 2: unchanged catalog folder → no override (no-override branch live)
// ---------------------------------------------------------------------------

// TestCatalogFolder_NoOverrideBranch_IsLive_WhenNotChanged verifies that when
// the user leaves the catalog folder unchanged (equal to Options.CatalogFolder),
// startSelectedSuite does NOT fire the override branch and preflight receives
// nil CatalogFolder override. Both branches being exercisable confirms that
// neither is dead code.
func TestCatalogFolder_NoOverrideBranch_IsLive_WhenNotChanged(t *testing.T) {
	const defaultCatalog = "/default/catalog"
	var captured preflight.Input

	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.CatalogFolder = defaultCatalog
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)
	// catalogFolder starts equal to Options.CatalogFolder; user makes no change.

	m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
	_ = m
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.CatalogFolder != nil {
		t.Errorf("preflight.Input.Overrides.CatalogFolder = %v; want nil when the user has not changed the catalog folder from the default (the no-override branch must also be live)", captured.Overrides.CatalogFolder)
	}
}

// ---------------------------------------------------------------------------
// Both branches confirmed live together
// ---------------------------------------------------------------------------

// TestCatalogFolder_BothBranches_ConfirmedLive_InContrast demonstrates that
// the override and no-override branches can both be reached, making it clear
// that neither is dead. Running both cases side by side in one test makes
// the contrast explicit and prevents either branch from being silently dropped
// without a test failure.
func TestCatalogFolder_BothBranches_ConfirmedLive_InContrast(t *testing.T) {
	const defaultCatalog = "/default/catalog"
	const overrideCatalog = "/override/catalog"

	// Branch A: unchanged → nil override (no-override branch is live).
	t.Run("unchanged catalog folder produces nil override", func(t *testing.T) {
		var captured preflight.Input
		runner := newFakeSuiteRunner()
		o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
		o.CatalogFolder = defaultCatalog
		o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
			captured = in
			return fixturePlan("suite-under-test"), authoring.Report{}
		}
		m := NewModel(o)

		m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
		_ = m
		if cmd != nil {
			_ = runCmd(t, cmd)
		}
		if captured.Overrides.CatalogFolder != nil {
			t.Errorf("Overrides.CatalogFolder = %v, want nil (no-override branch)", captured.Overrides.CatalogFolder)
		}
	})

	// Branch B: changed → non-nil override (override branch is live).
	t.Run("changed catalog folder produces non-nil override", func(t *testing.T) {
		var captured preflight.Input
		runner := newFakeSuiteRunner()
		o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
		o.CatalogFolder = defaultCatalog
		o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
			captured = in
			return fixturePlan("suite-under-test"), authoring.Report{}
		}
		m := NewModel(o)
		m.catalogFolder = overrideCatalog // simulate user changing catalog

		m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
		_ = m
		if cmd != nil {
			_ = runCmd(t, cmd)
		}
		if captured.Overrides.CatalogFolder == nil {
			t.Fatalf("Overrides.CatalogFolder = nil; want *%q (override branch must fire when catalog folder differs from default)", overrideCatalog)
		}
		if *captured.Overrides.CatalogFolder != overrideCatalog {
			t.Errorf("Overrides.CatalogFolder = %q, want %q", *captured.Overrides.CatalogFolder, overrideCatalog)
		}
	})
}

// ---------------------------------------------------------------------------
// Edge case: clearing the catalog folder to empty string
// ---------------------------------------------------------------------------

// TestCatalogFolder_ClearedToEmpty_TreatedAsOverride verifies that clearing
// the catalog folder (setting it to an empty string) is treated as an override
// when the initial default is non-empty. An empty string means "let the deploy
// tool resolve its own catalogue" — a distinct deliberate choice, not the same
// as leaving the default in place. The override branch must fire for this case.
func TestCatalogFolder_ClearedToEmpty_TreatedAsOverride(t *testing.T) {
	const defaultCatalog = "/default/catalog"
	var captured preflight.Input

	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.CatalogFolder = defaultCatalog
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)
	m.catalogFolder = "" // user cleared the catalog folder

	m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
	_ = m
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	// Empty string differs from the non-empty default: override branch must fire.
	if captured.Overrides.CatalogFolder == nil {
		t.Fatalf("preflight.Input.Overrides.CatalogFolder = nil; want a non-nil pointer to empty string when the user clears the catalog folder (empty string differs from the non-empty default %q and must be sent as an explicit override)", defaultCatalog)
	}
	if *captured.Overrides.CatalogFolder != "" {
		t.Errorf("preflight.Input.Overrides.CatalogFolder = %q, want empty string (the cleared value must reach preflight unchanged)", *captured.Overrides.CatalogFolder)
	}
}

// ---------------------------------------------------------------------------
// Settings screen: catalog folder display
// ---------------------------------------------------------------------------

// TestSettings_CatalogFolder_ViewShowsCurrentValueForOverrideLiveness verifies
// that the settings screen shows the catalog folder's current value in the View
// output. This is a prerequisite for the user being able to see and change the
// catalog folder — if the value is not visible, the editing affordance is blind.
func TestSettings_CatalogFolder_ViewShowsCurrentValueForOverrideLiveness(t *testing.T) {
	const catalog = "/current/catalog"
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = catalog
	m := NewModel(o)

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() after Tab = %q, want %q", m.Screen(), ScreenSettings)
	}

	view := safeView(t, m)
	if !strings.Contains(view, catalog) {
		t.Errorf("settings View() does not contain the current catalog folder %q; the user must be able to see the active value before changing it.\nView:\n%s", catalog, view)
	}
}
