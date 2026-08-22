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

	tea "github.com/charmbracelet/bubbletea"

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

// TestSettings_Space_CyclesTheRetentionPolicy verifies that the retention
// entry on the settings screen cycles through the full sequence
// Never → OnFailure → Always → Never when Enter is pressed repeatedly.
// This replaces the old Space-on-suite-select affordance with the uniform
// settings-screen interaction model.
func TestSettings_Space_CyclesTheRetentionPolicy(t *testing.T) {
	o := newFixtureOptions([]string{"suite-a.yaml"}, newFakeSuiteRunner())
	o.Retention = domain.RetainNever

	m := NewModel(o)

	// Navigate to settings.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() after Tab = %q, want %q", m.Screen(), ScreenSettings)
	}

	// Retention is the first entry; cursor starts at 0.
	entries := m.SettingsEntries()
	retIdx := -1
	for i, e := range entries {
		if e.Kind == SettingRetention {
			retIdx = i
			break
		}
	}
	if retIdx < 0 {
		t.Fatalf("SettingsEntries() does not include SettingRetention")
	}
	for i := 0; i < retIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	if got := m.Retention(); got != domain.RetainOnFailure {
		t.Errorf("Retention() after one Enter on retention entry = %q, want %q", got, domain.RetainOnFailure)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	if got := m.Retention(); got != domain.RetainAlways {
		t.Errorf("Retention() after two Enters = %q, want %q", got, domain.RetainAlways)
	}

	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter))
	if got := m.Retention(); got != domain.RetainNever {
		t.Errorf("Retention() after three Enters = %q, want %q (back to start)", got, domain.RetainNever)
	}
}

// TestSettings_ToggledRetention_ReachesTheStartedRun verifies that a
// retention value changed via the settings screen reaches the SuiteRunner
// when the run starts from suite-select. This is the equivalent of the old
// Space-toggle regression test, exercised through the new settings path.
func TestSettings_ToggledRetention_ReachesTheStartedRun(t *testing.T) {
	runner := newFakeSuiteRunner()
	o := newFixtureOptions([]string{"suite-a.yaml"}, runner)
	o.Retention = domain.RetainNever

	m := NewModel(o)

	// Navigate to settings and cycle retention twice:
	// RetainNever → RetainOnFailure → RetainAlways.
	m, _ = safeUpdate(t, m, keyType(tea.KeyTab))
	if m.Screen() != ScreenSettings {
		t.Fatalf("Screen() after Tab = %q, want %q", m.Screen(), ScreenSettings)
	}
	// Find retention entry.
	entries := m.SettingsEntries()
	retIdx := -1
	for i, e := range entries {
		if e.Kind == SettingRetention {
			retIdx = i
			break
		}
	}
	if retIdx < 0 {
		t.Fatalf("SettingsEntries() does not include SettingRetention")
	}
	for i := 0; i < retIdx; i++ {
		m, _ = safeUpdate(t, m, keyType(tea.KeyDown))
	}
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter)) // Never → OnFailure
	m, _ = safeUpdate(t, m, keyType(tea.KeyEnter)) // OnFailure → Always
	if got := m.Retention(); got != domain.RetainAlways {
		t.Fatalf("Retention() after two cycles = %q, want %q", got, domain.RetainAlways)
	}

	// Return to suite-select and start the run.
	m, _ = safeUpdate(t, m, keyType(tea.KeyEsc))
	if m.Screen() != ScreenSuiteSelect {
		t.Fatalf("Screen() after Esc = %q, want %q", m.Screen(), ScreenSuiteSelect)
	}
	m, cmd := safeUpdate(t, m, keyMsg("\r"))
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
		t.Errorf("SuiteRunner.Run received retention %q, want %q (the value cycled on the settings screen must reach the run)", got, domain.RetainAlways)
	}
}
