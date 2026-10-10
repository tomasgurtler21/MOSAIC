package engine_test

// BLOCKED / E501 in the auto and auto-review modes: the engine re-dispatches
// the same assignment without consultation until E501AttemptLimit attempts at
// the row and stage have failed since the last SUCCESS there. The count comes
// from the Execution Log alone, using each entry's recorded error code.

import (
	"errors"
	"strconv"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

const (
	e501 = domain.ErrorTOOL_UNAVAILABLE
	e502 = domain.ErrorPERMISSION_DENIED
)

// e501Log builds a log of n BLOCKED/E501 rows of the planner row.
func e501Log(n int) *runLog {
	return (&runLog{}).repeat(n, func(l *runLog) { l.blocked(rtPlanner, "", e501, rtRowPlanner) }).
		inPhase(rtPlanPhase)
}

func TestNext_E501_BudgetLeft_RedispatchesSameRow(t *testing.T) {
	f := newRetryFixture(t)
	for _, mode := range autoModes {
		for n := 1; n < engine.E501AttemptLimit; n++ {
			t.Run(modeName(mode)+"/failures="+strconv.Itoa(n), func(t *testing.T) {
				step := requireRetryStep(t, f.next(mode, e501Log(n), false),
					rtRowPlanner, "", domain.RetryToolUnavailable)

				if step.Request.AgentInstanceID != rtPlanner+"#"+strconv.Itoa(n+1) {
					t.Errorf("agent instance: got %s", step.Request.AgentInstanceID)
				}
				if step.Request.TaskDescription != "" {
					t.Errorf("an E501 retry leaves the task description to the session, got %q",
						step.Request.TaskDescription)
				}
			})
		}
	}
}

func TestNext_E501_BudgetUsedUp_Deviates(t *testing.T) {
	f := newRetryFixture(t)
	for _, mode := range autoModes {
		t.Run(modeName(mode), func(t *testing.T) {
			dev := requireNonSuccessDeviation(t, f.next(mode, e501Log(engine.E501AttemptLimit), false))

			if dev.Info.Response.ErrorCode != e501 {
				t.Errorf("deviation must carry the E501 response, got %q", dev.Info.Response.ErrorCode)
			}
		})
	}
}

func TestNext_E501_Orchestrated_Consults(t *testing.T) {
	f := newRetryFixture(t)

	requireConsult(t, f.next(domain.ExecutionModeOrchestrated, e501Log(1), false))
}

func TestNext_E501_StagedRow_RedispatchesSameStage(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).blocked(rtTestWriter, "Test.2", e501, rtRowTestWriter)

	requireRetryStep(t, f.next(domain.ExecutionModeAuto, log, false),
		rtRowTestWriter, "Test.2", domain.RetryToolUnavailable)
}

// A failed harness invocation is recorded as BLOCKED/E501 and spends the same
// budget as an agent-returned E501.
func TestNext_E501_HarnessErrorRows_CountTowardSameBudget(t *testing.T) {
	f := newRetryFixture(t)
	harness := domain.HarnessErrorResponse(rtPlanner+"#3", "run", errors.New("harness exited with status 1"))
	build := func(rows int) *runLog {
		log := &runLog{}
		for i := 0; i < rows-1; i++ {
			log.blocked(rtPlanner, "", e501, rtRowPlanner)
		}
		log.stepWith(rtPlanner, "", harness.StatusCode, harness.ErrorCode, rtRowPlanner, harness.StatusMessage)
		return log.inPhase(rtPlanPhase)
	}
	next := func(log *runLog) domain.EngineDecision {
		state := log.state()
		return engine.Next(engine.NextInput{
			Workflow: f.aw, Stages: f.stages, State: state, LastResponse: &harness,
			Agents: f.agents, Seq: log.last().Seq, Now: fixedNow, Mode: domain.ExecutionModeAuto,
		})
	}

	requireRetryStep(t, next(build(engine.E501AttemptLimit-1)), rtRowPlanner, "", domain.RetryToolUnavailable)
	requireNonSuccessDeviation(t, next(build(engine.E501AttemptLimit)))
}

