package tui

// multiselect_adapters_test.go verifies the pure-function adapters that convert
// the flat domain.Option slices from askWorkflows, askUtilityAgents, and askHooks
// into the typed inputs expected by their dedicated multi-select screens.
//
// No TUI or tea.Program is involved. Tests compile and run without a terminal.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// optionsToWorkflowCategories
// ---------------------------------------------------------------------------

// TestOptionsToWorkflowCategories_EmptyOptions_ReturnsEmpty verifies that a nil
// (empty) option slice produces no categories and does not panic.
func TestOptionsToWorkflowCategories_EmptyOptions_ReturnsEmpty(t *testing.T) {
	cats := optionsToWorkflowCategories(nil)

	if len(cats) != 0 {
		t.Errorf("optionsToWorkflowCategories(nil) returned %d categories; want 0", len(cats))
	}
}

// TestOptionsToWorkflowCategories_GroupOrderMatchesFirstAppearance verifies that
// category order matches the first appearance of each Group value in the option
// slice, not alphabetical order. Given Build, Analysis, Build the result must be
// [Build, Analysis].
func TestOptionsToWorkflowCategories_GroupOrderMatchesFirstAppearance(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "b1", Label: "Build Workflow 1", Group: "Build"},
		{ID: "a1", Label: "Analysis Workflow 1", Group: "Analysis"},
		{ID: "b2", Label: "Build Workflow 2", Group: "Build"},
	}

	// Act
	cats := optionsToWorkflowCategories(opts)

	// Assert
	if len(cats) != 2 {
		t.Fatalf("got %d categories; want 2", len(cats))
	}
	if cats[0].Name != "Build" {
		t.Errorf("cats[0].Name = %q; want %q (category order must match first appearance of Group, not alphabet)",
			cats[0].Name, "Build")
	}
	if cats[1].Name != "Analysis" {
		t.Errorf("cats[1].Name = %q; want %q (category order must match first appearance of Group, not alphabet)",
			cats[1].Name, "Analysis")
	}
}

// TestOptionsToWorkflowCategories_WorkflowOrderWithinCategoryPreserved verifies
// that workflows within a single category appear in the same order as their
// source options.
func TestOptionsToWorkflowCategories_WorkflowOrderWithinCategoryPreserved(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "wf-c", Label: "Workflow C", Group: "Deploy"},
		{ID: "wf-a", Label: "Workflow A", Group: "Deploy"},
		{ID: "wf-b", Label: "Workflow B", Group: "Deploy"},
	}

	// Act
	cats := optionsToWorkflowCategories(opts)

	// Assert
	if len(cats) != 1 {
		t.Fatalf("got %d categories; want 1", len(cats))
	}
	wfs := cats[0].Workflows
	if len(wfs) != 3 {
		t.Fatalf("category has %d workflows; want 3", len(wfs))
	}
	if wfs[0].ID != "wf-c" || wfs[1].ID != "wf-a" || wfs[2].ID != "wf-b" {
		t.Errorf("workflow IDs = [%s %s %s]; want [wf-c wf-a wf-b] (declaration order must be preserved)",
			wfs[0].ID, wfs[1].ID, wfs[2].ID)
	}
}

// TestOptionsToWorkflowCategories_FieldsPopulatedFromOption verifies that a
// workflow's ID, Name, Description, and Hint are set from the corresponding
// Option fields: ID, Label, Description, Hint.
func TestOptionsToWorkflowCategories_FieldsPopulatedFromOption(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{
			ID:          "ci-build",
			Label:       "CI Build",
			Description: "Runs the CI build pipeline",
			Hint:        "Requires a CI token",
			Group:       "CI",
		},
	}

	// Act
	cats := optionsToWorkflowCategories(opts)

	// Assert
	if len(cats) != 1 || len(cats[0].Workflows) != 1 {
		t.Fatal("expected exactly one category with one workflow")
	}
	wf := cats[0].Workflows[0]
	if wf.ID != "ci-build" {
		t.Errorf("wf.ID = %q; want %q", wf.ID, "ci-build")
	}
	if wf.Name != "CI Build" {
		t.Errorf("wf.Name = %q; want %q (Option.Label must map to Workflow.Name)", wf.Name, "CI Build")
	}
	if wf.Description != "Runs the CI build pipeline" {
		t.Errorf("wf.Description = %q; want %q", wf.Description, "Runs the CI build pipeline")
	}
	if wf.Hint != "Requires a CI token" {
		t.Errorf("wf.Hint = %q; want %q", wf.Hint, "Requires a CI token")
	}
}

