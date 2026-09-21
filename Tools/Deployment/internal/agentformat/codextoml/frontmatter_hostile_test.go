package codextoml_test

// frontmatter_hostile_test.go covers the frontmatter-value hostile corpus (T3.6).
//
// Build a corpus of TOML string values and apply each to the string-valued owned keys
// decode renders (description, name and model). For each entry assert the full chain:
//
//  1. Decode the TOML bytes.
//  2. docformat.Parse the decoded canonical bytes.
//  3. Assert the parsed field value equals the original TOML value byte for byte.
//  4. Re-encode the decoded canonical and assert the TOML value is still the original.
//
// For entries the quoting policy declares unrepresentable (a control character other
// than LF, CR or TAB), assert ErrUnrepresentableValue is returned from Decode instead.
//
// Testing by re-parsing catches the class of defect that a "YAML looks right" check
// would miss: a value that renders as YAML but re-parses to a different Go string.
//
// The corpus entries correspond to the T3.6 requirement: embedded LF/CRLF/TAB, a
// control character outside the representable set, leading '#', embedded ': ', leading
// and trailing spaces, double quote, backslash, backslash-before-quote, single quote,
// multi-byte unicode, empty string, all YAML-reserved spellings (including y, n,
// .inf, .nan, 0o/0b forms), all TOML-reserved spellings, and date/time forms.

import (
	"errors"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-common/docformat"

	"mosaic-deploy/internal/agentformat"
)

// fmCorpusCase is one entry in the frontmatter hostile corpus.
type fmCorpusCase struct {
	// name is the test case identifier.
	name string
	// tomlValue is the TOML value literal as it appears after "description = " in the
	// TOML file. It must be a valid TOML string literal (basic or literal string, single-
	// or multi-line). The TOML parser decodes it to want.
	tomlValue string
	// want is the expected Go string value: what the TOML parser would return for
	// tomlValue, and what the decode chain must preserve end-to-end.
	want string
	// unrepresentable, when true, means the value contains a control character outside
	// LF/CR/TAB that the docformat renderer cannot express. Decode must return
	// ErrUnrepresentableValue rather than producing canonical output.
	unrepresentable bool
}

