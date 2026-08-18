package claudecode_test

// Tests for inbound payload translation (T11.2) and outbound outcome
// translation (T11.3).
//
// Inbound: a native pre-invocation payload becomes a normalized
// domain.InterceptedCall carrying the composite collaborator identity, the
// parsed task message, the raw native payload and the adapter's capability
// flags. An unrecognised event, a payload missing the fields identity is
// derived from, and a payload that is not valid at all must each surface as
// a handleable error, never a panic.
//
// Outbound: every domain.InterceptionOutcome kind maps to a defined native
// reply, except OutcomeSubstitute, which this adapter's capabilities make
// unreachable and which must fail loudly rather than emit a wrong reply.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
	"mosaic-agent-test/internal/stubmatch"
)

func payloadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "testdata", "claudecode", "payloads", name))
	if err != nil {
		t.Fatalf("payloadFixture: resolving %q: %v", name, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("payloadFixture: reading %q: %v", path, err)
	}
	return data
}

func newTestAdapter() *claudecode.Adapter {
	return claudecode.New(claudecode.Options{})
}

// ---------------------------------------------------------------------------
// Inbound: TranslateCall
// ---------------------------------------------------------------------------

// TestTranslateCall_PreSubagentDispatch_ProducesCompositeIdentity fixes the
// defect this stage exists to remediate: the fixture's native payload
// reports the harness's own dispatch-tool name (claudecode.NativeDispatchToolName,
// "Agent" — what the real harness actually sends), and the identity
// TranslateCall produces must carry the normalized, harness-neutral name
// (domain.DispatchToolName, "dispatch") that authored tests use — never the
// native name verbatim.
func TestTranslateCall_PreSubagentDispatch_ProducesCompositeIdentity(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_subagent.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	want := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
	if call.Identity != want {
		t.Errorf("TranslateCall: Identity = %+v, want %+v (the normalized identity, not the harness's native tool name)", call.Identity, want)
	}
	if call.Phase != domain.PhasePre {
		t.Errorf("TranslateCall: Phase = %q, want %q", call.Phase, domain.PhasePre)
	}
}

// TestTranslateCall_Post_NormalizesNativeToolNameToDispatch is the same
// normalization obligation on the post-invocation translation path: a native
// payload reporting the harness's own dispatch-tool name must still yield
// the normalized neutral identity.
func TestTranslateCall_Post_NormalizesNativeToolNameToDispatch(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePost, payloadFixture(t, "post_valid.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	want := domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"}
	if call.Identity != want {
		t.Errorf("TranslateCall(post): Identity = %+v, want %+v (the normalized identity, not the harness's native tool name)", call.Identity, want)
	}
}

// TestTranslateCall_Pre_UnrecognisedNativeDispatchToolName_IsHandleableError
// pins the failure mode named in ContractsDesign.md's normalization
// contract: an unrecognised native dispatch-tool name must be returned as a
// translation error, never passed through silently. Silent pass-through is
// how a vendor's next rename would become another invisible defect.
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

// TestRegistrationToolName_DiffersFromNativeDispatchToolName pins the
// separation ContractsDesign.md requires: the name this harness accepts for
// hook-matcher registration and the name it reports at its interception
// point are two different concepts. Conflating them (making them the same
// constant, or the same value) is exactly the defect that broke interception
// once already.
func TestRegistrationToolName_DiffersFromNativeDispatchToolName(t *testing.T) {
	if claudecode.RegistrationToolName == claudecode.NativeDispatchToolName {
		t.Errorf("RegistrationToolName (%q) must differ from NativeDispatchToolName (%q); conflating registration with the reported name is the defect this separation prevents",
			claudecode.RegistrationToolName, claudecode.NativeDispatchToolName)
	}
}

// TestNormalizedIdentity_MatchesStubRegistryWrittenInNormalizedVocabulary is
// the end-to-end proof this stage exists to deliver (T11.2): a stub
// registry authored entirely in the normalized vocabulary
// (domain.DispatchToolName) must match the identity TranslateCall derives
// from a real native payload — the exact path that was broken when the
// harness's native "Agent" name reached a registry keyed on "Task" and
// went unmatched. The same domain.CollaboratorIdentity equality this proves
// for stub matching is what invocation-sequence assertions are matched by
// too (see internal/preflight's sequenceIdentities and
// checkCollaboratorsKnown), so this one proof covers both consumers.
func TestNormalizedIdentity_MatchesStubRegistryWrittenInNormalizedVocabulary(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_subagent.json"))
	if err != nil {
		t.Fatalf("TranslateCall: %v", err)
	}

	registry := domain.StubRegistry{
		OnUnmatched: domain.UnmatchedHalt,
		Stubs: []domain.Stub{
			{
				Match: domain.StubMatch{
					Identity:   domain.CollaboratorIdentity{ToolName: domain.DispatchToolName, AgentIdentity: "Worker"},
					Invocation: 1,
				},
				Response: json.RawMessage(`{"status_code":"SUCCESS"}`),
			},
		},
	}

	result := stubmatch.Match(registry, call.Identity, 1)
	if !result.Matched {
		t.Errorf("stubmatch.Match: Matched = false, want true — a stub registry authored in the normalized vocabulary (%q) must match the identity derived from a real native payload (%+v)",
			domain.DispatchToolName, call.Identity)
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
		t.Errorf("TranslateCall: ObservedResponse is empty; the post-invocation payload's tool_response must be captured")
	}
}

