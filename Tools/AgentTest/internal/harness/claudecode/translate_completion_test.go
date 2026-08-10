package claudecode_test

// Tests for translating this harness's completion signal (T12.1): the event
// this stage recovers the collaborator's real reply from, on a harness whose
// post-invocation point fires at launch and cannot be used for echo fidelity
// (see Stage-12/Plan.md's "The defect").
//
// The completion signal's exact wire shape has not been confirmed against a
// live harness run (see Stage-12/Plan.md's Risks table: "The completion
// signal's payload shape is unknown without a live harness run... drive it
// from fixtures; fail loudly on an unrecognised shape"). These tests pin a
// concrete shape grounded in the mosaic-logger hook bundle's own SubagentStop
// handling (.claude/hooks/mosaic_logger_handlers_invocation.py): hook_event_
// name, session_id, agent_id, agent_transcript_path, last_assistant_message.
// Correlation-token recovery is injected via claudecode.Options.TranscriptReader
// rather than performed against a real file, both so a test needs no
// filesystem and so translation stays a pure function of its inputs.
//
// An un-stubbed or partially-stubbed dispatch — no agent_transcript_path, or
// a transcript with no planted token in it — is a legitimate outcome (empty
// CorrelationToken, no error), not a translation failure: see
// ContractsDesign.md's PhaseCompletion contract and AC12.7.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

func newCompletionTestAdapter(t *testing.T, entries []string) *claudecode.Adapter {
	t.Helper()
	reader := func(path string) ([]string, error) {
		return entries, nil
	}
	return claudecode.New(claudecode.Options{TranscriptReader: reader})
}

// TestTranslateCall_Completion_ValidPayload_CapturesObservedResponseAndToken
// is the previously-impossible case this stage exists to make possible
// (AC12.1, AC12.2): a completion payload whose transcript carries the token
// planted at the pre-invocation point must translate into a call carrying
// both the recovered reply and the recovered correlation token.
func TestTranslateCall_Completion_ValidPayload_CapturesObservedResponseAndToken(t *testing.T) {
	token := claudecode.NewToken()
	plantedEntry := claudecode.PlantToken(
		claudecode.TaskToolInput{SubagentType: "Worker", Prompt: "the original rewritten prompt"},
		token,
	).Prompt

	a := newCompletionTestAdapter(t, []string{"an unrelated transcript entry", plantedEntry})

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_valid.json"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}

	if call.Phase != domain.PhaseCompletion {
		t.Errorf("TranslateCall: Phase = %q, want %q", call.Phase, domain.PhaseCompletion)
	}
	if call.ObservedResponse == "" {
		t.Errorf("TranslateCall: ObservedResponse is empty; the completion payload's recovered reply must be captured")
	}
	if call.CorrelationToken != token {
		t.Errorf("TranslateCall: CorrelationToken = %q, want the token recovered from the transcript %q", call.CorrelationToken, token)
	}
}

// TestTranslateCall_Completion_CarriesCapabilityFlags mirrors the same
// obligation the pre/post paths already carry.
func TestTranslateCall_Completion_CarriesCapabilityFlags(t *testing.T) {
	a := newCompletionTestAdapter(t, nil)

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_no_transcript_path.json"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}
	if !reflect.DeepEqual(call.Capabilities, a.Capabilities()) {
		t.Errorf("TranslateCall: Capabilities = %+v, want the adapter's own declared capabilities %+v", call.Capabilities, a.Capabilities())
	}
}

// TestTranslateCall_Completion_NoTranscriptPath_SucceedsWithEmptyToken pins
// AC12.7: an un-stubbed dispatch has no transcript to recover a token from.
// That is a legitimate condition this translation must not fail on — the
// call still carries the observed reply, just no correlation token to key an
// echo comparison by.
func TestTranslateCall_Completion_NoTranscriptPath_SucceedsWithEmptyToken(t *testing.T) {
	a := newCompletionTestAdapter(t, nil)

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_no_transcript_path.json"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion, no transcript path): unexpected error for a legitimate un-stubbed dispatch: %v", err)
	}
	if call.CorrelationToken != "" {
		t.Errorf("TranslateCall: CorrelationToken = %q, want empty — no transcript means no token to recover", call.CorrelationToken)
	}
	if call.ObservedResponse == "" {
		t.Errorf("TranslateCall: ObservedResponse is empty; the reply must still be captured for an un-stubbed dispatch")
	}
}

