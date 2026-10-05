package cli_test

import (
	"os"
	"strings"
	"testing"

	"mosaic-run/internal/cli"
	"mosaic-run/internal/domain"
)

func TestNewRunFlag_SetsIsNewRunTrue(t *testing.T) {
	// --new-run must set RunConfig.IsNewRun to true and cause the session to be called.
	// Currently fails (RED) because --new-run is not a recognised flag yet (I5.3).
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	_, _, _ = runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
	}, sess)

	if !sess.called {
		t.Fatal("session.Start was not called; --new-run must cause the session to run")
	}
	if !sess.config.IsNewRun {
		t.Error("IsNewRun = false, want true when --new-run is set")
	}
}

func TestRunFlag_SetsRunID(t *testing.T) {
	// --run <run_id> must set RunConfig.RunID to the given run_id value.
	// Currently fails (RED) because --run is not a recognised flag yet (I5.3).
	// Filesystem setup: create a resumable run folder so the CLI can find it after
	// --run is implemented and reads the artifact to confirm the run is not COMPLETED.
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
	_, _, _ = runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--run", testRunID,
	}, sess)

	if !sess.called {
		t.Fatal("session.Start was not called; --run must cause the session to run")
	}
	if sess.config.RunID != testRunID {
		t.Errorf("RunID = %q, want %q", sess.config.RunID, testRunID)
	}
}

func TestRunFlag_SetsIsNewRunFalse(t *testing.T) {
	// --run must set RunConfig.IsNewRun to false (resuming, not creating).
	// Currently fails (RED) because --run is not a recognised flag yet (I5.3).
	// Filesystem setup mirrors TestRunFlag_SetsRunID.
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
	_, _, _ = runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--run", testRunID,
	}, sess)

	if !sess.called {
		t.Fatal("session.Start was not called; --run must cause the session to run")
	}
	if sess.config.IsNewRun {
		t.Error("IsNewRun = true, want false when --run selects an existing run")
	}
}

func TestRunFlag_SetsRunFolder(t *testing.T) {
	// --run <run_id> must set RunConfig.RunFolder to the derived folder path.
	// Currently fails (RED) because --run is not a recognised flag yet (I5.3).
	// Filesystem setup mirrors TestRunFlag_SetsRunID.
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
	_, _, _ = runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--run", testRunID,
	}, sess)

	if !sess.called {
		t.Fatal("session.Start was not called; --run must cause the session to run")
	}
	if sess.config.RunFolder == "" {
		t.Error("RunFolder is empty, want a path derived from the run_id")
	}
	wantFolderName := "Orchestration-" + testRunID
	if !strings.Contains(sess.config.RunFolder, wantFolderName) {
		t.Errorf("RunFolder = %q, want it to contain %q", sess.config.RunFolder, wantFolderName)
	}
}

func TestRunAndNewRunFlags_MutuallyExclusive(t *testing.T) {
	// Passing both --run and --new-run must be rejected with a clear error.
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--run", testRunID,
		"--new-run",
		"--review-loop-limit", "3",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage (%d) for mutually exclusive flags", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("stderr %q does not contain %q", errOut, "mutually exclusive")
	}
	if sess.called {
		t.Error("session.Start should not be called when flags are mutually exclusive")
	}
}

func TestExistingArtifactFlag_IsNoLongerRecognized(t *testing.T) {
	// --existing-artifact was removed in Stage 5; cobra must reject it as unknown.
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--existing-artifact", "resume",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("--existing-artifact: exit code = %d, want ExitUsage (%d) (flag must be removed)",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "unknown flag") && !strings.Contains(errOut, "existing-artifact") {
		t.Errorf("stderr %q does not indicate that --existing-artifact is unknown", errOut)
	}
}

// TestOrchestratorFileFlag_IsNoLongerRecognized verifies that --orchestrator-file
// is not recognised by the run subcommand after Stage 3 removal. cobra must
// reject it as an unknown flag (ExitUsage), since orchestrator discovery is now
// automatic from the harness's agents directory.
func TestOrchestratorFileFlag_IsNoLongerRecognized(t *testing.T) {
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--orchestrator-file", "orch.md",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("--orchestrator-file: exit code = %d, want ExitUsage (%d) (flag must be removed)",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "unknown flag") && !strings.Contains(errOut, "orchestrator-file") {
		t.Errorf("stderr %q does not indicate that --orchestrator-file is unknown", errOut)
	}
	if sess.called {
		t.Error("session.Start must not be called when an unknown flag is supplied")
	}
}

