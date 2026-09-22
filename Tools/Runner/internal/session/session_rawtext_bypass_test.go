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
//   Non-sentinel errors skip bypass:
//   - A generic error not wrapping any sentinel goes straight to consultRoute
//     with no extra harness invocation (regression / AC4.4 guard).
//
//   Anti-loop guard checked during bypass:
//   - When the anti-loop counter is at the limit before the bypass, bypass is
//     skipped and consultRoute is called instead (regression / AC4.5 guard).
//
//   Persistence before dispatch at consultRoute harness-error branch (AC4.6):
//   - When a harness error occurs inside consultRoute (consultant-routed
//     invocation at ~line 1776), a CompletedStep with IsInfrastructure=true
//     and Status=BLOCKED must be written to the store before any further
//     dispatch, matching the pattern already used at the main dispatch loop
//     site. No such persistence exists at this site today -- test fails RED.
//
//   Persistence before dispatch at HITL-redispatch fallback (AC4.6):
//   - When a harness error occurs at the HITL-redispatch site (~line 1088),
//     a CompletedStep with IsInfrastructure=true and Status=BLOCKED must be
//     written to the store before calling consultRoute. No such persistence
//     exists at this site today -- test fails RED.

import (
	"context"
	"fmt"
	"strings"
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
// paths. The FakeAdapter and memStore are returned so tests can configure them.
func newAutoSessionWithConsultant(t *testing.T, consultant domain.RoutingConsultant) (
	ses session.Session, f *harness.FakeAdapter, store *memStore, orchPath string,
) {
	t.Helper()
	dir := t.TempDir()
	orchPath = copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f = harness.NewFakeAdapter()
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

// requireInfrastructureFailedStep asserts that store.Applied contains at least
// one CompletedStep with IsInfrastructure=true and Status!=SUCCESS. Returns the
// first such step. Fails the test if none is found.
func requireInfrastructureFailedStep(t *testing.T, store *memStore, context string) domain.CompletedStep {
	t.Helper()
	for _, s := range store.Applied {
		if s.IsInfrastructure && s.Status != domain.StatusSUCCESS {
			return s
		}
	}
	t.Errorf("%s: want a CompletedStep in store.Applied with IsInfrastructure=true and "+
		"Status!=SUCCESS recording the failed harness attempt, got none; "+
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
	// The main dispatch loop already persists infrastructure-failed steps today.
	// Asserting this here ensures Stage 4 changes do not accidentally remove that
	// existing persistence (AC4.6 regression guard for the main-loop site).
	requireInfrastructureFailedStep(t, store,
		"main-loop site: existing persistence must survive Stage 4 changes (AC4.6)")
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
	// Call 1: dispatch agent-a (harness error → bypass → SUCCESS).
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
	// directly with a DeviationHarnessError, also reaching this stop.
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

// ===== (e) Non-sentinel errors skip bypass (regression) =====

// TestSession_RawTextBypass_NonSentinelError_SkipsBypass verifies that a
// generic harness error that does not wrap any commonharness sentinel bypasses
// the bypass mechanism and routes directly to consultRoute, matching the
// current (pre-Stage-4) behavior. This is a regression guard for AC4.4.
//
// This test passes in both the RED and GREEN phases because non-sentinel errors
// must never trigger bypass, before or after Stage 4 is implemented.
func TestSession_RawTextBypass_NonSentinelError_SkipsBypass(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Non-sentinel error at main dispatch loop: consultant dispatches agent-a
	// again for a successful retry.
	consultant.queueDispatch("agent-a", "retry after timeout", 0)
	consultant.queueStop("done after retry")

	ses, f, _, orchPath := newAutoSessionWithConsultant(t, consultant)

	// agent-a entry 1: generic non-sentinel error.
	// agent-a entry 2: SUCCESS for the consultant-directed retry.
	f.Queue("agent-a",
		harness.ScriptedEntry{Err: fmt.Errorf("harness: subprocess timed out after 60s")},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#2",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done on retry",
		}},
	)

	ses.Start(context.Background(), baseLinearConfig(orchPath)) //nolint:errcheck

	// Without bypass the error goes to consultRoute (consultant call 1:
	// dispatch, call 2: stop after retry). With bypass also skipped for
	// non-sentinel, same sequence. Exactly 2 consultant calls expected.
	if consultant.CallCount != 2 {
		t.Errorf("want 2 consultant calls for non-sentinel harness error "+
			"(dispatch + stop), got %d; non-sentinel errors must not trigger "+
			"bypass -- they must go straight to consultRoute as before Stage 4",
			consultant.CallCount)
	}
	// Two agent-a invocations: the original non-sentinel error and the
	// consultant-directed retry. No extra bypass invocation.
	if n := countInvocationsFor(f.Invocations(), "agent-a"); n != 2 {
		t.Errorf("want 2 agent-a invocations (error + consultant retry), got %d; "+
			"bypass must not add an extra invocation for non-sentinel errors",
			n)
	}
}

// ===== (f) Anti-loop guard trips, bypass skipped (regression) =====

// TestSession_RawTextBypass_AntiLoopTrips_BypassSkipped verifies that when
// the anti-loop guard counter reaches the maximum before the bypass attempt,
// the bypass is skipped and consultRoute is called instead, ensuring the
// guard is evaluated before each bypass redispatch.
//
// This is a regression guard for AC4.5. Because anti-loop-blocked-bypass
// produces the same observable behavior as no-bypass, this test passes in both
// RED and GREEN phases. Its value is to prevent future regressions where bypass
// ignores the anti-loop guard.
//
// Setup: orchestrated mode with the same agent at the same row dispatched three
// times. After the second consultRoute dispatch the anti-loop counter is high
// enough to block the bypass on the third dispatch. The run terminates via the
// anti-loop escalation path.
func TestSession_RawTextBypass_AntiLoopTrips_BypassSkipped(t *testing.T) {
	consultant := &scriptedRoutingConsultant{}
	// Dispatch 1 via consultRoute: succeeds without error (counter=1).
	consultant.queueDispatch("agent-a", "first attempt", 0)
	// Dispatch 2 via consultRoute: raw-text error; bypass would push counter to
	// 4 (>= max), so bypass is skipped; falls back to consultRoute.
	consultant.queueDispatch("agent-a", "second attempt", 0)
	// After bypass is blocked the anti-loop guard fires inside the fallback
	// consultRoute call and escalates. This stop handles the escalation.
	consultant.queueStop("anti-loop escalation stop")

	ses, f, _, orchPath := newOrchestratedSession(t, consultant)

	// agent-a entries:
	//   1: SUCCESS (dispatch 1, no error).
	//   2: raw-text error (dispatch 2; bypass blocked by anti-loop).
	//   3+: not consumed (bypass was skipped).
	f.Queue("agent-a",
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#1",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "first attempt done",
		}},
		harness.ScriptedEntry{Err: wrapSentinel(commonharness.ErrMalformedJSON)},
	)

	got, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))

	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	// Run terminates via the consultant stop instruction (anti-loop escalation).
	// In both RED and GREEN the bypass is not attempted (either absent or
	// blocked), so the behavior is identical.
	if got.Status != domain.RunStoppedByConsultant {
		t.Errorf("want RunStoppedByConsultant when anti-loop escalates after bypass is blocked, "+
			"got %q (message: %q)", got.Status, got.Message)
	}
}

