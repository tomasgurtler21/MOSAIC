package session_test

// Tests for the anti-loop guard together with mechanical re-dispatches and for
// the bound on the guard's own escalation path: when the guard blocks a
// dispatch and the consultation answers with the same dispatch again, the run
// must end with a resumable outcome instead of consulting forever.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/harness"
)

const (
	// guardBound is the number of consecutive same-agent dispatches at one row
	// the guard allows.
	guardBound = 4
	// guardCycleMaxConsultations is the explicit upper bound on consultations
	// in a run whose consultant always requests the same dispatch: the
	// dispatches the guard allows, the escalations, and generous slack.
	guardCycleMaxConsultations = 10
	// consultantSafetyCap is the number of consultations after which the
	// always-same-dispatch double gives up, so a missing bound fails the test
	// instead of hanging it.
	consultantSafetyCap = 20
)

// sameDispatchConsultant answers every consultation with the same dispatch
// instruction. After safetyCap consultations it answers with a stop and records
// that the cap was reached.
type sameDispatchConsultant struct {
	agent      string
	rowIndex   int
	safetyCap  int
	calls      int
	capReached bool
}

func (c *sameDispatchConsultant) ConsultRouting(_ context.Context, _ domain.ConsultationRequest) (domain.RoutingInstruction, error) {
	c.calls++
	if c.calls > c.safetyCap {
		c.capReached = true
		return domain.RoutingInstruction{Stop: &domain.StopInstruction{Reason: "safety cap of the test double reached"}}, nil
	}
	return domain.RoutingInstruction{Dispatch: &domain.DispatchInstruction{
		Agent:           c.agent,
		RowIndex:        c.rowIndex,
		TaskDescription: "request the same dispatch again",
	}}, nil
}

// requireGuardEscalationEnd asserts the run ended through the escalation bound:
// a resumable outcome (not the double's cap stop) whose message names the agent
// and the 1-based row and says the anti-loop guard kept blocking the
// consultant's dispatch, after a bounded number of consultations.
func requireGuardEscalationEnd(t *testing.T, got domain.RunOutcome, err error, c *sameDispatchConsultant, wantRow1Based int) {
	t.Helper()
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if c.capReached {
		t.Fatalf("the consultant was consulted %d times without the run ending: the guard escalation cycle is not bounded", c.safetyCap)
	}
	if got.Status != domain.RunStopped && got.Status != domain.RunDeviationUnresolved {
		t.Errorf("want a resumable outcome from the escalation bound (stopped or deviation-unresolved), got %q (message: %q)",
			got.Status, got.Message)
	}
	if c.calls > guardCycleMaxConsultations {
		t.Errorf("want at most %d consultations, got %d", guardCycleMaxConsultations, c.calls)
	}
	msg := strings.ToLower(got.Message)
	if !strings.Contains(msg, strings.ToLower(c.agent)) {
		t.Errorf("want the outcome message to name agent %q, got %q", c.agent, got.Message)
	}
	rowPattern := regexp.MustCompile(fmt.Sprintf(`row\D{0,3}%d\b`, wantRow1Based))
	if !rowPattern.MatchString(msg) {
		t.Errorf("want the outcome message to name the 1-based row %d, got %q", wantRow1Based, got.Message)
	}
	if !strings.Contains(msg, "anti-loop") && !strings.Contains(msg, "guard") {
		t.Errorf("want the outcome message to say the anti-loop guard blocked the requested dispatch, got %q", got.Message)
	}
}

func failingAgentEntry(code domain.ErrorCode) harness.ScriptedEntry {
	return statusEntry(domain.StatusBLOCKED, code, "cannot continue")
}

// ===== (a) mechanical retries within the policy bounds are not blocked =====

