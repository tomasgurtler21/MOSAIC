package session_test

// Tests for non-sentinel bypass guard, anti-loop bypass guard, and persistence
// contract at harness-error sites (split from session_rawtext_bypass_test.go).
//
// Coverage:
//
//   Non-sentinel errors skip bypass:
//   - A generic error not wrapping any sentinel is one E501 attempt that the
//     engine re-dispatches, with no bypass invocation.
//
//   Anti-loop guard checked during bypass:
//   - When the anti-loop counter is at the limit before the bypass, bypass is
//     skipped and consultRoute is called instead (regression / AC4.5 guard).
//
//   Persistence before dispatch at consultRoute harness-error branch (AC4.6):
//   - When a harness error occurs inside consultRoute, a workflow
//     CompletedStep with Status=BLOCKED and ErrorCode=E501 must be written to
//     the store before any further dispatch.
//
//   Persistence before dispatch at HITL-redispatch fallback (AC4.6):
//   - When a harness error occurs at the HITL-redispatch site, a workflow
//     CompletedStep with Status=BLOCKED and ErrorCode=E501 must be written to
//     the store before calling consultRoute.
//
// Sentinel classification cross-reference:
//   ErrProtocolNotExtractable: TestSession_RawTextBypass_MainLoop_BypassSucceeds_RunCompletes
//                              TestSession_RawTextBypass_HITLRedispatch_BypassSucceeds_ThreeInvocations
//   ErrMalformedJSON:          TestSession_RawTextBypass_MainLoop_BypassFails_EngineRetriesWhileBudgetRemains
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

// ===== (e) Non-sentinel errors skip bypass =====

// TestSession_RawTextBypass_NonSentinelError_SkipsBypass verifies that a
// generic harness error that does not wrap any commonharness sentinel does not
// trigger the raw-text bypass: the failure is one BLOCKED/E501 attempt, and the
// engine re-dispatches the same agent without consulting the orchestrator.
func TestSession_RawTextBypass_NonSentinelError_SkipsBypass(t *testing.T) {
	// Empty consultant: any consultation fails the run early.
	consultant := &scriptedRoutingConsultant{}

	ses, f, store, orchPath := newAutoSessionWithConsultant(t, consultant)

	f.Queue("agent-a",
		harness.ScriptedEntry{Err: fmt.Errorf("harness: subprocess timed out after 60s")},
		harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: "agent-a#2",
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   "done on retry",
		}},
	)
	f.Queue("agent-b", harness.ScriptedEntry{Response: &domain.ProtocolResponse{
		AgentInstanceID: "agent-b#3",
		StatusCode:      domain.StatusSUCCESS,
		StatusMessage:   "done",
	}})

	got, err := ses.Start(context.Background(), baseLinearConfig(orchPath))

	requireRunStatus(t, got, err, domain.RunCompleted)
	if consultant.CallCount != 0 {
		t.Errorf("want no consultation for a harness error with budget left, got %d", consultant.CallCount)
	}
	if n := countInvocationsFor(f.Invocations(), "agent-a"); n != 2 {
		t.Errorf("want 2 agent-a invocations (error + engine re-dispatch), got %d", n)
	}
	// Exactly one E501 attempt: the error. A bypass would have been a second one.
	if n := countE501RowsFor(store, "agent-a"); n != 1 {
		t.Errorf("want 1 BLOCKED/E501 agent-a row for a non-sentinel error, got %d", n)
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

// ===== (g) Persistence at consultRoute harness-error branch =====

// TestSession_RawTextBypass_ConsultRoute_HarnessError_PersistenceBeforeDispatch
// verifies that when a harness invocation fails at the consultant-routed
// dispatch site (inside consultRoute), a workflow CompletedStep with
// Status=BLOCKED and ErrorCode=E501 is persisted to the store before the
// recursive consultRoute or bypass redispatch.
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

	// store.Applied must contain a workflow CompletedStep with Status=BLOCKED
	// and ErrorCode=E501 capturing the harness failure that occurred inside
	// consultRoute, recorded before any further dispatch.
	step := requireHarnessErrorStep(t, store,
		"consultant-routed harness error (inside consultRoute)")
	if step.AgentInstance != "" && !strings.Contains(step.Summary, errMsg) {
		t.Errorf("want step.Summary to contain the harness error message %q, got %q; "+
			"the persisted record must capture the exact error that caused the failure",
			errMsg, step.Summary)
	}
}

// ===== (g) Persistence at HITL-redispatch fallback =====

// TestSession_RawTextBypass_HITLRedispatch_HarnessError_PersistenceBeforeDispatch
// verifies that when a harness invocation fails at the HITL-redispatch site,
// a workflow CompletedStep with Status=BLOCKED and ErrorCode=E501 is persisted
// to the store (no HITL gate applies to it) before calling consultRoute or
// performing the bypass redispatch. The HITL-rejected initial attempt is
// recorded separately.
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

	// store.Applied must contain the initial HITL-rejected attempt and then an
	// accepted workflow row (not HITL-gated) with Status=BLOCKED and
	// ErrorCode=E501 whose Summary contains the harness error message.
	step := requireHarnessErrorStep(t, store, "HITL-redispatch harness error")
	if !strings.Contains(step.Summary, errMsg) {
		t.Errorf("want step.Summary to contain the harness error message %q, got %q",
			errMsg, step.Summary)
	}
}
