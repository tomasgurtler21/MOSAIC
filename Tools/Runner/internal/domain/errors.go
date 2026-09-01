package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ErrHarnessLaunchFailed is the class sentinel for the failure in which the
// harness subprocess could not be started at all. It is deliberately distinct
// from every failure of a process that did start (non-zero exit, timeout,
// cancellation, malformed output), because only a launch failure can be fixed
// by supplying a different executable path.
var ErrHarnessLaunchFailed = errors.New("harness: launch failed")

// HarnessLaunchError attributes a launch failure to a specific harness and
// the executable path that was tried. It is constructable in tests from any
// underlying error, including a synthetic one, so error-identity assertions
// need no subprocess.
//
// It reports true for errors.Is against ErrHarnessLaunchFailed and against
// every sentinel in the Err chain (notably
// mosaic-common/harness.ErrExecutableNotFound).
type HarnessLaunchError struct {
	// Harness is the harness identifier that failed (e.g. "ghcp-cli").
	Harness string

	// Executable is the path or command name that could not be launched.
	Executable string

	// Err is the underlying error from the spawner, preserved for identity
	// checks. Never nil.
	Err error
}

// Error returns a human-readable message naming the harness and the
// executable that could not be started.
func (e *HarnessLaunchError) Error() string {
	return fmt.Sprintf("harness %q: could not launch %q: %v", e.Harness, e.Executable, e.Err)
}

// Unwrap returns both the class sentinel and the underlying cause so that
// errors.Is matches ErrHarnessLaunchFailed and the spawner's own sentinel
// (e.g. mosaic-common/harness.ErrExecutableNotFound).
func (e *HarnessLaunchError) Unwrap() []error { return []error{ErrHarnessLaunchFailed, e.Err} }

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

// RowNotInGroupError is returned at the routing decision when the current
// row index does not fall within any execution group of the resolved stage.
// This is a mid-run routing-decision failure, analogous to
// UnresolvableApproachError: it is discovered after admission succeeded
// and is never recovered from.
//
// It replaces the previous silent sentinel (RowIndex: -1, nil error) that
// caused an unguarded negative-index slice access in the caller.
//
// The Stage field reflects whichever stage's groups were being searched at
// the point of failure. This may be the current stage (initial lookup) or
// the next-stage fallback (recursive orderedGroupsForStage call). Tests
// should cover both call sites.
type RowNotInGroupError struct {
	WorkflowID WorkflowID
	RowIndex   int
	Stage      StageNumber
	Groups     []ExecutionGroup // the resolved groups that were searched
}

func (e *RowNotInGroupError) Error() string {
	groupDescs := make([]string, len(e.Groups))
	for i, g := range e.Groups {
		if g.Name != "" {
			groupDescs[i] = fmt.Sprintf("%s[%d,%d)", string(g.Name), g.StartRow, g.EndRow)
		} else {
			groupDescs[i] = fmt.Sprintf("[%d,%d)", g.StartRow, g.EndRow)
		}
	}
	return fmt.Sprintf(
		"stage %d: row %d is not in any execution group of workflow %q (groups: %s)",
		e.Stage, e.RowIndex, e.WorkflowID, strings.Join(groupDescs, ", "),
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
