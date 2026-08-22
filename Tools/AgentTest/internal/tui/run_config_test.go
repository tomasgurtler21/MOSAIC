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

// TestSuiteSelect_Enter_StillStartsRun verifies that pressing Enter on the
// suite-select screen still starts the chosen suite, regardless of the layout
// change that removed per-run settings from this screen.
func TestSuiteSelect_Enter_StillStartsRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
	if m.Screen() != ScreenProgress {
		t.Errorf("Screen() after Enter on suite-select = %q, want %q (Enter must still start the run)", m.Screen(), ScreenProgress)
	}
	if cmd == nil {
		t.Errorf("Enter on suite-select produced no tea.Cmd; the run must have started")
	}
}

// TestSuiteSelect_Space_DoesNotChangeRetention verifies that Space no longer
// cycles retention on the suite-select screen. The retention toggle has moved
// to the settings screen.
func TestSuiteSelect_Space_DoesNotChangeRetention(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainNever
	m := NewModel(o)

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

// TestSuiteSelect_Tab_NavigatesToSettingsScreen verifies that pressing Tab on
// the suite-select screen navigates to the run-configuration settings screen
// (ScreenSettings). Tab is the affordance that makes the settings screen
// discoverable: the help bar must list it so a user can find it.
func TestSuiteSelect_Tab_NavigatesToSettingsScreen(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Errorf("Screen() after Tab from suite-select = %q, want %q", m.Screen(), ScreenSettings)
	}
}

// TestSettings_Tab_ReturnsToSuiteSelect verifies that pressing Tab from the
// settings screen returns to suite-select, completing the round-trip navigation.
// Esc should also return — this test covers Tab as the mirrored navigation key.
func TestSettings_Tab_ReturnsToSuiteSelect(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	// Navigate to settings.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() after first Tab = %q, want %q (prerequisite failed)", m.Screen(), ScreenSettings)
	}

	// Navigate back.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after second Tab from settings = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
}

// TestSettings_Esc_ReturnsToSuiteSelect verifies that pressing Esc from the
// settings screen returns to suite-select.
func TestSettings_Esc_ReturnsToSuiteSelect(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() after Tab = %q, want %q (prerequisite failed)", m.Screen(), ScreenSettings)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after Esc from settings = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
}

// TestSettings_ShowsAllFourSettings verifies that the settings screen View
// contains visible text for all four per-run settings: retention, repetitions,
// report path, and catalog folder.
func TestSettings_ShowsAllFourSettings(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainNever
	o.ReportPath = "/reports/out.json"
	o.CatalogFolder = "/catalog/main"
	m := NewModel(o)
	m.repetitions = intPtr(3)

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)

	if !strings.Contains(viewLower, "retain") {
		t.Errorf("settings View() does not mention retention; all four settings must be visible on the settings screen.\nView:\n%s", view)
	}
	if !strings.Contains(viewLower, "repetition") && !strings.Contains(viewLower, " reps") {
		t.Errorf("settings View() does not mention repetitions; all four settings must be visible on the settings screen.\nView:\n%s", view)
	}
	if !strings.Contains(view, "/reports/out.json") {
		t.Errorf("settings View() does not show the report path %q; all four settings must be visible.\nView:\n%s", "/reports/out.json", view)
	}
	if !strings.Contains(view, "/catalog/main") {
		t.Errorf("settings View() does not show the catalog folder %q; all four settings must be visible.\nView:\n%s", "/catalog/main", view)
	}
}

// TestSettings_CursorNavigation_MovesAmongEntries verifies that Down and Up
// move the cursor among the settings entries, and that the cursor starts at
// the first entry.
func TestSettings_CursorNavigation_MovesAmongEntries(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	entries := m.SettingsEntries()
	if len(entries) < 2 {
		t.Fatalf("SettingsEntries() returned %d entries; need at least 2 to test cursor movement", len(entries))
	}

	// Cursor should start at entry 0.
	if m.settingsCursor != 0 {
		t.Errorf("settingsCursor = %d at entry to settings screen, want 0", m.settingsCursor)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	if m.settingsCursor != 1 {
		t.Errorf("settingsCursor after Down = %d, want 1", m.settingsCursor)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyUp))
	if m.settingsCursor != 0 {
		t.Errorf("settingsCursor after Up = %d, want 0", m.settingsCursor)
	}
}

