package claudecode_test

// Tests for ClaudeCodeAdapter CLI output parsing and envelope recovery.
// Uses the fake-CLI helper process defined in helperprocess_test.go.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness/claudecode"
	"mosaic-run/internal/harness/cliexec"
)

// ---------------------------------------------------------------------------
// CLI output parsing
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_ValidEnvelope_ReturnsParsedResponse verifies that a
// valid --output-format json envelope containing a Communication Protocol
// response is correctly parsed into a domain.ProtocolResponse.
func TestClaudeCodeAdapter_ValidEnvelope_ReturnsParsedResponse(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "success")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
}

// TestClaudeCodeAdapter_EmptyOutput_ReturnsErrEmptyResponse verifies that
// empty CLI stdout returns ErrEmptyResponse.
func TestClaudeCodeAdapter_EmptyOutput_ReturnsErrEmptyResponse(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "empty")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrEmptyResponse) {
		t.Errorf("want ErrEmptyResponse, got %v", err)
	}
}

// TestClaudeCodeAdapter_NonJSONOutput_ReturnsErrMalformedJSON verifies that
// CLI stdout that is not valid JSON returns ErrMalformedJSON.
func TestClaudeCodeAdapter_NonJSONOutput_ReturnsErrMalformedJSON(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "bad-json")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON, got %v", err)
	}
}

// TestClaudeCodeAdapter_ValidJSONNoProtocolResponse_ReturnsErrMalformedOutput
// verifies that valid JSON CLI output with no extractable Communication Protocol
// response returns ErrMalformedOutput.
func TestClaudeCodeAdapter_ValidJSONNoProtocolResponse_ReturnsErrMalformedOutput(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "bad-envelope")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrMalformedOutput) {
		t.Errorf("want ErrMalformedOutput, got %v", err)
	}
}

// TestClaudeCodeAdapter_NonJSONOutput_ErrorHasNoGoTypeName verifies that when
// the CLI produces output that is not valid JSON and cannot be recovered, the
// error message does not contain the internal Go type name "cliEnvelopeEntry".
// That string originates in encoding/json's unmarshal error and must be
// suppressed so the user sees a readable diagnostic.
func TestClaudeCodeAdapter_NonJSONOutput_ErrorHasNoGoTypeName(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "bad-json")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if strings.Contains(err.Error(), "cliEnvelopeEntry") {
		t.Errorf("want error message free of internal Go type name 'cliEnvelopeEntry', got: %q", err.Error())
	}
}

// TestClaudeCodeAdapter_JSONObjectWithoutProtocolFields_ReturnsErrMalformedOutput
// verifies that a bare JSON object that IS valid JSON but is missing the
// required protocol fields (agent_instance_id, status_code) is not
// recoverable and returns an error wrapping ErrMalformedJSON.
//
// This test (and the two below it) supersede what was previously direct,
// in-package coverage of the unexported parseEnvelope helper. That helper is
// moving to mosaic-common/harness, so this behaviour is now asserted only
// through Invoke — the adapter's observable behaviour — which is what keeps
// this suite compiling and passing once the adapter starts delegating.
func TestClaudeCodeAdapter_JSONObjectWithoutProtocolFields_ReturnsErrMalformedOutput(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "json-object-no-protocol-fields")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON, got %v", err)
	}
}

// TestClaudeCodeAdapter_JSONObjectWithoutProtocolFields_ErrorContainsRawOutput
// verifies that the error carries the raw CLI output for traceability.
func TestClaudeCodeAdapter_JSONObjectWithoutProtocolFields_ErrorContainsRawOutput(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "json-object-no-protocol-fields")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), `{"hello":"world","foo":"bar"}`) {
		t.Errorf("want error to contain the raw CLI output, got %q", err.Error())
	}
}

// TestClaudeCodeAdapter_LargeUnrecoverableOutput_TruncatedWithIndicator
// verifies that when unrecoverable CLI output exceeds the 2000-character
// readability cap, the error message contains a visible truncation
// indicator beginning with "[truncated".
func TestClaudeCodeAdapter_LargeUnrecoverableOutput_TruncatedWithIndicator(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "large-garbage")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	_, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if !errors.Is(err, cliexec.ErrMalformedJSON) {
		t.Errorf("want ErrMalformedJSON, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "[truncated") {
		t.Errorf("want a truncation indicator '[truncated' in the error message, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Envelope recovery integration tests
// ---------------------------------------------------------------------------

// TestClaudeCodeAdapter_BareJSONProtocolResponse_Recovered verifies that when
// the CLI outputs a bare JSON object (not a JSON array) that is a valid
// Communication Protocol response, Invoke recovers it and returns the parsed
// response rather than ErrMalformedJSON.
func TestClaudeCodeAdapter_BareJSONProtocolResponse_Recovered(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "bare-json")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want successful recovery of bare JSON protocol response, got error: %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
}

// TestClaudeCodeAdapter_EmbeddedProtocolResponseInCliText_Recovered verifies
// that when the CLI outputs surrounding non-JSON text with a valid
// Communication Protocol response embedded within it, Invoke recovers the
// protocol response and returns it rather than ErrMalformedJSON.
func TestClaudeCodeAdapter_EmbeddedProtocolResponseInCliText_Recovered(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_CMD", "embedded-json")

	adapter := claudecode.NewClaudeCodeAdapter(helperExe(t), 5*time.Second)

	resp, err := adapter.Invoke(context.Background(), ordinaryAgentRefCC(t), minimalClaudeRequest("test-agent#1"))

	if err != nil {
		t.Fatalf("want successful recovery of embedded protocol response, got error: %v", err)
	}
	if resp.StatusCode != domain.StatusSUCCESS {
		t.Errorf("want StatusCode=SUCCESS, got %q", resp.StatusCode)
	}
	if resp.AgentInstanceID != "test-agent#1" {
		t.Errorf("want AgentInstanceID=test-agent#1, got %q", resp.AgentInstanceID)
	}
}
