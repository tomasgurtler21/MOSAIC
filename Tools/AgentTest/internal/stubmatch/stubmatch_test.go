package stubmatch_test

// Tests for stubmatch.Match: resolving an intercepted call to a registry
// entry by composite collaborator identity plus per-identity invocation
// ordinal, and the unmatched-invocation policy resolution when no entry
// matches.
//
// Coverage follows T6.1 / AC6.4: the first and second call to the same
// collaborator each pick their own entry, different collaborators keep
// independent per-identity counters (including the empty-agent-identity
// plain-tool-call case), an unmatched call resolves through the registry's
// declared policy, and Match is a pure function of its explicit inputs.

import (
	"encoding/json"
	"reflect"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/stubmatch"
)

func researcher() domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
}

func libraryResearcher() domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "library-researcher"}
}

func plainToolCall() domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: "Bash"}
}

func registryWith(stubs ...domain.Stub) domain.StubRegistry {
	return domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "example",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs:         stubs,
	}
}

func stubFor(id domain.CollaboratorIdentity, invocation int, response string) domain.Stub {
	return domain.Stub{
		Match:    domain.StubMatch{Identity: id, Invocation: invocation},
		Response: json.RawMessage(response),
	}
}

func TestMatch_FirstCallToACollaborator_MatchesTheFirstEntry(t *testing.T) {
	reg := registryWith(
		stubFor(researcher(), 1, `{"status_code":"SUCCESS","n":1}`),
		stubFor(researcher(), 2, `{"status_code":"SUCCESS","n":2}`),
	)

	result := stubmatch.Match(reg, researcher(), 1)

	if !result.Matched {
		t.Fatal("expected the first invocation to match, got Matched=false")
	}
	if string(result.Stub.Response) != `{"status_code":"SUCCESS","n":1}` {
		t.Errorf("Response = %s, want the first entry's response", result.Stub.Response)
	}
}

func TestMatch_SecondCallToTheSameCollaborator_MatchesTheSecondEntry(t *testing.T) {
	reg := registryWith(
		stubFor(researcher(), 1, `{"status_code":"SUCCESS","n":1}`),
		stubFor(researcher(), 2, `{"status_code":"SUCCESS","n":2}`),
	)

	result := stubmatch.Match(reg, researcher(), 2)

	if !result.Matched {
		t.Fatal("expected the second invocation to match, got Matched=false")
	}
	if string(result.Stub.Response) != `{"status_code":"SUCCESS","n":2}` {
		t.Errorf("Response = %s, want the second entry's response", result.Stub.Response)
	}
}

func TestMatch_DifferentCollaborators_KeepIndependentOrdinalCounters(t *testing.T) {
	// A second invocation of library-researcher must not be satisfied by
	// researcher's second entry, and vice versa: each identity's ordinal
	// sequence is independent of every other identity's.
	reg := registryWith(
		stubFor(researcher(), 1, `{"who":"researcher-1"}`),
		stubFor(libraryResearcher(), 1, `{"who":"library-researcher-1"}`),
		stubFor(libraryResearcher(), 2, `{"who":"library-researcher-2"}`),
	)

	researcherFirst := stubmatch.Match(reg, researcher(), 1)
	libFirst := stubmatch.Match(reg, libraryResearcher(), 1)
	libSecond := stubmatch.Match(reg, libraryResearcher(), 2)

	if !researcherFirst.Matched || string(researcherFirst.Stub.Response) != `{"who":"researcher-1"}` {
		t.Errorf("researcher ordinal 1: got %+v, want the researcher-1 entry", researcherFirst)
	}
	if !libFirst.Matched || string(libFirst.Stub.Response) != `{"who":"library-researcher-1"}` {
		t.Errorf("library-researcher ordinal 1: got %+v, want the library-researcher-1 entry", libFirst)
	}
	if !libSecond.Matched || string(libSecond.Stub.Response) != `{"who":"library-researcher-2"}` {
		t.Errorf("library-researcher ordinal 2: got %+v, want the library-researcher-2 entry", libSecond)
	}

	// researcher never had a second entry declared, so its own ordinal 2
	// must be unmatched rather than accidentally picking up
	// library-researcher's second entry.
	researcherSecond := stubmatch.Match(reg, researcher(), 2)
	if researcherSecond.Matched {
		t.Errorf("expected researcher ordinal 2 to be unmatched (no such entry declared), got Matched=true with %+v", researcherSecond.Stub)
	}
}