// TestSettings_CursorDown_Clamped_AtLastEntry verifies that Down does not move
// the cursor past the last entry: repeated Downs must stop at len(entries)-1.
func TestSettings_CursorDown_Clamped_AtLastEntry(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	entries := m.SettingsEntries()
	last := len(entries) - 1

	// Press Down enough times to reach (and attempt to pass) the last entry.
	for i := 0; i <= last+2; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}
	if m.settingsCursor != last {
		t.Errorf("settingsCursor after excess Downs = %d, want %d (clamped at last entry)", m.settingsCursor, last)
	}
}

// TestSettings_CursorUp_Clamped_AtFirstEntry verifies that Up does not move
// the cursor before the first entry: pressing Up when settingsCursor is already
// at 0 must leave it at 0. This is the symmetric boundary condition to
// TestSettings_CursorDown_Clamped_AtLastEntry.
func TestSettings_CursorUp_Clamped_AtFirstEntry(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	// Cursor starts at 0 on entry to the settings screen.
	if m.settingsCursor != 0 {
		t.Fatalf("settingsCursor = %d at entry to settings screen, want 0 (prerequisite failed)", m.settingsCursor)
	}

	// Press Up: cursor must stay at 0, not wrap or underflow.
	m, _ = safeUpdate(t, m, keyType(tea.KeyUp))
	if m.settingsCursor != 0 {
		t.Errorf("settingsCursor after Up from position 0 = %d, want 0 (cursor must be clamped at the first entry)", m.settingsCursor)
	}
}

// TestSettings_RetentionCycle_ViaActionKey verifies that pressing the action
// key (Enter) while the retention entry is focused cycles the retention value,
// and the changed value reaches the run that is subsequently started.
func TestSettings_RetentionCycle_ViaActionKey(t *testing.T) {
	var captured domain.RetentionPolicy
	runner := &fakeSuiteRunner{
		release:    make(chan struct{}),
		cancelDone: make(chan struct{}),
	}
	// Use a custom preflight that always succeeds so we can observe the run start.
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Retention = domain.RetainNever
	// Wrap the runner to capture gotRetention without races.
	m := NewModel(o)

	// Navigate to settings.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	// Position cursor on the retention entry (it is the first entry by design).
	entries := m.SettingsEntries()
	retentionIdx := -1
	for i, e := range entries {
		if e.Kind == SettingRetention {
			retentionIdx = i
			break
		}
	}
	if retentionIdx < 0 {
		t.Fatalf("SettingsEntries() does not include a SettingRetention entry; all four settings must be present")
	}
	for i := 0; i < retentionIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}

	// Press Enter to cycle: RetainNever -> RetainOnFailure.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	if got := m.Retention(); got != domain.RetainOnFailure {
		t.Errorf("Retention() after one Enter on retention entry = %q, want %q (Enter must cycle through retention values)", got, domain.RetainOnFailure)
	}

	// Cycle again: RetainOnFailure -> RetainAlways.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	if got := m.Retention(); got != domain.RetainAlways {
		t.Errorf("Retention() after two Enters on retention entry = %q, want %q", got, domain.RetainAlways)
	}

	// Return to suite-select and start the run; the cycled value must reach the runner.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after Esc = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() after Enter on suite-select = %q, want %q", m.Screen(), ScreenProgress)
	}
	if cmd == nil {
		t.Fatalf("Enter on suite-select produced no tea.Cmd")
	}
	msg := runCmd(t, cmd)
	if msg != nil {
		safeUpdate(t, m, msg)
	}

	if !runner.called {
		t.Fatalf("SuiteRunner.Run was never called")
	}
	captured = runner.gotRetention
	if captured != domain.RetainAlways {
		t.Errorf("SuiteRunner.Run received retention %q, want %q (the value cycled on the settings screen must reach the run)", captured, domain.RetainAlways)
	}
}