// fmCorpus is the frontmatter-value hostile corpus. Each entry is a valid TOML string
// value that decode must either preserve exactly or (for unrepresentable entries) reject
// with ErrUnrepresentableValue. The comments state the specific risk each entry guards.
var fmCorpus = []fmCorpusCase{
	// --- Whitespace and special characters requiring quoting ---
	{
		// Embedded LF: the quoting policy must pre-escape to \n; the parse side must
		// recover the original LF. A plain-emitted value containing a newline would break
		// the YAML line structure and re-parse to a different (truncated) value.
		name:      "embedded_lf",
		tomlValue: `"line1\nline2"`,
		want:      "line1\nline2",
	},
	{
		// Embedded CRLF: pre-escaped to \r\n. A verbatim CRLF in YAML would either be
		// treated as a line ending or normalised by some parsers.
		name:      "embedded_crlf",
		tomlValue: `"line1\r\nline2"`,
		want:      "line1\r\nline2",
	},
	{
		// TAB: representable (pre-escaped to \t). Verified round-trips to the same tab.
		name:      "tab",
		tomlValue: `"\t"`,
		want:      "\t",
	},
	{
		// Control character 0x0B (vertical tab): outside LF/CR/TAB, not representable
		// by docformat. Decode must return ErrUnrepresentableValue.
		name:            "control_char_vt_unrepresentable",
		tomlValue:       `"\u000B"`,
		want:            "\x0B",
		unrepresentable: true,
	},
	{
		// Control character 0x01 (SOH): unrepresentable. Decode must fail.
		name:            "control_char_soh_unrepresentable",
		tomlValue:       `"\u0001"`,
		want:            "\x01",
		unrepresentable: true,
	},
	{
		// Control character 0x00 (NUL): unrepresentable. Decode must fail.
		name:            "control_char_null_unrepresentable",
		tomlValue:       `"\u0000"`,
		want:            "\x00",
		unrepresentable: true,
	},
	{
		// Leading '#': YAML treats '#' after whitespace as a comment. A plain-emitted
		// value starting with '#' would be misread as a comment by YAML parsers. Must
		// be double-quoted.
		name:      "leading_hash",
		tomlValue: `"#not-a-yaml-comment"`,
		want:      "#not-a-yaml-comment",
	},
	{
		// Embedded ': ': a colon followed by a space is the YAML mapping separator.
		// A plain-emitted value containing ': ' would be misread by YAML parsers.
		name:      "embedded_colon_space",
		tomlValue: `"key: value"`,
		want:      "key: value",
	},
	{
		// Leading space: a plain-emitted value with a leading space would be stripped
		// by YAML parsers (leading whitespace in unquoted scalars is trimmed).
		name:      "leading_space",
		tomlValue: `" leading-space"`,
		want:      " leading-space",
	},
	{
		// Trailing space: similarly, trailing whitespace in unquoted scalars is trimmed.
		name:      "trailing_space",
		tomlValue: `"trailing-space "`,
		want:      "trailing-space ",
	},
	{
		// Double quote: must be escaped as \" in the rendered YAML double-quoted scalar.
		// The docformat renderer escapes \" exactly, and the parse side understands \" .
		name:      "double_quote",
		tomlValue: `"a\"b"`,
		want:      `a"b`,
	},
	{
		// Backslash: must be escaped as \\ in the rendered YAML double-quoted scalar.
		// A verbatim backslash in a YAML double-quoted scalar starts an escape sequence.
		name:      "backslash",
		tomlValue: `"a\\b"`,
		want:      `a\b`,
	},
	{
		// Backslash immediately before a quote (the \" sequence from the encode side
		// becomes a quoting ambiguity: the YAML renderer must produce \\\", not \\").
		name:      "backslash_before_quote",
		tomlValue: `"a\\\"b"`,
		want:      "a\\\"b",
	},
	{
		// Single quote: no escaping needed in a YAML double-quoted scalar. Verified
		// for completeness.
		name:      "single_quote",
		tomlValue: `"a'b"`,
		want:      "a'b",
	},
	{
		// Leading double-quote: docformat.Parse's parseScalarBytes tests data[0] == '"'
		// and treats the whole token as a double-quoted YAML scalar, stripping the
		// surrounding quotes. A plain-emitted value starting with '"' would be
		// silently corrupted on re-parse (e.g. `"hello"` would become `hello`).
		// canEmitUnquoted must reject any value whose first byte is '"'.
		name:      "leading_double_quote",
		tomlValue: `"\"hello\""`,
		want:      `"hello"`,
	},
	{
		// Leading single-quote: YAML parsers begin a single-quoted scalar on "'".
		// A plain-emitted value starting with "'" would be mis-parsed.
		name:      "leading_single_quote",
		tomlValue: `"'value'"`,
		want:      `'value'`,
	},
	{
		// Leading '|': YAML block literal scalar indicator. A plain scalar starting
		// with '|' would be interpreted as a block-scalar header.
		name:      "leading_pipe",
		tomlValue: `"|block"`,
		want:      `|block`,
	},
	{
		// Leading '>': YAML block folded scalar indicator.
		name:      "leading_gt",
		tomlValue: `">folded"`,
		want:      `>folded`,
	},
	{
		// Leading '&': YAML anchor sigil. A plain scalar starting with '&' would be
		// parsed as an anchor definition rather than a value.
		name:      "leading_ampersand",
		tomlValue: `"&anchor"`,
		want:      `&anchor`,
	},
	{
		// Leading '*': YAML alias sigil.
		name:      "leading_asterisk",
		tomlValue: `"*alias"`,
		want:      `*alias`,
	},
	{
		// Leading '!': YAML tag indicator.
		name:      "leading_exclamation",
		tomlValue: `"!tag"`,
		want:      `!tag`,
	},
	{
		// Leading '%': YAML directive indicator (e.g. %YAML, %TAG).
		name:      "leading_percent",
		tomlValue: `"%YAML"`,
		want:      `%YAML`,
	},
	{
		// Leading '@': reserved by YAML for future use.
		name:      "leading_at",
		tomlValue: `"@value"`,
		want:      `@value`,
	},
	{
		// Leading backtick: reserved by YAML for future use.
		name:      "leading_backtick",
		tomlValue: "'`value'",
		want:      "`value",
	},
	{
		// Multi-byte unicode: must round-trip byte for byte (UTF-8 is preserved).
		name:      "multibyte_unicode",
		tomlValue: `"éàü汉字"`,
		want:      "éàü汉字",
	},
	{
		// Empty string: the quoting policy must double-quote it (the empty string is a
		// YAML-reserved spelling that would re-parse as null in some YAML parsers).
		name:      "empty_string",
		tomlValue: `""`,
		want:      "",
	},
	// --- YAML-reserved spellings ---
	{
		name: "yaml_true", tomlValue: `"true"`, want: "true",
	},
	{
		name: "yaml_false", tomlValue: `"false"`, want: "false",
	},
	{
		name: "yaml_null", tomlValue: `"null"`, want: "null",
	},
	{
		name: "yaml_tilde", tomlValue: `"~"`, want: "~",
	},
	{
		name: "yaml_zero", tomlValue: `"0"`, want: "0",
	},
	{
		// "1.5" is a YAML float spelling; must be quoted so it re-parses as a string.
		name: "yaml_float_1_5", tomlValue: `"1.5"`, want: "1.5",
	},
	{
		name: "yaml_on", tomlValue: `"on"`, want: "on",
	},
	{
		// "off" is a YAML 1.1 boolean spelling (false); must be quoted so it re-parses
		// as a string. Omitting it from isReservedSpelling would emit it unquoted and
		// cause a YAML 1.1-aware parser to re-read it as the boolean false.
		name: "yaml_off", tomlValue: `"off"`, want: "off",
	},
	{
		// "yes" is a YAML 1.1 boolean spelling (true); must be quoted so it re-parses
		// as a string.
		name: "yaml_yes", tomlValue: `"yes"`, want: "yes",
	},
	{
		// "no" is a YAML 1.1 boolean spelling (false); must be quoted.
		name: "yaml_no_bool", tomlValue: `"no"`, want: "no",
	},
	{
		// "y" and "n" are YAML 1.1 boolean spellings; included to pin the reserved set.
		name: "yaml_y", tomlValue: `"y"`, want: "y",
	},
	{
		name: "yaml_n", tomlValue: `"n"`, want: "n",
	},
	{
		// ".inf" and ".nan" are YAML float spellings.
		name: "yaml_dotinf", tomlValue: `".inf"`, want: ".inf",
	},
	{
		name: "yaml_dotnan", tomlValue: `".nan"`, want: ".nan",
	},
	{
		// YAML octal form "0o755": would re-parse as an integer.
		name: "yaml_octal", tomlValue: `"0o755"`, want: "0o755",
	},
	{
		// YAML binary form "0b1010": would re-parse as an integer.
		name: "yaml_binary", tomlValue: `"0b1010"`, want: "0b1010",
	},
	// --- TOML-reserved spellings ---
	{
		// "0x10" is a TOML hex integer literal; must be quoted so TOML re-reads it as
		// a string, not as integer 16.
		name: "toml_hex", tomlValue: `"0x10"`, want: "0x10",
	},
	{
		// "1_000" is a TOML integer with underscore separator.
		name: "toml_underscore_int", tomlValue: `"1_000"`, want: "1_000",
	},
	{
		// "+5" is a TOML positive integer literal.
		name: "toml_plus_int", tomlValue: `"+5"`, want: "+5",
	},
	{
		// "-0" is a TOML negative zero.
		name: "toml_minus_zero", tomlValue: `"-0"`, want: "-0",
	},
	{
		// "1e3" is a TOML float in exponent form.
		name: "toml_exp_float", tomlValue: `"1e3"`, want: "1e3",
	},
	{
		// "inf" is a TOML positive infinity.
		name: "toml_inf", tomlValue: `"inf"`, want: "inf",
	},
	{
		// "-inf" is a TOML negative infinity.
		name: "toml_neg_inf", tomlValue: `"-inf"`, want: "-inf",
	},
	{
		// "nan" is a TOML not-a-number.
		name: "toml_nan", tomlValue: `"nan"`, want: "nan",
	},
	{
		// "+inf" is a TOML positive infinity spelled with explicit sign; the TOML half of
		// isReservedSpelling must include it alongside "inf" and "-inf".
		name: "toml_plus_inf", tomlValue: `"+inf"`, want: "+inf",
	},
	{
		// "+nan" is a TOML not-a-number with explicit positive sign.
		name: "toml_plus_nan", tomlValue: `"+nan"`, want: "+nan",
	},
	{
		// "-nan" is a TOML not-a-number with explicit negative sign.
		name: "toml_minus_nan", tomlValue: `"-nan"`, want: "-nan",
	},
	{
		// "1979-05-27" is a TOML local-date literal.
		name: "toml_date", tomlValue: `"1979-05-27"`, want: "1979-05-27",
	},
	{
		// "07:32:00" is a TOML local-time literal.
		name: "toml_time", tomlValue: `"07:32:00"`, want: "07:32:00",
	},
	{
		// "1979-05-27T07:32:00Z" is a TOML offset-date-time literal.
		name: "toml_datetime", tomlValue: `"1979-05-27T07:32:00Z"`, want: "1979-05-27T07:32:00Z",
	},
}

