package tui

// max_concurrent_runs_test.go covers the TUI's max-concurrent-runs setting
// (T12.2, T12.3 TUI side, T12.4 TUI side).
//
// RED-phase notes:
//   - All tests in this file reference Model.maxConcurrentRuns (an unexported
//     field) and/or Model.MaxConcurrentRuns() (an accessor) that do not yet
//     exist on Model. This file will not compile against the current code.
//     That is the expected RED state. Add these to app.go as part of I12.3.
//   - TestTUICLIParity_MaxConcurrentRunsKind_IsInParityDeclaration compiles
//     (SettingMaxConcurrentRuns exists as a contract stub) but FAILS until
//     SettingMaxConcurrentRuns is appended to CLIParitySettingKinds (I12.4).
//   - TestSettings_MaxConcurrentRunsEntry_PresentInSettingsEntries compiles
//     once the field and accessor exist, but FAILS until SettingsEntries()
//     returns the new entry (I12.3).

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// ---------------------------------------------------------------------------
// T12.3 — TUI parity declaration
// ---------------------------------------------------------------------------

// TestTUICLIParity_MaxConcurrentRunsKind_IsInParityDeclaration verifies that
// SettingMaxConcurrentRuns is listed in CLIParitySettingKinds. It must be
// listed so the cross-check tests enforce that the corresponding CLI flag
// --max-concurrent-runs is present on the command surface.
//
// RED-phase note: SettingMaxConcurrentRuns is declared (contract stub exists)
// but is not yet in CLIParitySettingKinds. Test compiles and FAILS.
func TestTUICLIParity_MaxConcurrentRunsKind_IsInParityDeclaration(t *testing.T) {
	for _, kind := range CLIParitySettingKinds {
		if kind == SettingMaxConcurrentRuns {
			return
		}
	}
	t.Errorf("CLIParitySettingKinds does not include SettingMaxConcurrentRuns; the CLI --max-concurrent-runs flag maps to this TUI setting, so it must appear in the parity declaration (I12.4)")
}

// ---------------------------------------------------------------------------
// T12.2 — SettingMaxConcurrentRuns present in SettingsEntries
// ---------------------------------------------------------------------------

// TestSettings_MaxConcurrentRunsEntry_PresentInSettingsEntries verifies that
// SettingsEntries() returns an entry with Kind == SettingMaxConcurrentRuns.
// The setting must appear on the settings screen alongside retention,
// repetitions, report path and catalog folder.
//
// RED-phase note: will not compile until Model.maxConcurrentRuns and
// Model.MaxConcurrentRuns() are declared; fails at runtime until I12.3 adds
// the entry to SettingsEntries().
func TestSettings_MaxConcurrentRunsEntry_PresentInSettingsEntries(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingMaxConcurrentRuns {
			return
		}
	}
	t.Errorf("SettingsEntries() does not include a SettingMaxConcurrentRuns entry; the max-concurrent-runs setting must be on the settings screen (I12.3)")
}

