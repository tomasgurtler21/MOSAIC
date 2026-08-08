package runner_test

// Snapshot tests: everything the verdict engine needs is captured before
// teardown removes anything. Ordering is the decisive case — a snapshot
// taken after teardown yields empty evidence and a test that fails for the
// wrong reason.

import (
	"context"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/protocolcheck"
	"mosaic-agent-test/internal/runner"
)

func TestRun_SnapshotFilesSurviveIntoEvidenceEvenThoughTheSandboxIsGone(t *testing.T) {
	h := newHarness(t)
	req := newRequest("snapshot-ordering")
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{Path: "notes/plan.md", Content: "seeded plan content"},
	}

	evidence, err := runner.Run(context.Background(), h.Deps, req)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	found := false
	for _, f := range evidence.SnapshotFiles {
		if f == "notes/plan.md" || f == `notes\plan.md` {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("evidence.SnapshotFiles = %v, want it to include the seeded file captured before teardown removed the sandbox", evidence.SnapshotFiles)
	}
}

func TestRun_SubjectResultIsPartOfTheCapturedEvidence(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		return domain.SubjectResult{
			ProtocolMessage: `{"agent_instance_id":"researcher#1"}`,
			Disposition:     domain.DispositionCompleted,
			ExitCode:        0,
		}, nil
	}
	req := newRequest("subject-result-evidence")

	evidence, err := runner.Run(context.Background(), h.Deps, req)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if evidence.SubjectResult.ProtocolMessage == "" {
		t.Error("evidence.SubjectResult.ProtocolMessage is empty, want the subject's own final protocol message")
	}
	if evidence.SubjectResult.Disposition != domain.DispositionCompleted {
		t.Errorf("evidence.SubjectResult.Disposition = %q, want %q", evidence.SubjectResult.Disposition, domain.DispositionCompleted)
	}
}

func TestRun_CostIsReadAfterExecutionAndBeforeTeardown(t *testing.T) {
	h := newHarness(t)
	req := newRequest("cost-ordering")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	order := h.Rec.all()
	launchIdx, costIdx, deprovisionIdx := -1, -1, -1
	for i, name := range order {
		switch name {
		case "launcher.Launch":
			if launchIdx == -1 {
				launchIdx = i
			}
		case "cost.Cost":
			if costIdx == -1 {
				costIdx = i
			}
		case "adapter.Deprovision":
			if deprovisionIdx == -1 {
				deprovisionIdx = i
			}
		}
	}

	if launchIdx == -1 || costIdx == -1 {
		t.Fatalf("call order = %v, want it to include both launcher.Launch and cost.Cost", order)
	}
	if costIdx < launchIdx {
		t.Errorf("cost.Cost happened before launcher.Launch (order=%v) — cost must be read after execution completes", order)
	}
	if deprovisionIdx != -1 && costIdx > deprovisionIdx {
		t.Errorf("cost.Cost happened after adapter.Deprovision (order=%v) — cost must be read before teardown removes the log tree", order)
	}
}

func TestRun_CostQueryNamesTheSnapshotsLogRoot(t *testing.T) {
	h := newHarness(t)
	req := newRequest("cost-log-root")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if h.Cost.lastQuery.RunID != req.Key.RunID {
		t.Errorf("cost query RunID = %q, want %q", h.Cost.lastQuery.RunID, req.Key.RunID)
	}
	if h.Cost.lastQuery.LogRoot == "" {
		t.Error("cost query LogRoot is empty, want the sandbox's captured log root")
	}
}

