package harness_test

// Tests for ParseGHCPCLIEnvelope: parsing the `copilot -p ... --output-format json`
// newline-delimited JSON event stream and deriving a success/failure verdict from
// the stream's own content, with stderr and process exit code as diagnostic context only.
//
// Coverage:
//
//   - Empty and whitespace-only input return ErrEmptyResponse.
//   - Input where no line decodes as JSON at all returns ErrMalformedJSON.
//   - A result line with exitCode == 0 returns the accumulated assistant text and no error.
//   - A result line with exitCode != 0 returns ErrGHCPCLIStreamError, naming the stream
//     exit code, with trimmed stderr and the process exit code as context.
//   - A stream that ends without any result line returns ErrGHCPCLIStreamIncomplete,
//     with trimmed stderr and the process exit code as context.
//   - A malformed (non-JSON) line inside an otherwise well-formed stream is skipped
//     without aborting and without converting an incomplete stream into a success.
//   - Unknown event types are ignored without error.
//   - The first result line is terminal; lines after it are not read.
//   - On every error path the returned text is empty.
//
// Schema-trap pins (each trap would pass a naive test):
//
//   - user.message carries data.content, but that content must not appear in the
//     returned text; accumulation keys on type == "assistant.message" only.
//   - assistant.message_delta duplicates assistant.message; a stream with both must
//     return the text once, not twice.
//   - The result line's exitCode is at the line's top level, not nested under data;
//     a non-zero top-level exitCode is reported as failure regardless of data.
//   - Text spread across several assistant.message events is accumulated in stream
//     order (the multi-turn, tool-using case).
//
// Fixture conventions:
//
//   The two principal fixtures are assembled from the real captured CLI output
//   (copilot v1.0.77 on Windows) using the inline helpers below. The success fixture
//   reproduces every event type observed in a real single-turn run, in emission order,
//   proving the parser survives the full event set including all events it must ignore.
//   The failure fixture reproduces the observed failure stream (MCP-connection events,
//   then nothing, with the model error on stderr).

import (
	"errors"
	"strings"
	"testing"

	"mosaic-common/harness"
)

// ---------------------------------------------------------------------------
// helpers for building a GHCP CLI JSONL event stream
// ---------------------------------------------------------------------------

// ghcpEventLine builds a standard envelope line:
//   {"type":"<t>","data":{<dataJSON>},"id":"id1","timestamp":"2026-08-12T21:00:00Z","parentId":"pid1"}
// Pass dataJSON as the raw JSON content inside "data":{}, e.g. `"content":"HELLO"`.
// Pass ephemeral=true to add the "ephemeral":true field (transient events).
func ghcpEventLine(t, dataJSON string, ephemeral bool) string {
	line := `{"type":` + jsonQuote(t) + `,"data":{` + dataJSON + `}`
	if ephemeral {
		line += `,"ephemeral":true`
	}
	line += `,"id":"id1","timestamp":"2026-08-12T21:00:00Z","parentId":"pid1"}`
	return line
}

// ghcpSessionEvent builds a session/mcp/model event line with empty data.
func ghcpSessionEvent(t string) string {
	return ghcpEventLine(t, "", false)
}

// ghcpSessionEventEphemeral builds a session event that has ephemeral:true.
func ghcpSessionEventEphemeral(t string) string {
	return ghcpEventLine(t, "", true)
}

// ghcpUserMessageLine builds a user.message event with the given content.
// user.message is a durable event (no ephemeral).
func ghcpUserMessageLine(content string) string {
	return ghcpEventLine("user.message", `"content":`+jsonQuote(content), false)
}

// ghcpAssistantMessageDeltaLine builds an assistant.message_delta (transient, ephemeral).
// It carries deltaContent, not content.
func ghcpAssistantMessageDeltaLine(deltaContent string) string {
	return ghcpEventLine("assistant.message_delta", `"deltaContent":`+jsonQuote(deltaContent), true)
}

// ghcpAssistantMessageLine builds an assistant.message event with the given content.
// This is the complete-message event (durable, not ephemeral).
func ghcpAssistantMessageLine(content string) string {
	return ghcpEventLine("assistant.message", `"content":`+jsonQuote(content), false)
}

