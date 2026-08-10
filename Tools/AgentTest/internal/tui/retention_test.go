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

// TestSuiteSelect_Space_CyclesTheRetentionPolicy drives the chosen toggle
// affordance through a full cycle and back to its start, so the mapping
// from repeated toggles to domain.RetentionPolicy values is pinned exactly.
func TestSuiteSelect_Space_CyclesTheRetentionPolicy(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainNever

	m := NewModel(o)
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("initial Screen() = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}

	m, _ = safeUpdate(t, m, keyMsg(" "))
	if got := m.Retention(); got != domain.RetainOnFailure {
		t.Errorf("Retention() after one toggle = %q, want %q", got, domain.RetainOnFailure)
	}

	m, _ = safeUpdate(t, m, keyMsg(" "))
	if got := m.Retention(); got != domain.RetainAlways {
		t.Errorf("Retention() after two toggles = %q, want %q", got, domain.RetainAlways)
	}

	m, _ = safeUpdate(t, m, keyMsg(" "))
	if got := m.Retention(); got != domain.RetainNever {
		t.Errorf("Retention() after three toggles = %q, want %q (back to the start)", got, domain.RetainNever)
	}
}

// TestSuiteSelect_ToggledRetention_ReachesTheStartedRun is a regression test
// for the gap implementation-review#105 found: the toggle changing what
// Model.Retention() reports and what the screen renders while the value the
// started run actually executed under stayed fixed at construction. It
// exercises the whole path ContractsDesign.md's Retention affordance
// contract names ("The value Model.Retention() holds when the suite starts
// is the suite.Options.Retention the TUI passes. There is no second path.")
// by toggling the policy away from the invocation's starting value and then
// starting the run, asserting on what the SuiteRunner actually received
// rather than only on Model.Retention()'s own fold.
func TestSuiteSelect_ToggledRetention_ReachesTheStartedRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Retention = domain.RetainNever

	m := NewModel(o)

	// Toggle twice: RetainNever -> RetainOnFailure -> RetainAlways. The run
	// must start under RetainAlways, not under the RetainNever the model was
	// constructed with.
	m, _ = safeUpdate(t, m, keyMsg(" "))
	m, _ = safeUpdate(t, m, keyMsg(" "))
	if got := m.Retention(); got != domain.RetainAlways {
		t.Fatalf("Retention() after two toggles = %q, want %q", got, domain.RetainAlways)
	}

	m, cmd := safeUpdate(t, m, keyMsg("\r")) // enter: start the selected suite
	if m.Screen() != ScreenProgress {
		t.Fatalf("Screen() after starting the suite = %q, want %q", m.Screen(), ScreenProgress)
	}
	msg := runCmd(t, cmd)
	if msg == nil {
		t.Fatalf("starting the suite produced no tea.Cmd to await the run")
	}
	safeUpdate(t, m, msg)

	if !runner.called {
		t.Fatalf("SuiteRunner.Run was never called")
	}
	if got := runner.gotRetention; got != domain.RetainAlways {
		t.Errorf("SuiteRunner.Run received retention %q, want %q (the toggled on-screen value at the moment the run started, not Options.Retention)", got, domain.RetainAlways)
	}
}
