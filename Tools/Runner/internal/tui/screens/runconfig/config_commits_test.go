package runconfig

// config_commits_test.go verifies the ConfigScreen commit and commit-branch steps.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Commits step conditional visibility
//
// The commits step is shown only when a Class=commit agent is declared.
// ---------------------------------------------------------------------------

// TestConfigScreen_CommitsStep_NotShown_WhenNoCommitAgentDeclared verifies that
// when no commit-class agent is declared, the commits step is skipped and the
// wizard reaches Done() without ever landing on configStepCommits.
//
// This test is a regression guard: it should pass in both RED and GREEN
// (the commits step is not yet implemented and is also correctly absent when
// no commit agent is declared). After I8.1 is implemented this test still
// passes — configStepCommits is still skipped for zero commit agents.
//
// Note: the always-shown configStepManualResolution step follows checkpoints in
// the orchestrated path; it is driven through here so Done() reflects completion
// of all applicable steps, not an unexpected stop at manual-resolution.
func TestConfigScreen_CommitsStep_NotShown_WhenNoCommitAgentDeclared(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "checkpoint-agent", Class: "checkpoint"}, // only checkpoint, no commit
	})

	driveConfigScreenModeSelect(s, 0) // mode
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// The commits step must not appear when no commit-class agent is declared.
	if s.step == configStepCommits {
		t.Errorf("step = configStepCommits after checkpoints when no commit agent is declared; "+
			"the commits step must be skipped when no commit-class agent exists")
		return
	}

	// The always-shown manual-resolution step follows checkpoints (orchestrated mode,
	// no commit agent). Drive through it with the default selection so Done() reflects
	// completion of all applicable steps.
	if s.step == configStepManualResolution {
		pressKey(s, tea.KeyEnter) // accept default (disabled)
	}

	acceptReviewLoopLimitIfAsked(s)
	if !s.Done() {
		t.Errorf("ConfigScreen did not reach Done() after all applicable steps (no commit agent, orchestrated mode); "+
			"current step = %v", s.step)
	}
}

