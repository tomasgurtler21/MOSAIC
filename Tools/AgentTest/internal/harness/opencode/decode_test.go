package opencode_test

// Tests for envelope decoding: mapping a completed invocation of the
// OpenCode CLI onto the subject's result. The load-bearing property this
// file exists to pin is that a zero exit code is never treated as success
// for this harness — `opencode run` exits 0 even when the run failed, so
// DecodeEnvelope must always parse the event stream before deciding, and a
// stream that reports failure or ends without a terminal event must land on
// DispositionSpawnFailed, never DispositionCompleted.
//
// No turn-limit disposition arm is exercised here: the documented OpenCode
// event stream distinguishes no such condition, so none is mapped, and a
// test asserting one would be inventing a requirement the design forbids.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonharness "mosaic-common/harness"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/opencode"
)

// ---------------------------------------------------------------------------
// helpers for building a minimal OpenCode JSONL event stream
// ---------------------------------------------------------------------------

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func ocTextLine(text string) string {
	return `{"type":"text","part":{"type":"text","text":` + jsonQuote(text) + `}}`
}

func ocStepFinishLine(reason string) string {
	return `{"type":"step_finish","part":{"type":"step-finish","reason":` + jsonQuote(reason) + `}}`
}

func ocErrorLine(name, message string) string {
	return `{"type":"error","error":{"name":` + jsonQuote(name) + `,"data":{"message":` + jsonQuote(message) + `}}}`
}

func ocStream(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n"))
}

// successfulStream is a complete, well-formed event stream: assistant text
// followed by a terminal step_finish reporting "stop".
func successfulStream() []byte {
	return ocStream(
		ocTextLine(`{"agent_instance_id":"orchestrator#1","status_code":"SUCCESS"}`),
		ocStepFinishLine("stop"),
	)
}

// ---------------------------------------------------------------------------
// err != nil
// ---------------------------------------------------------------------------

func TestDecodeEnvelope_Timeout_DispositionTimedOut(t *testing.T) {
	result, err := opencode.DecodeEnvelope(commonharness.Response{}, commonharness.ErrTimeout)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionTimedOut {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q", result.Disposition, domain.DispositionTimedOut)
	}
}

func TestDecodeEnvelope_ExecutableMissing_DispositionSpawnFailed(t *testing.T) {
	result, err := opencode.DecodeEnvelope(commonharness.Response{}, commonharness.ErrExecutableNotFound)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionSpawnFailed {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q", result.Disposition, domain.DispositionSpawnFailed)
	}
}

// TestDecodeEnvelope_UnclassifiedError_DispositionSpawnFailed guards against
// a panic or an unclassifiable disposition when this function receives an
// error it was not written to expect.
func TestDecodeEnvelope_UnclassifiedError_DispositionSpawnFailed(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DecodeEnvelope: panicked on an unclassified error: %v", r)
		}
	}()

	result, err := opencode.DecodeEnvelope(commonharness.Response{}, errors.New("some unrelated failure"))
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}
	if result.Disposition != domain.DispositionSpawnFailed {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q for an unclassified error", result.Disposition, domain.DispositionSpawnFailed)
	}
}

// ---------------------------------------------------------------------------
// err == nil, zero exit — the stream decides everything
// ---------------------------------------------------------------------------

func TestDecodeEnvelope_ZeroExitSuccessfulStream_DispositionCompletedWithProtocolMessage(t *testing.T) {
	res := commonharness.Response{
		ExitCode: 0,
		Stdout:   successfulStream(),
		Duration: 2 * time.Second,
	}

	result, err := opencode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionCompleted {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q", result.Disposition, domain.DispositionCompleted)
	}
	if result.ProtocolMessage == "" {
		t.Errorf("DecodeEnvelope: ProtocolMessage is empty; the subject's own final protocol message must be recovered")
	}
	if result.Duration != 2*time.Second {
		t.Errorf("DecodeEnvelope: Duration = %v, want %v", result.Duration, 2*time.Second)
	}
}

// TestDecodeEnvelope_ZeroExitStreamReportsFailure_NeverDispositionCompleted
// is the single load-bearing test this file exists for: a zero exit code
// with a stream that itself reports failure (a terminal step_finish whose
// reason is neither "stop" nor "tool-calls") must never decode as
// completed, because the exit code carries no success/failure information
// for this harness.
func TestDecodeEnvelope_ZeroExitStreamReportsFailure_NeverDispositionCompleted(t *testing.T) {
	res := commonharness.Response{
		ExitCode: 0,
		Stdout:   ocStream(ocTextLine("partial output"), ocStepFinishLine("error")),
	}

	result, err := opencode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition == domain.DispositionCompleted {
		t.Errorf("DecodeEnvelope: Disposition = %q, want anything but %q — a zero exit does not mean the stream succeeded", result.Disposition, domain.DispositionCompleted)
	}
	if result.Disposition != domain.DispositionSpawnFailed {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q for a stream-reported failure", result.Disposition, domain.DispositionSpawnFailed)
	}
}

