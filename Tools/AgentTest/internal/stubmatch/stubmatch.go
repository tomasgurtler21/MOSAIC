// Package stubmatch resolves an intercepted call to a registry entry.
//
// Matching is a pure function of its explicit inputs: the composite
// collaborator identity plus the per-identity invocation ordinal. It imports
// domain only and performs no I/O, no clock reads and no process control.
package stubmatch

import (
	"encoding/json"

	"mosaic-agent-test/internal/domain"
)

// Result is the outcome of resolving one call against a registry.
type Result struct {
	Matched bool
	Stub    domain.Stub            // valid only when Matched
	Policy  domain.UnmatchedPolicy // the registry's declared policy, always set

	// GenericResponse is the registry's fallback payload, populated only
	// when Matched is false and Policy is UnmatchedGenericResponse.
	GenericResponse json.RawMessage
}

// Match resolves a call to a registry entry.
//
// Matching keys on the composite collaborator identity plus the
// per-identity invocation ordinal (1-based). Counters are independent per
// identity: the global sequence number is never used for matching.
//
// Match is a pure function of its explicit inputs.
func Match(reg domain.StubRegistry, id domain.CollaboratorIdentity, ordinal int) Result {
	for _, stub := range reg.Stubs {
		if stub.Match.Identity == id && stub.Match.Invocation == ordinal {
			return Result{
				Matched: true,
				Stub:    stub,
				Policy:  reg.OnUnmatched,
			}
		}
	}

	result := Result{
		Matched: false,
		Policy:  reg.OnUnmatched,
	}
	if reg.OnUnmatched == domain.UnmatchedGenericResponse {
		result.GenericResponse = reg.GenericResponse
	}
	return result
}