// TestConfigScreen_CommitsStep_Shown_WhenCommitAgentDeclared verifies that
// when a Class=commit agent is declared, the commits step appears after the
// checkpoints step.
//
// RED: advance() after checkpoints goes to infraClass or Done, never to
// configStepCommits. The wizard completes without showing the commits step.
func TestConfigScreen_CommitsStep_Shown_WhenCommitAgentDeclared(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // mode
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// After checkpoints with a commit agent declared, the wizard must show the
	// commits step (configStepCommits), not jump to Done or infraClass.
	if s.Done() {
		t.Error("ConfigScreen reached Done() immediately after checkpoints when a commit agent is declared; " +
			"the commits step must be shown before finishing")
		return
	}
	if s.step != configStepCommits {
		t.Errorf("step = %v after checkpoints with commit agent, want configStepCommits (%v); "+
			"the commits step must be shown when a commit-class agent is declared",
			s.step, configStepCommits)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Commit branch variant step conditional visibility
//
// The commit branch variant step is shown only when commits are enabled.
// ---------------------------------------------------------------------------

// TestConfigScreen_CommitBranchStep_NotShown_WhenCommitsDisabled verifies that
// when commits are disabled at the commits step, the commit branch step is
// skipped.
//
// This test is a regression guard: it should pass in both RED and GREEN.
func TestConfigScreen_CommitBranchStep_NotShown_WhenCommitsDisabled(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // mode
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step == configStepCommits {
		// Commits step is shown — cursor 0 = Disabled by default, just confirm.
		pressKey(s, tea.KeyEnter) // confirm: commits disabled

		// The commit branch step must not appear.
		if s.step == configStepCommitBranch {
			t.Errorf("step = configStepCommitBranch after selecting commits=disabled; "+
				"the commit branch step must be skipped when commits are disabled")
		}
	}
	// If configStepCommits was never shown (RED state), the regression guard
	// still passes — there is nothing here that can fail in the RED path.
}

// TestConfigScreen_CommitBranchStep_Shown_WhenCommitsEnabled verifies that
// when commits are enabled at the commits step, the commit branch step appears
// next.
//
// RED: configStepCommits is never reached, so this test fails at the
// configStepCommits check.
func TestConfigScreen_CommitBranchStep_Shown_WhenCommitsEnabled(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0) // mode (orchestrated)
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.step != configStepCommits {
		t.Fatalf("step = %v after checkpoints with commit agent, want configStepCommits; "+
			"cannot test branch variant visibility without the commits step", s.step)
	}

	// Select commits=enabled: cursor 0 = Disabled (default), cursor 1 = Enabled.
	pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
	pressKey(s, tea.KeyEnter) // select Enabled

	if s.step != configStepCommitBranch {
		t.Errorf("step = %v after commits=enabled, want configStepCommitBranch (%v); "+
			"the branch variant step must appear when commits are enabled",
			s.step, configStepCommitBranch)
	}
}

// TestConfigScreen_CommitBranchStep_MOSAICOwnedIsRecommended verifies that the
// mosaic-owned branch variant option is visually marked as recommended in the
// branch variant step.
//
// RED: configStepCommitBranch is never shown.
func TestConfigScreen_CommitBranchStep_MOSAICOwnedIsRecommended(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents([]domain.DeclaredInfraAgent{
		{Name: "commit-agent", Class: "commit"},
	})

	driveConfigScreenModeSelect(s, 0)
	pressKey(s, tea.KeyEnter) // harness
	advanceConfigScreenTimeout(s)
	pressKey(s, tea.KeyEnter) // version drift
	pressKey(s, tea.KeyEnter) // checkpoints

	if s.step != configStepCommits {
		t.Fatalf("step = %v, want configStepCommits", s.step)
	}
	// cursor 0 = Disabled (default), cursor 1 = Enabled.
	pressKey(s, tea.KeyDown)  // move cursor to Enabled (cursor 1)
	pressKey(s, tea.KeyEnter) // commits enabled

	if s.step != configStepCommitBranch {
		t.Fatalf("step = %v, want configStepCommitBranch", s.step)
	}
	view := s.View()
	if !containsSubstr(view, "Recommended") && !containsSubstr(view, "recommended") {
		t.Errorf("commit branch step view does not mark mosaic-owned as recommended; "+
			"the branch variant step must present mosaic-owned as the recommended choice:\n%s", view)
	}
}

// ---------------------------------------------------------------------------
// Stage 8 — T8.1: Pre-consultation step conditional visibility
//
// The pre-consultation step is shown only when mode is auto or auto-review.
// ---------------------------------------------------------------------------

// TestConfigScreen_PreConsultStep_NotShown_WhenModeOrchestrated verifies that
// the pre-consultation step is skipped when the selected mode is orchestrated.
//
// This test should pass in both RED and GREEN: the pre-consult step does not
// yet exist, and it is correctly absent for the orchestrated mode.
func TestConfigScreen_PreConsultStep_NotShown_WhenModeOrchestrated(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 0) // orchestrated
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// After checkpoints with orchestrated mode, the pre-consult step must NOT appear.
	if s.step == configStepPreConsult {
		t.Error("step = configStepPreConsult after orchestrated mode selection; " +
			"the pre-consultation step must be skipped when mode is orchestrated")
	}
}

// TestConfigScreen_PreConsultStep_Shown_WhenModeAuto verifies that the
// pre-consultation step appears after the checkpoints step when mode is auto.
//
// RED: mode selection does not produce a stored mode value, so the pre-consult
// conditional cannot fire. The step is never shown.
func TestConfigScreen_PreConsultStep_Shown_WhenModeAuto(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil) // no commit agent

	driveConfigScreenModeSelect(s, 1) // auto
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	// After checkpoints with mode=auto, the pre-consult step must appear.
	// In RED the wizard reaches Done() or stays at an unexpected step.
	if s.Done() {
		t.Error("ConfigScreen reached Done() before showing the pre-consultation step; " +
			"the pre-consult step must appear when mode is auto")
		return
	}
	if s.step != configStepPreConsult {
		t.Errorf("step = %v after checkpoints with mode=auto, want configStepPreConsult (%v)",
			s.step, configStepPreConsult)
	}
}

// TestConfigScreen_PreConsultStep_Shown_WhenModeAutoReview verifies that the
// pre-consultation step appears when mode is auto-review.
//
// RED: same as above — the step is never shown.
func TestConfigScreen_PreConsultStep_Shown_WhenModeAutoReview(t *testing.T) {
	s := NewConfigScreen(80, 24, screens.Styles{})
	s.SetDeclaredAgents(nil)

	driveConfigScreenModeSelect(s, 2) // auto-review
	pressKey(s, tea.KeyEnter)          // harness
	advanceConfigScreenTimeout(s)      // timeout
	pressKey(s, tea.KeyEnter)          // version drift
	pressKey(s, tea.KeyEnter)          // checkpoints

	if s.Done() {
		t.Error("ConfigScreen reached Done() before the pre-consultation step; " +
			"the pre-consult step must appear when mode is auto-review")
		return
	}
	if s.step != configStepPreConsult {
		t.Errorf("step = %v after checkpoints with mode=auto-review, want configStepPreConsult (%v)",
			s.step, configStepPreConsult)
	}
}

