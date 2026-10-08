package session_test

// Tests for the E501 budget on consultant-routed dispatches: a harness error
// or a failed raw-text bypass of an agent the consultant dispatched is retried
// mechanically while budget remains, a failed bypass is logged as its own
// BLOCKED/E501 row under the bypass instance, and the consultation happens
// only once the budget is used up.

import (
	"context"
	"fmt"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/harness"
)

// consultPathRun is the outcome of a run in which agent-a is BLOCKED/E401
// (one consultation), the consultant dispatches agent-b at its row, and
// agent-b's dispatches then follow the scripted entries.
type consultPathRun struct {
	got        domain.RunOutcome
	err        error
	consultant *scriptedRoutingConsultant
	f          *harness.MockAdapter
	store      *memStore
}

func runConsultPathDispatch(t *testing.T, mode domain.ExecutionMode, agentBEntries ...harness.ScriptedEntry) consultPathRun {
	t.Helper()
	consultant := &scriptedRoutingConsultant{}
	consultant.queueDispatch("agent-b", "take over the failed step", 1)
	consultant.queueStop("budget used up")
	ses, f, store, orchPath := newAutoSessionWithConsultant(t, consultant)
	f.Queue("agent-a", failingAgentEntry(domain.ErrorDEPENDENCY_MISSING))
	f.Queue("agent-b", agentBEntries...)

	got, err := ses.Start(context.Background(), configForMode(orchPath, mode))
	return consultPathRun{got: got, err: err, consultant: consultant, f: f, store: store}
}

// requireConsultedOnlyAfterBudget asserts the dispatched agent-b ran exactly
// the budget of attempts and that the second consultation, after the first
// one that dispatched it, carries the BLOCKED/E501 deviation.
func (r consultPathRun) requireConsultedOnlyAfterBudget(t *testing.T) {
	t.Helper()
	requireRunStatus(t, r.got, r.err, domain.RunStoppedByConsultant)
	if n := countInvocationsFor(r.f.Invocations(), "agent-b"); n != engine.E501AttemptLimit {
		t.Errorf("want %d agent-b dispatches before the next consultation, got %d", engine.E501AttemptLimit, n)
	}
	if r.consultant.CallCount != 2 {
		t.Fatalf("want 2 consultations (agent-a failure, then the used-up budget), got %d", r.consultant.CallCount)
	}
	requireBlockedE501Deviation(t, r.consultant.Requests[1])
	if n := countE501RowsFor(r.store, "agent-b"); n != engine.E501AttemptLimit {
		t.Errorf("want %d BLOCKED/E501 agent-b rows, got %d", engine.E501AttemptLimit, n)
	}
}

func TestSession_E501_ConsultPath_HarnessErrors_RedispatchedUntilBudgetUsedUp(t *testing.T) {
	for _, mode := range autoModes {
		t.Run(string(mode), func(t *testing.T) {
			entries := repeatedEntries(engine.E501AttemptLimit+2, failEntry("harness: subprocess timed out"))

			r := runConsultPathDispatch(t, mode, entries...)

			r.requireConsultedOnlyAfterBudget(t)
			requireErrorReasonCarries(t, r.consultant.Requests[1], "subprocess timed out")
		})
	}
}

func TestSession_E501_ConsultPath_FailedBypass_IsLoggedAsOwnRowAndCountsAsAnAttempt(t *testing.T) {
	for _, mode := range autoModes {
		t.Run(string(mode), func(t *testing.T) {
			r := runConsultPathDispatch(t, mode, rawTextEntry(), failEntry("harness: bypass timed out"),
				failEntry("harness: third attempt failed"),
				statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "must stay unused"))

			r.requireConsultedOnlyAfterBudget(t)
			rows := agentRows(r.store, "agent-b")
			if len(rows) != engine.E501AttemptLimit {
				t.Fatalf("want %d rows (error, failed bypass, third attempt), got %d: %+v", engine.E501AttemptLimit, len(rows), rows)
			}
			for i, row := range rows {
				if row.WorkflowRow != rows[0].WorkflowRow || row.Phase != rows[0].Phase || row.Stage != rows[0].Stage {
					t.Errorf("row %d: want the same row, phase and stage as the first attempt", i+1)
				}
			}
			// The bypass row sits under the bypass attempt's own instance id.
			if want := fmt.Sprintf("agent-b#%d", rows[1].Seq); rows[1].AgentInstance != want {
				t.Errorf("want the bypass row recorded under %q, got %q", want, rows[1].AgentInstance)
			}
			requireInstanceIDsMatchRecordedSeq(t, r.store)
		})
	}
}

// The bypass is one attempt of the budget: a raw-text error as the third
// attempt is not retried but consulted.
func TestSession_E501_ConsultPath_RawTextErrorWithNoBudgetLeft_SkipsBypass(t *testing.T) {
	for _, mode := range autoModes {
		t.Run(string(mode), func(t *testing.T) {
			r := runConsultPathDispatch(t, mode, failEntry("harness: first"), failEntry("harness: second"), rawTextEntry(),
				statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "a bypass would consume this"))

			r.requireConsultedOnlyAfterBudget(t)
		})
	}
}
