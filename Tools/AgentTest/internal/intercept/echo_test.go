package intercept_test

import (
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/intercept"
)

// Tests for CompareEcho fence tolerance.
//
// Positive cases: a faithfully echoed response wrapped in a ```json or bare ```
// fence must be scored a match. These tests fail against the current
// implementation (TDD RED) because CompareEcho today requires bare JSON with no
// wrapper of any kind. They pass once I6.1 adds the bounded fence-stripping step.
//
// Boundary cases: prose, partial fences, wrong info strings, and fenced non-JSON
// must remain mismatches. These are regression guards that prevent the narrow
// tolerance from widening into a general prose wrapper.

// --- Positive cases (match expected) ---

func TestCompareEcho_FaithfulEchoInJsonFence_IsMatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS","result":"done"}`)
	observed := "```json\n{\"status_code\":\"SUCCESS\",\"result\":\"done\"}\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if !outcome.Match {
		t.Errorf("expected a match for faithfully echoed JSON inside a ```json fence, got Diff=%q", outcome.Diff)
	}
}

func TestCompareEcho_FaithfulEchoInBareFence_IsMatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "```\n{\"status_code\":\"SUCCESS\"}\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if !outcome.Match {
		t.Errorf("expected a match for faithfully echoed JSON inside a bare ``` fence, got Diff=%q", outcome.Diff)
	}
}

func TestCompareEcho_FencedEchoWithSurroundingWhitespace_IsMatch(t *testing.T) {
	// Leading/trailing whitespace is trimmed before fence detection, so a
	// newline before the opening fence does not prevent the match.
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "\n```json\n{\"status_code\":\"SUCCESS\"}\n```\n"

	outcome := intercept.CompareEcho(expected, observed)

	if !outcome.Match {
		t.Errorf("expected a match for fenced JSON with surrounding whitespace, got Diff=%q", outcome.Diff)
	}
}

func TestCompareEcho_FencedEcho_KeyOrderAndWhitespaceIgnored(t *testing.T) {
	// Structural comparison ignores key order and insignificant whitespace
	// inside the JSON body, even when the body is delivered inside a fence.
	expected := json.RawMessage(`{"status_code":"SUCCESS","detail":"ok"}`)
	observed := "```json\n{  \"detail\":  \"ok\",  \"status_code\":  \"SUCCESS\"  }\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if !outcome.Match {
		t.Errorf("expected a match for fenced JSON with different key order and whitespace, got Diff=%q", outcome.Diff)
	}
}

// --- Boundary cases (mismatch expected) ---

func TestCompareEcho_ProseBeforeOpeningFence_IsMismatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "Here is the response:\n```json\n{\"status_code\":\"SUCCESS\"}\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("prose before the opening fence must be scored a mismatch, but got Match=true")
	}
}

func TestCompareEcho_ProseAfterClosingFence_IsMismatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "```json\n{\"status_code\":\"SUCCESS\"}\n```\nHope that helps."

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("prose after the closing fence must be scored a mismatch, but got Match=true")
	}
}

func TestCompareEcho_OpeningFenceWithNoClosingFence_IsMismatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "```json\n{\"status_code\":\"SUCCESS\"}"

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("an opening fence with no closing fence must be scored a mismatch, but got Match=true")
	}
}

func TestCompareEcho_ClosingFenceWithNoOpeningFence_IsMismatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "{\"status_code\":\"SUCCESS\"}\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("a closing fence with no opening fence must be scored a mismatch, but got Match=true")
	}
}

func TestCompareEcho_FenceAroundNonJSON_IsMismatch(t *testing.T) {
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "```json\nnot valid json content\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("a code fence wrapping non-JSON content must be scored a mismatch, but got Match=true")
	}
}

func TestCompareEcho_InfoStringUppercaseJSON_IsMismatch(t *testing.T) {
	// The info string comparison is case-sensitive: ```JSON is not tolerated.
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "```JSON\n{\"status_code\":\"SUCCESS\"}\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("info string ```JSON (uppercase) must be scored a mismatch — the tolerance is case-sensitive and accepts only exactly ```json, but got Match=true")
	}
}

func TestCompareEcho_InfoStringJsonc_IsMismatch(t *testing.T) {
	// Only exactly "json" or empty info string is tolerated; "jsonc" is not.
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "```jsonc\n{\"status_code\":\"SUCCESS\"}\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("info string ```jsonc must be scored a mismatch — only exactly ```json or bare ``` is tolerated, but got Match=true")
	}
}

func TestCompareEcho_InfoStringWithTrailingSpace_IsMismatch(t *testing.T) {
	// An info string with trailing whitespace beyond what line-trimming covers
	// is not tolerated. The spec says "exactly ```json" after trimming the
	// line's own trailing whitespace, so a space between "json" and the
	// newline is still ```json (trimmed). But "```json extra" is not.
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "```json extra\n{\"status_code\":\"SUCCESS\"}\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("an info string of ```json extra must be scored a mismatch — only exactly ```json is tolerated, but got Match=true")
	}
}

func TestCompareEcho_EmptyObservedString_IsMismatch(t *testing.T) {
	// An empty response is not valid JSON and must be a mismatch regardless
	// of the expected value.
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := ""

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("an empty observed string must be scored a mismatch, but got Match=true")
	}
}

func TestCompareEcho_ObservedValueDiffersFromExpected_IsMismatch(t *testing.T) {
	// Even when the fence is stripped successfully, a value mismatch is still
	// a mismatch. The fence tolerance does not widen what counts as equal.
	expected := json.RawMessage(`{"status_code":"SUCCESS"}`)
	observed := "```json\n{\"status_code\":\"FAILURE\"}\n```"

	outcome := intercept.CompareEcho(expected, observed)

	if outcome.Match {
		t.Error("a fenced JSON value that differs from the expected stub must be scored a mismatch, but got Match=true")
	}
}
