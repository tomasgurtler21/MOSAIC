package tui

// run_config_test.go covers the Stage 9 TUI run-configuration surface.
//
// These tests specify four tasks:
//
//   T9.1 - The suite-select screen carries suite selection and no per-run
//           settings, and its remaining bindings (up/down navigation, enter
//           to start the run) still behave.
//
//   T9.2 - Run configuration is reachable from suite-select via a single
//           navigable key. Every setting displayed on the settings screen —
//           retention, repetitions, report path, catalog folder — can be
//           focused and changed through the same affordance, and the changed
//           values reach the run that starts from suite-select.
//
//   T9.3 - Every functional key binding on the affected screens appears in
//           the help bar, including the key that navigates to the settings
//           screen and the key that activates a focused setting.
//
//   T9.4 - All four settings are expressed through the same SettingsEntry
//           mechanism: each entry has a Kind, Label, Display and EditMode
//           field, and the interaction model is uniform across all of them.
//
// All tests are in package tui (not tui_test) so they can access Model's
// unexported fields and use the shared helpers from testhelpers_test.go.
//
// RED-phase dependencies (will not compile against current code):
//   - ScreenSettings (not yet declared in app.go)
//   - SettingsEntry, SettingKind, SettingEditMode (not yet in settings.go)
//   - Model.SettingsEntries() []SettingsEntry (not yet on Model)
//   - Tab key navigation from ScreenSuiteSelect to ScreenSettings (not wired)
//   - Suite-select rendering that omits per-run settings lines (not yet changed)
// This is the expected RED state. All tests should fail against the current
// implementation and pass once I9.1 through I9.4 are complete.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// T9.1 — Suite-select carries no per-run settings
// ---------------------------------------------------------------------------

// TestSuiteSelect_ShowsNoRetentionLine verifies that the suite-select screen
// does not render a retention setting line. Retention is a per-run setting
// and belongs exclusively on the settings screen after this stage.
func TestSuiteSelect_ShowsNoRetentionLine(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainOnFailure
	m := NewModel(o)
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	if strings.Contains(viewLower, "retain") {
		t.Errorf("suite-select View() contains retention text; per-run settings must be on the settings screen, not on suite-select.\nView:\n%s", view)
	}
}

// TestSuiteSelect_ShowsNoRepetitionsLine verifies that the suite-select screen
// does not render a repetitions setting line. Repetitions is a per-run setting
// and belongs exclusively on the settings screen.
func TestSuiteSelect_ShowsNoRepetitionsLine(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	m := NewModel(o)
	m.repetitions = intPtr(4)
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	if strings.Contains(viewLower, "repetition") || strings.Contains(viewLower, " reps") {
		t.Errorf("suite-select View() contains repetitions text; per-run settings must be on the settings screen, not on suite-select.\nView:\n%s", view)
	}
}

// TestSuiteSelect_ShowsNoReportPathLine verifies that the suite-select screen
// does not render a report-path setting line. Report path is a per-run setting
// and belongs exclusively on the settings screen.
func TestSuiteSelect_ShowsNoReportPathLine(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ReportPath = "/reports/output.json"
	m := NewModel(o)
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	// "report:" is the label prefix the old implementation rendered.
	// "report [editing]:" was the edit-mode variant.
	if strings.Contains(viewLower, "report:") || strings.Contains(viewLower, "report [editing]") {
		t.Errorf("suite-select View() contains a report-path line; per-run settings must be on the settings screen, not on suite-select.\nView:\n%s", view)
	}
}

// TestSuiteSelect_ShowsNoCatalogLine verifies that the suite-select screen does
// not render a catalog-folder setting line. Catalog folder is a per-run setting
// and belongs exclusively on the settings screen.
func TestSuiteSelect_ShowsNoCatalogLine(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = "/catalog/main"
	m := NewModel(o)
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	// "catalog:" was the label prefix the old implementation rendered.
	if strings.Contains(viewLower, "catalog:") {
		t.Errorf("suite-select View() contains a catalog-folder line; per-run settings must be on the settings screen, not on suite-select.\nView:\n%s", view)
	}
}

