package intercept_test

// Tests for the pure interception decision core: Decide, EchoInstruction and
// CompareEcho.
//
// Coverage follows:
//   - T6.2 / AC6.3: capability-driven outcome selection (substitute /
//     rewrite / passthrough / halt), with the decisive property that
//     flipping only HarnessCapabilities.SupportsDirectSubstitution flips the
//     outcome kind for otherwise identical input.
//   - T6.3 / AC6.5: the decision's state delta, log records and side
//     effects are returned as data, not performed.
//   - T6.4 / AC6.7: the protocol-extraction-failure escalation ladder.
//   - T6.5 / AC6.6: the echo instruction carries the stub response verbatim
//     and reveals no test vocabulary.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/intercept"
)

func researcherIdentity() domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
}

func baseState() domain.RunState {
	return domain.RunState{
		SchemaVersion:        1,
		TestID:               "example",
		RunNumber:            1,
		SequenceCounter:      0,
		CollaboratorCounters: map[string]int{},
		PendingStubs:         map[string]domain.PendingStub{},
		InFlight:             map[string]domain.InFlight{},
	}
}

func registryWithOneStub(id domain.CollaboratorIdentity, response string, effects []domain.FileEffect) domain.StubRegistry {
	return domain.StubRegistry{
		SchemaVersion: 1,
		TestID:        "example",
		OnUnmatched:   domain.UnmatchedHalt,
		Stubs: []domain.Stub{
			{
				Match:       domain.StubMatch{Identity: id, Invocation: 1},
				Response:    json.RawMessage(response),
				SideEffects: effects,
			},
		},
	}
}

func baseCall(id domain.CollaboratorIdentity, capabilities domain.HarnessCapabilities) domain.InterceptedCall {
	return domain.InterceptedCall{
		Phase:            domain.PhasePre,
		Identity:         id,
		Message:          domain.TaskMessage{AgentInstanceID: "researcher#1", TaskDescription: "do work", Extraction: domain.ExtractionParsed},
		CorrelationToken: "corr-token-1",
		Capabilities:     capabilities,
	}
}

// --- T6.2 / AC6.3: capability-driven outcome selection ---

func TestDecide_StubbedCall_WithDirectSubstitution_YieldsSubstitute(t *testing.T) {
	id := researcherIdentity()
	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomeSubstitute {
		t.Fatalf("Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomeSubstitute)
	}
	if string(decision.Outcome.StubResponse) != `{"status_code":"SUCCESS"}` {
		t.Errorf("StubResponse = %s, want the matched stub's response verbatim", decision.Outcome.StubResponse)
	}
}

func TestDecide_StubbedCall_WithoutDirectSubstitution_YieldsRewritePrompt(t *testing.T) {
	id := researcherIdentity()
	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: false}),
		State:    baseState(),
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomeRewritePrompt {
		t.Fatalf("Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomeRewritePrompt)
	}
	if decision.Outcome.RewrittenPrompt == "" {
		t.Error("expected a non-empty RewrittenPrompt on the rewrite path")
	}
}

// TestDecide_FlippingOnlyTheCapabilityFlag_FlipsTheOutcomeKind is the
// decisive property named in AC6.3: the substitute-versus-rewrite choice is
// driven only by the capability flag, never by a harness name. Everything
// else about the two inputs below is identical.
func TestDecide_FlippingOnlyTheCapabilityFlag_FlipsTheOutcomeKind(t *testing.T) {
	id := researcherIdentity()
	registry := registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil)
	now := time.Now()

	withSubstitution, err := intercept.Decide(intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registry,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("Decide (flag=true) returned unexpected error: %v", err)
	}

	withoutSubstitution, err := intercept.Decide(intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: false}),
		State:    baseState(),
		Registry: registry,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("Decide (flag=false) returned unexpected error: %v", err)
	}

	if withSubstitution.Outcome.Kind != domain.OutcomeSubstitute {
		t.Errorf("with the flag true: Outcome.Kind = %q, want %q", withSubstitution.Outcome.Kind, domain.OutcomeSubstitute)
	}
	if withoutSubstitution.Outcome.Kind != domain.OutcomeRewritePrompt {
		t.Errorf("with the flag false: Outcome.Kind = %q, want %q", withoutSubstitution.Outcome.Kind, domain.OutcomeRewritePrompt)
	}
	if withSubstitution.Outcome.Kind == withoutSubstitution.Outcome.Kind {
		t.Fatal("flipping only SupportsDirectSubstitution did not flip the outcome kind")
	}
}