func TestNext_E501_SuccessAtSameRowAndStage_ResetsBudget(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).
		blocked(rtPlanner, "", e501, rtRowPlanner).
		blocked(rtPlanner, "", e501, rtRowPlanner).
		ok(rtPlanner, "", rtRowPlanner).
		blocked(rtPlanner, "", e501, rtRowPlanner).inPhase(rtPlanPhase)

	requireRetryStep(t, f.next(domain.ExecutionModeAuto, log, false), rtRowPlanner, "", domain.RetryToolUnavailable)
}

func TestNext_E501_FailuresAfterSuccess_UseUpFreshBudget(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).blocked(rtPlanner, "", e501, rtRowPlanner).ok(rtPlanner, "", rtRowPlanner).
		repeat(engine.E501AttemptLimit, func(l *runLog) { l.blocked(rtPlanner, "", e501, rtRowPlanner) }).
		inPhase(rtPlanPhase)

	requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAuto, log, false))
}

// Only a SUCCESS at the same row (and stage) resets the window.
func TestNext_E501_SuccessAtOtherRow_DoesNotReset(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).
		blocked(rtPlanner, "", e501, rtRowPlanner).
		blocked(rtPlanner, "", e501, rtRowPlanner).
		ok(rtPlanReview, "", rtRowPlanReview).
		blocked(rtPlanner, "", e501, rtRowPlanner).inPhase(rtPlanPhase)

	requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAuto, log, false))
}

// An infrastructure row (no workflow row) is not a SUCCESS at the retried row
// and does not reset the window.
func TestNext_E501_InfrastructureRowInWindow_DoesNotReset(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).
		blocked(rtPlanner, "", e501, rtRowPlanner).
		blocked(rtPlanner, "", e501, rtRowPlanner).
		infraStep("checkpoint", "").
		blocked(rtPlanner, "", e501, rtRowPlanner).inPhase(rtPlanPhase)

	requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAuto, log, false))
}

func TestNext_E501_NewStageOfStagedRow_HasFreshBudget(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).
		repeat(engine.E501AttemptLimit-1, func(l *runLog) { l.blocked(rtTestWriter, "Test.1", e501, rtRowTestWriter) }).
		blocked(rtTestWriter, "Test.2", e501, rtRowTestWriter)

	requireRetryStep(t, f.next(domain.ExecutionModeAuto, log, false),
		rtRowTestWriter, "Test.2", domain.RetryToolUnavailable)
}

func TestNext_E501_SuccessAtOtherStage_DoesNotReset(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).
		repeat(engine.E501AttemptLimit-1, func(l *runLog) { l.blocked(rtTestWriter, "Test.1", e501, rtRowTestWriter) }).
		ok(rtTestWriter, "Test.2", rtRowTestWriter).
		blocked(rtTestWriter, "Test.1", e501, rtRowTestWriter)

	requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAuto, log, false))
}

// BLOCKED rows with another code, or no code, do not spend the E501 budget.
func TestNext_E501_OtherBlockedRowsInWindow_DoNotUseBudget(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).
		blocked(rtPlanner, "", e501, rtRowPlanner).
		blocked(rtPlanner, "", e502, rtRowPlanner).
		blocked(rtPlanner, "", domain.ErrorNone, rtRowPlanner).
		blocked(rtPlanner, "", e501, rtRowPlanner).inPhase(rtPlanPhase)

	requireRetryStep(t, f.next(domain.ExecutionModeAuto, log, false), rtRowPlanner, "", domain.RetryToolUnavailable)
}

// Resume: no response is supplied, the recorded status and code decide.
func TestNext_E501_Resume_RecognisesRecordedCodeAndContinuesBudget(t *testing.T) {
	f := newRetryFixture(t)
	for _, mode := range autoModes {
		t.Run(modeName(mode), func(t *testing.T) {
			requireRetryStep(t, f.next(mode, e501Log(engine.E501AttemptLimit-1), true),
				rtRowPlanner, "", domain.RetryToolUnavailable)
			requireNonSuccessDeviation(t, f.next(mode, e501Log(engine.E501AttemptLimit), true))
		})
	}
}

