package tui

// effective_defaults_test.go covers Stage 10's effective-defaults feature
// (T10.1): the repetitions setting shows the selected suite's authored default
// as a number rather than the generic "suite default" label, marks whether the
// shown value is that default or a user override, and updates as the selection
// moves between suites with different declared defaults.
//
// All tests reference Options.ResolveSuiteDefaults and SuiteDefaults, which
// are declared by Stage 10's contract files (defaults.go, app.go). The
// current implementation does not call the resolver or carry provenance
// markers, so these tests define the RED-phase target for implementation
// tasks I10.1 and related changes to SettingsEntries().
//
// Success criteria pinned here:
//   AC10.1 - Repetitions shows the number that will actually be used
//   AC10.2 - Provenance (authored default vs user override) is clear
//   AC10.3 - The value tracks the current suite selection
//   AC10.6 - Resolved values agree with the settings-layering chain

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// testResolver returns a ResolveSuiteDefaults function that returns known
// defaults for the given suite paths and an error for any unrecognised path.
// Tests supply only the paths they actually exercise, so an unexpected path
// produces a clear failure rather than silently returning a zero SuiteDefaults.
func testResolver(defaults map[string]SuiteDefaults) func(string) (SuiteDefaults, error) {
	return func(suitePath string) (SuiteDefaults, error) {
		d, ok := defaults[suitePath]
		if !ok {
			return SuiteDefaults{}, fmt.Errorf("testResolver: no defaults registered for suite path %q", suitePath)
		}
		return d, nil
	}
}

