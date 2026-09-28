package deviation_test

// Tests for ExtractJSONObject — reply shape extraction (T4.1).

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/deviation"
	"mosaic-run/internal/domain"
)

// ===== T4.1: ExtractJSONObject — reply shapes =====

// TestExtractJSONObject_BareObject verifies that when the reply is exactly a
// well-formed JSON object (with optional surrounding whitespace), ExtractJSONObject
// returns the object's bytes and a nil error.
func TestExtractJSONObject_BareObject(t *testing.T) {
	reply := []byte(`{"action":"stop","reason":"orchestration complete"}`)

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for bare JSON object reply, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for bare JSON object reply, got empty")
	}
	// The returned bytes must be valid JSON.
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got unmarshal error: %v", jsonErr)
	}
}

// TestExtractJSONObject_BareObject_WithWhitespace verifies that surrounding
// whitespace (newlines, spaces) does not prevent extraction of a bare object.
func TestExtractJSONObject_BareObject_WithWhitespace(t *testing.T) {
	reply := []byte("\n  {\"action\":\"stop\",\"reason\":\"done\"}  \n")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for bare JSON object with surrounding whitespace, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got %v", jsonErr)
	}
}

// TestExtractJSONObject_ProseSurrounded verifies that ExtractJSONObject locates
// and returns the JSON object when it appears inside prose, ignoring the
// surrounding text.
func TestExtractJSONObject_ProseSurrounded(t *testing.T) {
	const object = `{"action":"stop","reason":"orchestration complete"}`
	reply := []byte("Based on my analysis of the orchestration artifact:\n\n" + object + "\n\nThis concludes my recommendation.")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for prose-surrounded object, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for prose-surrounded object, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got %v", jsonErr)
	}
}

// TestExtractJSONObject_FencedWithLanguageTag verifies that ExtractJSONObject
// extracts the JSON object from a fenced code block carrying a language tag
// (e.g. ```json).
func TestExtractJSONObject_FencedWithLanguageTag(t *testing.T) {
	const object = `{"action":"stop","reason":"orchestration complete"}`
	reply := []byte("Here is my routing decision:\n\n```json\n" + object + "\n```\n")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for fenced-with-language-tag object, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for fenced object, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got %v", jsonErr)
	}
}

// TestExtractJSONObject_FencedWithoutLanguageTag verifies that ExtractJSONObject
// extracts the JSON object from a fenced code block that carries no language tag.
func TestExtractJSONObject_FencedWithoutLanguageTag(t *testing.T) {
	const object = `{"action":"stop","reason":"orchestration complete"}`
	reply := []byte("Here is my routing decision:\n\n```\n" + object + "\n```\n")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for fenced-without-language-tag object, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for fenced object, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got %v", jsonErr)
	}
}

// TestExtractJSONObject_FencedBlockPrecedesProseObject verifies that when both
// a fenced code block and a different JSON object appear in the prose, the
// fenced block's object is returned. Per the design, a fenced block is searched
// before the surrounding text.
func TestExtractJSONObject_FencedBlockPrecedesProseObject(t *testing.T) {
	// The prose contains one JSON object; the fenced block contains a different one.
	// The fenced object must win because fenced blocks are searched first.
	proseObject := `{"action":"stop","reason":"from-prose"}`
	fencedObject := `{"action":"stop","reason":"from-fence"}`
	reply := []byte(proseObject + "\n\n```json\n" + fencedObject + "\n```\n")

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error when both prose and fenced objects are present, got %v", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes, got empty")
	}
	var parsed map[string]interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Fatalf("want valid JSON, got %v", jsonErr)
	}
	// The fenced object must be preferred.
	if reason, _ := parsed["reason"].(string); reason != "from-fence" {
		t.Errorf("want fenced object (reason=%q) to be preferred over prose object (reason=%q), got reason=%q",
			"from-fence", "from-prose", reason)
	}
}

// TestExtractJSONObject_NoObject verifies that a reply containing no JSON object
// at all returns *ConsultationError with ConsultFailNoInstruction.
func TestExtractJSONObject_NoObject(t *testing.T) {
	reply := []byte("The orchestrator has reviewed the run state and will advise shortly. Please stand by.")

	_, err := deviation.ExtractJSONObject(reply)

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestExtractJSONObject_EmptyReply verifies that an empty reply returns
// *ConsultationError with ConsultFailNoInstruction.
func TestExtractJSONObject_EmptyReply(t *testing.T) {
	_, err := deviation.ExtractJSONObject([]byte{})

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestExtractJSONObject_BraceBearingProse verifies that prose containing brace
// characters that do not form a valid JSON object does not produce a false
// extraction. The extractor must use balanced-brace logic, not a naive
// first-brace scan.
func TestExtractJSONObject_BraceBearingProse(t *testing.T) {
	// These brace fragments are not valid JSON objects (unquoted keys, invalid syntax).
	reply := []byte("The routing table has entries {agent-a, agent-b}. Status: {BLOCKED, reason: timeout}. No valid instruction follows.")

	_, err := deviation.ExtractJSONObject(reply)

	assertConsultationError(t, err, domain.ConsultFailNoInstruction)
}

// TestExtractJSONObject_SchemaAgnostic verifies that ExtractJSONObject returns
// the bytes of a syntactically valid JSON object even when that object does not
// conform to any consultation schema. Extraction and schema validation are
// strictly separate steps — a schema mismatch is never reported by this function.
func TestExtractJSONObject_SchemaAgnostic(t *testing.T) {
	// A valid JSON object that matches no consultation wire schema.
	reply := []byte(`{"unknown_field": "some_value", "also_unknown": 42}`)

	got, err := deviation.ExtractJSONObject(reply)

	if err != nil {
		t.Fatalf("want no error for syntactically valid JSON object (even if schema-invalid), got %v — extraction and schema validation must be strictly separate steps", err)
	}
	if len(got) == 0 {
		t.Fatal("want non-empty bytes for syntactically valid JSON object, got empty")
	}
	var parsed interface{}
	if jsonErr := json.Unmarshal(got, &parsed); jsonErr != nil {
		t.Errorf("want returned bytes to be valid JSON, got unmarshal error: %v", jsonErr)
	}
}

// TestExtractJSONObject_NoObjectDetailDoesNotEmbedFullReply verifies that the
// ConsultationError's Detail names the condition without embedding the entire
// reply, per the design contract.
func TestExtractJSONObject_NoObjectDetailDoesNotEmbedFullReply(t *testing.T) {
	const distinctPhrase = "substantial-analysis-is-required-marker"
	reply := []byte("The orchestrator has reviewed the run state. " + distinctPhrase + " and needs more time before deciding.")

	_, err := deviation.ExtractJSONObject(reply)

	if err == nil {
		t.Fatal("want error for reply with no JSON object, got nil")
	}
	var ce *domain.ConsultationError
	if !errors.As(err, &ce) {
		t.Fatalf("want *ConsultationError, got %T: %v", err, err)
	}
	// Detail must name the condition without embedding the full reply.
	if strings.Contains(ce.Detail, distinctPhrase) {
		t.Errorf("want Detail to not embed the full reply, got Detail=%q", ce.Detail)
	}
}
