package engine_test

// Tests for routing when no response is supplied to Next (resume): the engine
// routes on the status recorded in current_state, not on the status of the
// trailing Execution Log row. After a repaired review the trailing row is the
// re-dispatch's SUCCESS while current_state records the original status.

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// recordedStatusNext runs engine.Next with no LastResponse for a run whose
// current_state records status/code under the last log row's agent.
func recordedStatusNext(t *testing.T, mode domain.ExecutionMode, limit int,
	recorded domain.StatusCode, code domain.ErrorCode, log []domain.ExecutionLogEntry) domain.EngineDecision {
	t.Helper()
	aw := mustParseAndAdmit(t, quickFixContent, "quick-fix", "3.0")
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "test-runner",
		"implementation-tdd", "implementation-review",
	)
	last := log[len(log)-1]
	state := domain.ArtifactState{
		GlobalSequence: last.Seq,
		CurrentState: domain.CurrentState{
			Phase:      last.Phase,
			Stage:      last.Stage,
			LastStatus: recorded,
			LastAgent:  last.Agent,
			ErrorCode:  code,
		},
		ExecutionLog: log,
	}
	state.ReviewLoopLimit = limit
	return engine.Next(engine.NextInput{
		Workflow: aw,
		Stages:   stages,
		State:    state,
		Agents:   agents,
		Seq:      last.Seq,
		Now:      fixedNow,
		Mode:     mode,
	})
}

// repairedReviewLog is a reviewer's rejected COMPLETED_NEEDS_ACTION followed by
// its SUCCESS re-dispatch.
func repairedReviewLog() []domain.ExecutionLogEntry {
	return []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusSUCCESS),
	}
}

func TestNext_Resume_RepairedReview_Mode3_RoutesRecordedFindingsToOnFindings(t *testing.T) {
	dec := recordedStatusNext(t, domain.ExecutionModeAutoReview, 0,
		domain.StatusCOMPLETED_NEEDS_ACTION, domain.ErrorNone, repairedReviewLog())

	requireRoutedTo(t, dec, rllFixer)
}

func TestNext_Resume_RepairedReview_Mode2_DeviatesOnRecordedFindings(t *testing.T) {
	dec := recordedStatusNext(t, domain.ExecutionModeAuto, 0,
		domain.StatusCOMPLETED_NEEDS_ACTION, domain.ErrorNone, repairedReviewLog())

	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationNonSuccess {
		t.Errorf("want DeviationNonSuccess, got %q", dev.Info.Kind)
	}
}

func TestNext_Resume_RepairedReview_LoopLimitCountsRepairedIterationOnce(t *testing.T) {
	log := repairedReviewLog()

	requireRoutedTo(t, recordedStatusNext(t, domain.ExecutionModeAutoReview, 2,
		domain.StatusCOMPLETED_NEEDS_ACTION, domain.ErrorNone, log), rllFixer)
	requireLoopLimitDeviation(t, recordedStatusNext(t, domain.ExecutionModeAutoReview, 1,
		domain.StatusCOMPLETED_NEEDS_ACTION, domain.ErrorNone, log))
}

func TestNext_Resume_RecordedBlockedE503_Deviates(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusBLOCKED),
	}
	for _, mode := range []domain.ExecutionMode{domain.ExecutionModeAuto, domain.ExecutionModeAutoReview} {
		dec := recordedStatusNext(t, mode, 0, domain.StatusBLOCKED, domain.ErrorUSER_CONTACT, log)

		dev := requireDeviation(t, dec)
		if dev.Info.Kind != domain.DeviationNonSuccess {
			t.Errorf("mode %v: want DeviationNonSuccess, got %q", mode, dev.Info.Kind)
		}
	}
}