// TestSuiteSelect_SuiteNavigation_StillWorks verifies that removing per-run
// settings lines from suite-select does not break cursor movement: Down/Up
// still move between suite entries.
func TestSuiteSelect_SuiteNavigation_StillWorks(t *testing.T) {
	suites := []string{"suite-a.yaml", "suite-b.yaml", "suite-c.yaml"}
	m := NewModel(newFixtureOptions(suites, newFakeSuiteRunner()))
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	if m.suiteCursor != 0 {
		t.Fatalf("suiteCursor = %d, want 0 at construction", m.suiteCursor)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	if m.suiteCursor != 1 {
		t.Errorf("suiteCursor after one Down = %d, want 1", m.suiteCursor)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	if m.suiteCursor != 2 {
		t.Errorf("suiteCursor after two Downs = %d, want 2", m.suiteCursor)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyUp))
	if m.suiteCursor != 1 {
		t.Errorf("suiteCursor after Up = %d, want 1", m.suiteCursor)
	}
}

// TestSuiteSelect_Enter_BeginsSettingsFlow verifies that pressing Enter on the
// suite-select screen enters the sequential settings flow, landing on
// ScreenRetention as the first step before the suite starts. The full settings
// flow must be completed before the run begins.
func TestSuiteSelect_Enter_BeginsSettingsFlow(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	if m.Screen() != ScreenRetention {
		t.Errorf("Screen() after Enter on suite-select = %q, want %q (Enter must enter the settings flow starting at ScreenRetention)", m.Screen(), ScreenRetention)
	}
}

// TestSuiteSelect_Space_DoesNotChangeRetention verifies that Space no longer
// cycles retention on the suite-select screen. The retention toggle has moved
// to the settings screen.
func TestSuiteSelect_Space_DoesNotChangeRetention(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainNever
	m := NewModel(o)
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Press Space: on the old screen this cycled retention. Now it must not.
	m, _ = safeUpdate(t, m, keyMsg(" "))
	if got := m.Retention(); got != domain.RetainNever {
		t.Errorf("Retention() after Space on suite-select = %q, want %q (Space must not toggle retention from suite-select after this stage; toggle is on settings screen)", got, domain.RetainNever)
	}
}

// ---------------------------------------------------------------------------
// T9.2 — Settings screen reachable; every setting focusable and changeable
// ---------------------------------------------------------------------------

// Note: The monolithic ScreenSettings was replaced by sequential per-setting
// screens (Stage 3). Tab no longer navigates to a settings screen; Enter on
// ScreenSuiteSelect enters the sequential flow (ScreenRetention → …).
// The navigation and back-navigation tests for the old ScreenSettings are
// superseded by settings_navigation_test.go which covers the sequential flow.

// TestSettings_AllScreensShowTheirSetting verifies that each sequential
// settings screen renders its respective setting — retention, repetitions,
// report path, and catalog folder — so the user can see the active value on
// the screen that lets them change it.
func TestSettings_AllScreensShowTheirSetting(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainNever
	o.ReportPath = "/reports/out.json"
	o.CatalogFolder = "/catalog/main"
	m := NewModel(o)
	m.repetitions = intPtr(3)

	// ScreenRetention must show a retention-related term.
	m = advanceToSettingsFlow(t, m)
	if m.Screen() != ScreenRetention {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}
	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	if !strings.Contains(viewLower, "retention") && !strings.Contains(viewLower, "retain") && !strings.Contains(viewLower, "never") && !strings.Contains(viewLower, "always") {
		t.Errorf("ScreenRetention View() does not mention retention.\nView:\n%s", view)
	}

	// ScreenRepetitions must show a repetitions-related term.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}
	view = safeView(t, m)
	if !strings.Contains(strings.ToLower(view), "repetition") && !strings.Contains(strings.ToLower(view), "reps") && !strings.Contains(strings.ToLower(view), "repeat") {
		t.Errorf("ScreenRepetitions View() does not mention repetitions.\nView:\n%s", view)
	}

	// ScreenReportPath must show the report path value.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenReportPath {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenReportPath)
	}
	view = safeView(t, m)
	if !strings.Contains(view, "/reports/out.json") {
		t.Errorf("ScreenReportPath View() does not show the report path.\nView:\n%s", view)
	}

	// ScreenCatalogFolder must show the catalog folder value.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenCatalogFolder {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenCatalogFolder)
	}
	view = safeView(t, m)
	if !strings.Contains(view, "/catalog/main") {
		t.Errorf("ScreenCatalogFolder View() does not show the catalog folder.\nView:\n%s", view)
	}
}

