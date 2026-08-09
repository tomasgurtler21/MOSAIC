package opencode_test

// Tests for inbound payload translation and outbound outcome translation.
// See ContractsDesign.md's "AgentTest: Translation" for the full contract.
//
// Inbound: a native pre- or post-invocation payload becomes a normalized
// domain.InterceptedCall carrying the composite collaborator identity, the
// parsed task message, the raw native payload and the adapter's capability
// flags. An unrecognised phase discriminator, a payload missing the fields
// identity is derived from, and a payload that is not valid JSON at all must
// each surface as a handleable error, never a panic and never a zero-valued
// call read as success.
//
// Outbound: every domain.InterceptionOutcome kind maps to a defined native
// reply, except OutcomeSubstitute, which this adapter's declared capability
// makes unreachable and which must fail loudly rather than emit a
// plausible-looking wrong reply. A post-phase call always passes through
// whatever outcome is supplied, because the real call has already run.
//
// TranslateCall and TranslateOutcome are still the not-implemented stubs in
// stub.go at the time these tests are written (T7.1/T7.2); every test below
// is therefore expected to fail on that stub error until the translation
// pass (I7.2/I7.3) replaces it — a valid RED per the lean-tdd skill.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
)

func payloadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "opencode", "payloads", name))
	if err != nil {
		t.Fatalf("payloadFixture: resolving %q: %v", name, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("payloadFixture: reading %q: %v", path, err)
	}
	return data
}

func newTestAdapter() *opencode.Adapter {
	return opencode.New(opencode.Options{})
}

// ---------------------------------------------------------------------------
// Inbound: TranslateCall
// ---------------------------------------------------------------------------

func TestTranslateCall_PreSubagentDispatch_ProducesCompositeIdentity(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_subagent.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	want := domain.CollaboratorIdentity{ToolName: "task", AgentIdentity: "Worker"}
	if call.Identity != want {
		t.Errorf("TranslateCall: Identity = %+v, want %+v", call.Identity, want)
	}
	if call.Phase != domain.PhasePre {
		t.Errorf("TranslateCall: Phase = %q, want %q", call.Phase, domain.PhasePre)
	}
}

func TestTranslateCall_PreSubagentDispatch_ParsesTaskMessage(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_subagent.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	if call.Message.AgentInstanceID != "Worker#1" {
		t.Errorf("TranslateCall: Message.AgentInstanceID = %q, want %q", call.Message.AgentInstanceID, "Worker#1")
	}
	if call.Message.TaskDescription != "do the work" {
		t.Errorf("TranslateCall: Message.TaskDescription = %q, want %q", call.Message.TaskDescription, "do the work")
	}
}

func TestTranslateCall_CarriesRawPayloadForDiagnostics(t *testing.T) {
	a := newTestAdapter()
	native := payloadFixture(t, "pre_valid_subagent.json")

	call, err := a.TranslateCall(domain.PhasePre, native)
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	if len(call.RawPayload) == 0 {
		t.Errorf("TranslateCall: RawPayload is empty; the native payload must be carried for diagnostics")
	}
}

func TestTranslateCall_CarriesCapabilityFlags(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_subagent.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	if call.Capabilities != a.Capabilities() {
		t.Errorf("TranslateCall: Capabilities = %+v, want the adapter's own declared capabilities %+v", call.Capabilities, a.Capabilities())
	}
}

func TestTranslateCall_Post_CapturesObservedResponse(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePost, payloadFixture(t, "post_valid.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	if call.Phase != domain.PhasePost {
		t.Errorf("TranslateCall: Phase = %q, want %q", call.Phase, domain.PhasePost)
	}
	if call.ObservedResponse == "" {
		t.Errorf("TranslateCall: ObservedResponse is empty; the post-invocation payload's output must be captured")
	}
}

func TestTranslateCall_WrongPhasePayload_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	for _, phase := range []domain.InterceptionPhase{domain.PhasePre, domain.PhasePost} {
		name := "pre_unrecognised_event.json"
		if phase == domain.PhasePost {
			name = "post_unrecognised_event.json"
		}

		call, err := a.TranslateCall(phase, payloadFixture(t, name))
		if err == nil {
			t.Errorf("TranslateCall(%s, wrong-phase payload): expected an error, got call=%+v", phase, call)
		}
	}
}

func TestTranslateCall_UnrecognisedPhase_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.InterceptionPhase("sideways"), payloadFixture(t, "pre_valid_subagent.json"))
	if err == nil {
		t.Errorf("TranslateCall(unrecognised phase): expected an error, got call=%+v", call)
	}
}

func TestTranslateCall_MissingIdentityFields_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_missing_identity.json"))
	if err == nil {
		t.Errorf("TranslateCall: expected an error for a payload missing the fields identity is derived from, got call=%+v", call)
	}
}