// ghcpResultLine builds a result event line. The result event is terminal and flat:
// exitCode sits at the top level, not nested under data; there is no id, parentId,
// or ephemeral field.
func ghcpResultLine(exitCode int) string {
	if exitCode == 0 {
		return `{"type":"result","timestamp":"2026-08-12T21:00:01Z","sessionId":"sess1","exitCode":0,"usage":{"premiumRequests":1,"totalApiDurationMs":1553,"sessionDurationMs":3945}}`
	}
	return `{"type":"result","timestamp":"2026-08-12T21:00:01Z","sessionId":"sess1","exitCode":` +
		itoa(exitCode) + `,"usage":{"premiumRequests":0,"totalApiDurationMs":500,"sessionDurationMs":1000}}`
}

// ghcpStream joins lines with newlines.
func ghcpStream(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n"))
}

// itoa converts a small int to its decimal string representation.
// Used to avoid importing strconv in the test helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ---------------------------------------------------------------------------
// Full fixtures from the captured real CLI output (copilot v1.0.77, Windows)
// ---------------------------------------------------------------------------

// ghcpFullSuccessStream builds a complete single-turn event stream reproducing
// every event type observed in the real successful capture, in emission order.
// The assistant text is "HELLO".
//
// Emission order:
//   session.mcp_servers_loaded
//   session.mcp_server_status_changed  (pending)
//   session.mcp_server_status_changed  (connected)
//   mcp.tools.list_changed
//   session.skills_loaded
//   session.tools_updated
//   user.message
//   assistant.turn_start
//   model.call_start
//   assistant.message_start
//   assistant.message_delta            ("H", then "ELLO")
//   assistant.message                  ("HELLO")
//   assistant.turn_end
//   session.usage_checkpoint
//   assistant.idle
//   result                             (exitCode 0)
func ghcpFullSuccessStream() []byte {
	return ghcpStream(
		ghcpSessionEvent("session.mcp_servers_loaded"),
		ghcpSessionEventEphemeral("session.mcp_server_status_changed"),
		ghcpSessionEventEphemeral("session.mcp_server_status_changed"),
		ghcpSessionEvent("mcp.tools.list_changed"),
		ghcpSessionEvent("session.skills_loaded"),
		ghcpSessionEvent("session.tools_updated"),
		ghcpUserMessageLine("Reply with exactly: HELLO"),
		ghcpSessionEvent("assistant.turn_start"),
		ghcpSessionEvent("model.call_start"),
		ghcpSessionEvent("assistant.message_start"),
		ghcpAssistantMessageDeltaLine("H"),
		ghcpAssistantMessageDeltaLine("ELLO"),
		ghcpAssistantMessageLine("HELLO"),
		ghcpSessionEvent("assistant.turn_end"),
		ghcpSessionEvent("session.usage_checkpoint"),
		ghcpSessionEvent("assistant.idle"),
		ghcpResultLine(0),
	)
}

// ghcpObservedFailureStream builds the failure stream observed in the real CLI
// capture when an unavailable model was specified. Stdout ends after MCP-connection
// events with no assistant.message and no result line. The model error appears only
// on stderr.
func ghcpObservedFailureStream() []byte {
	return ghcpStream(
		ghcpSessionEvent("session.mcp_servers_loaded"),
		ghcpSessionEventEphemeral("session.mcp_server_status_changed"),
		ghcpSessionEventEphemeral("session.mcp_server_status_changed"),
		ghcpSessionEvent("mcp.tools.list_changed"),
	)
}

const ghcpObservedFailureStderr = `Error: Model "definitely-not-a-real-model" from --model flag is not available.`

// ---------------------------------------------------------------------------
// empty / whitespace input
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_EmptyInput_ReturnsErrEmptyResponse(t *testing.T) {
	text, err := harness.ParseGHCPCLIEnvelope([]byte(""), nil, 0)

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on error path, got %q", text)
	}
}

