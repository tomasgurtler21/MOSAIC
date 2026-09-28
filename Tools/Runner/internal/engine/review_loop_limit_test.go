package engine_test

import (
	"strconv"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// Mode 3 review loop limit: before auto-routing a reviewer's
// COMPLETED_NEEDS_ACTION to its On Findings target, the engine counts that
// reviewer's CNA iterations at the current phase and stage. When the count
// reaches ReviewLoopLimit it returns a DeviationReviewLoopLimit deviation.
// ReviewLoopLimit 0 means no limit.

const (
	rllPhase    = "PLANNING"
	rllReviewer = "plan-review"
	rllFixer    = "planner-tdd-soft"
)

func rllRow(seq int, agent, phase, stage string, status domain.StatusCode) domain.ExecutionLogEntry {
	return domain.ExecutionLogEntry{
		Seq:    seq,
		Agent:  agent + "#" + strconv.Itoa(seq),
		Phase:  phase,
		Stage:  stage,
		Status: status,
	}
}

// rllNext runs engine.Next in the given mode for a CNA at the last log row,
// with the supplied log and limit.
func rllNext(t *testing.T, mode domain.ExecutionMode, limit int, log []domain.ExecutionLogEntry) domain.EngineDecision {
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
			LastStatus: last.Status,
			LastAgent:  last.Agent,
		},
		ExecutionLog: log,
	}
	state.ReviewLoopLimit = limit
	return engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: cnaResponse(last.Agent),
		Agents:       agents,
		Seq:          last.Seq,
		Now:          fixedNow,
		Mode:         mode,
	})
}

func requireLoopLimitDeviation(t *testing.T, dec domain.EngineDecision) domain.DeviationDecision {
	t.Helper()
	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationReviewLoopLimit {
		t.Fatalf("want DeviationReviewLoopLimit, got %q", dev.Info.Kind)
	}
	return dev
}

func requireRoutedTo(t *testing.T, dec domain.EngineDecision, want string) {
	t.Helper()
	step := requireDispatch(t, dec)
	if got := agentName(step.Request.AgentInstanceID); got != want {
		t.Errorf("want auto-route to %s, got %s", want, got)
	}
}

func TestNext_Mode3_ReviewLoopLimit_BelowLimit_AutoRoutes(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
}

func TestNext_Mode3_ReviewLoopLimit_CountReachesLimit_ReturnsDeviation(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(3, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(4, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	dev := requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log))
	if dev.Info.Response.StatusCode != domain.StatusCOMPLETED_NEEDS_ACTION {
		t.Errorf("deviation Response status: want CNA, got %q", dev.Info.Response.StatusCode)
	}
	if dev.Info.CurrentPhase != rllPhase || dev.Info.CurrentStage != "" {
		t.Errorf("deviation position: want %q/empty stage, got %q/%q",
			rllPhase, dev.Info.CurrentPhase, dev.Info.CurrentStage)
	}
}

func TestNext_Mode3_ReviewLoopLimit_One_DeviatesOnFirstFindings(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 1, log))
}

func TestNext_Mode3_ReviewLoopLimit_CountAboveLimit_ReturnsDeviation(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(4, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(5, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log))
}

func TestNext_Mode3_ReviewLoopLimit_NoLimit_NeverStops(t *testing.T) {
	var log []domain.ExecutionLogEntry
	seq := 0
	for i := 0; i < 12; i++ {
		seq++
		log = append(log, rllRow(seq, rllFixer, rllPhase, "", domain.StatusSUCCESS))
		seq++
		log = append(log, rllRow(seq, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION))
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 0, log), rllFixer)
}

func TestNext_Mode3_ReviewLoopLimit_OtherReviewerRows_DoNotCount(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, "test-runner", rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(3, "test-runner", rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(4, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(5, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
}

func TestNext_Mode3_ReviewLoopLimit_OtherStageRows_DoNotCount(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, rllPhase, "Stage-1", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, rllPhase, "Stage-1", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, rllPhase, "Stage-1", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(4, rllFixer, rllPhase, "Stage-2", domain.StatusSUCCESS),
		rllRow(5, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
}

func TestNext_Mode3_ReviewLoopLimit_OtherPhaseRows_DoNotCount(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, "REVIEW", "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, "REVIEW", "", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, "REVIEW", "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(4, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(5, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
}

func TestNext_Mode3_ReviewLoopLimit_ReviewerSuccessRows_DoNotCount(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(4, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(5, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
}

// A HITL-rejected CNA original followed by a re-dispatch that returned SUCCESS
// (routed on the original CNA) is one iteration and counts once.
func TestNext_Mode3_ReviewLoopLimit_RejectedThenRedispatchSuccess_CountsOnce(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusSUCCESS),
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 1, log))
}

// A rejected CNA and its CNA re-dispatch are one iteration, not two.
func TestNext_Mode3_ReviewLoopLimit_RejectedThenRedispatchCNA_CountsOnce(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 1, log))
}

// Two iterations, each with a re-dispatch, give count 2 and reach limit 2.
func TestNext_Mode3_ReviewLoopLimit_TwoIterationsWithRedispatches_ReachesLimit(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(5, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(6, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(7, rllReviewer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(8, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(9, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(10, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log))
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 3, log), rllFixer)
}

// A harness-error row followed by a CNA bypass retry is one iteration.
func TestNext_Mode3_ReviewLoopLimit_HarnessErrorThenCNARetry_CountsOnce(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(2, rllReviewer, rllPhase, "", domain.StatusBLOCKED),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	requireRoutedTo(t, rllNext(t, domain.ExecutionModeAutoReview, 2, log), rllFixer)
	requireLoopLimitDeviation(t, rllNext(t, domain.ExecutionModeAutoReview, 1, log))
}

// Mode 2 never produces a review-loop-limit deviation.
func TestNext_ReviewLoopLimit_Mode2_Unaffected(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	dev := requireDeviation(t, rllNext(t, domain.ExecutionModeAuto, 1, log))
	if dev.Info.Kind == domain.DeviationReviewLoopLimit {
		t.Errorf("Mode 2 must not produce DeviationReviewLoopLimit, got %q", dev.Info.Kind)
	}
}

// Mode 1 keeps consulting; the limit is not consulted.
func TestNext_ReviewLoopLimit_Mode1_Unaffected(t *testing.T) {
	log := []domain.ExecutionLogEntry{
		rllRow(1, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
		rllRow(2, rllFixer, rllPhase, "", domain.StatusSUCCESS),
		rllRow(3, rllReviewer, rllPhase, "", domain.StatusCOMPLETED_NEEDS_ACTION),
	}
	consult := requireConsult(t, rllNext(t, domain.ExecutionModeOrchestrated, 1, log))
	if consult.Trigger != domain.ConsultTriggerOrchestratedMode {
		t.Errorf("Mode 1: want ConsultTriggerOrchestratedMode, got %q", consult.Trigger)
	}
}
