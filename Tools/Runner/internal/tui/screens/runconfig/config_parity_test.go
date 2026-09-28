package runconfig

// config_parity_test.go verifies ConfigScreen settings parity with CLI flags and manual-resolution behavior.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Stage 8 — T8.4: Surface parity tests
//
// The wizard's ConfigSelection.Settings must be identical to what the CLI
// produces from the equivalent flags, so surface parity is a struct comparison.
// ---------------------------------------------------------------------------

// TestConfigScreen_Parity_OrchestratedMode_SettingsMatchCLIEquivalent verifies
// that completing the wizard with mode=orchestrated, checkpoints=disabled,
// commits=absent, pre-consult=absent, manual-resolution=disabled produces a
// Settings struct equivalent to what the CLI would produce from:
//   --mode orchestrated
//
// RED: Settings remains zero-valued (ExecutionModeUnset for Mode, false for all bools).
func TestConfigScreen_Parity_OrchestratedMode_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	// Drive through all steps with the simplest configuration:
	// mode=orchestrated, no commit agent, all other steps use defaults (disabled/no).
	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift: no drift (default)
	pressKey(s, tea.KeyEnter)          // checkpoints: disabled (default)
	// No commits step (no commit agent).
	// No pre-consult step (orchestrated mode).
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // manual resolution: disabled (default)
	}
	// No infra class step (no multiple same-class agents).

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all steps")
	}

	// Equivalent CLI configuration for --mode orchestrated (all else default).
	// When commits are disabled, CommitBranchVariant must be the zero value
	// (empty string), not mosaic-owned. The mosaic-owned default applies only
	// when commits are enabled.
	// RED: current implementation pre-populates CommitBranchMOSAICOwned unconditionally.
	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeOrchestrated,
		Checkpoints:         false,
		Commits:             false,
		CommitBranchVariant: "", // zero value: commits disabled
		PreConsultation:     false,
		ManualResolution:    false,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v\n"+
			"ConfigSelection.Settings must equal the RunSettings the CLI produces from the equivalent flags",
			got, wantSettings)
	}
}

// TestConfigScreen_Parity_AutoModeWithPreConsult_SettingsMatchCLIEquivalent
// verifies that selecting mode=auto and enabling pre-consultation produces
// Settings equivalent to:
//   --mode auto --pre-consult
//
// RED: Settings.Mode is ExecutionModeUnset, Settings.PreConsultation is false.
func TestConfigScreen_Parity_AutoModeWithPreConsult_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// Pre-consult step should appear for mode=auto; enable it.
	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	if s.step == configStepPreConsult {
		pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // select Enabled
	}

	// Manual resolution step (if present): leave disabled.
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // disabled (default cursor position)
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all steps")
	}

	// When commits are disabled, CommitBranchVariant must be the zero value
	// (empty string). The mosaic-owned default applies only when commits are enabled.
	// RED: current implementation pre-populates CommitBranchMOSAICOwned unconditionally.
	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeAuto,
		Checkpoints:         false,
		Commits:             false,
		CommitBranchVariant: "", // zero value: commits disabled
		PreConsultation:     true,
		ManualResolution:    false,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v",
			got, wantSettings)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Manual-resolution step
//
// The manual-resolution step is always shown after the pre-consult step
// sequence. It presents enabled and disabled options with disabled as the
// default.
// ---------------------------------------------------------------------------

// TestConfigScreen_ManualResolutionStep_ViewShowsOptions verifies that when the
// wizard reaches configStepManualResolution, the view shows both enabled and
// disabled options so the user can make an explicit choice.
//
// RED: configStepManualResolution is not wired into advance(); the step is
// never reached.
func TestConfigScreen_ManualResolutionStep_ViewShowsOptions(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil) // no commit agent; orchestrated mode skips commits and pre-consult

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v after checkpoints (orchestrated, no commit agent), want configStepManualResolution (%v); "+
			"cannot verify view without reaching the step", s.step, configStepManualResolution)
	}

	view := s.View()
	// The step must present both options so the user can choose.
	if !containsSubstr(view, "enabled") && !containsSubstr(view, "Enabled") {
		t.Errorf("manual-resolution step view does not contain an enabled option; view:\n%s", view)
	}
	if !containsSubstr(view, "disabled") && !containsSubstr(view, "Disabled") {
		t.Errorf("manual-resolution step view does not contain a disabled option; view:\n%s", view)
	}
}

