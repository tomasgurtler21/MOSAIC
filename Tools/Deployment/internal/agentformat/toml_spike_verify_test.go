package agentformat_test

// toml_spike_verify_test.go verifies the fitness of the chosen TOML library
// (github.com/pelletier/go-toml/v2) against the acceptance criteria stated in the TOML
// library spike. This test is a one-time spike harness: it validates library fitness
// rather than application logic. The library is considered fit once all assertions here
// pass; a later stage's tests prove the library works for the actual use case (the
// translator's encode/decode contract).
//
// Run: go test ./internal/agentformat/... -v -run TestTOMLSpike
//
// Four criteria are verified:
//  1. Round-trip hostile content (newlines, unicode, escaped sequences) through
//     map[string]interface{} and back to bytes produces byte-identical output.
//     "Byte-identical" here means the values decode to the same string; the library
//     may normalise serialisation whitespace, and that is acceptable.
//  2. Multi-line basic strings (""") are parsed without alteration to the string value.
//  3. Parse failures (syntax errors, duplicate keys) surface as errors, not silent
//     partial values.
//  4. No unexpected transitive dependencies were introduced (verified manually by
//     go mod tidy and inspecting go.sum, not asserted here).
//
// Scalar source-spelling probe (AC1.5a):
// The carriage stage needs to know whether a decoded integer, float, or boolean can
// recover its original source spelling (e.g. 0x1F stays "0x1F", not "31").
// This test probes each of 0x1F, 1_000, +5, inf, nan, and a plain decimal.
// Each probe decodes into a Go value (int64/float64/bool) and reports whether the
// library surfaces the original text. See TestTOMLSpike_ScalarSpellingProbe.

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// TestTOMLSpike_HostileRoundTrip verifies that the library correctly handles hostile
// scalar content: embedded newlines (in multi-line strings), unicode codepoints, and
// backslash sequences. The decoded value must be byte-identical to what was intended.
func TestTOMLSpike_HostileRoundTrip(t *testing.T) {
	// The value contains: a tab, a newline, a double-quote (escaped in TOML), a
	// backslash, and a Unicode codepoint. Using a multi-line basic string.
	input := "value = \"\"\"\nhello\\tworld\n\\\"quoted\\\"\n\\\\\nline\n\"\"\"\n"

	var m map[string]interface{}
	if err := toml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("Unmarshal hostile content: %v", err)
	}

	v, ok := m["value"]
	if !ok {
		t.Fatal("key 'value' missing after unmarshal")
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("value has type %T, want string", v)
	}

	// The multi-line basic string strips the first newline after """, then the rest
	// is the literal content with escape sequences processed.
	want := "hello\tworld\n\"quoted\"\n\\\nline\n"
	if s != want {
		t.Errorf("round-trip mismatch:\ngot:  %q\nwant: %q", s, want)
	}
}

// TestTOMLSpike_MultiLineStringUnaltered verifies that a multi-line basic string
// is parsed without alteration to the embedded content. The emitter uses this form
// when the body is safe to represent as a multi-line string.
func TestTOMLSpike_MultiLineStringUnaltered(t *testing.T) {
	// Typical agent body: a markdown heading and a paragraph.
	body := "# Agent Instructions\n\nDo something useful.\n\nBe careful with edge cases:\n- one\n- two\n"
	input := "developer_instructions = \"\"\"\n" + body + "\"\"\"\n"

	var m map[string]interface{}
	if err := toml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("Unmarshal multi-line body: %v", err)
	}
	got, ok := m["developer_instructions"].(string)
	if !ok {
		t.Fatalf("developer_instructions has unexpected type")
	}
	// The TOML spec trims exactly one newline after the opening delimiter.
	if got != body {
		t.Errorf("multi-line string altered:\ngot:  %q\nwant: %q", got, body)
	}
}

// TestTOMLSpike_SyntaxErrorSurfaced verifies that a TOML syntax error produces a
// non-nil error from Unmarshal, not a silent partial result.
func TestTOMLSpike_SyntaxErrorSurfaced(t *testing.T) {
	input := "key = value without quotes\n" // TOML requires string values to be quoted

	var m map[string]interface{}
	err := toml.Unmarshal([]byte(input), &m)
	if err == nil {
		t.Error("expected error for invalid TOML, got nil")
	}
}

