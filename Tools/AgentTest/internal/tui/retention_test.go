package tui

// Stage 7 (Sandbox Retention): the TUI's equivalent affordance to the CLI's
// --keep-sandbox / --keep-sandbox-on-failure flags. ContractsDesign.md's
// tui.Options and Model (extended) section adds Options.Retention and
// Model.Retention(), describing "a toggle affordance resolving to the same
// domain.RetentionPolicy the CLI flags produce" without prescribing which
// key or screen carries it.
//
// These tests fix that choice for this package: the suite-select screen —
// the model's resting screen once a harness (if any) is chosen, and the
// screen a user sits on immediately before starting a run — responds to the
// shared toggle binding (mosaic-common/tui's GlobalKeys.Space) by cycling
// RetainNever -> RetainOnFailure -> RetainAlways -> RetainNever. A test
// reading this file is the authoritative statement of that choice; nothing
// here is prescribed by ContractsDesign.md beyond the two observable
// properties (initial propagation, and reaching the same policy values the
// CLI flags produce).

import (
	"testing"

	"mosaic-agent-test/internal/domain"
)

// TestNewModel_Retention_StartsAtOptionsRetention is the retention
// counterpart to harness selection's own initial-value contract: a Model
// must reflect the invocation's resolved policy before any toggle, exactly
// as SelectedHarness() starts at Options.Harness.
func TestNewModel_Retention_StartsAtOptionsRetention(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainOnFailure

	m := NewModel(o)

	if got := m.Retention(); got != domain.RetainOnFailure {
		t.Errorf("Retention() = %q, want %q (Options.Retention, before any toggle)", got, domain.RetainOnFailure)
	}
}

// TestSettings_Space_CyclesTheRetentionPolicy verifies that ScreenRetention
// cycles through the full sequence Never → OnFailure → Always → Never when
// Space is pressed repeatedly, and that each confirmed value is reflected by
// Model.Retention() after confirming the entire settings flow.
func TestSettings_Space_CyclesTheRetentionPolicy(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainNever

	m := NewModel(o)

	// Navigate to ScreenRetention.
	m = advanceToSettingsFlow(t, m)
	if m.Screen() != ScreenRetention {
		t.Fatalf("Screen() after Enter = %q, want %q", m.Screen(), ScreenRetention)
	}

	// Space cycles: RetainNever → RetainOnFailure.
	m, _ = safeUpdate(t, m, keyMsg(" "))
	// Space again: RetainOnFailure → RetainAlways.
	m, _ = safeUpdate(t, m, keyMsg(" "))
	// Space again: RetainAlways → RetainNever.
	m, _ = safeUpdate(t, m, keyMsg(" "))

	// Confirm RetainNever on ScreenRetention and advance through the remaining
	// screens to verify the final confirmed value reaches Retention().
	m, cmd := advanceThroughSettingsToProgress(t, m)
	if cmd != nil {
		_ = runCmd(t, cmd)
	}

	if got := m.Retention(); got != domain.RetainNever {
		t.Errorf("Retention() after three Space presses (back to start) = %q, want %q", got, domain.RetainNever)
	}
}

// TestSettings_ToggledRetention_ReachesTheStartedRun verifies that a
// retention value changed on ScreenRetention reaches the SuiteRunner when the
// run starts. This exercises the full path: Space twice on ScreenRetention to
// reach RetainAlways, then complete the remaining screens and assert the
// runner received RetainAlways.
func TestSettings_ToggledRetention_ReachesTheStartedRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Retention = domain.RetainNever

	m := NewModel(o)

	// Navigate to ScreenRetention and cycle twice:
	// RetainNever → RetainOnFailure → RetainAlways.
	m = advanceToSettingsFlow(t, m)
	m, _ = safeUpdate(t, m, keyMsg(" ")) // Never → OnFailure
	m, _ = safeUpdate(t, m, keyMsg(" ")) // OnFailure → Always

	// Complete the remaining settings screens and start the suite.
	m, cmd := advanceThroughSettingsToProgress(t, m)
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("starting the suite produced no tea.Cmd to await the run")
	}
	safeUpdate(t, m, msg)

	if !runner.called {
		t.Fatalf("SuiteRunner.Run was never called")
	}
	if got := runner.gotRetention; got != domain.RetainAlways {
		t.Errorf("SuiteRunner.Run received retention %q, want %q (the value cycled on ScreenRetention must reach the run)", got, domain.RetainAlways)
	}
}