func TestBuildEvidence_MapsSnapshotSettlingsAndCostIntoRunEvidence(t *testing.T) {
	req := newRequest("build-evidence")
	snap := runner.Snapshot{
		Files:         []string{"a.md", "b.md"},
		Orchestration: domain.OrchestrationState{Present: true, Phase: "Execution"},
		SubjectResult: domain.SubjectResult{Disposition: domain.DispositionCompleted},
	}
	cost := domain.CostReport{TotalUSD: 0.42, Attribution: domain.AttributionAttributed}
	dur := 3 * time.Second

	evidence := runner.BuildEvidence(req, snap, cost, dur)

	if evidence.Definition.ID != req.Test.Definition.ID {
		t.Errorf("evidence.Definition.ID = %q, want %q", evidence.Definition.ID, req.Test.Definition.ID)
	}
	if len(evidence.SnapshotFiles) != 2 {
		t.Errorf("evidence.SnapshotFiles = %v, want the snapshot's file listing", evidence.SnapshotFiles)
	}
	if evidence.Cost != cost {
		t.Errorf("evidence.Cost = %+v, want %+v", evidence.Cost, cost)
	}
	if evidence.Duration != dur {
		t.Errorf("evidence.Duration = %v, want %v", evidence.Duration, dur)
	}
	if !evidence.Orchestration.Present {
		t.Error("evidence.Orchestration.Present = false, want the snapshot's orchestration state to carry through")
	}
}

// The six cases below close the coverage gap the Stage 18 re-review found:
// BuildEvidence's collaboratorProtocolViolations, subjectProtocolViolations
// and concurrency.Peaks integration had no committed test exercising them,
// so a regression in any of the three would have produced silently wrong
// verdicts with nothing to catch it.

func TestBuildEvidence_MalformedCollaboratorEchoYieldsProtocolViolations(t *testing.T) {
	req := newRequest("collaborator-violation")
	t0 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	snap := runner.Snapshot{
		Records: []domain.LogRecord{
			{
				Kind:      domain.RecordStart,
				Seq:       1,
				Timestamp: t0,
				Message: &domain.TaskMessage{
					AgentInstanceID:      "researcher#1",
					RunID:                "run-1",
					TaskDescription:      "investigate",
					IncludeResultSummary: true,
					Extraction:           domain.ExtractionParsed,
				},
			},
			{
				Kind:      domain.RecordEnd,
				Seq:       1,
				Timestamp: t0.Add(time.Second),
				// Missing run_id, status_code and status_message: three
				// distinct ViolationMissingRequiredField hits.
				Echo: &domain.EchoOutcome{Observed: `{"agent_instance_id":"researcher#1"}`},
			},
		},
	}

	evidence := runner.BuildEvidence(req, snap, domain.CostReport{}, 0)

	class := domain.ViolationClassKey(protocolcheck.ViolationMissingRequiredField)
	if got := evidence.ProtocolViolations[class]; got != 3 {
		t.Errorf("evidence.ProtocolViolations[%q] = %d, want 3 (missing run_id, status_code, status_message)", class, got)
	}
}

func TestBuildEvidence_EndRecordWithNoMatchingStartUsesUnknownRequestContext(t *testing.T) {
	req := newRequest("unmatched-end-record")
	t0 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	snap := runner.Snapshot{
		Records: []domain.LogRecord{
			{
				// No RecordStart with Seq 9 anywhere in Records: the
				// invocation this end record answers was never recovered.
				Kind:      domain.RecordEnd,
				Seq:       9,
				Timestamp: t0,
				// Otherwise well-formed, but carries result_data, which is a
				// violation only when the originating request is known to
				// have not asked for it. An unknown request must not
				// manufacture that violation.
				Echo: &domain.EchoOutcome{Observed: `{"agent_instance_id":"researcher#1","run_id":"run-1","status_code":"SUCCESS","status_message":"done","result_data":"extra"}`},
			},
		},
	}

	evidence := runner.BuildEvidence(req, snap, domain.CostReport{}, 0)

	class := domain.ViolationClassKey(protocolcheck.ViolationUnrequestedResultData)
	if got := evidence.ProtocolViolations[class]; got != 0 {
		t.Errorf("evidence.ProtocolViolations[%q] = %d, want 0 — an end record with no matching start must use UnknownRequest and never manufacture a violation from absent request context", class, got)
	}
	if len(evidence.ProtocolViolations) != 0 {
		t.Errorf("evidence.ProtocolViolations = %v, want empty — the response is otherwise well-formed under UnknownRequest", evidence.ProtocolViolations)
	}
}

