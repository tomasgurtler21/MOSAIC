package domain

import "fmt"

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
