package session_test

// Tests for commit setup dispatch at run start.
// Covers: branch marker extraction, first-log-row recording, harness errors,
// Apply failures, disabled commits, and .agent.md extension resolution.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== Commit setup dispatch =====

// TestSession_Start_CommitsEnabled_SuccessfulSetup_BranchRecordedInArtifact
// verifies that when commits are enabled and the commit-class agent returns a
// [branch:{name}] marker in its status_message, the reported branch name is
// stored as CommitBranch in the created artifact. This is the core success path
// for the commit setup dispatch.
func TestSession_Start_CommitsEnabled_SuccessfulSetup_BranchRecordedInArtifact(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	const wantBranch = "mosaic/run/test-run-id"

	// Commit setup dispatch returns a branch marker.
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch established [branch:" + wantBranch + "]",
	}})
	// Workflow agents for the dispatch loop that follows setup.
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"

	ses.Start(context.Background(), cfg) //nolint:errcheck

	if store.state.CommitBranch != wantBranch {
		t.Errorf("want CommitBranch=%q in artifact after successful commit setup, got %q",
			wantBranch, store.state.CommitBranch)
	}
}

// TestSession_Start_CommitsEnabled_SuccessfulSetup_IsFirstLogRow verifies that
// the commit setup dispatch is recorded as the first execution log row, with
// IsInfrastructure=true, before any workflow step is logged.
func TestSession_Start_CommitsEnabled_SuccessfulSetup_IsFirstLogRow(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)

	const wantBranch = "mosaic/run/test-run-id"

	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch ready [branch:" + wantBranch + "]",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"

	ses.Start(context.Background(), cfg) //nolint:errcheck

	// store.Applied records every CompletedStep passed to Apply, including
	// infrastructure steps (IsInfrastructure=true). The commit setup dispatch
	// must be the first applied step and must be marked as infrastructure.
	if len(store.Applied) == 0 {
		t.Fatal("want at least one applied step, got none")
	}
	first := store.Applied[0]
	if !first.IsInfrastructure {
		t.Errorf("want first applied step to be infrastructure (commit setup), got IsInfrastructure=false (agent=%q)", first.AgentInstance)
	}
	if !strings.Contains(first.AgentInstance, "commit-manager-git") {
		t.Errorf("want first applied step agent to contain \"commit-manager-git\", got %q", first.AgentInstance)
	}
	// Seq must be 1: the commit setup dispatch is the first recorded row.
	// The ContractsDesign specifies the commit setup dispatch Seq is always 1.
	if first.Seq != 1 {
		t.Errorf("want commit setup dispatch Seq=1 (first row), got Seq=%d", first.Seq)
	}
	// Status must mirror the dispatch's own status code (SUCCESS in this case).
	if first.Status != domain.StatusSUCCESS {
		t.Errorf("want commit setup dispatch Status=%q, got %q", domain.StatusSUCCESS, first.Status)
	}
}

// ===== Artifact-first ordering =====

