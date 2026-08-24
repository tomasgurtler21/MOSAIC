package intercept_test

// Tests for {run_id} placeholder expansion in stub response content and
// generic response content inside intercept.Decide (T3.3).
//
// Decide is a pure function: the expansion must happen inside it, before
// result.Stub.Response or result.GenericResponse is used to build both
// Outcome.StubResponse and Delta.AddPending[token].Expected. This is
// critical because AddPending is committed to persistent state inside a
// locked State.Update closure before the interceptor shell regains control;
// any expansion done after Decide returns would leave the persisted Expected
// unexpanded, causing permanent ECHO_MISMATCH.
//
// RED phase: all tests below fail until I3.3 is implemented — Decide does
// not yet use in.RunID to expand response content.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

// registryWithRunIDInResponse returns a stub registry whose sole stub
// response body contains the {run_id} placeholder at every realistic
// location a test author might write it.
func registryWithRunIDInResponse(id domain.CollaboratorIdentity) domain.StubRegistry {
	return domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage3-runid-response",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match: domain.StubMatch{Identity: id, Invocation: 1},
			// The response carries {run_id} in a field the subject would
			// naturally reference — an artifact path rooted at the run's
			// orchestration directory.
			Response: json.RawMessage(
				`{"artifact_path":"Orchestration-` + domain.RunIDPlaceholder + `/Plan.md","run_id":"` + domain.RunIDPlaceholder + `"}`,
			),
		}},
	}
}

// TestDecide_StubResponseWithRunIDPlaceholder_OutcomeContainsExpandedValue
// asserts that when Input.RunID is set and the matched stub's Response field
// contains {run_id}, the returned Decision.Outcome.StubResponse has the
// placeholder replaced by the actual run ID.
func TestDecide_StubResponseWithRunIDPlaceholder_OutcomeContainsExpandedValue(t *testing.T) {
	id := researcherIdentity()
	const runID = "20260823T210751Z-a854"

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registryWithRunIDInResponse(id),
		Now:      time.Now(),
		RunID:    runID,
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	stubResp := string(decision.Outcome.StubResponse)
	if strings.Contains(stubResp, domain.RunIDPlaceholder) {
		t.Errorf("Outcome.StubResponse still contains literal placeholder %q — it must be expanded to %q; got: %s",
			domain.RunIDPlaceholder, runID, stubResp)
	}
	if !strings.Contains(stubResp, runID) {
		t.Errorf("Outcome.StubResponse does not contain the actual run ID %q; got: %s", runID, stubResp)
	}
}

// TestDecide_StubResponseWithRunIDPlaceholder_PendingExpectedContainsExpandedValue
// asserts that Delta.AddPending[token].Expected also has {run_id} expanded —
// it must carry the same expanded value as Outcome.StubResponse, because
// the echo check at post-invocation compares the subject's reply against
// PendingStub.Expected.
func TestDecide_StubResponseWithRunIDPlaceholder_PendingExpectedContainsExpandedValue(t *testing.T) {
	id := researcherIdentity()
	const runID = "20260823T210751Z-a854"
	const token = "corr-token-1"

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registryWithRunIDInResponse(id),
		Now:      time.Now(),
		RunID:    runID,
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	pending, ok := decision.Delta.AddPending[token]
	if !ok {
		t.Fatalf("Delta.AddPending does not contain an entry for token %q", token)
	}

	expected := string(pending.Expected)
	if strings.Contains(expected, domain.RunIDPlaceholder) {
		t.Errorf("Delta.AddPending[%q].Expected still contains literal placeholder %q — it must be expanded to %q; got: %s",
			token, domain.RunIDPlaceholder, runID, expected)
	}
	if !strings.Contains(expected, runID) {
		t.Errorf("Delta.AddPending[%q].Expected does not contain the actual run ID %q; got: %s",
			token, runID, expected)
	}
}

