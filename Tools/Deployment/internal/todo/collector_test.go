package todo_test

// Tests for TODO item collection and category grouping (T15.6).
//
// The Collector maps every documented Gap kind to the correct TodoCategory and groups the
// resulting items deterministically: category declaration order first, then ascending Subject
// within each group. Every gap kind documented in the Stage 15 plan must be covered here.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
)

// ---------------------------------------------------------------------------
// Gap-to-category mapping: every gap kind listed in the Stage 15 plan
// ---------------------------------------------------------------------------

// TestCollector_AddGap_NoModel_MapsToModelsCategory verifies that a GapNoModel gap produces a
// TodoItem in the Models category, covering the "agents left without a model" requirement.
func TestCollector_AddGap_NoModel_MapsToModelsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-no-model"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoModels {
		t.Errorf("Category = %q; want %q", items[0].Category, domain.TodoModels)
	}
	if items[0].Subject != "agent-no-model" {
		t.Errorf("Subject = %q; want %q", items[0].Subject, "agent-no-model")
	}
}

// TestCollector_AddGap_UnmappedTool_MapsToToolMappingsCategory verifies that a GapUnmappedTool
// gap produces a TodoItem in the Tool mappings category, covering "generic tools left without a
// harness mapping".
func TestCollector_AddGap_UnmappedTool_MapsToToolMappingsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "file_read"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoToolMappings {
		t.Errorf("Category = %q; want %q", items[0].Category, domain.TodoToolMappings)
	}
}

// TestCollector_AddGap_EmptyInjection_MapsToInjectionsCategory verifies that a GapEmptyInjection
// gap produces a TodoItem in the Project-specific injections category.
func TestCollector_AddGap_EmptyInjection_MapsToInjectionsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapEmptyInjection, Subject: "HarnessConstraints"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoInjections {
		t.Errorf("Category = %q; want %q", items[0].Category, domain.TodoInjections)
	}
}

// TestCollector_AddGap_RemovedInjection_MapsToInjectionsCategory verifies that a
// GapRemovedInjection gap (injection point gone from source) produces a TodoItem in the
// Project-specific injections category so the user knows preserved content needs relocation.
func TestCollector_AddGap_RemovedInjection_MapsToInjectionsCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapRemovedInjection, Subject: "OldCustomSection"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoInjections {
		t.Errorf("Category = %q; want %q for GapRemovedInjection", items[0].Category, domain.TodoInjections)
	}
}

// TestCollector_AddGap_SkippedFile_MapsToSkippedFilesCategory verifies that a GapSkippedFile
// gap produces a TodoItem in the Skipped files category, covering "files skipped during update".
func TestCollector_AddGap_SkippedFile_MapsToSkippedFilesCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "agents/locally-modified.md"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoSkippedFiles {
		t.Errorf("Category = %q; want %q", items[0].Category, domain.TodoSkippedFiles)
	}
}

// TestCollector_AddGap_HookRegistration_MapsToRegistrationCategory verifies that a
// GapHookRegistration gap produces a TodoItem in the Hook registration category, covering
// "hook registration fragments the tool declined to merge into an existing user-owned config".
func TestCollector_AddGap_HookRegistration_MapsToRegistrationCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapHookRegistration, Subject: ".vscode/settings.json"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoRegistration {
		t.Errorf("Category = %q; want %q", items[0].Category, domain.TodoRegistration)
	}
}

// TestCollector_AddGap_ManualStep_MapsToManualCategory verifies that a GapManualStep gap
// produces a TodoItem in the Manual steps category, covering "registration steps the tool
// cannot perform at all".
func TestCollector_AddGap_ManualStep_MapsToManualCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapManualStep, Subject: "editor-font-size-setting"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoManual {
		t.Errorf("Category = %q; want %q", items[0].Category, domain.TodoManual)
	}
}

// TestCollector_AddGap_FallbackLocation_MapsToEnvironmentCategory verifies that a
// GapFallbackLocation gap produces a TodoItem in the Deployment location category.
func TestCollector_AddGap_FallbackLocation_MapsToEnvironmentCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapFallbackLocation, Subject: "workspace"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoEnvironment {
		t.Errorf("Category = %q; want %q", items[0].Category, domain.TodoEnvironment)
	}
}