func TestDecide_UnstubbedCall_UnderPassthroughPolicy_YieldsPassthrough(t *testing.T) {
	id := researcherIdentity()
	registry := registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil)
	registry.OnUnmatched = domain.UnmatchedPassthrough

	other := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "library-researcher"}
	in := intercept.Input{
		Call:     baseCall(other, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registry,
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Fatalf("Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

func TestDecide_UnmatchedCall_UnderHaltingPolicy_YieldsHaltUnmatched(t *testing.T) {
	id := researcherIdentity()
	registry := registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil)
	registry.OnUnmatched = domain.UnmatchedHalt

	other := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "library-researcher"}
	in := intercept.Input{
		Call:     baseCall(other, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registry,
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomeHalt {
		t.Fatalf("Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomeHalt)
	}
	if decision.Outcome.HaltReason != domain.HaltUnmatched {
		t.Errorf("HaltReason = %q, want %q", decision.Outcome.HaltReason, domain.HaltUnmatched)
	}
}

// TestDecide_EarlyExitThreshold_PreInvocationNoLongerHalts documents that
// the pre-invocation phase does NOT halt when the early-exit threshold has
// been reached. Stage 2 moved the cutoff to the Nth reply's completion phase;
// the (N+1)th pre-invocation call is never refused. See cutoff_test.go for
// the full suite of cutoff-specific cases.
func TestDecide_EarlyExitThreshold_PreInvocationNoLongerHalts(t *testing.T) {
	id := researcherIdentity()
	state := baseState()
	state.EarlyExitThreshold = 3
	state.SequenceCounter = 3 // threshold already reached

	// A call that would previously have halted at the pre-invocation point
	// must now proceed normally: the stub is matched, the outcome is
	// OutcomeSubstitute, and no HaltEarlyExit is produced.
	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    state,
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind == domain.OutcomeHalt && decision.Outcome.HaltReason == domain.HaltEarlyExit {
		t.Fatalf("Outcome is HaltEarlyExit: the pre-invocation phase must not halt for calls at or beyond the threshold; the cutoff fires at the Nth reply's completion, not here")
	}
}

// --- T6.3 / AC6.5: state delta, log records and side effects as data ---

func TestDecide_Delta_IncrementsSequenceAndTheCallsOwnCollaboratorCounter(t *testing.T) {
	id := researcherIdentity()
	state := baseState()
	state.SequenceCounter = 4
	state.CollaboratorCounters[id.Key()] = 1 // one prior invocation of this identity

	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    state,
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Delta.SequenceIncrement != 1 {
		t.Errorf("Delta.SequenceIncrement = %d, want 1", decision.Delta.SequenceIncrement)
	}
	if got := decision.Delta.CollaboratorIncrements[id.Key()]; got != 1 {
		t.Errorf("Delta.CollaboratorIncrements[%q] = %d, want 1", id.Key(), got)
	}
}

func TestDecide_Delta_RecordsTheCallInFlight(t *testing.T) {
	id := researcherIdentity()
	call := baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true})
	in := intercept.Input{
		Call:     call,
		State:    baseState(),
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	inFlight, ok := decision.Delta.MarkInFlight[call.CorrelationToken]
	if !ok {
		t.Fatalf("Delta.MarkInFlight does not carry an entry for correlation token %q: %+v", call.CorrelationToken, decision.Delta.MarkInFlight)
	}
	if inFlight.Identity != id {
		t.Errorf("MarkInFlight entry Identity = %+v, want %+v", inFlight.Identity, id)
	}
}

func TestDecide_Records_DescribeTheInvocation(t *testing.T) {
	id := researcherIdentity()
	state := baseState()
	state.TestID = "example-test"
	state.RunNumber = 2
	call := baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true})
	in := intercept.Input{
		Call:     call,
		State:    state,
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if len(decision.Records) == 0 {
		t.Fatal("expected at least one log record describing the invocation, got none")
	}
	found := false
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordStart && rec.Identity == id && rec.TestID == "example-test" && rec.RunNumber == 2 {
			found = true
			if rec.Outcome != decision.Outcome.Kind {
				t.Errorf("start record Outcome = %q, want the decision's own outcome %q", rec.Outcome, decision.Outcome.Kind)
			}
		}
	}
	if !found {
		t.Errorf("no start record matched identity %+v, test_id %q, run_number %d: got %+v", id, "example-test", 2, decision.Records)
	}
}

func TestDecide_SideEffects_AreExactlyThoseTheMatchedStubDeclared(t *testing.T) {
	id := researcherIdentity()
	effects := []domain.FileEffect{
		{Path: "notes/plan.md", Content: "seeded content"},
		{Path: "notes/ref.md", Ref: "fixtures/ref.md"},
	}
	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, effects),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if len(decision.SideEffects) != len(effects) {
		t.Fatalf("len(SideEffects) = %d, want %d: got %+v", len(decision.SideEffects), len(effects), decision.SideEffects)
	}
	for i, want := range effects {
		if decision.SideEffects[i] != want {
			t.Errorf("SideEffects[%d] = %+v, want %+v", i, decision.SideEffects[i], want)
		}
	}
}

