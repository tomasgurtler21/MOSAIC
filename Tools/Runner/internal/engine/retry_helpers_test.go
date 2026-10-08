package engine_test

// Helpers for the mechanical-retry tests: a fixture over the brownfield TDD
// workflow and log builders that record error codes and summaries.

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// 1-based rows and agents of brownfieldTDDContent used by the retry tests.
const (
	rtRowPlanner     = 4 // PLANNING, non-staged
	rtRowPlanReview  = 5 // PLANNING, non-staged
	rtRowTestWriter  = 8 // EXECUTION.Test, staged
	rtRowTestsReview = 9 // EXECUTION.Test, staged; On Findings names test-writer-tdd

	rtPlanner     = "planner-tdd-soft"
	rtPlanReview  = "plan-review"
	rtTestWriter  = "test-writer-tdd"
	rtTestsReview = "tests-review-tdd"
	rtPlanPhase   = "PLANNING"
	rtLastMessage = "wrote half of the plan, stopped at the stage list"
)

// retryFixture is the workflow, stage set and agents a retry test runs on.
type retryFixture struct {
	aw     domain.AdmittedWorkflow
	stages *domain.StageSet
	agents map[string]domain.AgentReference
}

func newRetryFixture(t *testing.T) retryFixture {
	t.Helper()
	return retryFixture{
		aw:     mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "3.4"),
		stages: twoStageSet("TDD"),
		agents: newTestAgents(
			"codebase-research", "requirements-refinement", "requirements-review",
			rtPlanner, rtPlanReview, "contracts-designer", "contracts-review",
			rtTestWriter, rtTestsReview, "implementation-tdd", "implementation-review",
			"test-runner",
		),
	}
}

// stepWith appends a workflow step with an explicit error code and summary.
func (l *runLog) stepWith(name, stage string, status domain.StatusCode, code domain.ErrorCode,
	row int, summary string) *runLog {
	l.workflowStep(name, stage, status, row)
	last := &l.entries[len(l.entries)-1]
	last.ErrorCode = code
	last.Summary = summary
	return l
}

// partial appends a PARTIALLY_DONE step.
func (l *runLog) partial(name, stage string, row int) *runLog {
	return l.stepWith(name, stage, domain.StatusPARTIALLY_DONE, domain.ErrorNone, row, rtLastMessage)
}

// blocked appends a BLOCKED step with the given error code.
func (l *runLog) blocked(name, stage string, code domain.ErrorCode, row int) *runLog {
	return l.stepWith(name, stage, domain.StatusBLOCKED, code, row, "blocked")
}

// ok appends a SUCCESS step.
func (l *runLog) ok(name, stage string, row int) *runLog {
	return l.stepWith(name, stage, domain.StatusSUCCESS, domain.ErrorNone, row, "done")
}

// repeat appends n copies of the step built by add.
func (l *runLog) repeat(n int, add func(*runLog)) *runLog {
	for i := 0; i < n; i++ {
		add(l)
	}
	return l
}

// responseWithCode is the live response for a log entry, carrying the entry's
// status, message and error code.
func responseWithCode(entry domain.ExecutionLogEntry) *domain.ProtocolResponse {
	return &domain.ProtocolResponse{
		AgentInstanceID: entry.Agent,
		StatusCode:      entry.Status,
		StatusMessage:   entry.Summary,
		ErrorCode:       entry.ErrorCode,
	}
}

// next runs engine.Next for the step that ended the log. With resume, no
// response is supplied and the status and code come from the recorded state.
func (f retryFixture) next(mode domain.ExecutionMode, log *runLog, resume bool,
	lastOutputs ...string) domain.EngineDecision {
	last := log.last()
	state := log.state()
	state.CurrentState.ErrorCode = last.ErrorCode
	in := engine.NextInput{
		Workflow:            f.aw,
		Stages:              f.stages,
		State:               state,
		Agents:              f.agents,
		Seq:                 last.Seq,
		Now:                 fixedNow,
		Mode:                mode,
		LastOutputArtifacts: lastOutputs,
	}
	if !resume {
		in.LastResponse = responseWithCode(last)
	}
	return engine.Next(in)
}

// autoModes are the execution modes in which the engine retries mechanically.
var autoModes = []domain.ExecutionMode{domain.ExecutionModeAuto, domain.ExecutionModeAutoReview}

func modeName(m domain.ExecutionMode) string { return "mode-" + string(m) }

// requireRetryStep requires a single dispatch of the given 1-based row and
// recorded stage, marked as the given retry kind.
func requireRetryStep(t *testing.T, dec domain.EngineDecision, wantRow int, wantStage string,
	kind domain.RetryKind) domain.DispatchStep {
	t.Helper()
	step := requireDispatch(t, dec)
	requireStepAt(t, step, wantRow-1, wantStage)
	if step.Retry != kind {
		t.Errorf("dispatch Retry: want %q, got %q", kind, step.Retry)
	}
	return step
}

// requireNonSuccessDeviation requires the generic non-SUCCESS deviation.
func requireNonSuccessDeviation(t *testing.T, dec domain.EngineDecision) domain.DeviationDecision {
	t.Helper()
	dev := requireDeviation(t, dec)
	if dev.Info.Kind != domain.DeviationNonSuccess {
		t.Errorf("want DeviationNonSuccess, got %q", dev.Info.Kind)
	}
	return dev
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func hasText(s, sub string) bool { return strings.Contains(s, sub) }

// nextAfterState runs engine.Next in auto-review mode for an explicit state and
// response, with the state's last sequence number.
func nextAfterState(aw domain.AdmittedWorkflow, stages *domain.StageSet,
	agents map[string]domain.AgentReference, state domain.ArtifactState,
	resp *domain.ProtocolResponse) domain.EngineDecision {
	return engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: resp,
		Agents:       agents,
		Seq:          state.GlobalSequence,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})
}