// TestFrontmatterHostileCorpus_DescriptionField applies the hostile corpus to the
// description field and asserts the full decode -> parse -> value chain.
func TestFrontmatterHostileCorpus_DescriptionField(t *testing.T) {
	tr := codexTranslator(t)

	for _, tc := range fmCorpus {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Build a minimal TOML file with the hostile description value.
			tomlBytes := []byte("name = \"corpus-agent\"\ndescription = " + tc.tomlValue + "\ndeveloper_instructions = \"body\"\n")

			// Decode.
			canonical, _, err := tr.Decode(tomlBytes, decodeCtx("corpus-agent"))

			if tc.unrepresentable {
				// Expect ErrUnrepresentableValue.
				if err == nil {
					t.Fatalf("Decode returned nil error; want ErrUnrepresentableValue for unrepresentable value %q", tc.want)
				}
				if !errors.Is(err, agentformat.ErrUnrepresentableValue) {
					t.Fatalf("Decode error = %v; want ErrUnrepresentableValue", err)
				}
				var ae *agentformat.ArtifactError
				if errors.As(err, &ae) && ae.Key != "description" {
					t.Errorf("ArtifactError.Key = %q; want %q", ae.Key, "description")
				}
				return
			}

			// Representable: decode must succeed.
			if err != nil {
				t.Fatalf("Decode error for value %q: %v", tc.want, err)
			}

			// Parse the decoded canonical bytes.
			doc, parseErr := docformat.Parse(canonical)
			if parseErr != nil {
				t.Fatalf("docformat.Parse canonical: %v\nCanonical:\n%s", parseErr, canonical)
			}

			// Assert the parsed field value equals the original byte for byte.
			fv, ok := doc.Frontmatter().Get("description")
			if !ok {
				t.Fatalf("description key absent from decoded canonical")
			}
			if fv.Scalar != tc.want {
				t.Errorf("description after decode -> parse = %q; want %q (must equal original byte for byte)", fv.Scalar, tc.want)
			}

			// Re-encode and assert the TOML value is still the original.
			tomlBytes2, _, encErr := tr.Encode(canonical, codexCtx("corpus-agent"))
			if encErr != nil {
				t.Fatalf("re-Encode error: %v", encErr)
			}
			var m map[string]interface{}
			if tomlErr := toml.Unmarshal(tomlBytes2, &m); tomlErr != nil {
				t.Fatalf("re-encoded TOML is not parseable: %v\nTOML:\n%s", tomlErr, tomlBytes2)
			}
			gotDesc, ok := m["description"].(string)
			if !ok {
				t.Fatalf("description key absent or not a string in re-encoded TOML")
			}
			if gotDesc != tc.want {
				t.Errorf("description after full round trip = %q; want %q (must close the round trip)", gotDesc, tc.want)
			}
		})
	}
}

