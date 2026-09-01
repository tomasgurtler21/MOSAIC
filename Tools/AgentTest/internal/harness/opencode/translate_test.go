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
	"reflect"
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

	want := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
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

	if !reflect.DeepEqual(call.Capabilities, a.Capabilities()) {
		t.Errorf("TranslateCall: Capabilities = %+v, want the adapter's own declared capabilities %+v", call.Capabilities, a.Capabilities())
	}
}

// TestTranslateCall_Pre_UnrecognisedNativeDispatchToolName_IsHandleableError
// pins the failure mode named in the normalization contract: an unrecognised
// native dispatch-tool name must be returned as a translation error, never
// passed through silently. Silent pass-through is how a vendor's next rename
// would become another invisible defect.
func TestTranslateCall_Pre_UnrecognisedNativeDispatchToolName_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_unknown_dispatch_tool.json"))
	if err == nil {
		t.Errorf("TranslateCall: expected an error for an unrecognised native dispatch-tool name, got call=%+v", call)
	}
}

// TestTranslateCall_Post_UnrecognisedNativeDispatchToolName_IsHandleableError
// is the same obligation on the post-invocation path.
func TestTranslateCall_Post_UnrecognisedNativeDispatchToolName_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePost, payloadFixture(t, "post_unknown_dispatch_tool.json"))
	if err == nil {
		t.Errorf("TranslateCall(post): expected an error for an unrecognised native dispatch-tool name, got call=%+v", call)
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

// ---------------------------------------------------------------------------
// Correlation identity: first dispatch (pre phase must mint, not just recover)
// ---------------------------------------------------------------------------

// buildPrePayloadNoToken creates a raw tool.execute.before payload whose
// args carry no correlation token. This simulates a genuinely first-time
// dispatch — one where no earlier interceptor has planted a token — so the
// adapter must mint one rather than recovering an existing one.
func buildPrePayloadNoToken(t *testing.T, tool, subagentType, prompt string) []byte {
	t.Helper()
	args := opencode.TaskToolArgs{SubagentType: subagentType, Prompt: prompt}
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("buildPrePayloadNoToken: marshaling args: %v", err)
	}
	payload := opencode.ToolBeforePayload{
		HookEventName: "tool.execute.before",
		Tool:          tool,
		Args:          rawArgs,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("buildPrePayloadNoToken: marshaling payload: %v", err)
	}
	return b
}

// buildPostPayloadFromArgs creates a raw tool.execute.after payload that
// echoes the given args and wraps observed in the {"output":"…"} form.
// It simulates the harness delivering the post-invocation event after the
// real tool call ran with the (possibly rewritten) args.
func buildPostPayloadFromArgs(t *testing.T, tool string, args opencode.TaskToolArgs, observed string) []byte {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("buildPostPayloadFromArgs: marshaling args: %v", err)
	}
	outputObj := struct {
		Output string `json:"output"`
	}{Output: observed}
	rawOutput, err := json.Marshal(outputObj)
	if err != nil {
		t.Fatalf("buildPostPayloadFromArgs: marshaling output: %v", err)
	}
	payload := opencode.ToolAfterPayload{
		HookEventName: "tool.execute.after",
		Tool:          tool,
		Args:          rawArgs,
		Output:        rawOutput,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("buildPostPayloadFromArgs: marshaling payload: %v", err)
	}
	return b
}

// TestTranslateCall_Pre_FirstDispatch_ProducesNonEmptyCorrelationToken asserts
// that a pre-invocation call whose args carry no existing correlation token
// produces a non-empty CorrelationToken on the returned InterceptedCall. The
// adapter must mint one rather than returning empty, so the post-phase event
// can be matched to this dispatch.
//
// This test FAILS against the current recover-only implementation, where
// translatePre only calls RecoverToken and returns empty when no token is
// present in the payload.
func TestTranslateCall_Pre_FirstDispatch_ProducesNonEmptyCorrelationToken(t *testing.T) {
	a := opencode.New(opencode.Options{})
	native := buildPrePayloadNoToken(t, "task", "Worker", "please do the work")

	call, err := a.TranslateCall(domain.PhasePre, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}

	if call.CorrelationToken == "" {
		t.Errorf("TranslateCall(PhasePre): CorrelationToken is empty for a first dispatch; every pre-invocation call must carry a non-empty correlation identity so the post-phase event can be matched to it")
	}
}

