package tui

// settings_initial_value_test.go verifies that initSettingScreens initializes
// the RepetitionsScreen and MaxConcurrentRunsScreen with the resolved suite
// default (or the documented package default) rather than a hard-coded zero
// when no user override is set, and that the process-reports flow initializes
// the StoreInputScreen with the current working directory rather than an empty
// string.
//
// These tests cover the call-site bugs targeted by I2.1 and I2.3. The screen-
// level contract tests in the screens/ package confirm that the screens handle
// non-zero initial values correctly, but they do not verify that the TUI passes
// those values to the constructors. These tests exercise that call site.
//
// RED phase:
//   - TestInitSettingScreens_RepetitionsScreen_ShowsSuiteDefault_WhenNoOverride:
//     fails because initSettingScreens currently passes 0 to NewRepetitionsScreen
//     when no user override is set; the screen therefore shows "0" instead of the
//     suite default "5".
//   - TestInitSettingScreens_MaxConcurrentRunsScreen_ShowsDefaultBound_WhenNoOverride:
//     fails because initSettingScreens currently passes 0 to NewMaxConcurrentRunsScreen;
//     the screen shows "0" instead of the suite.DefaultMaxConcurrentRuns value.
//   - TestProcessSelect_StoreReports_InitialPathIsNonEmpty: fails because
//     updateProcessSelect currently passes "" to NewStoreInputScreen; Path()
//     returns "" instead of the current working directory.
//
// TestInitSettingScreens_RepetitionsScreen_EnterWithoutTyping_KeepsNilOverride
// currently passes (Enter on a 0-initialized screen produces value 0, which the
// model treats as nil). It is included here as a guard: after a naive I2.1 fix
// (passing the suite default without updating the nil-preservation logic), it
// will fail, forcing the implementer to keep the nil-override semantic intact.

import (
	"fmt"
	"strings"
	"testing"

	"mosaic-agent-test/internal/suite"
)

// ---------------------------------------------------------------------------
// I2.1 call-site: RepetitionsScreen initial value
// ---------------------------------------------------------------------------

// TestInitSettingScreens_RepetitionsScreen_ShowsSuiteDefault_WhenNoOverride
// verifies that when a suite's authored repetitions default is known and the user
// has not set an explicit override, the RepetitionsScreen is initialized with the
// suite default and displays it before any input.
func TestInitSettingScreens_RepetitionsScreen_ShowsSuiteDefault_WhenNoOverride(t *testing.T) {
	// Arrange — suite declares repetitions default of 5; no user override.
	suiteDefault := 5
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = func(suitePath string) (SuiteDefaults, error) {
		return SuiteDefaults{Repetitions: intPtr(suiteDefault)}, nil
	}
	m := NewModel(o)
	// m.repetitions is nil — no user override set.

	// Act — navigate to ScreenRepetitions. Confirming a suite calls initSettingScreens,
	// which is where the bug lives: it passes repsInitial=0 instead of suiteDefault.
	m = advanceToSettingsFlow(t, m) // Enter on suite → ScreenRetention
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Enter on Retention → ScreenRepetitions
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Screen() = %q, want %q (test setup failed)", m.Screen(), ScreenRepetitions)
	}

	// Assert — View must show the suite default "5", not "0".
	view := safeView(t, m)
	if !strings.Contains(view, fmt.Sprintf("%d", suiteDefault)) {
		t.Errorf(
			"ScreenRepetitions View() does not contain the suite default %d:\n%s\n"+
				"initSettingScreens must pass the resolved suite default to "+
				"NewRepetitionsScreen instead of 0 when no user override is set (I2.1)",
			suiteDefault, view,
		)
	}
}

