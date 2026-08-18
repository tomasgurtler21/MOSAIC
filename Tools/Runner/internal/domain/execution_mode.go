package domain

import (
	"fmt"
	"strings"
)

// ExecutionMode selects how much routing autonomy the deterministic engine
// has. It is required at run start, immutable for the run, and persisted in
// the artifact frontmatter.
type ExecutionMode string

const (
	// ExecutionModeUnset is the zero value. It is never a valid run
	// configuration: an unspecified mode is a refusal, never a silent default.
	ExecutionModeUnset        ExecutionMode = ""
	ExecutionModeOrchestrated ExecutionMode = "orchestrated"
	ExecutionModeAuto         ExecutionMode = "auto"
	ExecutionModeAutoReview   ExecutionMode = "auto-review"
)

// ExecutionModes returns the valid modes in presentation order. Callers use it
// to build selection prompts and to list valid values in error messages.
func ExecutionModes() []ExecutionMode {
	return []ExecutionMode{
		ExecutionModeOrchestrated,
		ExecutionModeAuto,
		ExecutionModeAutoReview,
	}
}

// ParseExecutionMode maps a user- or file-supplied string to an ExecutionMode.
// An empty string yields ExecutionModeUnset with a nil error — absence is not
// a parse failure, it is a condition the caller decides about. Any other
// unrecognised value yields a *RefusalError naming the offending value and
// listing the valid ones.
func ParseExecutionMode(s string) (ExecutionMode, error) {
	switch ExecutionMode(s) {
	case ExecutionModeUnset:
		return ExecutionModeUnset, nil
	case ExecutionModeOrchestrated, ExecutionModeAuto, ExecutionModeAutoReview:
		return ExecutionMode(s), nil
	}
	modes := ExecutionModes()
	names := make([]string, len(modes))
	for i, m := range modes {
		names[i] = string(m)
	}
	return ExecutionModeUnset, &RefusalError{
		Component: "domain",
		Resource:  s,
		Reason:    fmt.Sprintf("unknown execution mode; valid values: %s", strings.Join(names, ", ")),
	}
}