// TestTranslateCall_Pre_FirstDispatch_UsesNewTokenSeam asserts that when
// Options.NewToken is set, the adapter uses it to generate the token for a
// first-time dispatch. The seam makes the planted token deterministic so
// round-trip assertions can be precise without depending on a random source.
//
// This test FAILS against the current implementation, which does not read
// opts.NewToken in translatePre.
func TestTranslateCall_Pre_FirstDispatch_UsesNewTokenSeam(t *testing.T) {
	const wantToken = "c8a3f2b1d7e4096a5f1b2e3d4c5a6b7c"
	a := opencode.New(opencode.Options{
		NewToken: func() string { return wantToken },
	})
	native := buildPrePayloadNoToken(t, "task", "Worker", "please do the work")

	call, err := a.TranslateCall(domain.PhasePre, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}

	if call.CorrelationToken != wantToken {
		t.Errorf("TranslateCall(PhasePre): CorrelationToken = %q, want the seam-supplied token %q; the adapter must use Options.NewToken for a first dispatch so tests can assert round-trip identity precisely", call.CorrelationToken, wantToken)
	}
}

// TestTranslateCall_Pre_ExistingToken_RecoveredNotMinted asserts that a
// pre-invocation call whose args already carry a correlation token recovers
// that token rather than minting a new one. A call that already carries an
// identity must never have a second one minted for it (idempotence).
func TestTranslateCall_Pre_ExistingToken_RecoveredNotMinted(t *testing.T) {
	const plantedToken = "d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1"
	a := opencode.New(opencode.Options{
		NewToken: func() string { return "should-never-be-used-for-an-already-identified-call" },
	})

	args := opencode.PlantToken(opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "please do the work"}, plantedToken)
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshaling args: %v", err)
	}
	payload := opencode.ToolBeforePayload{
		HookEventName: "tool.execute.before",
		Tool:          "task",
		Args:          rawArgs,
	}
	native, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}

	call, err := a.TranslateCall(domain.PhasePre, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}

	if call.CorrelationToken != plantedToken {
		t.Errorf("TranslateCall(PhasePre): CorrelationToken = %q, want the already-planted token %q; a call that already carries an identity must not have a second one minted for it", call.CorrelationToken, plantedToken)
	}
}

// TestTranslateCall_CorrelationIdentity_RecoverableAtPostPhase is the
// end-to-end round-trip test: the adapter mints a token at the pre phase,
// TranslateOutcome plants it in the rewritten args, the harness echoes those
// args at the post phase, and TranslateCall at the post phase recovers the
// same token. This is the core guarantee the correlation mechanism must
// provide.
//
// This test FAILS against the current implementation because translatePre
// does not mint a token, so preCall.CorrelationToken is empty and the seam
// is unused.
func TestTranslateCall_CorrelationIdentity_RecoverableAtPostPhase(t *testing.T) {
	const wantToken = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
	a := opencode.New(opencode.Options{
		NewToken: func() string { return wantToken },
	})

	// Pre phase: adapter must mint a token because none is present.
	preNative := buildPrePayloadNoToken(t, "task", "Worker", "please do the work")
	preCall, err := a.TranslateCall(domain.PhasePre, preNative)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePre): %v", err)
	}
	if preCall.CorrelationToken == "" {
		t.Fatalf("TranslateCall(PhasePre): CorrelationToken is empty after first dispatch; cannot verify recovery at post phase — the pre phase must mint a token before this test can proceed")
	}

	// Outcome translation plants the token in the rewritten args.
	outcome := domain.InterceptionOutcome{
		Kind:             domain.OutcomeRewritePrompt,
		RewrittenPrompt:  "please do the work",
		CorrelationToken: preCall.CorrelationToken,
	}
	rewrittenNative, err := a.TranslateOutcome(outcome, preCall)
	if err != nil {
		t.Fatalf("TranslateOutcome: %v", err)
	}

	hookReply := decodeHookReply(t, rewrittenNative)
	var plantedArgs opencode.TaskToolArgs
	if err := json.Unmarshal(hookReply.UpdatedArgs, &plantedArgs); err != nil {
		t.Fatalf("decoding updated args from hook reply: %v", err)
	}

	// Post phase: the harness echoes the rewritten (now token-carrying) args.
	postNative := buildPostPayloadFromArgs(t, "task", plantedArgs, `{"status_code":"SUCCESS"}`)
	postCall, err := a.TranslateCall(domain.PhasePost, postNative)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost): %v", err)
	}

	if postCall.CorrelationToken != preCall.CorrelationToken {
		t.Errorf("round-trip: post CorrelationToken = %q, want the same token planted at the pre phase %q; the token planted at pre-invocation must be recoverable from the echoed args at the post-invocation point", postCall.CorrelationToken, preCall.CorrelationToken)
	}
}

