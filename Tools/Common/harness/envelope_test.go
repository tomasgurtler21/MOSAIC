package harness_test

// Tests for ParseClaudeCodeEnvelope and ExtractProtocolJSON: the
// output-envelope parsing step and its defensive recovery path, ported and
// adapted from mosaic-run/internal/harness's parseEnvelope/extractProtocolResponse
// coverage.
//
// Coverage:
//
//   ExtractProtocolJSON:
//   - A bare JSON object is returned verbatim.
//   - A JSON object embedded within surrounding non-JSON text is found and
//     returned.
//   - Text with no JSON object at all is rejected (ErrProtocolNotExtractable).
//
//   ParseClaudeCodeEnvelope — normal path:
//   - Empty input returns ErrEmptyResponse.
//   - Whitespace-only input returns ErrEmptyResponse.
//   - A well-formed JSON array envelope with a single result entry returns
//     that entry's text.
//   - Scanning multiple entries picks the LAST "result" type entry.
//   - A well-formed JSON array envelope with no result entry returns
//     ErrProtocolNotExtractable.
//   - A result entry whose text is not itself extractable as a protocol
//     response returns ErrProtocolNotExtractable.
//
//   ParseClaudeCodeEnvelope — recovery path:
//   - When the envelope array fails to parse, but a protocol response is
//     recoverable from the raw text, the raw text is returned without error.
//   - When the envelope array fails to parse and nothing is recoverable,
//     ErrMalformedJSON is returned, wrapping the raw output text.
//   - The error message never contains a Go internal type name.
//   - Large unrecoverable output is truncated with a visible indicator.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-common/harness"
)

// validProtocolJSON is a minimal JSON object that a protocol-aware consumer
// would recognise as a Communication Protocol response. It is used here only
// as "a well-formed JSON object", since this package does not decode
// protocol semantics itself.
const validProtocolJSON = `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`

// ---------------------------------------------------------------------------
// ExtractProtocolJSON
// ---------------------------------------------------------------------------

func TestExtractProtocolJSON_BareObject_ReturnedVerbatim(t *testing.T) {
	got, err := harness.ExtractProtocolJSON(validProtocolJSON)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if strings.TrimSpace(string(got)) != validProtocolJSON {
		t.Errorf("want the object returned verbatim, got %q", got)
	}
}

func TestExtractProtocolJSON_EmbeddedInSurroundingText_Found(t *testing.T) {
	input := "Warning: pre-flight check failed\n" + validProtocolJSON + "\nCLI exiting\n"

	got, err := harness.ExtractProtocolJSON(input)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if !strings.Contains(string(got), `"agent_instance_id":"test-agent#1"`) {
		t.Errorf("want the embedded object recovered, got %q", got)
	}
}

