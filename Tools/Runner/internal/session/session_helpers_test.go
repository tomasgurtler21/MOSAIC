package session_test

// Shared test helpers for session tests: fixture path construction,
// agent-file creation, session builder, config factory, and assertion helpers.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ---- test data paths ----

const testdataDir = "../../testdata/session"

func orchFilePath(name string) string {
	return filepath.Join(testdataDir, name)
}

// ---- test helpers ----

// writeAgentFile creates a minimal agent definition file in dir with the
// given agent identifier as the filename stem.
func writeAgentFile(t *testing.T, dir, agentID string) {
	t.Helper()
	path := filepath.Join(dir, agentID+".md")
	if err := os.WriteFile(path, []byte("# Agent: "+agentID+"\n"), 0600); err != nil {
		t.Fatalf("writeAgentFile(%q): %v", agentID, err)
	}
}

// copyOrchestratorFile copies the named fixture file from testdata/session
// into the given directory and returns the destination path.
func copyOrchestratorFile(t *testing.T, dir, fixtureName string) string {
	t.Helper()
	src := orchFilePath(fixtureName)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("copyOrchestratorFile: read %q: %v", src, err)
	}
	dst := filepath.Join(dir, "orchestrator.md")
	if err := os.WriteFile(dst, data, 0600); err != nil {
		t.Fatalf("copyOrchestratorFile: write %q: %v", dst, err)
	}
	return dst
}

// newLinearSession builds a session backed by the linear-orch.md fixture.
// It creates agent files for agent-a and agent-b in the temp dir.
// The MockAdapter and memStore are returned so tests can configure them.
func newLinearSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// baseLinearConfig returns a RunConfig for the linear workflow in the given
// orchestrator file directory. IsNewRun is true (new run) by default; tests
// that exercise resume behaviour should override it with cfg.IsNewRun = false.
func baseLinearConfig(orchPath string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeAuto,
		},
	}
}

// requireRunStatus asserts that the RunOutcome has the expected status and no
// unexpected error.
func requireRunStatus(t *testing.T, got domain.RunOutcome, err error, want domain.RunStatus) {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != want {
		t.Errorf("want Status=%q, got %q (message: %q)", want, got.Status, got.Message)
	}
}

// requireRefused asserts that Start returned a RunRefused outcome with a nil
// error. Pre-invocation refusals are encoded in the RunOutcome (not as errors)
// so frontends can present them without error-branch handling.
func requireRefused(t *testing.T, got domain.RunOutcome, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error for refusal (refusals are encoded in RunOutcome), got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused status, got %q (message: %q)", got.Status, got.Message)
	}
	return got.Message
}

// newCheckpointAgentSession builds a session backed by the
// checkpoint-agent-orch.md fixture, which declares a checkpoint-class
// infrastructure agent. Agent files for agent-a and agent-b are written into
// the temp dir. The MockAdapter and memStore are returned for test configuration.
func newCheckpointAgentSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "checkpoint-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// newIntervalAgentSession builds a session backed by the interval-agent-orch.md
// fixture, which declares a checkpoint-class infrastructure agent
// (checkpoint-manager-git) with an INVOCATION_INTERVAL:1 trigger (fires after
// every workflow step). Agent files for agent-a and agent-b are written into
// the temp dir.
func newIntervalAgentSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "interval-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "checkpoint-manager-git")

	f = harness.NewMockAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// newCommitSession builds a session backed by the commit-agent-orch.md fixture,
// which declares a linear workflow (agent-a, agent-b) and a commit-class
// infrastructure agent (commit-manager-git). Agent definition files for all
// three are created in the temp directory. The MockAdapter, memStore, and
// orchestrator path are returned so tests can configure their scripted responses.
func newCommitSession(t *testing.T) (ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "commit-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	f = harness.NewMockAdapter()
	store = &memStore{}

	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// baseCommitConfig returns a RunConfig for the commit-agent-orch.md workflow
// with commits enabled and mode set to auto. Tests override individual fields
// as needed.
func baseCommitConfig(orchPath string) domain.RunConfig {
	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
	}
	cfg.Mode = domain.ExecutionModeAuto
	cfg.Commits = true
	cfg.CommitBranchVariant = domain.CommitBranchMOSAICOwned
	return cfg
}

// baseOrchestratedConfig returns a RunConfig for the linear workflow in
// orchestrated mode.
func baseOrchestratedConfig(orchPath string) domain.RunConfig {
	return domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings: domain.RunSettings{
			Mode: domain.ExecutionModeOrchestrated,
		},
	}
}

// newOrchestratedSession builds a session backed by the linear-orch.md fixture
// in orchestrated mode, using the supplied RoutingConsultant.
func newOrchestratedSession(t *testing.T, consultant domain.RoutingConsultant) (
	ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// newHITLLinearSession builds a session backed by the hitl-linear-orch.md
// fixture (agent-a has HITL=true) using the supplied RoutingConsultant and
// ApprovalReader.
func newHITLLinearSession(t *testing.T, consultant domain.RoutingConsultant, approvals domain.ApprovalReader) (
	ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: approvals,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// newAutoHITLSession builds a session backed by the hitl-linear-orch.md fixture
// running in auto-execution mode, using the supplied ApprovalReader. An optional
// RoutingConsultant may be wired for tests that exercise the HITL escalation path.
func newAutoHITLSession(t *testing.T, approvals domain.ApprovalReader, consultant domain.RoutingConsultant) (
	ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: approvals,
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})
	return
}

// requireHITLRejectedSteps asserts that store.Applied contains at least one step
// with HITLRejected=true, that every such step carries IsInfrastructure=true and
// nil OutputArtifacts, and that all Applied Seq values are strictly increasing.
func requireHITLRejectedSteps(t *testing.T, store *memStore) {
	t.Helper()

	var rejected []domain.CompletedStep
	for _, s := range store.Applied {
		if s.HITLRejected {
			rejected = append(rejected, s)
		}
	}
	if len(rejected) == 0 {
		t.Errorf("want at least one HITLRejected=true step in store.Applied, got none; "+
			"HITL-rejected dispatches must be persisted to the execution log before redispatch")
	}
	for _, s := range rejected {
		if !s.IsInfrastructure {
			t.Errorf("HITLRejected step %q: want IsInfrastructure=true, got false; "+
				"rejected steps must not update current_state.LastAgent",
				s.AgentInstance)
		}
		if s.OutputArtifacts != nil {
			t.Errorf("HITLRejected step %q: want OutputArtifacts=nil, got %v; "+
				"rejected steps must not pollute the artifact registry with non-compliant paths",
				s.AgentInstance, s.OutputArtifacts)
		}
	}

	// All Applied Seq values must be strictly increasing, regardless of whether
	// steps are workflow, infrastructure, or rejected.
	for i := 1; i < len(store.Applied); i++ {
		if store.Applied[i].Seq <= store.Applied[i-1].Seq {
			t.Errorf("store.Applied[%d].Seq=%d is not greater than Applied[%d].Seq=%d; "+
				"sequence numbers must be strictly increasing across all persisted dispatches",
				i, store.Applied[i].Seq, i-1, store.Applied[i-1].Seq)
		}
	}
}

// ---- small utilities ----

func containsInput(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}