// TestSettings_MaxConcurrentRunsEntry_UsesEditNumeric verifies that the
// max-concurrent-runs entry uses EditNumeric mode — it is a positive integer
// that the user types, not a cycle through a fixed set of values.
//
// RED-phase note: will not compile until Model.maxConcurrentRuns and
// Model.MaxConcurrentRuns() are declared; fails at runtime until I12.3.
func TestSettings_MaxConcurrentRunsEntry_UsesEditNumeric(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingMaxConcurrentRuns {
			if e.EditMode != EditNumeric {
				t.Errorf("SettingsEntry{Kind: %q}.EditMode = %v, want EditNumeric (max-concurrent-runs is a positive integer)", e.Kind, e.EditMode)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() does not include SettingMaxConcurrentRuns; cannot verify its EditMode")
}

// TestSettings_MaxConcurrentRunsEntry_HasNonEmptyLabel verifies that the
// max-concurrent-runs settings entry has a non-empty label, so it renders
// identifiably on the settings screen.
//
// RED-phase note: will not compile until Model.maxConcurrentRuns and
// Model.MaxConcurrentRuns() are declared.
func TestSettings_MaxConcurrentRunsEntry_HasNonEmptyLabel(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingMaxConcurrentRuns {
			if e.Label == "" {
				t.Errorf("SettingsEntry{Kind: %q}.Label is empty; the max-concurrent-runs entry must have a display label", e.Kind)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() does not include SettingMaxConcurrentRuns")
}

// TestNewModel_MaxConcurrentRuns_StartsAsNil verifies that a freshly
// constructed Model has no max-concurrent-runs override set — the suite's
// default (suite.DefaultMaxConcurrentRuns) applies until the user explicitly
// configures an override, mirroring how repetitions starts as nil.
//
// RED-phase note: will not compile until Model.maxConcurrentRuns field and
// Model.MaxConcurrentRuns() accessor are declared in app.go (I12.3).
func TestNewModel_MaxConcurrentRuns_StartsAsNil(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if got := m.MaxConcurrentRuns(); got != nil {
		t.Errorf("MaxConcurrentRuns() = %v (non-nil) on a freshly constructed Model; want nil (no override set, suite default applies)", got)
	}
}

// TestModel_MaxConcurrentRuns_WhenSet_ReturnsValue verifies that after the
// model's maxConcurrentRuns field is set to a value, the MaxConcurrentRuns()
// accessor reflects it.
//
// RED-phase note: will not compile until Model.maxConcurrentRuns field and
// Model.MaxConcurrentRuns() accessor are declared in app.go (I12.3).
func TestModel_MaxConcurrentRuns_WhenSet_ReturnsValue(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	want := 6
	m.maxConcurrentRuns = intPtr(want)

	got := m.MaxConcurrentRuns()
	if got == nil {
		t.Fatalf("MaxConcurrentRuns() = nil after setting maxConcurrentRuns to %d; want *%d", want, want)
	}
	if *got != want {
		t.Errorf("MaxConcurrentRuns() = %d, want %d", *got, want)
	}
}

// ---------------------------------------------------------------------------
// T12.2 — Setting is displayed on screen
// ---------------------------------------------------------------------------

// TestSettings_MaxConcurrentRuns_DisplayedOnScreen verifies that the
// max-concurrent-runs screen renders some indication of the setting, so the
// user can see the effective bound before starting a run.
func TestSettings_MaxConcurrentRuns_DisplayedOnScreen(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner()))

	// Navigate to ScreenMaxConcurrentRuns: Enter×5 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during navigation", m.Screen(), want)
		}
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)

	// Accept any term that clearly refers to the concurrent-runs concept.
	hasDisplay := strings.Contains(viewLower, "concurrent") ||
		strings.Contains(viewLower, "max-concurrent") ||
		strings.Contains(viewLower, "parallel")
	if !hasDisplay {
		t.Errorf("max-concurrent-runs screen View() does not mention the setting:\n%s", view)
	}
}

// TestSettings_MaxConcurrentRuns_OverrideValue_DisplayedOnScreen verifies
// that when a max-concurrent-runs override is set on the Model, its numeric
// value appears in the max-concurrent-runs screen's rendered output.
func TestSettings_MaxConcurrentRuns_OverrideValue_DisplayedOnScreen(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner()))
	m.maxConcurrentRuns = intPtr(7)

	// Navigate to ScreenMaxConcurrentRuns: Enter×5 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during navigation", m.Screen(), want)
		}
	}

	view := safeView(t, m)
	if !strings.Contains(view, "7") {
		t.Errorf("max-concurrent-runs screen View() does not display the override value 7:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// T12.2 — Setting is editable via the numeric editor
// ---------------------------------------------------------------------------

// TestSettings_MaxConcurrentRuns_ViaActionKey verifies that navigating to the
// max-concurrent-runs screen, typing a digit, and confirming with Enter commits
// the new bound to the Model.
func TestSettings_MaxConcurrentRuns_ViaActionKey(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	// Navigate to ScreenMaxConcurrentRuns: Enter×5 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during navigation", m.Screen(), want)
		}
	}

	// Type a digit and confirm.
	m, _ = safeUpdate(t, m, keyMsg("6"))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // confirm → Done → startSelectedSuite

	got := m.MaxConcurrentRuns()
	if got == nil {
		t.Fatalf("MaxConcurrentRuns() = nil after typing 6 and confirming on ScreenMaxConcurrentRuns; want *6")
	}
	if *got != 6 {
		t.Errorf("MaxConcurrentRuns() = %d after typing 6 and confirming, want 6", *got)
	}
}

