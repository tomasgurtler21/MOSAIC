package tui

// agent_tree_adapter_test.go verifies the optionsToAgentTree pure-function adapter
// that converts the flat domain.Option slice produced by askDeployAgents into a
// recursive AgentTreeNode tree by splitting each option's Group on "/".
//
// No TUI or tea.Program is involved. Tests compile and run without a terminal.
// Fixtures use synthetic folder and agent names throughout; no test reads from or
// depends on files under Catalog/.

import (
	"sort"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// ---------------------------------------------------------------------------
// Empty and nil input
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_EmptyOptions_ReturnsNilWithoutPanicking verifies that an
// empty (nil) option slice produces a nil tree and does not panic.
func TestOptionsToAgentTree_EmptyOptions_ReturnsNilWithoutPanicking(t *testing.T) {
	tree := optionsToAgentTree(nil)

	if tree != nil {
		t.Errorf("optionsToAgentTree(nil) returned non-nil tree; want nil")
	}
}

// ---------------------------------------------------------------------------
// Group segment splitting — placement
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_SingleSegmentGroup_AgentPlacedInTopLevelFolder verifies
// that an option with a single-segment Group (e.g. "Role") produces a root node
// with one child folder named "Role" that holds the agent directly. No intermediate
// folder is created between the root and the agent.
func TestOptionsToAgentTree_SingleSegmentGroup_AgentPlacedInTopLevelFolder(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "agent-one", Label: "Agent One", Group: "Role"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Children) != 1 {
		t.Fatalf("root has %d children; want 1", len(tree.Children))
	}
	folder := tree.Children[0]
	if folder.Name != "Role" {
		t.Errorf("top-level folder name = %q; want %q", folder.Name, "Role")
	}
	if len(folder.Agents) != 1 {
		t.Fatalf("folder has %d agents; want 1", len(folder.Agents))
	}
	if folder.Agents[0].Key != "agent-one" {
		t.Errorf("agent key = %q; want %q", folder.Agents[0].Key, "agent-one")
	}
	// The folder should have no child folders of its own
	if len(folder.Children) != 0 {
		t.Errorf("top-level folder has %d children; want 0 (single-segment group must not create intermediate folder)",
			len(folder.Children))
	}
}

// TestOptionsToAgentTree_MultiSegmentGroup_AgentNestedUnderAllSegments verifies that a
// two-segment Group ("Role/Category") produces root → Role → Category, with the agent
// in the Category folder's Agents slice.
func TestOptionsToAgentTree_MultiSegmentGroup_AgentNestedUnderAllSegments(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "agent-two", Label: "Agent Two", Group: "Role/Category"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Children) != 1 {
		t.Fatalf("root has %d children; want 1", len(tree.Children))
	}
	role := tree.Children[0]
	if role.Name != "Role" {
		t.Errorf("level-1 folder name = %q; want %q", role.Name, "Role")
	}
	if len(role.Children) != 1 {
		t.Fatalf("Role has %d children; want 1", len(role.Children))
	}
	category := role.Children[0]
	if category.Name != "Category" {
		t.Errorf("level-2 folder name = %q; want %q", category.Name, "Category")
	}
	if len(category.Agents) != 1 {
		t.Fatalf("Category has %d agents; want 1", len(category.Agents))
	}
	if category.Agents[0].Key != "agent-two" {
		t.Errorf("agent key = %q; want %q", category.Agents[0].Key, "agent-two")
	}
	// Role must hold no agents directly — the agent belongs to Category, not Role
	if len(role.Agents) != 0 {
		t.Errorf("Role.Agents len = %d; want 0 (agent must be at deepest segment only)", len(role.Agents))
	}
}

// TestOptionsToAgentTree_EmptyGroup_AgentPlacedAtRoot verifies that an option with
// an empty Group string lands directly in root.Agents, not in any child folder.
func TestOptionsToAgentTree_EmptyGroup_AgentPlacedAtRoot(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "root-agent", Label: "Root Agent", Group: ""},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Agents) != 1 {
		t.Fatalf("root.Agents len = %d; want 1", len(tree.Agents))
	}
	if tree.Agents[0].Key != "root-agent" {
		t.Errorf("root agent key = %q; want %q", tree.Agents[0].Key, "root-agent")
	}
	if len(tree.Children) != 0 {
		t.Errorf("root.Children len = %d; want 0 (empty Group must not create any folder)", len(tree.Children))
	}
}