// TestDecide_StubResponseWithRunIDPlaceholder_OutcomeAndPendingCarryIdenticalValue
// asserts that Outcome.StubResponse and Delta.AddPending[token].Expected are
// byte-identical after expansion. Both are constructed from the same
// already-expanded source, so divergence would indicate the expansion was
// applied to only one of the two fields — a state inconsistency that causes
// permanent ECHO_MISMATCH.
func TestDecide_StubResponseWithRunIDPlaceholder_OutcomeAndPendingCarryIdenticalValue(t *testing.T) {
	id := researcherIdentity()
	const runID = "20260823T210751Z-a854"
	const token = "corr-token-1"

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registryWithRunIDInResponse(id),
		Now:      time.Now(),
		RunID:    runID,
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	pending, ok := decision.Delta.AddPending[token]
	if !ok {
		t.Fatalf("Delta.AddPending does not contain an entry for token %q", token)
	}

	outcomeVal := string(decision.Outcome.StubResponse)
	pendingVal := string(pending.Expected)
	if outcomeVal != pendingVal {
		t.Errorf("Outcome.StubResponse (%s) != Delta.AddPending[%q].Expected (%s) — both must carry the same expanded value, constructed from the same expanded source before state commit",
			outcomeVal, token, pendingVal)
	}
}

// TestDecide_StubResponseWithMultipleRunIDOccurrences_AllAreExpanded asserts
// that every occurrence of {run_id} in the stub response is expanded, not
// only the first.
func TestDecide_StubResponseWithMultipleRunIDOccurrences_AllAreExpanded(t *testing.T) {
	id := researcherIdentity()
	const runID = "20260823T210751Z-a854"

	// Three occurrences: one in a field value, one in a path, one in an ID.
	multiResponse := json.RawMessage(
		`{"run_id":"` + domain.RunIDPlaceholder + `","path":"Orchestration-` + domain.RunIDPlaceholder + `/Plan.md","log_dir":"OrchestrationLogs/` + domain.RunIDPlaceholder + `"}`,
	)
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage3-multi-runid-response",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: multiResponse,
		}},
	}

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registry,
		Now:      time.Now(),
		RunID:    runID,
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	stubResp := string(decision.Outcome.StubResponse)
	if strings.Contains(stubResp, domain.RunIDPlaceholder) {
		t.Errorf("Outcome.StubResponse still contains the placeholder after expansion — all occurrences must be replaced; got: %s", stubResp)
	}
	occurrences := strings.Count(stubResp, runID)
	if occurrences != 3 {
		t.Errorf("Outcome.StubResponse contains the run ID %d time(s), want 3 (one per placeholder occurrence); got: %s",
			occurrences, stubResp)
	}
}

// TestDecide_StubResponseWithoutRunIDPlaceholder_IsUnchanged asserts that
// a stub response that does not contain {run_id} is returned byte-identical
// to what was declared — expansion is a no-op when the placeholder is absent.
func TestDecide_StubResponseWithoutRunIDPlaceholder_IsUnchanged(t *testing.T) {
	id := researcherIdentity()
	const runID = "20260823T210751Z-a854"
	const responseBody = `{"status_code":"SUCCESS","run_id":"literal-not-a-placeholder"}`

	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage3-no-placeholder-response",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: json.RawMessage(responseBody),
		}},
	}

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registry,
		Now:      time.Now(),
		RunID:    runID,
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	stubResp := string(decision.Outcome.StubResponse)
	if stubResp != responseBody {
		t.Errorf("Outcome.StubResponse = %s, want byte-identical declared response %s — placeholder-free responses must be left unchanged",
			stubResp, responseBody)
	}
}

// TestDecide_EmptyRunID_StubResponseIsUnchanged asserts that when RunID is
// empty, no expansion is performed — the response is returned as declared.
// An empty RunID indicates the run-state read did not succeed; the response
// must still be delivered rather than failing the call.
func TestDecide_EmptyRunID_StubResponseIsUnchanged(t *testing.T) {
	id := researcherIdentity()
	responseWithPlaceholder := json.RawMessage(
		`{"artifact_path":"Orchestration-` + domain.RunIDPlaceholder + `/Plan.md"}`,
	)
	registry := domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "stage3-empty-runid",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{{
			Match:    domain.StubMatch{Identity: id, Invocation: 1},
			Response: responseWithPlaceholder,
		}},
	}

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registry,
		Now:      time.Now(),
		RunID:    "", // empty — expansion disabled
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	stubResp := string(decision.Outcome.StubResponse)
	if stubResp != string(responseWithPlaceholder) {
		t.Errorf("Outcome.StubResponse = %s, want the declared response unchanged %s — empty RunID must disable expansion",
			stubResp, responseWithPlaceholder)
	}
}