func TestTranslateCall_MalformedArgs_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_malformed_args.json"))
	if err == nil {
		t.Errorf("TranslateCall: expected an error for a well-formed envelope with a malformed nested args value, got call=%+v", call)
	}
}

func TestTranslateCall_WellFormedPrompt_ProducesParsedExtraction(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_subagent.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	if call.Message.Extraction != domain.ExtractionParsed {
		t.Errorf("TranslateCall: Message.Extraction = %q, want %q for a well-formed Communication Protocol prompt", call.Message.Extraction, domain.ExtractionParsed)
	}
}

func TestTranslateCall_UnparseablePrompt_ProducesDegradedExtractionAndPreservesRawText(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_unparseable_prompt.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	if call.Message.Extraction != domain.ExtractionDegraded {
		t.Errorf("TranslateCall: Message.Extraction = %q, want %q for a prompt that is present but does not parse as a Communication Protocol invocation", call.Message.Extraction, domain.ExtractionDegraded)
	}
	want := "an ordinary prompt, not a Communication Protocol invocation"
	if call.Message.Raw != want {
		t.Errorf("TranslateCall: Message.Raw = %q, want the original prompt text preserved verbatim %q", call.Message.Raw, want)
	}
}

func TestTranslateCall_MalformedPayload_IsHandleableErrorNeverPanic(t *testing.T) {
	a := newTestAdapter()

	for _, phase := range []domain.InterceptionPhase{domain.PhasePre, domain.PhasePost} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("TranslateCall(%s, malformed): panicked instead of returning an error: %v", phase, r)
				}
			}()
			call, err := a.TranslateCall(phase, payloadFixture(t, "malformed_not_json.json"))
			if err == nil {
				t.Errorf("TranslateCall(%s, malformed): expected an error, got call=%+v", phase, call)
			}
		}()
	}
}

func TestTranslateCall_EmptyPayload_IsHandleableErrorNeverPanic(t *testing.T) {
	a := newTestAdapter()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateCall(empty): panicked instead of returning an error: %v", r)
		}
	}()
	call, err := a.TranslateCall(domain.PhasePre, []byte(``))
	if err == nil {
		t.Errorf("TranslateCall(empty): expected an error, got call=%+v", call)
	}
}

// ---------------------------------------------------------------------------
// Outbound: TranslateOutcome
// ---------------------------------------------------------------------------

func preCall(t *testing.T, a *opencode.Adapter) domain.InterceptedCall {
	t.Helper()
	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_subagent.json"))
	if err != nil {
		t.Fatalf("preCall: TranslateCall: %v", err)
	}
	return call
}

func decodeHookReply(t *testing.T, native []byte) opencode.HookReply {
	t.Helper()
	var reply opencode.HookReply
	if err := json.Unmarshal(native, &reply); err != nil {
		t.Fatalf("decodeHookReply: malformed reply %q: %v", native, err)
	}
	return reply
}

func TestTranslateOutcome_RewritePrompt_AllowsWithUpdatedArgs(t *testing.T) {
	a := newTestAdapter()
	call := preCall(t, a)

	outcome := domain.InterceptionOutcome{
		Kind:             domain.OutcomeRewritePrompt,
		RewrittenPrompt:  "please respond with the scripted stub content",
		CorrelationToken: "c-some-opaque-token",
	}

	native, err := a.TranslateOutcome(outcome, call)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}

	reply := decodeHookReply(t, native)
	if reply.Decision != "allow" {
		t.Errorf("TranslateOutcome(RewritePrompt): Decision = %q, want %q", reply.Decision, "allow")
	}
	if len(reply.UpdatedArgs) == 0 {
		t.Errorf("TranslateOutcome(RewritePrompt): UpdatedArgs is empty; the rewritten call arguments must replace the tool's arguments")
	}
}

func TestTranslateOutcome_RewritePrompt_PreservesOriginalIdentityFields(t *testing.T) {
	a := newTestAdapter()
	call := preCall(t, a)

	outcome := domain.InterceptionOutcome{
		Kind:            domain.OutcomeRewritePrompt,
		RewrittenPrompt: "please respond with the scripted stub content",
	}

	native, err := a.TranslateOutcome(outcome, call)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}

	reply := decodeHookReply(t, native)
	var updated opencode.TaskToolArgs
	if err := json.Unmarshal(reply.UpdatedArgs, &updated); err != nil {
		t.Fatalf("TranslateOutcome(RewritePrompt): malformed UpdatedArgs %q: %v", reply.UpdatedArgs, err)
	}
	if updated.SubagentType != "Worker" {
		t.Errorf("TranslateOutcome(RewritePrompt): UpdatedArgs.SubagentType = %q, want the original call's %q, not invented", updated.SubagentType, "Worker")
	}
	if updated.Description != "do the work" {
		t.Errorf("TranslateOutcome(RewritePrompt): UpdatedArgs.Description = %q, want the original call's %q, not invented", updated.Description, "do the work")
	}
}