// TestFrontmatterHostileCorpus_NameField applies a representative subset of the corpus
// to the name field, which also accepts free text. The subset covers quoting-boundary
// values that are equally important for name as for description.
func TestFrontmatterHostileCorpus_NameField(t *testing.T) {
	tr := codexTranslator(t)

	// Subset: values that most commonly cause silent type confusion in YAML.
	subsetCases := []fmCorpusCase{
		{name: "yaml_true", tomlValue: `"true"`, want: "true"},
		{name: "yaml_null", tomlValue: `"null"`, want: "null"},
		{name: "yaml_zero", tomlValue: `"0"`, want: "0"},
		{name: "toml_date", tomlValue: `"1979-05-27"`, want: "1979-05-27"},
		{name: "leading_space", tomlValue: `" with-space"`, want: " with-space"},
		{name: "double_quote", tomlValue: `"a\"b"`, want: `a"b`},
	}

	for _, tc := range subsetCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Build a TOML file where name carries the hostile value.
			// Note: decode does not force name from agentKey (that is encode's job).
			tomlBytes := []byte("name = " + tc.tomlValue + "\ndescription = \"d\"\ndeveloper_instructions = \"body\"\n")

			canonical, _, err := tr.Decode(tomlBytes, decodeCtx("agent"))
			if err != nil {
				t.Fatalf("Decode error for name value %q: %v", tc.want, err)
			}

			doc, parseErr := docformat.Parse(canonical)
			if parseErr != nil {
				t.Fatalf("docformat.Parse: %v", parseErr)
			}

			fv, ok := doc.Frontmatter().Get("name")
			if !ok {
				t.Fatalf("name key absent from decoded canonical")
			}
			if fv.Scalar != tc.want {
				t.Errorf("name after decode -> parse = %q; want %q", fv.Scalar, tc.want)
			}
		})
	}
}

