package todo_test

// groupbyowner_test.go covers todo.GroupByOwner, the pure partitioning function that feeds
// the owner-grouped rendering of project-specific injection items.
//
// GroupByOwner contract:
//   - Named owners come first, sorted ascending by owner key.
//   - Items with an empty Owner form a single final group with Owner == "".
//   - Items within a group keep the order they arrived in (already Subject-ascending from
//     the Collector), so no secondary sort is applied.
//   - The function is pure: it copies and never mutates its input slice.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// Basic partitioning
// ---------------------------------------------------------------------------

// TestGroupByOwner_EmptyInput_ReturnsEmptySlice verifies that GroupByOwner called with no
// items returns an empty slice rather than nil or a single empty group.
func TestGroupByOwner_EmptyInput_ReturnsEmptySlice(t *testing.T) {
	groups := todo.GroupByOwner(nil)
	if len(groups) != 0 {
		t.Errorf("GroupByOwner(nil) returned %d groups; want 0", len(groups))
	}
}

// TestGroupByOwner_AllSameOwner_OneGroup verifies that items sharing an owner end up in one
// group under that owner, preserving their order.
func TestGroupByOwner_AllSameOwner_OneGroup(t *testing.T) {
	items := []domain.TodoItem{
		{Category: domain.TodoInjections, Subject: "alpha", Owner: "plan-review"},
		{Category: domain.TodoInjections, Subject: "beta", Owner: "plan-review"},
	}

	groups := todo.GroupByOwner(items)

	if len(groups) != 1 {
		t.Fatalf("GroupByOwner: len(groups) = %d; want 1", len(groups))
	}
	if groups[0].Owner != "plan-review" {
		t.Errorf("groups[0].Owner = %q; want %q", groups[0].Owner, "plan-review")
	}
	if len(groups[0].Items) != 2 {
		t.Fatalf("groups[0].Items len = %d; want 2", len(groups[0].Items))
	}
	if groups[0].Items[0].Subject != "alpha" {
		t.Errorf("groups[0].Items[0].Subject = %q; want %q", groups[0].Items[0].Subject, "alpha")
	}
	if groups[0].Items[1].Subject != "beta" {
		t.Errorf("groups[0].Items[1].Subject = %q; want %q", groups[0].Items[1].Subject, "beta")
	}
}

// TestGroupByOwner_AllUnowned_OneFinalGroup verifies that items with an empty Owner end up in
// a single group with Owner == "" and that group is last (only group here since there are no
// named owners).
func TestGroupByOwner_AllUnowned_OneFinalGroup(t *testing.T) {
	items := []domain.TodoItem{
		{Category: domain.TodoInjections, Subject: "alpha", Owner: ""},
		{Category: domain.TodoInjections, Subject: "beta", Owner: ""},
	}

	groups := todo.GroupByOwner(items)

	if len(groups) != 1 {
		t.Fatalf("GroupByOwner: len(groups) = %d; want 1", len(groups))
	}
	if groups[0].Owner != "" {
		t.Errorf("groups[0].Owner = %q; want empty string (unattributed group)", groups[0].Owner)
	}
	if len(groups[0].Items) != 2 {
		t.Fatalf("groups[0].Items len = %d; want 2", len(groups[0].Items))
	}
}

// ---------------------------------------------------------------------------
// Ordering: named owners ascending, unowned last
// ---------------------------------------------------------------------------

// TestGroupByOwner_NamedOwnersAscendingOrder verifies that named owner groups appear in
// ascending lexicographic order regardless of the order items arrived in.
func TestGroupByOwner_NamedOwnersAscendingOrder(t *testing.T) {
	items := []domain.TodoItem{
		{Category: domain.TodoInjections, Subject: "r1", Owner: "plan-review"},
		{Category: domain.TodoInjections, Subject: "b1", Owner: "build-review"},
		{Category: domain.TodoInjections, Subject: "t1", Owner: "test-review"},
	}

	groups := todo.GroupByOwner(items)

	if len(groups) != 3 {
		t.Fatalf("GroupByOwner: len(groups) = %d; want 3", len(groups))
	}
	wantOwners := []string{"build-review", "plan-review", "test-review"}
	for i, want := range wantOwners {
		if groups[i].Owner != want {
			t.Errorf("groups[%d].Owner = %q; want %q (ascending order)", i, groups[i].Owner, want)
		}
	}
}

// TestGroupByOwner_UnownedGroupIsLast verifies that the group for items with an empty Owner
// always appears after all named owner groups, regardless of insertion order.
func TestGroupByOwner_UnownedGroupIsLast(t *testing.T) {
	items := []domain.TodoItem{
		{Category: domain.TodoInjections, Subject: "u1", Owner: ""},       // unowned, first in input
		{Category: domain.TodoInjections, Subject: "a1", Owner: "agent-a"},
		{Category: domain.TodoInjections, Subject: "u2", Owner: ""},       // unowned, last in input
	}

	groups := todo.GroupByOwner(items)

	if len(groups) != 2 {
		t.Fatalf("GroupByOwner: len(groups) = %d; want 2 (agent-a + unattributed)", len(groups))
	}
	if groups[0].Owner != "agent-a" {
		t.Errorf("groups[0].Owner = %q; want %q (named owner must come first)", groups[0].Owner, "agent-a")
	}
	if groups[1].Owner != "" {
		t.Errorf("groups[1].Owner = %q; want empty string (unattributed group must be last)", groups[1].Owner)
	}
}

