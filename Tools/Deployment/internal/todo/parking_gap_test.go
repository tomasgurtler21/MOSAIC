package todo_test

// parking_gap_test.go covers the mapping from GapParkedCustomRegion to TodoInjections.
//
// When the transform emits a GapParkedCustomRegion gap, the collector must map it to the
// TodoInjections category so it appears in the Injections section of the TODO report
// alongside other user-content-preservation notices (orphaned injections, renames, etc.).
//
// This test is in the TDD RED phase: the gapToCategory map entry for GapParkedCustomRegion
// is not yet present in collect.go, so AddGap currently falls through to the default
// category (TodoManual). The test will fail at runtime until Stage 7 implementation is complete.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// TestCollector_AddGap_ParkedCustomRegion_MapsToInjectionsCategory verifies that a gap of
// kind GapParkedCustomRegion is catalogued under the TodoInjections category when added to
// the collector. This is the expected mapping defined by the Stage 7 design: parking a
// user-owned custom region is a user-content notice, not a manual-action item, and therefore
// belongs in the same category as orphaned injection content and injection renames.
func TestCollector_AddGap_ParkedCustomRegion_MapsToInjectionsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapParkedCustomRegion,
		Subject: "some-agent",
		Detail:  "Schema reorder detected. These custom regions were preserved at end of file — move them to the correct section: MyNote. (detected 2026-08-11T17:07:26Z)",
		Fragment: "",
	})

	items := c.Items()

	// Find the item produced by GapParkedCustomRegion.
	var found *domain.TodoItem
	for i := range items {
		if items[i].Subject == "some-agent" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no todo item was emitted for GapParkedCustomRegion; expected one item with Subject 'some-agent'")
	}

	// The item must be in the TodoInjections category, not any other category.
	if found.Category != domain.TodoInjections {
		t.Errorf("GapParkedCustomRegion item Category: want %q (TodoInjections), got %q; "+
			"a parking gap must surface in the Injections section of the TODO report alongside "+
			"other user-content-preservation notices (orphaned injections, injection renames, etc.)",
			domain.TodoInjections, found.Category)
	}
}
