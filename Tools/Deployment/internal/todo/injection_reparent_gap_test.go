package todo_test

// injection_reparent_gap_test.go covers the mapping from GapInjectionReparented to
// TodoInjections.
//
// When the transform emits a GapInjectionReparented gap, the collector must map it to the
// TodoInjections category so it appears in the Injections section of the TODO report
// alongside other user-content-preservation notices (orphaned injections, parked custom
// regions, fallthrough custom regions).
//
// This test is in the TDD RED phase: the gapToCategory map entry for
// GapInjectionReparented is not yet present in collect.go, so AddGap currently falls
// through to the default category (TodoManual). The test will fail at runtime until the
// Stage 4 implementation adds the entry.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// TestCollector_AddGap_InjectionReparented_MapsToInjectionsCategory verifies that a gap
// of kind GapInjectionReparented is catalogued under the TodoInjections category. A
// re-parenting advisory concerns user-owned injection content and belongs in the same
// category as other user-content-preservation notices.
func TestCollector_AddGap_InjectionReparented_MapsToInjectionsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapInjectionReparented,
		Subject: "IdentityExtension",
		Detail:  `Injection "IdentityExtension" moved from Identity to Capabilities. Your content was preserved at the new location — review that it still reads correctly there. (detected 2026-08-12T14:25:33Z)`,
		Fragment: "",
	})

	items := c.Items()

	var found *domain.TodoItem
	for i := range items {
		if items[i].Subject == "IdentityExtension" {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatal("no todo item was emitted for GapInjectionReparented; expected one item with Subject 'IdentityExtension'")
	}
	if found.Category != domain.TodoInjections {
		t.Errorf("GapInjectionReparented item Category: want %q (TodoInjections), got %q; "+
			"a re-parenting advisory must surface in the Injections section of the TODO report alongside "+
			"other user-content-preservation notices",
			domain.TodoInjections, found.Category)
	}
}

// TestCollector_AddGap_InjectionReparented_SubjectAndDetailPreserved verifies that the
// Subject and Detail fields supplied to AddGap are carried unchanged into the TodoItem.
func TestCollector_AddGap_InjectionReparented_SubjectAndDetailPreserved(t *testing.T) {
	const subject = "ContextExtension"
	const detail = `Injection "ContextExtension" moved from Capabilities to ErrorHandling. Your content was preserved at the new location — review that it still reads correctly there. (detected 2026-08-12T14:25:33Z)`

	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapInjectionReparented,
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
		t.Errorf("TodoItem Fragment must be empty for GapInjectionReparented; got %q", found.Fragment)
	}
}

// TestCollector_AddGap_InjectionReparented_FragmentIsEmpty verifies that the Fragment
// field is empty for GapInjectionReparented items. Content is preserved in the output at
// the new location; no paste-fragment is needed.
func TestCollector_AddGap_InjectionReparented_FragmentIsEmpty(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapInjectionReparented,
		Subject: "MyInjection",
		Detail:  "some detail",
		Fragment: "",
	})

	items := c.Items()
	for _, item := range items {
		if item.Subject == "MyInjection" {
			if item.Fragment != "" {
				t.Errorf("GapInjectionReparented TodoItem Fragment must be empty; got %q", item.Fragment)
			}
			return
		}
	}
	t.Fatal("no todo item found with Subject 'MyInjection'")
}
