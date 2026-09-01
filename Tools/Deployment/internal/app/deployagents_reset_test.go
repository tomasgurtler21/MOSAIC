package app_test

// deployagents_reset_test.go verifies that the DeployAgents flow resets the todo collector
// at the start of each deployment attempt, ensuring gaps from a declined plan do not appear
// in the final report of a subsequently confirmed plan.
//
// The scenario under test is the ErrPlanNotConfirmed restart path: the user presents the
// plan for review, declines it, and the TUI restarts the flow. The second attempt must
// begin with a clean collector so that gaps recorded during the first attempt cannot leak
// into the report written at the end of the second attempt.
//
// Two tests are provided:
//   - TestDeployAgents_DeclinedPlanGapsNotInFinalReport: end-to-end verification that the
//     collector holds no stale gaps after a declined-then-confirmed sequence.
//   - TestDeployAgents_ResetCalledOncePerAttempt: unit-style verification that Reset() is
//     called exactly once per DeployAgents invocation, confirming the call site is correct.
//   - TestDeployAgents_DeclinedThenConfirmedResetCalledTwice: two-call sequence confirmation
//     that Reset() is called once for the declined attempt and once for the confirmed one.

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// resetTrackingCollector — spy collector that records Reset() calls
// ---------------------------------------------------------------------------

// resetTrackingCollector satisfies todo.Collector and tracks how many times Reset() is
// called in addition to the items and gaps currently accumulated. After each Reset(), the
// current items and gaps are discarded, mirroring the expected behavior of the concrete
// collector implementation. The total reset count is never decremented.
type resetTrackingCollector struct {
	items      []domain.TodoItem
	gaps       []domain.Gap
	resetCalls int
}

func (r *resetTrackingCollector) Add(item domain.TodoItem) {
	r.items = append(r.items, item)
}

func (r *resetTrackingCollector) AddGap(g domain.Gap) {
	r.gaps = append(r.gaps, g)
}

func (r *resetTrackingCollector) Items() []domain.TodoItem { return r.items }
func (r *resetTrackingCollector) Groups() []todo.Group     { return nil }
func (r *resetTrackingCollector) Empty() bool              { return len(r.items) == 0 && len(r.gaps) == 0 }

// Reset increments resetCalls and discards all accumulated items and gaps. It mirrors what
// the concrete collector's Reset() must do.
func (r *resetTrackingCollector) Reset() {
	r.resetCalls++
	r.items = nil
	r.gaps = nil
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// countTrackedGapsOfKind returns the number of gaps currently held in the spy with the
// given kind. Used to verify that stale gaps from a declined plan are absent after reset.
func countTrackedGapsOfKind(spy *resetTrackingCollector, kind domain.GapKind) int {
	n := 0
	for _, g := range spy.gaps {
		if g.Kind == kind {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// T4.2a — end-to-end: gaps from declined plan absent after confirmed plan
// ---------------------------------------------------------------------------

// TestDeployAgents_DeclinedPlanGapsNotInFinalReport verifies that gaps accumulated during
// a declined plan attempt are absent from the todo collector after a subsequent confirmed
// attempt.
//
// Sequence:
//  1. First DeployAgents call: stubPlanner returns a plan with one GapNoModel gap; the
//     user declines the review. Returns ErrPlanNotConfirmed. The gap has been added to
//     the collector (the flow adds gaps before presenting the review).
//  2. Second DeployAgents call (same collector): stubPlanner returns a plan with no gaps;
//     the user confirms the review. Succeeds.
//
// The assertion: the collector must hold no GapNoModel gaps after the second call.
// Without Collector.Reset() at the start of each attempt, the gap from the first call
// survives into the final report — reproducing the bug. With Reset(), the gap is
// discarded before the second plan is built.
func TestDeployAgents_DeclinedPlanGapsNotInFinalReport(t *testing.T) {
	spy := &resetTrackingCollector{}

	// ------------------------------------------------------------------
	// First call: plan has a GapNoModel gap; user declines the review.
	// ------------------------------------------------------------------

	declineStub := interactiontest.NewBuilder().
		AnswerReview(false).
		Build()
	deps, workspace := newBaseDeps(t, declineStub)
	deps.Todo = spy

	// Plan carries one GapNoModel gap, as produced by plan.Build when the agent
	// has no model selected and no deployed file to inherit from.
	planWithGap := newMinimalPlan(workspace)
	planWithGap.Gaps = []domain.Gap{
		{Kind: domain.GapNoModel, Subject: "test-runner",
			Detail: "no model has been selected for agent test-runner"},
	}
	deps.Planner = &stubPlanner{plan: planWithGap}

	svc := app.New(deps)

	_, firstErr := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		SubagentIDs:     []string{"test-runner"},
		TierModels:      map[domain.Tier]string{"HIGH": "model-a"},
		AutoConfirmPlan: false, // honour the declined review answer
	})

	if !errors.Is(firstErr, app.ErrPlanNotConfirmed) {
		t.Fatalf("first DeployAgents: want ErrPlanNotConfirmed; got %v — "+
			"test precondition failed: the first call must be declined", firstErr)
	}

	// ------------------------------------------------------------------
	// Second call: plan has no gaps; user confirms the review.
	// Collector.Reset() must be called before plan.Build so the GapNoModel
	// from the first attempt is discarded.
	// ------------------------------------------------------------------

	confirmStub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps.Interaction = confirmStub

	// Plan has no gaps: the user resolved the model in this second attempt.
	planNoGaps := newMinimalPlan(workspace)
	planNoGaps.Gaps = nil
	deps.Planner = &stubPlanner{plan: planNoGaps}

	svc = app.New(deps)

	_, secondErr := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		SubagentIDs:     []string{"test-runner"},
		TierModels:      map[domain.Tier]string{"HIGH": "model-a"},
		AutoConfirmPlan: true,
	})
	if secondErr != nil {
		t.Fatalf("second DeployAgents: %v", secondErr)
	}

	// Assert: no GapNoModel gaps must remain in the collector after the confirmed attempt.
	// If Reset() was not called before the second plan.Build, the spy still holds the gap
	// from the first attempt and this count is non-zero.
	if n := countTrackedGapsOfKind(spy, domain.GapNoModel); n != 0 {
		t.Errorf("collector holds %d GapNoModel gap(s) after declined-then-confirmed sequence; want 0 — "+
			"Collector.Reset() must be called at the start of the second attempt to discard the "+
			"gap recorded during the declined first attempt", n)
	}
}

