package session_test

// Tests for launch-failure preservation contracts at pre-consultation.
// Covers: both IsNewRun=false and IsNewRun=true keep the run folder and return
// a resumable start failure carrying HarnessLaunchError in outcome.Cause.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// TestSession_Start_PreConsultation_LaunchFailure_IsNewRunFalse_RunFolderPreserved
// verifies that when a pre-consultation fails with a *domain.HarnessLaunchError
// on a resumed run (IsNewRun=false), the session:
//  1. Returns RunStartFailed,
//  2. Carries the HarnessLaunchError in outcome.Cause (so the TUI can detect
//     the launch-failure identity without parsing the message), and
//  3. Preserves the run folder and its Orchestration.md byte-for-byte.
//
// This is the session-level counterpart to the app-level restart tests. Without
// this test the destructive behavior (silently wiping a user's existing run
// history on every launch failure during a resume) is invisible to the suite.
//
func TestSession_Start_PreConsultation_LaunchFailure_IsNewRunFalse_RunFolderPreserved(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Create the run folder and write a pre-existing Orchestration.md so the
	// test can assert the file survives the pre-consultation failure.
	runFolder := filepath.Join(dir, domain.RunScopedFolder(testRunID))
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: create run folder: %v", err)
	}
	orchContent := []byte("---\nrun_id: test\nhuman_approved: false\n---\n# Existing run history\n")
	orchFile := filepath.Join(runFolder, "Orchestration.md")
	if err := os.WriteFile(orchFile, orchContent, 0o600); err != nil {
		t.Fatalf("setup: write Orchestration.md: %v", err)
	}

	// The memStore simulates a valid existing artifact so the session proceeds
	// past the artifact-read step and reaches pre-consultation.
	store := &memStore{
		state: domain.ArtifactState{
			Workflow:        "linear",
			WorkflowVersion: "1.0",
			Task:            "test task",
			GlobalSequence:  1,
			RunSettings: domain.RunSettings{
				Mode:            domain.ExecutionModeAuto,
				PreConsultation: true,
			},
		},
		exists: true,
	}

	// Inject a preConsultant that returns a HarnessLaunchError — the exact error
	// the harness adapters will produce when the subprocess cannot be started.
	launchErr := &domain.HarnessLaunchError{
		Harness:    "ghcp-cli",
		Executable: "/usr/bin/copilot",
		Err:        fmt.Errorf("exec: no such file or directory"),
	}
	preConsultant := &scriptedPreConsultant{err: launchErr}

	f := harness.NewMockAdapter()
	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	cfg := baseLinearConfig(orchPath)
	markResume(&cfg)
	cfg.RunFolder = runFolder
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	// The run must stop as a resumable start failure.
	requireStartFailed(t, got, err)

	// The Cause field must carry the HarnessLaunchError so the TUI can detect
	// a launch failure via errors.As without parsing the message text.
	var found *domain.HarnessLaunchError
	if !errors.As(got.Cause, &found) {
		t.Error("outcome.Cause does not carry *domain.HarnessLaunchError; " +
			"the TUI relies on errors.As(outcome.Cause, &le) to route to the " +
			"override screen — without it, the override screen is never reached")
	}

	// The run folder must be preserved byte-for-byte. On IsNewRun=false the
	// folder predates this attempt (it contains the user's run history) and the
	// session must never delete it on pre-consultation failure.
	if _, statErr := os.Stat(runFolder); os.IsNotExist(statErr) {
		t.Error("run folder was removed on IsNewRun=false pre-consultation failure; " +
			"a resumed run's folder must never be deleted — it contains existing run history " +
			"that the override restart depends on")
	}
	gotContent, readErr := os.ReadFile(orchFile)
	if readErr != nil {
		t.Fatalf("Orchestration.md could not be read after pre-consultation failure: %v", readErr)
	}
	if string(gotContent) != string(orchContent) {
		t.Errorf("Orchestration.md content changed after pre-consultation failure; "+
			"want byte-for-byte preservation, got %q", gotContent)
	}
}

// TestSession_Start_PreConsultation_LaunchFailure_IsNewRunTrue_RunFolderKept
// verifies that when a pre-consultation fails with a *domain.HarnessLaunchError
// on a new run (IsNewRun=true), the session:
//  1. Returns RunStartFailed,
//  2. Carries the HarnessLaunchError in outcome.Cause, and
//  3. Keeps the run folder and the artifact, so the failed start is resumable
//     after the user fixes the launch problem.
func TestSession_Start_PreConsultation_LaunchFailure_IsNewRunTrue_RunFolderKept(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	// Create the run folder that the session must remove on new-run failure.
	runFolder := filepath.Join(dir, "Orchestration-20260817T140615Z-new")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: create run folder: %v", err)
	}

	launchErr := &domain.HarnessLaunchError{
		Harness:    "ghcp-cli",
		Executable: "/usr/bin/copilot",
		Err:        fmt.Errorf("exec: no such file or directory"),
	}
	preConsultant := &scriptedPreConsultant{err: launchErr}

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:    f,
		Store:      store,
		Clock:      fixedClock{t: epoch},
		Interact:   &noopInteraction{},
		PreConsult: preConsultant,
	})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = true
	cfg.RunFolder = runFolder
	cfg.Mode = domain.ExecutionModeAuto
	cfg.PreConsultation = true

	got, err := ses.Start(context.Background(), cfg)

	// The run must stop as a resumable start failure.
	requireStartFailed(t, got, err)

	// The Cause field must carry the HarnessLaunchError so the TUI can display
	// the override screen regardless of whether the failure was on a new or resumed run.
	var found *domain.HarnessLaunchError
	if !errors.As(got.Cause, &found) {
		t.Error("outcome.Cause does not carry *domain.HarnessLaunchError on IsNewRun=true; " +
			"the TUI uses the same errors.As check on all pre-consultation failures " +
			"and must detect the launch-failure identity in both cases")
	}

	// On a new run the folder must also be kept: the artifact exists and a
	// resume retries pre-consultation.
	if _, statErr := os.Stat(runFolder); statErr != nil {
		t.Errorf("run folder was removed on IsNewRun=true pre-consultation failure; "+
			"a failed start must keep its run folder so it can be resumed (stat error: %v)", statErr)
	}
	if !store.exists {
		t.Error("artifact missing after IsNewRun=true pre-consultation failure; the artifact must be kept")
	}
}
