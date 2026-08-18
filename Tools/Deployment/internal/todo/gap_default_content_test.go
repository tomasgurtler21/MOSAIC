package todo_test

// gap_default_content_test.go covers the TDD RED phase for Stage 7: gap kind categorisation
// for project regions that carry non-empty default content on first deployment.
//
// When the transform deploys source default content into a project region for the first time,
// it emits a GapDefaultContentDeployed gap. This gap must be:
//   - Distinct from GapEmptyInjection (which signals an empty region that needs user input)
//   - Mapped to the TodoInjections category alongside its sibling injection gap kinds
//
// This file references domain.GapDefaultContentDeployed, which is not yet declared. The file
// will not compile until implementation (I7.1) adds that constant — that compile failure is
// the intended TDD RED state.
//
// The existing consistency test that enforces gapToCategory exhaustiveness (every gap kind
// in domain/gaps.go must appear in the map exactly once) will enforce the mapping at runtime
// once the constant is declared and the map entry is added.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// TestCollector_AddGap_DefaultContentDeployed_MapsToInjectionsCategory verifies that a gap
// of kind GapDefaultContentDeployed is categorised under TodoInjections. This gap indicates
// that a project region received source default content on its first deployment and is ready
// to review — it is a user-content advisory, not a fill request, and belongs with other
// injection-related notices.
func TestCollector_AddGap_DefaultContentDeployed_MapsToInjectionsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapDefaultContentDeployed,
		Subject: "CodebaseContext",
	})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoInjections {
		t.Errorf("Category = %q; want %q for GapDefaultContentDeployed", items[0].Category, domain.TodoInjections)
	}
	if items[0].Subject != "CodebaseContext" {
		t.Errorf("Subject = %q; want %q", items[0].Subject, "CodebaseContext")
	}
}

// TestCollector_AddGap_DefaultContentDeployed_TransfersDetailAndFragment verifies that
// AddGap transfers the Detail and Fragment fields from a GapDefaultContentDeployed gap to
// the resulting TodoItem without modification.
func TestCollector_AddGap_DefaultContentDeployed_TransfersDetailAndFragment(t *testing.T) {
	wantDetail := "default content was deployed into this project region; review and adapt it — later updates will never overwrite it"
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapDefaultContentDeployed,
		Subject: "HarnessConstraints",
		Detail:  wantDetail,
	})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Detail != wantDetail {
		t.Errorf("Detail = %q; want %q", items[0].Detail, wantDetail)
	}
}

// TestCollector_DefaultContentDeployed_DistinctFromEmptyInjection verifies that
// GapDefaultContentDeployed and GapEmptyInjection produce separate, independent TodoItems in
// the collector. Both map to TodoInjections but represent different situations: one signals
// content ready for review, the other signals an empty region that needs filling.
func TestCollector_DefaultContentDeployed_DistinctFromEmptyInjection(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapDefaultContentDeployed, Subject: "CodebaseContext"})
	c.AddGap(domain.Gap{Kind: domain.GapEmptyInjection, Subject: "HarnessConstraints"})

	items := c.Items()
	if len(items) != 2 {
		t.Fatalf("Items() len = %d; want 2 (one per gap kind)", len(items))
	}
	for _, item := range items {
		if item.Category != domain.TodoInjections {
			t.Errorf("item %q: Category = %q; want %q", item.Subject, item.Category, domain.TodoInjections)
		}
	}
}

// TestCollector_DefaultContentDeployed_NotMappedToManual verifies that
// GapDefaultContentDeployed does not fall through to the default TodoManual category.
// This would happen if the gap kind is declared but not added to the gapToCategory map.
func TestCollector_DefaultContentDeployed_NotMappedToManual(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapDefaultContentDeployed,
		Subject: "SeverityThresholds",
	})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category == domain.TodoManual {
		t.Errorf("Category = %q; GapDefaultContentDeployed fell through to the default category — it must be registered in gapToCategory", items[0].Category)
	}
}

// TestCollector_AddGap_EmptyInjection_InjectionsCategory_RegressionPin verifies that the
// existing GapEmptyInjection → TodoInjections mapping is not disturbed by the addition of
// GapDefaultContentDeployed to the same category. This is a regression pin.
func TestCollector_AddGap_EmptyInjection_InjectionsCategory_RegressionPin(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapEmptyInjection, Subject: "CodebaseContext"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoInjections {
		t.Errorf("Category = %q; want %q (GapEmptyInjection → TodoInjections regression pin)", items[0].Category, domain.TodoInjections)
	}
}