func TestTranslateCall_UnrecognisedEvent_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	for _, phase := range []domain.InterceptionPhase{domain.PhasePre, domain.PhasePost} {
		name := "pre_unrecognised_event.json"
		if phase == domain.PhasePost {
			name = "post_unrecognised_event.json"
		}

		call, err := a.TranslateCall(phase, payloadFixture(t, name))
		if err == nil {
			t.Errorf("TranslateCall(%s, unrecognised event): expected an error, got call=%+v", phase, call)
		}
	}
}

func TestTranslateCall_MissingIdentityFields_IsHandleableError(t *testing.T) {
	a := newTestAdapter()

	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_missing_identity.json"))
	if err == nil {
		t.Errorf("TranslateCall: expected an error for a payload missing the fields identity is derived from, got call=%+v", call)
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

func preCall(t *testing.T, a *claudecode.Adapter) domain.InterceptedCall {
	t.Helper()
	call, err := a.TranslateCall(domain.PhasePre, payloadFixture(t, "pre_valid_subagent.json"))
	if err != nil {
		t.Fatalf("preCall: TranslateCall: %v", err)
	}
	return call
}

func decodeHookReply(t *testing.T, native []byte) claudecode.HookReply {
	t.Helper()
	var reply claudecode.HookReply
	if err := json.Unmarshal(native, &reply); err != nil {
		t.Fatalf("decodeHookReply: malformed reply %q: %v", native, err)
	}
	return reply
}

func TestTranslateOutcome_RewritePrompt_AllowsWithUpdatedInput(t *testing.T) {
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
	if reply.HookSpecificOutput == nil {
		t.Fatalf("TranslateOutcome(RewritePrompt): no hookSpecificOutput in reply %q", native)
	}
	if reply.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("TranslateOutcome(RewritePrompt): PermissionDecision = %q, want %q", reply.HookSpecificOutput.PermissionDecision, "allow")
	}
	if len(reply.HookSpecificOutput.UpdatedInput) == 0 {
		t.Errorf("TranslateOutcome(RewritePrompt): UpdatedInput is empty; the rewritten call input must replace the tool's arguments")
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
	if reply.HookSpecificOutput == nil {
		t.Fatalf("TranslateOutcome(Halt): no hookSpecificOutput in reply %q", native)
	}
	if reply.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("TranslateOutcome(Halt): PermissionDecision = %q, want %q", reply.HookSpecificOutput.PermissionDecision, "deny")
	}
	if reply.HookSpecificOutput.PermissionDecisionReason == "" {
		t.Errorf("TranslateOutcome(Halt): PermissionDecisionReason is empty; the subject needs a plausible reason")
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
	if reply.HookSpecificOutput != nil {
		t.Errorf("TranslateOutcome(Passthrough): hookSpecificOutput = %+v, want none — passthrough must change nothing", reply.HookSpecificOutput)
	}
}

func TestTranslateOutcome_Substitute_IsUnreachableAndFailsLoudly(t *testing.T) {
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
	if reply.HookSpecificOutput != nil && reply.HookSpecificOutput.PermissionDecision == "deny" {
		t.Errorf("TranslateOutcome(post): reply denied (%+v); the post-invocation reply must never deny, the real call already ran", reply.HookSpecificOutput)
	}
}

// ---------------------------------------------------------------------------
// NeutralReply
// ---------------------------------------------------------------------------

func TestNeutralReply_IsAValidNativeReplyForBothPhases(t *testing.T) {
	for _, phase := range []domain.InterceptionPhase{domain.PhasePre, domain.PhasePost} {
		native := claudecode.NeutralReply(phase)
		if len(native) == 0 {
			t.Fatalf("NeutralReply(%s): returned an empty reply; the harness's interception point needs a valid answer", phase)
		}
		reply := decodeHookReply(t, native)
		if reply.HookSpecificOutput != nil {
			t.Errorf("NeutralReply(%s): hookSpecificOutput = %+v, want none — the neutral reply changes nothing", phase, reply.HookSpecificOutput)
		}
	}
}
