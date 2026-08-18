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

	if !reflect.DeepEqual(call.Capabilities, a.Capabilities()) {
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