func TestTranslateOutcome_Halt_DeniesWithReason(t *testing.T) {
	a := newTestAdapter()
	call := preCall(t, a)

	outcome := domain.InterceptionOutcome{
		Kind:       domain.OutcomeHalt,
		HaltReason: domain.HaltUnmatched,
		Message:    "no stub was registered for this call",
	}

	native, err := a.TranslateOutcome(outcome, call)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}

	reply := decodeHookReply(t, native)
	if reply.Decision != "deny" {
		t.Errorf("TranslateOutcome(Halt): Decision = %q, want %q", reply.Decision, "deny")
	}
	if reply.Reason == "" {
		t.Errorf("TranslateOutcome(Halt): Reason is empty; the subject needs a plausible reason")
	}
}

func TestTranslateOutcome_Passthrough_ChangesNothing(t *testing.T) {
	a := newTestAdapter()
	call := preCall(t, a)

	outcome := domain.InterceptionOutcome{Kind: domain.OutcomePassthrough}

	native, err := a.TranslateOutcome(outcome, call)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}

	reply := decodeHookReply(t, native)
	if reply.Decision != "" {
		t.Errorf("TranslateOutcome(Passthrough): Decision = %q, want empty — passthrough must change nothing", reply.Decision)
	}
}

func TestTranslateOutcome_Substitute_IsUnsupportedAndFailsLoudly(t *testing.T) {
	a := newTestAdapter()
	call := preCall(t, a)

	outcome := domain.InterceptionOutcome{
		Kind:         domain.OutcomeSubstitute,
		StubResponse: json.RawMessage(`{"status_code":"SUCCESS"}`),
	}

	if _, err := a.TranslateOutcome(outcome, call); err == nil {
		t.Errorf("TranslateOutcome(Substitute): expected an error — this adapter cannot achieve direct substitution and must not emit a plausible-looking wrong reply")
	}
}

func TestTranslateOutcome_Post_AlwaysPassesThroughNeverDenies(t *testing.T) {
	a := newTestAdapter()

	postCall, err := a.TranslateCall(domain.PhasePost, payloadFixture(t, "post_valid.json"))
	if err != nil {
		t.Fatalf("TranslateCall(post): %v", err)
	}

	// Even an outcome that would deny at the pre-invocation point must never
	// deny once the real call has already executed.
	outcome := domain.InterceptionOutcome{Kind: domain.OutcomeHalt, HaltReason: domain.HaltUnmatched, Message: "would have halted"}

	native, err := a.TranslateOutcome(outcome, postCall)
	if err != nil {
		t.Fatalf("TranslateOutcome(post): %v", err)
	}

	reply := decodeHookReply(t, native)
	if reply.Decision == "deny" {
		t.Errorf("TranslateOutcome(post): Decision = %q; the post-invocation reply must never deny, the real call already ran", reply.Decision)
	}
}

func TestTranslateOutcome_Post_IgnoresSubstituteAndPassesThrough(t *testing.T) {
	a := newTestAdapter()

	postCall, err := a.TranslateCall(domain.PhasePost, payloadFixture(t, "post_valid.json"))
	if err != nil {
		t.Fatalf("TranslateCall(post): %v", err)
	}

	outcome := domain.InterceptionOutcome{Kind: domain.OutcomeSubstitute, StubResponse: json.RawMessage(`{"status_code":"SUCCESS"}`)}

	native, err := a.TranslateOutcome(outcome, postCall)
	if err != nil {
		t.Fatalf("TranslateOutcome(post, substitute): expected the post-phase pass-through, not an error: %v", err)
	}
	if len(native) == 0 {
		t.Errorf("TranslateOutcome(post, substitute): returned an empty native reply")
	}
}

// ---------------------------------------------------------------------------
// NeutralReply
// ---------------------------------------------------------------------------

func TestNeutralReply_IsAValidNativeReplyForBothPhases(t *testing.T) {
	for _, phase := range []domain.InterceptionPhase{domain.PhasePre, domain.PhasePost} {
		native := opencode.NeutralReply(phase)
		if len(native) == 0 {
			t.Fatalf("NeutralReply(%s): returned an empty reply; the harness's interception point needs a valid answer", phase)
		}
		reply := decodeHookReply(t, native)
		if reply.Decision != "" {
			t.Errorf("NeutralReply(%s): Decision = %q, want empty — the neutral reply changes nothing", phase, reply.Decision)
		}
	}
}