// TestDecodeEnvelope_ZeroExitStreamReportsErrorEvent_NeverDispositionCompleted
// covers the other stream-failure shape: an explicit "error" event rather
// than a non-terminal step_finish reason.
func TestDecodeEnvelope_ZeroExitStreamReportsErrorEvent_NeverDispositionCompleted(t *testing.T) {
	res := commonharness.Response{
		ExitCode: 0,
		Stdout:   ocStream(ocErrorLine("some_error", "something went wrong")),
	}

	result, err := opencode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition == domain.DispositionCompleted {
		t.Errorf("DecodeEnvelope: Disposition = %q, want anything but %q — an error event must never decode as completion", result.Disposition, domain.DispositionCompleted)
	}
}

// TestDecodeEnvelope_ZeroExitIncompleteStream_NeverDispositionCompleted
// covers a stream that ends without any terminal event: the run may have
// succeeded, failed, or been cut off mid-flight, and nothing in the output
// distinguishes them, so this must never be reported as success.
func TestDecodeEnvelope_ZeroExitIncompleteStream_NeverDispositionCompleted(t *testing.T) {
	res := commonharness.Response{
		ExitCode: 0,
		Stdout:   ocStream(ocTextLine("still working")),
	}

	result, err := opencode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.Disposition != domain.DispositionSpawnFailed {
		t.Errorf("DecodeEnvelope: Disposition = %q, want %q for an incomplete stream", result.Disposition, domain.DispositionSpawnFailed)
	}
}

func TestDecodeEnvelope_RawOutputIsCaptured(t *testing.T) {
	res := commonharness.Response{Stdout: successfulStream()}

	result, err := opencode.DecodeEnvelope(res, nil)
	if err != nil {
		t.Fatalf("DecodeEnvelope: %v", err)
	}

	if result.RawOutput == "" {
		t.Errorf("DecodeEnvelope: RawOutput is empty; the raw output must be preserved for diagnostics")
	}
}

// ---------------------------------------------------------------------------
// dispositions this function must never produce
// ---------------------------------------------------------------------------

// TestDecodeEnvelope_NeverProducesEarlyExit asserts that no combination of
// inputs this function is ever handed decodes to DispositionEarlyExit —
// that disposition belongs exclusively to the supervisor that watches the
// sentinel file, never to envelope decoding.
func TestDecodeEnvelope_NeverProducesEarlyExit(t *testing.T) {
	cases := []struct {
		name string
		res  commonharness.Response
		err  error
	}{
		{"completed", commonharness.Response{Stdout: successfulStream()}, nil},
		{"timeout", commonharness.Response{}, commonharness.ErrTimeout},
		{"stream-error", commonharness.Response{Stdout: ocStream(ocStepFinishLine("error"))}, nil},
		{"incomplete", commonharness.Response{Stdout: ocStream(ocTextLine("x"))}, nil},
		{"executable-missing", commonharness.Response{}, commonharness.ErrExecutableNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := opencode.DecodeEnvelope(tc.res, tc.err)
			if err != nil {
				t.Fatalf("DecodeEnvelope: %v", err)
			}
			if result.Disposition == domain.DispositionEarlyExit {
				t.Errorf("DecodeEnvelope(%s): Disposition = %q, early exit must never be produced by decoding", tc.name, result.Disposition)
			}
		})
	}
}

// TestDecodeEnvelope_NeverProducesTurnLimit asserts that no case this
// function is ever handed decodes to DispositionTurnLimit: the documented
// OpenCode event stream distinguishes no turn-limit condition, so none must
// be invented and mapped from some other failure.
func TestDecodeEnvelope_NeverProducesTurnLimit(t *testing.T) {
	cases := []struct {
		name string
		res  commonharness.Response
		err  error
	}{
		{"completed", commonharness.Response{Stdout: successfulStream()}, nil},
		{"stream-error", commonharness.Response{Stdout: ocStream(ocStepFinishLine("error"))}, nil},
		{"error-event", commonharness.Response{Stdout: ocStream(ocErrorLine("n", "m"))}, nil},
		{"incomplete", commonharness.Response{Stdout: ocStream(ocTextLine("x"))}, nil},
		{"timeout", commonharness.Response{}, commonharness.ErrTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := opencode.DecodeEnvelope(tc.res, tc.err)
			if err != nil {
				t.Fatalf("DecodeEnvelope: %v", err)
			}
			if result.Disposition == domain.DispositionTurnLimit {
				t.Errorf("DecodeEnvelope(%s): Disposition = %q, no turn-limit condition exists for this harness's event stream", tc.name, result.Disposition)
			}
		})
	}
}

// TestDecodeEnvelope_NeverReturnsAnError pins the function's own contract:
// it degrades every failure to a disposition rather than propagating an
// error, on every input this test exercises.
func TestDecodeEnvelope_NeverReturnsAnError(t *testing.T) {
	cases := []struct {
		name string
		res  commonharness.Response
		err  error
	}{
		{"completed", commonharness.Response{Stdout: successfulStream()}, nil},
		{"stream-error", commonharness.Response{Stdout: ocStream(ocStepFinishLine("error"))}, nil},
		{"incomplete", commonharness.Response{Stdout: ocStream(ocTextLine("x"))}, nil},
		{"timeout", commonharness.Response{}, commonharness.ErrTimeout},
		{"executable-missing", commonharness.Response{}, commonharness.ErrExecutableNotFound},
		{"unclassified", commonharness.Response{}, errors.New("unrelated")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := opencode.DecodeEnvelope(tc.res, tc.err); err != nil {
				t.Errorf("DecodeEnvelope(%s) returned a non-nil error %v; DecodeEnvelope must never fail", tc.name, err)
			}
		})
	}
}
