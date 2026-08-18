package runner_test

// Run identity contract tests owned by this package:
//
//   - T10.3: the run id is injected into the subject's prompt, in a form
//     the bundle's extraction accepts, before the subject's original
//     opening message — reaching both SpawnPlan's subject argument and the
//     provision request's subject — without mutating the caller's Request
//     or any other SubjectUnderTest field.
//   - T10.2: the same run id reaches every consumer this package owns: the
//     cost query, the captured log root, and the seed-file {run_id}
//     placeholder.
//   - T10.4: LogRoot/LogsProduced survive from a Snapshot into the built
//     evidence unchanged, and a run whose sandbox log root actually holds a
//     file reports LogsProduced true.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

// loggerBundleShapedRunID is a run id already in the shape the logger
// bundle's extraction accepts, used wherever a test needs to drive a real
// id through the runner rather than newRequest's default "run-{testID}"
// placeholder shape.
const loggerBundleShapedRunID = "20260809T171229Z-79ca"

func TestRun_SubjectPromptCarriesTheRunIDPreludeBeforeTheOriginalOpeningMessage(t *testing.T) {
	h := newHarness(t)
	req := newRequest("prelude-ordering")
	req.Key.RunID = loggerBundleShapedRunID
	const originalMessage = "start"
	req.Test.Definition.Subject.OpeningMessage = originalMessage

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	gotSpawn := h.Adapter.lastSpawnPlanSubject.OpeningMessage
	if !strings.Contains(gotSpawn, loggerBundleShapedRunID) {
		t.Errorf("SpawnPlan subject.OpeningMessage = %q, want it to contain the run id %q", gotSpawn, loggerBundleShapedRunID)
	}
	if !strings.Contains(gotSpawn, originalMessage) {
		t.Errorf("SpawnPlan subject.OpeningMessage = %q, want it to still contain the original opening message %q", gotSpawn, originalMessage)
	}
	if idRunID, idOriginal := strings.Index(gotSpawn, loggerBundleShapedRunID), strings.Index(gotSpawn, originalMessage); idRunID > idOriginal {
		t.Errorf("SpawnPlan subject.OpeningMessage = %q, want the run id to precede the original message, not follow it", gotSpawn)
	}

	gotProvision := h.Adapter.lastProvisionReq.Subject.OpeningMessage
	if !strings.Contains(gotProvision, loggerBundleShapedRunID) {
		t.Errorf("ProvisionRequest.Subject.OpeningMessage = %q, want it to contain the run id %q", gotProvision, loggerBundleShapedRunID)
	}
	if !strings.Contains(gotProvision, originalMessage) {
		t.Errorf("ProvisionRequest.Subject.OpeningMessage = %q, want it to still contain the original opening message %q", gotProvision, originalMessage)
	}
	if idRunID, idOriginal := strings.Index(gotProvision, loggerBundleShapedRunID), strings.Index(gotProvision, originalMessage); idRunID > idOriginal {
		t.Errorf("ProvisionRequest.Subject.OpeningMessage = %q, want the run id to precede the original message, not follow it", gotProvision)
	}
}