// TestOptionsToWorkflowCategories_UngroupedOptions_NotDropped verifies that
// options with an empty Group field are included in the output. Workflows without
// a group may legitimately exist and must not be silently discarded.
func TestOptionsToWorkflowCategories_UngroupedOptions_NotDropped(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "grouped-wf", Label: "Grouped Workflow", Group: "CI"},
		{ID: "ungrouped-wf", Label: "Ungrouped Workflow", Group: ""},
	}

	// Act
	cats := optionsToWorkflowCategories(opts)

	// Assert: count all workflows across all categories.
	total := 0
	for _, cat := range cats {
		total += len(cat.Workflows)
	}
	if total != 2 {
		t.Errorf("total workflows across all categories = %d; want 2 (ungrouped options must not be dropped)",
			total)
	}
}

// TestOptionsToWorkflowCategories_MultipleCategoriesWithInterleavedOptions
// verifies that interleaved options from multiple groups are correctly sorted
// into their categories without cross-contamination.
func TestOptionsToWorkflowCategories_MultipleCategoriesWithInterleavedOptions(t *testing.T) {
	// Arrange — four options across three groups, interleaved
	opts := []domain.Option{
		{ID: "build-1", Label: "Build 1", Group: "Build"},
		{ID: "test-1", Label: "Test 1", Group: "Test"},
		{ID: "build-2", Label: "Build 2", Group: "Build"},
		{ID: "deploy-1", Label: "Deploy 1", Group: "Deploy"},
	}

	// Act
	cats := optionsToWorkflowCategories(opts)

	// Assert
	if len(cats) != 3 {
		t.Fatalf("got %d categories; want 3 (Build, Test, Deploy)", len(cats))
	}

	buildCat := cats[0]
	if buildCat.Name != "Build" {
		t.Errorf("cats[0].Name = %q; want Build", buildCat.Name)
	}
	if len(buildCat.Workflows) != 2 {
		t.Errorf("Build category has %d workflows; want 2", len(buildCat.Workflows))
	}

	testCat := cats[1]
	if testCat.Name != "Test" {
		t.Errorf("cats[1].Name = %q; want Test", testCat.Name)
	}
	if len(testCat.Workflows) != 1 {
		t.Errorf("Test category has %d workflows; want 1", len(testCat.Workflows))
	}

	deployCat := cats[2]
	if deployCat.Name != "Deploy" {
		t.Errorf("cats[2].Name = %q; want Deploy", deployCat.Name)
	}
	if len(deployCat.Workflows) != 1 {
		t.Errorf("Deploy category has %d workflows; want 1", len(deployCat.Workflows))
	}
}

// ---------------------------------------------------------------------------
// optionsToAgents
// ---------------------------------------------------------------------------

// TestOptionsToAgents_EmptyOptions_ReturnsEmpty verifies that a nil option slice
// produces no agents and does not panic.
func TestOptionsToAgents_EmptyOptions_ReturnsEmpty(t *testing.T) {
	agents := optionsToAgents(nil)

	if len(agents) != 0 {
		t.Errorf("optionsToAgents(nil) returned %d agents; want 0", len(agents))
	}
}

// TestOptionsToAgents_FieldsMappedCorrectly verifies that Option.ID maps to
// Agent.Key, Option.Label maps to Agent.Name, and Option.Description maps to
// Agent.Description.
func TestOptionsToAgents_FieldsMappedCorrectly(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "code-reviewer", Label: "Code Reviewer", Description: "Reviews code quality"},
		{ID: "doc-writer", Label: "Documentation Writer", Description: "Writes documentation"},
	}

	// Act
	agents := optionsToAgents(opts)

	// Assert
	if len(agents) != 2 {
		t.Fatalf("got %d agents; want 2", len(agents))
	}
	if agents[0].Key != "code-reviewer" {
		t.Errorf("agents[0].Key = %q; want %q (Option.ID must map to Agent.Key)", agents[0].Key, "code-reviewer")
	}
	if agents[0].Name != "Code Reviewer" {
		t.Errorf("agents[0].Name = %q; want %q (Option.Label must map to Agent.Name)", agents[0].Name, "Code Reviewer")
	}
	if agents[0].Description != "Reviews code quality" {
		t.Errorf("agents[0].Description = %q; want %q", agents[0].Description, "Reviews code quality")
	}
	if agents[1].Key != "doc-writer" {
		t.Errorf("agents[1].Key = %q; want %q", agents[1].Key, "doc-writer")
	}
	if agents[1].Name != "Documentation Writer" {
		t.Errorf("agents[1].Name = %q; want %q", agents[1].Name, "Documentation Writer")
	}
}

