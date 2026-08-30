package todo_test

// reset_test.go verifies the behavior of Collector.Reset().
//
// These tests cover the full contract for Reset() as required by the TODO Collector Reset
// feature: clearing accumulated state, correct behavior of Empty() and Items() after reset,
// preservation of items added after reset, and the two restart-then-succeed sequences
// described in the plan (declined-with-no-second-gaps and declined-with-second-attempt-gaps).

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// Basic Reset behavior
// ---------------------------------------------------------------------------

// TestCollector_Reset_ClearsAllItemsAddedDirectly verifies that Reset() discards all
// TodoItems previously added via Add(), returning the collector to an empty state.
func TestCollector_Reset_ClearsAllItemsAddedDirectly(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{Category: domain.TodoModels, Subject: "agent-a"})
	c.Add(domain.TodoItem{Category: domain.TodoModels, Subject: "agent-b"})

	c.Reset()

	if items := c.Items(); len(items) != 0 {
		t.Errorf("Items() len = %d after Reset(); want 0 — Reset must discard all accumulated items",
			len(items))
	}
}

// TestCollector_Reset_ClearsAllGapsAddedViaAddGap verifies that Reset() discards all
// TodoItems created by AddGap(), returning the collector to an empty state.
func TestCollector_Reset_ClearsAllGapsAddedViaAddGap(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-no-model"})
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "file_read"})

	c.Reset()

	if items := c.Items(); len(items) != 0 {
		t.Errorf("Items() len = %d after Reset(); want 0 — Reset must discard all accumulated gaps",
			len(items))
	}
}

// TestCollector_Reset_EmptyReturnsTrueAfterReset verifies that Empty() returns true
// immediately after Reset(), even when items had been added before the call.
func TestCollector_Reset_EmptyReturnsTrueAfterReset(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})

	c.Reset()

	if !c.Empty() {
		t.Error("Empty() = false after Reset(); want true — " +
			"Reset must return the collector to the same empty state as a newly created collector")
	}
}

// TestCollector_Reset_ItemsReturnsEmptySliceAfterReset verifies that Items() returns an
// empty (zero-length) result after Reset(), covering the observable contract that callers
// iterate over Items() to build the todo report.
func TestCollector_Reset_ItemsReturnsEmptySliceAfterReset(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-x"})
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "file.md"})

	c.Reset()

	items := c.Items()
	if len(items) != 0 {
		t.Errorf("Items() len = %d after Reset(); want 0", len(items))
	}
}

// TestCollector_Reset_GroupsReturnsEmptyAfterReset verifies that Groups() returns an empty
// result after Reset(). Groups is the partitioned view of Items used by the renderer;
// it must also reflect the cleared state.
func TestCollector_Reset_GroupsReturnsEmptyAfterReset(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})
	c.AddGap(domain.Gap{Kind: domain.GapHookRegistration, Subject: "hook-x"})

	c.Reset()

	if groups := c.Groups(); len(groups) != 0 {
		t.Errorf("Groups() len = %d after Reset(); want 0", len(groups))
	}
}

// ---------------------------------------------------------------------------
// Post-reset accumulation
// ---------------------------------------------------------------------------

// TestCollector_Reset_GapsAddedAfterResetArePreserved verifies that items added after
// Reset() are accumulated normally and appear in Items(). Only pre-reset items are
// discarded; post-reset items are kept intact.
func TestCollector_Reset_GapsAddedAfterResetArePreserved(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-before-reset"})

	c.Reset()

	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "post-reset-tool"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d after Reset() and one AddGap; want 1 — "+
			"post-Reset items must be preserved, pre-Reset items must be discarded",
			len(items))
	}
	if items[0].Subject != "post-reset-tool" {
		t.Errorf("Items()[0].Subject = %q; want %q — "+
			"the surviving item must be the one added after Reset, not the one added before",
			items[0].Subject, "post-reset-tool")
	}
}

