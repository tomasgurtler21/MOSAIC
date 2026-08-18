package screens

// agenttree_test.go verifies AgentTreeNode's AgentKeys() enumeration helper.
// No TUI or tea.Program is involved; all tests construct trees directly as
// AgentTreeNode literals.
//
// Fixtures use synthetic folder and agent names throughout. No test reads
// from or depends on files under Catalog/.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// AgentKeys — nil and empty cases
// ---------------------------------------------------------------------------

// TestAgentKeys_NilReceiver_ReturnsNilWithoutPanicking verifies the nil-safety
// contract: calling AgentKeys on a nil *AgentTreeNode returns nil and does not panic.
func TestAgentKeys_NilReceiver_ReturnsNilWithoutPanicking(t *testing.T) {
	var node *AgentTreeNode

	// Act — must not panic
	keys := node.AgentKeys()

	// Assert
	if keys != nil {
		t.Errorf("AgentKeys() on nil receiver = %v; want nil", keys)
	}
}

// TestAgentKeys_EmptyRootNode_ReturnsEmpty verifies that a root node with no
// children and no agents produces an empty (nil or zero-length) key list.
func TestAgentKeys_EmptyRootNode_ReturnsEmpty(t *testing.T) {
	// Arrange
	root := &AgentTreeNode{}

	// Act
	keys := root.AgentKeys()

	// Assert
	if len(keys) != 0 {
		t.Errorf("AgentKeys() on empty root = %v (len %d); want empty", keys, len(keys))
	}
}

// ---------------------------------------------------------------------------
// AgentKeys — single-level traversal
// ---------------------------------------------------------------------------

// TestAgentKeys_FlatAgentsAtRoot_ReturnsKeysInSourceOrder verifies that agents
// held directly by a node are returned in the order they appear in the Agents slice.
func TestAgentKeys_FlatAgentsAtRoot_ReturnsKeysInSourceOrder(t *testing.T) {
	// Arrange — three agents in deliberate non-alphabetical order
	root := &AgentTreeNode{
		Agents: []domain.Agent{
			{Key: "agent-gamma"},
			{Key: "agent-alpha"},
			{Key: "agent-beta"},
		},
	}

	// Act
	keys := root.AgentKeys()

	// Assert
	want := []string{"agent-gamma", "agent-alpha", "agent-beta"}
	assertStringSliceEqual(t, "AgentKeys() flat", keys, want)
}

// ---------------------------------------------------------------------------
// AgentKeys — nested traversal order
// ---------------------------------------------------------------------------

// TestAgentKeys_NestedFolders_ChildrenTraversedBeforeOwnAgents verifies the
// traversal contract: for each node, child folders are visited recursively in
// Children order first, then the node's own Agents in slice order.
func TestAgentKeys_NestedFolders_ChildrenTraversedBeforeOwnAgents(t *testing.T) {
	// Arrange
	//   root
	//   ├─ FolderA
	//   │   └─ agent-a1
	//   └─ own agents: agent-root1
	folderA := &AgentTreeNode{
		Name:   "FolderA",
		Agents: []domain.Agent{{Key: "agent-a1"}},
	}
	root := &AgentTreeNode{
		Children: []*AgentTreeNode{folderA},
		Agents:   []domain.Agent{{Key: "agent-root1"}},
	}

	// Act
	keys := root.AgentKeys()

	// Assert — children before own agents
	want := []string{"agent-a1", "agent-root1"}
	assertStringSliceEqual(t, "AgentKeys() children-before-own", keys, want)
}