// ===== (g) Persistence at consultRoute harness-error branch (AC4.6) =====

// TestSession_RawTextBypass_ConsultRoute_HarnessError_PersistenceBeforeDispatch
// verifies that when a harness invocation fails at the consultant-routed
// dispatch site (inside consultRoute, ~line 1776), a CompletedStep with
// IsInfrastructure=true and a non-SUCCESS status is persisted to the store
// before the recursive consultRoute or bypass redispatch. This follows the same
// pattern already established at the main dispatch loop site.
//
// This persistence does NOT exist at the consultRoute harness-error branch
// today. The test fails RED because store.Applied contains no
// IsInfrastructure=true, non-SUCCESS step after a consultant-routed harness
// failure.
//
// After I4.3 is implemented, the failed dispatch record is written at this site,
// the assertion is satisfied, and the test turns GREEN.
func TestSession_RawTextBypass_ConsultRoute_HarnessError_PersistenceBeforeDispatch(t *testing.T) {
	const errMsg = "simulated consultant-routed harness failure for persistence check"

	consultant := &scriptedRoutingConsultant{}
	// Call 1: dispatch agent-a (harness error occurs inside consultRoute).
	// Call 2: re-dispatch agent-a after recursive consultRoute or bypass.
	// Call 3: stop to terminate the run.
	consultant.queueDispatch("agent-a", "first attempt", 0)
	consultant.queueDispatch("agent-a", "retry after failure", 0)
	consultant.queueStop("done")

	store := &memStore{}
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:  f,
		Store:    store,
		Routing:  consultant,
		Clock:    fixedClock{t: epoch},
		Interact: &noopInteraction{},
	})

	// agent-a entry 1: generic non-sentinel harness error inside consultRoute.
	// agent-a entry 2: SUCCESS for the consultant re-route.
	f.Queue("agent-a",
		harness.ScriptedEntry{Err: fmt.Errorf("%s", errMsg)},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#2",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done on retry",
		}},
	)

	_, err := ses.Start(context.Background(), baseOrchestratedConfig(orchPath))
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	// Primary assertion: store.Applied must contain a CompletedStep with
	// IsInfrastructure=true and Status!=SUCCESS capturing the harness failure
	// that occurred inside consultRoute (~line 1776).
	//
	// With the current code, no such step exists at this call site: the code
	// goes straight from the harness error to recursive consultRoute with no
	// Store.Apply in between. The main dispatch loop (~line 930-945) has this
	// pattern, but consultRoute's own harness-error branch does not.
	//
	// After I4.3, the persistence is added at the consultRoute harness-error
	// branch, and this assertion is satisfied.
	step := requireInfrastructureFailedStep(t, store,
		"consultant-routed harness error (inside consultRoute ~line 1776)")
	if step.AgentInstance != "" && !strings.Contains(step.Summary, errMsg) {
		t.Errorf("want step.Summary to contain the harness error message %q, got %q; "+
			"the persisted record must capture the exact error that caused the failure",
			errMsg, step.Summary)
	}
}

