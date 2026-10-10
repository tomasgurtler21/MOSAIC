package session_test

// Shared helpers for the mechanical-retry session tests: scripted harness
// entries, row counting over the recorded steps, and a run seeded through the
// real artifact file store.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// autoModes are the execution modes in which the engine re-dispatches
// mechanically.
var autoModes = []domain.ExecutionMode{domain.ExecutionModeAuto, domain.ExecutionModeAutoReview}

// configForMode returns the linear-workflow config for a new run in mode.
func configForMode(orchPath string, mode domain.ExecutionMode) domain.RunConfig {
	cfg := baseLinearConfig(orchPath)
	cfg.RunSettings.Mode = mode
	return cfg
}

// statusEntry scripts a harness reply with the given status, error code and message.
func statusEntry(status domain.StatusCode, code domain.ErrorCode, msg string) harness.ScriptedEntry {
	return harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		StatusCode:    status,
		ErrorCode:     code,
		StatusMessage: msg,
		ErrorReason:   msg,
	}}
}

// queueRepeated queues n copies of entry for agent.
func queueRepeated(f *harness.MockAdapter, agent string, n int, entry harness.ScriptedEntry) {
	for i := 0; i < n; i++ {
		f.Queue(agent, entry)
	}
}

// agentRows returns the workflow rows recorded for agent, in order.
func agentRows(store *memStore, agent string) []domain.CompletedStep {
	var rows []domain.CompletedStep
	for _, s := range store.Applied {
		if !s.IsInfrastructure && !s.HITLRejected && strings.HasPrefix(s.AgentInstance, agent+"#") {
			rows = append(rows, s)
		}
	}
	return rows
}

// countE501RowsFor counts the BLOCKED/E501 workflow rows recorded for agent.
func countE501RowsFor(store *memStore, agent string) int {
	n := 0
	for _, s := range agentRows(store, agent) {
		if s.Status == domain.StatusBLOCKED && s.ErrorCode == domain.ErrorTOOL_UNAVAILABLE {
			n++
		}
	}
	return n
}

// seedRow is one workflow row to pre-record for the first routing-table row.
type seedRow struct {
	status domain.StatusCode
	code   domain.ErrorCode
}

// seededRun is a run recorded through the real artifact file store.
type seededRun struct {
	orchPath     string
	artifactPath string
	runFolder    string
}

// emptyFileStoreRun lays out the linear workflow and the run folder on disk
// without creating the artifact.
func emptyFileStoreRun(t *testing.T) seededRun {
	t.Helper()
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	runFolder := filepath.Join(dir, domain.RunScopedFolder(testRunID))
	if err := os.MkdirAll(runFolder, 0o700); err != nil {
		t.Fatalf("emptyFileStoreRun: %v", err)
	}
	return seededRun{orchPath: orchPath, artifactPath: filepath.Join(runFolder, "Orchestration.md"), runFolder: runFolder}
}

// newRunConfig returns the request that starts a new auto-mode run on the
// run's file store.
func (r seededRun) newRunConfig() domain.RunConfig {
	cfg := baseLinearConfig(r.orchPath)
	cfg.RunID = testRunID
	cfg.RunFolder = r.runFolder
	return cfg
}

// seedFileStoreRun creates a linear-workflow run in auto mode on disk and
// records rows as agent-a steps of the first routing-table row, through the
// real artifact file store. The error code of a BLOCKED row travels only through
// the Execution Log's Summary marker.
func seedFileStoreRun(t *testing.T, rows []seedRow) seededRun {
	t.Helper()
	run := emptyFileStoreRun(t)
	artifactPath := run.artifactPath
	store := artifact.NewFileStore(artifactPath)
	ctx := context.Background()
	state, err := store.Create(ctx, domain.WorkflowInfo{ID: "linear", Version: "1.0"}, "test task",
		domain.RunSettings{Mode: domain.ExecutionModeAuto}, epoch, testRunID)
	if err != nil {
		t.Fatalf("seedFileStoreRun: create: %v", err)
	}
	for i, r := range rows {
		state, err = store.Apply(ctx, state, domain.CompletedStep{
			Seq:           i + 1,
			AgentInstance: fmt.Sprintf("agent-a#%d", i+1),
			Phase:         "PLANNING",
			WorkflowRow:   domain.WorkflowRowFromIndex(0),
			Status:        r.status,
			ErrorCode:     r.code,
			Summary:       "seeded attempt",
			Timestamp:     epoch,
		})
		if err != nil {
			t.Fatalf("seedFileStoreRun: apply row %d: %v", i+1, err)
		}
	}
	return run
}

// resumeConfig returns the resume request for the seeded run.
func (r seededRun) resumeConfig() domain.RunConfig {
	cfg := baseLinearConfig(r.orchPath)
	cfg.IsNewRun = false
	cfg.RunID = testRunID
	cfg.RunFolder = r.runFolder
	return cfg
}

// newSession builds a session on the seeded run's file store.
func (r seededRun) newSession(f *harness.MockAdapter, consultant domain.RoutingConsultant) session.Session {
	return session.New(session.Deps{
		Harness:  f,
		Store:    artifact.NewFileStore(r.artifactPath),
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
}

// withMechanicalFollowUp adds the attempt the engine dispatches after a
// PARTIALLY_DONE in the auto modes. Routing follows the original attempt's
// status, also when a gate re-dispatch repaired it. The follow-up succeeds
// without writing anything; extra is the number of attempts added.
func withMechanicalFollowUp(kind dispatchPathKind, attempts []attempt) (out []attempt, extra int) {
	if kind != pathAuto || attempts[0].status != domain.StatusPARTIALLY_DONE {
		return attempts, 0
	}
	return append(append([]attempt(nil), attempts...), attempt{status: domain.StatusSUCCESS}), 1
}
