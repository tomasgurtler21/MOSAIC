package cli_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

// TestModeFlag_Orchestrated_ReachesRunSettings verifies that --mode orchestrated
// sets RunSettings.Mode to ExecutionModeOrchestrated.
func TestModeFlag_Orchestrated_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "orchestrated",
		"--new-run",
	}
	_, _, _ = runCLI(t, args, sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.Mode != domain.ExecutionModeOrchestrated {
		t.Errorf("Mode = %q, want %q", sess.config.Mode, domain.ExecutionModeOrchestrated)
	}
}

// TestModeFlag_Auto_ReachesRunSettings verifies that --mode auto sets
// RunSettings.Mode to ExecutionModeAuto.
func TestModeFlag_Auto_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
	}
	_, _, _ = runCLI(t, args, sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.Mode != domain.ExecutionModeAuto {
		t.Errorf("Mode = %q, want %q", sess.config.Mode, domain.ExecutionModeAuto)
	}
}

// TestModeFlag_AutoReview_ReachesRunSettings verifies that --mode auto-review
// sets RunSettings.Mode to ExecutionModeAutoReview.
func TestModeFlag_AutoReview_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	args := []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto-review",
		"--new-run",
	}
	_, _, _ = runCLI(t, args, sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.Mode != domain.ExecutionModeAutoReview {
		t.Errorf("Mode = %q, want %q", sess.config.Mode, domain.ExecutionModeAutoReview)
	}
}

// TestCommitsFlag_Enabled_ReachesRunSettings verifies that --commits enabled
// sets RunSettings.Commits to true.
func TestCommitsFlag_Enabled_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commits", "enabled"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.Commits {
		t.Error("Commits = false, want true when --commits=enabled")
	}
}

// TestCommitsFlag_Disabled_ReachesRunSettings verifies that --commits disabled
// (or omitted, since disabled is the default) sets RunSettings.Commits to false.
func TestCommitsFlag_Disabled_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commits", "disabled"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.Commits {
		t.Error("Commits = true, want false when --commits=disabled")
	}
}

// TestCommitBranchFlag_MOSAICOwned_ReachesRunSettings verifies that
// --commit-branch mosaic-owned sets RunSettings.CommitBranchVariant to
// CommitBranchMOSAICOwned.
func TestCommitBranchFlag_MOSAICOwned_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commit-branch", "mosaic-owned"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant = %q, want %q",
			sess.config.CommitBranchVariant, domain.CommitBranchMOSAICOwned)
	}
}

// TestCommitBranchFlag_UserOwn_ReachesRunSettings verifies that
// --commit-branch user-own sets RunSettings.CommitBranchVariant to
// CommitBranchUserOwn.
func TestCommitBranchFlag_UserOwn_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commit-branch", "user-own"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.CommitBranchVariant != domain.CommitBranchUserOwn {
		t.Errorf("CommitBranchVariant = %q, want %q",
			sess.config.CommitBranchVariant, domain.CommitBranchUserOwn)
	}
}

// TestCommitBranchFlag_DefaultIsMOSAICOwned_WhenOmittedAndCommitsEnabled verifies
// that omitting --commit-branch when --commits enabled leaves CommitBranchVariant
// at CommitBranchMOSAICOwned, the documented default for commits-enabled runs.
func TestCommitBranchFlag_DefaultIsMOSAICOwned_WhenOmittedAndCommitsEnabled(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commits", "enabled"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.CommitBranchVariant != domain.CommitBranchMOSAICOwned {
		t.Errorf("CommitBranchVariant = %q, want default %q when --commits enabled and --commit-branch omitted",
			sess.config.CommitBranchVariant, domain.CommitBranchMOSAICOwned)
	}
}

// TestCommitBranchVariant_IsEmpty_WhenCommitsDisabledAndBranchOmitted verifies
// that when --commits is disabled (or omitted, since disabled is the default)
// and --commit-branch is not specified, RunSettings.CommitBranchVariant is
// the zero value (empty string), not mosaic-owned.
// RED: current implementation defaults to mosaic-owned regardless of --commits.
func TestCommitBranchVariant_IsEmpty_WhenCommitsDisabledAndBranchOmitted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--commits", "disabled"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.CommitBranchVariant != "" {
		t.Errorf("CommitBranchVariant = %q, want %q (zero value) when --commits disabled and --commit-branch omitted",
			sess.config.CommitBranchVariant, "")
	}
}

// TestPreConsultFlag_ReachesRunSettings verifies that --pre-consult sets
// RunSettings.PreConsultation to true.
func TestPreConsultFlag_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--pre-consult"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.PreConsultation {
		t.Error("PreConsultation = false, want true when --pre-consult is present")
	}
}