// TestAgentKeys_MultipleChildFolders_ChildrenInOrder verifies that sibling
// child folders are traversed in their position in the Children slice.
func TestAgentKeys_MultipleChildFolders_ChildrenInOrder(t *testing.T) {
	// Arrange
	//   root
	//   ├─ FolderX  → agent-x1
	//   └─ FolderY  → agent-y1, agent-y2
	folderX := &AgentTreeNode{
		Name:   "FolderX",
		Agents: []domain.Agent{{Key: "agent-x1"}},
	}
	folderY := &AgentTreeNode{
		Name:   "FolderY",
		Agents: []domain.Agent{{Key: "agent-y1"}, {Key: "agent-y2"}},
	}
	root := &AgentTreeNode{
		Children: []*AgentTreeNode{folderX, folderY},
	}

	// Act
	keys := root.AgentKeys()

	// Assert
	want := []string{"agent-x1", "agent-y1", "agent-y2"}
	assertStringSliceEqual(t, "AgentKeys() sibling order", keys, want)
}

// ---------------------------------------------------------------------------
// AgentKeys — mixed shape (folder holds both sub-folders and own agents)
// ---------------------------------------------------------------------------

// TestAgentKeys_MixedShape_ChildFoldersAndOwnAgentsBothRetained verifies that a
// node that has both child folders and directly-held agents retains both, and that
// traversal visits the sub-folders before the node's own agents at every level.
func TestAgentKeys_MixedShape_ChildFoldersAndOwnAgentsBothRetained(t *testing.T) {
	// Arrange
	//   root
	//   ├─ Role        (mixed: has a sub-folder AND its own agents)
	//   │   ├─ Category  → agent-cat1
	//   │   └─ own agents: agent-role-direct
	//   └─ own agents: agent-root-direct
	category := &AgentTreeNode{
		Name:   "Category",
		Agents: []domain.Agent{{Key: "agent-cat1"}},
	}
	role := &AgentTreeNode{
		Name:     "Role",
		Children: []*AgentTreeNode{category},
		Agents:   []domain.Agent{{Key: "agent-role-direct"}},
	}
	root := &AgentTreeNode{
		Children: []*AgentTreeNode{role},
		Agents:   []domain.Agent{{Key: "agent-root-direct"}},
	}

	// Act
	keys := root.AgentKeys()

	// Assert — traversal: root's child (role) first, then root's own agents;
	// within role: role's child (category) first, then role's own agents.
	want := []string{"agent-cat1", "agent-role-direct", "agent-root-direct"}
	assertStringSliceEqual(t, "AgentKeys() mixed shape", keys, want)
}

// ---------------------------------------------------------------------------
// AgentKeys — arbitrary depth
// ---------------------------------------------------------------------------

// TestAgentKeys_ArbitraryDepth_FourLevelsDeep verifies that the traversal handles
// trees that are four or more levels deep without any fixed-depth assumption.
func TestAgentKeys_ArbitraryDepth_FourLevelsDeep(t *testing.T) {
	// Arrange — A → B → C → D, agent at each level
	//   root
	//   └─ A (agent-a)
	//       └─ B (agent-b)
	//           └─ C (agent-c)
	//               └─ D (agent-d)
	levelD := &AgentTreeNode{Name: "D", Agents: []domain.Agent{{Key: "agent-d"}}}
	levelC := &AgentTreeNode{Name: "C", Children: []*AgentTreeNode{levelD}, Agents: []domain.Agent{{Key: "agent-c"}}}
	levelB := &AgentTreeNode{Name: "B", Children: []*AgentTreeNode{levelC}, Agents: []domain.Agent{{Key: "agent-b"}}}
	levelA := &AgentTreeNode{Name: "A", Children: []*AgentTreeNode{levelB}, Agents: []domain.Agent{{Key: "agent-a"}}}
	root := &AgentTreeNode{Children: []*AgentTreeNode{levelA}}

	// Act
	keys := root.AgentKeys()

	// Assert — children-before-own at every level, deepest first
	want := []string{"agent-d", "agent-c", "agent-b", "agent-a"}
	assertStringSliceEqual(t, "AgentKeys() 4 levels deep", keys, want)
}

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------

func assertStringSliceEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: len = %d; want %d\n  got:  %v\n  want: %v", label, len(got), len(want), got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s: [%d] = %q; want %q\n  got:  %v\n  want: %v", label, i, got[i], want[i], got, want)
		}
	}
}