// Note: The ScreenSettings cursor navigation tests (Down/Up among entries,
// clamping at boundaries) are no longer applicable: the monolithic settings
// screen with a shared cursor was replaced by sequential per-setting screens
// (ScreenRetention → ScreenRepetitions → …). Navigation between settings
// is now by screen transition rather than cursor movement.

// TestSettings_RetentionCycle_ViaSpaceOnRetentionScreen verifies that pressing
// Space on ScreenRetention cycles the retention value, and the changed value
// reaches the run that is subsequently started.
func TestSettings_RetentionCycle_ViaSpaceOnRetentionScreen(t *testing.T) {
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Retention = domain.RetainNever
	m := NewModel(o)

	// Navigate to ScreenRetention.
	m = advanceToSettingsFlow(t, m)
	if m.Screen() != ScreenRetention {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}

	// Space cycles: RetainNever → RetainOnFailure → RetainAlways.
	m, _ = safeUpdate(t, m, keyMsg(" "))
	m, _ = safeUpdate(t, m, keyMsg(" "))

	// Complete the settings flow to start the run with RetainAlways.
	m, cmd := advanceThroughSettingsToProgress(t, m)
	if cmd == nil {
		t.Fatalf("suite start produced no tea.Cmd")
	}
	msg := runCmd(t, cmd)
	if msg != nil {
		safeUpdate(t, m, msg)
	}

	if !runner.called {
		t.Fatalf("SuiteRunner.Run was never called")
	}
	if got := runner.gotRetention; got != domain.RetainAlways {
		t.Errorf("SuiteRunner.Run received retention %q, want %q", got, domain.RetainAlways)
	}
}

// TestSettings_ReportPath_ViaActionKey verifies that on ScreenReportPath, typing
// a new path and pressing Enter commits the new value to the Model's report path.
func TestSettings_ReportPath_ViaActionKey(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ReportPath = "/original/path.json"
	m := NewModel(o)

	// Navigate to ScreenReportPath: Enter×3 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenReportPath {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenReportPath)
	}

	// Type the new path character by character.
	const newPath = "/new/path.json"
	for _, ch := range newPath {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}

	// Confirm the edit.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenCatalogFolder {
		t.Fatalf("Screen() after Enter on ScreenReportPath = %q, want %q", m.Screen(), ScreenCatalogFolder)
	}

	if got := m.ReportPath(); got != newPath {
		t.Errorf("ReportPath() after editing via ScreenReportPath = %q, want %q", got, newPath)
	}
}

// TestSettings_ReportPath_EscCancelsEdit verifies that pressing Esc on
// ScreenReportPath while a draft is present discards the draft, leaves the
// original value in place, and navigates back to ScreenRepetitions.
func TestSettings_ReportPath_EscCancelsEdit(t *testing.T) {
	const original = "/original/path.json"
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ReportPath = original
	m := NewModel(o)

	// Navigate to ScreenReportPath.
	m = advanceToSettingsFlow(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // → ScreenRepetitions
	m, _ = safeUpdate(t, m, keyMsg("\r")) // → ScreenReportPath
	if m.Screen() != ScreenReportPath {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenReportPath)
	}

	// Type a draft then cancel with Esc.
	for _, ch := range "/typed/draft" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))

	if m.Screen() != ScreenRepetitions {
		t.Errorf("Screen() after Esc on ScreenReportPath = %q, want %q", m.Screen(), ScreenRepetitions)
	}
	if got := m.ReportPath(); got != original {
		t.Errorf("ReportPath() after Esc = %q, want %q (Esc must discard the draft)", got, original)
	}
}

