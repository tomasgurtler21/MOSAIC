package tui

// settings_navigation_test.go verifies the sequential run-configuration flow
// introduced in Stage 3: after explicit suite confirmation, the root Model
// navigates through five self-contained setting screens in order, and completing
// the last screen starts the suite.
//
// Screen sequence:
//   ScreenSuiteSelect  --Enter-->  ScreenRetention
//   ScreenRetention    --Enter-->  ScreenRepetitions
//   ScreenRepetitions  --Enter-->  ScreenReportPath
//   ScreenReportPath   --Enter-->  ScreenCatalogFolder
//   ScreenCatalogFolder --Enter--> ScreenMaxConcurrentRuns
//   ScreenMaxConcurrentRuns --Enter--> ScreenProgress (suite starts)
//
// Back navigation:
//   ScreenRetention       --Esc--> ScreenSuiteSelect
//   ScreenRepetitions     --Esc--> ScreenRetention
//   ScreenReportPath      --Esc--> ScreenRepetitions
//   ScreenCatalogFolder   --Esc--> ScreenReportPath
//   ScreenMaxConcurrentRuns --Esc--> ScreenCatalogFolder
//
// These tests are TDD RED-phase specifications. They compile against the new
// Screen constants but fail against the current implementation, which transitions
// directly from ScreenSuiteSelect to ScreenProgress on Enter, bypassing the
// settings flow entirely.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/domain"
)

// advanceToSettingsFlow navigates from ScreenSuiteSelect to ScreenRetention
// by pressing Enter on the first suite. It fails the test immediately if the
// transition does not land on ScreenRetention, because all subsequent
// settings-navigation assertions depend on being inside the settings flow.
func advanceToSettingsFlow(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Enter: confirm suite selection
	if m.Screen() != ScreenRetention {
		t.Fatalf("advanceToSettingsFlow: Screen() after Enter = %q, want %q (settings flow must begin at ScreenRetention after explicit suite confirmation)", m.Screen(), ScreenRetention)
	}
	return m
}

// pressEnterOnScreen sends an Enter key to the model and returns the result.
// It uses keyMsg("\r") — the carriage-return rune — which is the form that
// GlobalKeys.Select matches, consistent with advanceToSettingsFlow above.
// Both helpers must use the same key representation so they exercise the same
// code path in the implementation's key-binding dispatch.
func pressEnterOnScreen(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	return safeUpdate(t, m, keyMsg("\r"))
}

// pressEscOnScreen sends an Esc key to the model and returns the result.
func pressEscOnScreen(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	return safeUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
}

// ---------------------------------------------------------------------------
// Forward navigation
// ---------------------------------------------------------------------------

// TestSettingsNavigation_EnterAdvancesFromRetentionToRepetitions verifies that
// pressing Enter on ScreenRetention advances the root Model to ScreenRepetitions.
func TestSettingsNavigation_EnterAdvancesFromRetentionToRepetitions(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m) // ScreenSuiteSelect → ScreenRetention

	m, _ = pressEnterOnScreen(t, m)
	if m.Screen() != ScreenRepetitions {
		t.Errorf("Enter on ScreenRetention: Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}
}

// TestSettingsNavigation_EnterAdvancesFromRepetitionsToReportPath verifies that
// pressing Enter on ScreenRepetitions advances the root Model to ScreenReportPath.
func TestSettingsNavigation_EnterAdvancesFromRepetitionsToReportPath(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m)

	m, _ = pressEnterOnScreen(t, m) // ScreenRetention → ScreenRepetitions
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Enter on ScreenRetention: Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}

	m, _ = pressEnterOnScreen(t, m)
	if m.Screen() != ScreenReportPath {
		t.Errorf("Enter on ScreenRepetitions: Screen() = %q, want %q", m.Screen(), ScreenReportPath)
	}
}

// TestSettingsNavigation_EnterAdvancesFromReportPathToCatalogFolder verifies that
// pressing Enter on ScreenReportPath advances the root Model to ScreenCatalogFolder.
func TestSettingsNavigation_EnterAdvancesFromReportPathToCatalogFolder(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m)

	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath} {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Enter: Screen() = %q, want %q", m.Screen(), want)
		}
	}

	m, _ = pressEnterOnScreen(t, m)
	if m.Screen() != ScreenCatalogFolder {
		t.Errorf("Enter on ScreenReportPath: Screen() = %q, want %q", m.Screen(), ScreenCatalogFolder)
	}
}