// TestCollector_Reset_ItemsAddedAfterResetArePreserved verifies the same post-reset
// preservation for items added via Add() (rather than AddGap()).
func TestCollector_Reset_ItemsAddedAfterResetArePreserved(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{Category: domain.TodoModels, Subject: "stale-agent"})

	c.Reset()

	c.Add(domain.TodoItem{Category: domain.TodoManual, Subject: "fresh-step"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d after Reset() and one Add; want 1", len(items))
	}
	if items[0].Subject != "fresh-step" {
		t.Errorf("Items()[0].Subject = %q; want %q", items[0].Subject, "fresh-step")
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

// TestCollector_Reset_OnEmptyCollectorIsNoOp verifies that calling Reset() on a collector
// that has never had any items added is safe and leaves the collector in a valid empty state.
// The collector must not panic or enter a corrupted state.
func TestCollector_Reset_OnEmptyCollectorIsNoOp(t *testing.T) {
	c := todo.NewCollector()

	// Should not panic.
	c.Reset()

	if !c.Empty() {
		t.Error("Empty() = false after Reset() on a never-used collector; want true")
	}
	if items := c.Items(); len(items) != 0 {
		t.Errorf("Items() len = %d after Reset() on a never-used collector; want 0", len(items))
	}
}

// TestCollector_Reset_MultipleConsecutiveResetsAreIdempotent verifies that calling Reset()
// several times in succession is safe: the collector remains empty and does not panic or
// corrupt state after the second and subsequent calls.
func TestCollector_Reset_MultipleConsecutiveResetsAreIdempotent(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})

	c.Reset()
	c.Reset()
	c.Reset()

	if !c.Empty() {
		t.Error("Empty() = false after three consecutive Reset() calls; want true")
	}
	if items := c.Items(); len(items) != 0 {
		t.Errorf("Items() len = %d after three consecutive Reset() calls; want 0", len(items))
	}
}

// ---------------------------------------------------------------------------
// Restart-then-succeed sequences
// ---------------------------------------------------------------------------

// TestCollector_Reset_DeclinedThenSucceedProducesNoGaps verifies the primary bug fix scenario:
// when the first plan attempt adds GapNoModel gaps but the plan is then declined, and Reset()
// is called before the second attempt which adds no gaps, Items() returns empty after the
// second attempt.
//
// Without Reset(), gaps from the declined attempt would contaminate the final report, causing
// the user to see stale "no model selected" entries even after the model was resolved in the
// confirmed plan.
func TestCollector_Reset_DeclinedThenSucceedProducesNoGaps(t *testing.T) {
	c := todo.NewCollector()

	// First attempt: plan has model gaps; user declines the plan review.
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-no-model",
		Detail: "no model has been selected for agent agent-no-model"})

	// Caller (the service layer) calls Reset() before starting the next attempt.
	c.Reset()

	// Second attempt: all models resolved; the confirmed plan has no gaps.
	// Nothing is added to the collector.

	if !c.Empty() {
		t.Error("Empty() = false after declined-then-succeed sequence with Reset(); want true — " +
			"gaps from the declined attempt must not persist into the final report")
	}
	if items := c.Items(); len(items) != 0 {
		t.Errorf("Items() len = %d after declined-then-succeed sequence with Reset(); want 0 — "+
			"stale gaps must be discarded so the confirmed plan's empty gap list is the final state",
			len(items))
	}
}

// TestCollector_Reset_DeclinedThenSucceedPreservesSecondAttemptGaps verifies that when the
// second (confirmed) attempt has its own legitimate gaps, exactly those gaps appear in
// Items() and the declined attempt's gaps do not.
//
// This is the dual of TestCollector_Reset_DeclinedThenSucceedProducesNoGaps: it confirms
// that Reset() does not suppress all gap reporting — only stale gaps from abandoned attempts
// are discarded. Gaps produced by the confirmed plan must survive to the final report.
func TestCollector_Reset_DeclinedThenSucceedPreservesSecondAttemptGaps(t *testing.T) {
	c := todo.NewCollector()

	// First attempt: plan has a GapNoModel; plan is declined.
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-declined",
		Detail: "no model has been selected for agent agent-declined"})
	// Reset clears the stale gap before the second attempt.
	c.Reset()

	// Second attempt: confirmed plan has its own legitimate gap (a skipped file).
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "agents/conflict.md"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1 — only the second attempt's gap must survive; "+
			"got: %+v", len(items), items)
	}
	if items[0].Subject != "agents/conflict.md" {
		t.Errorf("Items()[0].Subject = %q; want %q — "+
			"the surviving gap must be from the confirmed attempt, not from the declined one",
			items[0].Subject, "agents/conflict.md")
	}
	if items[0].Category != domain.TodoSkippedFiles {
		t.Errorf("Items()[0].Category = %q; want %q",
			items[0].Category, domain.TodoSkippedFiles)
	}
}