// ---------------------------------------------------------------------------
// Ordering
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_FolderOrderFollowsFirstAppearance verifies that top-level
// folders appear in the order their segment name is first seen in the option slice,
// not in alphabetical order.
func TestOptionsToAgentTree_FolderOrderFollowsFirstAppearance(t *testing.T) {
	// Arrange — Zebra group appears before Alpha group
	opts := []domain.Option{
		{ID: "z1", Label: "Z1", Group: "Zebra"},
		{ID: "a1", Label: "A1", Group: "Alpha"},
		{ID: "z2", Label: "Z2", Group: "Zebra"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Children) != 2 {
		t.Fatalf("root has %d children; want 2", len(tree.Children))
	}
	if tree.Children[0].Name != "Zebra" {
		t.Errorf("Children[0].Name = %q; want %q (folder order must match first appearance, not alphabet)",
			tree.Children[0].Name, "Zebra")
	}
	if tree.Children[1].Name != "Alpha" {
		t.Errorf("Children[1].Name = %q; want %q (folder order must match first appearance, not alphabet)",
			tree.Children[1].Name, "Alpha")
	}
}

// TestOptionsToAgentTree_AgentOrderWithinFolderPreserved verifies that agents
// within a folder appear in the same order as their source options.
func TestOptionsToAgentTree_AgentOrderWithinFolderPreserved(t *testing.T) {
	// Arrange — three agents in deliberate non-alphabetical order
	opts := []domain.Option{
		{ID: "agent-c", Label: "C", Group: "Folder"},
		{ID: "agent-a", Label: "A", Group: "Folder"},
		{ID: "agent-b", Label: "B", Group: "Folder"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil || len(tree.Children) != 1 {
		t.Fatal("expected exactly one top-level folder")
	}
	agents := tree.Children[0].Agents
	if len(agents) != 3 {
		t.Fatalf("folder has %d agents; want 3", len(agents))
	}
	wantOrder := []string{"agent-c", "agent-a", "agent-b"}
	for i, want := range wantOrder {
		if agents[i].Key != want {
			t.Errorf("agents[%d].Key = %q; want %q (source order must be preserved)", i, agents[i].Key, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Mixed shape (folder with both children and own agents)
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_MixedShape_FolderHoldsBothSubfoldersAndOwnAgents verifies
// that a folder produced by the adapter can simultaneously hold child folders (from
// multi-segment options) and its own agents (from single-segment options with the
// same top-level segment). Both slices must be non-empty.
func TestOptionsToAgentTree_MixedShape_FolderHoldsBothSubfoldersAndOwnAgents(t *testing.T) {
	// Arrange — Role/Sub produces a child folder; Role alone produces a direct agent.
	opts := []domain.Option{
		{ID: "sub-agent", Label: "Sub Agent", Group: "Role/Sub"},
		{ID: "direct-agent", Label: "Direct Agent", Group: "Role"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Children) != 1 {
		t.Fatalf("root has %d children; want 1", len(tree.Children))
	}
	role := tree.Children[0]
	if role.Name != "Role" {
		t.Errorf("top-level folder name = %q; want %q", role.Name, "Role")
	}
	if len(role.Children) == 0 {
		t.Error("Role.Children is empty; want at least one child folder (from Role/Sub option)")
	}
	if len(role.Agents) == 0 {
		t.Error("Role.Agents is empty; want at least one direct agent (from Role option)")
	}
}

// ---------------------------------------------------------------------------
// Arbitrary depth (three-or-more segments)
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_ArbitraryDepth_FourSegments verifies that a Group value
// with four slash-separated segments ("A/B/C/D") nests the agent four levels deep
// under the root, with no fixed-depth assumption in the adapter.
func TestOptionsToAgentTree_ArbitraryDepth_FourSegments(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "deep-agent", Label: "Deep Agent", Group: "A/B/C/D"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}

	// Walk down: root → A → B → C → D
	level := tree
	path := []string{"A", "B", "C", "D"}
	for depth, segment := range path {
		if len(level.Children) != 1 {
			t.Fatalf("at depth %d (%q): len(Children) = %d; want 1", depth, segment, len(level.Children))
		}
		child := level.Children[0]
		if child.Name != segment {
			t.Errorf("at depth %d: folder name = %q; want %q", depth, child.Name, segment)
		}
		level = child
	}
	// At the deepest level (D), the agent must be present
	if len(level.Agents) != 1 || level.Agents[0].Key != "deep-agent" {
		t.Errorf("deepest folder Agents = %v; want [{Key:deep-agent}]", level.Agents)
	}
}

// ---------------------------------------------------------------------------
// Field mapping
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_FieldMapping_IDToKey_LabelToName_DescriptionPreserved
// verifies the per-option field mapping contract: Option.ID → Agent.Key,
// Option.Label → Agent.Name, Option.Description → Agent.Description.
func TestOptionsToAgentTree_FieldMapping_IDToKey_LabelToName_DescriptionPreserved(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{
			ID:          "test-key",
			Label:       "Test Name",
			Description: "A test agent description",
			Group:       "SomeFolder",
		},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil || len(tree.Children) != 1 {
		t.Fatal("expected exactly one top-level folder")
	}
	agents := tree.Children[0].Agents
	if len(agents) != 1 {
		t.Fatalf("folder has %d agents; want 1", len(agents))
	}
	a := agents[0]
	if a.Key != "test-key" {
		t.Errorf("Agent.Key = %q; want %q (Option.ID must map to Agent.Key)", a.Key, "test-key")
	}
	if a.Name != "Test Name" {
		t.Errorf("Agent.Name = %q; want %q (Option.Label must map to Agent.Name)", a.Name, "Test Name")
	}
	if a.Description != "A test agent description" {
		t.Errorf("Agent.Description = %q; want %q", a.Description, "A test agent description")
	}
}

// ---------------------------------------------------------------------------
// Every-key-once invariant
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_AllInputKeysReachableExactlyOnce verifies the fundamental
// tree integrity contract: every agent key present in the input options is reachable
// from the resulting tree root exactly once via AgentKeys(). This is the property
// that keeps the downstream flat SelectedIDs() contract intact.
func TestOptionsToAgentTree_AllInputKeysReachableExactlyOnce(t *testing.T) {
	// Arrange — options spanning multiple groups, depths, and empty groups
	opts := []domain.Option{
		{ID: "k1", Group: "Folder/Sub"},
		{ID: "k2", Group: "Folder"},
		{ID: "k3", Group: "Other"},
		{ID: "k4", Group: ""},
		{ID: "k5", Group: "Folder/Sub"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert — collect all keys from the tree
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	gotKeys := tree.AgentKeys()
	if len(gotKeys) != len(opts) {
		t.Errorf("AgentKeys() returned %d keys; want %d (each input option must appear exactly once)",
			len(gotKeys), len(opts))
	}

	// Verify every input key is present in the tree output
	wantKeys := make([]string, len(opts))
	for i, o := range opts {
		wantKeys[i] = o.ID
	}
	sort.Strings(wantKeys)
	sortedGot := make([]string, len(gotKeys))
	copy(sortedGot, gotKeys)
	sort.Strings(sortedGot)
	for i, want := range wantKeys {
		if sortedGot[i] != want {
			t.Errorf("key %q present in input but missing from AgentKeys() output\n  want (sorted): %v\n  got  (sorted): %v",
				want, wantKeys, sortedGot)
			break
		}
	}

	// Verify no duplicates
	seen := make(map[string]int, len(gotKeys))
	for _, k := range gotKeys {
		seen[k]++
	}
	for k, count := range seen {
		if count > 1 {
			t.Errorf("key %q appears %d times in AgentKeys() output; want exactly once", k, count)
		}
	}
}

// ---------------------------------------------------------------------------
// Empty-segment handling (malformed Group values)
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_LeadingSlash_EmptySegmentsDropped verifies that a Group
// value with a leading slash ("/Role") is treated the same as "Role": empty segments
// are silently dropped rather than creating an empty-named folder or panicking.
func TestOptionsToAgentTree_LeadingSlash_EmptySegmentsDropped(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "slash-agent", Label: "Slash Agent", Group: "/Role"},
	}

	// Act — must not panic
	tree := optionsToAgentTree(opts)

	// Assert — agent must land in "Role" folder, not in an empty-named folder
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	for _, child := range tree.Children {
		if child.Name == "" {
			t.Error("tree contains an empty-named folder; leading-slash empty segment must be dropped")
		}
	}
	// The agent must be reachable
	keys := tree.AgentKeys()
	found := false
	for _, k := range keys {
		if k == "slash-agent" {
			found = true
		}
	}
	if !found {
		t.Error("agent key 'slash-agent' not found in tree; leading-slash option must not be dropped")
	}
}

// TestOptionsToAgentTree_TrailingSlash_EmptySegmentsDropped verifies that a trailing
// slash ("Role/") is treated as "Role": the trailing empty segment is dropped.
func TestOptionsToAgentTree_TrailingSlash_EmptySegmentsDropped(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "trail-agent", Label: "Trailing Agent", Group: "Role/"},
	}

	// Act — must not panic
	tree := optionsToAgentTree(opts)

	// Assert — no empty-named child folder; agent in "Role"
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Children) != 1 {
		t.Fatalf("root has %d children; want 1", len(tree.Children))
	}
	role := tree.Children[0]
	if role.Name != "Role" {
		t.Errorf("top-level folder name = %q; want %q", role.Name, "Role")
	}
	for _, child := range role.Children {
		if child.Name == "" {
			t.Error("Role contains an empty-named sub-folder; trailing-slash empty segment must be dropped")
		}
	}
	// Agent must be in Role.Agents (trailing slash stripped → single segment "Role")
	if len(role.Agents) != 1 || role.Agents[0].Key != "trail-agent" {
		t.Errorf("Role.Agents = %v; want [{Key:trail-agent}]", role.Agents)
	}
}

// TestOptionsToAgentTree_SlashOnly_AgentLandsAtRoot verifies that a Group of "/"
// (all segments empty after splitting) places the agent in root.Agents, equivalent
// to an empty Group string.
func TestOptionsToAgentTree_SlashOnly_AgentLandsAtRoot(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "slash-only-agent", Label: "Slash Only", Group: "/"},
	}

	// Act — must not panic
	tree := optionsToAgentTree(opts)

	// Assert — agent must be in root.Agents; no folders created
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Children) != 0 {
		t.Errorf("root.Children len = %d; want 0 (slash-only group has no non-empty segments)", len(tree.Children))
	}
	found := false
	for _, a := range tree.Agents {
		if a.Key == "slash-only-agent" {
			found = true
		}
	}
	if !found {
		t.Error("agent key 'slash-only-agent' not found in root.Agents; slash-only group must place agent at root")
	}
}

// ---------------------------------------------------------------------------
// Shared folder reuse (same segment across multiple options)
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_SharedTopLevelFolder_SingleFolderNodeCreated verifies
// that multiple options with the same top-level segment share a single folder node
// rather than creating one folder per option.
func TestOptionsToAgentTree_SharedTopLevelFolder_SingleFolderNodeCreated(t *testing.T) {
	// Arrange — three options all in "SharedRole"
	opts := []domain.Option{
		{ID: "s1", Group: "SharedRole"},
		{ID: "s2", Group: "SharedRole"},
		{ID: "s3", Group: "SharedRole"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert — exactly one top-level folder
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Children) != 1 {
		t.Errorf("root has %d children; want 1 (same-segment options must share a folder)", len(tree.Children))
	}
	if len(tree.Children[0].Agents) != 3 {
		t.Errorf("SharedRole has %d agents; want 3", len(tree.Children[0].Agents))
	}
}

// ---------------------------------------------------------------------------
// Nested folder reuse (same multi-segment path across multiple options)
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_SharedNestedFolder_SingleNodeCreated verifies that two
// options with the same two-segment group share both folder nodes.
func TestOptionsToAgentTree_SharedNestedFolder_SingleNodeCreated(t *testing.T) {
	// Arrange
	opts := []domain.Option{
		{ID: "n1", Group: "Parent/Child"},
		{ID: "n2", Group: "Parent/Child"},
	}

	// Act
	tree := optionsToAgentTree(opts)

	// Assert
	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if len(tree.Children) != 1 {
		t.Errorf("root has %d children; want 1 (Parent must be created once)", len(tree.Children))
	}
	parent := tree.Children[0]
	if len(parent.Children) != 1 {
		t.Errorf("Parent has %d children; want 1 (Child must be created once)", len(parent.Children))
	}
	child := parent.Children[0]
	if len(child.Agents) != 2 {
		t.Errorf("Child has %d agents; want 2 (both options must be in the shared folder)", len(child.Agents))
	}
}

// ---------------------------------------------------------------------------
// Root node identity
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_RootNode_HasEmptyName verifies that the returned root node
// has an empty Name, matching the synthetic-root contract ("Empty only for the
// synthetic root node").
func TestOptionsToAgentTree_RootNode_HasEmptyName(t *testing.T) {
	opts := []domain.Option{
		{ID: "any-agent", Group: "AnyFolder"},
	}

	tree := optionsToAgentTree(opts)

	if tree == nil {
		t.Fatal("optionsToAgentTree returned nil for non-empty input")
	}
	if tree.Name != "" {
		t.Errorf("root.Name = %q; want empty string (root is a synthetic node with no name)", tree.Name)
	}
}

// ---------------------------------------------------------------------------
// Type assertion helpers (ensure the type is usable in the tui package context)
// ---------------------------------------------------------------------------

// TestOptionsToAgentTree_ReturnType_IsPointerToAgentTreeNode is a compile-time
// assertion that the return type of optionsToAgentTree is *screens.AgentTreeNode.
// If the type changes, this test will fail to compile.
func TestOptionsToAgentTree_ReturnType_IsPointerToAgentTreeNode(t *testing.T) {
	opts := []domain.Option{{ID: "type-check", Group: "F"}}
	var _ *screens.AgentTreeNode = optionsToAgentTree(opts)
}