func TestDecide_NoDeclaredSideEffects_ReturnsNoneToApply(t *testing.T) {
	id := researcherIdentity()
	in := intercept.Input{
		Call:     baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true}),
		State:    baseState(),
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if len(decision.SideEffects) != 0 {
		t.Errorf("expected no side effects when the matched stub declares none, got %+v", decision.SideEffects)
	}
}

// --- T6.4 / AC6.7: protocol-extraction-failure escalation ladder ---

func TestDecide_ExtractionRecovered_ProceedsNormallyAndRecordsTheRecovery(t *testing.T) {
	id := researcherIdentity()
	call := baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true})
	call.Message.Extraction = domain.ExtractionRecovered
	in := intercept.Input{
		Call:     call,
		State:    baseState(),
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomeSubstitute {
		t.Errorf("a recovered extraction must still decide normally: Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomeSubstitute)
	}
	found := false
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordRun && rec.Event == domain.RunEventExtractionRecovered {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a run record noting the recovery, got %+v", decision.Records)
	}
}

func TestDecide_ExtractionDegraded_IdentityDeterminable_ProceedsAgainstEmptyMessageWithErrorRecord(t *testing.T) {
	id := researcherIdentity()
	call := baseCall(id, domain.HarnessCapabilities{SupportsDirectSubstitution: true})
	call.Message = domain.TaskMessage{Extraction: domain.ExtractionDegraded} // no fields recovered
	in := intercept.Input{
		Call:     call,
		State:    baseState(),
		Registry: registryWithOneStub(id, `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	// Matching is by identity and ordinal, never by message content, so a
	// degraded extraction must not prevent the identity from being
	// recognised and matched normally.
	if decision.Outcome.Kind != domain.OutcomeSubstitute {
		t.Errorf("a degraded extraction with a determinable identity must still decide normally: Outcome.Kind = %q, want %q", decision.Outcome.Kind, domain.OutcomeSubstitute)
	}
	if decision.Outcome.Kind == domain.OutcomeHalt {
		t.Fatal("a degraded extraction must not silently pass through or halt on identity grounds alone — it degrades, it does not fail")
	}
	found := false
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error record for the degraded extraction, got %+v", decision.Records)
	}
}

func TestDecide_ExtractionFailed_IdentityNotDeterminable_HaltsWithExtractionFailed(t *testing.T) {
	call := domain.InterceptedCall{
		Phase:            domain.PhasePre,
		Identity:         domain.CollaboratorIdentity{}, // not determinable
		Message:          domain.TaskMessage{Extraction: domain.ExtractionFailed},
		CorrelationToken: "corr-token-failed",
		Capabilities:     domain.HarnessCapabilities{SupportsDirectSubstitution: true},
	}
	in := intercept.Input{
		Call:     call,
		State:    baseState(),
		Registry: registryWithOneStub(researcherIdentity(), `{"status_code":"SUCCESS"}`, nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomeHalt {
		t.Fatalf("Outcome.Kind = %q, want %q — an undeterminable identity must never be silently guessed or passed through", decision.Outcome.Kind, domain.OutcomeHalt)
	}
	if decision.Outcome.HaltReason != domain.HaltExtractionFailed {
		t.Errorf("HaltReason = %q, want %q", decision.Outcome.HaltReason, domain.HaltExtractionFailed)
	}
	found := false
	for _, rec := range decision.Records {
		if rec.Kind == domain.RecordError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an error record for the failed extraction, got %+v", decision.Records)
	}
}

// --- T6.5 / AC6.6: the echo instruction ---

func TestEchoInstruction_CarriesTheStubResponseVerbatim(t *testing.T) {
	stubResponse := json.RawMessage(`{"status_code":"SUCCESS","note":"exact payload"}`)

	instruction := intercept.EchoInstruction(stubResponse)

	if !strings.Contains(instruction, string(stubResponse)) {
		t.Fatalf("EchoInstruction does not carry the stub response verbatim: instruction=%q, response=%q", instruction, stubResponse)
	}
}

// TestEchoInstruction_ContainsNoTestRevealingVocabulary is the transparency
// obligation named in T6.5: the subject must not be able to tell it is
// being tested from the text of the instruction it is handed.
func TestEchoInstruction_ContainsNoTestRevealingVocabulary(t *testing.T) {
	stubResponse := json.RawMessage(`{"status_code":"SUCCESS"}`)

	instruction := intercept.EchoInstruction(stubResponse)

	lower := strings.ToLower(instruction)
	banned := []string{"test", "stub", "mock", "fixture", "fake", "harness", "assert", "verdict", "mosaic-agent-test"}
	for _, word := range banned {
		if strings.Contains(lower, word) {
			t.Errorf("EchoInstruction contains test-revealing vocabulary %q: %q", word, instruction)
		}
	}
}

func TestCompareEcho_ObservedMatchesExpected_Verbatim_ReportsMatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS","n":1}`)

	outcome := intercept.CompareEcho(expected, `{"status_code":"SUCCESS","n":1}`)

	if !outcome.Match {
		t.Fatalf("expected a verbatim echo to match, got Match=false, Diff=%q", outcome.Diff)
	}
}

func TestCompareEcho_ObservedDiffersOnlyByKeyOrderAndWhitespace_StillReportsMatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS","n":1}`)

	outcome := intercept.CompareEcho(expected, `{
  "n": 1,
  "status_code": "SUCCESS"
}`)

	if !outcome.Match {
		t.Fatalf("expected key order and whitespace to be insignificant, got Match=false, Diff=%q", outcome.Diff)
	}
}

func TestCompareEcho_ObservedWrappedInSurroundingProse_ReportsMismatch(t *testing.T) {
	// Surrounding prose in the observed text is a mismatch, not a tolerated
	// wrapper — a collaborator that echoes the JSON but adds commentary
	// around it did not echo faithfully.
	expected := json.RawMessage(`{"status_code":"SUCCESS","n":1}`)

	outcome := intercept.CompareEcho(expected, `Sure, here is the result: {"status_code":"SUCCESS","n":1}`)

	if outcome.Match {
		t.Fatal("expected surrounding prose to be reported as a mismatch, got Match=true")
	}
}

func TestCompareEcho_ObservedDiffersInValue_ReportsMismatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS","n":1}`)

	outcome := intercept.CompareEcho(expected, `{"status_code":"SUCCESS","n":2}`)

	if outcome.Match {
		t.Fatal("expected a differing value to be reported as a mismatch, got Match=true")
	}
}