// TestPreConsultFlag_DefaultIsTrue_WhenOmitted verifies that omitting --pre-consult
// leaves RunSettings.PreConsultation enabled — pre-consultation is on by default
// so that automated runs are safe without explicit opt-in.
func TestPreConsultFlag_DefaultIsTrue_WhenOmitted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, newStage7BaseArgs(), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.PreConsultation {
		t.Error("PreConsultation = false, want true when --pre-consult is omitted; " +
			"pre-consultation must be enabled by default")
	}
}

// TestPreConsultFlag_ExplicitFalse_DisablesPreConsultation verifies that passing
// --pre-consult=false explicitly disables pre-consultation even though the flag
// defaults to enabled. The =false form is the only way users can opt out of the
// default, so it must be honoured by the cobra flag machinery.
//
// NOTE — conditional RED phase: with the current cobra default of false for
// --pre-consult, passing --pre-consult=false already produces PreConsultation =
// false, so this test passes before implementation. It only enters RED once I2.1
// flips the cobra default to true. The RED phase for this test must be
// re-verified after I2.1 lands, not before.
func TestPreConsultFlag_ExplicitFalse_DisablesPreConsultation(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--pre-consult=false"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.PreConsultation {
		t.Error("PreConsultation = true, want false when --pre-consult=false is passed; " +
			"the explicit-false form must override the default")
	}
}

// TestPreConsultFlag_ExplicitTrue_EnablesPreConsultation verifies that passing
// --pre-consult=true explicitly enables pre-consultation. The design behavioral
// contract table lists this invocation form as required to produce
// PreConsultation == true. Cobra handles this correctly for bool flags; this
// test closes the gap against the contract table for the =true form through the
// full CLI path into RunSettings.PreConsultation.
//
// REGRESSION GUARD — this test passes before I2.1 lands because --pre-consult=true
// produces PreConsultation = true regardless of the cobra default. It is not a
// TDD RED-phase driver; it pins the =true form's behavior so a future refactor
// cannot silently break explicit enabling.
func TestPreConsultFlag_ExplicitTrue_EnablesPreConsultation(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--pre-consult=true"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.PreConsultation {
		t.Error("PreConsultation = false, want true when --pre-consult=true is passed; " +
			"the explicit-true form must set PreConsultation enabled")
	}
}

// TestManualResolutionFlag_ReachesRunSettings verifies that --manual-resolution
// sets RunSettings.ManualResolution to true.
func TestManualResolutionFlag_ReachesRunSettings(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, append(newStage7BaseArgs(), "--manual-resolution"), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !sess.config.ManualResolution {
		t.Error("ManualResolution = false, want true when --manual-resolution is present")
	}
}

// TestManualResolutionFlag_DefaultIsFalse_WhenOmitted verifies that omitting
// --manual-resolution leaves RunSettings.ManualResolution as false.
func TestManualResolutionFlag_DefaultIsFalse_WhenOmitted(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, newStage7BaseArgs(), sess)
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.ManualResolution {
		t.Error("ManualResolution = true, want false when --manual-resolution is omitted")
	}
}

// TestRunStoppedByConsultant_ExitsWithDistinctNonZeroCode verifies that a
// RunStoppedByConsultant outcome maps to ExitStoppedByConsultant, which is a
// distinct non-zero exit code (AC7.5).
func TestRunStoppedByConsultant_ExitsWithDistinctNonZeroCode(t *testing.T) {
	const stopReason = "orchestrator decided to stop the run"
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			Message:    "run stopped by consultant",
			StopReason: stopReason,
		},
	}
	code, _, _ := runCLI(t, newStage7BaseArgs(), sess)
	if code == cli.ExitSuccess {
		t.Error("exit code = 0 (ExitSuccess), want non-zero for RunStoppedByConsultant")
	}
	if code != cli.ExitStoppedByConsultant {
		t.Errorf("exit code = %d, want ExitStoppedByConsultant (%d)",
			code, cli.ExitStoppedByConsultant)
	}
}

// TestRunStoppedByConsultant_StopReasonPrintedToStderr verifies that the
// consultant's stop reason is printed to stderr when the run is stopped
// (AC7.5). The artifact is left resumable; stderr is the operator's signal.
func TestRunStoppedByConsultant_StopReasonPrintedToStderr(t *testing.T) {
	const stopReason = "workflow prerequisites not met: missing artifact X"
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			Message:    stopReason,
			StopReason: stopReason,
		},
	}
	_, _, errOut := runCLI(t, newStage7BaseArgs(), sess)
	if !strings.Contains(errOut, stopReason) {
		t.Errorf("stderr %q does not contain stop reason %q", errOut, stopReason)
	}
}