// TestInitSettingScreens_RepetitionsScreen_EnterWithoutTyping_KeepsNilOverride
// verifies that when the RepetitionsScreen is initialized with the suite default
// (after I2.1 is applied) and the user presses Enter without typing, the model's
// repetitions override remains nil. Pressing Enter without typing must not commit
// the displayed suite default as a user override; nil must be preserved so the
// suite applies its own default at run time rather than treating the screen's
// initial value as an explicit user choice.
func TestInitSettingScreens_RepetitionsScreen_EnterWithoutTyping_KeepsNilOverride(t *testing.T) {
	// Arrange — suite declares repetitions default of 5; no user override.
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = func(suitePath string) (SuiteDefaults, error) {
		return SuiteDefaults{Repetitions: intPtr(5)}, nil
	}
	m := NewModel(o)
	// m.repetitions is nil — no user override set.

	// Act — navigate to ScreenRepetitions, then press Enter without typing.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	m, _ = safeUpdate(t, m, keyMsg("\r")) // → ScreenRepetitions
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Screen() = %q, want %q (test setup failed)", m.Screen(), ScreenRepetitions)
	}
	// Press Enter without typing any digits.
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Done → updateRepetitionsScreen → ScreenReportPath

	// Assert — m.repetitions must still be nil (no override committed by Enter alone).
	if got := m.Repetitions(); got != nil {
		t.Errorf(
			"Repetitions() = *%d after Enter without typing on ScreenRepetitions; "+
				"want nil — pressing Enter without typing must not commit the suite default "+
				"as a user override; nil signals the suite to apply its own default (I2.1)",
			*got,
		)
	}
}

// ---------------------------------------------------------------------------
// I2.1 call-site: MaxConcurrentRunsScreen initial value
// ---------------------------------------------------------------------------

// TestInitSettingScreens_MaxConcurrentRunsScreen_ShowsDefaultBound_WhenNoOverride
// verifies that when the user has not set an explicit max-concurrent-runs override,
// the MaxConcurrentRunsScreen is initialized with suite.DefaultMaxConcurrentRuns
// and displays it before any input. The current bug: initSettingScreens passes 0,
// so the screen shows "0" rather than the documented conservative default.
func TestInitSettingScreens_MaxConcurrentRunsScreen_ShowsDefaultBound_WhenNoOverride(t *testing.T) {
	// Arrange — no user override; m.maxConcurrentRuns is nil.
	m := NewModel(newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))

	// Act — navigate to ScreenMaxConcurrentRuns (Enter x5 from suite-select).
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	for _, want := range []Screen{ScreenRepetitions, ScreenReportPath, ScreenCatalogFolder, ScreenMaxConcurrentRuns} {
		m, _ = safeUpdate(t, m, keyMsg("\r"))
		if m.Screen() != want {
			t.Fatalf("navigation failed: Screen() = %q, want %q", m.Screen(), want)
		}
	}

	// Assert — View must show the default bound (4), not "0".
	wantStr := fmt.Sprintf("%d", suite.DefaultMaxConcurrentRuns)
	view := safeView(t, m)
	if !strings.Contains(view, wantStr) {
		t.Errorf(
			"ScreenMaxConcurrentRuns View() does not contain suite.DefaultMaxConcurrentRuns (%s):\n%s\n"+
				"initSettingScreens must pass suite.DefaultMaxConcurrentRuns to "+
				"NewMaxConcurrentRunsScreen instead of 0 when no user override is set (I2.1)",
			wantStr, view,
		)
	}
}

// ---------------------------------------------------------------------------
// I2.3 call-site: StoreInputScreen initial path
// ---------------------------------------------------------------------------

// TestProcessSelect_StoreReports_InitialPathIsNonEmpty verifies that when the
// user selects "Store Reports" from the process-select screen, the StoreInputScreen
// is initialized with a non-empty initial path (the current working directory).
// The current bug: updateProcessSelect passes "" to NewStoreInputScreen; after
// I2.3 it must pass os.Getwd() (falling back to "." on error).
func TestProcessSelect_StoreReports_InitialPathIsNonEmpty(t *testing.T) {
	// Arrange
	m := NewModel(newProcessOptions())

	// Act — navigate to ScreenStoreInput via mode-select and process-select.
	m = advanceToProcessSelect(t, m) // → ScreenProcessSelect (cursor on "Store Reports")
	m, _ = safeUpdate(t, m, keyMsg("\r")) // Enter on "Store Reports" → ScreenStoreInput
	if m.Screen() != ScreenStoreInput {
		t.Fatalf("Screen() = %q, want %q (test setup failed)", m.Screen(), ScreenStoreInput)
	}

	// Assert — the storeInput screen must have been initialized with a non-empty path.
	if m.processReports.storeInput == nil {
		t.Fatal("processReports.storeInput is nil after navigating to ScreenStoreInput")
	}
	path := m.processReports.storeInput.Path()
	if path == "" {
		t.Errorf(
			"processReports.storeInput.Path() is empty; "+
				"process_reports.go must pass os.Getwd() to NewStoreInputScreen "+
				"instead of \"\" so the screen defaults to the current working directory (I2.3)",
		)
	}
}