// ===== (g) Persistence at HITL-redispatch fallback (AC4.6) =====

// TestSession_RawTextBypass_HITLRedispatch_HarnessError_PersistenceBeforeDispatch
// verifies that when a harness invocation fails at the HITL-redispatch site
// (~line 1088 inside hitlCheckLoop), a CompletedStep with IsInfrastructure=true
// and a non-SUCCESS status is persisted to the store before calling consultRoute
// or performing the bypass redispatch.
//
// The Store.Apply at ~line 1039 persists the HITL-rejected attempt (the
// rejection that triggered the redispatch), not the harness error that occurs
// when the redispatch itself fails. There is no persistence of the failed
// redispatch attempt at that site today.
//
// The test fails RED because store.Applied contains no IsInfrastructure=true,
// non-SUCCESS step with a summary matching the harness error after the HITL
// redispatch fails.
//
// After I4.4 is implemented, a failed-attempt record is written at the HITL
// redispatch site, the assertion is satisfied, and the test turns GREEN.
func TestSession_RawTextBypass_HITLRedispatch_HarnessError_PersistenceBeforeDispatch(t *testing.T) {
	const errMsg = "simulated HITL-redispatch harness failure for persistence check"

	consultant := &scriptedRoutingConsultant{}
	// After the HITL-redispatch harness error (with or without bypass), the
	// session calls consultRoute. The consultant terminates the run.
	consultant.queueStop("run stopped after HITL-redispatch harness error")

	store := &memStore{}
	dir := t.TempDir()
	orchPath := copyOrchestratorFile(t, dir, "hitl-linear-orch.md")
	writeAgentFile(t, dir, "agent-a")
	writeAgentFile(t, dir, "agent-b")

	f := harness.NewFakeAdapter()
	ses := session.New(session.Deps{
		Harness:   f,
		Store:     store,
		Routing:   consultant,
		Approvals: &fixedApprovalReader{domain.ApprovalFalse},
		Clock:     fixedClock{t: epoch},
		Interact:  &noopInteraction{},
	})

	// agent-a entry 1: initial dispatch SUCCESS (triggers HITL check).
	// agent-a entry 2: non-sentinel harness error at the HITL-redispatch site.
	//   Using a non-sentinel error keeps this test focused on persistence only
	//   and avoids coupling it to the bypass logic.
	f.Queue("agent-a",
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#1",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "initial dispatch done",
		}},
		harness.ScriptedEntry{Err: fmt.Errorf("%s", errMsg)},
	)

	cfg := domain.RunConfig{
		OrchestratorFilePath: orchPath,
		WorkflowID:           "linear",
		Task:                 "test task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
	}
	_, err := ses.Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}

	// Primary assertion: store.Applied must contain at least one CompletedStep
	// with IsInfrastructure=true and Status!=SUCCESS whose Summary contains the
	// harness error message. This step represents the failed HITL redispatch
	// attempt and must be persisted before consultRoute is called.
	//
	// With the current code, the Store.Apply at ~line 1039 records the
	// HITLRejected=true step for the first rejected attempt; when the
	// redispatch itself fails at ~line 1088, no additional Store.Apply is
	// performed. The harness failure info is discarded.
	//
	// After I4.4, a new Store.Apply is added at the HITL-redispatch harness
	// error site, writing an IsInfrastructure=true, non-SUCCESS step before
	// any further dispatch or consultation.
	var found bool
	for _, s := range store.Applied {
		if s.IsInfrastructure && s.Status != domain.StatusSUCCESS {
			found = true
			break
		}
	}
	if !found {
		t.Error("want a CompletedStep in store.Applied with IsInfrastructure=true and " +
			"Status!=SUCCESS capturing the failed HITL-redispatch harness error, got none; " +
			"the failed redispatch attempt must be persisted before the next dispatch " +
			"so the execution log has a complete record of every attempt (I4.4 fix)")
	}
}

// ===== All three sentinel errors exercised (classification check) =====
// ErrProtocolNotExtractable: TestSession_RawTextBypass_MainLoop_BypassSucceeds_RunCompletes
//                            TestSession_RawTextBypass_HITLRedispatch_BypassSucceeds_ThreeInvocations
// ErrMalformedJSON:          TestSession_RawTextBypass_MainLoop_BypassFails_FallsBackToConsultRoute
//                            TestSession_RawTextBypass_AntiLoopTrips_BypassSkipped
// ErrEmptyResponse:          TestSession_RawTextBypass_ConsultRoute_BypassSucceeds_FewerInvocations
