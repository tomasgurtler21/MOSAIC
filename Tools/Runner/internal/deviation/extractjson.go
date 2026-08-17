package deviation

import (
	"encoding/json"
	"regexp"

	"mosaic-run/internal/domain"
)

// fencedBlockRe matches a fenced code block: opening ``` with an optional
// language tag, the block content, and the closing ```. The (?s) flag makes
// . match newlines so the content group captures multi-line blocks.
var fencedBlockRe = regexp.MustCompile("(?s)```[^\n]*\n(.*?)```")

// ExtractJSONObject locates a single well-formed JSON object inside an
// orchestrator's reply and returns its bytes, ready for per-context schema
// validation. It performs no schema validation of its own: locating and
// validating are strictly separate steps, so "no instruction was found" and
// "an instruction was found but is invalid" are never conflated.
//
// Accepted shapes, all yielding the same object bytes:
//   - the reply is exactly the object, with or without surrounding whitespace
//   - the object is surrounded by prose
//   - the object is inside a fenced code block, with or without a language tag
//
// Extraction is balanced-brace, not first-brace: brace characters inside JSON
// string literals and their escape sequences do not open or close an object,
// and a brace-bearing fragment of prose that does not parse as a JSON object
// is not a candidate. The first candidate that parses as a JSON object wins.
// A fenced block, when present, is searched before the surrounding text.
//
// Returns *domain.ConsultationError{Failure: domain.ConsultFailNoInstruction}
// when the reply contains no JSON object at all. Detail names the condition;
// it does not embed the entire reply.
//
// This is the single extraction step shared by both consultation contexts.
// It is exported so it can be tested against reply shapes directly, without
// going through a consultant and a transport.
func ExtractJSONObject(reply []byte) ([]byte, error) {
	// Search fenced code blocks before the surrounding text.
	matches := fencedBlockRe.FindAllSubmatch(reply, -1)
	for _, m := range matches {
		if obj := findFirstJSONObject(m[1]); obj != nil {
			return obj, nil
		}
	}

	// Fall back to scanning the full reply.
	if obj := findFirstJSONObject(reply); obj != nil {
		return obj, nil
	}

	return nil, &domain.ConsultationError{
		Failure: domain.ConsultFailNoInstruction,
		Detail:  "reply contains no JSON object",
	}
}

// findFirstJSONObject scans data for the first balanced JSON object candidate
// that parses as a JSON object (map). Returns the object bytes, or nil if none
// is found.
func findFirstJSONObject(data []byte) []byte {
	i := 0
	for i < len(data) {
		if data[i] != '{' {
			i++
			continue
		}
		end := balancedObjectEnd(data, i)
		if end < 0 {
			i++
			continue
		}
		candidate := data[i : end+1]
		// Verify the candidate is a JSON object (not just a balanced brace
		// fragment that happens to be syntactically plausible but semantically
		// wrong). Unmarshal into a map so arrays and primitives are rejected.
		var obj map[string]json.RawMessage
		if json.Unmarshal(candidate, &obj) == nil {
			return candidate
		}
		i++
	}
	return nil
}

// balancedObjectEnd returns the index of the closing '}' that ends the JSON
// object beginning at data[start]. String literals and their escape sequences
// are tracked so that brace characters inside strings do not affect the depth
// counter. Returns -1 if no balanced closing brace is found.
func balancedObjectEnd(data []byte, start int) int {
	if start >= len(data) || data[start] != '{' {
		return -1
	}
	depth := 0
	i := start
	for i < len(data) {
		switch data[i] {
		case '"':
			// Enter string literal: advance past the opening quote and skip
			// to the matching closing quote, honouring backslash escapes.
			i++
			for i < len(data) {
				if data[i] == '\\' {
					i += 2 // skip the escape sequence character
					continue
				}
				if data[i] == '"' {
					break // closing quote found
				}
				i++
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}