// ---------------------------------------------------------------------------
// Correlation identity: degradation path (un-rewritten dispatch)
// ---------------------------------------------------------------------------

// TestTranslateCall_Post_UnrewrittenDispatch_TranslatesWithoutError asserts
// that a post-phase payload whose args carry no correlation token translates
// without error. An un-rewritten dispatch — one whose pre-invocation hook was
// not reached by this adapter — is a legitimate condition, not a fault.
func TestTranslateCall_Post_UnrewrittenDispatch_TranslatesWithoutError(t *testing.T) {
	a := opencode.New(opencode.Options{})
	// Args with no planted token: the pre phase never intercepted this call.
	native := buildPostPayloadFromArgs(t, "task",
		opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "an ordinary prompt this adapter never rewrote"},
		`{"status_code":"SUCCESS"}`,
	)

	_, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Errorf("TranslateCall(PhasePost, no token): unexpected error %v; an un-rewritten dispatch at the post phase is a legitimate condition, not a fault", err)
	}
}

// TestTranslateCall_Post_UnrewrittenDispatch_CorrelationTokenIsEmpty asserts
// that a post-phase payload with no planted token produces an empty
// CorrelationToken. The post phase must never fabricate a correlation identity
// for a dispatch the pre phase did not rewrite; an empty token is the correct
// signal that this call was not intercepted end-to-end.
func TestTranslateCall_Post_UnrewrittenDispatch_CorrelationTokenIsEmpty(t *testing.T) {
	a := opencode.New(opencode.Options{})
	native := buildPostPayloadFromArgs(t, "task",
		opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "an ordinary prompt this adapter never rewrote"},
		`{"status_code":"SUCCESS"}`,
	)

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost): %v", err)
	}

	if call.CorrelationToken != "" {
		t.Errorf("TranslateCall(PhasePost, no token): CorrelationToken = %q, want empty; the post phase must not fabricate a correlation identity for a dispatch the pre phase did not rewrite", call.CorrelationToken)
	}
}

// TestTranslateCall_Post_NeverMintsToken asserts that the post phase never
// uses the NewToken seam, even when one is supplied. Minting is exclusively
// the pre phase's responsibility; a post-phase token must be a recovery or
// nothing.
func TestTranslateCall_Post_NeverMintsToken(t *testing.T) {
	a := opencode.New(opencode.Options{
		NewToken: func() string { return "should-never-be-used-at-post-phase" },
	})
	native := buildPostPayloadFromArgs(t, "task",
		opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "an ordinary prompt this adapter never rewrote"},
		`{"status_code":"SUCCESS"}`,
	)

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost): %v", err)
	}

	if call.CorrelationToken != "" {
		t.Errorf("TranslateCall(PhasePost): CorrelationToken = %q, want empty; the post phase must not use the NewToken seam — minting belongs exclusively to the pre phase", call.CorrelationToken)
	}
}

// ---------------------------------------------------------------------------
// extractOutput: XML envelope handling
//
// OpenCode wraps the subagent response in an XML envelope of the form
//   <task><task_result>...JSON payload...</task_result></task>
// when reporting the tool result in a post-invocation hook event. The
// extractOutput function must strip this envelope and return the inner
// payload so that echo fidelity comparisons operate on the actual response,
// not the XML wrapper.
//
// All tests in this section drive extractOutput indirectly through
// TranslateCall at the post phase, which calls translatePost which calls
// extractOutput on payload.Output. The output field is a JSON-encoded string
// whose value is the XML envelope (or a non-XML string for unchanged-behavior
// cases). This matches the realistic wire format: OpenCode places the tool
// result as a JSON string value in the "output" field of the after-hook event.
//
// Tests that assert new XML extraction behavior are expected to FAIL against
// the current extractOutput implementation, which returns the full XML string
// via the bare-JSON-string fallback. They will pass once extractOutput gains
// XML detection and task_result extraction.
// ---------------------------------------------------------------------------

