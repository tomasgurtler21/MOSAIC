package tui

// repetitions_test.go covers the TUI repetitions override feature (Stage 3):
//
//   T3.2 (TUI) - The Model gains a repetitions *int field and a Repetitions()
//                accessor. When a repetitions override is set on the model,
//                it reaches preflight.Input.Overrides.Repetitions when a suite
//                starts, following the same threading pattern as SubjectModel
//                and StubModel in startSelectedSuite(). The suite-select screen
//                renders an indication of the current repetitions setting so
//                the user can see the effective value before starting a run.
//
// All tests are in package tui (not tui_test) so they can access the Model's
// unexported fields and use the shared helpers from testhelpers_test.go.
//
// NOTE: These tests are in TDD RED phase. They reference Model.repetitions
// (unexported field) and Model.Repetitions() (accessor) which do not yet exist
// in the implementation. They will not compile against the current code. That
// is the expected RED state — they specify the target behaviour for
// implementation tasks I3.3 and I3.4.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// intPtr is a helper that returns a pointer to v, so tests can express
// pointer-typed override values concisely.
func intPtr(v int) *int { return &v }

// ---------------------------------------------------------------------------
// Repetitions accessor
// ---------------------------------------------------------------------------

// TestNewModel_Repetitions_StartsAsNil verifies that a freshly constructed
// Model has no repetitions override set — the suite's authored default applies
// until the user explicitly configures an override, mirroring how SubjectModel
// and StubModel start as the empty string.
func TestNewModel_Repetitions_StartsAsNil(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner()))
	if got := m.Repetitions(); got != nil {
		t.Errorf("Repetitions() = %v (non-nil); want nil on a freshly constructed Model (no override set)", got)
	}
}

// TestModel_Repetitions_WhenSet_ReturnsValue verifies that after the model's
// repetitions field is set to a value, the Repetitions() accessor reflects it.
func TestModel_Repetitions_WhenSet_ReturnsValue(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner()))
	want := 3
	m.repetitions = intPtr(want)

	got := m.Repetitions()
	if got == nil {
		t.Fatalf("Repetitions() = nil after setting repetitions to %d; want *%d", want, want)
	}
	if *got != want {
		t.Errorf("Repetitions() = %d, want %d", *got, want)
	}
}

// ---------------------------------------------------------------------------
// T3.2: repetitions wired through preflight.Overrides
// ---------------------------------------------------------------------------

// TestSuiteSelect_Repetitions_NilWhenNotSet_ReachesPreflightOverrides verifies
// that when no repetitions override is set on the Model (the default), the
// preflight input receives a nil Repetitions override, leaving the suite's
// authored default in effect. This mirrors the behaviour of SubjectModel
// (empty string) and StubModel (empty string) when not configured.
func TestSuiteSelect_Repetitions_NilWhenNotSet_ReachesPreflightOverrides(t *testing.T) {
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)
	// repetitions is deliberately not set; it must be nil by default.

	// Navigate through the full settings flow without typing a repetitions value
	// so the screen keeps the default (0 = no override) and m.repetitions stays nil.
	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.Repetitions != nil {
		t.Errorf("preflight.Input.Overrides.Repetitions = %v, want nil (no repetitions override configured)", captured.Overrides.Repetitions)
	}
}

// TestSuiteSelect_Repetitions_WhenSet_ReachesPreflightOverrides verifies that
// when a repetitions value is configured on the Model, it reaches
// preflight.Input.Overrides.Repetitions when a suite starts, following the
// same threading pattern as SubjectModel/StubModel in startSelectedSuite().
func TestSuiteSelect_Repetitions_WhenSet_ReachesPreflightOverrides(t *testing.T) {
	wantReps := 5
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)
	m.repetitions = intPtr(wantReps)

	// Navigate through the full settings flow; repetitions screen is pre-loaded
	// with the configured override and confirms it on Enter.
	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.Repetitions == nil {
		t.Fatalf("preflight.Input.Overrides.Repetitions = nil; want %d (the override that was set on the Model)", wantReps)
	}
	if *captured.Overrides.Repetitions != wantReps {
		t.Errorf("preflight.Input.Overrides.Repetitions = %d, want %d", *captured.Overrides.Repetitions, wantReps)
	}
}

// TestSuiteSelect_Repetitions_OverridePreservesOtherOverrides verifies that
// wiring the repetitions override into preflight.Overrides does not clobber
// SubjectModel or StubModel — all three travel through startSelectedSuite()
// simultaneously and must all be present in the captured input.
func TestSuiteSelect_Repetitions_OverridePreservesOtherOverrides(t *testing.T) {
	wantReps := 2
	wantSubject := "claude-opus-4-5"
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)
	m.repetitions = intPtr(wantReps)
	m.selectedSubjectModel = wantSubject

	m, cmd := startSuiteFromSuiteSelect(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.Repetitions == nil || *captured.Overrides.Repetitions != wantReps {
		t.Errorf("preflight.Input.Overrides.Repetitions = %v, want %d", captured.Overrides.Repetitions, wantReps)
	}
	if captured.Overrides.SubjectModel != wantSubject {
		t.Errorf("preflight.Input.Overrides.SubjectModel = %q, want %q (must not be clobbered when Repetitions is set)", captured.Overrides.SubjectModel, wantSubject)
	}
}

// ---------------------------------------------------------------------------
// T3.2: repetitions displayed on suite-select screen
// ---------------------------------------------------------------------------

// TestSettings_Repetitions_DisplayedOnScreen verifies that ScreenRepetitions
// renders some indication of the repetitions setting, so the user can see the
// effective count after navigating into the settings flow.
func TestSettings_Repetitions_DisplayedOnScreen(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner()))

	// Navigate to ScreenRepetitions: Enter (SuiteSelect→Retention) then Enter (Retention→Repetitions).
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Screen() after Enter on ScreenRetention = %q, want %q", m.Screen(), ScreenRepetitions)
	}

	view := safeView(t, m)
	viewLower := strings.ToLower(view)

	hasRepetitionDisplay := strings.Contains(viewLower, "repetition") ||
		strings.Contains(viewLower, " reps") ||
		strings.Contains(viewLower, "repeat") ||
		strings.Contains(viewLower, "×") ||
		strings.Contains(viewLower, "times")
	if !hasRepetitionDisplay {
		t.Errorf("ScreenRepetitions View() does not mention repetitions:\n%s", view)
	}
}

// TestSettings_Repetitions_OverrideValue_DisplayedOnScreen verifies that
// when a repetitions override is set on the Model, its numeric value appears
// in ScreenRepetitions' rendered output.
func TestSettings_Repetitions_OverrideValue_DisplayedOnScreen(t *testing.T) {
	m := NewModel(newFixtureOptions([]string{"suite.yaml"}, newFakeSuiteRunner()))
	m.repetitions = intPtr(7)

	// Navigate to ScreenRepetitions.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Screen() after Enter on ScreenRetention = %q, want %q", m.Screen(), ScreenRepetitions)
	}

	view := safeView(t, m)
	if !strings.Contains(view, "7") {
		t.Errorf("ScreenRepetitions View() does not display the repetitions override value 7:\n%s", view)
	}
}
