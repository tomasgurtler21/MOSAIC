package claudecode

// Internal tests for parseTaskMessage. This file is in package claudecode
// (not claudecode_test) because parseTaskMessage is unexported and can only
// be called from within the same package.
//
// These tests drive the prose-JSON recovery behaviour that the Stage 2
// implementation will provide. The prose-wrapped cases (ExtractionRecovered)
// will FAIL until the fallback extraction is implemented — that is the
// intended TDD RED state.
//
// NOTE: The test bodies in this file are intentionally identical to those in
// opencode/parse_task_message_test.go. Both adapters share the same
// parseTaskMessage contract, and the per-adapter duplication matches the
// duplication in translate.go. If adapter behaviour diverges in future, both
// files must be updated independently.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
)

// TestParseTaskMessage_PureJSON_ProducesExtractionParsed verifies that a
// prompt carrying only a well-formed JSON object with a valid agent_instance_id
// continues to produce ExtractionParsed. This is the happy-path contract that
// the recovery fallback must not disturb.
func TestParseTaskMessage_PureJSON_ProducesExtractionParsed(t *testing.T) {
	// Arrange
	raw := `{"agent_instance_id":"foo#1","task_description":"do something"}`

	// Act
	msg := parseTaskMessage(raw)

	// Assert
	if msg.Extraction != domain.ExtractionParsed {
		t.Errorf("parseTaskMessage(pure JSON): Extraction = %q, want %q", msg.Extraction, domain.ExtractionParsed)
	}
	if msg.AgentInstanceID != "foo#1" {
		t.Errorf("parseTaskMessage(pure JSON): AgentInstanceID = %q, want %q", msg.AgentInstanceID, "foo#1")
	}
}

// TestParseTaskMessage_ProseWrappedJSON_ProducesExtractionRecovered verifies
// that a prompt where valid JSON is embedded inside surrounding prose text
// produces ExtractionRecovered and correctly populates AgentInstanceID.
// This test will FAIL until the fallback extraction is implemented.
func TestParseTaskMessage_ProseWrappedJSON_ProducesExtractionRecovered(t *testing.T) {
	// Arrange
	raw := `some preamble {"agent_instance_id":"foo#1","task_description":"do something"} some suffix`

	// Act
	msg := parseTaskMessage(raw)

	// Assert
	if msg.Extraction != domain.ExtractionRecovered {
		t.Errorf("parseTaskMessage(prose-wrapped JSON): Extraction = %q, want %q", msg.Extraction, domain.ExtractionRecovered)
	}
	if msg.AgentInstanceID != "foo#1" {
		t.Errorf("parseTaskMessage(prose-wrapped JSON): AgentInstanceID = %q, want %q", msg.AgentInstanceID, "foo#1")
	}
}

// TestParseTaskMessage_ProseWithNoValidJSONObject_ProducesExtractionDegraded
// verifies that a prompt with surrounding prose but no extractable JSON object
// produces ExtractionDegraded, never a panic.
func TestParseTaskMessage_ProseWithNoValidJSONObject_ProducesExtractionDegraded(t *testing.T) {
	// Arrange
	raw := `this is just plain text with no JSON object at all`

	// Act
	msg := parseTaskMessage(raw)

	// Assert
	if msg.Extraction != domain.ExtractionDegraded {
		t.Errorf("parseTaskMessage(no JSON): Extraction = %q, want %q", msg.Extraction, domain.ExtractionDegraded)
	}
}

// TestParseTaskMessage_ProseWithJSONLackingAgentInstanceID_ProducesExtractionDegraded
// verifies that prose containing a JSON object that does not carry
// agent_instance_id produces ExtractionDegraded. A JSON object without the
// field is not a valid Communication Protocol dispatch message.
func TestParseTaskMessage_ProseWithJSONLackingAgentInstanceID_ProducesExtractionDegraded(t *testing.T) {
	// Arrange
	raw := `some preamble {"task_description":"do something","run_id":"abc123"} some suffix`

	// Act
	msg := parseTaskMessage(raw)

	// Assert
	if msg.Extraction != domain.ExtractionDegraded {
		t.Errorf("parseTaskMessage(JSON without agent_instance_id): Extraction = %q, want %q", msg.Extraction, domain.ExtractionDegraded)
	}
}

// TestParseTaskMessage_EmptyString_ProducesExtractionDegraded verifies that an
// empty prompt produces ExtractionDegraded. An empty string is not a valid
// dispatch message and must not panic or produce a false positive.
func TestParseTaskMessage_EmptyString_ProducesExtractionDegraded(t *testing.T) {
	// Arrange
	raw := ``

	// Act
	msg := parseTaskMessage(raw)

	// Assert
	if msg.Extraction != domain.ExtractionDegraded {
		t.Errorf("parseTaskMessage(empty string): Extraction = %q, want %q", msg.Extraction, domain.ExtractionDegraded)
	}
}

// TestParseTaskMessage_MalformedJSON_ProducesExtractionDegraded verifies that a
// prompt containing a partial/unclosed JSON object produces ExtractionDegraded.
// The fallback scanner must not treat a fragment without a closing brace as a
// successful extraction.
func TestParseTaskMessage_MalformedJSON_ProducesExtractionDegraded(t *testing.T) {
	// Arrange
	raw := `some preamble {"agent_instance_id":"foo#1"`

	// Act
	msg := parseTaskMessage(raw)

	// Assert
	if msg.Extraction != domain.ExtractionDegraded {
		t.Errorf("parseTaskMessage(malformed JSON): Extraction = %q, want %q", msg.Extraction, domain.ExtractionDegraded)
	}
}

// TestParseTaskMessage_MultipleJSONObjects_ExtractsValidOne verifies that when
// multiple JSON objects appear in prose the implementation finds the object
// carrying agent_instance_id rather than stopping at the first object it
// encounters. This pins the requirement that the scanner iterates candidates,
// not just the first-match heuristic.
func TestParseTaskMessage_MultipleJSONObjects_ExtractsValidOne(t *testing.T) {
	// Arrange — first object lacks agent_instance_id; second carries it.
	raw := `preamble {"other":"val"} {"agent_instance_id":"foo#1","task_description":"do something"} suffix`

	// Act
	msg := parseTaskMessage(raw)

	// Assert
	if msg.Extraction != domain.ExtractionRecovered {
		t.Errorf("parseTaskMessage(multiple JSON objects): Extraction = %q, want %q", msg.Extraction, domain.ExtractionRecovered)
	}
	if msg.AgentInstanceID != "foo#1" {
		t.Errorf("parseTaskMessage(multiple JSON objects): AgentInstanceID = %q, want %q", msg.AgentInstanceID, "foo#1")
	}
}