// TestSettings_ReportPath_ViaActionKey verifies that pressing the action key
// (Enter) on the report-path entry opens an inline editor, typing into it
// accumulates the text, and confirming with Enter commits the new value to the
// Model's report path.
func TestSettings_ReportPath_ViaActionKey(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ReportPath = "/original/path.json"
	m := NewModel(o)

	// Navigate to settings.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	// Find the report-path entry and position cursor on it.
	entries := m.SettingsEntries()
	reportIdx := -1
	for i, e := range entries {
		if e.Kind == SettingReportPath {
			reportIdx = i
			break
		}
	}
	if reportIdx < 0 {
		t.Fatalf("SettingsEntries() does not include a SettingReportPath entry")
	}
	for i := 0; i < reportIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}

	// Press Enter to open the inline editor for the report path.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))

	// Type the new path character by character.
	const newPath = "/new/path.json"
	for _, ch := range newPath {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}

	// Confirm the edit.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))

	if got := m.ReportPath(); got != newPath {
		t.Errorf("ReportPath() after editing via settings screen = %q, want %q", got, newPath)
	}
}

// TestSettings_ReportPath_EscCancelsEdit verifies that pressing Esc while the
// inline editor for report path is open discards the draft and leaves the
// original value in place — the same cancel semantics the old inline editor had.
func TestSettings_ReportPath_EscCancelsEdit(t *testing.T) {
	const original = "/original/path.json"
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ReportPath = original
	m := NewModel(o)

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	entries := m.SettingsEntries()
	reportIdx := -1
	for i, e := range entries {
		if e.Kind == SettingReportPath {
			reportIdx = i
			break
		}
	}
	if reportIdx < 0 {
		t.Fatalf("SettingsEntries() does not include a SettingReportPath entry")
	}
	for i := 0; i < reportIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter)) // open inline editor
	m, _ = safeUpdate(t, m, keyMsg("/typed/draft"))
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc)) // cancel

	if got := m.ReportPath(); got != original {
		t.Errorf("ReportPath() after Esc during edit = %q, want %q (Esc must restore the original value)", got, original)
	}
}

// TestSettings_CatalogFolder_ViaActionKey verifies that the catalog-folder entry
// on the settings screen is editable via the same inline-editor affordance as
// report path: Enter opens the editor, typing accumulates, Enter confirms.
func TestSettings_CatalogFolder_ViaActionKey(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = "/original/catalog"
	m := NewModel(o)

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	entries := m.SettingsEntries()
	catIdx := -1
	for i, e := range entries {
		if e.Kind == SettingCatalog {
			catIdx = i
			break
		}
	}
	if catIdx < 0 {
		t.Fatalf("SettingsEntries() does not include a SettingCatalog entry")
	}
	for i := 0; i < catIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}

	const newCatalog = "/new/catalog"
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	for _, ch := range newCatalog {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))

	if got := m.CatalogFolder(); got != newCatalog {
		t.Errorf("CatalogFolder() after editing via settings screen = %q, want %q", got, newCatalog)
	}
}

// TestSettings_CatalogFolder_EscCancelsEdit verifies that pressing Esc while
// the inline editor for catalog folder is open discards the draft and leaves
// the original value in place — the same cancel semantics as the report-path
// inline editor.
func TestSettings_CatalogFolder_EscCancelsEdit(t *testing.T) {
	const original = "/original/catalog"
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.CatalogFolder = original
	m := NewModel(o)

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	entries := m.SettingsEntries()
	catIdx := -1
	for i, e := range entries {
		if e.Kind == SettingCatalog {
			catIdx = i
			break
		}
	}
	if catIdx < 0 {
		t.Fatalf("SettingsEntries() does not include a SettingCatalog entry")
	}
	for i := 0; i < catIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter)) // open inline editor
	m, _ = safeUpdate(t, m, keyMsg("/typed/draft"))
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc)) // cancel

	if got := m.CatalogFolder(); got != original {
		t.Errorf("CatalogFolder() after Esc during edit = %q, want %q (Esc must restore the original value)", got, original)
	}
}