// TestHarnessID_InRunConfig verifies that the --harness flag value is propagated
// to RunConfig.HarnessID so the session layer can use it for discovery and
// snapshot creation.
func TestHarnessID_InRunConfig(t *testing.T) {
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLIWithStore(t, []string{
		"run",
		"--workflow", "w1",
		"--task", "do work",
		"--mode", "auto",
		"--new-run",
		"--review-loop-limit", "3",
		"--harness", "fake",
	}, &spyStore{}, sess)

	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want ExitSuccess; stderr: %q", code, errOut)
	}
	if !sess.called {
		t.Fatal("session.Start was not called")
	}
	if sess.config.HarnessID != "fake" {
		t.Errorf("HarnessID = %q, want %q", sess.config.HarnessID, "fake")
	}
}

func TestArtifactLocationFlag_IsNoLongerRecognized(t *testing.T) {
	// --artifact-location was removed in Stage 5; cobra must reject it as unknown.
	sess := &scriptedSession{outcome: domain.RunOutcome{Status: domain.RunCompleted}}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--artifact-location", "/some/path",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("--artifact-location: exit code = %d, want ExitUsage (%d) (flag must be removed)",
			code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "unknown flag") && !strings.Contains(errOut, "artifact-location") {
		t.Errorf("stderr %q does not indicate that --artifact-location is unknown", errOut)
	}
}

func TestHelpText_ContainsNewRunFlag(t *testing.T) {
	// The help text must mention --new-run after implementation.
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if !strings.Contains(errOut, "--new-run") {
		t.Errorf("help text does not contain --new-run; got:\n%s", errOut)
	}
}

func TestHelpText_ContainsRunFlag(t *testing.T) {
	// The help text must mention --run after implementation.
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if !strings.Contains(errOut, "--run") {
		t.Errorf("help text does not contain --run; got:\n%s", errOut)
	}
}

func TestHelpText_DoesNotContainExistingArtifactFlag(t *testing.T) {
	// --existing-artifact was removed in Stage 5; it must not appear in help text.
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if strings.Contains(errOut, "existing-artifact") {
		t.Errorf("help text still contains 'existing-artifact' (must be removed); got:\n%s", errOut)
	}
}

func TestHelpText_DoesNotContainArtifactLocationFlag(t *testing.T) {
	// --artifact-location was removed in Stage 5; it must not appear in help text.
	sess := &scriptedSession{}
	_, _, errOut := runCLI(t, []string{"run", "--help"}, sess)

	if strings.Contains(errOut, "artifact-location") {
		t.Errorf("help text still contains 'artifact-location' (must be removed); got:\n%s", errOut)
	}
}

func TestRunFlag_TargetingCompletedRun_IsRejected(t *testing.T) {
	// --run with a run_id whose artifact has phase==COMPLETED must be rejected.
	// The CLI must not call session.Start for a completed run.
	rootDir := t.TempDir()
	writeCompletedRunArtifact(t, rootDir, testRunID)

	// Change to rootDir so the CLI can find the run folder.
	// Note: this changes the process working directory for this test.
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
		"--run", testRunID,
	}, sess)

	if code != cli.ExitUsage && code != cli.ExitRefused {
		t.Errorf("exit code = %d, want ExitUsage or ExitRefused for completed run", code)
	}
	if !strings.Contains(errOut, "completed") && !strings.Contains(errOut, testRunID) {
		t.Errorf("stderr %q does not mention 'completed' or the run_id %q", errOut, testRunID)
	}
	if sess.called {
		t.Error("session.Start must not be called for a completed run")
	}
}

func TestRunFlag_InvalidFormat_IsRejected(t *testing.T) {
	// --run with a run_id that does not match the expected format must be rejected.
	sess := &scriptedSession{}
	code, _, errOut := runCLI(t, []string{
		"run",

		"--workflow", "w1",
		"--task", "do work",
		"--run", "not-a-valid-run-id",
	}, sess)

	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want ExitUsage for invalid run_id format", code)
	}
	_ = errOut // error message content not asserted (allow flexibility in wording)
	if sess.called {
		t.Error("session.Start must not be called for an invalid run_id")
	}
}

func TestRunFlag_NonExistentRun_IsRejected(t *testing.T) {
	// --run <run_id> whose folder does not exist on disk must be rejected.
	// The CLI must not call session.Start when the specified run folder is absent.
	rootDir := t.TempDir()
	// No run folder created — the folder for testRunID is absent.
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
		"--run", testRunID,
	}, sess)

	if code != cli.ExitUsage && code != cli.ExitRefused {
		t.Errorf("exit code = %d, want ExitUsage or ExitRefused for non-existent run_id", code)
	}
	if !strings.Contains(errOut, testRunID) {
		t.Errorf("stderr %q does not contain the run_id %q", errOut, testRunID)
	}
	if sess.called {
		t.Error("session.Start must not be called when the run folder is absent")
	}
}