// alwaysErrorResolver returns a ResolveSuiteDefaults function that always
// fails, so tests can exercise the resolver-error degradation path without
// depending on file I/O.
func alwaysErrorResolver(msg string) func(string) (SuiteDefaults, error) {
	return func(string) (SuiteDefaults, error) {
		return SuiteDefaults{}, fmt.Errorf("alwaysErrorResolver: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// Resolver nil / zero-value path (graceful degradation)
// ---------------------------------------------------------------------------

// TestSettingsEntries_Repetitions_ResolverNil_NoPanic verifies that when
// Options.ResolveSuiteDefaults is nil (the zero value for a function field),
// SettingsEntries() and View() do not panic, and the repetitions entry still
// produces a non-empty Display. The implementation must degrade gracefully to
// the previous "suite default" text rather than crashing on a nil function.
func TestSettingsEntries_Repetitions_ResolverNil_NoPanic(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	// ResolveSuiteDefaults deliberately not set (nil).
	m := NewModel(o)

	var entries []SettingsEntry
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SettingsEntries() panicked with nil ResolveSuiteDefaults: %v", r)
			}
		}()
		entries = m.SettingsEntries()
	}()

	for _, e := range entries {
		if e.Kind == SettingRepetitions {
			if e.Display == "" {
				t.Errorf("SettingsEntry{Kind: %q}.Display is empty when resolver is nil; want a non-empty fallback label", e.Kind)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() produced no SettingRepetitions entry")
}

// TestSettings_Repetitions_ResolverNil_ViewDoesNotPanic verifies that the
// repetitions screen renders without panic even when no resolver is configured.
func TestSettings_Repetitions_ResolverNil_ViewDoesNotPanic(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	m := NewModel(o)
	// Navigate to ScreenRepetitions: Enter×2 from SuiteSelect.
	m = advanceToSettingsFlow(t, m) // → ScreenRetention
	m, _ = safeUpdate(t, m, keyMsg("\r"))
	if m.Screen() != ScreenRepetitions {
		t.Fatalf("Screen() = %q, want %q (test setup failed)", m.Screen(), ScreenRepetitions)
	}
	view := safeView(t, m)
	if view == "" {
		t.Errorf("ScreenRepetitions View() returned empty string with nil resolver; want non-empty output")
	}
}

// ---------------------------------------------------------------------------
// AC10.1 — Display shows the actual suite-default number, not a generic label
// ---------------------------------------------------------------------------

// TestSettingsEntries_Repetitions_ResolverReturnsDefault_ShowsNumber verifies
// that when ResolveSuiteDefaults returns a suite default of N and no user
// override is set, the repetitions entry's Display contains the number N. The
// current implementation shows only the generic "suite default" label; this
// test specifies that the actual default (e.g., "3") must appear instead.
func TestSettingsEntries_Repetitions_ResolverReturnsDefault_ShowsNumber(t *testing.T) {
	suiteDefault := 3
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(suiteDefault)},
	})
	m := NewModel(o)
	// No user override set; the suite's authored default (3) should show.

	entries := m.SettingsEntries()
	for _, e := range entries {
		if e.Kind == SettingRepetitions {
			if !strings.Contains(e.Display, "3") {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want it to contain the suite default %d (the actual number the run will use, not just the label 'suite default')", e.Kind, e.Display, suiteDefault)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() produced no SettingRepetitions entry")
}

// TestSettingsEntries_Repetitions_SuiteDeclaresNoDefault_DegradesToLabel
// verifies that when ResolveSuiteDefaults returns SuiteDefaults{Repetitions:
// nil} (the suite file declares no repetitions default), the entry degrades
// gracefully to a non-empty label. It must not show a raw "0" (zero is not a
// valid repetitions count and would mislead the user).
func TestSettingsEntries_Repetitions_SuiteDeclaresNoDefault_DegradesToLabel(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: nil}, // suite declares no default
	})
	m := NewModel(o)

	entries := m.SettingsEntries()
	for _, e := range entries {
		if e.Kind == SettingRepetitions {
			if e.Display == "" {
				t.Errorf("SettingsEntry{Kind: %q}.Display is empty when suite declares no default; want a non-empty fallback label", e.Kind)
			}
			if e.Display == "0" {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; must not show '0' when the suite declares no repetitions default (would mislead the user into thinking zero tests run)", e.Kind, e.Display)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() produced no SettingRepetitions entry")
}

// TestSettingsEntries_Repetitions_ResolverError_DegradesToUnknown verifies
// that when ResolveSuiteDefaults returns an error, the repetitions entry
// degrades to an "unknown" or fallback display rather than blocking or
// panicking. Navigation must remain functional after a parse failure
// (AC10.2's "degrade to ProvenanceUnknown rather than blocking").
func TestSettingsEntries_Repetitions_ResolverError_DegradesToUnknown(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = alwaysErrorResolver("suite file is malformed")
	m := NewModel(o)

	var entries []SettingsEntry
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SettingsEntries() panicked after resolver error: %v", r)
			}
		}()
		entries = m.SettingsEntries()
	}()

	for _, e := range entries {
		if e.Kind == SettingRepetitions {
			if e.Display == "" {
				t.Errorf("SettingsEntry{Kind: %q}.Display is empty after resolver error; want an 'unknown' or fallback label", e.Kind)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() produced no SettingRepetitions entry after resolver error")
}

// ---------------------------------------------------------------------------
// AC10.2 — Provenance marker distinguishes suite default from user override
// ---------------------------------------------------------------------------

// TestSettingsEntries_Repetitions_SuiteDefault_ShowsDefaultProvenance verifies
// that when the displayed value comes from the suite file (no user override set),
// the entry's Display includes a visual marker that identifies the source as a
// suite default (e.g., "(suite default)", "(default)", "default:"). The current
// implementation shows only a number with no provenance context.
func TestSettingsEntries_Repetitions_SuiteDefault_ShowsDefaultProvenance(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(5)},
	})
	m := NewModel(o)
	// No user override.

	entries := m.SettingsEntries()
	for _, e := range entries {
		if e.Kind == SettingRepetitions {
			lower := strings.ToLower(e.Display)
			hasDefaultMarker := strings.Contains(lower, "default") || strings.Contains(lower, "suite")
			if !hasDefaultMarker {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want a 'default' or 'suite' provenance marker so the user can tell the number comes from the suite file, not a personal override", e.Kind, e.Display)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() produced no SettingRepetitions entry")
}

// TestSettingsEntries_Repetitions_UserOverride_ShowsOverrideValue verifies
// that when a user override is set, the entry's Display shows the override
// value (not the suite's authored default). This is a regression guard: the
// override value must take priority over whatever the suite file declares.
func TestSettingsEntries_Repetitions_UserOverride_ShowsOverrideValue(t *testing.T) {
	suiteDefault := 3
	userOverride := 9
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(suiteDefault)},
	})
	m := NewModel(o)
	m.repetitions = intPtr(userOverride) // user explicitly set an override

	entries := m.SettingsEntries()
	for _, e := range entries {
		if e.Kind == SettingRepetitions {
			if !strings.Contains(e.Display, "9") {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want it to contain the user override %d (the override must take priority over the suite default %d)", e.Kind, e.Display, userOverride, suiteDefault)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() produced no SettingRepetitions entry")
}

// TestSettingsEntries_Repetitions_UserOverride_ShowsOverrideProvenance verifies
// that when a user override is set, the entry's Display includes a visual
// marker identifying the source as a user override (e.g., "(override)", "(user
// override)", "override:"). The current implementation shows only the plain
// number with no provenance context, so this test defines the desired behavior.
func TestSettingsEntries_Repetitions_UserOverride_ShowsOverrideProvenance(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(3)},
	})
	m := NewModel(o)
	m.repetitions = intPtr(7) // user override

	entries := m.SettingsEntries()
	for _, e := range entries {
		if e.Kind == SettingRepetitions {
			lower := strings.ToLower(e.Display)
			hasOverrideMarker := strings.Contains(lower, "override") || strings.Contains(lower, "user")
			if !hasOverrideMarker {
				t.Errorf("SettingsEntry{Kind: %q}.Display = %q; want an 'override' or 'user' provenance marker so the user can distinguish their own setting from a suite default", e.Kind, e.Display)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() produced no SettingRepetitions entry")
}

// TestSettingsEntries_Repetitions_OverrideAndSuiteDefault_DisplayDiffer
// verifies that the Display for an override and the Display for the same
// numeric value coming from the suite default are visually distinct. A user
// must be able to tell which source is in effect at a glance.
func TestSettingsEntries_Repetitions_OverrideAndSuiteDefault_DisplayDiffer(t *testing.T) {
	// Both the suite default and the user override are 5 — same number,
	// but the provenance must differ so the labels are not identical.
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(5)},
	})

	// With suite default.
	mDefault := NewModel(o)
	var defaultDisplay string
	for _, e := range mDefault.SettingsEntries() {
		if e.Kind == SettingRepetitions {
			defaultDisplay = e.Display
			break
		}
	}

	// With user override of the same numeric value.
	mOverride := NewModel(o)
	mOverride.repetitions = intPtr(5)
	var overrideDisplay string
	for _, e := range mOverride.SettingsEntries() {
		if e.Kind == SettingRepetitions {
			overrideDisplay = e.Display
			break
		}
	}

	if defaultDisplay == "" || overrideDisplay == "" {
		t.Fatalf("SettingsEntries() missing SettingRepetitions entry (default=%q, override=%q)", defaultDisplay, overrideDisplay)
	}
	if defaultDisplay == overrideDisplay {
		t.Errorf("SettingsEntry{Kind: %q}.Display is identical (%q) for suite-default 5 and user-override 5; the provenance marker must make them visually distinct", SettingRepetitions, defaultDisplay)
	}
}

