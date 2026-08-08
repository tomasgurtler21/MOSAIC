package domain

import (
	"fmt"
	"strings"
)

// RefusalError is a structured error for pre-invocation refusals.
// Every refusal names the specific condition and the resource involved.
// All refusals in the system produce one of these rather than guessing or
// silently falling back.
type RefusalError struct {
	Component string // which component refused (e.g. "orchfile", "compat", "artifact")
	Resource  string // what was being examined (file path, workflow id, row index)
	Reason    string // specific condition (e.g. "no version comment", "forward dependency")
}

func (e *RefusalError) Error() string {
	return fmt.Sprintf("%s: refused %q: %s", e.Component, e.Resource, e.Reason)
}

// UnresolvableApproachError is returned at the routing decision when a stage's
// Approach value has no matching row in the workflow's Execution Groups table.
// It is never recovered from: there is no default approach and no fallback order.
//
// This error is distinct from RefusalError. RefusalError describes a pre-invocation
// refusal attributable to a named component and resource. UnresolvableApproachError
// describes a routing-decision failure discovered mid-run, after admission succeeded;
// its identifying facts are a workflow, a stage, an approach token, and the declared
// alternatives.
type UnresolvableApproachError struct {
	WorkflowID WorkflowID
	Stage      StageNumber
	Approach   Approach
	Declared   []Approach // every approach the workflow's table declares, in table order
}

func (e *UnresolvableApproachError) Error() string {
	declared := make([]string, len(e.Declared))
	for i, a := range e.Declared {
		declared[i] = string(a)
	}
	return fmt.Sprintf(
		"stage %d: Approach %q is not declared in workflow %q Execution Groups table (declared: %s)",
		e.Stage, e.Approach, e.WorkflowID, strings.Join(declared, ", "),
	)
}

// PositionUnresolvedCause states why a position could not be resolved.
type PositionUnresolvedCause int

const (
	// CauseAgentNotInWorkflow: the recorded agent is neither a routing table
	// participant nor a declared infrastructure agent for this run.
	CauseAgentNotInWorkflow PositionUnresolvedCause = iota

	// CauseNoMatchingRow: the agent is in the routing table, but no row
	// matches the recorded phase and stage.
	CauseNoMatchingRow

	// CauseAmbiguousRow: several rows match and the sequence-based
	// disambiguation could not settle between them.
	CauseAmbiguousRow
)

// PositionUnresolvedError is returned when the workflow position cannot be
// determined from the artifact. Callers may inspect Cause; tests should
// assert on Cause rather than on message substrings.
type PositionUnresolvedError struct {
	AgentInstance string // the recorded agent, "{AgentName}#{Seq}"
	Phase         string // the recorded phase, as stored
	Stage         string // the recorded stage, as stored
	Cause         PositionUnresolvedCause
}

// Error states the cause, not merely the symptom. For CauseAgentNotInWorkflow
// it says that the named agent is not a participant in this workflow.
func (e *PositionUnresolvedError) Error() string {
	switch e.Cause {
	case CauseAgentNotInWorkflow:
		return fmt.Sprintf(
			"position unresolved: agent %q is not a participant in this workflow (phase %q, stage %q)",
			e.AgentInstance, e.Phase, e.Stage,
		)
	case CauseNoMatchingRow:
		return fmt.Sprintf(
			"position unresolved: no routing row matches agent %q at phase %q, stage %q",
			e.AgentInstance, e.Phase, e.Stage,
		)
	case CauseAmbiguousRow:
		return fmt.Sprintf(
			"position unresolved: multiple routing rows match agent %q at phase %q, stage %q, and sequence-based disambiguation could not settle between them",
			e.AgentInstance, e.Phase, e.Stage,
		)
	default:
		return fmt.Sprintf("position unresolved for agent %q at phase %q, stage %q", e.AgentInstance, e.Phase, e.Stage)
	}
}