// --- Post-invocation phase: recovering the pending stub, comparing echo
// fidelity, producing an end record, clearing the in-flight entry, and
// always resolving to OutcomePassthrough regardless of the pre-invocation
// halt conditions (early-exit threshold, unmatched policy). ---

// postState returns a run state with one pending stub and one in-flight
// entry for token, as a pre-invocation call for id would have left behind.
func postState(token string, id domain.CollaboratorIdentity, expected json.RawMessage) domain.RunState {
	state := baseState()
	state.PendingStubs[token] = domain.PendingStub{Seq: 1, Identity: id, Expected: expected}
	state.InFlight[token] = domain.InFlight{Seq: 1, Identity: id, StartedAt: time.Now()}
	return state
}

func postCall(id domain.CollaboratorIdentity, token, observedResponse string) domain.InterceptedCall {
	return domain.InterceptedCall{
		Phase:            domain.PhasePost,
		Identity:         id,
		CorrelationToken: token,
		ObservedResponse: observedResponse,
	}
}

// endRecordFor returns the end record carrying the given correlation token,
// if any.
func endRecordFor(records []domain.LogRecord, token string) *domain.LogRecord {
	for i := range records {
		if records[i].Kind == domain.RecordEnd && records[i].CorrelationToken == token {
			return &records[i]
		}
	}
	return nil
}