// TestDecide_GenericResponseWithRunIDPlaceholder_IsExpanded asserts that the
// generic-response path (UnmatchedGenericResponse policy) also has {run_id}
// expanded in both Outcome.StubResponse and Delta.AddPending[token].Expected.
// The expansion contract applies to all substituted response content, not
// only matched-stub responses.
func TestDecide_GenericResponseWithRunIDPlaceholder_IsExpanded(t *testing.T) {
	id := researcherIdentity()
	const runID = "20260823T210751Z-a854"
	const token = "corr-token-1"

	genericResponse := json.RawMessage(
		`{"status_code":"SUCCESS","run_id":"` + domain.RunIDPlaceholder + `"}`,
	)
	// The registry has no stubs, so the generic-response policy applies.
	registry := domain.StubRegistry{
		SchemaVersion:   1,
		TestID:          "stage3-generic-response-runid",
		OnUnmatched:     domain.UnmatchedGenericResponse,
		GenericResponse: genericResponse,
	}

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registry,
		Now:      time.Now(),
		RunID:    runID,
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	// Outcome.StubResponse must have the placeholder expanded.
	stubResp := string(decision.Outcome.StubResponse)
	if strings.Contains(stubResp, domain.RunIDPlaceholder) {
		t.Errorf("generic-response path: Outcome.StubResponse still contains placeholder %q — must be expanded to %q; got: %s",
			domain.RunIDPlaceholder, runID, stubResp)
	}
	if !strings.Contains(stubResp, runID) {
		t.Errorf("generic-response path: Outcome.StubResponse does not contain the actual run ID %q; got: %s", runID, stubResp)
	}

	// Delta.AddPending[token].Expected must carry the same expanded value.
	pending, ok := decision.Delta.AddPending[token]
	if !ok {
		t.Fatalf("Delta.AddPending does not contain an entry for token %q", token)
	}
	expectedVal := string(pending.Expected)
	if strings.Contains(expectedVal, domain.RunIDPlaceholder) {
		t.Errorf("generic-response path: Delta.AddPending[%q].Expected still contains placeholder %q — must be expanded to %q; got: %s",
			token, domain.RunIDPlaceholder, runID, expectedVal)
	}
	if !strings.Contains(expectedVal, runID) {
		t.Errorf("generic-response path: Delta.AddPending[%q].Expected does not contain the actual run ID %q; got: %s",
			token, runID, expectedVal)
	}
}

// TestDecide_GenericResponseWithRunIDPlaceholder_OutcomeAndPendingMatch asserts
// that the generic-response path produces matching expanded values in both
// Outcome.StubResponse and Delta.AddPending[token].Expected, confirming they
// are derived from the same expanded source.
func TestDecide_GenericResponseWithRunIDPlaceholder_OutcomeAndPendingMatch(t *testing.T) {
	id := researcherIdentity()
	const runID = "20260823T210751Z-a854"
	const token = "corr-token-1"

	genericResponse := json.RawMessage(
		`{"status_code":"SUCCESS","path":"Orchestration-` + domain.RunIDPlaceholder + `/"}`,
	)
	registry := domain.StubRegistry{
		SchemaVersion:   1,
		TestID:          "stage3-generic-match",
		OnUnmatched:     domain.UnmatchedGenericResponse,
		GenericResponse: genericResponse,
	}

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registry,
		Now:      time.Now(),
		RunID:    runID,
	}

	decision, err := intercept.Decide(in)
	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}

	pending, ok := decision.Delta.AddPending[token]
	if !ok {
		t.Fatalf("Delta.AddPending does not contain an entry for token %q", token)
	}

	outcomeVal := string(decision.Outcome.StubResponse)
	pendingVal := string(pending.Expected)
	if outcomeVal != pendingVal {
		t.Errorf("generic-response path: Outcome.StubResponse (%s) != Delta.AddPending[%q].Expected (%s) — both must be identical expanded values",
			outcomeVal, token, pendingVal)
	}
}