// TestFrontmatterHostileCorpus_ModelField applies a representative subset of the corpus
// to the model field, which also accepts free text.
func TestFrontmatterHostileCorpus_ModelField(t *testing.T) {
	tr := codexTranslator(t)

	subsetCases := []fmCorpusCase{
		{name: "yaml_true", tomlValue: `"true"`, want: "true"},
		{name: "toml_inf", tomlValue: `"inf"`, want: "inf"},
		{name: "toml_date", tomlValue: `"1979-05-27"`, want: "1979-05-27"},
		{name: "leading_hash", tomlValue: `"#model-name"`, want: "#model-name"},
		{name: "backslash", tomlValue: `"a\\b"`, want: `a\b`},
	}

	for _, tc := range subsetCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tomlBytes := []byte("name = \"a\"\ndescription = \"d\"\nmodel = " + tc.tomlValue + "\ndeveloper_instructions = \"body\"\n")

			canonical, _, err := tr.Decode(tomlBytes, decodeCtx("a"))
			if err != nil {
				t.Fatalf("Decode error for model value %q: %v", tc.want, err)
			}

			doc, parseErr := docformat.Parse(canonical)
			if parseErr != nil {
				t.Fatalf("docformat.Parse: %v", parseErr)
			}

			fv, ok := doc.Frontmatter().Get("model")
			if !ok {
				t.Fatalf("model key absent from decoded canonical")
			}
			if fv.Scalar != tc.want {
				t.Errorf("model after decode -> parse = %q; want %q", fv.Scalar, tc.want)
			}
		})
	}
}