// TestSession_Start_CommitsEnabled_ArtifactExistsBeforeSetupDispatch verifies
// the durable state at the moment the commit setup agent and the first workflow
// agent are dispatched. At the setup dispatch the artifact already exists with
// commits enabled, no commit_branch and no log rows. At the first workflow
// dispatch the setup row is recorded with Seq 1, commit_branch is set and
// current_state still names no workflow step.
func TestSession_Start_CommitsEnabled_ArtifactExistsBeforeSetupDispatch(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "commit-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	writeAgentFile(t, dir, "commit-manager-git")

	const wantBranch = "mosaic/run/order-test"
	f := harness.NewMockAdapter()
	store := &memStore{}

	type snapshot struct {
		seen         bool
		exists       bool
		commits      bool
		branch       string
		globalSeq    int
		logRows      int
		currentState domain.CurrentState
		appliedCount int
	}
	var atSetup, atFirstWorkflow snapshot
	capture := func(dst *snapshot) {
		dst.seen = true
		dst.exists = store.exists
		dst.commits = store.state.RunSettings.Commits
		dst.branch = store.state.CommitBranch
		dst.globalSeq = store.state.GlobalSequence
		dst.logRows = len(store.state.ExecutionLog)
		dst.currentState = store.state.CurrentState
		dst.appliedCount = len(store.Applied)
	}
	hooked := &beforeInvokeHarness{
		delegate: f,
		before: func(agentID string) {
			switch agentID {
			case "commit-manager-git":
				capture(&atSetup)
			case "agent-a":
				if !atFirstWorkflow.seen {
					capture(&atFirstWorkflow)
				}
			}
		},
	}
	ses := session.New(session.Deps{
		Harness:  hooked,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "branch ready [branch:" + wantBranch + "]",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if !atSetup.seen {
		t.Fatal("want commit setup agent dispatched, but it was not")
	}
	if !atSetup.exists {
		t.Error("want the artifact created before the commit setup dispatch, but the store had no artifact")
	}
	if !atSetup.commits {
		t.Error("want commits enabled recorded in the artifact at the setup dispatch")
	}
	if atSetup.branch != "" || atSetup.globalSeq != 0 || atSetup.logRows != 0 {
		t.Errorf("want a fresh artifact at the setup dispatch (no commit_branch, global_sequence 0, no rows), got branch=%q seq=%d rows=%d",
			atSetup.branch, atSetup.globalSeq, atSetup.logRows)
	}
	if !atFirstWorkflow.seen {
		t.Fatal("want the first workflow agent dispatched after a successful setup")
	}
	if atFirstWorkflow.branch != wantBranch {
		t.Errorf("want commit_branch=%q recorded before the first workflow dispatch, got %q", wantBranch, atFirstWorkflow.branch)
	}
	if atFirstWorkflow.appliedCount != 1 || atFirstWorkflow.globalSeq != 1 || atFirstWorkflow.logRows != 1 {
		t.Errorf("want exactly the setup row (Seq 1) recorded before the first workflow dispatch, got applied=%d seq=%d rows=%d",
			atFirstWorkflow.appliedCount, atFirstWorkflow.globalSeq, atFirstWorkflow.logRows)
	}
	if !reflect.DeepEqual(atFirstWorkflow.currentState, domain.CurrentState{}) {
		t.Errorf("want current_state untouched by commit setup, got %+v", atFirstWorkflow.currentState)
	}
}

// TestSession_Start_CommitsEnabled_SetupAndWorkflowSeqsStrictlyIncrease verifies
// that the setup row and every following workflow row carry strictly
// increasing Seq values, with the setup row first.
func TestSession_Start_CommitsEnabled_SetupAndWorkflowSeqsStrictlyIncrease(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1", StatusCode: domain.StatusSUCCESS,
		StatusMessage: "ok [branch:mosaic/run/seq-test]",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})

	got, err := ses.Start(context.Background(), baseCommitConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunCompleted)
	if len(store.Applied) != 3 {
		t.Fatalf("want 3 applied rows (setup + 2 workflow), got %d", len(store.Applied))
	}
	for i, step := range store.Applied {
		if step.Seq != i+1 {
			t.Errorf("applied row %d: want Seq=%d, got %d", i, i+1, step.Seq)
		}
	}
	if store.Applied[0].AgentInstance != "commit-manager-git#1" {
		t.Errorf("want setup instance commit-manager-git#1, got %q", store.Applied[0].AgentInstance)
	}
	if store.Applied[1].IsInfrastructure {
		t.Error("want the first workflow row to be a workflow row, got an infrastructure row")
	}
}

// ===== Failed setup keeps the artifact =====

// setupFailureRun holds the observable result of a run whose commit setup
// dispatch did not succeed.
type setupFailureRun struct {
	out       domain.RunOutcome
	err       error
	f         *harness.MockAdapter
	store     *memStore
	runFolder string
}

// startWithSetupEntry starts a new commits-enabled run whose commit setup
// dispatch yields the given scripted entry.
func startWithSetupEntry(t *testing.T, entry harness.ScriptedEntry) setupFailureRun {
	t.Helper()
	ses, f, store, orchPath := newCommitSession(t)
	runFolder := filepath.Join(t.TempDir(), "Orchestration-test-run-id")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: create run folder: %v", err)
	}
	f.Queue("commit-manager-git", entry)

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"
	cfg.RunFolder = runFolder

	out, err := ses.Start(context.Background(), cfg)
	return setupFailureRun{out: out, err: err, f: f, store: store, runFolder: runFolder}
}

