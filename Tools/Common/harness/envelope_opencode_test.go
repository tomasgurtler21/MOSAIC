package harness_test

// Tests for ParseOpenCodeEnvelope: parsing the `opencode run --format json`
// newline-delimited event stream and deriving a success/failure verdict from
// the stream's own content, never from a process exit code.
//
// Coverage:
//
//   - Empty and whitespace-only input return ErrEmptyResponse.
//   - Text spread across multiple "text" events is accumulated, in stream
//     order, into one string.
//   - A terminal step_finish with reason "stop" ends the stream successfully.
//   - An intermediate step_finish with reason "tool-calls" does not end the
//     stream: parsing continues and later events still contribute.
//   - An "error" event produces ErrOpenCodeStreamError, naming the error's
//     name and message.
//   - A step_finish with a reason that is neither "stop" nor "tool-calls"
//     produces ErrOpenCodeStreamError, naming the reason.
//   - EOF with at least one decoded line but no terminal event produces
//     ErrOpenCodeStreamIncomplete, never success.
//   - A malformed (non-JSON) line inside an otherwise well-formed stream is
//     skipped without aborting and without turning a failure into a success.
//   - Input where no line decodes at all produces ErrMalformedJSON.
//   - Interleaved step_start and tool_use events are recognised and
//     contribute nothing to the accumulated text.
//   - The first terminal event wins: events after it are not read.
//   - On every error path the returned text is empty.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// helpers for building an OpenCode JSONL event stream
// ---------------------------------------------------------------------------

func ocStepStartLine() string {
	return `{"type":"step_start","timestamp":0,"sessionID":"s1","part":{"id":"p1","type":"step-start"}}`
}

func ocTextLine(text string) string {
	return `{"type":"text","timestamp":0,"sessionID":"s1","part":{"id":"p2","type":"text","text":` +
		jsonQuote(text) + `}}`
}

func ocToolUseLine() string {
	return `{"type":"tool_use","timestamp":0,"sessionID":"s1","part":{"id":"p3","callID":"c1","tool":"bash",` +
		`"state":{"status":"completed"}}}`
}

func ocStepFinishLine(reason string) string {
	return `{"type":"step_finish","timestamp":0,"sessionID":"s1","part":{"type":"step-finish","reason":` +
		jsonQuote(reason) + `}}`
}

func ocErrorLine(name, message string) string {
	return `{"type":"error","timestamp":0,"sessionID":"s1","error":{"name":` + jsonQuote(name) +
		`,"data":{"message":` + jsonQuote(message) + `}}}`
}

