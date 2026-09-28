package harness_test

// Tests that Communication Protocol response recognition tolerates a mistyped
// error_code, so that a recognised response which later fails to decode is
// reported as a decode failure rather than as "not valid JSON".
//
// Coverage:
//
//   - A candidate with string agent_instance_id and status_code and a
//     non-string error_code (number, object, array, boolean) is found by
//     ExtractProtocolJSON.
//   - The same candidate is returned by the raw-text recovery path of
//     ParseClaudeCodeEnvelope instead of ErrMalformedJSON.
//   - Recognition of a BLOCKED response carrying the string error_code "E100"
//     without agent_instance_id is unchanged, and a mistyped error_code does
//     not make such an id-less candidate recognisable.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-common/harness"
)

// mistypedErrorCodeCases are protocol-shaped objects whose error_code is not a
// JSON string. Each has a valid agent_instance_id and status_code.
var mistypedErrorCodeCases = map[string]string{
	"number":  `{"agent_instance_id":"test-agent#1","status_code":"BLOCKED","status_message":"bad","error_code":501,"error_reason":"r"}`,
	"object":  `{"agent_instance_id":"test-agent#1","status_code":"BLOCKED","status_message":"bad","error_code":{"code":"E501"},"error_reason":"r"}`,
	"array":   `{"agent_instance_id":"test-agent#1","status_code":"BLOCKED","status_message":"bad","error_code":["E501"],"error_reason":"r"}`,
	"boolean": `{"agent_instance_id":"test-agent#1","status_code":"BLOCKED","status_message":"bad","error_code":true,"error_reason":"r"}`,
}

func TestExtractProtocolJSON_NonStringErrorCode_StillRecognised(t *testing.T) {
	for name, input := range mistypedErrorCodeCases {
		t.Run(name, func(t *testing.T) {
			got, err := harness.ExtractProtocolJSON(input)

			if err != nil {
				t.Fatalf("want the candidate recognised, got error %v", err)
			}
			if string(got) != input {
				t.Errorf("want the object returned verbatim, got %q", got)
			}
		})
	}
}

func TestExtractProtocolJSON_NonStringErrorCode_EmbeddedInText_StillRecognised(t *testing.T) {
	input := "note before\n" + mistypedErrorCodeCases["number"] + "\nnote after\n"

	got, err := harness.ExtractProtocolJSON(input)

	if err != nil {
		t.Fatalf("want the embedded candidate recognised, got error %v", err)
	}
	if !strings.Contains(string(got), `"error_code":501`) {
		t.Errorf("want the embedded object returned, got %q", got)
	}
}

func TestParseClaudeCodeEnvelope_NonStringErrorCode_RecoveredNotMalformedJSON(t *testing.T) {
	for name, input := range mistypedErrorCodeCases {
		t.Run(name, func(t *testing.T) {
			text, err := harness.ParseClaudeCodeEnvelope([]byte(input))

			if errors.Is(err, harness.ErrMalformedJSON) {
				t.Fatalf("want a recognised response, got ErrMalformedJSON: %v", err)
			}
			if err != nil {
				t.Fatalf("want raw-text recovery to succeed, got error %v", err)
			}
			if !strings.Contains(text, `"agent_instance_id":"test-agent#1"`) {
				t.Errorf("want the recovered text to carry the protocol object, got %q", text)
			}
		})
	}
}

func TestExtractProtocolJSON_E100WithoutAgentInstanceID_StillRecognisedAmongOtherFields(t *testing.T) {
	input := `{"status_code":"BLOCKED","status_message":"invalid","error_code":"E100","error_reason":"x","result_data":"{}"}`

	got, err := harness.ExtractProtocolJSON(input)

	if err != nil {
		t.Fatalf("want E100 rejection recognised, got error %v", err)
	}
	if string(got) != input {
		t.Errorf("want the object returned verbatim, got %q", got)
	}
}

func TestExtractProtocolJSON_NonStringErrorCodeWithoutAgentInstanceID_NotRecognised(t *testing.T) {
	input := `{"status_code":"BLOCKED","status_message":"invalid","error_code":100}`

	_, err := harness.ExtractProtocolJSON(input)

	if !errors.Is(err, harness.ErrProtocolNotExtractable) {
		t.Errorf("want ErrProtocolNotExtractable for an id-less candidate without string E100, got %v", err)
	}
}