func TestDecide_PostInvocation_FaithfulEcho_YieldsPassthroughWithMatchInEndRecord(t *testing.T) {
	id := researcherIdentity()
	token := "corr-token-post-match"
	expected := json.RawMessage(`{"status_code":"SUCCESS","n":1}`)

	in := intercept.Input{
		Call:     postCall(id, token, `{"status_code":"SUCCESS","n":1}`),
		State:    postState(token, id, expected),
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Fatalf("Outcome.Kind = %q, want %q on the post-invocation path", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
	rec := endRecordFor(decision.Records, token)
	if rec == nil {
		t.Fatalf("expected an end record for correlation token %q, got %+v", token, decision.Records)
	}
	if rec.Echo == nil || !rec.Echo.Match {
		t.Errorf("end record Echo = %+v, want a match for a faithful echo", rec.Echo)
	}
}

func TestDecide_PostInvocation_MismatchedEcho_StillYieldsPassthroughButEndRecordCarriesMismatch(t *testing.T) {
	// A mismatch is recorded as evidence, never as a halt: the collaborator
	// has already run by the post-invocation point, so refusing it can only
	// damage the subject's run.
	id := researcherIdentity()
	token := "corr-token-post-mismatch"
	expected := json.RawMessage(`{"status_code":"SUCCESS","n":1}`)

	in := intercept.Input{
		Call:     postCall(id, token, `{"status_code":"SUCCESS","n":2}`),
		State:    postState(token, id, expected),
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Fatalf("Outcome.Kind = %q, want %q — a mismatched echo must never halt post-invocation", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
	rec := endRecordFor(decision.Records, token)
	if rec == nil {
		t.Fatalf("expected an end record for correlation token %q, got %+v", token, decision.Records)
	}
	if rec.Echo == nil || rec.Echo.Match {
		t.Errorf("end record Echo = %+v, want the mismatch reported as evidence", rec.Echo)
	}
}

func TestDecide_PostInvocation_ClearsTheInFlightEntryForTheCorrelationToken(t *testing.T) {
	id := researcherIdentity()
	token := "corr-token-post-clear"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	in := intercept.Input{
		Call:     postCall(id, token, `{"status_code":"SUCCESS"}`),
		State:    postState(token, id, expected),
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	found := false
	for _, cleared := range decision.Delta.ClearInFlight {
		if cleared == token {
			found = true
		}
	}
	if !found {
		t.Errorf("Delta.ClearInFlight = %v, want it to contain %q", decision.Delta.ClearInFlight, token)
	}
}

func TestDecide_PostInvocation_UnaffectedByEarlyExitThreshold_StillYieldsPassthrough(t *testing.T) {
	id := researcherIdentity()
	token := "corr-token-post-early-exit"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	state := postState(token, id, expected)
	state.EarlyExitThreshold = 1
	state.SequenceCounter = 1 // threshold already reached, which would halt a pre-invocation call

	in := intercept.Input{
		Call:     postCall(id, token, `{"status_code":"SUCCESS"}`),
		State:    state,
		Registry: registryWithOneStub(id, string(expected), nil),
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Fatalf("Outcome.Kind = %q, want %q — the early-exit threshold must not reach the post-invocation phase", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}

func TestDecide_PostInvocation_UnaffectedByUnmatchedPolicy_StillYieldsPassthrough(t *testing.T) {
	// The post-invocation phase resolves by recovering the pending stub from
	// state, not by matching against the registry, so a halting unmatched
	// policy must not reach it even when the call's identity has no stub.
	id := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "no-stub-for-this-identity"}
	token := "corr-token-post-unmatched-policy"
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)

	registry := registryWithOneStub(researcherIdentity(), `{"status_code":"SUCCESS"}`, nil)
	registry.OnUnmatched = domain.UnmatchedHalt

	in := intercept.Input{
		Call:     postCall(id, token, `{"status_code":"SUCCESS"}`),
		State:    postState(token, id, expected),
		Registry: registry,
		Now:      time.Now(),
	}

	decision, err := intercept.Decide(in)

	if err != nil {
		t.Fatalf("Decide returned unexpected error: %v", err)
	}
	if decision.Outcome.Kind != domain.OutcomePassthrough {
		t.Fatalf("Outcome.Kind = %q, want %q — a halting unmatched policy must not reach the post-invocation phase", decision.Outcome.Kind, domain.OutcomePassthrough)
	}
}
