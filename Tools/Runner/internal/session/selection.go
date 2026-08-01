package session

import (
	"fmt"
	"mosaic-run/internal/domain"
)

// gatedInfraClasses is the set of infrastructure agent class names that are
// subject to at-most-one-per-class enforcement at run start. Non-gated classes
// (e.g. "review") have all declared agents active unconditionally.
// Note: isGatedInfraClass() in Tools/Runner/internal/tui/screens/setup.go
// maintains an equivalent set for the TUI layer. Keep both in sync when adding
// new gated classes.
var gatedInfraClasses = map[string]bool{
	"checkpoint": true,
	"commit":     true,
	"restore":    true,
}

// buildActiveAgentsFilter constructs the activeAgents map from the per-class
// selection in RunConfig and the declared infrastructure agents.
//
// Contract:
//   - For each gated class (checkpoint, commit, restore): if selections contains
//     an entry for that class, only the selected agent is included. If no entry
//     exists (single agent auto-selected), the single agent for that class is
//     included. If multiple agents of a gated class exist without a selection,
//     the session must have refused before calling this function — the caller
//     is responsible for that validation.
//   - All non-gated class agents (e.g. "review") are included unconditionally
//     whenever a non-nil filter is returned.
//   - Returns nil when no filtering is needed (every gated class has at most
//     one declared agent). The nil value is passed to evaluateTriggers as the
//     "all agents active" signal.
func buildActiveAgentsFilter(
	declared []domain.DeclaredInfraAgent,
	selections map[string]string,
) map[string]bool {
	if len(declared) == 0 {
		return nil
	}

	// Count agents per gated class to determine whether filtering is needed.
	classCount := make(map[string]int)
	for _, a := range declared {
		if gatedInfraClasses[a.Class] {
			classCount[a.Class]++
		}
	}

	// Check whether any gated class has more than one agent.
	needsFilter := false
	for _, count := range classCount {
		if count > 1 {
			needsFilter = true
			break
		}
	}
	if !needsFilter {
		return nil
	}

	// Build the active agents map.
	active := make(map[string]bool)
	for _, a := range declared {
		if !gatedInfraClasses[a.Class] {
			// Non-gated classes (e.g. "review") are always included.
			active[a.Name] = true
			continue
		}
		if classCount[a.Class] == 1 {
			// Single agent for this gated class — auto-selected.
			active[a.Name] = true
			continue
		}
		// Multiple agents for this gated class: include only the selected one.
		if selected, ok := selections[a.Class]; ok && selected == a.Name {
			active[a.Name] = true
		}
		// Non-selected agents are omitted (zero value false).
	}

	return active
}

// validateClassSelections checks that for every gated class with multiple
// declared agents, a selection entry exists in the selections map. Returns a
// non-nil error (suitable for use as a run-start refusal message) when any
// gated class has multiple agents and no selection is provided.
//
// Non-interactive CLI runs must supply --infra-class mappings for all such
// classes. The TUI wizard ensures the user selects before proceeding.
func validateClassSelections(declared []domain.DeclaredInfraAgent, selections map[string]string) error {
	classCount := make(map[string]int)
	for _, a := range declared {
		if gatedInfraClasses[a.Class] {
			classCount[a.Class]++
		}
	}
	for class, count := range classCount {
		if count > 1 {
			if _, ok := selections[class]; !ok {
				return fmt.Errorf(
					"multiple %s-class agents declared but no agent selected for this run; "+
						"use --infra-class %s=<agent> to specify which one to use",
					class, class,
				)
			}
		}
	}
	return nil
}