// buildPostPayloadWithJSONStringOutput creates a raw tool.execute.after payload
// where the output field is a JSON-encoded string (not wrapped in a
// {"output":"..."} object). This matches the format used when OpenCode delivers
// the tool result directly as a bare JSON string value — which may be an XML
// envelope on its content.
func buildPostPayloadWithJSONStringOutput(t *testing.T, tool string, args opencode.TaskToolArgs, output string) []byte {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("buildPostPayloadWithJSONStringOutput: marshaling args: %v", err)
	}
	rawOutput, err := json.Marshal(output) // encodes output as a JSON string value
	if err != nil {
		t.Fatalf("buildPostPayloadWithJSONStringOutput: marshaling output string: %v", err)
	}
	payload := opencode.ToolAfterPayload{
		HookEventName: "tool.execute.after",
		Tool:          tool,
		Args:          rawArgs,
		Output:        rawOutput,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("buildPostPayloadWithJSONStringOutput: marshaling payload: %v", err)
	}
	return b
}

// TestTranslateCall_Post_XMLEnvelope_ExtractsJSONFromTaskResult asserts that
// when the output field contains an XML-wrapped response, TranslateCall
// extracts the JSON payload from inside the <task_result> tags rather than
// returning the full XML wrapper string.
//
// This test is expected to FAIL against the current extractOutput
// implementation: without XML detection, the bare-JSON-string fallback returns
// the entire XML string, which is not the actual agent response.
func TestTranslateCall_Post_XMLEnvelope_ExtractsJSONFromTaskResult(t *testing.T) {
	a := opencode.New(opencode.Options{})
	args := opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "do the work"}

	xmlEnvelope := `<task><task_result>{"status_code":"SUCCESS","status_message":"done"}</task_result></task>`
	native := buildPostPayloadWithJSONStringOutput(t, "task", args, xmlEnvelope)

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost, XML envelope): %v", err)
	}

	want := `{"status_code":"SUCCESS","status_message":"done"}`
	if call.ObservedResponse != want {
		t.Errorf("TranslateCall(PhasePost, XML envelope): ObservedResponse = %q, want %q; XML-wrapped responses must have the JSON payload extracted from inside <task_result> tags, not returned as the raw XML string", call.ObservedResponse, want)
	}
}

// TestTranslateCall_Post_XMLEnvelope_WhitespaceAroundPayload_Trimmed asserts
// that whitespace between the <task_result> tags and the inner JSON payload is
// trimmed, so the extracted content matches the literal JSON the agent wrote.
//
// This test is expected to FAIL against the current extractOutput
// implementation.
func TestTranslateCall_Post_XMLEnvelope_WhitespaceAroundPayload_Trimmed(t *testing.T) {
	a := opencode.New(opencode.Options{})
	args := opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "do the work"}

	// Whitespace on both sides of the payload inside task_result.
	xmlEnvelope := "<task><task_result>\n  {\"status_code\":\"SUCCESS\"}\n</task_result></task>"
	native := buildPostPayloadWithJSONStringOutput(t, "task", args, xmlEnvelope)

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost, XML envelope with whitespace): %v", err)
	}

	want := `{"status_code":"SUCCESS"}`
	if call.ObservedResponse != want {
		t.Errorf("TranslateCall(PhasePost, XML envelope with whitespace): ObservedResponse = %q, want %q; whitespace surrounding the JSON payload inside <task_result> must be trimmed", call.ObservedResponse, want)
	}
}