// requireSetupFailureKept asserts everything a failed setup must leave behind:
// a start-failed outcome, the artifact, the setup row (Seq 1, infrastructure,
// with the given recorded status), no commit_branch, an untouched
// current_state and no workflow dispatch.
func requireSetupFailureKept(t *testing.T, r setupFailureRun, wantRowStatus domain.StatusCode) {
	t.Helper()
	requireStartFailed(t, r.out, r.err)

	if !r.store.exists {
		t.Error("want the artifact kept after a failed commit setup, but the store has none")
	}
	if _, statErr := os.Stat(r.runFolder); statErr != nil {
		t.Errorf("want the run folder kept after a failed commit setup, stat error: %v", statErr)
	}
	if len(r.store.Applied) != 1 {
		t.Fatalf("want exactly the setup row recorded, got %d applied rows", len(r.store.Applied))
	}
	row := r.store.Applied[0]
	if row.Seq != 1 || row.AgentInstance != "commit-manager-git#1" {
		t.Errorf("want setup row commit-manager-git#1 with Seq=1, got %q Seq=%d", row.AgentInstance, row.Seq)
	}
	if !row.IsInfrastructure {
		t.Error("want the setup row recorded as an infrastructure row")
	}
	if row.Status != wantRowStatus {
		t.Errorf("want setup row Status=%q, got %q", wantRowStatus, row.Status)
	}
	if r.store.state.CommitBranch != "" || len(r.store.BranchCalls) != 0 {
		t.Errorf("want commit_branch absent after a failed setup, got %q (SetCommitBranch calls: %v)",
			r.store.state.CommitBranch, r.store.BranchCalls)
	}
	if !reflect.DeepEqual(r.store.state.CurrentState, domain.CurrentState{}) {
		t.Errorf("want current_state unchanged by the failed setup, got %+v", r.store.state.CurrentState)
	}
	if r.store.state.GlobalSequence != 1 {
		t.Errorf("want global_sequence=1 after the setup row, got %d", r.store.state.GlobalSequence)
	}
	for _, inv := range r.f.Invocations() {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow dispatch after a failed setup, got %q", inv.Agent.Identifier)
		}
	}
}

// TestSession_Start_CommitsEnabled_SetupBlocked_KeepsArtifactAndRow verifies
// that a BLOCKED setup response yields a start failure that keeps the artifact
// and records the setup row with the response's own status.
func TestSession_Start_CommitsEnabled_SetupBlocked_KeepsArtifactAndRow(t *testing.T) {
	r := startWithSetupEntry(t, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusBLOCKED,
		StatusMessage:   "dirty working tree",
		ErrorCode:       domain.ErrorPERMISSION_DENIED,
		ErrorReason:     "uncommitted changes",
	}})

	requireSetupFailureKept(t, r, domain.StatusBLOCKED)
}

// TestSession_Start_CommitsEnabled_MissingBranchMarker_KeepsArtifactAndRow
// verifies that a SUCCESS setup response without a [branch:{name}] marker is a
// start failure: the branch was not established, so the artifact and the row
// are kept, commit_branch stays absent and no workflow step is dispatched.
func TestSession_Start_CommitsEnabled_MissingBranchMarker_KeepsArtifactAndRow(t *testing.T) {
	r := startWithSetupEntry(t, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "setup complete (no branch marker)",
	}})

	requireSetupFailureKept(t, r, domain.StatusSUCCESS)
}

// TestSession_Start_CommitsEnabled_EmptyBranchName_KeepsArtifactAndRow verifies
// that an empty [branch:] marker is unreadable and treated like a missing one.
func TestSession_Start_CommitsEnabled_EmptyBranchName_KeepsArtifactAndRow(t *testing.T) {
	r := startWithSetupEntry(t, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "setup complete [branch:]",
	}})

	requireSetupFailureKept(t, r, domain.StatusSUCCESS)
}