// TestSettingsNavigation_EnterAdvancesFromCatalogFolderToMaxConcurrentRuns verifies
// that pressing Enter on ScreenCatalogFolder advances the root Model to
// ScreenMaxConcurrentRuns.
func TestSettingsNavigation_EnterAdvancesFromCatalogFolderToMaxConcurrentRuns(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m)

	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder} {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Enter: Screen() = %q, want %q", m.Screen(), want)
		}
	}

	m, _ = pressEnterOnScreen(t, m)
	if m.Screen() != ScreenMaxConcurrentRuns {
		t.Errorf("Enter on ScreenCatalogFolder: Screen() = %q, want %q", m.Screen(), ScreenMaxConcurrentRuns)
	}
}

// TestSettingsNavigation_CompletingLastSettingScreenStartsSuite verifies that
// pressing Enter on ScreenMaxConcurrentRuns (the last settings screen) transitions
// the root Model to ScreenProgress and produces a Cmd to start the suite run.
func TestSettingsNavigation_CompletingLastSettingScreenStartsSuite(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = advanceToSettingsFlow(t, m)

	// Advance through screens 1-4 (Retention is already current).
	forwardPath := []Screen{
		ScreenRepetitions,
		ScreenReportPath,
		ScreenCatalogFolder,
		ScreenMaxConcurrentRuns,
	}
	for _, want := range forwardPath {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Enter: Screen() = %q, want %q (cannot reach last screen)", m.Screen(), want)
		}
	}

	// Enter on the last screen must start the suite.
	m, cmd := pressEnterOnScreen(t, m)
	if m.Screen() != ScreenProgress {
		t.Errorf("Enter on ScreenMaxConcurrentRuns: Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}
	if cmd == nil {
		t.Error("Enter on last settings screen produced no Cmd; expected a suite-start command")
	}
}

// ---------------------------------------------------------------------------
// Backward navigation
// ---------------------------------------------------------------------------

// TestSettingsNavigation_EscFromRetentionReturnsToSuiteSelect verifies that
// pressing Esc on ScreenRetention (the first settings screen) returns the root
// Model to ScreenSuiteSelect, allowing the user to choose a different suite.
func TestSettingsNavigation_EscFromRetentionReturnsToSuiteSelect(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m)

	m, _ = pressEscOnScreen(t, m)
	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Esc on ScreenRetention: Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
}

// TestSettingsNavigation_EscFromRepetitionsReturnsToRetention verifies that
// pressing Esc on ScreenRepetitions returns the root Model to ScreenRetention.
func TestSettingsNavigation_EscFromRepetitionsReturnsToRetention(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m)

	m, _ = pressEnterOnScreen(t, m) // → ScreenRepetitions
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Enter on ScreenRetention: Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}

	m, _ = pressEscOnScreen(t, m)
	if m.Screen() != ScreenRetention {
		t.Errorf("Esc on ScreenRepetitions: Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}
}

// TestSettingsNavigation_EscFromReportPathReturnsToRepetitions verifies that
// pressing Esc on ScreenReportPath returns the root Model to ScreenRepetitions.
func TestSettingsNavigation_EscFromReportPathReturnsToRepetitions(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m)

	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath} {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Enter: Screen() = %q, want %q", m.Screen(), want)
		}
	}

	m, _ = pressEscOnScreen(t, m)
	if m.Screen() != ScreenRepetitions {
		t.Errorf("Esc on ScreenReportPath: Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}
}

// TestSettingsNavigation_EscFromCatalogFolderReturnsToReportPath verifies that
// pressing Esc on ScreenCatalogFolder returns the root Model to ScreenReportPath.
func TestSettingsNavigation_EscFromCatalogFolderReturnsToReportPath(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m)

	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder} {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Enter: Screen() = %q, want %q", m.Screen(), want)
		}
	}

	m, _ = pressEscOnScreen(t, m)
	if m.Screen() != ScreenReportPath {
		t.Errorf("Esc on ScreenCatalogFolder: Screen() = %q, want %q", m.Screen(), ScreenReportPath)
	}
}