func TestParseGHCPCLIEnvelope_WhitespaceOnlyInput_ReturnsErrEmptyResponse(t *testing.T) {
	text, err := harness.ParseGHCPCLIEnvelope([]byte("   \n\t  \n"), nil, 0)

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on error path, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// no decodable lines
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_NoLineDecodesAtAll_ReturnsErrMalformedJSON(t *testing.T) {
	data := ghcpStream(
		"not json",
		"still not json",
		"nope",
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON when nothing in the stream decodes, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on error path, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// successful verdict
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_ResultExitCodeZero_ReturnsAssistantTextAndNoError(t *testing.T) {
	data := ghcpStream(
		ghcpAssistantMessageLine("hello"),
		ghcpResultLine(0),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "hello" {
		t.Errorf("want text=%q, got %q", "hello", text)
	}
}

func TestParseGHCPCLIEnvelope_FullRealSuccessStream_ReturnsAssistantTextOnly(t *testing.T) {
	// Proves the parser survives the complete real event set (including all
	// events it must ignore) and returns only the assistant.message content.
	text, err := harness.ParseGHCPCLIEnvelope(ghcpFullSuccessStream(), nil, 0)

	if err != nil {
		t.Fatalf("want no error for the full real success stream, got %v", err)
	}
	if text != "HELLO" {
		t.Errorf("want text=%q from the full real stream, got %q", "HELLO", text)
	}
}

// ---------------------------------------------------------------------------
// stream-error verdict
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_ResultExitCodeNonZero_ReturnsErrGHCPCLIStreamError(t *testing.T) {
	data := ghcpStream(
		ghcpAssistantMessageLine("some text"),
		ghcpResultLine(2),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, []byte("stderr: something went wrong"), 2)

	if !errors.Is(err, harness.ErrGHCPCLIStreamError) {
		t.Errorf("want ErrGHCPCLIStreamError for non-zero stream exit code, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on error path, got %q", text)
	}
}

func TestParseGHCPCLIEnvelope_StreamError_ErrorMessageNamesStreamExitCode(t *testing.T) {
	data := ghcpStream(
		ghcpResultLine(3),
	)

	_, err := harness.ParseGHCPCLIEnvelope(data, []byte("auth failure"), 3)

	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("want error to name the stream exit code 3, got %q", err.Error())
	}
}

func TestParseGHCPCLIEnvelope_StreamError_ErrorMessageContainsStderr(t *testing.T) {
	data := ghcpStream(
		ghcpResultLine(1),
	)
	stderrContent := "authentication token expired"

	_, err := harness.ParseGHCPCLIEnvelope(data, []byte(stderrContent), 1)

	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), stderrContent) {
		t.Errorf("want error to contain stderr %q, got %q", stderrContent, err.Error())
	}
}

func TestParseGHCPCLIEnvelope_StreamError_ErrorMessageContainsProcessExitCode(t *testing.T) {
	data := ghcpStream(
		ghcpResultLine(1),
	)

	_, err := harness.ParseGHCPCLIEnvelope(data, []byte("stderr text"), 42)

	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("want error to contain process exit code 42, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// incomplete stream verdict
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_NoResultLine_ReturnsErrGHCPCLIStreamIncomplete(t *testing.T) {
	data := ghcpStream(
		ghcpAssistantMessageLine("partial text"),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if !errors.Is(err, harness.ErrGHCPCLIStreamIncomplete) {
		t.Errorf("want ErrGHCPCLIStreamIncomplete when no result line, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on error path, got %q", text)
	}
}

func TestParseGHCPCLIEnvelope_ObservedFailureStream_ReturnsErrGHCPCLIStreamIncomplete(t *testing.T) {
	// Reproduces the real failure capture: MCP events then silence, model error on stderr.
	text, err := harness.ParseGHCPCLIEnvelope(
		ghcpObservedFailureStream(),
		[]byte(ghcpObservedFailureStderr),
		1,
	)

	if !errors.Is(err, harness.ErrGHCPCLIStreamIncomplete) {
		t.Errorf("want ErrGHCPCLIStreamIncomplete for the observed failure stream, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on error path, got %q", text)
	}
}

func TestParseGHCPCLIEnvelope_Incomplete_ErrorMessageContainsStderr(t *testing.T) {
	data := ghcpStream(
		ghcpSessionEvent("session.mcp_servers_loaded"),
	)
	stderrContent := `Error: Model "bad-model" from --model flag is not available.`

	_, err := harness.ParseGHCPCLIEnvelope(data, []byte(stderrContent), 1)

	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), stderrContent) {
		t.Errorf("want error to contain stderr %q, got %q", stderrContent, err.Error())
	}
}

func TestParseGHCPCLIEnvelope_Incomplete_ErrorMessageContainsProcessExitCode(t *testing.T) {
	data := ghcpStream(
		ghcpSessionEvent("session.mcp_servers_loaded"),
	)

	_, err := harness.ParseGHCPCLIEnvelope(data, []byte("stderr"), 7)

	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !strings.Contains(err.Error(), "7") {
		t.Errorf("want error to contain process exit code 7, got %q", err.Error())
	}
}

func TestParseGHCPCLIEnvelope_ZeroProcessExitCodeButNoResultLine_ReturnsIncompleteNotSuccess(t *testing.T) {
	// The process exit code is never sufficient for a success verdict; a result
	// line is required. A zero exit with no result line is still incomplete.
	data := ghcpStream(
		ghcpAssistantMessageLine("text"),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err == nil {
		t.Fatal("want an error when no result line, even when process exit code is 0")
	}
	if !errors.Is(err, harness.ErrGHCPCLIStreamIncomplete) {
		t.Errorf("want ErrGHCPCLIStreamIncomplete, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on error path, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// malformed lines
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_MalformedLineAmongWellFormedLines_SkippedWithoutAborting(t *testing.T) {
	data := ghcpStream(
		ghcpAssistantMessageLine("before "),
		"this is not valid JSON at all",
		ghcpAssistantMessageLine("after"),
		ghcpResultLine(0),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want a malformed line to be skipped rather than abort parsing, got %v", err)
	}
	if text != "before after" {
		t.Errorf("want malformed line to contribute nothing, got %q", text)
	}
}

func TestParseGHCPCLIEnvelope_MalformedLineThenNoResultLine_ReturnsIncompleteNotSuccess(t *testing.T) {
	// A skipped malformed line must never convert a failure into a success.
	data := ghcpStream(
		ghcpAssistantMessageLine("some text"),
		"not json either",
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err == nil {
		t.Fatal("want an error (a malformed line must not mask an incomplete stream as success)")
	}
	if !errors.Is(err, harness.ErrGHCPCLIStreamIncomplete) {
		t.Errorf("want ErrGHCPCLIStreamIncomplete, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text, got %q", text)
	}
}

func TestParseGHCPCLIEnvelope_BlankLinesAmongEvents_Skipped(t *testing.T) {
	data := ghcpStream(
		"",
		ghcpAssistantMessageLine("content"),
		"",
		ghcpResultLine(0),
		"",
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want blank lines to be skipped without error, got %v", err)
	}
	if text != "content" {
		t.Errorf("want text=%q, got %q", "content", text)
	}
}

// ---------------------------------------------------------------------------
// unknown event types
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_UnknownEventType_IgnoredWithoutError(t *testing.T) {
	data := ghcpStream(
		ghcpSessionEvent("completely.unknown.type"),
		ghcpAssistantMessageLine("answer"),
		ghcpResultLine(0),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want unknown event types to be ignored without error, got %v", err)
	}
	if text != "answer" {
		t.Errorf("want text=%q, got %q", "answer", text)
	}
}

// ---------------------------------------------------------------------------
// first result line wins
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_FirstResultLineWins_SubsequentLinesNotRead(t *testing.T) {
	data := ghcpStream(
		ghcpAssistantMessageLine("kept"),
		ghcpResultLine(0),
		ghcpAssistantMessageLine("never read"),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "kept" {
		t.Errorf("want only text accumulated before the first result line, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// schema-trap pins
// ---------------------------------------------------------------------------

// Trap 1: user.message also carries data.content, but must never contribute to
// the returned text. Accumulation keys strictly on type == "assistant.message".
func TestParseGHCPCLIEnvelope_UserMessageContent_DoesNotAppearInReturnedText(t *testing.T) {
	// The user.message carries protocol-shaped content — the same scenario that
	// would cause ExtractProtocolJSON to find a fabricated response if the
	// parser accumulated on field presence rather than type.
	protocolShapedContent := `{"agent_instance_id":"fabricated#1","status_code":"SUCCESS","status_message":"fabricated"}`
	data := ghcpStream(
		ghcpUserMessageLine(protocolShapedContent),
		ghcpAssistantMessageLine("real answer"),
		ghcpResultLine(0),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "real answer" {
		t.Errorf("want only assistant.message content, got %q", text)
	}
	if strings.Contains(text, "fabricated") {
		t.Errorf("want user.message content to be excluded from returned text, got %q", text)
	}
}

// Trap 2: assistant.message_delta events duplicate assistant.message. A stream
// carrying both must return the completed text exactly once, not twice.
func TestParseGHCPCLIEnvelope_MessageDeltasAndCompletedMessage_ReturnsTextOnceNotTwice(t *testing.T) {
	data := ghcpStream(
		ghcpAssistantMessageDeltaLine("H"),
		ghcpAssistantMessageDeltaLine("ELLO"),
		ghcpAssistantMessageLine("HELLO"),
		ghcpResultLine(0),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "HELLO" {
		t.Errorf("want assistant.message_delta to contribute nothing (deltas + completed = text once), got %q", text)
	}
}

// Trap 3: the result line's exitCode is at the line's top level, not nested
// under data. A non-zero top-level exitCode must be reported as failure.
func TestParseGHCPCLIEnvelope_ResultExitCodeAtTopLevel_NonZeroIsFailure(t *testing.T) {
	// Build a result line with exitCode at the top level (as the real CLI emits).
	// The line also has a data object with no exitCode inside it, so a parser
	// that reads data.exitCode would decode zero and call the run a success.
	resultLineWithNonZeroTopLevelExitCode := `{"type":"result","timestamp":"2026-08-12T21:00:01Z","sessionId":"s1","exitCode":5,"data":{},"usage":{}}`
	data := ghcpStream(
		ghcpAssistantMessageLine("some output"),
		resultLineWithNonZeroTopLevelExitCode,
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, []byte("stderr"), 5)

	if !errors.Is(err, harness.ErrGHCPCLIStreamError) {
		t.Errorf("want ErrGHCPCLIStreamError for non-zero top-level exitCode, got %v", err)
	}
	if text != "" {
		t.Errorf("want empty text on error path, got %q", text)
	}
}

// Confirm the converse: a result line with exitCode == 0 at the top level is success.
func TestParseGHCPCLIEnvelope_ResultExitCodeZeroAtTopLevel_IsSuccess(t *testing.T) {
	resultLineWithZeroTopLevelExitCode := `{"type":"result","timestamp":"2026-08-12T21:00:01Z","sessionId":"s1","exitCode":0,"usage":{}}`
	data := ghcpStream(
		ghcpAssistantMessageLine("answer"),
		resultLineWithZeroTopLevelExitCode,
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want no error for exitCode 0 at top level, got %v", err)
	}
	if text != "answer" {
		t.Errorf("want text=%q, got %q", "answer", text)
	}
}

// Trap 4: text spread across several assistant.message events (multi-turn or
// tool-using case) is accumulated in stream order.
func TestParseGHCPCLIEnvelope_MultipleAssistantMessages_AccumulatedInStreamOrder(t *testing.T) {
	data := ghcpStream(
		ghcpAssistantMessageLine("first "),
		ghcpAssistantMessageLine("second "),
		ghcpAssistantMessageLine("third"),
		ghcpResultLine(0),
	)

	text, err := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != "first second third" {
		t.Errorf("want text accumulated in stream order, got %q", text)
	}
}

// ---------------------------------------------------------------------------
// error paths: returned text is always empty
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_AllErrorPaths_ReturnedTextIsEmpty(t *testing.T) {
	cases := []struct {
		name   string
		stdout []byte
		stderr []byte
		exit   int
	}{
		{
			name:   "empty stdout",
			stdout: []byte(""),
		},
		{
			name:   "whitespace-only stdout",
			stdout: []byte("   \n  "),
		},
		{
			name:   "no lines decode as JSON",
			stdout: []byte("not json\nstill not json"),
		},
		{
			name:   "non-zero stream exit code",
			stdout: ghcpStream(ghcpResultLine(1)),
			exit:   1,
		},
		{
			name:   "no result line",
			stdout: ghcpStream(ghcpAssistantMessageLine("partial")),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, err := harness.ParseGHCPCLIEnvelope(tc.stdout, tc.stderr, tc.exit)

			if err == nil {
				t.Fatal("want an error, got nil")
			}
			if text != "" {
				t.Errorf("want empty text on error path %q, got %q", tc.name, text)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// I/O independence
// ---------------------------------------------------------------------------

func TestParseGHCPCLIEnvelope_IdenticalInput_ProducesIdenticalOutput(t *testing.T) {
	// Purity assertion: the function is deterministic and reads nothing beyond
	// its arguments.
	data := ghcpFullSuccessStream()

	text1, err1 := harness.ParseGHCPCLIEnvelope(data, nil, 0)
	text2, err2 := harness.ParseGHCPCLIEnvelope(data, nil, 0)

	if text1 != text2 {
		t.Errorf("want identical output for identical input, got %q vs %q", text1, text2)
	}
	if (err1 == nil) != (err2 == nil) {
		t.Errorf("want identical error presence for identical input, got %v vs %v", err1, err2)
	}
}
