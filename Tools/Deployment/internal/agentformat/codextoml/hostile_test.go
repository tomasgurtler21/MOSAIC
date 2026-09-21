package codextoml_test

// hostile_test.go covers T2.4: TOML string safety for hostile body content. For each
// corpus entry the test asserts:
//  1. The emitted TOML is accepted by the standard TOML parser.
//  2. The parsed developer_instructions value is byte-identical to the original body.
//
// The parser is called directly (not through the translator's own Decode, which arrives
// in Stage 3). An empty body is not in this corpus: it fails encode loudly, covered by
// TestCodexEncode_EmptyBody_FailsWithErrEmptyBody in encode_test.go.

import (
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"
)

// hostileCase is one entry in the hostile-content corpus.
type hostileCase struct {
	name string
	body string // raw body bytes; must equal developer_instructions after round-trip
}

// hostilebodies is the hostile-content corpus. Each case exercises a body that cannot
// use a naive TOML multi-line basic string without data loss or a parse error.
//
// String form selection rule (from the encode contract):
//  1. Use multi-line basic string (""") when: contains LF, no CR, no """, no forbidden
//     control chars (only LF and TAB allowed), does not end with " or \, does not
//     begin with LF.
//  2. Otherwise use single-line basic string with full escaping.
//
// Cases that force the escaped single-line form are annotated.
var hostilebodies = []hostileCase{
	{
		// Forces escaped form: body contains """, which terminates a multi-line string.
		name: "triple_quotes",
		body: `Body with """ inside it and more text.`,
	},
	{
		// Forces escaped form: body ends with ", which would produce """" at close.
		name: "ends_with_quote",
		body: `Body that ends with a quote"`,
	},
	{
		// Forces escaped form: body ends with \, which would be an incomplete escape
		// before the closing """.
		name: "ends_with_backslash",
		body: `Body that ends with backslash\`,
	},
	{
		// Does not force escaped form on its own, but exercises correct backslash
		// handling: the literal two-char sequence \n (backslash + n) must encode as \\n
		// and round-trip to the same two chars, not a newline.
		name: "backslash_escape_lookalike",
		body: "Body with literal \\n backslash-n sequence, not a newline.",
	},
	{
		// Forces escaped form: body begins with LF, and TOML multi-line basic strings
		// strip exactly one newline after the opening delimiter. A body starting with LF
		// would have that newline stripped on decode.
		name: "leading_newline",
		body: "\nBody that starts with a newline.",
	},
	{
		// Forces escaped form: CRLF in body; multi-line basic strings forbid CR.
		name: "crlf",
		body: "Line one.\r\nLine two.\r\n",
	},
	{
		// Forces escaped form: contains a control character (vertical tab, 0x0B) that
		// TOML multi-line basic strings forbid (only LF and TAB are allowed as
		// unescaped control characters in multi-line strings).
		name: "control_char_vertical_tab",
		body: "Body with \x0B vertical tab inside.",
	},
	{
		// Forces escaped form: contains a control character (form feed, 0x0C).
		name: "control_char_form_feed",
		body: "Body with \x0C form feed inside.",
	},
	{
		// Contains only LF-terminated lines and may use the multi-line form. Trailing
		// whitespace must be preserved exactly, including the spaces.
		name: "trailing_whitespace",
		body: "Line with trailing whitespace.   \nAnother line.\n",
	},
	{
		// Whitespace-only body (not empty): must encode and round-trip correctly.
		// The body is not empty so ErrEmptyBody does not apply.
		name: "whitespace_only",
		body: "   \n   \n",
	},
	{
		// Body consisting of multiple LF-terminated lines with a backslash in the middle.
		// The backslash must be escaped as \\ to avoid being treated as an escape sequence.
		name: "multiline_with_backslash",
		body: "Line one.\nLine with \\ backslash.\nLine three.\n",
	},
	{
		// Body with a line that starts with a quote character. In multi-line basic
		// strings this is legal but exercises the boundary where a """ could appear.
		name: "line_starts_with_quote",
		body: "First line.\n\"Quoted line\".\nThird line.\n",
	},
	{
		// Body that mixes TOML-special sequences that are only significant in string
		// contexts. These must not be interpreted by the TOML parser.
		name: "toml_special_sequences",
		body: "Contains \\n \\t \\r \\u0041 escape-like sequences.\n",
	},
	{
		// Body with unicode: must round-trip byte for byte.
		name: "unicode",
		body: "Body with unicode: éàü汉字.\n",
	},
	{
		// Body with a null byte (0x00). TOML forbids null bytes even in basic strings
		// (it is a C0 control character). The encoder must use the \\u0000 escape.
		name: "null_byte",
		body: "Body with \x00 null byte.",
	},
}

// TestHostileContent_RoundTrip verifies, for each hostile-content corpus entry, that:
//  1. The emitted TOML is accepted by the standard TOML parser.
//  2. The parsed developer_instructions value equals the original body bytes exactly.
func TestHostileContent_RoundTrip(t *testing.T) {
	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup CodexTOML: %v", err)
	}
	ctx := agentformat.ArtifactContext{
		AgentKey: "hostile-test",
		Op:       agentformat.OpCreate,
	}

	for _, tc := range hostilebodies {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			canonical := []byte("---\nname: hostile-test\ndescription: d\n---\n" + tc.body)

			out, _, encErr := tr.Encode(canonical, ctx)
			if encErr != nil {
				t.Fatalf("Encode error: %v", encErr)
			}

			// 1. Emitted TOML must be accepted by the standard TOML parser.
			var m map[string]interface{}
			if tomlErr := toml.Unmarshal(out, &m); tomlErr != nil {
				t.Fatalf("Emitted TOML is not parseable: %v\nOutput:\n%s", tomlErr, out)
			}

			// 2. The developer_instructions value must be byte-identical to the original body.
			got, ok := m["developer_instructions"].(string)
			if !ok {
				t.Fatal("developer_instructions is absent or not a string in emitted TOML")
			}
			if got != tc.body {
				t.Errorf("developer_instructions round-trip mismatch:\ngot:  %q\nwant: %q", got, tc.body)
			}
		})
	}
}

// TestHostileContent_EmptyBodyNotInCorpus_FailsLoudly verifies that an empty body
// is NOT silently handled as whitespace-only: it fails encode with ErrEmptyBody. This
// test documents the corpus boundary stated in the test file header.
func TestHostileContent_EmptyBodyNotInCorpus_FailsLoudly(t *testing.T) {
	canonical := []byte("---\nname: a\ndescription: d\n---\n") // truly empty body

	tr, _ := agentformat.Lookup(formatid.CodexTOML)
	ctx := agentformat.ArtifactContext{AgentKey: "a", Op: agentformat.OpCreate}
	_, _, err := tr.Encode(canonical, ctx)
	if err == nil {
		t.Fatal("Encode with empty body returned nil error; want ErrEmptyBody (empty body is outside the hostile corpus)")
	}
}
