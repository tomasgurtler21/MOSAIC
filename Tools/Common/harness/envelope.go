// Envelope handling lives in its own file with an explicitly
// Claude-Code-shaped name and doc comment, so a second harness's envelope
// handling can sit beside it rather than being forced through it.
package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// rawOutputCap bounds how much raw CLI output is echoed into an
// unrecoverable-parse error message, so a runaway or binary-garbage payload
// stays readable in a log or TUI.
const rawOutputCap = 2000

// cliEnvelopeEntry is one element of the --output-format json array the
// Claude Code CLI emits.
type cliEnvelopeEntry struct {
	Type   string `json:"type"`
	Result string `json:"result"`
}

// cliEnvelopeObject is the single-object envelope shape the Claude Code CLI
// emits for an ordinary turn: one JSON object (not an array) carrying a
// "type" discriminator and, when present, a "result" field holding the
// assistant's raw text. Result is a pointer so a present-but-empty field can
// be told apart from an absent one.
type cliEnvelopeObject struct {
	Type   string  `json:"type"`
	Result *string `json:"result"`
}

// protocolFields is the minimal shape used to recognise a Communication
// Protocol response object without decoding its full vocabulary: presence of
// both fields is what distinguishes a protocol response from any other JSON
// object that might appear in CLI output.
type protocolFields struct {
	AgentInstanceID string `json:"agent_instance_id"`
	StatusCode      string `json:"status_code"`
}

// ParseClaudeCodeEnvelope extracts the assistant text from the CLI's JSON
// output envelope, with a defensive recovery path: when the envelope array
// itself fails to parse, a protocol response recoverable from the raw text
// is still returned.
//
// Error messages never leak Go unmarshal errors or internal type names, and
// echoed raw output is capped.
//
// Extraction strategy:
//  1. Empty (or whitespace-only) input → ErrEmptyResponse.
//  2. Try to unmarshal as a JSON array envelope:
//     a. On success → scan backward for the last "result" type entry and
//        return its text. Whether that text is itself extractable as a
//        protocol response is left to a subsequent ExtractProtocolJSON call.
//        No "result" entry at all → ErrProtocolNotExtractable.
//     b. On failure → try the single-object envelope shape: unmarshal as a
//        JSON object with a "type" discriminator and an optional "result"
//        field.
//     i. If it unmarshals and its "type" is "result" → its "result"
//        field's text is returned verbatim when present; when absent,
//        ErrProtocolNotExtractable (the recognised envelope shape carries
//        no payload).
//     ii. Otherwise (not that object shape at all, e.g. a bare protocol
//        response or non-JSON garbage) → fall through to raw-text
//        recovery via ExtractProtocolJSON on the raw trimmed output text.
//        If recovery succeeds, the raw trimmed text is returned without
//        error. If recovery also fails → ErrMalformedJSON wrapping the raw
//        output text (trimmed, capped with a visible truncation indicator
//        when it exceeds the cap).
func ParseClaudeCodeEnvelope(data []byte) (string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return "", ErrEmptyResponse
	}

	var entries []cliEnvelopeEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		var obj cliEnvelopeObject
		if objErr := json.Unmarshal(data, &obj); objErr == nil && obj.Type == "result" {
			if obj.Result != nil {
				return *obj.Result, nil
			}
			return "", fmt.Errorf("%w: no result field in CLI envelope object", ErrProtocolNotExtractable)
		}

		rawText := strings.TrimSpace(string(data))
		if _, recErr := ExtractProtocolJSON(rawText); recErr == nil {
			return rawText, nil
		}

		trimmed := rawText
		if len(trimmed) > rawOutputCap {
			total := len(trimmed)
			trimmed = trimmed[:rawOutputCap] + fmt.Sprintf("... [truncated, %d bytes total]", total)
		}
		return "", fmt.Errorf("%w: %s", ErrMalformedJSON, trimmed)
	}

	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == "result" {
			return entries[i].Result, nil
		}
	}
	return "", fmt.Errorf("%w: no result entry in CLI envelope", ErrProtocolNotExtractable)
}

// ExtractProtocolJSON finds the Communication Protocol JSON object in an
// assistant text block and returns it verbatim, unparsed.
//
// It first attempts a direct unmarshal of the full (trimmed) text as the
// fast path for clean responses where the text IS the protocol JSON. If that
// fails, it scans for an embedded JSON object recognisable as a protocol
// response by the presence of both "agent_instance_id" and "status_code".
func ExtractProtocolJSON(text string) ([]byte, error) {
	trimmed := strings.TrimSpace(text)
	if looksLikeProtocolJSON(trimmed) {
		return []byte(trimmed), nil
	}

	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		for j := i + 1; j <= len(text); j++ {
			if text[j-1] != '}' {
				continue
			}
			candidate := text[i:j]
			if looksLikeProtocolJSON(candidate) {
				return []byte(candidate), nil
			}
		}
	}

	return nil, fmt.Errorf("%w: no Communication Protocol response found in text", ErrProtocolNotExtractable)
}

// looksLikeProtocolJSON reports whether candidate parses as a JSON object
// carrying both fields that identify a Communication Protocol response.
func looksLikeProtocolJSON(candidate string) bool {
	var f protocolFields
	if err := json.Unmarshal([]byte(candidate), &f); err != nil {
		return false
	}
	return f.AgentInstanceID != "" && f.StatusCode != ""
}
