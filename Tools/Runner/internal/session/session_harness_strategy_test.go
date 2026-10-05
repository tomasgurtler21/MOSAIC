package session_test

// Tests for harness strategy selection (copy-and-invoke vs backup-and-transform
// vs skip) and the FR-21 expanded cleanup for the remaining CLI-harness terminal
// outcomes (RunFailed, RunRefused, RunStoppedByConsultant).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
	"mosaic-run/internal/snapshot/backup"
)

// ---- Strategy selection ----

// TestSession_Start_CLIHarness_StrategySelection_ClaudeCodeUsesCopyAndInvoke
// verifies that the claude-code harness (LoadingMechanismPath) uses the
// copy-and-invoke strategy. Agents are dispatched from a run-scoped snapshot
// directory, confirming copy-and-invoke is active. No backup directory is
// created because backup-and-transform is for name-based harnesses only.
func TestSession_Start_CLIHarness_StrategySelection_ClaudeCodeUsesCopyAndInvoke(t *testing.T) {
	const runID = "strat-cc-copy-01"
	workDir, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseCLIHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Agents must be dispatched from the snapshot dir (copy-and-invoke strategy).
	invs := f.Invocations()
	if len(invs) == 0 {
		t.Fatal("want at least one invocation, got none")
	}
	for _, inv := range invs {
		if !containsSnapshotPathSegment(inv.Agent.DefinitionPath, runID) {
			t.Errorf("agent %q: DefinitionPath %q does not contain snapshot path segment; "+
				"claude-code (LoadingMechanismPath) must use copy-and-invoke strategy, "+
				"dispatching agents from the run-scoped snapshot directory",
				inv.Agent.Identifier, inv.Agent.DefinitionPath)
		}
	}

	// No backup directory must exist for path-based harnesses.
	expectedBackupDir := filepath.Join(workDir, ".claude", backup.BackupDirName)
	if _, statErr := os.Stat(expectedBackupDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("backup dir %q must not be created for claude-code (copy-and-invoke); "+
			"backup-and-transform is for name-based harnesses only; stat returned: %v",
			expectedBackupDir, statErr)
	}
	_ = snapshotDir // confirmed deleted by snapshot cleanup test; not the focus here
}

