package tui

// chronology_test.go verifies that suite-scoped settings are gated behind
// explicit suite confirmation. Before the user presses Enter on a suite:
//   - Tab from ScreenSuiteSelect must not navigate to any settings screen.
//   - ResolveSuiteDefaults must not be called (suite-scoped defaults such as
//     repetitions count must remain unresolved).
//
// After the user presses Enter:
//   - The root Model must transition to ScreenRetention, the first
//     settings screen in the sequential run-configuration flow.
//
// These tests are TDD RED-phase specifications. They compile against the new
// Screen constants added in this stage but fail against the current
// implementation, which sends Tab to ScreenSettings and Enter directly to
// ScreenProgress without a settings flow.

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// settingScreens is the complete set of screens that constitute the
// run-configuration flow introduced in Stage 3. Tab from ScreenSuiteSelect
// must not navigate to any of them; Enter must land on the first one.
//
// Note: ScreenSettings (the old monolithic settings screen) is intentionally
// absent from this list. The I3.1 implementation task removes that constant
// from app.go, so referencing it here would cause a compilation failure when
// implementation begins.
var settingScreens = []Screen{
	ScreenRetention,         // new sequential screen 1
	ScreenRepetitions,       // new sequential screen 2
	ScreenReportPath,        // new sequential screen 3
	ScreenCatalogFolder,     // new sequential screen 4
	ScreenMaxConcurrentRuns, // new sequential screen 5
}

// TestChronology_TabDoesNotNavigateToAnySettingsScreen verifies that pressing
// Tab from ScreenSuiteSelect does not navigate to any settings screen. In the
// new flow, Tab must not open settings at all; Enter is the only confirmed
// transition out of suite selection.
func TestChronology_TabDoesNotNavigateToAnySettingsScreen(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyTab})

	for _, s := range settingScreens {
		if m.Screen() == s {
			t.Errorf("Tab from ScreenSuiteSelect navigated to %q; Tab must not open any settings screen", s)
		}
	}
}

// TestChronology_EnterOnSuiteNavigatesToRetentionScreen verifies that pressing
// Enter on a suite in ScreenSuiteSelect transitions the root Model to
// ScreenRetention, the first screen in the sequential settings flow, rather
// than starting the suite immediately.
func TestChronology_EnterOnSuiteNavigatesToRetentionScreen(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	// "\r" is the carriage-return rune that the existing suite-select key
	// binding (GlobalKeys.Select) matches, consistent with other navigation
	// tests in this package.
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenRetention {
		t.Errorf("Enter on suite select: Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}
}

// TestChronology_NewModelDoesNotPreResolveSuiteDefaults verifies that
// NewModel does not call ResolveSuiteDefaults for suite index 0. Suite-scoped
// defaults (repetition count, max-concurrent-runs) must be resolved only after
// the user explicitly confirms a suite via Enter, not at construction time.
func TestChronology_NewModelDoesNotPreResolveSuiteDefaults(t *testing.T) {
	called := false
	opts := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	opts.ResolveSuiteDefaults = func(suitePath string) (SuiteDefaults, error) {
		called = true
		return SuiteDefaults{}, nil
	}

	_ = NewModel(opts)

	if called {
		t.Error("NewModel called ResolveSuiteDefaults before any suite was explicitly confirmed; suite-scoped defaults must only be resolved after Enter on a suite")
	}
}

// TestChronology_EnterOnSuiteCallsResolveSuiteDefaultsWithCorrectPath verifies
// that pressing Enter on a suite invokes ResolveSuiteDefaults with the path of
// the currently highlighted suite. The path must match the suite the user
// confirmed, not always index 0 or an empty string.
func TestChronology_EnterOnSuiteCallsResolveSuiteDefaultsWithCorrectPath(t *testing.T) {
	var capturedPath string
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	opts.ResolveSuiteDefaults = func(suitePath string) (SuiteDefaults, error) {
		capturedPath = suitePath
		return SuiteDefaults{}, nil
	}

	m := NewModel(opts)

	// Move the cursor to suite-b.yaml (index 1).
	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})

	// Confirm the selection with Enter.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	_ = m

	if capturedPath != "suite-b.yaml" {
		t.Errorf("ResolveSuiteDefaults called with path %q, want %q (must use the confirmed suite, not always index 0)", capturedPath, "suite-b.yaml")
	}
}

// TestChronology_ResolveSuiteDefaultsErrorDoesNotNavigateToSettingsFlow verifies
// that when ResolveSuiteDefaults returns an error after the user presses Enter,
// the root Model does not advance to the settings flow (ScreenRetention). An
// error from suite-defaults resolution must keep the user on or near
// ScreenSuiteSelect so they can choose a different suite.
func TestChronology_ResolveSuiteDefaultsErrorDoesNotNavigateToSettingsFlow(t *testing.T) {
	opts := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	opts.ResolveSuiteDefaults = func(suitePath string) (SuiteDefaults, error) {
		return SuiteDefaults{}, fmt.Errorf("resolution failed: suite defaults unavailable")
	}

	m := NewModel(opts)
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Enter: attempt to confirm suite

	if m.Screen() == ScreenRetention {
		t.Errorf("ResolveSuiteDefaults error: Screen() = %q; must not navigate into settings flow when suite-defaults resolution fails", m.Screen())
	}
}

// TestChronology_CursorMovementDoesNotPreResolveSuiteDefaults verifies that
// moving the cursor between suites in ScreenSuiteSelect does not call
// ResolveSuiteDefaults. Suite-scoped defaults must remain unresolved until the
// user presses Enter to confirm a selection.
func TestChronology_CursorMovementDoesNotPreResolveSuiteDefaults(t *testing.T) {
	called := false
	opts := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	opts.ResolveSuiteDefaults = func(suitePath string) (SuiteDefaults, error) {
		called = true
		return SuiteDefaults{}, nil
	}

	m := NewModel(opts)
	called = false // reset: NewModel must not have set it (tested separately above)

	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown}) // move cursor down
	if called {
		t.Error("cursor Down triggered ResolveSuiteDefaults; suite-scoped defaults must only be resolved after explicit Enter confirmation")
	}

	m, _ = safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyUp}) // move cursor back up
	if called {
		t.Error("cursor Up triggered ResolveSuiteDefaults; suite-scoped defaults must only be resolved after explicit Enter confirmation")
	}
	_ = m
}
