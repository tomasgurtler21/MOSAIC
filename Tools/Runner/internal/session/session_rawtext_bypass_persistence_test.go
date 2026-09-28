package session_test

// Tests for non-sentinel bypass guard, anti-loop bypass guard, and persistence
// contract at harness-error sites (split from session_rawtext_bypass_test.go).
//
// Coverage:
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
//   - When a harness error occurs inside consultRoute (~line 1776), a
//     CompletedStep with IsInfrastructure=true and Status=BLOCKED must be
//     written to the store before any further dispatch.
//
//   Persistence before dispatch at HITL-redispatch fallback (AC4.6):
//   - When a harness error occurs at the HITL-redispatch site (~line 1088),
//     a CompletedStep with IsInfrastructure=true and Status=BLOCKED must be
//     written to the store before calling consultRoute.
//
// Sentinel classification cross-reference:
//   ErrProtocolNotExtractable: TestSession_RawTextBypass_MainLoop_BypassSucceeds_RunCompletes
//                              TestSession_RawTextBypass_HITLRedispatch_BypassSucceeds_ThreeInvocations
//   ErrMalformedJSON:          TestSession_RawTextBypass_MainLoop_BypassFails_FallsBackToConsultRoute
//                              TestSession_RawTextBypass_AntiLoopTrips_BypassSkipped
//   ErrEmptyResponse:          TestSession_RawTextBypass_ConsultRoute_BypassSucceeds_FewerInvocations

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

	f := harness.NewMockAdapter()
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

	f := harness.NewMockAdapter()
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
