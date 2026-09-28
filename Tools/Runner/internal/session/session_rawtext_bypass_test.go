package session_test

// Tests for the raw-text reply bypass feature (Stage 4).
//
// Coverage:
//
//   Main dispatch loop bypass (auto-routed path, session.go ~line 897):
//   - Raw-text sentinel error (ErrProtocolNotExtractable): bypass succeeds on
//     direct redispatch, run continues normally without consulting orchestrator.
//   - Raw-text sentinel error (ErrMalformedJSON): bypass fails on redispatch,
//     session falls back to consultRoute as today; consultant called once fewer
//     time than without bypass.
//
//   Consultant-routed dispatch bypass (consultRoute path, session.go ~line 1776):
//   - Raw-text sentinel error (ErrEmptyResponse): bypass inside consultRoute
//     succeeds, no recursive consultRoute call triggered; fewer harness
//     invocations observed than the no-bypass path.
//
//   HITL-redispatch fallback bypass (hitlCheckLoop path, session.go ~line 1088):
//   - Raw-text sentinel error (ErrProtocolNotExtractable): bypass inside the
//     HITL-redispatch branch succeeds, producing one extra harness invocation
//     compared to the no-bypass path.
//
// (Non-sentinel, anti-loop, and persistence tests are in
// session_rawtext_bypass_persistence_test.go.)

import (
	"context"
	"fmt"
	"testing"

	commonharness "mosaic-common/harness"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
	"mosaic-run/internal/session"
)

// ===== helpers =====

// wrapSentinel wraps a commonharness sentinel error in a realistic error chain
// matching what a real harness adapter would produce.
func wrapSentinel(sentinel error) error {
	return fmt.Errorf("agent output: %w", sentinel)
}

// newAutoSessionWithConsultant builds a session backed by the linear-orch.md
// fixture in auto-execution mode, with a RoutingConsultant wired for fallback
// paths. The MockAdapter and memStore are returned so tests can configure them.
func newAutoSessionWithConsultant(t *testing.T, consultant domain.RoutingConsultant) (
	ses session.Session, f *harness.MockAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewMockAdapter()
	store = &memStore{}
	ses = session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})
	return
}

// countInvocationsFor returns the number of times agentID was invoked in invs.
func countInvocationsFor(invs []harness.Invocation, agentID string) int {
	n := 0
	for _, inv := range invs {
		if inv.Agent.Identifier == agentID {
			n++
		}
	}
	return n
}

// requireHarnessErrorStep asserts that store.Applied contains at least one
// accepted workflow CompletedStep (neither infrastructure nor HITL rejected)
// with Status BLOCKED and ErrorCode E501 recording the failed harness attempt.
// Returns the first such step. Fails the test if none is found.
func requireHarnessErrorStep(t *testing.T, store *memStore, context string) domain.CompletedStep {
	t.Helper()
	for _, s := range store.Applied {
		if !s.IsInfrastructure && !s.HITLRejected &&
			s.Status == domain.StatusBLOCKED && s.ErrorCode == domain.ErrorTOOL_UNAVAILABLE {
			return s
		}
	}
	t.Errorf("%s: want a workflow CompletedStep in store.Applied with Status=BLOCKED and "+
		"ErrorCode=E501 recording the failed harness attempt, got none; "+
		"the failed attempt must be persisted before the next dispatch so the "+
		"execution log has a complete record of every dispatch attempt", context)
	return domain.CompletedStep{}
}

// ===== (a) Main dispatch loop: bypass succeeds =====

// TestSession_RawTextBypass_MainLoop_BypassSucceeds_RunCompletes verifies that
// when a raw-text harness sentinel error occurs at the auto-routed dispatch
// site, the session re-dispatches the same agent directly (without consulting
// the orchestrator) and, if the redispatch succeeds, the run continues normally
// to completion.
//
// Sentinel exercised: ErrProtocolNotExtractable.
//
// RED failure: without bypass the harness error triggers consultRoute, but the
// consultant queue is empty, causing the run to terminate with a non-Completed
// status instead of RunCompleted.
func TestSession_RawTextBypass_MainLoop_BypassSucceeds_RunCompletes(t *testing.T) {
	// Empty consultant: any call to ConsultRouting returns an error, ensuring
	// the test fails if bypass is absent and consultRoute is reached.
	consultant := &scriptedRoutingConsultant{}

	ses, f, store, orchPath := newAutoSessionWithConsultant(t, consultant)

	// agent-a: first dispatch returns a raw-text sentinel, second (bypass)
	// returns SUCCESS.
	f.Queue("agent-a",
		harness.ScriptedEntry{Err: wrapSentinel(commonharness.ErrProtocolNotExtractable)},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#2",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done on bypass",
		}},
	)
	// agent-b: dispatched after agent-a completes successfully.
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	// The run must complete normally when bypass succeeds.
	if got.Status != domain.RunCompleted {
		t.Errorf("want RunCompleted when bypass redispatch succeeds, got %q (message: %q); "+
			"without bypass the harness error would reach consultRoute, which has an empty "+
			"queue and terminates the run early",
			got.Status, got.Message)
	}
	// The consultant must never have been called: bypass resolved the error
	// without consultation.
	if consultant.CallCount != 0 {
		t.Errorf("want RoutingConsultant.ConsultRouting not called when bypass succeeds, "+
			"got %d call(s); bypass must handle the raw-text error without routing consultation",
			consultant.CallCount)
	}
	// Two invocations for agent-a: the original (raw-text) and the bypass.
	if n := countInvocationsFor(f.Invocations(), "agent-a"); n != 2 {
		t.Errorf("want 2 agent-a harness invocations (original + bypass), got %d; "+
			"bypass must invoke the same agent exactly once before proceeding",
			n)
	}
	// The failed first attempt is persisted as a BLOCKED/E501 workflow row even
	// though the bypass retry then succeeds.
	requireHarnessErrorStep(t, store, "main-loop site")
}