// TestRun_RunIDPreludeInjectionPreservesEverySubjectFieldExceptOpeningMessage
// asserts the prelude injection touches only OpeningMessage: every other
// field the subject declares reaches the adapter unchanged.
func TestRun_RunIDPreludeInjectionPreservesEverySubjectFieldExceptOpeningMessage(t *testing.T) {
	h := newHarness(t)
	req := newRequest("prelude-field-preservation")
	req.Key.RunID = loggerBundleShapedRunID
	req.Test.Definition.Subject = domain.SubjectUnderTest{
		Identity:       "orchestrator",
		DefinitionPath: "orchestrator.md",
		OpeningMessage: "start",
		InvocationKind: "task",
		Model:          "opus",
		AllowedTools:   []string{"dispatch"},
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	got := h.Adapter.lastSpawnPlanSubject
	want := req.Test.Definition.Subject
	if got.Identity != want.Identity {
		t.Errorf("subject.Identity = %q, want %q", got.Identity, want.Identity)
	}
	if got.DefinitionPath != want.DefinitionPath {
		t.Errorf("subject.DefinitionPath = %q, want %q", got.DefinitionPath, want.DefinitionPath)
	}
	if got.InvocationKind != want.InvocationKind {
		t.Errorf("subject.InvocationKind = %q, want %q", got.InvocationKind, want.InvocationKind)
	}
	if got.Model != want.Model {
		t.Errorf("subject.Model = %q, want %q", got.Model, want.Model)
	}
	if len(got.AllowedTools) != 1 || got.AllowedTools[0] != "dispatch" {
		t.Errorf("subject.AllowedTools = %v, want %v unchanged", got.AllowedTools, want.AllowedTools)
	}
}

// TestRun_RunIDPreludeInjectionDoesNotMutateTheCallersRequest asserts
// subjectWithRunIDPrelude works on a copy: req's own Subject.OpeningMessage
// is unchanged by the call, so a caller reusing req (e.g. a retry) never
// observes an accumulating prelude.
func TestRun_RunIDPreludeInjectionDoesNotMutateTheCallersRequest(t *testing.T) {
	h := newHarness(t)
	req := newRequest("prelude-no-mutation")
	req.Key.RunID = loggerBundleShapedRunID
	const originalMessage = "start"
	req.Test.Definition.Subject.OpeningMessage = originalMessage

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if req.Test.Definition.Subject.OpeningMessage != originalMessage {
		t.Errorf("req.Test.Definition.Subject.OpeningMessage = %q after Run, want it unchanged at %q — the caller's Request must never be mutated in place", req.Test.Definition.Subject.OpeningMessage, originalMessage)
	}
}

// TestRun_RunIDIsConsistentAcrossTheCostQueryAndTheCapturedLogRoot drives a
// logger-bundle-shaped run id through a real Run() call and asserts the
// same id reaches the cost query's RunID, and that the cost query's
// LogRoot matches the sandbox's own LogRoot() — the two consumers
// ContractsDesign.md's "Run identity contract" table names for this
// package, alongside the seed-file placeholder covered separately below.
func TestRun_RunIDIsConsistentAcrossTheCostQueryAndTheCapturedLogRoot(t *testing.T) {
	h := newHarness(t)
	req := newRequest("run-id-consistency")
	req.Key.RunID = loggerBundleShapedRunID

	var sandboxLogRoot string
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sandboxLogRoot = h.Adapter.lastProvisionReq.Sandbox.LogRoot()
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if h.Cost.lastQuery.RunID != loggerBundleShapedRunID {
		t.Errorf("cost query RunID = %q, want %q", h.Cost.lastQuery.RunID, loggerBundleShapedRunID)
	}
	if sandboxLogRoot == "" {
		t.Fatal("sandbox LogRoot() was never observed; launcher callback did not run")
	}
	if h.Cost.lastQuery.LogRoot != sandboxLogRoot {
		t.Errorf("cost query LogRoot = %q, want the sandbox's own LogRoot() %q", h.Cost.lastQuery.LogRoot, sandboxLogRoot)
	}
	if !strings.Contains(sandboxLogRoot, loggerBundleShapedRunID) {
		t.Errorf("sandbox LogRoot() = %q, want it to be keyed by the run id %q", sandboxLogRoot, loggerBundleShapedRunID)
	}
}

// TestRun_RunIDExpandsTheSeedFilePlaceholder asserts the same run id that
// reaches the cost query also expands a declared seed file's {run_id}
// placeholder — the fourth consumer this package owns, alongside the cost
// query and the captured log root above.
func TestRun_RunIDExpandsTheSeedFilePlaceholder(t *testing.T) {
	h := newHarness(t)
	req := newRequest("seed-file-run-id-placeholder")
	req.Key.RunID = loggerBundleShapedRunID
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{Path: "Orchestration-{run_id}/Orchestration.md", Content: "seeded orchestration doc"},
	}

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		wantPath := filepath.Join(sb.SubjectDir, "Orchestration-"+loggerBundleShapedRunID, "Orchestration.md")
		if _, err := os.Stat(wantPath); err != nil {
			t.Errorf("expected the seed file's {run_id} placeholder to expand to %q, but it does not exist: %v", wantPath, err)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
}

// TestBuildEvidence_LogRootAndLogsProducedSurviveIntoEvidence asserts the
// pass-through this stage adds to BuildEvidence: whatever the snapshot
// captured for LogRoot and LogsProduced reaches the built evidence
// unchanged, exactly as every other snapshot-derived field already does.
func TestBuildEvidence_LogRootAndLogsProducedSurviveIntoEvidence(t *testing.T) {
	req := newRequest("build-evidence-log-fields")
	snap := runner.Snapshot{
		SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
		LogRoot:       "/sandbox/subject/OrchestrationLogs/20260809T171229Z-79ca",
		LogsProduced:  true,
	}

	evidence := runner.BuildEvidence(req, snap, domain.CostReport{}, 0)

	if evidence.LogRoot != snap.LogRoot {
		t.Errorf("evidence.LogRoot = %q, want the snapshot's LogRoot %q", evidence.LogRoot, snap.LogRoot)
	}
	if evidence.LogsProduced != snap.LogsProduced {
		t.Errorf("evidence.LogsProduced = %v, want the snapshot's LogsProduced %v", evidence.LogsProduced, snap.LogsProduced)
	}
}

// TestRun_EvidenceReportsLogsProducedTrueWhenTheLogRootActuallyHoldsLogs
// drives a full Run() and writes a file into the sandbox's log root from
// inside the launcher callback (after setup, before teardown removes the
// sandbox), asserting the resulting evidence reports LogsProduced true —
// the no-logs signal's positive case, distinguishing a run that genuinely
// produced logs from one that did not.
func TestRun_EvidenceReportsLogsProducedTrueWhenTheLogRootActuallyHoldsLogs(t *testing.T) {
	h := newHarness(t)
	req := newRequest("logs-produced-true")
	req.Key.RunID = loggerBundleShapedRunID

	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		sb := h.Adapter.lastProvisionReq.Sandbox
		if err := os.MkdirAll(sb.LogRoot(), 0o755); err != nil {
			t.Fatalf("creating log root: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sb.LogRoot(), "session.jsonl"), []byte(`{"ok":true}`), 0o644); err != nil {
			t.Fatalf("writing a log file into the log root: %v", err)
		}
		return domain.SubjectResult{Disposition: domain.DispositionCompleted}, nil
	}

	var capturedEvidence domain.RunEvidence
	eval := func(ev domain.RunEvidence) domain.TestResult {
		capturedEvidence = ev
		return domain.TestResult{}
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, eval); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if !capturedEvidence.LogsProduced {
		t.Error("capturedEvidence.LogsProduced = false, want true — a file was written into the queried log root before teardown")
	}
}

// TestRun_EvidenceReportsLogsProducedFalseWhenTheLogRootStaysEmpty is the
// no-logs signal's negative case: a run whose log root is never written to
// reports LogsProduced false, so the two cases are distinguishable rather
// than one masking the other.
func TestRun_EvidenceReportsLogsProducedFalseWhenTheLogRootStaysEmpty(t *testing.T) {
	h := newHarness(t)
	req := newRequest("logs-produced-false")
	req.Key.RunID = loggerBundleShapedRunID

	var capturedEvidence domain.RunEvidence
	eval := func(ev domain.RunEvidence) domain.TestResult {
		capturedEvidence = ev
		return domain.TestResult{}
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, eval); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if capturedEvidence.LogsProduced {
		t.Error("capturedEvidence.LogsProduced = true, want false — nothing was ever written into the queried log root")
	}
	if capturedEvidence.LogRoot == "" {
		t.Error("capturedEvidence.LogRoot is empty, want the queried log root to be named even when it held nothing")
	}
}