// TestSettings_ChangedCatalogFolder_ReachesPreflightOverrides verifies that a
// catalog folder edited via the settings screen reaches
// preflight.Overrides.CatalogFolder when the run starts — the same contract
// the old direct-field tests verified, but now exercised through the real
// key-driven path.
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

	// Navigate to settings and edit the catalog folder.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}
	entries := m.SettingsEntries()
	catIdx := -1
	for i, e := range entries {
		if e.Kind == SettingCatalog {
			catIdx = i
			break
		}
	}
	if catIdx < 0 {
		t.Fatalf("SettingsEntries() does not include SettingCatalog")
	}
	for i := 0; i < catIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	for _, ch := range newCatalog {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))

	// Return to suite-select and start the run.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after Esc = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.CatalogFolder == nil {
		t.Fatalf("preflight.Input.Overrides.CatalogFolder = nil; want *%q (edited value must reach the run)", newCatalog)
	}
	if *captured.Overrides.CatalogFolder != newCatalog {
		t.Errorf("preflight.Input.Overrides.CatalogFolder = %q, want %q", *captured.Overrides.CatalogFolder, newCatalog)
	}
}

// TestSettings_Repetitions_ViaActionKey verifies that pressing the action key
// (Enter) on the repetitions entry opens an inline numeric editor, typing a
// digit accumulates the value, and confirming with Enter commits the new
// repetitions override to the Model.
func TestSettings_Repetitions_ViaActionKey(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	m := NewModel(o)

	// Navigate to settings.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	// Find the repetitions entry and position cursor on it.
	entries := m.SettingsEntries()
	repIdx := -1
	for i, e := range entries {
		if e.Kind == SettingRepetitions {
			repIdx = i
			break
		}
	}
	if repIdx < 0 {
		t.Fatalf("SettingsEntries() does not include a SettingRepetitions entry; all four settings must be present")
	}
	for i := 0; i < repIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}

	// Press Enter to open the numeric editor.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))

	// Type the new repetitions count digit by digit.
	const newReps = "5"
	for _, ch := range newReps {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}

	// Confirm the edit.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))

	got := m.Repetitions()
	if got == nil {
		t.Fatalf("Repetitions() = nil after editing via settings screen; want *5 (numeric editor must commit the typed value)")
	}
	if *got != 5 {
		t.Errorf("Repetitions() = %d after editing via settings screen, want 5", *got)
	}
}

// TestSettings_ChangedRepetitions_ReachesPreflightOverrides verifies that a
// repetitions count edited via the settings screen reaches
// preflight.Overrides.Repetitions when the run starts — the same contract
// the old direct-field tests verified, but now exercised through the real
// key-driven numeric-editor path.
func TestSettings_ChangedRepetitions_ReachesPreflightOverrides(t *testing.T) {
	const newReps = "3"
	const wantReps = 3
	var captured preflight.Input

	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)

	// Navigate to settings and edit the repetitions count.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}
	entries := m.SettingsEntries()
	repIdx := -1
	for i, e := range entries {
		if e.Kind == SettingRepetitions {
			repIdx = i
			break
		}
	}
	if repIdx < 0 {
		t.Fatalf("SettingsEntries() does not include SettingRepetitions")
	}
	for i := 0; i < repIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	for _, ch := range newReps {
		m, _ = safeUpdate(t, m, keyMsg(string(ch)))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))

	// Return to suite-select and start the run.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after Esc = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	m, cmd := safeUpdate(t, m, keyType(tea.KeyEnter))
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.Repetitions == nil {
		t.Fatalf("preflight.Input.Overrides.Repetitions = nil; want *%d (edited value must reach the run)", wantReps)
	}
	if *captured.Overrides.Repetitions != wantReps {
		t.Errorf("preflight.Input.Overrides.Repetitions = %d, want %d", *captured.Overrides.Repetitions, wantReps)
	}
}