// TestTranslateCall_Completion_TranscriptHasNoPlantedToken_SucceedsWithEmptyToken
// pins the partially-stubbed case named in AC12.7: a transcript exists (this
// dispatch went through the pre-invocation point) but carries no planted
// token, e.g. because the pre-invocation call resolved to passthrough rather
// than a rewrite. This is still not an error.
func TestTranslateCall_Completion_TranscriptHasNoPlantedToken_SucceedsWithEmptyToken(t *testing.T) {
	a := newCompletionTestAdapter(t, []string{"an ordinary transcript entry with no token in it"})

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_valid.json"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion, no planted token): unexpected error: %v", err)
	}
	if call.CorrelationToken != "" {
		t.Errorf("TranslateCall: CorrelationToken = %q, want empty — nothing was planted in this transcript", call.CorrelationToken)
	}
}

// TestTranslateCall_Completion_MalformedPayload_IsErrPayloadMalformedNeverPanic
// pins the specific error class, not merely "an error": a caller distinguishes
// "not JSON at all" from "recognisable JSON in an unrecognised shape".
func TestTranslateCall_Completion_MalformedPayload_IsErrPayloadMalformedNeverPanic(t *testing.T) {
	a := newCompletionTestAdapter(t, nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateCall(PhaseCompletion, malformed): panicked instead of returning an error: %v", r)
		}
	}()

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "malformed_not_json.json"))
	if err == nil {
		t.Fatalf("TranslateCall(PhaseCompletion, malformed): expected an error, got call=%+v", call)
	}
	if !errors.Is(err, claudecode.ErrPayloadMalformed) {
		t.Errorf("TranslateCall(PhaseCompletion, malformed): error = %v, want it to wrap %v", err, claudecode.ErrPayloadMalformed)
	}
}

// TestTranslateCall_Completion_WrongEventName_IsHandleableErrorNamingTheField
// pins that this path validates hook_event_name itself, the same way
// translatePre and translatePost do, rather than relying on the generic
// unrecognised-phase rejection every other phase value falls into.
func TestTranslateCall_Completion_WrongEventName_IsHandleableErrorNamingTheField(t *testing.T) {
	a := newCompletionTestAdapter(t, nil)

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_wrong_event_name.json"))
	if err == nil {
		t.Fatalf("TranslateCall(PhaseCompletion, wrong hook_event_name): expected an error, got call=%+v", call)
	}
	if !strings.Contains(err.Error(), "hook_event_name") {
		t.Errorf("TranslateCall(PhaseCompletion, wrong hook_event_name): error = %v, want it to name the offending field (hook_event_name)", err)
	}
}

// TestTranslateCall_Completion_EmptyPayload_IsHandleableErrorNeverPanic
// mirrors the same obligation TestTranslateCall_EmptyPayload_IsHandleableErrorNeverPanic
// pins for pre/post.
func TestTranslateCall_Completion_EmptyPayload_IsHandleableErrorNeverPanic(t *testing.T) {
	a := newCompletionTestAdapter(t, nil)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TranslateCall(PhaseCompletion, empty): panicked instead of returning an error: %v", r)
		}
	}()
	call, err := a.TranslateCall(domain.PhaseCompletion, []byte(``))
	if err == nil {
		t.Errorf("TranslateCall(PhaseCompletion, empty): expected an error, got call=%+v", call)
	}
}

// ---------------------------------------------------------------------------
// Outbound: TranslateOutcome on PhaseCompletion
// ---------------------------------------------------------------------------

// TestTranslateOutcome_Completion_AlwaysPassesThroughNeverDenies is the
// decisive containment property (AC12.5): the completion event fires after
// the collaborator has already run, exactly like PhasePost. Even an outcome
// that would deny at the pre-invocation point (e.g. because internal/intercept
// still misroutes PhaseCompletion through its pre-invocation decision path)
// must never reach the subject as a denial once translated here.
func TestTranslateOutcome_Completion_AlwaysPassesThroughNeverDenies(t *testing.T) {
	a := newCompletionTestAdapter(t, nil)

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_valid.json"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}

	outcome := domain.InterceptionOutcome{Kind: domain.OutcomeHalt, HaltReason: domain.HaltUnmatched, Message: "would have halted"}

	native, err := a.TranslateOutcome(outcome, call)
	if err != nil {
		t.Fatalf("TranslateOutcome(completion): %v", err)
	}

	reply := decodeHookReply(t, native)
	if reply.HookSpecificOutput != nil && reply.HookSpecificOutput.PermissionDecision == "deny" {
		t.Errorf("TranslateOutcome(completion): reply denied (%+v); the completion reply must never deny, the real call already ran", reply.HookSpecificOutput)
	}
}