// TestTranslateCall_Post_XMLEnvelope_NestedTaskResultTags_ExtractsOutermost
// asserts that when the content inside <task_result> itself contains nested
// <task_result> tags (a defensive case for unknown nesting), extractOutput
// uses the first opening tag and the last closing tag, yielding the outermost
// span of content.
//
// This test is expected to FAIL against the current extractOutput
// implementation.
func TestTranslateCall_Post_XMLEnvelope_NestedTaskResultTags_ExtractsOutermost(t *testing.T) {
	a := opencode.New(opencode.Options{})
	args := opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "do the work"}

	// Nested task_result: the inner tags are part of the payload content.
	xmlEnvelope := `<task><task_result>outer<task_result>inner</task_result>more</task_result></task>`
	native := buildPostPayloadWithJSONStringOutput(t, "task", args, xmlEnvelope)

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost, nested XML tags): %v", err)
	}

	want := `outer<task_result>inner</task_result>more`
	if call.ObservedResponse != want {
		t.Errorf("TranslateCall(PhasePost, nested XML tags): ObservedResponse = %q, want %q; extractOutput must use first <task_result> and last </task_result> to handle nested occurrences defensively", call.ObservedResponse, want)
	}
}

// TestTranslateCall_Post_XMLWithoutTaskResultTags_FallsThrough asserts that
// XML-looking content that contains no <task_result> tags is returned as-is,
// without modification. The extraction logic must fall through rather than
// panic or return an error when the expected tags are absent.
func TestTranslateCall_Post_XMLWithoutTaskResultTags_FallsThrough(t *testing.T) {
	a := opencode.New(opencode.Options{})
	args := opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "do the work"}

	// XML content with no task_result tags — extraction should be a no-op.
	xmlNoTaskResult := `<task><other_tag>some content here</other_tag></task>`
	native := buildPostPayloadWithJSONStringOutput(t, "task", args, xmlNoTaskResult)

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost, XML without task_result): %v", err)
	}

	// When no <task_result> tags are found, the full XML is returned unchanged.
	want := xmlNoTaskResult
	if call.ObservedResponse != want {
		t.Errorf("TranslateCall(PhasePost, XML without task_result): ObservedResponse = %q, want %q; when <task_result> tags are absent, extraction must fall through and return the content unchanged", call.ObservedResponse, want)
	}
}

// TestTranslateCall_Post_BareJSONOutput_UnchangedByXMLExtraction asserts that
// a non-XML output (a bare JSON object string) continues to be returned as-is
// after the XML envelope feature is added. This is a regression guard: the
// new XML path must not alter behavior for non-XML content.
func TestTranslateCall_Post_BareJSONOutput_UnchangedByXMLExtraction(t *testing.T) {
	a := opencode.New(opencode.Options{})
	args := opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "do the work"}

	// A non-XML response — should pass through unchanged.
	jsonResponse := `{"status_code":"SUCCESS","status_message":"task complete"}`
	native := buildPostPayloadWithJSONStringOutput(t, "task", args, jsonResponse)

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost, bare JSON output): %v", err)
	}

	if call.ObservedResponse != jsonResponse {
		t.Errorf("TranslateCall(PhasePost, bare JSON output): ObservedResponse = %q, want %q; non-XML responses must be returned unchanged by the XML extraction path", call.ObservedResponse, jsonResponse)
	}
}

// ---------------------------------------------------------------------------
// extractOutput: content-array XML envelope handling
//
// When OpenCode wraps its tool result as a JSON content array of the form
//   {"content":[{"type":"text","text":"<task ...><task_result>...</task_result></task>"}]}
// the extractOutput function must still strip the XML envelope from the
// extracted text value and return only the inner payload.
//
// The test in this section drives extractOutput indirectly through TranslateCall
// at the post phase, following the same pattern as the JSON-string XML envelope
// tests above. The content-array wrapper is synthetically constructed around
// real envelope text captured from a production run, per the test fixture
// provenance requirement.
// ---------------------------------------------------------------------------

// buildPostPayloadWithContentArrayOutput creates a raw tool.execute.after payload
// where the output field is a JSON content array with a single text entry. This
// matches the format used when OpenCode delivers the tool result as a content array
// whose text value may contain an XML envelope.
func buildPostPayloadWithContentArrayOutput(t *testing.T, tool string, args opencode.TaskToolArgs, outputText string) []byte {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("buildPostPayloadWithContentArrayOutput: marshaling args: %v", err)
	}
	contentArray := struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}{
		Content: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{{Type: "text", Text: outputText}},
	}
	rawOutput, err := json.Marshal(contentArray)
	if err != nil {
		t.Fatalf("buildPostPayloadWithContentArrayOutput: marshaling content array: %v", err)
	}
	payload := opencode.ToolAfterPayload{
		HookEventName: "tool.execute.after",
		Tool:          tool,
		Args:          rawArgs,
		Output:        rawOutput,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("buildPostPayloadWithContentArrayOutput: marshaling payload: %v", err)
	}
	return b
}