// ===== (b) Main dispatch loop: bypass fails, falls back to consultRoute =====

// TestSession_RawTextBypass_MainLoop_BypassFails_FallsBackToConsultRoute
// verifies that when the bypass redispatch at the auto-routed dispatch site
// also fails (with any error), the session falls back to the normal
// consultation path (consultRoute). The bypass adds one extra harness
// invocation (the failed bypass attempt itself) before consultRoute is called,
// giving agent-a three total invocations in the GREEN path.
//
// Sentinel exercised: ErrMalformedJSON.
//
// RED failure: without bypass the failed bypass attempt is absent, so agent-a
// is invoked twice (original error + consultant-directed retry) rather than
// three times. The assertion `agent-a invocations == 3` fails in RED (count=2).
func TestSession_RawTextBypass_MainLoop_BypassFails_FallsBackToConsultRoute(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// In RED, consultRoute is called for the original ErrMalformedJSON error (entry 1).
	// Call 1: dispatch agent-a at row 0. The harness consumes entry 2 (timeout),
	//         which triggers a recursive consultRoute.
	// Call 2 (recursive, RED only): stop -- run terminates; entry 3 never reached.
	//
	// In GREEN, bypass consumes entry 2 (timeout) instead of a consultant dispatch.
	// consultRoute is then called once for the bypass-failure deviation.
	// Call 1: dispatch agent-a at row 0 -- harness consumes entry 3 (SUCCESS).
	// Call 2: stop (run terminates cleanly).
	consultant.queueDispatch("agent-a", "retry after bypass failure", 0)
	consultant.queueStop("run stopped")

	ses, f, _, orchPath := newAutoSessionWithConsultant(t, consultant)

	// Three agent-a entries:
	//   Entry 1: ErrMalformedJSON (original dispatch).
	//   Entry 2: generic timeout -- consumed by bypass in GREEN, by consultant
	//            dispatch in RED.
	//   Entry 3: SUCCESS -- consumed by consultant dispatch in GREEN; never
	//            reached in RED (run stops at call 2 before this entry is used).
	f.Queue("agent-a",
		harness.ScriptedEntry{Err: wrapSentinel(commonharness.ErrMalformedJSON)},
		harness.ScriptedEntry{Err: fmt.Errorf("harness: subprocess timed out")},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#3",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done after fallback",
		}},
	)

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	// Three agent-a invocations expected: original error, failed bypass
	// attempt, and the consultant-directed retry that succeeds.
	// Without bypass: only two invocations (original error consumed by bypass
	// is absent; entry 2 is consumed by the consultant dispatch instead, but
	// entry 3 is never reached because consultant call 2 is stop).
	if n := countInvocationsFor(f.Invocations(), "agent-a"); n != 3 {
		t.Errorf("want 3 agent-a harness invocations "+
			"(original ErrMalformedJSON + failed bypass timeout + consultant retry SUCCESS), "+
			"got %d; without bypass only 2 invocations occur (original + consultant dispatch "+
			"consumes entry 2, but run terminates at call 2 stop before entry 3 is used)",
			n)
	}
}

// ===== (c) Consultant-routed dispatch: bypass succeeds =====

