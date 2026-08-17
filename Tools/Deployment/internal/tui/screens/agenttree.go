package screens

// agenttree.go declares the AgentTreeNode type: a recursive, depth-unbounded
// representation of the agent catalog folder tree. It is the display-only model
// consumed by DeployAgentScreen and produced by tui.optionsToAgentTree.
//
// This file contains declarations and the AgentKeys enumeration helper only.
// No dependency on the tui package is allowed here; screens must not import
// their parent.

import (
	"mosaic-deploy/internal/domain"
)

// AgentTreeNode is one folder in the agent catalog tree. A node holds ordered child
// folders and the agents that live directly inside it. Depth is unbounded: the type
// makes no assumption about how many levels the catalog has.
//
// The tree is a display-only model. Agent identity across the screen boundary is always
// the agent Key; folder names never cross that boundary.
type AgentTreeNode struct {
	// Name is the folder's own segment name, exactly as it appeared in the source
	// Group string. Empty only for the synthetic root node.
	Name string

	// Children are the child folders in first-appearance order.
	Children []*AgentTreeNode

	// Agents are the agents held directly by this folder, in source order.
	Agents []domain.Agent
}

// AgentKeys returns every agent key reachable from this node exactly once, in a stable
// traversal order: for each node, its child folders recursively in Children order, then
// the node's own Agents in slice order. Safe to call on a nil receiver (returns nil).
func (n *AgentTreeNode) AgentKeys() []string {
	if n == nil {
		return nil
	}
	var keys []string
	for _, child := range n.Children {
		keys = append(keys, child.AgentKeys()...)
	}
	for _, agent := range n.Agents {
		keys = append(keys, agent.Key)
	}
	return keys
}