// TestSettingsNavigation_EscFromMaxConcurrentRunsReturnsToCatalogFolder verifies
// that pressing Esc on ScreenMaxConcurrentRuns returns the root Model to
// ScreenCatalogFolder.
func TestSettingsNavigation_EscFromMaxConcurrentRunsReturnsToCatalogFolder(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m)

	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Enter: Screen() = %q, want %q", m.Screen(), want)
		}
	}

	m, _ = pressEscOnScreen(t, m)
	if m.Screen() != ScreenCatalogFolder {
		t.Errorf("Esc on ScreenMaxConcurrentRuns: Screen() = %q, want %q", m.Screen(), ScreenCatalogFolder)
	}
}

// ---------------------------------------------------------------------------
// Re-entry after back navigation
// ---------------------------------------------------------------------------

// TestSettingsNavigation_ForwardAfterBackNavigationWorks verifies that navigating
// backward and then forward again through the settings flow works correctly.
// The root Model must re-enter screens cleanly (Reset state) so the user can
// change their mind and proceed again without stale Done/Back flags.
func TestSettingsNavigation_ForwardAfterBackNavigationWorks(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m) // ScreenSuiteSelect → ScreenRetention

	// Advance to ScreenRepetitions, then go back.
	m, _ = pressEnterOnScreen(t, m) // → ScreenRepetitions
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Enter: Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}
	m, _ = pressEscOnScreen(t, m) // ← ScreenRetention
	if m.Screen() != ScreenRetention {
		t.Fatalf("Esc: Screen() = %q, want %q", m.Screen(), ScreenRetention)
	}

	// Going forward again from ScreenRetention must work.
	m, _ = pressEnterOnScreen(t, m) // → ScreenRepetitions again
	if m.Screen() != ScreenRepetitions {
		t.Errorf("Enter on ScreenRetention after back-and-re-enter: Screen() = %q, want %q", m.Screen(), ScreenRepetitions)
	}
}

// ---------------------------------------------------------------------------
// Window resize during settings flow
// ---------------------------------------------------------------------------

// TestSettingsNavigation_WindowResizeDoesNotChangeScreenDuringSettingsFlow
// verifies that a terminal resize message while inside the settings flow does
// not change the active screen and does not trigger unexpected transitions.
func TestSettingsNavigation_WindowResizeDoesNotChangeScreenDuringSettingsFlow(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m) // ScreenSuiteSelect → ScreenRetention

	m, _ = safeUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.Screen() != ScreenRetention {
		t.Errorf("WindowSizeMsg during settings flow changed Screen() to %q, want %q", m.Screen(), ScreenRetention)
	}
}

// ---------------------------------------------------------------------------
// Suite re-entry after Esc back to ScreenSuiteSelect
// ---------------------------------------------------------------------------

// TestSettingsNavigation_EscToSuiteSelectAndReEnterWorks verifies that after
// pressing Esc from ScreenRetention (which returns to ScreenSuiteSelect), the
// user can press Enter again to re-enter the settings flow. ScreenSuiteSelect
// must remain fully functional after a back-navigation from the settings flow.
func TestSettingsNavigation_EscToSuiteSelectAndReEnterWorks(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m) // ScreenSuiteSelect → ScreenRetention

	// Esc back to suite selection.
	m, _ = pressEscOnScreen(t, m)
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Esc from ScreenRetention: Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	// Re-enter the settings flow with Enter — must reach ScreenRetention again.
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenRetention {
		t.Errorf("Enter on ScreenSuiteSelect after prior Esc: Screen() = %q, want %q (settings re-entry must work after back-navigation)", m.Screen(), ScreenRetention)
	}
}

// ---------------------------------------------------------------------------
// Extended back-navigation coverage
// ---------------------------------------------------------------------------