func ocStream(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------
// empty / whitespace input
// ---------------------------------------------------------------------------

func TestParseOpenCodeEnvelope_EmptyInput_ReturnsErrEmptyResponse(t *testing.T) {
	_, err := harness.ParseOpenCodeEnvelope([]byte(""))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestParseOpenCodeEnvelope_WhitespaceOnlyInput_ReturnsErrEmptyResponse(t *testing.T) {
	_, err := harness.ParseOpenCodeEnvelope([]byte("   \n\t  \n"))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// text accumulation
// ---------------------------------------------------------------------------

func TestParseOpenCodeEnvelope_MultipleTextEvents_AccumulatedInStreamOrder(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("Hello, "),
		ocTextLine("world"),
		ocTextLine("!"),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "Hello, world!" {
		t.Errorf("want accumulated text %q, got %q", "Hello, world!", text)
	}
}

func TestParseOpenCodeEnvelope_InterleavedToolUseEvents_ContributeNothingToText(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("start "),
		ocToolUseLine(),
		ocToolUseLine(),
		ocTextLine("end"),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "start end" {
		t.Errorf("want tool_use events to contribute nothing, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// step_finish reasons
// ---------------------------------------------------------------------------

func TestParseOpenCodeEnvelope_TerminalStepFinishStop_ReturnsAccumulatedTextAndNoError(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("final answer"),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "final answer" {
		t.Errorf("want text=%q, got %q", "final answer", text)
	}
}

func TestParseOpenCodeEnvelope_IntermediateStepFinishToolCalls_ParsingContinues(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("first "),
		ocStepFinishLine("tool-calls"),
		ocStepStartLine(),
		ocTextLine("second"),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "first second" {
		t.Errorf("want an intermediate tool-calls step_finish to not terminate parsing, got %q", text)
	}
}

func TestParseOpenCodeEnvelope_NonSuccessStepFinishReason_ReturnsErrOpenCodeStreamError(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("partial work"),
		ocStepFinishLine("aborted"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if !errors.Is(err, harness.ErrOpenCodeStreamError) {
		t.Errorf("want ErrOpenCodeStreamError, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on a stream-reported failure, got %q", text)
	}
	if err != nil && !strings.Contains(err.Error(), "aborted") {
		t.Errorf("want the error to name the reason %q, got %q", "aborted", err.Error())
	}
}

func TestParseOpenCodeEnvelope_FirstTerminalEventWins_SubsequentEventsIgnored(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("kept"),
		ocStepFinishLine("stop"),
		ocTextLine("never read"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "kept" {
		t.Errorf("want only text before the first terminal event, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// error event
// ---------------------------------------------------------------------------

func TestParseOpenCodeEnvelope_ErrorEvent_ReturnsErrOpenCodeStreamError(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("partial"),
		ocErrorLine("ToolExecutionError", "the bash tool failed"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if !errors.Is(err, harness.ErrOpenCodeStreamError) {
		t.Errorf("want ErrOpenCodeStreamError, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on an error event, got %q", text)
	}
	if err != nil {
		if !strings.Contains(err.Error(), "ToolExecutionError") {
			t.Errorf("want the error to name error.name, got %q", err.Error())
		}
		if !strings.Contains(err.Error(), "the bash tool failed") {
			t.Errorf("want the error to name error.data.message, got %q", err.Error())
		}
	}
}

// ---------------------------------------------------------------------------
// incomplete stream
// ---------------------------------------------------------------------------

func TestParseOpenCodeEnvelope_EOFWithNoTerminalEvent_ReturnsErrOpenCodeStreamIncomplete(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("hello"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if !errors.Is(err, harness.ErrOpenCodeStreamIncomplete) {
		t.Errorf("want ErrOpenCodeStreamIncomplete, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on an incomplete stream, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// malformed lines
// ---------------------------------------------------------------------------

func TestParseOpenCodeEnvelope_MalformedLineAmongWellFormedLines_SkippedWithoutAborting(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("before "),
		"this is not valid JSON at all",
		ocTextLine("after"),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want a malformed line to be skipped rather than abort parsing, got error: %v", err)
	}
	if text != "before after" {
		t.Errorf("want the malformed line to contribute nothing, got %q", text)
	}
}

func TestParseOpenCodeEnvelope_MalformedLineThenNoTerminalEvent_ReturnsIncompleteNotSuccess(t *testing.T) {
	// AC2.6: a skipped malformed line must never convert a failure (or an
	// otherwise-ambiguous ending) into a success.
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("some text"),
		"not json either",
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err == nil {
		t.Fatal("want an error, got nil (a malformed line must not mask an incomplete stream as success)")
	}
	if !errors.Is(err, harness.ErrOpenCodeStreamIncomplete) {
		t.Errorf("want ErrOpenCodeStreamIncomplete, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text, got %q", text)
	}
}

func TestParseOpenCodeEnvelope_NoLineDecodesAtAll_ReturnsErrMalformedJSON(t *testing.T) {
	data := ocStream(
		"not json",
		"still not json",
		"nope",
	)

	_, err := harness.ParseOpenCodeEnvelope(data)

	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON when nothing in the stream decodes, got %v", err)
	}
}

func TestParseOpenCodeEnvelope_BlankLinesAmongEvents_Skipped(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		"",
		ocTextLine("content"),
		"",
		ocStepFinishLine("stop"),
		"",
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want blank lines to be skipped without error, got %v", err)
	}
	if text != "content" {
		t.Errorf("want text=%q, got %q", "content", text)
	}
}