// TestSession_Start_CommitsEnabled_HarnessError_KeepsArtifactAndRow verifies
// that a harness failure during setup is recorded as a BLOCKED row (E501), the
// artifact is kept, and the outcome carries the underlying error.
func TestSession_Start_CommitsEnabled_HarnessError_KeepsArtifactAndRow(t *testing.T) {
	harnessErr := errors.New("git: authentication failed")
	r := startWithSetupEntry(t, harness.ScriptedEntry{Err: harnessErr})

	requireSetupFailureKept(t, r, domain.StatusBLOCKED)
	if len(r.store.Applied) == 1 && r.store.Applied[0].ErrorCode != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("want the harness error recorded with error code %q, got %q",
			domain.ErrorTOOL_UNAVAILABLE, r.store.Applied[0].ErrorCode)
	}
	if !errors.Is(r.out.Cause, harnessErr) {
		t.Errorf("want outcome Cause to wrap the harness error, got %v", r.out.Cause)
	}
}

// TestSession_Start_CommitsEnabled_SetupApplyFailure_KeepsArtifact verifies
// that when the setup dispatch succeeds but recording its row fails, the run
// stops without dispatching a workflow step, does not set commit_branch, and
// keeps the run folder: the run is resumable and nothing is removed.
func TestSession_Start_CommitsEnabled_SetupApplyFailure_KeepsArtifact(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)
	runFolder := filepath.Join(t.TempDir(), "run")
	if err := os.MkdirAll(runFolder, 0o755); err != nil {
		t.Fatalf("setup: failed to create run folder: %v", err)
	}
	store.applyErrOnFirst = true
	store.applyFirstErr = errors.New("disk full: unable to record commit setup row")
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1", StatusCode: domain.StatusSUCCESS,
		StatusMessage: "branch ready [branch:mosaic/run/test-run-id]",
	}})

	cfg := baseCommitConfig(orchPath)
	cfg.RunID = "test-run-id"
	cfg.RunFolder = runFolder

	got, _ := ses.Start(context.Background(), cfg)

	if got.Status != domain.RunFailed && got.Status != domain.RunStartFailed {
		t.Errorf("want RunFailed or RunStartFailed when the setup row cannot be recorded, got %q", got.Status)
	}
	if _, statErr := os.Stat(runFolder); statErr != nil {
		t.Errorf("want run folder kept when the setup row cannot be recorded, stat error: %v", statErr)
	}
	if !store.exists {
		t.Error("want the artifact kept when the setup row cannot be recorded")
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow dispatch, got %q", inv.Agent.Identifier)
		}
	}
}

// ===== Resume =====

// commitResumeState returns a stored artifact for a run with commits enabled.
func commitResumeState() domain.ArtifactState {
	return domain.ArtifactState{
		RunID:           testRunID,
		Workflow:        "linear",
		WorkflowVersion: "1.0",
		Task:            "test task",
		RunSettings: domain.RunSettings{
			Mode:    domain.ExecutionModeAuto,
			Commits: true,
		},
	}
}

func queueSuccessfulSetupAndWorkflow(f *harness.MockAdapter, branch string) {
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#x", StatusCode: domain.StatusSUCCESS,
		StatusMessage: "branch ready [branch:" + branch + "]",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#x", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#x", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})
}

// TestSession_Start_Resume_CommitsEnabledNoBranch_InterruptedSetup_RetriesSetup
// verifies that an artifact left by an interrupted setup (created, no row
// recorded, no commit_branch) retries setup before any workflow step, records
// the retry as the next sequence, and then runs the workflow.
func TestSession_Start_Resume_CommitsEnabledNoBranch_InterruptedSetup_RetriesSetup(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)
	store.state = commitResumeState()
	store.exists = true
	const wantBranch = "mosaic/run/retry-test"
	queueSuccessfulSetupAndWorkflow(f, wantBranch)

	cfg := baseCommitConfig(orchPath)
	markResume(&cfg)
	cfg.Supplied.CommitBranchVariant = true // a pending setup retry needs the variant

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	invs := f.Invocations()
	if len(invs) != 3 || invs[0].Agent.Identifier != "commit-manager-git" {
		t.Fatalf("want setup dispatched first, then both workflow agents; got %d invocations", len(invs))
	}
	if store.state.CommitBranch != wantBranch {
		t.Errorf("want commit_branch=%q after the retried setup, got %q", wantBranch, store.state.CommitBranch)
	}
	if len(store.Applied) != 3 {
		t.Fatalf("want 3 applied rows (setup + 2 workflow), got %d", len(store.Applied))
	}
	for i, step := range store.Applied {
		if step.Seq != i+1 {
			t.Errorf("applied row %d: want Seq=%d, got %d", i, i+1, step.Seq)
		}
	}
	if !store.Applied[0].IsInfrastructure {
		t.Error("want the retried setup recorded as an infrastructure row")
	}
}

