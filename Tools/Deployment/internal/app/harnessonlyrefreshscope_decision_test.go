package app

// harnessonlyrefreshscope_decision_test.go verifies the RefreshDecision return contract for
// askHarnessOnlyRefreshScope, which changes in Stage 2.
//
// Stage 2 requirement: every non-Answered outcome (SkippedOne, SkippedAll, Cancelled, or a
// transport error) must yield a declined decision (Refresh=false), NOT the old narrow-scope
// default. Only an explicit answer may produce Refresh=true.
//
// This file is in package app (not package app_test) because askHarnessOnlyRefreshScope is
// a package-private method. It shares the interaction stub helpers from
// harnessonlyrefreshscope_test.go (scopePromptCapture, newScopePromptService, scopePromptAgent).
//
// All tests in this file reference the RefreshDecision type. That type does not exist in the
// current implementation: the current function returns (RefreshScope, bool). The tests
// therefore fail to compile until Stage 2's I2.1 materialises RefreshDecision and changes
// the function signature. This is the intended TDD RED state.
//
// Verified invariants:
//
// Explicit answer handling:
//   - "protocol-only" answer yields RefreshDecision{Refresh:true, Scope:RefreshProtocolOnly, ApplyToAll:false}
//   - "all-deployed" answer yields RefreshDecision{Refresh:true, Scope:RefreshAllDeployed, ApplyToAll:false}
//   - "all:protocol-only" answer yields RefreshDecision{Refresh:true, Scope:RefreshProtocolOnly, ApplyToAll:true}
//   - "all:all-deployed" answer yields RefreshDecision{Refresh:true, Scope:RefreshAllDeployed, ApplyToAll:true}
//
// Non-answered outcomes:
//   - SkippedOne   → Refresh=false, Scope=zero, ApplyToAll=false
//   - SkippedAll   → Refresh=false, Scope=zero, ApplyToAll=true
//   - Cancelled    → Refresh=false, Scope=zero, ApplyToAll=false
//   - Transport error → Refresh=false, Scope=zero, ApplyToAll=false
//
// The zero value of RefreshDecision is the safe default (declined, this file only).

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Explicit answer → Refresh=true with correct scope and applyToAll
// ---------------------------------------------------------------------------

// TestAskHarnessOnlyRefreshScope_Decision_PerAgentProtocolOnly_RefreshTrue verifies that
// an explicit "protocol-only" answer yields a RefreshDecision with Refresh=true,
// Scope=RefreshProtocolOnly, and ApplyToAll=false.
func TestAskHarnessOnlyRefreshScope_Decision_PerAgentProtocolOnly_RefreshTrue(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: string(RefreshProtocolOnly),
		},
	}
	svc := newScopePromptService(capture)

	// The current function returns (RefreshScope, bool); after Stage 2 it returns RefreshDecision.
	// Assigning the result to a single RefreshDecision variable will fail to compile until
	// the signature changes — this is the TDD RED compile failure.
	decision := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if !decision.Refresh {
		t.Error("decision.Refresh = false for explicit \"protocol-only\" answer, want true; " +
			"an explicit answer must authorise a refresh")
	}
	if decision.Scope != RefreshProtocolOnly {
		t.Errorf("decision.Scope = %q, want %q; the scope must match the option the user selected",
			decision.Scope, RefreshProtocolOnly)
	}
	if decision.ApplyToAll {
		t.Error("decision.ApplyToAll = true for a per-agent answer, want false; " +
			"a per-agent answer (no \"all:\" prefix) must not latch for remaining agents")
	}
}

// TestAskHarnessOnlyRefreshScope_Decision_PerAgentAllDeployed_RefreshTrue verifies that
// an explicit "all-deployed" answer yields Refresh=true, Scope=RefreshAllDeployed,
// ApplyToAll=false.
func TestAskHarnessOnlyRefreshScope_Decision_PerAgentAllDeployed_RefreshTrue(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: string(RefreshAllDeployed),
		},
	}
	svc := newScopePromptService(capture)

	decision := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if !decision.Refresh {
		t.Error("decision.Refresh = false for explicit \"all-deployed\" answer, want true")
	}
	if decision.Scope != RefreshAllDeployed {
		t.Errorf("decision.Scope = %q, want %q", decision.Scope, RefreshAllDeployed)
	}
	if decision.ApplyToAll {
		t.Error("decision.ApplyToAll = true for a per-agent answer, want false")
	}
}