// TestSettings_CatalogFolder_ViaActionKey verifies that on ScreenCatalogFolder,
// typing a new path and pressing Enter commits the new value.
func TestSettings_CatalogFolder_ViaActionKey(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = "/original/catalog"
	m := NewModel(o)

	// Navigate to ScreenCatalogFolder: Enter×4 from SuiteSelect.
	m = advanceToSettingsFlow(t, m)
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during navigation to ScreenCatalogFolder", m.Screen(), want)
		}
	}

	const newCatalog = "/new/catalog"
	for _, ch := range newCatalog {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenMaxConcurrentRuns {
		t.Fatalf("Screen() after Enter on ScreenCatalogFolder = %q, want %q", m.Screen(), ScreenMaxConcurrentRuns)
	}

	if got := m.CatalogFolder(); got != newCatalog {
		t.Errorf("CatalogFolder() after editing via ScreenCatalogFolder = %q, want %q", got, newCatalog)
	}
}

// TestSettings_CatalogFolder_EscCancelsEdit verifies that pressing Esc on
// ScreenCatalogFolder discards the draft and navigates back to ScreenReportPath.
func TestSettings_CatalogFolder_EscCancelsEdit(t *testing.T) {
	const original = "/original/catalog"
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = original
	m := NewModel(o)

	// Navigate to ScreenCatalogFolder.
	m = advanceToSettingsFlow(t, m)
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during navigation", m.Screen(), want)
		}
	}

	// Type a draft then cancel.
	for _, ch := range "/typed/draft" {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))

	if m.Screen() != ScreenReportPath {
		t.Errorf("Screen() after Esc on ScreenCatalogFolder = %q, want %q", m.Screen(), ScreenReportPath)
	}
	if got := m.CatalogFolder(); got != original {
		t.Errorf("CatalogFolder() after Esc = %q, want %q (Esc must discard the draft)", got, original)
	}
}

// TestSettings_ChangedCatalogFolder_ReachesPreflightOverrides verifies that a
// catalog folder changed before starting a run reaches
// preflight.Overrides.CatalogFolder.
func TestSettings_ChangedCatalogFolder_ReachesPreflightOverrides(t *testing.T) {
	const defaultCatalog = "/default/catalog"
	const newCatalog = "/new/catalog"
	var captured preflight.Input

	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.CatalogFolder = defaultCatalog
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)

	// Pre-set the catalog folder via direct field assignment (simulating prior edits).
	m.catalogFolder = newCatalog

	// Start the run via the settings flow.
	m, cmd := startSuiteFromSuiteSelect(t, m)
	_ = m
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.CatalogFolder == nil {
		t.Fatalf("preflight.Input.Overrides.CatalogFolder = nil; want *%q", newCatalog)
	}
	if *captured.Overrides.CatalogFolder != newCatalog {
		t.Errorf("preflight.Input.Overrides.CatalogFolder = %q, want %q", *captured.Overrides.CatalogFolder, newCatalog)
	}
}

// TestSettings_Repetitions_ViaActionKey verifies that on ScreenRepetitions,
// typing a digit and pressing Enter commits the new repetitions override.
func TestSettings_Repetitions_ViaActionKey(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	m := NewModel(o)

	// Navigate to ScreenRepetitions: Enter×2 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}

	// Type the new repetitions count and confirm.
	m, _ = safeUpdate(t, m, keyMsg("5"))
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenReportPath {
		t.Fatalf("Screen() after Enter on ScreenRepetitions = %q, want %q", m.Screen(), ScreenReportPath)
	}

	got := m.Repetitions()
	if got == nil {
		t.Fatalf("Repetitions() = nil after editing via ScreenRepetitions; want *5")
	}
	if *got != 5 {
		t.Errorf("Repetitions() = %d after editing via ScreenRepetitions, want 5", *got)
	}
}