func TestExtractProtocolJSON_NoObjectPresent_ReturnsErrProtocolNotExtractable(t *testing.T) {
	_, err := harness.ExtractProtocolJSON("this is complete garbage, not JSON at all")

	if !errors.Is(err, harness.ErrProtocolNotExtractable) {
		t.Errorf("want ErrProtocolNotExtractable, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ParseClaudeCodeEnvelope — normal path
// ---------------------------------------------------------------------------

func TestParseClaudeCodeEnvelope_EmptyInput_ReturnsErrEmptyResponse(t *testing.T) {
	_, err := harness.ParseClaudeCodeEnvelope([]byte(""))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestParseClaudeCodeEnvelope_WhitespaceOnlyInput_ReturnsErrEmptyResponse(t *testing.T) {
	_, err := harness.ParseClaudeCodeEnvelope([]byte("   \n\t  "))

	if !errors.Is(err, harness.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

func TestParseClaudeCodeEnvelope_ArrayWithResultEntry_ReturnsResultText(t *testing.T) {
	data := []byte(`[{"type":"result","result":` + jsonQuote(validProtocolJSON) + `}]`)

	text, err := harness.ParseClaudeCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != validProtocolJSON {
		t.Errorf("want text=%q, got %q", validProtocolJSON, text)
	}
}

func TestParseClaudeCodeEnvelope_MultipleEntries_UsesLastResultEntry(t *testing.T) {
	firstResult := `{"agent_instance_id":"first#1","status_code":"SUCCESS","status_message":"first"}`
	lastResult := `{"agent_instance_id":"last#1","status_code":"SUCCESS","status_message":"last"}`
	data := []byte(`[` +
		`{"type":"result","result":` + jsonQuote(firstResult) + `},` +
		`{"type":"assistant","result":"ignored"},` +
		`{"type":"result","result":` + jsonQuote(lastResult) + `}` +
		`]`)

	text, err := harness.ParseClaudeCodeEnvelope(data)

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if text != lastResult {
		t.Errorf("want the LAST result entry's text %q, got %q", lastResult, text)
	}
}

func TestParseClaudeCodeEnvelope_ArrayWithNoResultEntry_ReturnsErrProtocolNotExtractable(t *testing.T) {
	data := []byte(`[{"type":"assistant","message":"hello there"}]`)

	_, err := harness.ParseClaudeCodeEnvelope(data)

	if !errors.Is(err, harness.ErrProtocolNotExtractable) {
		t.Errorf("want ErrProtocolNotExtractable, got %v", err)
	}
}

func TestParseClaudeCodeEnvelope_ResultEntryTextNotExtractable_ReturnsErrProtocolNotExtractable(t *testing.T) {
	// A well-formed array with a result entry, but that entry's text isn't
	// itself extractable via ExtractProtocolJSON downstream (no JSON object
	// inside it at all). This does not have to fail inside
	// ParseClaudeCodeEnvelope itself — but its returned text must fail a
	// subsequent ExtractProtocolJSON call, which is the observable behaviour
	// this test locks down for the pair of functions together.
	data := []byte(`[{"type":"result","result":"plain prose, no JSON here"}]`)

	text, err := harness.ParseClaudeCodeEnvelope(data)
	if err != nil {
		t.Fatalf("want ParseClaudeCodeEnvelope to succeed and defer extractability to ExtractProtocolJSON, got %v", err)
	}
	if _, exErr := harness.ExtractProtocolJSON(text); !errors.Is(exErr, harness.ErrProtocolNotExtractable) {
		t.Errorf("want the returned text to fail ExtractProtocolJSON with ErrProtocolNotExtractable, got %v", exErr)
	}
}

// ---------------------------------------------------------------------------
// ParseClaudeCodeEnvelope — recovery path
// ---------------------------------------------------------------------------

func TestParseClaudeCodeEnvelope_BareObjectArrayParseFails_RecoveredAsText(t *testing.T) {
	// A bare JSON object is not a JSON array, so the envelope-array unmarshal
	// fails. The recovery path must still succeed because a protocol
	// response is recoverable from the raw text.
	text, err := harness.ParseClaudeCodeEnvelope([]byte(validProtocolJSON))

	if err != nil {
		t.Fatalf("want successful recovery, got error: %v", err)
	}
	if !strings.Contains(text, `"agent_instance_id":"test-agent#1"`) {
		t.Errorf("want the recovered text to carry the protocol object, got %q", text)
	}
}

func TestParseClaudeCodeEnvelope_EmbeddedObjectArrayParseFails_RecoveredAsText(t *testing.T) {
	input := "Warning: pre-flight check failed\n" + validProtocolJSON + "\nCLI exiting\n"

	text, err := harness.ParseClaudeCodeEnvelope([]byte(input))

	if err != nil {
		t.Fatalf("want successful recovery, got error: %v", err)
	}
	if !strings.Contains(text, `"agent_instance_id":"test-agent#1"`) {
		t.Errorf("want the recovered text to carry the protocol object, got %q", text)
	}
}

func TestParseClaudeCodeEnvelope_UnrecoverableGarbage_WrapsErrMalformedJSON(t *testing.T) {
	_, err := harness.ParseClaudeCodeEnvelope([]byte("this is complete garbage, not JSON at all"))

	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Errorf("want errors.Is(err, ErrMalformedJSON) true, got %v", err)
	}
}

func TestParseClaudeCodeEnvelope_UnrecoverableGarbage_ErrorContainsRawOutput(t *testing.T) {
	rawOutput := "this is complete garbage, not JSON at all"

	_, err := harness.ParseClaudeCodeEnvelope([]byte(rawOutput))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), rawOutput) {
		t.Errorf("want error to contain raw output %q, got %q", rawOutput, err.Error())
	}
}

func TestParseClaudeCodeEnvelope_UnrecoverableGarbage_ErrorHasNoGoTypeName(t *testing.T) {
	_, err := parseUnrecoverableGarbage(t)

	if strings.Contains(err.Error(), "cliEnvelopeEntry") {
		t.Errorf("want error free of internal Go type name, got %q", err.Error())
	}
}

func parseUnrecoverableGarbage(t *testing.T) (string, error) {
	t.Helper()
	_, err := harness.ParseClaudeCodeEnvelope([]byte("this is complete garbage, not JSON at all"))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	return "", err
}

func TestParseClaudeCodeEnvelope_LargeUnrecoverableOutput_TruncatedWithIndicator(t *testing.T) {
	largeOutput := strings.Repeat("x", 3000)

	_, err := harness.ParseClaudeCodeEnvelope([]byte(largeOutput))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, harness.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON, got %v", err)
	}
	if !strings.Contains(err.Error(), "[truncated") {
		t.Errorf("want a truncation indicator '[truncated' in the error message, got %q", err.Error())
	}
}

// jsonQuote returns s as a double-quoted, escaped JSON string literal, so
// test fixtures can embed a JSON object as the value of another JSON field
// without hand-escaping quotes.
func jsonQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