// TestSession_RawTextBypass_ConsultRoute_BypassSucceeds_FewerInvocations
// verifies that when a raw-text harness sentinel error occurs at the
// consultant-routed dispatch site (inside consultRoute, ~line 1776), the
// session re-dispatches the same agent directly rather than triggering a
// recursive consultRoute call. If the bypass succeeds, the run continues as
// if the original dispatch had succeeded.
//
// Sentinel exercised: ErrEmptyResponse.
//
// Observable difference: with bypass the agent is invoked twice (original
// error + bypass success) and the consultant is called twice (initial dispatch
// + stop). Without bypass, the recursive consultRoute consumes the stop
// instruction early, so only ONE harness invocation occurs for agent-a before
// the run terminates.
//
// RED failure: without bypass the recursive consultRoute call consumes the
// stop instruction on its second consultant call, leaving only 1 agent-a
// invocation. The test asserts 2.
func TestSession_RawTextBypass_ConsultRoute_BypassSucceeds_FewerInvocations(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Call 1: dispatch agent-a (harness error -> bypass -> SUCCESS).
	// Call 2: stop (run terminates after bypass completes the step).
	consultant.queueDispatch("agent-a", "do the work", 0)
	consultant.queueStop("run complete")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	// agent-a entry 1: raw-text error at the consultant-routed dispatch site.
	// agent-a entry 2: SUCCESS for the bypass redispatch.
	//
	// GREEN: both entries consumed (1 original + 1 bypass).
	// RED:   only entry 1 consumed; stop instruction reached on recursive
	//        consultant call 2 before entry 2 is ever used.
	f.Queue("agent-a",
		harness.ScriptedEntry{Err: wrapSentinel(commonharness.ErrEmptyResponse)},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#2",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done on bypass",
		}},
	)

	ses.Start(context.Background(), baseOrchestratedConfig(orchPath)) //nolint:errcheck

	// Two agent-a invocations expected: original error + bypass success.
	// Without bypass: recursive consultRoute consumes the stop instruction on
	// call 2 before dispatching agent-a again, so only 1 invocation.
	if n := countInvocationsFor(f.Invocations(), "agent-a"); n != 2 {
		t.Errorf("want 2 agent-a harness invocations (original raw-text error + "+
			"bypass redispatch SUCCESS) at the consultant-routed dispatch site, got %d; "+
			"without bypass the recursive consultRoute call reaches the stop instruction "+
			"immediately, producing only 1 invocation",
			n)
	}
}

// ===== (d) HITL-redispatch fallback: bypass succeeds =====

// TestSession_RawTextBypass_HITLRedispatch_BypassSucceeds_ThreeInvocations
// verifies that when the HITL-redispatch invocation (hitlCheckLoop, ~line 1088)
// fails with a raw-text sentinel error, the session performs one additional
// direct re-dispatch (bypass) before calling consultRoute. If the bypass
// succeeds, the hitlCheckLoop receives a valid response and the escalation
// path is taken (RedispatchUsed=true with approval still absent) rather than
// an immediate consultRoute call for a harness error.
//
// Sentinel exercised: ErrProtocolNotExtractable.
//
// Observable difference: with bypass, agent-a is invoked three times (initial
// dispatch, HITL-redispatch raw-text, bypass SUCCESS). Without bypass, only
// two invocations occur (initial dispatch, HITL-redispatch raw-text) before
// consultRoute is called.
//
// RED failure: without bypass agent-a has 2 invocations. The test asserts 3.
func TestSession_RawTextBypass_HITLRedispatch_BypassSucceeds_ThreeInvocations(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// After the HITL bypass succeeds, the HITL loop re-checks with
	// RedispatchUsed=true. With ApprovalFalse, DecideHITLCompliance returns
	// HITLEscalate, which calls consultRoute with an escalation deviation.
	// The consultant terminates the run here.
	//
	// Without bypass: the HITL-redispatch raw-text error calls consultRoute
	// directly with a BLOCKED/E501 deviation, also reaching this stop.
	consultant.queueStop("run stopped after HITL bypass attempt")

	// Use newHITLLinearSession (hitl-linear-orch.md, HITL=TRUE for agent-a) in
	// auto-execution mode so the engine auto-dispatches agent-a without requiring
	// a consultant for the initial dispatch.
	ses, f, _, orchPath := newHITLLinearSession(t, consultant, &fixedApprovalReader{domain.ApprovalFalse})

	// agent-a entry 1: initial auto-dispatch SUCCESS (triggers HITL check).
	// agent-a entry 2: raw-text error at the HITL-redispatch site (~line 1088).
	// agent-a entry 3: SUCCESS for the bypass redispatch (GREEN only).
	f.Queue("agent-a",
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#1",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "initial dispatch done",
		}},
		harness.ScriptedEntry{Err: wrapSentinel(commonharness.ErrProtocolNotExtractable)},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#3",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "bypass done",
		}},
	)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}
	ses.Start(context.Background(), cfg) //nolint:errcheck

	// Three agent-a invocations: initial dispatch, HITL-redispatch raw-text
	// error, and bypass redispatch SUCCESS.
	// Without bypass: only 2 invocations before consultRoute is called.
	if n := countInvocationsFor(f.Invocations(), "agent-a"); n != 3 {
		t.Errorf("want 3 agent-a harness invocations (initial + HITL-redispatch raw-text + bypass), "+
			"got %d; without bypass the HITL-redispatch raw-text error calls consultRoute "+
			"immediately, producing only 2 invocations",
			n)
	}
}