// TestCollector_AddGap_UnsupportedArtifact_MapsToCategory verifies that a GapUnsupportedArtifact
// gap (e.g. hooks requested for a harness that does not support them) produces a TodoItem in the
// Deployment location category. This is an environment-level limitation — the harness does not
// support the requested artifact kind — so it groups with other deployment-environment gaps.
func TestCollector_AddGap_UnsupportedArtifact_MapsToCategory(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapUnsupportedArtifact, Subject: "hook-bundle"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Category != domain.TodoEnvironment {
		t.Errorf("Category = %q; want %q for GapUnsupportedArtifact", items[0].Category, domain.TodoEnvironment)
	}
	if items[0].Subject != "hook-bundle" {
		t.Errorf("Subject = %q; want %q", items[0].Subject, "hook-bundle")
	}
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

// TestCollector_Empty_TrueWhenNothingAdded verifies that a newly created Collector reports
// itself empty before any items are added.
func TestCollector_Empty_TrueWhenNothingAdded(t *testing.T) {
	c := todo.NewCollector()
	if !c.Empty() {
		t.Error("Empty() = false on a newly created collector; want true")
	}
}

// TestCollector_Empty_FalseAfterAdd verifies that Empty() returns false after a direct Add.
func TestCollector_Empty_FalseAfterAdd(t *testing.T) {
	c := todo.NewCollector()
	c.Add(domain.TodoItem{Category: domain.TodoModels, Subject: "agent-a"})
	if c.Empty() {
		t.Error("Empty() = true after Add; want false")
	}
}

// TestCollector_Empty_FalseAfterAddGap verifies that Empty() returns false after AddGap.
func TestCollector_Empty_FalseAfterAddGap(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-b"})
	if c.Empty() {
		t.Error("Empty() = true after AddGap; want false")
	}
}

// ---------------------------------------------------------------------------
// Grouping and ordering
// ---------------------------------------------------------------------------

// TestCollector_Groups_OnlyNonEmptyCategoriesReturned verifies that Groups() includes only
// categories that have at least one item, not all seven categories.
func TestCollector_Groups_OnlyNonEmptyCategoriesReturned(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"}) // → TodoModels only

	groups := c.Groups()
	if len(groups) != 1 {
		t.Fatalf("Groups() len = %d; want 1 (only TodoModels has items)", len(groups))
	}
	if groups[0].Category != domain.TodoModels {
		t.Errorf("groups[0].Category = %q; want %q", groups[0].Category, domain.TodoModels)
	}
}

// TestCollector_Groups_OrderFollowsCategoryDeclaration verifies that Groups() returns groups
// in the canonical category order regardless of insertion order.
func TestCollector_Groups_OrderFollowsCategoryDeclaration(t *testing.T) {
	c := todo.NewCollector()
	// Insert in reverse of expected output order.
	c.AddGap(domain.Gap{Kind: domain.GapFallbackLocation, Subject: "workspace"})
	c.AddGap(domain.Gap{Kind: domain.GapHookRegistration, Subject: "pre-commit"})
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "file.md"})
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})

	groups := c.Groups()
	// Canonical order per domain category declaration:
	// TodoModels < TodoSkippedFiles < TodoRegistration < TodoEnvironment
	wantOrder := []domain.TodoCategory{
		domain.TodoModels,
		domain.TodoSkippedFiles,
		domain.TodoRegistration,
		domain.TodoEnvironment,
	}
	if len(groups) != len(wantOrder) {
		t.Fatalf("Groups() len = %d; want %d", len(groups), len(wantOrder))
	}
	for i, want := range wantOrder {
		if groups[i].Category != want {
			t.Errorf("groups[%d].Category = %q; want %q", i, groups[i].Category, want)
		}
	}
}

