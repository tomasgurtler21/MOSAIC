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
//   - A tool_use event with part.tool == "task" and part.state.status == "completed"
//     has its <task_result>-wrapped payload extracted as the accumulated text.
//   - A tool_use event with part.tool == "task" but status != "completed"
//     contributes nothing.
//   - A completed task tool_use event interleaved with text events: the task
//     result text is accumulated alongside any surrounding text events.
//   - A completed task tool_use event whose output has no <task_result> tags
//     falls through gracefully: the raw output is accumulated unchanged.

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

// ocTaskToolUseLine produces a tool_use JSONL line for a completed task-tool
// event. The output parameter is the raw string placed into part.state.output,
// which for real subagent results is a <task_result>...</task_result>-wrapped
// Communication Protocol JSON.
func ocTaskToolUseLine(output string) string {
	return `{"type":"tool_use","timestamp":0,"sessionID":"s1","part":{"id":"p4","callID":"c2","tool":"task",` +
		`"state":{"status":"completed","output":` + jsonQuote(output) + `}}}`
}

// ocTaskToolUseLineStatus produces a tool_use JSONL line for a task-tool event
// with an arbitrary status. Use this to test non-completed task events.
func ocTaskToolUseLineStatus(status, output string) string {
	return `{"type":"tool_use","timestamp":0,"sessionID":"s1","part":{"id":"p5","callID":"c3","tool":"task",` +
		`"state":{"status":` + jsonQuote(status) + `,"output":` + jsonQuote(output) + `}}}`
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

// ---------------------------------------------------------------------------
// task tool_use event extraction
// ---------------------------------------------------------------------------

// A completed task tool_use event whose output is wrapped in <task_result>
// tags must have those tags stripped and the inner content returned as the
// accumulated text. This is the primary subagent result delivery path for
// OpenCode runners.
func TestParseOpenCodeEnvelope_CompletedTaskToolUse_ExtractsTaskResultPayload(t *testing.T) {
	protocolJSON := `{"agent_instance_id":"worker#1","status_code":"SUCCESS","status_message":"done"}`
	output := `<task id="t1" state="completed"><task_result>` + protocolJSON + `</task_result></task>`

	data := ocStream(
		ocStepStartLine(),
		ocTaskToolUseLine(output),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != protocolJSON {
		t.Errorf("want tag-stripped payload %q, got %q", protocolJSON, text)
	}
}

// An ordinary (non-task) tool_use event must continue to contribute nothing
// to the accumulated text. This pins the existing contract against regression
// now that the parser gains a new code path for task-tool events.
func TestParseOpenCodeEnvelope_OrdinaryToolUseEvent_StillContributesNothing(t *testing.T) {
	data := ocStream(
		ocStepStartLine(),
		ocTextLine("before"),
		ocToolUseLine(),
		ocTextLine("after"),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "beforeafter" {
		t.Errorf("want non-task tool_use to contribute nothing (text=%q), got %q", "beforeafter", text)
	}
}

// A completed task tool_use event interleaved with surrounding text events
// must accumulate its extracted payload alongside those text events. The task
// result text is written to the accumulator at the point in stream order where
// the event appears.
func TestParseOpenCodeEnvelope_TaskToolUseInterleavedWithText_AccumulatesCorrectly(t *testing.T) {
	protocolJSON := `{"agent_instance_id":"worker#2","status_code":"SUCCESS","status_message":"ok"}`
	output := `<task_result>` + protocolJSON + `</task_result>`

	data := ocStream(
		ocStepStartLine(),
		ocTextLine("prefix "),
		ocTaskToolUseLine(output),
		ocTextLine(" suffix"),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	want := "prefix " + protocolJSON + " suffix"
	if text != want {
		t.Errorf("want %q, got %q", want, text)
	}
}

// A completed task tool_use event whose output contains no <task_result> tags
// must fall through gracefully: the raw output is accumulated unchanged rather
// than returning an error.
func TestParseOpenCodeEnvelope_TaskToolUseNoTaskResultTags_AccumulatesRawOutput(t *testing.T) {
	rawOutput := `{"agent_instance_id":"worker#3","status_code":"SUCCESS","status_message":"bare"}`

	data := ocStream(
		ocStepStartLine(),
		ocTaskToolUseLine(rawOutput),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want graceful fallback (no error), got %v", err)
	}
	if text != rawOutput {
		t.Errorf("want raw output accumulated unchanged %q, got %q", rawOutput, text)
	}
}

// A task tool_use event that is NOT completed (e.g. status == "running" or
// "error") must contribute nothing to the accumulated text, preserving the
// no-contribution contract for non-terminal tool states.
func TestParseOpenCodeEnvelope_TaskToolUseNotCompleted_ContributesNothing(t *testing.T) {
	output := `<task_result>{"agent_instance_id":"worker#4","status_code":"SUCCESS","status_message":"x"}</task_result>`

	data := ocStream(
		ocStepStartLine(),
		ocTextLine("only this"),
		ocTaskToolUseLineStatus("running", output),
		ocStepFinishLine("stop"),
	)

	text, err := harness.ParseOpenCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "only this" {
		t.Errorf("want non-completed task tool_use to contribute nothing, got %q", text)
	}
}