// TestFrontmatterHostileCorpus_ReservedSpellings_SurviveAsStrings verifies the core
// reserved-spelling invariant: every entry whose TOML-file value is a reserved spelling
// (one that a YAML or TOML parser would otherwise read as a non-string type) survives
// the full chain as a string, not as a number, boolean or date.
//
// This is the assertion that turns red if isReservedSpelling misses a member -- for
// example if "y", "n", ".inf", or ".nan" are omitted from the corpus.
func TestFrontmatterHostileCorpus_ReservedSpellings_SurviveAsStrings(t *testing.T) {
	reservedCases := []struct {
		name  string
		value string
	}{
		{"true", "true"},
		{"false", "false"},
		{"null", "null"},
		{"tilde", "~"},
		{"zero", "0"},
		{"on", "on"},
		{"off", "off"},
		{"yes", "yes"},
		{"no", "no"},
		{"y", "y"},
		{"n", "n"},
		{"dotinf", ".inf"},
		{"dotnan", ".nan"},
		{"octal", "0o755"},
		{"binary", "0b1010"},
		{"hex", "0x10"},
		{"underscore_int", "1_000"},
		{"exp_float", "1e3"},
		{"toml_inf", "inf"},
		{"toml_neg_inf", "-inf"},
		{"toml_nan", "nan"},
		{"toml_plus_inf", "+inf"},
		{"toml_plus_nan", "+nan"},
		{"toml_minus_nan", "-nan"},
		{"date", "1979-05-27"},
		{"time", "07:32:00"},
		{"datetime", "1979-05-27T07:32:00Z"},
	}

	tr := codexTranslator(t)

	for _, rc := range reservedCases {
		rc := rc
		t.Run(rc.name, func(t *testing.T) {
			// Encode the value as a TOML basic string literal.
			tomlBytes := []byte("name = \"a\"\ndescription = \"" + rc.value + "\"\ndeveloper_instructions = \"body\"\n")

			canonical, _, err := tr.Decode(tomlBytes, decodeCtx("a"))
			if err != nil {
				t.Fatalf("Decode error for reserved value %q: %v", rc.value, err)
			}

			doc, parseErr := docformat.Parse(canonical)
			if parseErr != nil {
				t.Fatalf("docformat.Parse: %v\nCanonical:\n%s", parseErr, canonical)
			}

			fv, ok := doc.Frontmatter().Get("description")
			if !ok {
				t.Fatalf("description absent from canonical frontmatter")
			}
			if fv.Scalar != rc.value {
				t.Errorf("reserved value %q re-parsed as %q; must survive as a string", rc.value, fv.Scalar)
			}

			// Verify re-encoding produces the same TOML string value.
			tomlBytes2, _, encErr := tr.Encode(canonical, codexCtx("a"))
			if encErr != nil {
				t.Fatalf("re-Encode error: %v", encErr)
			}
			var m map[string]interface{}
			if tomlErr := toml.Unmarshal(tomlBytes2, &m); tomlErr != nil {
				t.Fatalf("re-encoded TOML not parseable: %v", tomlErr)
			}
			got, ok := m["description"].(string)
			if !ok {
				t.Fatalf("description not a string in re-encoded TOML")
			}
			if got != rc.value {
				t.Errorf("reserved value %q changed to %q after full round trip; must survive as a string", rc.value, got)
			}
		})
	}
}