// TestSettings_MaxConcurrentRuns_EscCancelsEdit verifies that pressing Esc on
// the max-concurrent-runs screen discards the draft and leaves the original
// value unchanged.
func TestSettings_MaxConcurrentRuns_EscCancelsEdit(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m.maxConcurrentRuns = intPtr(4)

	// Navigate to ScreenMaxConcurrentRuns: Enter×5 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during navigation", m.Screen(), want)
		}
	}

	m, _ = safeUpdate(t, m, keyMsg("9"))           // type a draft value
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))   // cancel

	got := m.MaxConcurrentRuns()
	if got == nil || *got != 4 {
		t.Errorf("MaxConcurrentRuns() after Esc = %v, want *4 (original value must be preserved)", got)
	}
}

// ---------------------------------------------------------------------------
// T12.4 — Value reaches run configuration (TUI path)
// ---------------------------------------------------------------------------

// TestSettings_MaxConcurrentRuns_DisplayReflectsCurrentValue verifies that
// the SettingsEntry for max-concurrent-runs displays the model's current
// value, so the user sees the effective bound before editing.
//
// RED-phase note: will not compile until Model.maxConcurrentRuns field exists.
func TestSettings_MaxConcurrentRuns_DisplayReflectsCurrentValue(t *testing.T) {
	wantBound := 8
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m.maxConcurrentRuns = intPtr(wantBound)

	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingMaxConcurrentRuns {
			if !strings.Contains(e.Display, "8") {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want it to contain \"8\" (current max-concurrent-runs override)", e.Kind, e.Display)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() does not include SettingMaxConcurrentRuns")
}

// ---------------------------------------------------------------------------
// T12.2 — TUI numeric editor rejects invalid values (Issue 5 addition)
// ---------------------------------------------------------------------------

// TestSettings_MaxConcurrentRuns_ZeroValue_IsRejected verifies that typing
// "0" on the max-concurrent-runs screen and confirming with Enter does not
// commit zero to the model. Zero is below the minimum of 1 and must be rejected.
func TestSettings_MaxConcurrentRuns_ZeroValue_IsRejected(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	// Leave maxConcurrentRuns nil (no override) so any acceptance is visible.

	// Navigate to ScreenMaxConcurrentRuns: Enter×5 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during navigation", m.Screen(), want)
		}
	}

	m, _ = safeUpdate(t, m, keyMsg("0"))           // type invalid value
	m, _ = safeUpdate(t, m, keyMsg("\r"))           // attempt to confirm

	got := m.MaxConcurrentRuns()
	if got != nil && *got == 0 {
		t.Errorf("MaxConcurrentRuns() = *0 after typing 0; zero is below the minimum of 1 and must be rejected")
	}
}

// TestSettings_MaxConcurrentRuns_NegativeValue_IsRejected verifies that typing
// a negative value on the max-concurrent-runs screen does not commit an
// invalid negative bound. A negative bound is nonsensical and must be rejected.
func TestSettings_MaxConcurrentRuns_NegativeValue_IsRejected(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	originalValue := m.MaxConcurrentRuns() // nil for a fresh model

	// Navigate to ScreenMaxConcurrentRuns: Enter×5 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("Screen() = %q, want %q during navigation", m.Screen(), want)
		}
	}

	// Type "-1": the '-' key is a non-digit and must be ignored; only '1' is accepted.
	m, _ = safeUpdate(t, m, keyMsg("-"))
	m, _ = safeUpdate(t, m, keyMsg("1"))
	m, _ = safeUpdate(t, m, keyMsg("\r")) // attempt to confirm

	got := m.MaxConcurrentRuns()
	// The committed result must not be negative.
	if originalValue == nil && got != nil && *got < 0 {
		t.Errorf("MaxConcurrentRuns() = *%d after typing a negative value; negative bounds must be rejected", *got)
	}
	if originalValue != nil && (got == nil || *got != *originalValue) {
		t.Errorf("MaxConcurrentRuns() changed from *%d to %v after a rejected negative entry; the original value must be preserved", *originalValue, got)
	}
}