// TestConfigScreen_ManualResolutionStep_EnabledSetsManualResolutionTrue verifies
// that selecting the enabled option on the manual-resolution step results in
// Settings.ManualResolution == true after the wizard completes.
//
// RED: configStepManualResolution is not wired in; Settings.ManualResolution
// remains false regardless of input.
func TestConfigScreen_ManualResolutionStep_EnabledSetsManualResolutionTrue(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v, want configStepManualResolution; cannot test selection", s.step)
	}

	// Select the enabled option. Convention: disabled is the default preselection at
	// cursor 0 (ContractsDesign.md: "configStepManualResolution | always | disabled").
	// Enabled is at cursor 1; one Down press moves to it before confirming.
	pressKey(s, tea.KeyDown)  // move cursor to enabled (cursor 1)
	pressKey(s, tea.KeyEnter) // select enabled

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after accepting manual-resolution step")
	}

	if !s.Selection().Settings.ManualResolution {
		t.Error("Settings.ManualResolution = false after selecting enabled; " +
			"the manual-resolution step must set Settings.ManualResolution = true when enabled is chosen")
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.2: Backward navigation from configStepCommitBranch
// ---------------------------------------------------------------------------

// TestConfigScreen_BackNav_CommitBranchReachable_WhenCommitsEnabled verifies
// that pressing Esc from the manual-resolution step when commits are enabled
// lands on configStepCommitBranch, not on configStepCommits or any earlier step.
//
// RED: configStepManualResolution is not reachable.
func TestConfigScreen_BackNav_CommitBranchReachable_WhenCommitsEnabled(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // orchestrated (no pre-consult step)
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// Enable commits so the commit-branch step appears.
	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	if s.step == configStepCommits {
		pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // commits: enabled
	}
	// Accept the default branch variant (mosaic-owned).
	if s.step == configStepCommitBranch {
		pressKey(s, tea.KeyEnter) // branch: mosaic-owned (cursor 0)
	}

	if s.step != configStepManualResolution {
		t.Fatalf("step = %v, want configStepManualResolution; cannot test backward navigation", s.step)
	}

	// Pressing Esc from manual-resolution when commits are enabled must return
	// to configStepCommitBranch (not configStepCommits or configStepCheckpoints).
	pressKey(s, tea.KeyEsc)
	if s.step != configStepCommitBranch {
		t.Errorf("Esc from manual-resolution (commits enabled) landed on step %v, want configStepCommitBranch (%v); "+
			"backward navigation must return to commit-branch when commits were enabled",
			s.step, configStepCommitBranch)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.4: Additional parity tests completing the parity matrix
// ---------------------------------------------------------------------------

// TestConfigScreen_Parity_CommitsEnabledUserOwn_SettingsMatchCLIEquivalent
// verifies that selecting commits=enabled with branch=user-own produces
// Settings equivalent to:
//   --mode orchestrated --commits enabled --commit-branch user-own
//
// RED: commits and commit-branch steps are not shown.
func TestConfigScreen_Parity_CommitsEnabledUserOwn_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	if s.step == configStepCommits {
		pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // commits: enabled
	}
	if s.step == configStepCommitBranch {
		// mosaic-owned is at cursor 0 (recommended/first); user-own is at cursor 1.
		pressKey(s, tea.KeyDown)  // move cursor to user-own
		pressKey(s, tea.KeyEnter) // select user-own
	}
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // manual resolution: disabled (default)
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all steps")
	}

	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeOrchestrated,
		Checkpoints:         false,
		Commits:             true,
		CommitBranchVariant: domain.CommitBranchUserOwn,
		PreConsultation:     false,
		ManualResolution:    false,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v\n"+
			"ConfigSelection.Settings must equal the RunSettings the CLI produces from the equivalent flags",
			got, wantSettings)
	}
}

// TestConfigScreen_Parity_ManualResolutionEnabled_SettingsMatchCLIEquivalent
// verifies that enabling manual resolution produces Settings equivalent to:
//   --mode orchestrated --manual-resolution
//
// RED: configStepManualResolution is not wired in; Settings.ManualResolution
// remains false.
func TestConfigScreen_Parity_ManualResolutionEnabled_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil) // no commit agent

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step == configStepManualResolution {
		// Disabled is the default preselection at cursor 0; enabled is at cursor 1.
		pressKey(s, tea.KeyDown)  // move cursor to enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // select enabled
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after accepting manual-resolution step")
	}

	// When commits are disabled, CommitBranchVariant must be the zero value
	// (empty string). The mosaic-owned default applies only when commits are enabled.
	// RED: current implementation pre-populates CommitBranchMOSAICOwned unconditionally.
	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeOrchestrated,
		Checkpoints:         false,
		Commits:             false,
		CommitBranchVariant: "", // zero value: commits disabled
		PreConsultation:     false,
		ManualResolution:    true,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v\n"+
			"ConfigSelection.Settings must equal the RunSettings the CLI produces from the equivalent flags",
			got, wantSettings)
	}
}

// TestConfigScreen_Parity_CommitsEnabledMOSAICOwned_SettingsMatchCLIEquivalent
// verifies that selecting commits=enabled with branch=mosaic-owned produces
// Settings equivalent to:
//   --mode orchestrated --commits enabled --commit-branch mosaic-owned
//
// RED: neither commits step nor commit-branch step is shown.
func TestConfigScreen_Parity_CommitsEnabledMOSAICOwned_SettingsMatchCLIEquivalent(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	if s.step == configStepCommits {
		pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
		pressKey(s, tea.KeyEnter) // commits: enabled
	}
	if s.step == configStepCommitBranch {
		pressKey(s, tea.KeyEnter) // branch: mosaic-owned (cursor 0, the recommended default)
	}
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // manual resolution: disabled
	}

	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving all steps")
	}

	wantSettings := domain.RunSettings{
		Mode:                domain.ExecutionModeOrchestrated,
		Checkpoints:         false,
		Commits:             true,
		CommitBranchVariant: domain.CommitBranchMOSAICOwned,
		PreConsultation:     false,
		ManualResolution:    false,
	}

	got := s.Selection().Settings
	if got != wantSettings {
		t.Errorf("Settings mismatch:\n  got  = %+v\n  want = %+v\n"+
			"ConfigSelection.Settings must equal the RunSettings the CLI produces from the equivalent flags",
			got, wantSettings)
	}
}


