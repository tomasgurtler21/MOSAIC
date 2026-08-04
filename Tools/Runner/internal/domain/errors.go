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
