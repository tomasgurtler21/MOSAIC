package claudecode_test

// Tests for translating this harness's completion signal: the SubagentStop
// event this stage recovers the collaborator's real reply from, on a harness
// whose post-invocation point fires at launch and cannot be used for echo
// fidelity.
//
// The correlation mechanism is tool_use_id: the harness sends a dispatch-
// scoped identifier on both the originating PreToolUse event and the
// SubagentStop completion event. An absent tool_use_id is a legitimate
// outcome for an un-stubbed dispatch (CorrelationToken comes back empty,
// never an error). See correlation.go for the full mechanism basis.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/claudecode"
)

// TestTranslateCall_Completion_ValidPayload_CapturesObservedResponseAndToken
// asserts the primary guarantee of the completion mechanism: a SubagentStop
// payload carrying both last_assistant_message and tool_use_id produces a
// call with both the real reply and the correlation token populated.
func TestTranslateCall_Completion_ValidPayload_CapturesObservedResponseAndToken(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

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
	// The fixture carries tool_use_id "tu-pre-valid-1".
	const wantToken = "tu-pre-valid-1"
	if call.CorrelationToken != wantToken {
		t.Errorf("TranslateCall: CorrelationToken = %q, want %q (from fixture tool_use_id)", call.CorrelationToken, wantToken)
	}
}

// TestTranslateCall_Completion_CarriesCapabilityFlags mirrors the same
// obligation the pre/post paths already carry.
func TestTranslateCall_Completion_CarriesCapabilityFlags(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_no_transcript_path.json"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion): %v", err)
	}
	if !reflect.DeepEqual(call.Capabilities, a.Capabilities()) {
		t.Errorf("TranslateCall: Capabilities = %+v, want the adapter's own declared capabilities %+v", call.Capabilities, a.Capabilities())
	}
}

// TestTranslateCall_Completion_NoToolUseID_SucceedsWithEmptyToken asserts
// degradation: a SubagentStop payload without tool_use_id is a legitimate
// un-stubbed dispatch. The call still carries the observed reply; it just
// has no correlation token to key an echo comparison by.
func TestTranslateCall_Completion_NoToolUseID_SucceedsWithEmptyToken(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	// completion_no_transcript_path.json has no tool_use_id.
	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_no_transcript_path.json"))
	if err != nil {
		t.Fatalf("TranslateCall(PhaseCompletion, no tool_use_id): unexpected error for a legitimate un-stubbed dispatch: %v", err)
	}
	if call.CorrelationToken != "" {
		t.Errorf("TranslateCall: CorrelationToken = %q, want empty — no tool_use_id means no token to correlate by", call.CorrelationToken)
	}
	if call.ObservedResponse == "" {
		t.Errorf("TranslateCall: ObservedResponse is empty; the reply must still be captured for an un-stubbed dispatch")
	}
}

// TestTranslateCall_Completion_MalformedPayload_IsErrPayloadMalformedNeverPanic
// pins the specific error class, not merely "an error": a caller distinguishes
// "not JSON at all" from "recognisable JSON in an unrecognised shape".
func TestTranslateCall_Completion_MalformedPayload_IsErrPayloadMalformedNeverPanic(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

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
// pins that this path validates hook_event_name itself.
func TestTranslateCall_Completion_WrongEventName_IsHandleableErrorNamingTheField(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

	call, err := a.TranslateCall(domain.PhaseCompletion, payloadFixture(t, "completion_wrong_event_name.json"))
	if err == nil {
		t.Fatalf("TranslateCall(PhaseCompletion, wrong hook_event_name): expected an error, got call=%+v", call)
	}
	if !strings.Contains(err.Error(), "hook_event_name") {
		t.Errorf("TranslateCall(PhaseCompletion, wrong hook_event_name): error = %v, want it to name the offending field (hook_event_name)", err)
	}
}

// TestTranslateCall_Completion_EmptyPayload_IsHandleableErrorNeverPanic
// mirrors the same obligation for pre/post.
func TestTranslateCall_Completion_EmptyPayload_IsHandleableErrorNeverPanic(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

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
// decisive containment property: the completion event fires after the
// collaborator has already run, exactly like PhasePost. Even an outcome that
// would deny at the pre-invocation point must never reach the subject as a
// denial once translated here.
func TestTranslateOutcome_Completion_AlwaysPassesThroughNeverDenies(t *testing.T) {
	a := claudecode.New(claudecode.Options{})

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