// TestCollector_Groups_ItemsSortedBySubjectWithinGroup verifies that within each group items
// are sorted by Subject in ascending lexicographic order.
func TestCollector_Groups_ItemsSortedBySubjectWithinGroup(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "zeta-agent"})
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "alpha-agent"})
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "beta-agent"})

	groups := c.Groups()
	if len(groups) != 1 {
		t.Fatalf("Groups() len = %d; want 1", len(groups))
	}
	items := groups[0].Items
	if len(items) != 3 {
		t.Fatalf("group.Items len = %d; want 3", len(items))
	}
	wantSubjects := []string{"alpha-agent", "beta-agent", "zeta-agent"}
	for i, want := range wantSubjects {
		if items[i].Subject != want {
			t.Errorf("items[%d].Subject = %q; want %q", i, items[i].Subject, want)
		}
	}
}

// TestCollector_Items_DeterministicOrderAcrossCalls verifies that Items() returns the same
// order on every call given the same set of added items.
func TestCollector_Items_DeterministicOrderAcrossCalls(t *testing.T) {
	build := func() []domain.TodoItem {
		c := todo.NewCollector()
		c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "file.md"})
		c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})
		c.AddGap(domain.Gap{Kind: domain.GapHookRegistration, Subject: "hook-x"})
		return c.Items()
	}

	first := build()
	second := build()

	if len(first) != len(second) {
		t.Fatalf("Items() len differs between calls: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("Items()[%d] differs between calls: %+v vs %+v", i, first[i], second[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Field preservation
// ---------------------------------------------------------------------------

// TestCollector_Add_PreservesAllFields verifies that Add stores the TodoItem verbatim,
// including Detail and Fragment.
func TestCollector_Add_PreservesAllFields(t *testing.T) {
	c := todo.NewCollector()
	want := domain.TodoItem{
		Category: domain.TodoRegistration,
		Subject:  "settings.json",
		Detail:   "Add the hook configuration to VS Code settings",
		Fragment: `{"hooks": {"pre-commit": true}}`,
	}
	c.Add(want)

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0] != want {
		t.Errorf("Items()[0] = %+v; want %+v", items[0], want)
	}
}

// TestCollector_AddGap_TransfersDetailAndFragment verifies that AddGap transfers the Gap's
// Detail and Fragment fields to the resulting TodoItem unchanged.
func TestCollector_AddGap_TransfersDetailAndFragment(t *testing.T) {
	c := todo.NewCollector()
	gap := domain.Gap{
		Kind:     domain.GapHookRegistration,
		Subject:  "pre-commit",
		Detail:   "Add the hook script to .git/hooks/pre-commit",
		Fragment: "#!/bin/sh\nmosaic-deploy hook run\n",
	}
	c.AddGap(gap)

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Detail != gap.Detail {
		t.Errorf("Detail = %q; want %q", items[0].Detail, gap.Detail)
	}
	if items[0].Fragment != gap.Fragment {
		t.Errorf("Fragment = %q; want %q", items[0].Fragment, gap.Fragment)
	}
}

// TestCollector_AddGap_TransfersSubject verifies that AddGap preserves the Gap Subject in
// the resulting TodoItem.
func TestCollector_AddGap_TransfersSubject(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "search_web"})

	items := c.Items()
	if len(items) != 1 {
		t.Fatalf("Items() len = %d; want 1", len(items))
	}
	if items[0].Subject != "search_web" {
		t.Errorf("Subject = %q; want %q", items[0].Subject, "search_web")
	}
}

// TestCollector_MultipleCategories_CountsAreCorrect verifies that Items() returns one item
// per AddGap call even when multiple categories are involved.
func TestCollector_MultipleCategories_CountsAreCorrect(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-a"})
	c.AddGap(domain.Gap{Kind: domain.GapNoModel, Subject: "agent-b"})
	c.AddGap(domain.Gap{Kind: domain.GapUnmappedTool, Subject: "file_edit"})
	c.AddGap(domain.Gap{Kind: domain.GapSkippedFile, Subject: "agents/conflict.md"})

	items := c.Items()
	if len(items) != 4 {
		t.Errorf("Items() len = %d; want 4", len(items))
	}
}