// TestSession_Start_Resume_AfterFailedSetup_RetriesSetupWithNextSequence
// verifies that after a failed setup left its row behind, a resume retries
// setup, records a new row with the next sequence (the earlier row stays), and
// then runs the workflow.
func TestSession_Start_Resume_AfterFailedSetup_RetriesSetupWithNextSequence(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)
	store.state = commitResumeState()
	store.state.GlobalSequence = 1
	store.state.ExecutionLog = []domain.ExecutionLogEntry{
		{Seq: 1, Agent: "commit-manager-git#1", Status: domain.StatusBLOCKED},
	}
	store.exists = true
	const wantBranch = "mosaic/run/retry-after-failure"
	queueSuccessfulSetupAndWorkflow(f, wantBranch)

	cfg := baseCommitConfig(orchPath)
	markResume(&cfg)
	cfg.Supplied.CommitBranchVariant = true // a pending setup retry needs the variant

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	if len(store.Applied) != 3 {
		t.Fatalf("want 3 newly applied rows (retried setup + 2 workflow), got %d", len(store.Applied))
	}
	if store.Applied[0].Seq != 2 || store.Applied[0].AgentInstance != "commit-manager-git#2" {
		t.Errorf("want retried setup row commit-manager-git#2 with Seq=2, got %q Seq=%d",
			store.Applied[0].AgentInstance, store.Applied[0].Seq)
	}
	if store.Applied[1].Seq != 3 || store.Applied[2].Seq != 4 {
		t.Errorf("want workflow rows Seq 3 and 4, got %d and %d", store.Applied[1].Seq, store.Applied[2].Seq)
	}
	if store.state.CommitBranch != wantBranch {
		t.Errorf("want commit_branch=%q after the retry, got %q", wantBranch, store.state.CommitBranch)
	}
}

// TestSession_Start_Resume_CommitsEnabledNoBranch_RetryFailure_StaysResumable
// verifies that a retried setup that fails again keeps the artifact, records
// its row and dispatches no workflow step.
func TestSession_Start_Resume_CommitsEnabledNoBranch_RetryFailure_StaysResumable(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)
	store.state = commitResumeState()
	store.exists = true
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1", StatusCode: domain.StatusSUCCESS,
		StatusMessage: "no marker",
	}})

	cfg := baseCommitConfig(orchPath)
	markResume(&cfg)
	cfg.Supplied.CommitBranchVariant = true // a pending setup retry needs the variant

	got, err := ses.Start(context.Background(), cfg)

	requireStartFailed(t, got, err)
	if len(store.Applied) != 1 || store.Applied[0].Seq != 1 {
		t.Errorf("want the retry row recorded with Seq=1, got %d rows", len(store.Applied))
	}
	if store.state.CommitBranch != "" {
		t.Errorf("want commit_branch absent after a failed retry, got %q", store.state.CommitBranch)
	}
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier != "commit-manager-git" {
			t.Errorf("want no workflow dispatch after a failed retry, got %q", inv.Agent.Identifier)
		}
	}
}

// TestSession_Start_Resume_CommitSetupPending_NoVariantSupplied_Refused
// verifies that a pending setup retry without a commit branch variant is
// refused before any dispatch.
func TestSession_Start_Resume_CommitSetupPending_NoVariantSupplied_Refused(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)
	store.state = commitResumeState()
	store.exists = true
	queueSuccessfulSetupAndWorkflow(f, "mosaic/run/never")

	cfg := baseCommitConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got.Status != domain.RunRefused {
		t.Errorf("want RunRefused, got %q (message: %q)", got.Status, got.Message)
	}
	if n := len(f.Invocations()); n != 0 {
		t.Errorf("want no dispatch after a refusal, got %d invocations", n)
	}
	if len(store.Applied) != 0 {
		t.Errorf("want no rows applied after a refusal, got %d", len(store.Applied))
	}
}