// TestSettingsNavigation_DeepBackNavigationAndRetraversalWorks verifies that
// a user can back-navigate all the way from ScreenMaxConcurrentRuns to
// ScreenRetention and then re-traverse all five screens forward. This catches
// bugs where Reset() clears Done/Back flags on early screens but not on deeper
// screens, which would prevent re-entry beyond the first Esc point.
func TestSettingsNavigation_DeepBackNavigationAndRetraversalWorks(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	m = advanceToSettingsFlow(t, m) // → ScreenRetention

	// Traverse all five screens forward to reach ScreenMaxConcurrentRuns.
	forwardPath := []Screen{
		ScreenRepetitions,
		ScreenReportPath,
		ScreenCatalogFolder,
		ScreenMaxConcurrentRuns,
	}
	for _, want := range forwardPath {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Enter: Screen() = %q, want %q (cannot reach ScreenMaxConcurrentRuns)", m.Screen(), want)
		}
	}

	// Back-navigate all the way through to ScreenRetention.
	backPath := []Screen{
		ScreenCatalogFolder,
		ScreenReportPath,
		ScreenRepetitions,
		ScreenRetention,
	}
	for _, want := range backPath {
		m, _ = pressEscOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Esc: Screen() = %q, want %q (back-navigation failed mid-sequence)", m.Screen(), want)
		}
	}

	// Re-traverse all five screens forward again from ScreenRetention.
	for _, want := range forwardPath {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Errorf("Enter on re-traversal: Screen() = %q, want %q (screen after back+re-enter did not match expected sequence)", m.Screen(), want)
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end value integration
// ---------------------------------------------------------------------------

// TestSettingsNavigation_ConfiguredRetentionReachesRunner verifies that the
// retention policy configured on ScreenRetention flows into the suite runner
// when the settings flow is completed. This catches a class of regression
// where screen values are collected but not passed to startSelectedSuite, or
// where the root model passes a stale/default value instead of the
// screen-configured one.
//
// The test cycles retention to RetainOnFailure (one Space press from the
// default RetainNever) and completes all remaining screens, then executes the
// suite-start Cmd and asserts the runner received the non-default policy.
func TestSettingsNavigation_ConfiguredRetentionReachesRunner(t *testing.T) {
	runner := newFakeSuiteRunner()
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, runner))
	m = advanceToSettingsFlow(t, m) // → ScreenRetention

	// Cycle retention from RetainNever to RetainOnFailure with one Space press.
	// Space cycles: RetainNever → RetainOnFailure → RetainAlways → RetainNever.
	m, _ = safeUpdate(t, m, keyMsg(" "))

	// Advance through all remaining setting screens.
	remainingScreens := []Screen{
		ScreenRepetitions,
		ScreenReportPath,
		ScreenCatalogFolder,
		ScreenMaxConcurrentRuns,
	}
	for _, want := range remainingScreens {
		m, _ = pressEnterOnScreen(t, m)
		if m.Screen() != want {
			t.Fatalf("Enter: Screen() = %q, want %q (cannot reach ScreenMaxConcurrentRuns)", m.Screen(), want)
		}
	}

	// Confirm the last screen — must transition to ScreenProgress and return a Cmd.
	m, cmd := pressEnterOnScreen(t, m)
	if m.Screen() != ScreenProgress {
		t.Fatalf("Enter on ScreenMaxConcurrentRuns: Screen() = %q, want %q", m.Screen(), ScreenProgress)
	}
	if cmd == nil {
		t.Fatal("Enter on last settings screen produced no Cmd; cannot verify runner received configured values")
	}

	// Execute the command chain to trigger fakeSuiteRunner.Run. The cmd may
	// require one or two hops (preflight then run); exhaust one level of
	// intermediate messages to cover both cases.
	msg := runCmd(t, cmd)
	if msg != nil {
		m, cmd = safeUpdate(t, m, msg)
		if cmd != nil {
			_ = runCmd(t, cmd)
		}
	}
	_ = m

	// Verify the runner received the retention policy that was configured on
	// ScreenRetention, not the default zero value.
	runner.mu.Lock()
	gotRetention := runner.gotRetention
	runner.mu.Unlock()

	if gotRetention != domain.RetainOnFailure {
		t.Errorf("fakeSuiteRunner received RetentionPolicy = %v, want %v (screen-configured retention did not reach the runner)", gotRetention, domain.RetainOnFailure)
	}
}