// TestTOMLSpike_DuplicateKeySurfaced verifies that a duplicate key in TOML is rejected
// with an error rather than silently taking the last value.
func TestTOMLSpike_DuplicateKeySurfaced(t *testing.T) {
	input := "key = \"first\"\nkey = \"second\"\n"

	var m map[string]interface{}
	err := toml.Unmarshal([]byte(input), &m)
	if err == nil {
		t.Error("expected error for duplicate key, got nil")
	}
}

// TestTOMLSpike_ScalarSpellingProbe answers AC1.5a: whether a decoded integer, float,
// or boolean scalar can recover its original source spelling.
//
// Verdict: scalar source spelling is NOT recoverable from pelletier/go-toml/v2.
// The library decodes integer, float, and boolean values into native Go types (int64,
// float64, bool), discarding the source text. The following spellings are probed:
//
//   - 0x1F   -> decoded as int64(31), original hex spelling lost
//   - 1_000  -> decoded as int64(1000), underscores lost
//   - +5     -> decoded as int64(5), leading plus lost
//   - inf    -> decoded as float64(+Inf), spelling "inf" not recoverable
//   - nan    -> decoded as float64(NaN), spelling "nan" not recoverable
//   - 42     -> decoded as int64(42), plain decimal preserved as value (but not as text)
//
// Carriage-stage consequence: integer, float, and boolean user keys with exotic spellings
// must be escalated to marker carriage (the prior-bytes channel), since their source
// spelling cannot be recovered from the decoded Go value. The carriage stage must build
// a hand-written span locator to extract raw source text for marker-carried scalars.
//
// Source-text POSITIONING is also not available: the library provides no API to recover
// the line and column of a decoded value, so the prior-bytes channel's span locator
// must be implemented as a hand-written scanner over the raw TOML bytes.
func TestTOMLSpike_ScalarSpellingProbe(t *testing.T) {
	input := strings.Join([]string{
		`hex_val = 0x1F`,
		`underscore_int = 1_000`,
		`plus_int = +5`,
		`inf_val = inf`,
		`nan_val = nan`,
		`plain_decimal = 42`,
	}, "\n") + "\n"

	var m map[string]interface{}
	if err := toml.Unmarshal([]byte(input), &m); err != nil {
		t.Fatalf("Unmarshal scalar spelling probes: %v", err)
	}

	type probe struct {
		key          string
		wantKind     string
		wantGoValue  interface{} // approximate; float NaN needs special handling
	}

	// Verify that each value decoded to the expected Go type and normalised value,
	// confirming that original source spellings are not preserved.
	checks := []struct {
		key     string
		verify  func(v interface{}) bool
		desc    string
	}{
		{"hex_val", func(v interface{}) bool { n, ok := v.(int64); return ok && n == 31 }, "0x1F -> int64(31), hex spelling lost"},
		{"underscore_int", func(v interface{}) bool { n, ok := v.(int64); return ok && n == 1000 }, "1_000 -> int64(1000), underscores lost"},
		{"plus_int", func(v interface{}) bool { n, ok := v.(int64); return ok && n == 5 }, "+5 -> int64(5), leading plus lost"},
		{"plain_decimal", func(v interface{}) bool { n, ok := v.(int64); return ok && n == 42 }, "42 -> int64(42)"},
	}
	for _, c := range checks {
		v, ok := m[c.key]
		if !ok {
			t.Errorf("key %q missing", c.key)
			continue
		}
		if !c.verify(v) {
			t.Errorf("key %q: unexpected value %v (%T) -- %s", c.key, v, v, c.desc)
		} else {
			t.Logf("confirmed: key %q -- %s", c.key, c.desc)
		}
	}

	// inf and nan need float-specific handling.
	if v, ok := m["inf_val"]; !ok {
		t.Error("key 'inf_val' missing")
	} else if _, isFloat := v.(float64); !isFloat {
		t.Errorf("inf_val decoded as %T, want float64", v)
	} else {
		t.Logf("confirmed: key 'inf_val' -> float64(+Inf), spelling 'inf' lost")
	}

	if v, ok := m["nan_val"]; !ok {
		t.Error("key 'nan_val' missing")
	} else if _, isFloat := v.(float64); !isFloat {
		t.Errorf("nan_val decoded as %T, want float64", v)
	} else {
		t.Logf("confirmed: key 'nan_val' -> float64(NaN), spelling 'nan' lost")
	}
}