// TestSession_Start_OpenCodeHarness_StrategySelection_UsesBackupAndTransform
// verifies that the opencode harness (LoadingMechanismName, non-nil rules)
// uses the backup-and-transform strategy. During dispatch, agent files must
// be transformed in-place (mode=primary). After the run, originals are
// restored (mode=subagent) and the backup directory is removed.
//
// A test-local agentContentScanHarness wrapper reads the agent file during
// Invoke to confirm the transform is active at dispatch time.
func TestSession_Start_OpenCodeHarness_StrategySelection_UsesBackupAndTransform(t *testing.T) {
	const runID = "strat-oc-bat-01"
	workDir, orchPath, backupDir := writeOpenCodeHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".opencode", "agents")

	delegate := harness.NewMockAdapter()
	delegate.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	delegate.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	scanner := &agentContentScanHarness{delegate: delegate}

	ses := session.New(session.Deps{
		Harness:  scanner,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseOpenCodeHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	// During dispatch, agent-a.md must have been transformed in-place:
	// mode=subagent -> mode=primary (opencode transform rule).
	contentDuringRun := scanner.contentFor("agent-a")
	if len(contentDuringRun) == 0 {
		t.Fatal("agentContentScanHarness: no content captured for agent-a; did Invoke fire?")
	}
	if !strings.Contains(string(contentDuringRun), "primary") {
		t.Errorf("agent-a.md content at dispatch time: want mode=primary (in-place transform), "+
			"got %q; backup-and-transform must apply transforms before the first dispatch",
			string(contentDuringRun))
	}
	if strings.Contains(string(contentDuringRun), "subagent") {
		t.Errorf("agent-a.md content at dispatch time: still contains mode=subagent; " +
			"the in-place transform must have replaced it with mode=primary before dispatch")
	}

	// Agents must NOT be dispatched from a snapshot dir (no copy-and-invoke for opencode).
	for _, inv := range delegate.Invocations() {
		if strings.Contains(filepath.ToSlash(inv.Agent.DefinitionPath), "agents-runner-") {
			t.Errorf("agent %q: DefinitionPath %q contains snapshot path segment; "+
				"opencode uses backup-and-transform (in-place), not copy-and-invoke",
				inv.Agent.Identifier, inv.Agent.DefinitionPath)
		}
	}

	// AC10.12: backup-and-transform must NOT re-resolve orchRef. The orchestrator
	// file is transformed in-place at its original path, so all dispatched agents
	// (including the orchestrator) must be resolved from the original agentsDir,
	// not from any copy.
	for _, inv := range delegate.Invocations() {
		if !strings.HasPrefix(
			filepath.Clean(inv.Agent.DefinitionPath),
			filepath.Clean(agentsDir)+string(os.PathSeparator),
		) {
			t.Errorf("agent %q: DefinitionPath %q is not under the original agentsDir %q; "+
				"backup-and-transform must dispatch from original paths without re-binding orchRef",
				inv.Agent.Identifier, inv.Agent.DefinitionPath, agentsDir)
		}
	}

	// After RunCompleted, cleanup must have restored originals and deleted the
	// backup directory (last-out check: sole run, so restore and clean up).
	agentAPath := filepath.Join(agentsDir, "agent-a.md")
	contentAfterRun, readErr := os.ReadFile(agentAPath)
	if readErr != nil {
		t.Fatalf("read agent-a.md after run: %v", readErr)
	}
	if !strings.Contains(string(contentAfterRun), "subagent") {
		t.Errorf("agent-a.md after run: want mode=subagent (original restored by Cleanup), "+
			"got %q; last-out check must restore the original before removing the backup",
			string(contentAfterRun))
	}

	if _, statErr := os.Stat(backupDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("backup dir %q must be deleted after RunCompleted (last-out cleanup); "+
			"stat returned: %v", backupDir, statErr)
	}
}

// TestSession_Start_GHCPCLIHarness_NilRules_SkipsBackupAndTransform verifies
// that the ghcp-cli harness (LoadingMechanismName, nil transform rules) skips
// backup-and-transform entirely (FR-3). Agents are dispatched from the original
// agents directory without any transformation, and no backup directory is
// created.
//
// TransformationsFor("ghcp-cli") returns nil (no rules). SetupBackupAndTransform
// with nil rules returns (nil, nil), and the session skips the backup step.
func TestSession_Start_GHCPCLIHarness_NilRules_SkipsBackupAndTransform(t *testing.T) {
	const runID = "ghcp-skip-01"
	workDir, orchPath := writeGHCPCLIHarnessDir(t)
	agentsDir := filepath.Join(workDir, ".github", "agents")

	// Read original content of agent-a.md before the run to compare later.
	originalContent, err := os.ReadFile(filepath.Join(agentsDir, "agent-a.md"))
	if err != nil {
		t.Fatalf("read agent-a.md before run: %v", err)
	}

	delegate := harness.NewMockAdapter()
	delegate.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	delegate.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses := session.New(session.Deps{
		Harness:  delegate,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseGHCPCLIHarnessConfig(orchPath, runID))
	requireRunStatus(t, got, err, domain.RunCompleted)

	// Agents must be dispatched from the original agents directory (no snapshot,
	// no copy-and-invoke, no in-place transform for ghcp-cli).
	for _, inv := range delegate.Invocations() {
		if strings.Contains(filepath.ToSlash(inv.Agent.DefinitionPath), "agents-runner-") {
			t.Errorf("agent %q: DefinitionPath %q contains snapshot path segment; "+
				"ghcp-cli (nil rules) must skip backup-and-transform entirely and "+
				"dispatch from the original agents directory",
				inv.Agent.Identifier, inv.Agent.DefinitionPath)
		}
		if strings.Contains(filepath.ToSlash(inv.Agent.DefinitionPath), backup.BackupDirName) {
			t.Errorf("agent %q: DefinitionPath %q references backup directory; "+
				"ghcp-cli must not use backup-and-transform",
				inv.Agent.Identifier, inv.Agent.DefinitionPath)
		}
	}

	// No backup directory must exist for ghcp-cli (nil rules, FR-3 skip).
	expectedBackupDir := filepath.Join(workDir, ".github", backup.BackupDirName)
	if _, statErr := os.Stat(expectedBackupDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("backup dir %q must not be created for ghcp-cli (nil rules, FR-3 skip); "+
			"stat returned: %v", expectedBackupDir, statErr)
	}

	// Agent files must not have been modified (no in-place transforms for ghcp-cli).
	contentAfterRun, readErr := os.ReadFile(filepath.Join(agentsDir, "agent-a.md"))
	if readErr != nil {
		t.Fatalf("read agent-a.md after run: %v", readErr)
	}
	if string(contentAfterRun) != string(originalContent) {
		t.Errorf("agent-a.md was modified during a ghcp-cli run; " +
			"nil-rules harnesses must not transform agent files")
	}
}

// ---- FR-21 expanded cleanup: remaining CLIHarness terminal outcomes ----

// TestSession_Start_CLIHarness_SnapshotDeletedOnRunFailed verifies that the
// copy-and-invoke snapshot directory is deleted when the run ends with RunFailed
// (FR-21: all six terminal outcomes trigger cleanup).
//
// RunFailed is triggered by forcing store.Apply to fail on the first call after
// the snapshot has been created and agent-a has returned SUCCESS.
func TestSession_Start_CLIHarness_SnapshotDeletedOnRunFailed(t *testing.T) {
	const runID = "testsnap-cleanup-failed-01"
	_, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	failStore := &memStore{
		applyErrOnFirst: true,
		applyFirstErr:   errors.New("test: forced apply failure to trigger RunFailed"),
	}

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    failStore,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	got, err := ses.Start(context.Background(), baseCLIHarnessConfig(orchPath, runID))
	// RunFailed returns a non-nil error; we verify the status directly.
	if err == nil {
		t.Error("want non-nil error for RunFailed, got nil")
	}
	if got.Status != domain.RunFailed {
		t.Errorf("want RunFailed, got %q (message: %q)", got.Status, got.Message)
	}

	// Snapshot must be deleted on RunFailed (FR-21: all terminal outcomes clean up).
	if _, statErr := os.Stat(snapshotDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("snapshot directory %q must be deleted after RunFailed "+
			"(FR-21: all six terminal outcomes trigger copy-and-invoke cleanup); "+
			"stat returned: %v", snapshotDir, statErr)
	}
}

// TestSession_Start_CLIHarness_SnapshotDeletedOnRunRefused verifies that the
// copy-and-invoke snapshot directory is deleted when the run is refused after
// the snapshot was created (FR-21). The refusal is triggered at step 7.5a
// (mode=unset), which runs after step 5b (snapshot setup).
func TestSession_Start_CLIHarness_SnapshotDeletedOnRunRefused(t *testing.T) {
	const runID = "testsnap-cleanup-refused-01"
	_, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)

	ses := session.New(session.Deps{
		Harness:  harness.NewMockAdapter(),
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// Override mode to unset: triggers refusal at step 7.5a, which is AFTER
	// the snapshot is created at step 5b. The deferred cleanup must still run.
	cfg := baseCLIHarnessConfig(orchPath, runID)
	cfg.RunSettings.Mode = domain.ExecutionModeUnset

	got, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error for RunRefused (refusals use nil error), got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused, got %q (message: %q)", got.Status, got.Message)
	}

	// The snapshot was created at step 5b, then the run was refused. FR-21
	// requires cleanup on all terminal outcomes including RunRefused.
	if _, statErr := os.Stat(snapshotDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("snapshot directory %q must be deleted after RunRefused "+
			"(FR-21: all six terminal outcomes trigger copy-and-invoke cleanup); "+
			"stat returned: %v", snapshotDir, statErr)
	}
}

// TestSession_Start_CLIHarness_SnapshotDeletedOnRunStoppedByConsultant verifies
// that the copy-and-invoke snapshot directory is deleted when the routing
// consultant issues a stop instruction (RunStoppedByConsultant), which is one
// of the six terminal outcomes covered by FR-21.
func TestSession_Start_CLIHarness_SnapshotDeletedOnRunStoppedByConsultant(t *testing.T) {
	const runID = "testsnap-cleanup-consultant-01"
	_, orchPath, snapshotDir := writeCLIHarnessDir(t, runID)

	consultant := &scriptedRoutingConsultant{}
	// First dispatch: send agent-a so the run enters the dispatch loop (past step 5b).
	consultant.queueDispatch("agent-a", "test task", 0)
	// Second call: stop the run after agent-a returns.
	consultant.queueStop("FR-21 cleanup test stop")

	f := harness.NewMockAdapter()
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	ses := session.New(session.Deps{
		Harness:  f,
		Store:    &memStore{},
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
		Routing:  consultant,
	})

	cfg := baseCLIHarnessConfig(orchPath, runID)
	cfg.RunSettings.Mode = domain.ExecutionModeOrchestrated

	got, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant, got %q (message: %q)", got.Status, got.Message)
	}

	// Snapshot must be deleted on RunStoppedByConsultant (FR-21).
	if _, statErr := os.Stat(snapshotDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("snapshot directory %q must be deleted after RunStoppedByConsultant "+
			"(FR-21: all six terminal outcomes trigger copy-and-invoke cleanup); "+
			"stat returned: %v", snapshotDir, statErr)
	}
}