// TestAskHarnessOnlyRefreshScope_Decision_ApplyToAllProtocolOnly_RefreshTrueApplyToAllTrue
// verifies that the "all:protocol-only" compound option yields Refresh=true,
// Scope=RefreshProtocolOnly, ApplyToAll=true.
func TestAskHarnessOnlyRefreshScope_Decision_ApplyToAllProtocolOnly_RefreshTrueApplyToAllTrue(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: "all:" + string(RefreshProtocolOnly),
		},
	}
	svc := newScopePromptService(capture)

	decision := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if !decision.Refresh {
		t.Error("decision.Refresh = false for apply-to-all protocol-only answer, want true; " +
			"an explicit apply-to-all answer must still authorise a refresh")
	}
	if decision.Scope != RefreshProtocolOnly {
		t.Errorf("decision.Scope = %q, want %q; the \"all:\" prefix must be stripped to recover "+
			"the underlying RefreshScope value",
			decision.Scope, RefreshProtocolOnly)
	}
	if !decision.ApplyToAll {
		t.Error("decision.ApplyToAll = false for \"all:protocol-only\" answer, want true; " +
			"the apply-to-all variant must latch the decision for remaining agents")
	}
}

// TestAskHarnessOnlyRefreshScope_Decision_ApplyToAllAllDeployed_RefreshTrueApplyToAllTrue
// verifies that the "all:all-deployed" compound option yields Refresh=true,
// Scope=RefreshAllDeployed, ApplyToAll=true.
func TestAskHarnessOnlyRefreshScope_Decision_ApplyToAllAllDeployed_RefreshTrueApplyToAllTrue(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{
			Status:   domain.Answered,
			OptionID: "all:" + string(RefreshAllDeployed),
		},
	}
	svc := newScopePromptService(capture)

	decision := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if !decision.Refresh {
		t.Error("decision.Refresh = false for apply-to-all all-deployed answer, want true")
	}
	if decision.Scope != RefreshAllDeployed {
		t.Errorf("decision.Scope = %q, want %q", decision.Scope, RefreshAllDeployed)
	}
	if !decision.ApplyToAll {
		t.Error("decision.ApplyToAll = false for \"all:all-deployed\" answer, want true")
	}
}

// ---------------------------------------------------------------------------
// Non-answered outcomes → Refresh=false (declined, NOT the narrow-scope default)
// ---------------------------------------------------------------------------

// TestAskHarnessOnlyRefreshScope_Decision_SkippedOne_DeclinedThisFileOnly verifies that
// a SkippedOne outcome yields Refresh=false and ApplyToAll=false. Skip is a true opt-out
// for this file only; it must not fall back to the narrow scope and must not latch.
func TestAskHarnessOnlyRefreshScope_Decision_SkippedOne_DeclinedThisFileOnly(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{Status: domain.SkippedOne},
	}
	svc := newScopePromptService(capture)

	decision := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if decision.Refresh {
		t.Error("decision.Refresh = true on SkippedOne, want false; " +
			"skip is a true opt-out — it must not be interpreted as consent to the narrow scope. " +
			"The caller must produce no plan item for this file.")
	}
	// Scope must be the zero value when Refresh=false; callers must not read it.
	if decision.Scope != "" {
		t.Errorf("decision.Scope = %q on SkippedOne, want zero value (\"\"); "+
			"the Scope field is meaningful only when Refresh=true; "+
			"a declined decision must carry the zero scope to prevent callers from accidentally using it",
			decision.Scope)
	}
	if decision.ApplyToAll {
		t.Error("decision.ApplyToAll = true on SkippedOne, want false; " +
			"SkippedOne declines only this file — it must not latch for remaining agents")
	}
}