// TestOptionsToAgents_OrderPreserved verifies that agents are returned in the
// same order as the input option slice.
func TestOptionsToAgents_OrderPreserved(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "agent-c", Label: "C"},
		{ID: "agent-a", Label: "A"},
		{ID: "agent-b", Label: "B"},
	}

	// Act
	agents := optionsToAgents(opts)

	// Assert
	if len(agents) != 3 {
		t.Fatalf("got %d agents; want 3", len(agents))
	}
	wantOrder := []string{"agent-c", "agent-a", "agent-b"}
	for i, want := range wantOrder {
		if agents[i].Key != want {
			t.Errorf("agents[%d].Key = %q; want %q (option order must be preserved)", i, agents[i].Key, want)
		}
	}
}

// ---------------------------------------------------------------------------
// optionsToHookBundles
// ---------------------------------------------------------------------------

// TestOptionsToHookBundles_EmptyOptions_HarnessNotSupported verifies that when
// the option slice is empty, harnessSupported is false. An empty option list
// signals that no hooks are available, which the HookScreen treats identically
// to the harness not supporting hooks.
func TestOptionsToHookBundles_EmptyOptions_HarnessNotSupported(t *testing.T) {
	_, supported := optionsToHookBundles(nil)

	if supported {
		t.Error("harnessSupported = true for empty options; want false " +
			"(empty options must signal not-supported so HookScreen shows its informational state)")
	}
}

// TestOptionsToHookBundles_NonEmptyOptions_HarnessSupported verifies that when
// options are present, harnessSupported is true.
func TestOptionsToHookBundles_NonEmptyOptions_HarnessSupported(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "pre-commit", Label: "pre-commit", Description: "Pre-commit checks"},
	}

	// Act
	_, supported := optionsToHookBundles(opts)

	// Assert
	if !supported {
		t.Error("harnessSupported = false for non-empty options; want true")
	}
}

// TestOptionsToHookBundles_EmptyOptions_ReturnsEmptyBundles verifies that an
// empty option slice produces an empty (or nil) bundle slice with no panic.
func TestOptionsToHookBundles_EmptyOptions_ReturnsEmptyBundles(t *testing.T) {
	bundles, _ := optionsToHookBundles(nil)

	if len(bundles) != 0 {
		t.Errorf("optionsToHookBundles(nil) returned %d bundles; want 0", len(bundles))
	}
}

// TestOptionsToHookBundles_FieldsMappedCorrectly verifies that Option.ID maps
// to HookBundle.Key and Option.Description maps to HookBundle.Description.
func TestOptionsToHookBundles_FieldsMappedCorrectly(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "pre-commit", Label: "pre-commit", Description: "Runs pre-commit checks"},
		{ID: "post-merge", Label: "post-merge", Description: "Runs post-merge hooks"},
	}

	// Act
	bundles, _ := optionsToHookBundles(opts)

	// Assert
	if len(bundles) != 2 {
		t.Fatalf("got %d bundles; want 2", len(bundles))
	}
	if bundles[0].Key != "pre-commit" {
		t.Errorf("bundles[0].Key = %q; want %q (Option.ID must map to HookBundle.Key)",
			bundles[0].Key, "pre-commit")
	}
	if bundles[0].Description != "Runs pre-commit checks" {
		t.Errorf("bundles[0].Description = %q; want %q",
			bundles[0].Description, "Runs pre-commit checks")
	}
	if bundles[1].Key != "post-merge" {
		t.Errorf("bundles[1].Key = %q; want %q", bundles[1].Key, "post-merge")
	}
}

// TestOptionsToHookBundles_OrderPreserved verifies that bundles are returned in
// the same order as the input option slice.
func TestOptionsToHookBundles_OrderPreserved(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "hook-z", Label: "hook-z"},
		{ID: "hook-a", Label: "hook-a"},
	}

	// Act
	bundles, _ := optionsToHookBundles(opts)

	// Assert
	if len(bundles) != 2 {
		t.Fatalf("got %d bundles; want 2", len(bundles))
	}
	if bundles[0].Key != "hook-z" || bundles[1].Key != "hook-a" {
		t.Errorf("bundle order = [%s %s]; want [hook-z hook-a] (option order must be preserved)",
			bundles[0].Key, bundles[1].Key)
	}
}
