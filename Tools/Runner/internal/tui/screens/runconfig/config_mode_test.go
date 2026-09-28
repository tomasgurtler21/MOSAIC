package runconfig

// config_mode_test.go verifies the ConfigScreen execution-mode step.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Mode step tests
//
// The mode step is the first configuration step. It presents three execution
// modes (orchestrated, auto, auto-review) with no preselected default. The
// wizard cannot advance until the user makes an explicit choice.
//
// All tests in this section are RED: the ConfigScreen currently starts at
// configStepHarness, not configStepMode, and nothing populates Settings.Mode.
// ---------------------------------------------------------------------------

// TestConfigScreen_ModeStep_IsFirstStep verifies that a freshly created
// ConfigScreen starts at configStepMode, making mode selection the first
// configuration prompt the user sees.
//
// RED: NewConfigScreen() currently starts at configStepHarness.
func TestConfigScreen_ModeStep_IsFirstStep(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	if s.step != configStepMode {
		t.Errorf("initial step = %v, want configStepMode (%v); the mode step must be the first configuration prompt",
			s.step, configStepMode)
	}
}

// TestConfigScreen_ModeStep_ViewShowsAllThreeModes verifies that the mode step
// renders all three valid execution modes: orchestrated, auto, and auto-review.
//
// RED: the initial view shows the harness step, not mode options.
func TestConfigScreen_ModeStep_ViewShowsAllThreeModes(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	if s.step != configStepMode {
		t.Fatalf("precondition: step = %v, want configStepMode; test cannot proceed", s.step)
	}
	view := s.View()
	for _, mode := range []string{"orchestrated", "auto", "auto-review"} {
		if !containsSubstr(view, mode) {
			t.Errorf("mode step view does not contain %q; all three execution modes must be shown:\n%s", mode, view)
		}
	}
}

// TestConfigScreen_ModeStep_NoPreselection verifies that the mode step begins
// with no option highlighted as a default — the cursor is logically unset so
// the user must make an explicit choice before the step advances.
//
// RED: the mode step does not exist yet; the screen starts at the harness step
// which always has option 0 preselected.
func TestConfigScreen_ModeStep_NoPreselection(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	if s.step != configStepMode {
		t.Fatalf("precondition: step = %v, want configStepMode; test cannot proceed", s.step)
	}
	// Pressing Enter immediately (no cursor movement) must not advance the step —
	// the step requires an explicit choice, no option is preselected.
	pressKey(s, tea.KeyEnter)
	if s.step != configStepMode {
		t.Errorf("step = %v after Enter with no cursor movement, want configStepMode; "+
			"the mode step must not advance without an explicit selection", s.step)
	}
}

// TestConfigScreen_ModeStep_SelectOrchestrated_ProducesOrchestratedMode verifies
// that selecting the first option on the mode step results in
// Selection().Settings.Mode == ExecutionModeOrchestrated after the wizard completes.
//
// RED: Nothing populates Settings.Mode; it remains ExecutionModeUnset.
func TestConfigScreen_ModeStep_SelectOrchestrated_ProducesOrchestratedMode(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil) // no declared agents — simplest case
	s.SetIsNewRun(false)     // simulate resumed run so version-drift screen appears
	s.SetNeedsRunnerAdoption(true) // a resumed native artifact still asks the mode first

	// Drive to done: mode (orchestrated = cursor 0), harness, timeout, version drift, checkpoints,
	// manual-resolution (always shown; orchestrated has no pre-consult step).
	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints
	pressKey(s, tea.KeyEnter)          // manual-resolution (disabled default, no commit agent, no infra class)

	acceptReviewLoopLimitIfAsked(s)
	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all steps")
	}
	sel := s.Selection()
	if sel.Settings.Mode != domain.ExecutionModeOrchestrated {
		t.Errorf("Settings.Mode = %v, want %v; selecting the orchestrated option must produce ExecutionModeOrchestrated",
			sel.Settings.Mode, domain.ExecutionModeOrchestrated)
	}
}

// TestConfigScreen_ModeStep_SelectAuto_ProducesAutoMode verifies that selecting
// the second option (auto) results in Settings.Mode == ExecutionModeAuto.
//
// RED: Settings.Mode remains ExecutionModeUnset.
func TestConfigScreen_ModeStep_SelectAuto_ProducesAutoMode(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints
	pressKey(s, tea.KeyEnter)          // pre-consult (shown for auto mode; accept default)
	pressKey(s, tea.KeyEnter)          // manual-resolution (always shown; accept default)

	acceptReviewLoopLimitIfAsked(s)
	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all steps")
	}
	if s.Selection().Settings.Mode != domain.ExecutionModeAuto {
		t.Errorf("Settings.Mode = %v, want %v",
			s.Selection().Settings.Mode, domain.ExecutionModeAuto)
	}
}

// TestConfigScreen_ModeStep_SelectAutoReview_ProducesAutoReviewMode verifies
// that selecting the third option (auto-review) results in
// Settings.Mode == ExecutionModeAutoReview.
//
// RED: Settings.Mode remains ExecutionModeUnset.
func TestConfigScreen_ModeStep_SelectAutoReview_ProducesAutoReviewMode(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 2) // auto-review
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints
	pressKey(s, tea.KeyEnter)          // pre-consult (shown for auto-review mode; accept default)
	pressKey(s, tea.KeyEnter)          // manual-resolution (always shown; accept default)

	acceptReviewLoopLimitIfAsked(s)
	if !s.Done() {
		t.Fatal("ConfigScreen did not reach Done() after driving through all steps")
	}
	if s.Selection().Settings.Mode != domain.ExecutionModeAutoReview {
		t.Errorf("Settings.Mode = %v, want %v",
			s.Selection().Settings.Mode, domain.ExecutionModeAutoReview)
	}
}

