package engine_test

// Helpers for tests that drive engine.Next from a hand-built Execution Log
// whose entries carry the recorded workflow row.

import (
	"errors"
	"strconv"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// runLog builds an Execution Log one step at a time. Seq numbers are assigned
// in order, starting at 1, the way the session numbers invocations.
type runLog struct {
	entries []domain.ExecutionLogEntry
	// globalSeq overrides the artifact's GlobalSequence when non-zero.
	globalSeq int
}

// workflowStep appends a workflow step that ran the given 1-based row.
// The phase is the bare "EXECUTION" the Runner records.
func (l *runLog) workflowStep(name, stage string, status domain.StatusCode, row int) *runLog {
	seq := len(l.entries) + 1
	id := name + "#" + strconv.Itoa(seq)
	l.entries = append(l.entries, execLogEntry(seq, id, "EXECUTION", stage, status, row))
	return l
}

// infraStep appends an infrastructure step, which carries no workflow row.
func (l *runLog) infraStep(name, stage string) *runLog {
	seq := len(l.entries) + 1
	id := name + "#" + strconv.Itoa(seq)
	l.entries = append(l.entries, execLogEntry(seq, id, "EXECUTION", stage, domain.StatusSUCCESS, 0))
	return l
}

// last returns the most recent entry.
func (l *runLog) last() domain.ExecutionLogEntry {
	return l.entries[len(l.entries)-1]
}

// state returns the artifact state after the last entry. The last entry must
// be a workflow step: it supplies the current state.
func (l *runLog) state() domain.ArtifactState {
	seq := len(l.entries)
	if l.globalSeq != 0 {
		seq = l.globalSeq
	}
	return stateWithLog(seq, l.entries...)
}

// responseFor builds the protocol response matching the log entry's status.
func responseFor(entry domain.ExecutionLogEntry) *domain.ProtocolResponse {
	switch entry.Status {
	case domain.StatusSUCCESS:
		return successResponse(entry.Agent)
	case domain.StatusCOMPLETED_NEEDS_ACTION:
		return cnaResponse(entry.Agent)
	default:
		return nonSuccessResponse(entry.Agent, entry.Status)
	}
}

// nextAfterLog calls engine.Next in auto-review mode for the step that ended
// the log.
func nextAfterLog(
	aw domain.AdmittedWorkflow,
	stages *domain.StageSet,
	agents map[string]domain.AgentReference,
	log *runLog,
) domain.EngineDecision {
	last := log.last()
	return engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        log.state(),
		LastResponse: responseFor(last),
		Agents:       agents,
		Seq:          last.Seq,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})
}

// engineNextAuto calls engine.Next in auto-review mode with an explicit state.
func engineNextAuto(
	aw domain.AdmittedWorkflow,
	stages *domain.StageSet,
	state domain.ArtifactState,
	resp *domain.ProtocolResponse,
) domain.EngineDecision {
	return engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: resp,
		Agents:       buildVerifiedAgents(),
		Seq:          state.GlobalSequence,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})
}

// requireNextStep calls nextAfterLog and requires a single dispatch.
func requireNextStep(
	t *testing.T,
	aw domain.AdmittedWorkflow,
	stages *domain.StageSet,
	agents map[string]domain.AgentReference,
	log *runLog,
) domain.DispatchStep {
	t.Helper()
	return requireDispatch(t, nextAfterLog(aw, stages, agents, log))
}

// buildVerifiedAgents returns the agent references for the build-verified
// workflow used by the row-drift tests.
func buildVerifiedAgents() map[string]domain.AgentReference {
	return newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "build-review", "tests-review-tdd",
		"implementation-tdd", "implementation-review",
	)
}

// 1-based routing-table rows of the build-verified workflow's EXECUTION
// phase, in table order.
const (
	bvRowTestWriter     = 8
	bvRowTestBuild      = 9
	bvRowTestsReview    = 10
	bvRowImplementation = 11
	bvRowImplBuild      = 12
	bvRowImplReview     = 13
)

// inPhase sets the phase of every logged entry. workflowStep records the bare
// EXECUTION phase; rows of other phases record their own.
func (l *runLog) inPhase(phase string) *runLog {
	for i := range l.entries {
		l.entries[i].Phase = phase
	}
	return l
}

// lastInPhase sets the phase of the most recent entry only.
func (l *runLog) lastInPhase(phase string) *runLog {
	l.entries[len(l.entries)-1].Phase = phase
	return l
}

// requirePositionCause asserts a Stop whose typed cause is a
// *domain.PositionUnresolvedError with the given Cause, and returns it.
func requirePositionCause(
	t *testing.T, dec domain.EngineDecision, want domain.PositionUnresolvedCause,
) *domain.PositionUnresolvedError {
	t.Helper()
	stop := requireStop(t, dec)
	var perr *domain.PositionUnresolvedError
	if !errors.As(stop.Err, &perr) {
		t.Fatalf("stop must carry a *PositionUnresolvedError, got Err=%v (reason %q)", stop.Err, stop.Reason)
	}
	if perr.Cause != want {
		t.Errorf("position cause: want %d, got %d (%v)", want, perr.Cause, perr)
	}
	return perr
}
