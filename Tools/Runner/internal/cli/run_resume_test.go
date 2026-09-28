package cli_test

import (
	"os"
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

// TestDefaultPath_ZeroCandidates_Refuses verifies the removal of the
// zero-candidate auto-start defect (AC2.6). With neither --run nor --new-run
// and an empty working directory, the CLI must refuse rather than silently
// minting a new run: the number of runs present never changes whether the
// question is asked, and starting a new run must be stated as an explicit
// choice (--new-run), not inferred from an empty workspace.
//
// Currently fails (RED): the CLI still auto-starts a new run for zero
// candidates (the defect this stage removes).
func TestDefaultPath_ZeroCandidates_Refuses(t *testing.T) {
	rootDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) when no run exists and no selection flag was given", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--new-run") {
		t.Errorf("stderr %q does not mention --new-run as the way to start a new run", errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when selection is unresolved (zero candidates, no flags)")
	}
}

// TestDefaultPath_OneCandidateWithNoFlag_Refuses is the CLI-side expression
// of the core defect this stage removes (AC2.2): a workspace with exactly one
// resumable run must no longer be resumed silently. With neither --run nor
// --new-run, the CLI must refuse and name the candidate so the caller can
// pass --run explicitly, or --new-run to start fresh.
//
// Currently fails (RED): the CLI still auto-resumes the single candidate.
func TestDefaultPath_OneCandidateWithNoFlag_Refuses(t *testing.T) {
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, cliRunID1)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) when exactly one resumable run exists and no selection flag was given", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, cliRunID1) {
		t.Errorf("stderr %q does not name the single candidate %q", errOut, cliRunID1)
	}
	if !strings.Contains(errOut, "--new-run") {
		t.Errorf("stderr %q does not mention --new-run as an available choice", errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when a single candidate exists and no selection flag was given")
	}
}

func TestDefaultPath_MultipleCandidates_CLIRejectsWithRunIDList(t *testing.T) {
	// With no --run or --new-run and multiple resumable candidates in the
	// working directory, the CLI cannot resolve ambiguity in non-interactive
	// mode. It must reject with ExitUsage and list the candidate run_ids in
	// stderr. Currently fails (RED): I5.3 has not added the default scan path.
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, cliRunID1)
	writeResumableRunArtifact(t, rootDir, cliRunID2)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage when multiple candidates exist and no --run flag", code)
	}
	// stderr must identify the candidate run_ids so the user knows which to pass to --run.
	if !strings.Contains(errOut, cliRunID1) {
		t.Errorf("stderr %q does not contain candidate run_id %q", errOut, cliRunID1)
	}
	if !strings.Contains(errOut, cliRunID2) {
		t.Errorf("stderr %q does not contain candidate run_id %q", errOut, cliRunID2)
	}
	if sess.called {
		t.Error("session.Start must not be called when multiple candidates exist and no --run flag")
	}
}

// TestDefaultPath_MultipleCandidates_RefusalMentionsNewRunOption verifies
// AC2.3: whatever the workspace contains, starting a new run is an available
// outcome, so the multi-candidate refusal must also mention --new-run, not
// just the existing candidates.
func TestDefaultPath_MultipleCandidates_RefusalMentionsNewRunOption(t *testing.T) {
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, cliRunID1)
	writeResumableRunArtifact(t, rootDir, cliRunID2)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d)", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "--new-run") {
		t.Errorf("stderr %q does not mention --new-run; starting a new run must remain an available choice", errOut)
	}
}

func TestCOMPLETEDMarker_WrittenWhenRunCompleted(t *testing.T) {
	// When session.Start returns RunCompleted, the CLI must call
	// store.SetPhase with phase="COMPLETED".
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status: domain.RunCompleted,
			Message: "run finished",
		},
	}
	code, _, _ := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
	}, spy, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess", code)
	}
	if len(spy.setCalls) == 0 {
		t.Error("store.SetPhase was not called; want it called with phase=COMPLETED after RunCompleted")
	} else if spy.setCalls[0].phase != "COMPLETED" {
		t.Errorf("SetPhase called with phase=%q, want %q", spy.setCalls[0].phase, "COMPLETED")
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunStopped(t *testing.T) {
	// When session.Start returns RunStopped, the CLI must NOT call SetPhase
	// (the run must remain resumable).
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunStopped},
	}
	runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunStopped", len(spy.setCalls))
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunDeviationUnresolved(t *testing.T) {
	// When session.Start returns RunDeviationUnresolved, the CLI must NOT call SetPhase.
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunDeviationUnresolved},
	}
	runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunDeviationUnresolved", len(spy.setCalls))
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunRefused(t *testing.T) {
	// When session.Start returns RunRefused, the CLI must NOT call SetPhase.
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunRefused},
	}
	runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunRefused", len(spy.setCalls))
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunFailed(t *testing.T) {
	// When session.Start returns RunFailed, the CLI must NOT call SetPhase.
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{Status: domain.RunFailed},
	}
	runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunFailed", len(spy.setCalls))
	}
}

func TestCOMPLETEDMarker_NotWrittenWhenRunStoppedByConsultant(t *testing.T) {
	// When session.Start returns RunStoppedByConsultant, the CLI must NOT call
	// SetPhase (the run must remain resumable — AC7.5). This test guards against
	// an implementation that inadvertently writes the COMPLETED marker for the
	// new stop outcome, which would silently break resumability.
	spy := &spyStore{}
	sess := &scriptedSession{
		outcome: domain.RunOutcome{
			Status:     domain.RunStoppedByConsultant,
			StopReason: "consultant decided to halt the run",
		},
	}
	runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
	}, spy, sess)

	if len(spy.setCalls) != 0 {
		t.Errorf("store.SetPhase called %d time(s), want 0 when status is RunStoppedByConsultant", len(spy.setCalls))
	}
}

// TestAnnouncement_NewRun_StatedBeforeDispatch verifies that starting a new
// run via --new-run writes an announcement to stdout, before the session
// outcome, naming the resolved run_id and stating that the run is new.
func TestAnnouncement_NewRun_StatedBeforeDispatch(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, stdout, errOut := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.RunID == "" {
		t.Fatal("sess.config.RunID is empty; cannot verify announcement without a resolved run_id")
	}
	if !strings.Contains(stdout, sess.config.RunID) {
		t.Errorf("stdout %q does not contain the resolved run_id %q; the chosen run must be announced before dispatch", stdout, sess.config.RunID)
	}
	if !strings.Contains(strings.ToLower(stdout), "new") {
		t.Errorf("stdout %q does not state that the run is new", stdout)
	}
}

// TestAnnouncement_ResumedRun_ContainsPosition verifies that resuming a run
// via --run writes an announcement to stdout naming the run_id, stating that
// it is resumed, and reporting its recorded phase.
func TestAnnouncement_ResumedRun_ContainsPosition(t *testing.T) {
	rootDir := t.TempDir()
	writeResumableRunArtifact(t, rootDir, testRunID)
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, stdout, errOut := runCLIWithStore(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--run", testRunID,
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if !strings.Contains(stdout, testRunID) {
		t.Errorf("stdout %q does not contain the resumed run_id %q", stdout, testRunID)
	}
	if !strings.Contains(strings.ToLower(stdout), "resum") {
		t.Errorf("stdout %q does not state that the run is resumed", stdout)
	}
	// writeResumableRunArtifact records current_state.phase: EXECUTION.
	if !strings.Contains(stdout, "EXECUTION") {
		t.Errorf("stdout %q does not contain the recorded phase %q", stdout, "EXECUTION")
	}
}