// ---------------------------------------------------------------------------
// T9.3 — Every functional binding appears in the help bar
// ---------------------------------------------------------------------------

// TestSuiteSelect_HelpBar_IncludesSettingsNavKey verifies that the suite-select
// screen's help bar lists the Tab key (the key that navigates to the settings
// screen), so it is discoverable — closing the discoverability defect the old
// undocumented 'e' key had.
func TestSuiteSelect_HelpBar_IncludesSettingsNavKey(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	// "tab" should appear in the help bar section of the rendered output.
	if !strings.Contains(viewLower, "tab") {
		t.Errorf("suite-select View() help bar does not include 'tab' for the settings-navigation key; every functional key must be discoverable in the help bar.\nView:\n%s", view)
	}
}

// TestSuiteSelect_HelpBar_DoesNotIncludeUndiscoverableEditKey verifies that the
// 'e' key (the old undiscoverable inline-edit affordance) no longer appears as a
// binding in the suite-select help bar, since it has been removed from this screen.
func TestSuiteSelect_HelpBar_DoesNotIncludeUndiscoverableEditKey(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
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

// TestSettings_HelpBar_IncludesActionKey verifies that the settings screen's
// help bar lists the Enter key (the action key that edits or cycles a focused
// setting). This key is the centerpiece of the settings interaction model and
// must be discoverable.
func TestSettings_HelpBar_IncludesActionKey(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	// The action key (Enter) must appear in the help bar with a descriptor.
	// Accept "enter", "return", or "↵" as valid representations of the key.
	hasEnterKey := strings.Contains(viewLower, "enter") ||
		strings.Contains(viewLower, "return") ||
		strings.Contains(view, "↵")
	if !hasEnterKey {
		t.Errorf("settings View() help bar does not mention the action key (enter/return/↵); every functional key must be discoverable.\nView:\n%s", view)
	}
}

// TestSettings_HelpBar_IncludesNavigationKeys verifies that the settings screen's
// help bar lists the navigation keys (up/down to move between entries), so a
// user can discover how to move focus without trial and error.
func TestSettings_HelpBar_IncludesNavigationKeys(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	// Accept "up/down", "↑/↓", "up" + "down", or the shared "j/k" convention.
	hasNavKeys := strings.Contains(viewLower, "up/down") ||
		strings.Contains(viewLower, "↑/↓") ||
		strings.Contains(view, "↑") ||
		(strings.Contains(viewLower, "up") && strings.Contains(viewLower, "down"))
	if !hasNavKeys {
		t.Errorf("settings View() help bar does not include navigation keys (up/down); they must be discoverable.\nView:\n%s", view)
	}
}

// TestSettings_HelpBar_IncludesBackKey verifies that the settings screen's help
// bar lists the Esc key (the key to return to suite-select), so a user can find
// the exit without guessing.
func TestSettings_HelpBar_IncludesBackKey(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)
	// Accept "esc", "escape", or "back" as the back/cancel key label.
	hasBackKey := strings.Contains(viewLower, "esc") ||
		strings.Contains(viewLower, "escape") ||
		strings.Contains(viewLower, "back")
	if !hasBackKey {
		t.Errorf("settings View() help bar does not include the back/esc key; it must be discoverable.\nView:\n%s", view)
	}
}

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

// TestSettings_FocusedEntryHighlighted verifies that the settings screen's
// View marks the focused entry distinctly — with a cursor prefix, highlighting,
// or similar affordance — so the user can tell which entry is focused. The
// exact styling is an implementation choice; the test requires only that the
// focused entry is visually differentiated from the others.
func TestSettings_FocusedEntryHighlighted(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() = %q, want %q", m.Screen(), ScreenSettings)
	}

	// View at cursor=0.
	viewAtZero := safeView(t, m)

	// Move cursor to position 1 and re-render.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	viewAtOne := safeView(t, m)

	// The two views must differ — a focused-entry indicator must move.
	if viewAtZero == viewAtOne {
		t.Errorf("settings View() is identical before and after moving the cursor; the focused entry must be visually differentiated (cursor prefix, colour, etc.)")
	}
}