// TestTranslateCall_Post_ContentArrayOutput_XMLEnvelope_ExtractsInnerPayload
// asserts that when the output field is a content array whose text entry contains
// an XML-wrapped response, TranslateCall extracts the JSON payload from inside
// the <task_result> tags rather than returning the raw XML string.
//
// Source: Tools/AgentTest/dist/report-status-routing.suite.yaml-20260824T163459.json,
// echo_fidelity assertion target "1", run 20260824T163515Z-9739. The realEnvelopeText
// below is the genuine un-stripped envelope text from that production run. The
// content-array wrapper is synthetically constructed around it — this is a
// reconstruction: real envelope text, synthetically constructed content-array
// wrapper.
//
// This test is expected to FAIL against the current extractOutput implementation:
// the content-array branch extracts and concatenates the text parts but does not
// apply XML stripping, so the full XML envelope text is returned instead of the
// inner payload. The fix (I1.1) must apply the shared XML-stripping pass to the
// text extracted from all container shapes, including content arrays.
func TestTranslateCall_Post_ContentArrayOutput_XMLEnvelope_ExtractsInnerPayload(t *testing.T) {
	a := opencode.New(opencode.Options{})
	args := opencode.TaskToolArgs{SubagentType: "plan-review", Prompt: "review the plan"}

	// Real XML envelope text from report-status-routing.suite.yaml-20260824T163459.json,
	// echo_fidelity assertion target "1", run 20260824T163515Z-9739. Reconstruction:
	// genuine envelope text, synthetically constructed content-array wrapper.
	realEnvelopeText := "<task id=\"ses_fcb5f4d10ffe9i0rGL3NJbKEUB\" state=\"completed\">\n<task_result>\n{\n        \"agent_instance_id\": \"plan-review#5\",\n        \"run_id\": \"20260824T163515Z-9739\",\n        \"status_code\": \"COMPLETED_NEEDS_ACTION\",\n        \"status_message\": \"Plan lacks error handling strategy for pflag parse failures.\"\n      }\n</task_result>\n</task>"
	native := buildPostPayloadWithContentArrayOutput(t, "task", args, realEnvelopeText)

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost, content-array XML envelope): %v", err)
	}

	// The expected value is the content between <task_result> and </task_result>,
	// with surrounding whitespace trimmed, matching the behavior of the bare-XML
	// and JSON-string XML envelope tests.
	want := "{\n        \"agent_instance_id\": \"plan-review#5\",\n        \"run_id\": \"20260824T163515Z-9739\",\n        \"status_code\": \"COMPLETED_NEEDS_ACTION\",\n        \"status_message\": \"Plan lacks error handling strategy for pflag parse failures.\"\n      }"
	if call.ObservedResponse != want {
		t.Errorf("TranslateCall(PhasePost, content-array XML envelope): ObservedResponse = %q, want %q; content-array output whose text contains an XML envelope must have the inner payload extracted from <task_result> tags, not returned as the raw XML string", call.ObservedResponse, want)
	}
}

// TestTranslateCall_Post_EmptyOutput_UnchangedByXMLExtraction asserts that an
// empty (zero-length) output continues to produce an empty ObservedResponse.
// This is a regression guard for the empty-input early-return path.
func TestTranslateCall_Post_EmptyOutput_UnchangedByXMLExtraction(t *testing.T) {
	a := opencode.New(opencode.Options{})
	args := opencode.TaskToolArgs{SubagentType: "Worker", Prompt: "do the work"}

	// Construct a payload with an empty output field (null JSON value).
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshaling args: %v", err)
	}
	payload := opencode.ToolAfterPayload{
		HookEventName: "tool.execute.after",
		Tool:          "task",
		Args:          rawArgs,
		Output:        nil, // no output
	}
	native, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}

	call, err := a.TranslateCall(domain.PhasePost, native)
	if err != nil {
		t.Fatalf("TranslateCall(PhasePost, empty output): %v", err)
	}

	if call.ObservedResponse != "" {
		t.Errorf("TranslateCall(PhasePost, empty output): ObservedResponse = %q, want empty string; empty output must remain empty after XML extraction is added", call.ObservedResponse)
	}
}
