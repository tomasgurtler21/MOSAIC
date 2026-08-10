package tui

// Stage 5 (Harness-Agnostic Suites and Selection): the TUI's equivalent
// affordance to the CLI's --harness flag. ContractsDesign.md's tui.Options
// and Model (extended) section adds Options.Harnesses (the shared catalog),
// a new ScreenHarnessSelect member and Model.SelectedHarness(), following
// "the same cursor-and-select pattern as ScreenSuiteSelect" — the shape
// already proven out by the suite-select screen's own navigation tests in
// navigation_test.go.
//
// Package tui (not tui_test), matching navigation_test.go's own choice, so
// these tests can construct a Model already positioned on the harness-select
// screen without needing Update to have implemented the transition into it
// yet.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/preflight"
)

// twoHarnessCatalog is a small, deterministic stand-in for
// mosaic-common/harness's real catalog, so these tests do not depend on its
// membership or ordering.
func twoHarnessCatalog() []commonharness.CLIHarness {
	return []commonharness.CLIHarness{
		{ID: "claude-code", Label: "Claude Code CLI"},
		{ID: "opencode", Label: "OpenCode CLI"},
	}
}

// newHarnessFixtureOptions extends newFixtureOptions with a harness catalog
// and an initial selection, for tests exercising the harness-select screen
// specifically.
func newHarnessFixtureOptions(suites []string, runner *fakeSuiteRunner) Options {
	o := newFixtureOptions(suites, runner)
	o.Harnesses = twoHarnessCatalog()
	o.Harness = "claude-code"
	return o
}

// TestNavigation_InitialScreenIsHarnessSelect_WhenHarnessesOffered is AC5.3:
// the TUI offers an equivalent harness-selection affordance. A frontend
// that never presents it is not offering one, so the entry point — before
// any suite is even chosen — is where a user's first opportunity to change
// the harness must be, exactly as the CLI's --harness is read once, up
// front, before anything runs.
func TestNavigation_InitialScreenIsHarnessSelect_WhenHarnessesOffered(t *testing.T) {
	m := NewModel(newHarnessFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if m.Screen() != ScreenHarnessSelect {
		t.Errorf("initial Screen() = %q, want %q — a harness catalog was offered and must be presented before suite selection", m.Screen(), ScreenHarnessSelect)
	}
}

// TestNavigation_HarnessSelect_DownEnter_MovesToSuiteSelectAndRecordsSelection
// drives the harness-select screen's cursor-and-select pattern — down to the
// catalog's second entry, then select — and requires both the screen
// transition and the recorded selection: SelectedHarness() must reflect the
// entry the cursor was actually on, not the initial Options.Harness.
func TestNavigation_HarnessSelect_DownEnter_MovesToSuiteSelectAndRecordsSelection(t *testing.T) {
	catalog := twoHarnessCatalog()
	m := NewModel(newHarnessFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner()))
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r"))

	if m.Screen() != ScreenSuiteSelect {
		t.Errorf("Screen() after down+enter on harness-select = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	if got, want := m.SelectedHarness(), catalog[1].ID; got != want {
		t.Errorf("SelectedHarness() = %q, want %q (the cursor's entry after moving down once)", got, want)
	}
}

// TestHarnessSelect_SelectionReachesPreflightInput is AC5.3's "and the
// selection reaches the run" half: once a harness is chosen on the
// harness-select screen, starting a suite must preflight with that
// selection — not silently fall back to the initial Options.Harness the
// invocation started with.
func TestHarnessSelect_SelectionReachesPreflightInput(t *testing.T) {
	catalog := twoHarnessCatalog()

	var captured preflight.Input
	o := newHarnessFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Preflight = func(in preflight.Input) (preflight.Plan, authoring.Report) {
		captured = in
		return fixturePlan("suite-under-test"), authoring.Report{}
	}

	m := NewModel(o)
	if m.Screen() != ScreenHarnessSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenHarnessSelect)
	}

	// Select the catalog's second entry ("opencode"), then the offered
	// suite.
	m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	m, _ = safeUpdate(t, m, keyMsg("\r"))    // choose the harness
	m, cmd := safeUpdate(t, m, keyMsg("\r")) // choose the suite
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if captured.HarnessID != catalog[1].ID {
		t.Errorf("preflight.Input.HarnessID = %q, want %q (the harness-select screen's own choice, not the initial default)", captured.HarnessID, catalog[1].ID)
	}
}