func TestNext_E501_Resume_OtherRecordedCode_Deviates(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).blocked(rtPlanner, "", e502, rtRowPlanner).inPhase(rtPlanPhase)

	requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAuto, log, true))
}

func TestNext_E501_Resume_RecordedBlockedWithoutCode_Deviates(t *testing.T) {
	f := newRetryFixture(t)
	log := (&runLog{}).blocked(rtPlanner, "", domain.ErrorNone, rtRowPlanner).inPhase(rtPlanPhase)

	requireNonSuccessDeviation(t, f.next(domain.ExecutionModeAuto, log, true))
}

func TestE501BudgetRemaining_CountsRecordedCodeSinceLastSuccessAtRowAndStage(t *testing.T) {
	entry := func(row int, stage string, status domain.StatusCode, code domain.ErrorCode) domain.ExecutionLogEntry {
		e := execLogEntry(1, "agent#1", "EXECUTION", stage, status, row)
		e.ErrorCode = code
		return e
	}
	blockedE501 := func(row int, stage string) domain.ExecutionLogEntry {
		return entry(row, stage, domain.StatusBLOCKED, e501)
	}
	cases := []struct {
		name  string
		log   []domain.ExecutionLogEntry
		stage string
		want  int
	}{
		{"empty log", nil, "", engine.E501AttemptLimit},
		{"one failure", []domain.ExecutionLogEntry{blockedE501(4, "")}, "", engine.E501AttemptLimit - 1},
		{"limit reached", []domain.ExecutionLogEntry{blockedE501(4, ""), blockedE501(4, ""), blockedE501(4, "")}, "", 0},
		{"never negative", []domain.ExecutionLogEntry{
			blockedE501(4, ""), blockedE501(4, ""), blockedE501(4, ""), blockedE501(4, ""), blockedE501(4, ""),
		}, "", 0},
		{"success resets", []domain.ExecutionLogEntry{
			blockedE501(4, ""), blockedE501(4, ""), entry(4, "", domain.StatusSUCCESS, ""), blockedE501(4, ""),
		}, "", engine.E501AttemptLimit - 1},
		{"success at other row ignored", []domain.ExecutionLogEntry{
			blockedE501(4, ""), entry(5, "", domain.StatusSUCCESS, ""), blockedE501(4, ""),
		}, "", engine.E501AttemptLimit - 2},
		{"other row ignored", []domain.ExecutionLogEntry{blockedE501(5, ""), blockedE501(4, "")}, "",
			engine.E501AttemptLimit - 1},
		{"other stage ignored", []domain.ExecutionLogEntry{blockedE501(8, "Test.1"), blockedE501(8, "Test.2")},
			"Test.2", engine.E501AttemptLimit - 1},
		{"other code ignored", []domain.ExecutionLogEntry{
			entry(4, "", domain.StatusBLOCKED, e502), entry(4, "", domain.StatusBLOCKED, ""), blockedE501(4, ""),
		}, "", engine.E501AttemptLimit - 1},
		{"E501 code on non-blocked row ignored", []domain.ExecutionLogEntry{
			entry(4, "", domain.StatusPARTIALLY_DONE, e501), blockedE501(4, ""),
		}, "", engine.E501AttemptLimit - 1},
		{"rows without workflow row ignored", []domain.ExecutionLogEntry{blockedE501(0, ""), blockedE501(4, "")}, "",
			engine.E501AttemptLimit - 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := domain.WorkflowRow(4)
			if tc.stage != "" {
				row = domain.WorkflowRow(8)
			}

			if got := engine.E501BudgetRemaining(tc.log, row, tc.stage); got != tc.want {
				t.Errorf("E501BudgetRemaining: want %d, got %d", tc.want, got)
			}
		})
	}
}
