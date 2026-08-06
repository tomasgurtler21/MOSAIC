package harness

// Direct unit tests for parseEnvelope — recovery behavior, failure paths, and
// regression guard for behavior that must not change.
//
// This file is in package harness (in-package) rather than harness_test because
// parseEnvelope is unexported. Integration-level tests that exercise the same
// paths through Invoke are in claudecode_test.go (package harness_test).
//
// Coverage:
//
//   Recovery (bare JSON object and embedded JSON object):
//   - A bare JSON object that is a valid protocol response is recovered, not rejected.
//   - A valid protocol response embedded in surrounding non-JSON CLI text is recovered.
//
//   Failure path — error shape:
//   - Unrecoverable non-JSON garbage wraps ErrMalformedJSON.
//   - Unrecoverable non-JSON garbage includes the raw CLI output text.
//   - Unrecoverable non-JSON garbage message contains no Go type name "cliEnvelopeEntry".
//   - Unrecoverable JSON object missing protocol fields wraps ErrMalformedJSON.
//   - Unrecoverable JSON object missing protocol fields includes the raw CLI output text.
//   - Unrecoverable JSON object missing protocol fields message contains no "cliEnvelopeEntry".
//   - Large unrecoverable output is truncated with a visible "[truncated" indicator.
//
//   Regression — behavior that must not change:
//   - Empty input returns ErrEmptyResponse.
//   - Whitespace-only input returns ErrEmptyResponse.
//   - A well-formed JSON array envelope with a result entry parses successfully.
//   - A well-formed JSON array envelope with no result entry returns ErrMalformedOutput.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildArrayEnvelope returns a JSON array envelope with a single "result" entry
// whose Result field is set to protocolJSON. Fails the test if marshalling fails.
func buildArrayEnvelope(t *testing.T, protocolJSON string) []byte {
	t.Helper()
	entries := []cliEnvelopeEntry{{Type: "result", Result: protocolJSON}}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("buildArrayEnvelope: json.Marshal: %v", err)
	}
	return data
}

// validProtocolJSON is a minimal JSON string that satisfies both
// agent_instance_id and status_code requirements for a recoverable response.
const validProtocolJSON = `{"agent_instance_id":"test-agent#1","status_code":"SUCCESS","status_message":"ok"}`

// ---------------------------------------------------------------------------
// Recovery tests
// ---------------------------------------------------------------------------

// TestParseEnvelope_BareProtocolResponseObject_Recovered verifies that when the
// CLI outputs a bare JSON object (not an array) that is a valid Communication
// Protocol response, parseEnvelope recovers it successfully rather than
// returning ErrMalformedJSON.
func TestParseEnvelope_BareProtocolResponseObject_Recovered(t *testing.T) {
	resp, err := parseEnvelope([]byte(validProtocolJSON))

	if err != nil {
		t.Fatalf("want successful recovery of bare JSON object, got error: %v", err)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
}

// TestParseEnvelope_EmbeddedProtocolResponseInText_Recovered verifies that when
// the CLI outputs surrounding non-JSON text with a valid Communication Protocol
// response embedded within it, parseEnvelope recovers the protocol response
// successfully rather than returning ErrMalformedJSON.
func TestParseEnvelope_EmbeddedProtocolResponseInText_Recovered(t *testing.T) {
	protocolPart := `{"agent_instance_id":"test-agent#2","status_code":"BLOCKED","status_message":"a reason"}`
	input := "Warning: something happened\n" + protocolPart + "\nMore CLI output here"

	resp, err := parseEnvelope([]byte(input))

	if err != nil {
		t.Fatalf("want successful recovery of embedded JSON object, got error: %v", err)
	}
	if resp.AgentInstanceID != "test-agent#2" {
		t.Errorf("want AgentInstanceID=test-agent#2, got %q", resp.AgentInstanceID)
	}
	if resp.StatusCode != domain.StatusBLOCKED {
		t.Errorf("want StatusCode=BLOCKED, got %q", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Failure path tests — error shape
// ---------------------------------------------------------------------------

// TestParseEnvelope_NonJSONGarbage_WrapsErrMalformedJSON verifies that
// unrecoverable non-JSON output returns an error that wraps ErrMalformedJSON
// so callers using errors.Is can distinguish it from other sentinel errors.
func TestParseEnvelope_NonJSONGarbage_WrapsErrMalformedJSON(t *testing.T) {
	_, err := parseEnvelope([]byte("this is complete garbage, not JSON at all"))

	if !errors.Is(err, ErrMalformedJSON) {
		t.Errorf("want errors.Is(err, ErrMalformedJSON) true, got error: %v", err)
	}
}

// TestParseEnvelope_NonJSONGarbage_ErrorContainsRawOutput verifies that the
// error message for unrecoverable non-JSON output includes the raw CLI output
// text, so the user can see what the CLI actually produced.
func TestParseEnvelope_NonJSONGarbage_ErrorContainsRawOutput(t *testing.T) {
	rawOutput := "this is complete garbage, not JSON at all"

	_, err := parseEnvelope([]byte(rawOutput))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), rawOutput) {
		t.Errorf("want error to contain raw output %q, got error: %q", rawOutput, err.Error())
	}
}

// TestParseEnvelope_NonJSONGarbage_ErrorHasNoGoTypeName verifies that the error
// message for unrecoverable non-JSON output does not contain the internal Go
// type name "cliEnvelopeEntry". That string originates in encoding/json's
// unmarshal error and must be suppressed so the user sees a readable diagnostic.
func TestParseEnvelope_NonJSONGarbage_ErrorHasNoGoTypeName(t *testing.T) {
	_, err := parseEnvelope([]byte("this is complete garbage, not JSON at all"))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if strings.Contains(err.Error(), "cliEnvelopeEntry") {
		t.Errorf("want error message free of internal Go type name 'cliEnvelopeEntry', got: %q", err.Error())
	}
}

// TestParseEnvelope_JSONObjectWithoutProtocolFields_WrapsErrMalformedJSON verifies
// that a JSON object missing the required protocol fields (agent_instance_id and
// status_code) is not recoverable and returns an error wrapping ErrMalformedJSON.
func TestParseEnvelope_JSONObjectWithoutProtocolFields_WrapsErrMalformedJSON(t *testing.T) {
	_, err := parseEnvelope([]byte(`{"hello":"world","foo":"bar"}`))

	if !errors.Is(err, ErrMalformedJSON) {
		t.Errorf("want errors.Is(err, ErrMalformedJSON) true, got error: %v", err)
	}
}

// TestParseEnvelope_JSONObjectWithoutProtocolFields_ErrorContainsRawOutput verifies
// that the error message for a JSON object that cannot be recovered as a protocol
// response contains the raw CLI output text.
func TestParseEnvelope_JSONObjectWithoutProtocolFields_ErrorContainsRawOutput(t *testing.T) {
	rawOutput := `{"hello":"world","foo":"bar"}`

	_, err := parseEnvelope([]byte(rawOutput))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), rawOutput) {
		t.Errorf("want error to contain raw output %q, got error: %q", rawOutput, err.Error())
	}
}

