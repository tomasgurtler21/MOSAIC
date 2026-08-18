package todo_test

// enclosing_section_changed_gap_test.go covers the mapping from
// GapEnclosingSectionChanged to TodoInjections.
//
// When the transform emits a GapEnclosingSectionChanged gap, the collector must map it to
// the TodoInjections category so it appears in the Injections section of the TODO report
// alongside other user-content-preservation notices (parked custom regions, orphaned
// injections, fallthrough regions, deployed-region-content-changed, etc.).
//
// This test is in the TDD RED phase: the gapToCategory map entry for
// GapEnclosingSectionChanged must already have been added as part of the T2.1 contract
// surface, so AddGap should map it correctly immediately. If the entry is missing, AddGap
// silently falls back to TodoManual, which this test detects.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// TestCollector_AddGap_EnclosingSectionChanged_MapsToInjectionsCategory verifies that a
// gap of kind GapEnclosingSectionChanged is catalogued under the TodoInjections category
// when added to the collector. This is the expected mapping: an enclosing-section-changed
// gap is a user-content notice concerning a custom region's surrounding context, and it
// belongs in the same category as other user-content-preservation notices (parked custom
// regions, orphaned injections, fallthrough regions, deployed-region-content-changed gaps).
func TestCollector_AddGap_EnclosingSectionChanged_MapsToInjectionsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:     domain.GapEnclosingSectionChanged,
		Subject:  "some-agent",
		Owner:    "some-agent",
		Detail:   `Section "Identity" changed and contains your custom content. Review for contradictions: IdentityNote. (detected 2026-08-17T22:41:49Z)`,
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
		t.Fatal("no todo item was emitted for GapEnclosingSectionChanged; expected one item with Subject 'some-agent'")
	}

	if found.Category != domain.TodoInjections {
		t.Errorf("GapEnclosingSectionChanged item Category: want %q (TodoInjections), got %q; "+
			"an enclosing-section-changed gap must surface in the Injections section of the TODO report "+
			"alongside other user-content-preservation notices (parked custom regions, orphaned injections, "+
			"fallthrough regions, deployed-region-content-changed gaps)",
			domain.TodoInjections, found.Category)
	}
}

// TestCollector_AddGap_EnclosingSectionChanged_SubjectOwnerDetailPreserved verifies that
// the Subject, Owner, and Detail fields supplied to AddGap are carried unchanged into the
// TodoItem.
func TestCollector_AddGap_EnclosingSectionChanged_SubjectOwnerDetailPreserved(t *testing.T) {
	const subject = "my-agent"
	const owner = "my-agent"
	const detail = `Section "Constraints" changed and contains your custom content. Review for contradictions: TeamPolicy. (detected 2026-08-17T22:41:49Z)`

	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapEnclosingSectionChanged,
		Subject: subject,
		Owner:   owner,
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
	if found.Owner != owner {
		t.Errorf("TodoItem Owner: want %q, got %q", owner, found.Owner)
	}
	if found.Detail != detail {
		t.Errorf("TodoItem Detail: want %q, got %q", detail, found.Detail)
	}
	if found.Fragment != "" {
		t.Errorf("TodoItem Fragment must be empty for GapEnclosingSectionChanged; got %q", found.Fragment)
	}
}

// TestCollector_AddGap_EnclosingSectionChanged_AppearsInGroups verifies that a
// GapEnclosingSectionChanged gap appears in the TodoInjections group when Groups() is
// called, consistent with how the gap reaches the TODO file and the on-screen summary.
func TestCollector_AddGap_EnclosingSectionChanged_AppearsInGroups(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapEnclosingSectionChanged,
		Subject: "some-agent",
		Detail:  `Section "Identity" changed and contains your custom content. Review for contradictions: IdentityNote.`,
	})

	groups := c.Groups()

	var injectionsGroup *todo.Group
	for i := range groups {
		if groups[i].Category == domain.TodoInjections {
			injectionsGroup = &groups[i]
			break
		}
	}
	if injectionsGroup == nil {
		t.Fatal("TodoInjections group is absent from Groups() output; " +
			"GapEnclosingSectionChanged must produce a TodoInjections group entry when it is the only gap")
	}

	var found bool
	for _, item := range injectionsGroup.Items {
		if item.Subject == "some-agent" {
			found = true
			break
		}
	}
	if !found {
		t.Error("GapEnclosingSectionChanged item not found inside the TodoInjections group; " +
			"the gap must reach the Injections section of both the TODO file and the on-screen summary")
	}
}