// TestAskHarnessOnlyRefreshScope_Decision_SkippedAll_DeclinedApplyToAllTrue verifies that
// a SkippedAll outcome yields Refresh=false and ApplyToAll=true, latching the decline for
// every remaining harness-only agent in the run.
func TestAskHarnessOnlyRefreshScope_Decision_SkippedAll_DeclinedApplyToAllTrue(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{Status: domain.SkippedAll},
	}
	svc := newScopePromptService(capture)

	decision := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if decision.Refresh {
		t.Error("decision.Refresh = true on SkippedAll, want false; " +
			"SkippedAll is a true opt-out for this and all remaining files")
	}
	if decision.Scope != "" {
		t.Errorf("decision.Scope = %q on SkippedAll, want zero value; "+
			"the Scope field must not carry the old narrow-default value when the user declined",
			decision.Scope)
	}
	if !decision.ApplyToAll {
		t.Error("decision.ApplyToAll = false on SkippedAll, want true; " +
			"SkippedAll must latch the decline for all remaining harness-only agents so the prompt " +
			"is not re-asked, mirroring the existing apply-to-all latch shape")
	}
}

// TestAskHarnessOnlyRefreshScope_Decision_Cancelled_DeclinedThisFileOnly verifies that a
// Cancelled outcome yields Refresh=false and ApplyToAll=false. Cancellation is not consent;
// it must not be interpreted as the narrow scope and must not latch for remaining agents.
func TestAskHarnessOnlyRefreshScope_Decision_Cancelled_DeclinedThisFileOnly(t *testing.T) {
	capture := &scopePromptCapture{
		answer: domain.ChoiceAnswer{Status: domain.Cancelled},
	}
	svc := newScopePromptService(capture)

	decision := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if decision.Refresh {
		t.Error("decision.Refresh = true on Cancelled, want false; " +
			"a cancelled prompt is absence of answer, not consent to any refresh scope")
	}
	if decision.Scope != "" {
		t.Errorf("decision.Scope = %q on Cancelled, want zero value; "+
			"the Scope field must carry the zero value when Refresh=false",
			decision.Scope)
	}
	if decision.ApplyToAll {
		t.Error("decision.ApplyToAll = true on Cancelled, want false; " +
			"a single cancellation must not latch for remaining agents")
	}
}

// TestAskHarnessOnlyRefreshScope_Decision_TransportError_DeclinedThisFileOnly verifies that
// when SelectOne returns a non-nil transport error, the function returns Refresh=false and
// ApplyToAll=false. A transport failure is not consent; it must be treated as a decline for
// this file only, consistent with the "absence of answer is not consent" rule.
func TestAskHarnessOnlyRefreshScope_Decision_TransportError_DeclinedThisFileOnly(t *testing.T) {
	capture := &scopePromptCapture{
		err: errors.New("transport failure: connection reset by peer"),
	}
	svc := newScopePromptService(capture)

	decision := svc.askHarnessOnlyRefreshScope(context.Background(), scopePromptAgent("some/agent.md"))

	if decision.Refresh {
		t.Error("decision.Refresh = true on transport error, want false; " +
			"a SelectOne transport error must yield a declined decision — " +
			"the caller must not silently fall back to the narrow scope and produce a plan item")
	}
	if decision.Scope != "" {
		t.Errorf("decision.Scope = %q on transport error, want zero value",
			decision.Scope)
	}
	if decision.ApplyToAll {
		t.Error("decision.ApplyToAll = true on transport error, want false; " +
			"a transport error on a single file must not latch the decline for remaining agents")
	}
}

// ---------------------------------------------------------------------------
// Zero value is the safe default
// ---------------------------------------------------------------------------

// TestRefreshDecision_ZeroValue_IsDeclinedThisFileOnly verifies that the zero value of
// RefreshDecision represents "declined, this file only" — Refresh=false, Scope=zero,
// ApplyToAll=false. Any path that forgets to set a RefreshDecision must produce a safe,
// non-refreshing outcome.
func TestRefreshDecision_ZeroValue_IsDeclinedThisFileOnly(t *testing.T) {
	var zero RefreshDecision

	if zero.Refresh {
		t.Error("zero RefreshDecision.Refresh = true, want false; " +
			"the zero value must represent a declined decision so that any path that forgets " +
			"to set it is safe by default")
	}
	if zero.Scope != "" {
		t.Errorf("zero RefreshDecision.Scope = %q, want zero value", zero.Scope)
	}
	if zero.ApplyToAll {
		t.Error("zero RefreshDecision.ApplyToAll = true, want false")
	}
}