// TestParseEnvelope_JSONObjectWithoutProtocolFields_ErrorHasNoGoTypeName verifies
// that the error message for a non-array JSON object that is not recoverable does
// not contain the internal Go type name "cliEnvelopeEntry".
func TestParseEnvelope_JSONObjectWithoutProtocolFields_ErrorHasNoGoTypeName(t *testing.T) {
	_, err := parseEnvelope([]byte(`{"hello":"world","foo":"bar"}`))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if strings.Contains(err.Error(), "cliEnvelopeEntry") {
		t.Errorf("want error message free of internal Go type name 'cliEnvelopeEntry', got: %q", err.Error())
	}
}

// TestParseEnvelope_LargeUnrecoverableOutput_TruncatedWithIndicator verifies
// that when the raw CLI output exceeds the 2000-character readability cap, the
// error message contains a visible truncation indicator beginning with
// "[truncated". The full text is preserved for the debug log, but the
// user-facing error is capped for TUI readability.
func TestParseEnvelope_LargeUnrecoverableOutput_TruncatedWithIndicator(t *testing.T) {
	// Build output clearly larger than the 2000-character cap.
	largeOutput := strings.Repeat("x", 3000)

	_, err := parseEnvelope([]byte(largeOutput))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, ErrMalformedJSON) {
		t.Errorf("want errors.Is(err, ErrMalformedJSON) true, got error: %v", err)
	}
	if !strings.Contains(err.Error(), "[truncated") {
		t.Errorf("want truncation indicator '[truncated' in error message, got: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Regression tests — behavior that must not change
// ---------------------------------------------------------------------------

// TestParseEnvelope_EmptyInput_ReturnsErrEmptyResponse verifies that an empty
// byte slice still returns ErrEmptyResponse after the recovery change.
func TestParseEnvelope_EmptyInput_ReturnsErrEmptyResponse(t *testing.T) {
	_, err := parseEnvelope([]byte(""))

	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

// TestParseEnvelope_WhitespaceOnlyInput_ReturnsErrEmptyResponse verifies that
// input containing only whitespace still returns ErrEmptyResponse.
func TestParseEnvelope_WhitespaceOnlyInput_ReturnsErrEmptyResponse(t *testing.T) {
	_, err := parseEnvelope([]byte("   \n\t  "))

	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

// TestParseEnvelope_WellFormedArrayEnvelope_Parsed verifies that a well-formed
// JSON array envelope containing a result entry with a valid protocol response
// still parses successfully after the recovery change.
func TestParseEnvelope_WellFormedArrayEnvelope_Parsed(t *testing.T) {
	data := buildArrayEnvelope(t, validProtocolJSON)

	resp, err := parseEnvelope(data)

	if err != nil {
		t.Fatalf("want successful parse of array envelope, got error: %v", err)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
}

// TestParseEnvelope_ArrayEnvelopeWithNoResultEntry_ReturnsErrMalformedOutput verifies
// that a well-formed JSON array containing only non-result entries still returns
// ErrMalformedOutput after the recovery change (no result entry means the existing
// array path fails, and recovery does not apply to valid arrays).
func TestParseEnvelope_ArrayEnvelopeWithNoResultEntry_ReturnsErrMalformedOutput(t *testing.T) {
	input := `[{"type":"assistant","message":"hello there"}]`

	_, err := parseEnvelope([]byte(input))

	if !errors.Is(err, ErrMalformedOutput) {
		t.Errorf("want ErrMalformedOutput, got %v", err)
	}
}