func TestMatch_PlainToolCall_EmptyAgentIdentityMatchesByToolNameAlone(t *testing.T) {
	// A future mode's plain tool call carries an empty AgentIdentity.
	// Matching must key on the composite identity even when one half is
	// empty, not treat an empty AgentIdentity as "any agent".
	reg := registryWith(
		stubFor(plainToolCall(), 1, `{"kind":"plain-tool"}`),
		stubFor(researcher(), 1, `{"kind":"agent-dispatch"}`),
	)

	result := stubmatch.Match(reg, plainToolCall(), 1)

	if !result.Matched {
		t.Fatal("expected the plain tool call to match its own entry, got Matched=false")
	}
	if string(result.Stub.Response) != `{"kind":"plain-tool"}` {
		t.Errorf("Response = %s, want the plain-tool-call entry, not the agent-dispatch entry", result.Stub.Response)
	}
}

func TestMatch_NoEntryForTheOrdinal_ResolvesThroughHaltPolicy(t *testing.T) {
	reg := registryWith(stubFor(researcher(), 1, `{"status_code":"SUCCESS"}`))
	reg.OnUnmatched = domain.UnmatchedHalt

	result := stubmatch.Match(reg, researcher(), 2)

	if result.Matched {
		t.Fatalf("expected no entry for ordinal 2, got Matched=true with %+v", result.Stub)
	}
	if result.Policy != domain.UnmatchedHalt {
		t.Errorf("Policy = %q, want %q", result.Policy, domain.UnmatchedHalt)
	}
}

func TestMatch_NoEntry_UnderGenericResponsePolicy_ReturnsTheRegistrysFallback(t *testing.T) {
	reg := registryWith(stubFor(researcher(), 1, `{"status_code":"SUCCESS"}`))
	reg.OnUnmatched = domain.UnmatchedGenericResponse
	reg.GenericResponse = json.RawMessage(`{"status_code":"SUCCESS","note":"generic"}`)

	result := stubmatch.Match(reg, libraryResearcher(), 1)

	if result.Matched {
		t.Fatalf("expected an entirely unregistered collaborator to be unmatched, got Matched=true with %+v", result.Stub)
	}
	if result.Policy != domain.UnmatchedGenericResponse {
		t.Errorf("Policy = %q, want %q", result.Policy, domain.UnmatchedGenericResponse)
	}
	if string(result.GenericResponse) != string(reg.GenericResponse) {
		t.Errorf("GenericResponse = %s, want the registry's declared fallback %s", result.GenericResponse, reg.GenericResponse)
	}
}

func TestMatch_NoEntry_UnderPassthroughPolicy_ReportsPassthroughWithNoGenericResponse(t *testing.T) {
	reg := registryWith(stubFor(researcher(), 1, `{"status_code":"SUCCESS"}`))
	reg.OnUnmatched = domain.UnmatchedPassthrough

	result := stubmatch.Match(reg, researcher(), 5)

	if result.Matched {
		t.Fatalf("expected ordinal 5 to be unmatched, got Matched=true with %+v", result.Stub)
	}
	if result.Policy != domain.UnmatchedPassthrough {
		t.Errorf("Policy = %q, want %q", result.Policy, domain.UnmatchedPassthrough)
	}
}

func TestMatch_PolicyIsAlwaysSet_EvenWhenTheCallMatches(t *testing.T) {
	reg := registryWith(stubFor(researcher(), 1, `{"status_code":"SUCCESS"}`))
	reg.OnUnmatched = domain.UnmatchedGenericResponse

	result := stubmatch.Match(reg, researcher(), 1)

	if !result.Matched {
		t.Fatal("expected a match, got Matched=false")
	}
	if result.Policy != domain.UnmatchedGenericResponse {
		t.Errorf("Policy = %q, want the registry's declared policy %q even on a match", result.Policy, domain.UnmatchedGenericResponse)
	}
}

func TestMatch_IsPure_SameInputsAlwaysYieldTheSameResult(t *testing.T) {
	reg := registryWith(
		stubFor(researcher(), 1, `{"status_code":"SUCCESS","n":1}`),
		stubFor(researcher(), 2, `{"status_code":"SUCCESS","n":2}`),
	)

	first := stubmatch.Match(reg, researcher(), 2)
	second := stubmatch.Match(reg, researcher(), 2)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Match is not pure: identical inputs produced different results:\n  first:  %+v\n  second: %+v", first, second)
	}
}