// TestSession_Start_Resume_CommitBranchPresent_DoesNotRerunSetup verifies that
// a recorded commit_branch means setup is never run again on resume.
func TestSession_Start_Resume_CommitBranchPresent_DoesNotRerunSetup(t *testing.T) {
	ses, f, store, orchPath := newCommitSession(t)
	store.state = commitResumeState()
	store.state.CommitBranch = "mosaic/run/test-run-id"
	store.state.GlobalSequence = 2
	store.state.ExecutionLog = []domain.ExecutionLogEntry{
		{Seq: 1, Agent: "commit-manager-git#1", Status: domain.StatusSUCCESS},
		{Seq: 2, Agent: "agent-a#2", Phase: "PLANNING", Status: domain.StatusSUCCESS},
	}
	store.state.CurrentState = domain.CurrentState{
		Phase: "PLANNING", LastStatus: domain.StatusSUCCESS, LastAgent: "agent-a#2",
	}
	store.exists = true
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3", StatusCode: domain.StatusSUCCESS, StatusMessage: "done",
	}})

	cfg := baseCommitConfig(orchPath)
	markResume(&cfg)

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)
	for _, inv := range f.Invocations() {
		if inv.Agent.Identifier == "commit-manager-git" {
			t.Error("want commit setup NOT re-run when commit_branch is already recorded")
		}
	}
	if len(store.BranchCalls) != 0 {
		t.Errorf("want no SetCommitBranch call when commit_branch is present, got %v", store.BranchCalls)
	}
}

// TestSession_Start_CommitsDisabled_NoCommitAgentDispatchedAtStart verifies that
// when commits are disabled, the commit-class agent is not invoked during run
// start. Only workflow agents appear in the harness invocation log.
func TestSession_Start_CommitsDisabled_NoCommitAgentDispatchedAtStart(t *testing.T) {
	ses, f, _, orchPath := newCommitSession(t)

	// No commit-manager-git entry queued: any invocation would return a harness
	// error, causing the test to observe the wrong failure mode.
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

	cfg := baseCommitConfig(orchPath)
	cfg.Commits = false // disable commits

	ses.Start(context.Background(), cfg) //nolint:errcheck

	invs := f.Invocations()
	for _, inv := range invs {
		if strings.Contains(inv.Agent.Identifier, "commit-manager-git") {
			t.Errorf("want no commit-manager-git invocation when commits are disabled, got invocation with agent_instance_id=%q",
				inv.Request.AgentInstanceID)
		}
	}
}

// TestSession_Start_CommitsEnabled_CommitAgentDotAgentMdExtension_SetupSucceeds
// verifies that doCommitSetupDispatch resolves commit-class agent definition
// files named with the .agent.md compound extension (e.g.
// commit-manager-git.agent.md) just as it would a .md file. This
// regression-locks the extension-agnostic behavior introduced by the switch
// to agentresolve.ResolveOne in doCommitSetupDispatch.
func TestSession_Start_CommitsEnabled_CommitAgentDotAgentMdExtension_SetupSucceeds(t *testing.T) {
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "commit-agent-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")
	// Use .agent.md extension for the commit agent to verify extension-agnostic resolution.
	agentFilePath := filepath.Join(dir, "commit-manager-git.agent.md")
	if err := os.WriteFile(agentFilePath, []byte("# Agent: commit-manager-git\n"), 0600); err != nil {
		t.Fatalf("write commit-manager-git.agent.md: %v", err)
	}

	f := harness.NewMockAdapter()
	store := &memStore{}
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	const wantBranch = "mosaic/run/agent-md-ext-test"
	f.Queue("commit-manager-git", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "commit-manager-git#1",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "commit setup complete [branch:" + wantBranch + "]",
	}})
	f.Queue("agent-a", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-a#2",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	cfg := baseCommitConfig(orchPath)

	got, err := ses.Start(context.Background(), cfg)

	requireRunStatus(t, got, err, domain.RunCompleted)

	// Verify commit-manager-git was dispatched from the .agent.md file path.
	invs := f.Invocations()
	dispatched := false
	for _, inv := range invs {
		if inv.Agent.Identifier == "commit-manager-git" {
			dispatched = true
			if !strings.HasSuffix(inv.Agent.DefinitionPath, "commit-manager-git.agent.md") {
				t.Errorf("want DefinitionPath ending in .agent.md, got %q", inv.Agent.DefinitionPath)
			}
		}
	}
	if !dispatched {
		t.Error("want commit-manager-git dispatched (commit setup), but it was not")
	}
}