func TestBuildEvidence_MalformedSubjectProtocolMessageYieldsSubjectProtocolViolations(t *testing.T) {
	req := newRequest("subject-violation")
	snap := runner.Snapshot{
		SubjectResult: domain.SubjectResult{
			// Missing run_id, status_code and status_message.
			ProtocolMessage: `{"agent_instance_id":"researcher#1"}`,
			Disposition:     domain.DispositionCompleted,
		},
	}

	evidence := runner.BuildEvidence(req, snap, domain.CostReport{}, 0)

	class := domain.ViolationClassKey(protocolcheck.ViolationMissingRequiredField)
	if got := evidence.SubjectProtocolViolations[class]; got != 3 {
		t.Errorf("evidence.SubjectProtocolViolations[%q] = %d, want 3 (missing run_id, status_code, status_message)", class, got)
	}
}

func TestBuildEvidence_EmptySubjectProtocolMessageYieldsEmptyNotNilSubjectProtocolViolations(t *testing.T) {
	req := newRequest("subject-no-message")
	snap := runner.Snapshot{
		SubjectResult: domain.SubjectResult{
			ProtocolMessage: "",
			Disposition:     domain.DispositionCompleted,
		},
	}

	evidence := runner.BuildEvidence(req, snap, domain.CostReport{}, 0)

	if evidence.SubjectProtocolViolations == nil {
		t.Error("evidence.SubjectProtocolViolations is nil, want an empty (non-nil) map — safe ranging in the evaluator relies on this")
	}
	if len(evidence.SubjectProtocolViolations) != 0 {
		t.Errorf("evidence.SubjectProtocolViolations = %v, want empty", evidence.SubjectProtocolViolations)
	}
}

func TestBuildEvidence_OverlappingParallelGroupRecordsYieldExpectedPeakConcurrency(t *testing.T) {
	req := newRequest("peak-concurrency")
	req.Test.Definition.ParallelGroups = []domain.ParallelGroup{
		{Name: "researchers"},
	}
	t0 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	// Invocation "a" spans [t0, t0+3s); invocation "b" spans [t0+1s, t0+4s).
	// They overlap for [t0+1s, t0+3s), so the group's peak is 2.
	snap := runner.Snapshot{
		Records: []domain.LogRecord{
			{Kind: domain.RecordStart, Seq: 1, Group: "researchers", CorrelationToken: "a", Timestamp: t0},
			{Kind: domain.RecordStart, Seq: 2, Group: "researchers", CorrelationToken: "b", Timestamp: t0.Add(time.Second)},
			{Kind: domain.RecordEnd, CorrelationToken: "a", Timestamp: t0.Add(3 * time.Second)},
			{Kind: domain.RecordEnd, CorrelationToken: "b", Timestamp: t0.Add(4 * time.Second)},
		},
	}

	evidence := runner.BuildEvidence(req, snap, domain.CostReport{}, 0)

	if got := evidence.PeakConcurrency["researchers"]; got != 2 {
		t.Errorf(`evidence.PeakConcurrency["researchers"] = %d, want 2`, got)
	}
}

func TestBuildEvidence_StartRecordWithNoMatchingEndYieldsUnterminatedSeq(t *testing.T) {
	req := newRequest("unterminated-invocation")
	req.Test.Definition.ParallelGroups = []domain.ParallelGroup{
		{Name: "researchers"},
	}
	t0 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	snap := runner.Snapshot{
		Records: []domain.LogRecord{
			{Kind: domain.RecordStart, Seq: 7, Group: "researchers", CorrelationToken: "crashed", Timestamp: t0},
		},
	}

	evidence := runner.BuildEvidence(req, snap, domain.CostReport{}, 0)

	if len(evidence.ConcurrencyProblems.UnterminatedSeqs) == 0 {
		t.Fatal("evidence.ConcurrencyProblems.UnterminatedSeqs is empty, want it to include the unterminated invocation's Seq")
	}
	if evidence.ConcurrencyProblems.UnterminatedSeqs[0] != 7 {
		t.Errorf("evidence.ConcurrencyProblems.UnterminatedSeqs = %v, want it to contain 7", evidence.ConcurrencyProblems.UnterminatedSeqs)
	}
}