// ---------------------------------------------------------------------------
// AC10.3 — Display tracks the current suite selection
// ---------------------------------------------------------------------------

// TestSuiteNavigation_MovingCursor_UpdatesRepetitionsDefault verifies that
// when the suite cursor moves from one suite to another, and each suite has a
// different authored default, the repetitions entry's Display updates to reflect
// the newly selected suite's default. The current implementation never calls
// the resolver, so this test fails until the selection-change path is wired.
func TestSuiteNavigation_MovingCursor_UpdatesRepetitionsDefault(t *testing.T) {
	suiteADefault := 3
	suiteBDefault := 7
	o := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(suiteADefault)},
		"suite-b.yaml": {Repetitions: intPtr(suiteBDefault)},
	})
	m := NewModel(o)

	// Initial selection is suite-a (cursor=0). Verify its default (3) is shown.
	var displayBeforeMove string
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingRepetitions {
			displayBeforeMove = e.Display
			break
		}
	}
	if !strings.Contains(displayBeforeMove, "3") {
		t.Errorf("initial repetitions Display = %q; want it to contain suite-a's default (%d) before any cursor movement", displayBeforeMove, suiteADefault)
	}

	// Move cursor to suite-b (cursor=1). Default must update to 7.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))

	var displayAfterMove string
	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingRepetitions {
			displayAfterMove = e.Display
			break
		}
	}
	if !strings.Contains(displayAfterMove, "7") {
		t.Errorf("repetitions Display after cursor move = %q; want it to contain suite-b's default (%d) after moving to suite-b", displayAfterMove, suiteBDefault)
	}

	// Sanity: the two displays must differ (resolution happened).
	if displayBeforeMove == displayAfterMove {
		t.Errorf("repetitions Display did not change (%q) after moving from suite-a (default=%d) to suite-b (default=%d); the resolver must be re-called on selection change", displayBeforeMove, suiteADefault, suiteBDefault)
	}
}