// TestSettings_ChangedRepetitions_ReachesPreflightOverrides verifies that a
// repetitions count changed before starting a run reaches
// preflight.Overrides.Repetitions.
func TestSettings_ChangedRepetitions_ReachesPreflightOverrides(t *testing.T) {
	const wantReps = 3
	var captured preflight.Input

	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)

	// Navigate to ScreenRepetitions and type "3".
	m = advanceToSettingsFlow(t, m)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // → ScreenRepetitions
	m, _ = safeUpdate(t, m, keyMsg("3"))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // confirm → ScreenReportPath

	// Complete the remaining screens: ReportPath → CatalogFolder → MaxConcurrentRuns → Progress.
	for _, want := range []Screen{ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during final settings navigation", m.Screen(), want)
		}
	}
	var cmd tea.Cmd
	m, cmd = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() = %q, want %q after final Enter", m.Screen(), ScreenProgress)
	}
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.Repetitions == nil {
		t.Fatalf("preflight.Input.Overrides.Repetitions = nil; want *%d", wantReps)
	}
	if *captured.Overrides.Repetitions != wantReps {
		t.Errorf("preflight.Input.Overrides.Repetitions = %d, want %d", *captured.Overrides.Repetitions, wantReps)
	}
}

// ---------------------------------------------------------------------------
// T9.3 — Every functional binding appears in the help bar
// ---------------------------------------------------------------------------

// Note: TestSuiteSelect_HelpBar_IncludesSettingsNavKey (Tab key) is removed.
// The monolithic ScreenSettings accessed by Tab was replaced by sequential
// per-setting screens. Tab is no longer a navigation key from suite-select.

// TestSuiteSelect_HelpBar_DoesNotIncludeUndiscoverableEditKey verifies that the
// 'e' key (old undiscoverable inline-edit affordance) no longer appears as a
// binding in the suite-select help bar.
// 'e' key (the old undiscoverable inline-edit affordance) no longer appears as a
// binding in the suite-select help bar, since it has been removed from this screen.
func TestSuiteSelect_HelpBar_DoesNotIncludeUndiscoverableEditKey(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToRunFlow(t, m) // navigate through mode-select to ScreenSuiteSelect
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	// The old binding "e: edit report path" must no longer appear in the help bar.
	// We check for "e: " as a pattern that would indicate a bare 'e' help key.
	if strings.Contains(viewLower, "e: edit") || strings.Contains(viewLower, "e:edit") {
		t.Errorf("suite-select help bar still shows the old undiscoverable 'e' edit key; it must be removed from this screen.\nView:\n%s", view)
	}
}

// Note: TestSettings_HelpBar_IncludesActionKey, TestSettings_HelpBar_IncludesNavigationKeys,
// and TestSettings_HelpBar_IncludesBackKey are removed. They tested the help bar of
// the monolithic ScreenSettings which was replaced by sequential per-setting screens.
// Each sequential screen's help bar is verified by its own screen-specific test file.

// ---------------------------------------------------------------------------
// T9.4 — All four settings go through one uniform SettingsEntry mechanism
// ---------------------------------------------------------------------------

// TestSettings_AllFourEntries_PresentInSettingsEntries verifies that
// SettingsEntries() returns all four expected SettingKind values, confirming
// that each setting is expressed as a SettingsEntry rather than as a
// per-setting special case in the rendering or key-handling code.
func TestSettings_AllFourEntries_PresentInSettingsEntries(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	// Entries are reachable from any screen — we do not need to navigate to
	// ScreenSettings to call SettingsEntries().
	entries := m.SettingsEntries()

	want := map[SettingKind]bool{
		SettingRetention:   false,
		SettingRepetitions: false,
		SettingReportPath:  false,
		SettingCatalog:     false,
	}
	for _, e := range entries {
		if _, ok := want[e.Kind]; ok {
			want[e.Kind] = true
		}
	}
	for kind, found := range want {
		if !found {
			t.Errorf("SettingsEntries() does not include a %q entry; all four settings must be expressed as SettingsEntry instances", kind)
		}
	}
}

// TestSettings_AllEntries_HaveNonEmptyLabel verifies that every SettingsEntry
// has a non-empty Label field — a structural invariant of the abstraction. An
// entry without a label cannot be rendered in the settings list.
func TestSettings_AllEntries_HaveNonEmptyLabel(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	for _, e := range m.SettingsEntries() {
		if e.Label == "" {
			t.Errorf("SettingsEntry{Kind: %q}.Label is empty; every entry must have a non-empty display label", e.Kind)
		}
	}
}