// TestGroupByOwner_ItemsWithinGroupKeepArrivalOrder verifies that items within one owner group
// keep the order they arrived in (the Collector already sorted them by Subject ascending, so
// GroupByOwner must not reorder within a group).
func TestGroupByOwner_ItemsWithinGroupKeepArrivalOrder(t *testing.T) {
	// Simulate Collector output order: Subject ascending within one owner.
	items := []domain.TodoItem{
		{Category: domain.TodoInjections, Subject: "Alpha", Owner: "agent-x"},
		{Category: domain.TodoInjections, Subject: "Beta", Owner: "agent-x"},
		{Category: domain.TodoInjections, Subject: "Gamma", Owner: "agent-x"},
	}

	groups := todo.GroupByOwner(items)

	if len(groups) != 1 {
		t.Fatalf("GroupByOwner: len(groups) = %d; want 1", len(groups))
	}
	got := groups[0].Items
	if len(got) != 3 {
		t.Fatalf("groups[0].Items len = %d; want 3", len(got))
	}
	wantSubjects := []string{"Alpha", "Beta", "Gamma"}
	for i, want := range wantSubjects {
		if got[i].Subject != want {
			t.Errorf("groups[0].Items[%d].Subject = %q; want %q (arrival order must be preserved)",
				i, got[i].Subject, want)
		}
	}
}

// TestGroupByOwner_TwoAgentsSameRegionName verifies that two agents carrying an injection
// region of the same name produce two distinct groups — making them distinguishable in the
// rendered checklist. This is the direct test for the "same-name disambiguation" requirement.
func TestGroupByOwner_TwoAgentsSameRegionName(t *testing.T) {
	items := []domain.TodoItem{
		{Category: domain.TodoInjections, Subject: "SeverityThresholds", Owner: "build-review"},
		{Category: domain.TodoInjections, Subject: "SeverityThresholds", Owner: "plan-review"},
	}

	groups := todo.GroupByOwner(items)

	if len(groups) != 2 {
		t.Fatalf("GroupByOwner: len(groups) = %d; want 2 (one per owner)", len(groups))
	}
	// build-review comes before plan-review (ascending).
	if groups[0].Owner != "build-review" {
		t.Errorf("groups[0].Owner = %q; want %q", groups[0].Owner, "build-review")
	}
	if len(groups[0].Items) != 1 || groups[0].Items[0].Subject != "SeverityThresholds" {
		t.Errorf("groups[0] does not contain exactly SeverityThresholds: %+v", groups[0].Items)
	}
	if groups[1].Owner != "plan-review" {
		t.Errorf("groups[1].Owner = %q; want %q", groups[1].Owner, "plan-review")
	}
	if len(groups[1].Items) != 1 || groups[1].Items[0].Subject != "SeverityThresholds" {
		t.Errorf("groups[1] does not contain exactly SeverityThresholds: %+v", groups[1].Items)
	}
}

// ---------------------------------------------------------------------------
// Purity: input must not be mutated
// ---------------------------------------------------------------------------

// TestGroupByOwner_DoesNotMutateInput verifies that GroupByOwner is a pure function: calling
// it does not modify the input slice or any of its elements.
func TestGroupByOwner_DoesNotMutateInput(t *testing.T) {
	original := []domain.TodoItem{
		{Category: domain.TodoInjections, Subject: "alpha", Owner: "agent-a"},
		{Category: domain.TodoInjections, Subject: "beta", Owner: ""},
	}
	// Make a deep copy to compare against after the call.
	snapshot := make([]domain.TodoItem, len(original))
	copy(snapshot, original)

	todo.GroupByOwner(original)

	for i := range original {
		if original[i] != snapshot[i] {
			t.Errorf("GroupByOwner mutated input[%d]: before=%+v after=%+v", i, snapshot[i], original[i])
		}
	}
}

// TestGroupByOwner_IsDeterministic verifies that calling GroupByOwner twice with the same
// input produces the same output — the function is pure and not affected by iteration order
// over maps or other non-deterministic sources.
func TestGroupByOwner_IsDeterministic(t *testing.T) {
	items := []domain.TodoItem{
		{Category: domain.TodoInjections, Subject: "r1", Owner: "plan-review"},
		{Category: domain.TodoInjections, Subject: "b1", Owner: "build-review"},
		{Category: domain.TodoInjections, Subject: "u1", Owner: ""},
	}

	first := todo.GroupByOwner(items)
	second := todo.GroupByOwner(items)

	if len(first) != len(second) {
		t.Fatalf("GroupByOwner not deterministic: first call returned %d groups, second returned %d",
			len(first), len(second))
	}
	for i := range first {
		if first[i].Owner != second[i].Owner {
			t.Errorf("groups[%d].Owner differs between calls: %q vs %q", i, first[i].Owner, second[i].Owner)
		}
		if len(first[i].Items) != len(second[i].Items) {
			t.Errorf("groups[%d].Items len differs: %d vs %d", i, len(first[i].Items), len(second[i].Items))
		}
	}
}
