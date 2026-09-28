package session_test

// Tests for seed input validation and resume bypass.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Seeding: pre-creation validation =====
//
// For a new run, the session must build and validate the seed plan before calling
// Store.Create so that an invalid seed set leaves no run folder on disk. These
// tests use the in-memory store; actual disk rollback is exercised in the
// integration tests.
//
// RED phase: all tests fail until the session inserts seed.NewPlan before
// Store.Create in the run-start sequence and returns a run refusal on any
// planning error.

// TestSession_Start_SeedInputs_MissingSource_RefusesBeforeStoreCreate verifies
// that a seed source path that does not exist on disk produces a RunRefused
// outcome whose message names the path, and that Store.Create is never called.
func TestSession_Start_SeedInputs_MissingSource_RefusesBeforeStoreCreate(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	nonExistent := filepath.Join(t.TempDir(), "nonexistent-input.md")
	cfg := baseLinearConfig(orchPath)
	cfg.SeedInputs = []string{nonExistent}

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	// Store.Create must not have been called (planning must precede creation).
	if store.exists {
		t.Error("want Store.Create NOT called before seed planning refusal, but store.exists is true")
	}
	if !strings.Contains(msg, "nonexistent-input.md") {
		t.Errorf("want refusal message to name the missing source path; got %q", msg)
	}
}

// TestSession_Start_SeedInputs_RunnerManagedDestination_RefusesBeforeStoreCreate
// verifies that a source file whose base name matches a runner-managed destination
// ("Orchestration.md") produces a RunRefused before Store.Create is called.
func TestSession_Start_SeedInputs_RunnerManagedDestination_RefusesBeforeStoreCreate(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	// A source named "Orchestration.md" maps to the runner-managed destination.
	seedDir := t.TempDir()
	src := filepath.Join(seedDir, "Orchestration.md")
	if err := os.WriteFile(src, []byte("# should be rejected\n"), 0600); err != nil {
		t.Fatalf("write seed source: %v", err)
	}
	// A Requirement* candidate keeps the seed-naming rule from masking the
	// runner-managed-destination refusal this test targets.
	reqSrc := filepath.Join(seedDir, "Requirement.md")
	if err := os.WriteFile(reqSrc, []byte("# req\n"), 0600); err != nil {
		t.Fatalf("write seed source: %v", err)
	}

	cfg := baseLinearConfig(orchPath)
	cfg.SeedInputs = []string{src, reqSrc}

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	if store.exists {
		t.Error("want Store.Create NOT called for runner-managed destination refusal, but store.exists is true")
	}
	if !strings.Contains(msg, "Orchestration.md") {
		t.Errorf("want refusal message to name the runner-managed destination (Orchestration.md); got %q", msg)
	}
}

// TestSession_Start_SeedInputs_CrossSourceCollision_RefusesBeforeStoreCreate
// verifies that two source files with the same base name -- producing a
// destination collision -- refuse the run before Store.Create is called, and
// that the refusal message names both offending sources.
func TestSession_Start_SeedInputs_CrossSourceCollision_RefusesBeforeStoreCreate(t *testing.T) {
	ses, _, store, orchPath := newLinearSession(t)

	d := t.TempDir()
	src1 := filepath.Join(d, "dirA", "Plan.md")
	src2 := filepath.Join(d, "dirB", "Plan.md")
	for _, p := range []string{src1, src2} {
		if mkErr := os.MkdirAll(filepath.Dir(p), 0700); mkErr != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), mkErr)
		}
		if wErr := os.WriteFile(p, []byte("content\n"), 0600); wErr != nil {
			t.Fatalf("write %s: %v", p, wErr)
		}
	}
	// A Requirement* candidate keeps the seed-naming rule from masking the
	// cross-source collision refusal this test targets.
	reqSrc := filepath.Join(d, "Requirement.md")
	if wErr := os.WriteFile(reqSrc, []byte("req\n"), 0600); wErr != nil {
		t.Fatalf("write %s: %v", reqSrc, wErr)
	}

	cfg := baseLinearConfig(orchPath)
	cfg.SeedInputs = []string{src1, src2, reqSrc}

	got, err := ses.Start(context.Background(), cfg)

	msg := requireRefused(t, got, err)

	if store.exists {
		t.Error("want Store.Create NOT called for cross-source collision, but store.exists is true")
	}
	// Both offending source roots must appear in the refusal message.
	if !strings.Contains(msg, "dirA") || !strings.Contains(msg, "dirB") {
		t.Errorf("want refusal message to name both colliding sources; got %q", msg)
	}
}

// ===== Seeding: resume skips seeding entirely =====

// TestSession_Start_Resume_SeedInputsPopulated_ValidationAndCopySkipped verifies
// that when IsNewRun is false, SeedInputs is ignored entirely -- no NewPlan
// validation, no Apply copy -- even when SeedInputs contains paths that would
// fail validation. A resumed run must never be refused for seed-related reasons.
func TestSession_Start_Resume_SeedInputsPopulated_ValidationAndCopySkipped(t *testing.T) {
	ses, f, store, orchPath := newLinearSession(t)

	// Set up a resumable artifact: agent-a completed, agent-b pending.
	store.state = domain.ArtifactState{
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		GlobalSequence:  1,
		RunSettings:     domain.RunSettings{Mode: domain.ExecutionModeAuto},
		CurrentState: domain.CurrentState{
			Phase:      "PLANNING",
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  "agent-a#1",
		},
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "agent-a#1", Phase: "PLANNING", Status: domain.StatusSUCCESS},
		},
	}
	store.exists = true

	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseLinearConfig(orchPath)
	cfg.IsNewRun = false
	// A non-existent path: would refuse the run if validation ran on resume.
	cfg.SeedInputs = []string{filepath.Join(t.TempDir(), "does-not-exist.md")}

	got, err := ses.Start(context.Background(), cfg)

	// The run must complete normally: SeedInputs is silently ignored on resume.
	requireRunStatus(t, got, err, domain.RunCompleted)
}