// ---------------------------------------------------------------------------
// T4.2b — unit: Reset() called once per single successful DeployAgents call
// ---------------------------------------------------------------------------

// TestDeployAgents_ResetCalledOncePerAttempt verifies that Collector.Reset() is called
// exactly once during a single successful DeployAgents invocation.
//
// Reset() is specified to run at the start of every attempt, before Planner.Build, so that
// gaps from any prior abandoned attempt are always cleared. This test confirms the call
// happens unconditionally — not only on the restart path.
//
// If resetCalls == 0 after the call, Reset() was never invoked (the fix is missing).
// If resetCalls > 1, Reset() was called more than once within a single attempt (unexpected).
func TestDeployAgents_ResetCalledOncePerAttempt(t *testing.T) {
	spy := &resetTrackingCollector{}

	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Todo = spy
	deps.Planner = &stubPlanner{plan: newMinimalPlan(workspace)}

	svc := app.New(deps)

	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		SubagentIDs:     []string{"test-runner"},
		TierModels:      map[domain.Tier]string{"HIGH": "model-a"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployAgents: %v", err)
	}

	// NOTE: This assertion checks the call-site contract (Reset() fires exactly once per
	// attempt). It is a secondary guard-rail on HOW the fix is implemented rather than
	// WHAT observable outcome the caller sees. The primary behavioral signal is provided
	// by TestDeployAgents_DeclinedPlanGapsNotInFinalReport. If the implementation later
	// calls Reset() at a different frequency while preserving the behavioral invariant,
	// this assertion may need updating.
	if spy.resetCalls != 1 {
		t.Errorf("Collector.Reset() called %d time(s) during a single DeployAgents call; want 1 — "+
			"Reset must be called exactly once per attempt, at the start of the plan-build section",
			spy.resetCalls)
	}
}

// ---------------------------------------------------------------------------
// T4.2c — integration: Reset() called once per attempt across two calls
// ---------------------------------------------------------------------------

// TestDeployAgents_DeclinedThenConfirmedResetCalledTwice verifies that Collector.Reset() is
// called once per DeployAgents invocation across a declined-then-confirmed sequence, totaling
// two resets.
//
// The reset is unconditional: it fires at the start of every attempt, not only when a prior
// ErrPlanNotConfirmed was returned. A count of two after two calls confirms this invariant.
func TestDeployAgents_DeclinedThenConfirmedResetCalledTwice(t *testing.T) {
	spy := &resetTrackingCollector{}

	// First call — decline the plan review.
	declineStub := interactiontest.NewBuilder().
		AnswerReview(false).
		Build()
	deps, workspace := newBaseDeps(t, declineStub)
	deps.Todo = spy
	deps.Planner = &stubPlanner{plan: newMinimalPlan(workspace)}
	svc := app.New(deps)

	_, firstErr := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		SubagentIDs:     []string{"test-runner"},
		TierModels:      map[domain.Tier]string{"HIGH": "model-a"},
		AutoConfirmPlan: false,
	})
	if !errors.Is(firstErr, app.ErrPlanNotConfirmed) {
		t.Fatalf("first DeployAgents: want ErrPlanNotConfirmed; got %v — "+
			"test precondition failed: the first call must be declined", firstErr)
	}

	// Second call — confirm the plan review.
	confirmStub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps.Interaction = confirmStub
	deps.Planner = &stubPlanner{plan: newMinimalPlan(workspace)}
	svc = app.New(deps)

	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		SubagentIDs:     []string{"test-runner"},
		TierModels:      map[domain.Tier]string{"HIGH": "model-a"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("second DeployAgents: %v", err)
	}

	// Two DeployAgents calls must produce exactly two Reset() calls.
	// NOTE: This assertion checks the call-site contract (Reset() fires once per attempt,
	// unconditionally). Like TestDeployAgents_ResetCalledOncePerAttempt, it is a secondary
	// guard-rail on the implementation detail of WHERE Reset() is called. The behavioral
	// outcome (no stale gaps) is verified by TestDeployAgents_DeclinedPlanGapsNotInFinalReport.
	// Update this count if the implementation legitimately moves or consolidates the call site.
	if spy.resetCalls != 2 {
		t.Errorf("Collector.Reset() called %d time(s) across a declined+confirmed sequence; want 2 — "+
			"Reset must be called once per attempt: once for the declined attempt and once for the "+
			"confirmed attempt; a count of 0 means Reset is never called, 1 means it is only called "+
			"conditionally (e.g. only on restart), and >2 means multiple resets per attempt",
			spy.resetCalls)
	}
}