// TestSettings_AllEntries_HaveKnownEditMode verifies that every SettingsEntry
// carries a recognized SettingEditMode — either EditCycle, EditInline, or
// EditNumeric. An entry with an unrecognized mode cannot be driven by the
// uniform interaction model.
func TestSettings_AllEntries_HaveKnownEditMode(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	known := map[SettingEditMode]bool{
		EditCycle:   true,
		EditInline:  true,
		EditNumeric: true,
	}
	for _, e := range m.SettingsEntries() {
		if !known[e.EditMode] {
			t.Errorf("SettingsEntry{Kind: %q}.EditMode = %v is not a recognized SettingEditMode; every entry must carry EditCycle, EditInline, or EditNumeric", e.Kind, e.EditMode)
		}
	}
}

// TestSettings_RetentionEntry_UsesEditCycle verifies that the retention entry
// has EditCycle mode — it cycles through a fixed set of values rather than
// opening a text editor.
func TestSettings_RetentionEntry_UsesEditCycle(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingRetention {
			if e.EditMode != EditCycle {
				t.Errorf("SettingsEntry{Kind: %q}.EditMode = %v, want EditCycle (retention cycles through Never/OnFailure/Always)", e.Kind, e.EditMode)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() does not include SettingRetention")
}

// TestSettings_RepetitionsEntry_UsesEditNumeric verifies that the repetitions
// entry has EditNumeric mode — it opens an inline numeric editor, not a cycle.
func TestSettings_RepetitionsEntry_UsesEditNumeric(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingRepetitions {
			if e.EditMode != EditNumeric {
				t.Errorf("SettingsEntry{Kind: %q}.EditMode = %v, want EditNumeric", e.Kind, e.EditMode)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() does not include SettingRepetitions")
}

// TestSettings_ReportPathEntry_UsesEditInline verifies that the report-path
// entry has EditInline mode — it opens a text editor, not a value cycle.
func TestSettings_ReportPathEntry_UsesEditInline(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingReportPath {
			if e.EditMode != EditInline {
				t.Errorf("SettingsEntry{Kind: %q}.EditMode = %v, want EditInline", e.Kind, e.EditMode)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() does not include SettingReportPath")
}

// TestSettings_CatalogEntry_UsesEditInline verifies that the catalog-folder
// entry has EditInline mode — it opens a text editor, not a value cycle.
func TestSettings_CatalogEntry_UsesEditInline(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingCatalog {
			if e.EditMode != EditInline {
				t.Errorf("SettingsEntry{Kind: %q}.EditMode = %v, want EditInline", e.Kind, e.EditMode)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() does not include SettingCatalog")
}

// TestSettings_DisplayReflectsCurrentValues verifies that each SettingsEntry's
// Display field reflects the model's current value for that setting, so the
// rendered list shows accurate state before a user begins editing.
func TestSettings_DisplayReflectsCurrentValues(t *testing.T) {
	const wantReport = "/current/report.json"
	const wantCatalog = "/current/catalog"
	wantReps := 7
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainAlways
	o.ReportPath = wantReport
	o.CatalogFolder = wantCatalog
	m := NewModel(o)
	m.repetitions = intPtr(wantReps)

	entries := m.SettingsEntries()
	for _, e := range entries {
		switch e.Kind {
		case SettingRetention:
			if !strings.Contains(e.Display, string(domain.RetainAlways)) {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want it to contain %q (current retention value)", e.Kind, e.Display, domain.RetainAlways)
			}
		case SettingRepetitions:
			if !strings.Contains(e.Display, "7") {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want it to contain %q (current repetitions override)", e.Kind, e.Display, "7")
			}
		case SettingReportPath:
			if !strings.Contains(e.Display, wantReport) {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want it to contain %q (current report path)", e.Kind, e.Display, wantReport)
			}
		case SettingCatalog:
			if !strings.Contains(e.Display, wantCatalog) {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want it to contain %q (current catalog folder)", e.Kind, e.Display, wantCatalog)
			}
		}
	}
}

// Note: TestSettings_FocusedEntryHighlighted is removed. It tested cursor
// highlighting on the monolithic ScreenSettings which was replaced by sequential
// per-setting screens (ScreenRetention → ScreenRepetitions → …). Each sequential
// screen is the sole focused element, so cursor differentiation within a single
// screen is not applicable.
