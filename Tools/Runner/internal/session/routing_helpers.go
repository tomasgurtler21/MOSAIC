package session

import (
	"strings"

	"mosaic-run/internal/domain"
)

// rowAtIndex returns the routing table row at the given zero-based index, or
// false if no row with that index exists.
func rowAtIndex(table domain.RoutingTable, idx int) (domain.RoutingRow, bool) {
	for _, row := range table.Rows {
		if row.Index == idx {
			return row, true
		}
	}
	return domain.RoutingRow{}, false
}

// extractAgentIdentifier extracts the agent identifier from an agent instance
// ID (e.g. "agent-a#1" -> "agent-a"). If the input contains no "#", it is
// returned unchanged.
func extractAgentIdentifier(instanceID string) string {
	if idx := strings.LastIndex(instanceID, "#"); idx >= 0 {
		return instanceID[:idx]
	}
	return instanceID
}

// rewindStateForRerun adjusts the artifact CurrentState for a mid-invocation
// interruption (FR-33): the last logged step was interrupted before Apply
// completed, so the session must re-dispatch it. Rewinding CurrentState to
// the last WORKFLOW log entry before the interrupted one (or empty) causes
// engine.Next to route to the interrupted row again.
//
// Trailing infrastructure entries between the interrupted step and the
// workflow step that precedes it are skipped: current_state must reflect the
// last completed WORKFLOW step, never an infrastructure invocation, so the
// engine's row-lookup stays correct when infrastructure activity interleaves
// with the interruption.
//
// An entry is recognised as infrastructure by the same two-condition
// derivation used elsewhere: absent from the routing table and present in
// infra. infra may be nil, meaning no infrastructure agents are declared.
func rewindStateForRerun(table domain.RoutingTable, infra domain.InfraAgentSet, state domain.ArtifactState) domain.ArtifactState {
	for i := len(state.ExecutionLog) - 2; i >= 0; i-- {
		entry := state.ExecutionLog[i]
		agentID := extractAgentIdentifier(entry.Agent)
		if !isRoutingTableAgent(table, agentID) {
			if infra != nil && infra.IsInfrastructureAgent(entry.Agent) {
				continue // recognised infrastructure entry: keep looking
			}
			// Not a routing table participant and not recognised
			// infrastructure either. Unreachable in practice -- ResumePoint
			// would already have stopped on this entry with
			// PositionUnresolvedError before this function is ever called --
			// but applying the same two-condition recognition rule used
			// elsewhere (rather than "absent from the routing table" alone)
			// keeps this function from silently masking a defect if that
			// call ordering ever changes.
			continue
		}
		state.CurrentState = domain.CurrentState{
			Phase:      entry.Phase,
			Stage:      entry.Stage,
			LastStatus: domain.StatusSUCCESS,
			LastAgent:  entry.Agent,
		}
		return state
	}
	state.CurrentState = domain.CurrentState{}
	return state
}

// isRoutingTableAgent reports whether agentID names a participant in table,
// in any row.
func isRoutingTableAgent(table domain.RoutingTable, agentID string) bool {
	for _, row := range table.Rows {
		if row.Agent == agentID {
			return true
		}
	}
	return false
}

// bindRunContext calls BindRunContext on v if v is non-nil and implements
// domain.RunContextBinder. Consultants that do not implement the interface are
// silently skipped, so callers need not check before calling.
func bindRunContext(v any, rc domain.RunContext) {
	if v == nil {
		return
	}
	if binder, ok := v.(domain.RunContextBinder); ok {
		binder.BindRunContext(rc)
	}
}

// uniqueAgentIdentifiers returns the unique agent identifiers from the routing
// table in first-occurrence order.
func uniqueAgentIdentifiers(table domain.RoutingTable) []string {
	seen := make(map[string]bool, len(table.Rows))
	result := make([]string, 0, len(table.Rows))
	for _, row := range table.Rows {
		if !seen[row.Agent] {
			seen[row.Agent] = true
			result = append(result, row.Agent)
		}
	}
	return result
}

// hasStageStarArtifact reports whether any artifact path in the slice contains
// the Stage-* wildcard pattern.
func hasStageStarArtifact(artifacts []string) bool {
	for _, a := range artifacts {
		if strings.Contains(a, "Stage-*") {
			return true
		}
	}
	return false
}

// onInfrastructureAgentTrigger is the named no-op hook that marks the
// infrastructure-agent trigger point in the dispatch loop (FR-40). It is
// called alongside Deps.OnInfrastructureTrigger after each harness invocation,
// making the trigger point discoverable by name. Production code passes nil
// for Deps.OnInfrastructureTrigger; tests inject a counter.
func onInfrastructureAgentTrigger() {
	// no-op: production hook; real infrastructure-agent dispatch goes here.
}

// resolveToRunScoped prepends the run-scoped folder prefix to each artifact
// path that does not already carry it. Paths that already start with prefix
// are passed through unchanged, preventing double-prefixing.
func resolveToRunScoped(paths []string, prefix string) []string {
	if len(paths) == 0 {
		return paths
	}
	resolved := make([]string, len(paths))
	for i, p := range paths {
		if strings.HasPrefix(p, prefix) {
			resolved[i] = p
		} else {
			resolved[i] = prefix + p
		}
	}
	return resolved
}

// formatInputs formats a list of input artifact paths as a comma-separated
// string for the Inputs column of the Execution Log. Returns "" when the
// list is empty (rendered as "-" in the table).
func formatInputs(paths []string) string {
	return strings.Join(paths, ", ")
}