// TestSuiteNavigation_MovingCursorBack_RestoresPreviousSuiteDefault verifies
// that when the cursor moves away from suite-a and then back, suite-a's default
// is restored in the display. The resolver must be called again for the
// re-selected suite, not just cached from the initial load.
func TestSuiteNavigation_MovingCursorBack_RestoresPreviousSuiteDefault(t *testing.T) {
	suiteADefault := 2
	suiteBDefault := 8
	o := newFixtureOptions([]string{"suite-a.yaml", "suite-b.yaml"}, newFakeSuiteRunner())
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(suiteADefault)},
		"suite-b.yaml": {Repetitions: intPtr(suiteBDefault)},
	})
	m := NewModel(o)

	// Move to suite-b.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	// Move back to suite-a.
	m, _ = safeUpdate(t, m, keyType(tea.KeyUp))

	for _, e := range m.SettingsEntries() {
		if e.Kind == SettingRepetitions {
			if !strings.Contains(e.Display, "2") {
				t.Errorf("repetitions Display after returning to suite-a = %q; want it to contain suite-a's default (%d) after navigating back", e.Display, suiteADefault)
			}
			return
		}
	}
	t.Errorf("SettingsEntries() produced no SettingRepetitions entry")
}

// ---------------------------------------------------------------------------
// AC10.6 — Resolved value agrees with the settings-layering chain
// ---------------------------------------------------------------------------

// TestStartSuite_SuiteDefault_NoUserOverride_PreflightGetsNilRepetitions
// verifies that when the repetitions display shows the suite's authored
// default and no user override is set, the preflight call receives nil
// Repetitions. Preflight then applies the suite default through its own
// layering chain, ensuring the displayed value and the executed value agree.
func TestStartSuite_SuiteDefault_NoUserOverride_PreflightGetsNilRepetitions(t *testing.T) {
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(4)},
	})
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)
	// No user override: m.repetitions is nil.

	m, cmd := startSuiteFromSuiteSelect(t, m)
	_ = m
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.Repetitions != nil {
		t.Errorf("preflight.Input.Overrides.Repetitions = %v; want nil when showing the suite default (preflight must apply the suite file's own default through its layering chain, not a forced value from the TUI)", captured.Overrides.Repetitions)
	}
}

// TestStartSuite_UserOverride_PreflightGetsOverrideValue verifies that when
// the user has explicitly set a repetitions override in the TUI (which the
// display marks as ProvenanceUserOverride), that value reaches
// preflight.Overrides.Repetitions, overriding whatever the suite file declares.
// This is a regression guard for the threading path that already exists.
func TestStartSuite_UserOverride_PreflightGetsOverrideValue(t *testing.T) {
	wantReps := 6
	var captured preflight.Input
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.ResolveSuiteDefaults = testResolver(map[string]SuiteDefaults{
		"suite-a.yaml": {Repetitions: intPtr(2)}, // suite default differs from user override
	})
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}
	m := NewModel(o)
	m.repetitions = intPtr(wantReps) // user override

	m, cmd := startSuiteFromSuiteSelect(t, m)
	_ = m
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.Overrides.Repetitions == nil {
		t.Fatalf("preflight.Input.Overrides.Repetitions = nil; want *%d (user override must reach preflight even when a suite default is available)", wantReps)
	}
	if *captured.Overrides.Repetitions != wantReps {
		t.Errorf("preflight.Input.Overrides.Repetitions = %d, want %d", *captured.Overrides.Repetitions, wantReps)
	}
}