func TestSession_AntiLoopGuard_DoesNotBlockMechanicalRetriesWithinBounds(t *testing.T) {
	cases := []struct {
		name    string
		entries []harness.ScriptedEntry
		wantA   int
	}{
		{"PARTIALLY_DONE up to the re-dispatch bound, then SUCCESS", append(
			repeatedEntries(engine.PartiallyDoneRedispatchLimit, statusEntry(domain.StatusPARTIALLY_DONE, domain.ErrorNone, "half done")),
			statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done")), 1 + engine.PartiallyDoneRedispatchLimit},
		{"E501 up to just inside the budget, then SUCCESS", append(
			repeatedEntries(engine.E501AttemptLimit-1, e501Entry()),
			statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done")), engine.E501AttemptLimit},
		{"harness errors up to just inside the budget, then SUCCESS", append(
			repeatedEntries(engine.E501AttemptLimit-1, failEntry("harness: subprocess timed out")),
			statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done")), engine.E501AttemptLimit},
	}
	for _, mode := range autoModes {
		for _, tc := range cases {
			t.Run(string(mode)+"/"+tc.name, func(t *testing.T) {
				consultant := &scriptedRoutingConsultant{}
				ses, f, _, orchPath := newAutoSessionWithConsultant(t, consultant)
				f.Queue("agent-a", tc.entries...)
				f.Queue("agent-b", statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done"))

				got, err := ses.Start(context.Background(), configForMode(orchPath, mode))

				requireRunStatus(t, got, err, domain.RunCompleted)
				if consultant.CallCount != 0 {
					t.Errorf("want no consultation (the guard must not escalate a policy retry), got %d", consultant.CallCount)
				}
				if n := countInvocationsFor(f.Invocations(), "agent-a"); n != tc.wantA {
					t.Errorf("want %d agent-a dispatches, got %d", tc.wantA, n)
				}
			})
		}
	}
}

func repeatedEntries(n int, e harness.ScriptedEntry) []harness.ScriptedEntry {
	out := make([]harness.ScriptedEntry, n)
	for i := range out {
		out[i] = e
	}
	return out
}

// ===== (b) the guard escalation cycle terminates =====

func TestSession_AntiLoopGuard_Orchestrated_ConsultantKeepsRequestingBlockedDispatch_EndsResumable(t *testing.T) {
	// agent-b is the agent of row 2: the outcome must name the 1-based row.
	consultant := &sameDispatchConsultant{agent: "agent-b", rowIndex: 1, safetyCap: consultantSafetyCap}
	ses, f, store, orchPath := newOrchestratedSession(t, consultant)
	queueRepeated(f, "agent-b", 12, failingAgentEntry(domain.ErrorDEPENDENCY_MISSING))

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	requireGuardEscalationEnd(t, got, err, consultant, 2)
	n := countInvocationsFor(f.Invocations(), "agent-b")
	if n != guardBound {
		t.Errorf("want exactly %d agent-b dispatches before the guard blocks, got %d", guardBound, n)
	}
	if len(store.Applied) != n {
		t.Errorf("want one Execution Log row per dispatch (blocked attempts write none): %d rows for %d dispatches",
			len(store.Applied), n)
	}
}

func TestSession_AntiLoopGuard_Auto_ConsultantKeepsRequestingBlockedDispatch_EndsResumable(t *testing.T) {
	cases := []struct {
		name  string
		entry harness.ScriptedEntry
	}{
		{"agent fails with E401", failingAgentEntry(domain.ErrorDEPENDENCY_MISSING)},
		{"agent fails with E501 after the budget is used", e501Entry()},
	}
	for _, mode := range autoModes {
		for _, tc := range cases {
			t.Run(string(mode)+"/"+tc.name, func(t *testing.T) {
				consultant := &sameDispatchConsultant{agent: "agent-a", rowIndex: 0, safetyCap: consultantSafetyCap}
				ses, f, store, orchPath := newAutoSessionWithConsultant(t, consultant)
				queueRepeated(f, "agent-a", 12, tc.entry)

				got, err := ses.Start(context.Background(), configForMode(orchPath, mode))

				requireGuardEscalationEnd(t, got, err, consultant, 1)
				n := countInvocationsFor(f.Invocations(), "agent-a")
				// Mechanical dispatches up to the policy bounds plus the dispatches
				// the guard allows the consultant.
				if limit := engine.E501AttemptLimit + guardBound; n > limit {
					t.Errorf("want at most %d agent-a dispatches, got %d", limit, n)
				}
				if len(store.Applied) != n {
					t.Errorf("want one Execution Log row per dispatch (blocked attempts write none): %d rows for %d dispatches",
						len(store.Applied), n)
				}
			})
		}
	}
}

// ===== (c) the same cycle entered from a HITL re-dispatch =====

func TestSession_AntiLoopGuard_HITLRedispatch_ConsultantKeepsRequestingBlockedDispatch_EndsResumable(t *testing.T) {
	t.Run("orchestrated, consult-path re-dispatch", func(t *testing.T) {
		consultant := &sameDispatchConsultant{agent: "agent-a", rowIndex: 0, safetyCap: consultantSafetyCap}
		ses, f, store, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})
		// The consultant's first guardBound-1 dispatches fail without a HITL
		// gate and the next one succeeds into a rejected approval, so the
		// HITL re-dispatch is the dispatch beyond the bound whichever way the
		// guard counts that re-dispatch itself.
		queueRepeated(f, "agent-a", guardBound-1, failingAgentEntry(domain.ErrorDEPENDENCY_MISSING))
		queueRepeated(f, "agent-a", 12, statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done"))

		got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

		requireGuardEscalationEnd(t, got, err, consultant, 1)
		requireHITLEntryBlocked(t, f, store)
	})
	t.Run("auto, engine-path re-dispatch", func(t *testing.T) {
		consultant := &sameDispatchConsultant{agent: "agent-a", rowIndex: 0, safetyCap: consultantSafetyCap}
		ses, f, store, orchPath := newAutoHITLSession(t, &fixedApprovalReader{domain.ApprovalFalse}, consultant)
		// The engine re-dispatches PARTIALLY_DONE mechanically; the accepted
		// SUCCESS that follows is then gated by HITL.
		queueRepeated(f, "agent-a", engine.PartiallyDoneRedispatchLimit,
			statusEntry(domain.StatusPARTIALLY_DONE, domain.ErrorNone, "half done"))
		queueRepeated(f, "agent-a", 12, statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done"))

		got, err := ses.Start(context.Background(), configForMode(orchPath, domain.ExecutionModeAuto))

		requireGuardEscalationEnd(t, got, err, consultant, 1)
		requireHITLRejectedSteps(t, store)
		if n := countInvocationsFor(f.Invocations(), "agent-a"); len(store.Applied) != n {
			t.Errorf("want one Execution Log row per dispatch (blocked attempts write none): %d rows for %d dispatches",
				len(store.Applied), n)
		}
	})
}

// requireHITLEntryBlocked asserts the guard stopped the HITL re-dispatch of
// the last allowed dispatch: that dispatch was HITL-rejected, exactly
// guardBound agent-a dispatches ran, and the blocked re-dispatch wrote no row.
func requireHITLEntryBlocked(t *testing.T, f *harness.MockAdapter, store *memStore) {
	t.Helper()
	requireHITLRejectedSteps(t, store)
	n := countInvocationsFor(f.Invocations(), "agent-a")
	if n != guardBound {
		t.Errorf("want exactly %d agent-a dispatches (the HITL re-dispatch beyond the bound blocked), got %d", guardBound, n)
	}
	if len(store.Applied) != n {
		t.Errorf("want one Execution Log row per dispatch (blocked attempts write none): %d rows for %d dispatches",
			len(store.Applied), n)
	}
}

// ===== (d) a consultant that changes course is not stopped =====

func TestSession_AntiLoopGuard_ConsultantChangingCourseAfterEscalation_ContinuesNormally(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	for i := 0; i < guardBound+1; i++ {
		consultant.queueDispatch("agent-a", "try again", 0) // the last one is blocked by the guard
	}
	consultant.queueDispatch("agent-b", "different dispatch after the escalation", 1)
	consultant.queueStop("consultant finished the run")
	ses, f, _, orchPath := newOrchestratedSession(t, consultant)
	queueRepeated(f, "agent-a", 12, failingAgentEntry(domain.ErrorDEPENDENCY_MISSING))
	f.Queue("agent-b", statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done"))

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	if n := countInvocationsFor(f.Invocations(), "agent-a"); n != guardBound {
		t.Errorf("want %d agent-a dispatches (the next one blocked by the guard), got %d", guardBound, n)
	}
	if n := countInvocationsFor(f.Invocations(), "agent-b"); n != 1 {
		t.Errorf("want the changed dispatch to run agent-b once, got %d", n)
	}
	if got.StopReason != "consultant finished the run" {
		t.Errorf("want the run to end with the consultant's own stop, got %q (message: %q)", got.StopReason, got.Message)
	}
}

// Two guard escalations separated by an actual dispatch are not consecutive:
// the escalation counter resets when an agent is dispatched.
func TestSession_AntiLoopGuard_EscalationCounterResetsOnDispatch(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	for round := 0; round < 3; round++ {
		for i := 0; i < guardBound+1; i++ {
			consultant.queueDispatch("agent-a", "try again", 0) // the last one of each round is blocked
		}
		consultant.queueDispatch("agent-b", "different dispatch after the escalation", 1)
	}
	consultant.queueStop("consultant finished the run")
	ses, f, _, orchPath := newOrchestratedSession(t, consultant)
	queueRepeated(f, "agent-a", 3*guardBound+3, failingAgentEntry(domain.ErrorDEPENDENCY_MISSING))
	queueRepeated(f, "agent-b", 3, statusEntry(domain.StatusSUCCESS, domain.ErrorNone, "done"))

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunStoppedByConsultant)
	if got.StopReason != "consultant finished the run" {
		t.Errorf("want the run to end with the consultant's own stop after three non-consecutive escalations, got %q (message: %q)",
			got.StopReason, got.Message)
	}
	if n := countInvocationsFor(f.Invocations(), "agent-b"); n != 3 {
		t.Errorf("want agent-b dispatched after each escalation (3), got %d", n)
	}
}