// ---------------------------------------------------------------------------
// T12.5 / T12.4 — TUI default path: nil means DefaultMaxConcurrentRuns (Issue 6)
// ---------------------------------------------------------------------------

// TestModel_MaxConcurrentRunsNil_ModelIndicatesDefaultApplies verifies that a
// Model with maxConcurrentRuns == nil exposes nil from MaxConcurrentRuns(),
// which is the signal to the composition root that suite.DefaultMaxConcurrentRuns
// should be used when constructing the suite. The composition root wiring test
// (in cmd/mosaic-agent-test) verifies the full chain; this test verifies the
// model's contribution to it.
//
// RED-phase note: compiles once field/accessor are declared; this specific test
// is trivially satisfied by the accessor returning nil for nil — it compiles
// and passes in GREEN immediately once the accessor stub exists. Its purpose
// is to make the nil-means-default contract explicit and tie it to the model.
func TestModel_MaxConcurrentRunsNil_ModelIndicatesDefaultApplies(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	// A fresh model has no override; MaxConcurrentRuns() returns nil so the
	// composition root applies suite.DefaultMaxConcurrentRuns.
	got := m.MaxConcurrentRuns()
	if got != nil {
		t.Errorf("MaxConcurrentRuns() = *%d on a Model with no override; want nil so the composition root knows to apply suite.DefaultMaxConcurrentRuns (I12.5)", *got)
	}
}

// TestSettings_MaxConcurrentRuns_SettingsEntry_DisplaysDefaultWhenNilOverride
// verifies that the settings-screen entry for max-concurrent-runs displays an
// indication of the default value when no override is set. A user who has not
// touched the setting should see the effective value (DefaultMaxConcurrentRuns)
// rather than a blank or "unset" marker that hides what bound will be applied.
//
// RED-phase note: will not compile until field/accessor are declared; FAILS
// until I12.3 renders the default in the entry's Display field.
func TestSettings_MaxConcurrentRuns_SettingsEntry_DisplaysDefaultWhenNilOverride(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	// No override: maxConcurrentRuns is nil.
	if m.MaxConcurrentRuns() != nil {
		t.Skip("maxConcurrentRuns is not nil; this test requires the nil-override case")
	}

	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingMaxConcurrentRuns {
			if e.Display == "" {
				t.Errorf("SettingsEntry{Kind: %q}.Display is empty when no override is set; the entry should display the effective default (suite.DefaultMaxConcurrentRuns) so the user sees what bound will be applied (I12.3)", e.Kind)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() does not include SettingMaxConcurrentRuns")
}

// ---------------------------------------------------------------------------
// Continued from T12.4 — value reaches run configuration (TUI path)
// ---------------------------------------------------------------------------

// TestSuiteSelect_MaxConcurrentRuns_WhenSet_ReachesPreflightPath documents the
// expected behaviour that a configured maxConcurrentRuns value is carried into
// the suite run. The exact threading mechanism is determined by the
// composition root (I12.5): the TUI's SuiteRunner wrapper rebuilds the suite
// per call with the live maxConcurrentRuns at run-start time.
//
// This test verifies that maxConcurrentRuns is set on the model before the
// run starts — the per-call threading is tested by the composition-root's own
// wiring tests (I12.5), which are outside this package's scope.
//
// RED-phase note: will not compile until Model.maxConcurrentRuns field and
// Model.MaxConcurrentRuns() accessor are declared.
func TestSuiteSelect_MaxConcurrentRuns_WhenSet_ModelCarriesValue(t *testing.T) {
	wantBound := 3
	var captured *int

	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)
	m.maxConcurrentRuns = intPtr(wantBound)

	// Verify the model carries the value before the run starts.
	got := m.MaxConcurrentRuns()
	if got == nil {
		t.Fatalf("MaxConcurrentRuns() = nil after setting maxConcurrentRuns to %d; want *%d", wantBound, wantBound)
	}
	captured = got

	m, cmd := safeUpdate(t, m, keyMsg("\r"))
	if cmd != nil {
		_ = runCmd(t, cmd)
	}
	_ = m

	if *captured != wantBound {
		t.Errorf("captured MaxConcurrentRuns = %d before run; want %d", *captured, wantBound)
	}
}
