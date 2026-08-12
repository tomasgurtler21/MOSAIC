package todo_test

// fallthrough_gap_test.go covers the mapping from GapCustomRegionFallthrough to TodoInjections.
//
// When the transform emits a GapCustomRegionFallthrough gap, the collector must map it to
// the TodoInjections category so it appears in the Injections section of the TODO report
// alongside other user-content-preservation notices (orphaned injections, parked custom
// regions, etc.).
//
// This test is in the TDD RED phase: the gapToCategory map entry for
// GapCustomRegionFallthrough is not yet present in collect.go, so AddGap currently falls
// through to the default category (TodoManual). The test will fail at runtime until Stage 3
// implementation (I3.2) is complete.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// TestCollector_AddGap_FallthroughCustomRegion_MapsToInjectionsCategory verifies that a
// gap of kind GapCustomRegionFallthrough is catalogued under the TodoInjections category.
// This is the expected mapping: a custom-region fallthrough is a user-content notice
// concerning a [[CUSTOM:]] region's placement, and belongs in the same category as other
// user-content-preservation notices (parked custom regions, orphaned injections, etc.).
func TestCollector_AddGap_FallthroughCustomRegion_MapsToInjectionsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:     domain.GapCustomRegionFallthrough,
		Subject:  "some-agent",
		Detail:   "Parent section no longer exists. These custom regions were placed at body level — move them to the correct section: MyNote. (detected 2026-08-11T17:07:26Z)",
		Fragment: "",
	})

	items := c.Items()

	var found *domain.TodoItem
	for i := range items {
		if items[i].Subject == "some-agent" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no todo item was emitted for GapCustomRegionFallthrough; expected one item with Subject 'some-agent'")
	}

	if found.Category != domain.TodoInjections {
		t.Errorf("GapCustomRegionFallthrough item Category: want %q (TodoInjections), got %q; "+
			"a fallthrough gap must surface in the Injections section of the TODO report alongside "+
			"other user-content-preservation notices (parked custom regions, orphaned injections, etc.)",
			domain.TodoInjections, found.Category)
	}
}

// TestCollector_AddGap_FallthroughCustomRegion_SubjectAndDetailPreserved verifies that the
// Subject and Detail fields supplied to AddGap are carried unchanged into the TodoItem.
func TestCollector_AddGap_FallthroughCustomRegion_SubjectAndDetailPreserved(t *testing.T) {
	const subject = "my-agent"
	const detail = "Parent section no longer exists. These custom regions were placed at body level — move them to the correct section: OldNote. (detected 2026-08-11T17:07:26Z)"

	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapCustomRegionFallthrough,
		Subject: subject,
		Detail:  detail,
	})

	items := c.Items()

	var found *domain.TodoItem
	for i := range items {
		if items[i].Subject == subject {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no todo item found with Subject %q", subject)
	}
	if found.Detail != detail {
		t.Errorf("TodoItem Detail: want %q, got %q", detail, found.Detail)
	}
	if found.Fragment != "" {
		t.Errorf("TodoItem Fragment must be empty for GapCustomRegionFallthrough; got %q", found.Fragment)
	}
}
