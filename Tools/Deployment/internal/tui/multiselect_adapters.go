package tui

// multiselect_adapters.go holds pure-function adapters that translate the flat
// domain.Option slices produced by askWorkflows, askUtilityAgents, askHooks, and
// askStandaloneAgents into the typed inputs expected by their dedicated multi-select screens.
//
// Because these adapters have no side effects they can be exercised in unit tests
// without a running tea.Program.

import (
	"strings"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/tui/screens"
)

// optionsToWorkflowCategories reconstructs an ordered list of workflow categories
// from the flat option slice produced by askWorkflows. Categories are ordered by
// first appearance of each Group value. Options whose Group field is empty are
// placed in a category with an empty name at the position their group first
// appears.
//
// Each option maps to a domain.Workflow: ID→ID, Label→Name, Description→Description,
// Hint→Hint. The Category field of the returned Workflow is the Group value.
func optionsToWorkflowCategories(opts []domain.Option) []domain.WorkflowCategory {
	if len(opts) == 0 {
		return nil
	}

	// Preserve first-appearance order of category groups.
	categoryOrder := []string{}
	categoryMap := map[string][]domain.Workflow{}

	for _, opt := range opts {
		group := opt.Group
		if _, exists := categoryMap[group]; !exists {
			categoryOrder = append(categoryOrder, group)
			categoryMap[group] = []domain.Workflow{}
		}
		categoryMap[group] = append(categoryMap[group], domain.Workflow{
			ID:          opt.ID,
			Name:        opt.Label,
			Description: opt.Description,
			Hint:        opt.Hint,
			Category:    group,
		})
	}

	categories := make([]domain.WorkflowCategory, len(categoryOrder))
	for i, name := range categoryOrder {
		categories[i] = domain.WorkflowCategory{
			Name:      name,
			Workflows: categoryMap[name],
		}
	}
	return categories
}

// optionsToAgents converts the flat option slice from askUtilityAgents into a
// slice of domain.Agent values. Each option's ID becomes the agent Key; Label
// becomes Name; Description is preserved. Order is preserved.
func optionsToAgents(opts []domain.Option) []domain.Agent {
	if len(opts) == 0 {
		return nil
	}
	agents := make([]domain.Agent, len(opts))
	for i, opt := range opts {
		agents[i] = domain.Agent{
			Key:         opt.ID,
			Name:        opt.Label,
			Description: opt.Description,
		}
	}
	return agents
}

// optionsToAgentTree reconstructs the agent folder tree from the flat option slice
// produced by askDeployAgents. Each option's Group is a slash-joined path of on-disk
// folder names ("Subagents/Audit", "UtilityAgents", ""); the adapter splits it on "/"
// and files the agent under the deepest segment's folder, creating folders on first
// appearance.
//
// Returns the synthetic root node whose Children are the top-level folders and whose
// Agents are the options with no path segments. Returns nil for an empty option slice.
//
// Field mapping per option: ID→Key, Label→Name, Description→Description,
// Category→the deepest non-empty path segment ("" for root-held agents).
func optionsToAgentTree(opts []domain.Option) *screens.AgentTreeNode {
	if len(opts) == 0 {
		return nil
	}

	root := &screens.AgentTreeNode{}

	for _, opt := range opts {
		// Split the group path on "/" and drop empty segments (handles leading/trailing/double slashes).
		rawSegments := strings.Split(opt.Group, "/")
		var segments []string
		for _, s := range rawSegments {
			if s != "" {
				segments = append(segments, s)
			}
		}

		agent := domain.Agent{
			Key:         opt.ID,
			Name:        opt.Label,
			Description: opt.Description,
		}
		if len(segments) > 0 {
			// Category is the deepest non-empty path segment, per the design contract.
			agent.Category = segments[len(segments)-1]
		}

		if len(segments) == 0 {
			// No path segments — agent belongs directly at root.
			root.Agents = append(root.Agents, agent)
			continue
		}

		// Walk (or create) the folder chain, reusing existing nodes on first-appearance ordering.
		current := root
		for _, seg := range segments {
			var found *screens.AgentTreeNode
			for _, child := range current.Children {
				if child.Name == seg {
					found = child
					break
				}
			}
			if found == nil {
				found = &screens.AgentTreeNode{Name: seg}
				current.Children = append(current.Children, found)
			}
			current = found
		}

		// Append the agent to the deepest folder in its path.
		current.Agents = append(current.Agents, agent)
	}

	return root
}

// optionsToHookBundles converts the flat option slice from askHooks into a slice
// of domain.HookBundle values and infers whether the harness supports hooks.
//
// harnessSupported is true when len(opts) > 0, false otherwise. This lets
// HookScreen distinguish "harness does not support hooks" (show an informational
// message) from "no bundles available" (show an empty-state message).
//
// Each option maps to a HookBundle: ID→Key, Description→Description.
func optionsToHookBundles(opts []domain.Option) ([]domain.HookBundle, bool) {
	if len(opts) == 0 {
		return nil, false
	}
	bundles := make([]domain.HookBundle, len(opts))
	for i, opt := range opts {
		bundles[i] = domain.HookBundle{
			Key:         opt.ID,
			Description: opt.Description,
		}
	}
	return bundles, true
}
