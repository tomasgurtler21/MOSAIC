package codextoml_test

// carriage_marker_test.go covers:
//
//   - Marker-carried complex keys via the prior-bytes channel (T4.2): nested tables
//     and arrays-of-tables decode to mosaic_carriage_markers; encode with prior bytes
//     re-emits the original text; encode without recoverable prior bytes returns
//     ErrMarkerUnrecoverable. Includes the hostile-span corpus.
//
//   - Extended lossless invariant for user-owned keys (T4.4): decode(encode(D)) ==
//     N(D) over user-owned keys as well as MOSAIC-owned keys, over multiple round
//     trips. The frontmatter-value hostile corpus is run through the carriage path
//     both as scalar user keys and as array elements.
//
//   - Owned-key resurrection guard (T4.5): a marker naming a MOSAIC-owned key must
//     never restore that key's prior value; MOSAIC's current value wins; the marker
//     is refused with EntryRefusedMarker.
//
//   - Hostile key corpus (T4.7): tests the three-way outcome (value-carried, marker-
//     carried, ErrMarkerUnrecoverable) for the full bare-key-charset rule.

import (
	"bytes"
	"errors"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-common/mosaic"

	"mosaic-deploy/internal/agentformat"
)

// ---------------------------------------------------------------------------
// T4.2: Marker-carried complex keys
// ---------------------------------------------------------------------------

// TestCarriageMarker_NestedTable_DecodesIntoMarkers verifies that a top-level user
// TOML key whose value is a nested table appears in mosaic_carriage_markers after
// decode. Top-level key naming: the entire [mcp_servers] subtree is carried under
// the top-level key name "mcp_servers".
func TestCarriageMarker_NestedTable_DecodesIntoMarkers(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\n\n[mcp_servers]\nurl = \"https://api.example.com\"\n")
	canonical := decodeToml(t, input, "a")

	markers := carriageMarkerNames(t, canonical)
	found := false
	for _, name := range markers {
		if name == "mcp_servers" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("mcp_servers absent from mosaic_carriage_markers after decode; nested-table keys must be marker-carried, got: %v", markers)
	}

	// Must NOT appear as a value-carried key.
	_, inValues := findCarriageValuePair(t, canonical, "mcp_servers")
	if inValues {
		t.Error("mcp_servers appeared in mosaic_carriage values; nested tables must be in markers only")
	}
}

// TestCarriageMarker_DottedKey_TopLevelGrouping verifies that a dotted key
// (mcp_servers.foo = "bar") is carried under the top-level key name "mcp_servers",
// not as the dotted path "mcp_servers.foo". Marker naming is by top-level key only.
func TestCarriageMarker_DottedKey_TopLevelGrouping(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\nmcp_servers.foo = \"bar\"\n")
	canonical := decodeToml(t, input, "a")

	markers := carriageMarkerNames(t, canonical)

	// Top-level key "mcp_servers" must appear in markers.
	found := false
	for _, name := range markers {
		if name == "mcp_servers" {
			found = true
		}
		// The dotted path must NOT appear as a separate marker.
		if name == "mcp_servers.foo" {
			t.Error("dotted path \"mcp_servers.foo\" appeared as a marker; only the top-level key \"mcp_servers\" should be carried")
		}
	}
	if !found {
		t.Errorf("mcp_servers absent from markers after dotted-key decode; got: %v", markers)
	}
}

// TestCarriageMarker_MarkerNamesInAscendingByteOrder verifies that the marker names
// inside mosaic_carriage_markers appear in ascending byte order.
func TestCarriageMarker_MarkerNamesInAscendingByteOrder(t *testing.T) {
	// Declare three nested tables in reverse order.
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\n[z_table]\nx = 1\n[a_table]\ny = 2\n[m_table]\nz = 3\n")
	canonical := decodeToml(t, input, "a")

	markers := carriageMarkerNames(t, canonical)
	if len(markers) < 2 {
		t.Skipf("only %d markers found; need at least 2 to test order", len(markers))
	}

	for i := 1; i < len(markers); i++ {
		if markers[i-1] >= markers[i] {
			t.Errorf("markers not in ascending byte order: %v", markers)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// T4.2: Encode with prior bytes re-emits original text
// ---------------------------------------------------------------------------

// TestCarriageMarker_Encode_ReEmitsOriginalSpan verifies that when prior deployed
// bytes are provided and a canonical document contains mosaic_carriage_markers
// naming a key, encode re-emits that key's original text from the prior bytes.
func TestCarriageMarker_Encode_ReEmitsOriginalSpan(t *testing.T) {
	// Prior bytes contain the mcp_servers table with a known structure.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\n\n[mcp_servers]\nurl = \"https://api.example.com\"\ntoken = \"secret\"\n\ndeveloper_instructions = \"body\"\n")

	// Canonical with a marker for mcp_servers.
	canonical := buildCanonicalWithMarkers(t, []string{"mcp_servers"}, "body\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	// The re-emitted output must contain the mcp_servers section.
	var m map[string]interface{}
	if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
		t.Fatalf("encoded output is not valid TOML: %v\nOutput:\n%s", parseErr, out)
	}

	mcpRaw, exists := m["mcp_servers"]
	if !exists {
		t.Fatalf("mcp_servers absent from re-encoded TOML; marker-carried key must be re-emitted from prior bytes\nOutput:\n%s", out)
	}

	mcpMap, ok := mcpRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("mcp_servers is not a table in re-encoded TOML; got %T", mcpRaw)
	}
	if mcpMap["url"] != "https://api.example.com" {
		t.Errorf("mcp_servers.url = %q; want \"https://api.example.com\"", mcpMap["url"])
	}
}

// TestCarriageMarker_Encode_UnrecoverableSpan_ReturnsNamedError verifies that when
// a marker's key cannot be found in the prior deployed bytes, encode returns an error
// wrapping ErrMarkerUnrecoverable rather than silently dropping the user's key.
func TestCarriageMarker_Encode_UnrecoverableSpan_ReturnsNamedError(t *testing.T) {
	// Prior bytes do NOT contain mcp_servers.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"body\"\n")

	// Canonical with a marker for mcp_servers (which is absent from prior bytes).
	canonical := buildCanonicalWithMarkers(t, []string{"mcp_servers"}, "body\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	_, _, err := tr.Encode(canonical, ctx)
	if err == nil {
		t.Fatal("Encode succeeded; want ErrMarkerUnrecoverable when the marker span cannot be found in prior bytes")
	}
	if !errors.Is(err, agentformat.ErrMarkerUnrecoverable) {
		t.Errorf("Encode returned error %v; want error wrapping ErrMarkerUnrecoverable", err)
	}
}

// ---------------------------------------------------------------------------
// T4.2: Hostile-span corpus
// ---------------------------------------------------------------------------

// TestCarriageMarker_HostileSpan_NonContiguousTable verifies span extraction against
// a non-contiguous table: [mcp_servers] header with keys interspersed with other
// top-level keys. The span for mcp_servers must include only its own keys.
func TestCarriageMarker_HostileSpan_NonContiguousTable(t *testing.T) {
	// mcp_servers keys are interspersed with a top-level key.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\n\n[mcp_servers]\nfoo = 1\n\nother_key = \"x\"\n\n[mcp_servers.sub]\nbar = 2\n\ndeveloper_instructions = \"body\"\n")

	canonical := buildCanonicalWithMarkers(t, []string{"mcp_servers"}, "body\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned error for non-contiguous table: %v", err)
	}

	var m map[string]interface{}
	if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
		t.Fatalf("output is not valid TOML after non-contiguous-table re-emission: %v", parseErr)
	}
	if _, exists := m["mcp_servers"]; !exists {
		t.Fatal("mcp_servers absent from output after non-contiguous-table re-emission")
	}
}

// TestCarriageMarker_HostileSpan_ArrayOfTables verifies span extraction against an
// array-of-tables ([[items]]). A naive line-scanner might confuse [[items]] with
// [items] (a regular table) or misparse the double-bracket header, corrupting the
// re-emitted output. The output must be valid TOML containing the full array-of-tables
// section with all entries.
func TestCarriageMarker_HostileSpan_ArrayOfTables(t *testing.T) {
	// Prior bytes contain an array-of-tables under "items" with two entries.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\n\n[[items]]\nid = 1\nlabel = \"first\"\n\n[[items]]\nid = 2\nlabel = \"second\"\n\ndeveloper_instructions = \"body\"\n")

	canonical := buildCanonicalWithMarkers(t, []string{"items"}, "body\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned error for array-of-tables span extraction: %v", err)
	}

	// Output must be valid TOML and contain the items array.
	var m map[string]interface{}
	if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
		t.Fatalf("output is not valid TOML after array-of-tables re-emission: %v\nOutput:\n%s", parseErr, out)
	}
	itemsRaw, exists := m["items"]
	if !exists {
		t.Fatalf("items absent from re-emitted output; array-of-tables must be recovered from prior bytes\nOutput:\n%s", out)
	}
	// items must decode as a slice (array of tables).
	items, ok := itemsRaw.([]interface{})
	if !ok {
		t.Fatalf("items has type %T; want []interface{} (array of tables)", itemsRaw)
	}
	if len(items) != 2 {
		t.Errorf("items has %d entries; want 2 (both array-of-tables entries must be recovered)", len(items))
	}
}

// TestCarriageMarker_HostileSpan_DottedKey verifies span extraction against prior
// bytes that use dotted-key syntax (mcp_servers.foo.bar = "value") rather than a
// table header ([mcp_servers]). A span locator that only recognises table-header
// syntax may silently fail for dotted-key syntax, producing an unrecoverable span
// error or an empty extracted span. The output must be valid TOML and contain the
// mcp_servers key with the correct value.
func TestCarriageMarker_HostileSpan_DottedKey(t *testing.T) {
	// Prior bytes use dotted-key assignment instead of [mcp_servers] table header.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\nmcp_servers.foo.bar = \"value\"\ndeveloper_instructions = \"body\"\n")

	canonical := buildCanonicalWithMarkers(t, []string{"mcp_servers"}, "body\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned error for dotted-key span extraction: %v", err)
	}

	var m map[string]interface{}
	if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
		t.Fatalf("output is not valid TOML after dotted-key re-emission: %v\nOutput:\n%s", parseErr, out)
	}
	mcpRaw, exists := m["mcp_servers"]
	if !exists {
		t.Fatalf("mcp_servers absent from re-emitted output; dotted-key span must be recovered from prior bytes\nOutput:\n%s", out)
	}
	mcpMap, ok := mcpRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("mcp_servers is not a table in re-emitted output; got %T", mcpRaw)
	}
	fooRaw, fooExists := mcpMap["foo"]
	if !fooExists {
		t.Fatalf("mcp_servers.foo absent from re-emitted output; dotted-key span extraction must preserve sub-keys\nOutput:\n%s", out)
	}
	fooMap, ok2 := fooRaw.(map[string]interface{})
	if !ok2 {
		t.Fatalf("mcp_servers.foo is not a table; got %T", fooRaw)
	}
	if fooMap["bar"] != "value" {
		t.Errorf("mcp_servers.foo.bar = %q; want \"value\"", fooMap["bar"])
	}
}

// TestCarriageMarker_HostileSpan_BracketInMultilineString verifies that a '[' at
// the start of a line inside a TOML multi-line basic string (which looks like a table
// header to a naive scanner) does not confuse the span locator.
func TestCarriageMarker_HostileSpan_BracketInMultilineString(t *testing.T) {
	// The developer_instructions value contains a line that starts with '[',
	// which would look like a table header to a line-scanning locator.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\n\n[mcp_servers]\nurl = \"https://example.com\"\n\ndeveloper_instructions = \"\"\"\nSome instructions.\n[not-a-table]\nMore text.\n\"\"\"\n")

	canonical := buildCanonicalWithMarkers(t, []string{"mcp_servers"}, "Some instructions.\n[not-a-table]\nMore text.\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned error when prior bytes contain '[' inside multi-line string: %v", err)
	}

	var m map[string]interface{}
	if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
		t.Fatalf("output is not valid TOML: %v\nOutput:\n%s", parseErr, out)
	}
	if _, exists := m["mcp_servers"]; !exists {
		t.Error("mcp_servers absent from output; bracket inside multi-line string must not confuse span locator")
	}
}

// ---------------------------------------------------------------------------
// T4.4: Extended lossless invariant for user-owned keys
// ---------------------------------------------------------------------------

// TestCarriageLossless_UserString_RoundTrip verifies that decode(encode(D)) == N(D)
// when D contains a user-owned string key: the key survives in the canonical form
// with the correct type, and a second round trip produces identical bytes.
func TestCarriageLossless_UserString_RoundTrip(t *testing.T) {
	const agentKey = "inv-agent"
	input := []byte("name = \"" + agentKey + "\"\ndescription = \"d\"\ncustom_key = \"my value\"\ndeveloper_instructions = \"body\"\n")

	// First round trip.
	canonical1 := decodeToml(t, input, agentKey)
	fv1, ok := findCarriageValuePair(t, canonical1, "custom_key")
	if !ok {
		t.Fatal("custom_key absent from mosaic_carriage after first decode")
	}
	if fv1.Quote != mosaic.QuoteDouble {
		t.Errorf("custom_key quote after decode = %q; want QuoteDouble", fv1.Quote)
	}

	// Encode and decode again for idempotence.
	canonical2 := roundTrip(t, canonical1, agentKey)
	fv2, ok2 := findCarriageValuePair(t, canonical2, "custom_key")
	if !ok2 {
		t.Fatal("custom_key absent from mosaic_carriage after second round trip")
	}
	if fv2.Scalar != fv1.Scalar || fv2.Quote != fv1.Quote {
		t.Errorf("custom_key changed across round trips: first=%q/%q, second=%q/%q",
			fv1.Scalar, fv1.Quote, fv2.Scalar, fv2.Quote)
	}
}

// TestCarriageLossless_UserAndMOSAICKeys_BothPreserved verifies the invariant holds
// for a file containing both user-owned and MOSAIC-owned keys: both sets are preserved
// across multiple round trips.
func TestCarriageLossless_UserAndMOSAICKeys_BothPreserved(t *testing.T) {
	const agentKey = "mixed-agent"
	input := []byte("name = \"" + agentKey + "\"\ndescription = \"Desc.\"\nsandbox_mode = \"read-only\"\ncustom_setting = \"value\"\ndeveloper_instructions = \"body\"\n")

	// First round trip: encode to TOML, decode back.
	canonical := decodeToml(t, input, agentKey)

	// MOSAIC-owned key must be present at canonical top level.
	doc := parseCanonical(t, canonical)
	checkField(t, doc, "description", "Desc.")
	checkField(t, doc, "sandbox_mode", "read-only")

	// User-owned key must be in carriage container.
	_, ok := findCarriageValuePair(t, canonical, "custom_setting")
	if !ok {
		t.Fatal("custom_setting absent from mosaic_carriage after decode")
	}

	// Second round trip must be stable.
	canonical2 := roundTrip(t, canonical, agentKey)
	_, ok2 := findCarriageValuePair(t, canonical2, "custom_setting")
	if !ok2 {
		t.Fatal("custom_setting absent from mosaic_carriage after second round trip")
	}
}

// TestCarriageLossless_HostileValueCorpus_AsUserScalar runs the frontmatter-value
// hostile corpus through the carriage path as user-added scalar keys. For each
// corpus entry that is not unrepresentable, the value must round-trip correctly
// (value and type preserved) or be escalated to a marker and recovered from prior
// bytes. For unrepresentable entries (control characters), escalation to a marker
// is the required outcome -- the artifact must not abort.
func TestCarriageLossless_HostileValueCorpus_AsUserScalar(t *testing.T) {
	priorBytes := func(tomlValue, want string) []byte {
		// Construct prior bytes containing the user key with the given TOML value.
		// The TOML value is already a TOML literal (quoted or unquoted).
		return []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\nuser_key = " + tomlValue + "\ndeveloper_instructions = \"body\"\n")
	}

	for _, tc := range fmCorpus {
		tc := tc
		t.Run("corpus/"+tc.name, func(t *testing.T) {
			input := priorBytes(tc.tomlValue, tc.want)

			// The carriage path must not abort for any corpus entry: unrepresentable
			// control-character values must be escalated to markers, not abort.
			canonical := decodeToml(t, input, "a")

			// The key must appear either in values or in markers.
			_, inValues := findCarriageValuePair(t, canonical, "user_key")
			markers := carriageMarkerNames(t, canonical)
			inMarkers := false
			for _, name := range markers {
				if name == "user_key" {
					inMarkers = true
					break
				}
			}

			if !inValues && !inMarkers {
				t.Errorf("user_key absent from both mosaic_carriage and mosaic_carriage_markers for corpus entry %q; must be carried in one of the two containers", tc.name)
				return
			}

			// If value-carried, the scalar must equal the decoded want value with correct type.
			if inValues {
				fv, _ := findCarriageValuePair(t, canonical, "user_key")
				if fv.Scalar != tc.want {
					t.Errorf("user_key scalar = %q; want %q for corpus entry %q", fv.Scalar, tc.want, tc.name)
				}
				// All corpus entries are TOML strings, so must be QuoteDouble.
				if fv.Quote != mosaic.QuoteDouble {
					t.Errorf("user_key quote = %q; want QuoteDouble for TOML string corpus entry %q", fv.Quote, tc.name)
				}
			}

			// Recovery assertion (T4.4): when the key was escalated to a marker, encode
			// with OpUpdate + the prior bytes must re-emit user_key from the marker span.
			// The output must be valid TOML and contain user_key with the original value.
			// This verifies the second half of the two-branch outcome: marker-carried hostile
			// values are correctly recovered, not silently lost.
			if inMarkers {
				tr := codexTranslator(t)
				encCtx := agentformat.ArtifactContext{
					AgentKey:      "a",
					Op:            agentformat.OpUpdate,
					PriorDeployed: input,
				}
				out, _, encErr := tr.Encode(canonical, encCtx)
				if encErr != nil {
					t.Fatalf("Encode with OpUpdate returned error for marker-carried corpus entry %q: %v", tc.name, encErr)
				}
				var m map[string]interface{}
				if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
					t.Fatalf("re-encoded output is not valid TOML for corpus entry %q: %v\nOutput:\n%s", tc.name, parseErr, out)
				}
				outVal, exists := m["user_key"]
				if !exists {
					t.Errorf("user_key absent from re-encoded output for marker-carried corpus entry %q; escalated hostile value must be re-emitted from prior bytes", tc.name)
				} else if outStr, ok := outVal.(string); !ok || outStr != tc.want {
					t.Errorf("user_key = %v (%T); want %q (string) for marker-carried corpus entry %q", outVal, outVal, tc.want, tc.name)
				}
			}
		})
	}
}

// TestCarriageLossless_HostileValueCorpus_AsArrayElement runs a subset of the
// hostile corpus through the carriage path as elements of a user-added TOML array.
// Each element must either round-trip identically by value (type and text), or the
// array as a whole escalates to a marker and is recovered from prior bytes. The
// artifact must not abort for any corpus element.
func TestCarriageLossless_HostileValueCorpus_AsArrayElement(t *testing.T) {
	// Hostile TOML array elements: a subset of critical cases including flow-list
	// delimiters, empty string, and TOML-reserved spellings as strings.
	type arrayCase struct {
		name     string
		tomlExpr string // full TOML value for the array key, e.g. ["a", "b"]
		wantVals []string
	}

	cases := []arrayCase{
		{
			name:     "strings_with_comma",
			tomlExpr: `["a, b", "c"]`,
			wantVals: []string{"a, b", "c"},
		},
		{
			name:     "string_with_bracket",
			tomlExpr: `["x]y"]`,
			wantVals: []string{"x]y"},
		},
		{
			name:     "string_with_escaped_quote",
			tomlExpr: `["x\"y"]`,
			wantVals: []string{`x"y`},
		},
		{
			name:     "empty_string_element",
			tomlExpr: `["", "a"]`,
			wantVals: []string{"", "a"},
		},
		{
			name:     "reserved_spelling_as_string",
			tomlExpr: `["1_000", "inf"]`,
			wantVals: []string{"1_000", "inf"},
		},
		{
			// A backslash inside a flow-list element interacts with docformat escaping
			// rules in a distinct way from other delimiters. The element must round-trip
			// with text and type unchanged, or the array escalates to a marker.
			name:     "string_with_backslash",
			tomlExpr: `["a\\b", "c"]`,
			wantVals: []string{`a\b`, "c"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run("array/"+tc.name, func(t *testing.T) {
			// Prior bytes contain the user array key for marker recovery.
			priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\nnickname_candidates = " + tc.tomlExpr + "\ndeveloper_instructions = \"body\"\n")
			canonical := decodeToml(t, priorBytes, "a")

			// The key must be either value-carried or marker-carried; never absent.
			carriedFV, inValues := findCarriageValuePair(t, canonical, "nickname_candidates")
			markers := carriageMarkerNames(t, canonical)
			inMarkers := false
			for _, name := range markers {
				if name == "nickname_candidates" {
					inMarkers = true
					break
				}
			}

			if !inValues && !inMarkers {
				t.Errorf("nickname_candidates absent from both carriage containers for array case %q", tc.name)
				return
			}

			if inValues {
				// Value-carried: must be a flow list with correct element values and types.
				if carriedFV.Kind != mosaic.KindList {
					t.Errorf("nickname_candidates in container has kind %q; want list", carriedFV.Kind)
					return
				}
				if len(carriedFV.Items) != len(tc.wantVals) {
					t.Errorf("nickname_candidates has %d items; want %d", len(carriedFV.Items), len(tc.wantVals))
					return
				}
				for i, item := range carriedFV.Items {
					if item.Scalar != tc.wantVals[i] {
						t.Errorf("nickname_candidates[%d] = %q; want %q", i, item.Scalar, tc.wantVals[i])
					}
					// All elements are TOML strings -- must be QuoteDouble.
					if item.Quote != mosaic.QuoteDouble {
						t.Errorf("nickname_candidates[%d] quote = %q; want QuoteDouble for string element", i, item.Quote)
					}
				}
			}

			// Recovery assertion (T4.4): when the array was escalated to a marker, encode
			// with OpUpdate + prior bytes must re-emit nickname_candidates from the marker
			// span. The output must be valid TOML and contain the array with the correct
			// element values. This verifies the second half of the two-branch outcome: a
			// marker-carried hostile array is correctly recovered, not silently lost.
			if inMarkers {
				tr := codexTranslator(t)
				encCtx := agentformat.ArtifactContext{
					AgentKey:      "a",
					Op:            agentformat.OpUpdate,
					PriorDeployed: priorBytes,
				}
				out, _, encErr := tr.Encode(canonical, encCtx)
				if encErr != nil {
					t.Fatalf("Encode with OpUpdate returned error for marker-carried array case %q: %v", tc.name, encErr)
				}
				var m map[string]interface{}
				if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
					t.Fatalf("re-encoded output is not valid TOML for marker-carried array case %q: %v\nOutput:\n%s", tc.name, parseErr, out)
				}
				rawArr, exists := m["nickname_candidates"]
				if !exists {
					t.Errorf("nickname_candidates absent from re-encoded output for marker-carried array case %q; escalated array must be re-emitted from prior bytes", tc.name)
					return
				}
				items, ok := rawArr.([]interface{})
				if !ok {
					t.Errorf("nickname_candidates has type %T; want []interface{} (array) for marker-carried array case %q", rawArr, tc.name)
					return
				}
				if len(items) != len(tc.wantVals) {
					t.Errorf("nickname_candidates has %d items; want %d for marker-carried array case %q", len(items), len(tc.wantVals), tc.name)
					return
				}
				for i, item := range items {
					str, isStr := item.(string)
					if !isStr {
						t.Errorf("nickname_candidates[%d] has type %T; want string for marker-carried array case %q", i, item, tc.name)
						continue
					}
					if str != tc.wantVals[i] {
						t.Errorf("nickname_candidates[%d] = %q; want %q for marker-carried array case %q", i, str, tc.wantVals[i], tc.name)
					}
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T4.5: Owned-key resurrection guard
// ---------------------------------------------------------------------------

// TestCarriageResurrection_OwnedKeyMarker_DoesNotRestorePriorValue verifies that a
// carriage marker naming a MOSAIC-owned key (the shape a hostile or malformed input
// could produce) never restores that key's prior value. MOSAIC's current value must
// be emitted, exactly once, and the marker must be refused.
func TestCarriageResurrection_OwnedKeyMarker_DoesNotRestorePriorValue(t *testing.T) {
	// Prior bytes contain an old model value.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nmodel = \"old-model\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"body\"\n")

	// Build canonical with model as an owned key AND as a marker. This simulates
	// a malformed canonical that a hostile decode could produce.
	fmText := "name: a\ndescription: d\nmodel: current-model\n" + agentformat.CarriageMarkersKey + ": [\"model\"]\n"
	canonicalFull := []byte("---\n" + fmText + "---\nbody\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, report, err := tr.Encode(canonicalFull, ctx)
	if err != nil {
		// If the implementation refuses the owned-key marker with an error rather than
		// reporting it, the error must wrap ErrMarkerUnrecoverable. Any other error is
		// unexpected and unrelated to the owned-key guard -- that would be a defect.
		if !errors.Is(err, agentformat.ErrMarkerUnrecoverable) {
			t.Fatalf("Encode returned error %v; if an error is returned for an owned-key marker, it must wrap ErrMarkerUnrecoverable (not an unrelated error)", err)
		}
		// ErrMarkerUnrecoverable is the loud-refusal outcome: the marker for an owned key
		// cannot be applied, so encode aborts. This satisfies the defined behavior.
		return
	}

	// Parse the output and verify MOSAIC's current model value was emitted.
	m := parseTomlMap(t, out)
	modelVal, exists := m["model"]
	if !exists {
		t.Fatal("model absent from output; MOSAIC-owned key must be emitted by encode")
	}
	modelStr, isString := modelVal.(string)
	if !isString || modelStr == "old-model" {
		t.Errorf("model = %v; want \"current-model\" (prior value must not be restored by marker)", modelVal)
	}
	if isString && modelStr != "current-model" {
		t.Errorf("model = %q; want \"current-model\" (MOSAIC's current value must win, not any other value)", modelStr)
	}

	// The report must contain EntryRefusedMarker for the model marker.
	refusedFound := false
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryRefusedMarker && entry.Key == "model" {
			refusedFound = true
			break
		}
	}
	if !refusedFound {
		t.Error("report has no EntryRefusedMarker for \"model\"; a marker naming a MOSAIC-owned key must be refused and reported")
	}
}

// TestCarriageResurrection_OwnedKeyNeverEmittedTwice verifies that when a marker
// names a MOSAIC-owned key, the key is emitted at most once in the output. A marker
// must not produce a duplicate that creates a TOML parse error or silently overwrites.
func TestCarriageResurrection_OwnedKeyNeverEmittedTwice(t *testing.T) {
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"body\"\n")
	// canonical has "sandbox_mode" as both an owned key and a marker.
	fmText := "name: a\ndescription: d\nsandbox_mode: read-only\n" + agentformat.CarriageMarkersKey + ": [\"sandbox_mode\"]\n"
	canonicalFull := []byte("---\n" + fmText + "---\nbody\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonicalFull, ctx)
	if err != nil {
		// If the implementation refuses the owned-key marker with an error, it must be
		// ErrMarkerUnrecoverable. Duplicate emission cannot occur when the marker is
		// refused, so this satisfies the "never emitted twice" contract.
		if !errors.Is(err, agentformat.ErrMarkerUnrecoverable) {
			t.Fatalf("Encode returned error %v; if an error is returned for an owned-key marker, it must wrap ErrMarkerUnrecoverable (not an unrelated error)", err)
		}
		return
	}

	// The TOML output must be parseable (no duplicate-key error).
	var m map[string]interface{}
	if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
		t.Fatalf("output has TOML parse error (possible duplicate key from marker resurrection): %v\nOutput:\n%s", parseErr, out)
	}
}

// ---------------------------------------------------------------------------
// T4.7: Hostile key corpus
// ---------------------------------------------------------------------------

// TestCarriageKey_BareLegal_IsValueCarried verifies that a top-level key composed
// entirely of TOML bare-key characters (A-Za-z0-9_-) is value-carried. This is the
// "safe case" that proves the rule does not degrade into escalating everything.
func TestCarriageKey_BareLegal_IsValueCarried(t *testing.T) {
	input := []byte("name = \"a\"\nmax_threads = 4\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	_, inValues := findCarriageValuePair(t, canonical, "max_threads")
	if !inValues {
		t.Error("max_threads absent from mosaic_carriage values; bare-legal keys must be value-carried")
	}
}

// TestCarriageKey_BareIllegal_DoubleQuotable_IsMarkerCarried tests that top-level
// keys outside the bare-key charset (A-Za-z0-9_-) but representable as double-quoted
// list items are marker-carried, whatever their value shape.
//
// TOML allows quoted keys using double quotes. The key "odd key: x" is only expressible
// as a quoted TOML key, so decode must escalate it to marker carriage. The corpus
// exercises the most important boundary cases: empty key (extreme of the charset rule),
// a key with an escaped double quote (tests escape survival), leading/trailing spaces,
// and a multi-byte unicode character.
func TestCarriageKey_BareIllegal_DoubleQuotable_IsMarkerCarried(t *testing.T) {
	// TOML quoted keys. tomlKey is the TOML source representation (as written in the file).
	// wantKey is the decoded key name (after TOML unescaping).
	cases := []struct {
		name    string
		tomlKey string // as it appears in TOML source
		wantKey string // the decoded key name
	}{
		{
			name:    "key_with_space",
			tomlKey: `"odd key: x"`,
			wantKey: "odd key: x",
		},
		{
			name:    "key_with_hash",
			tomlKey: `"#tag"`,
			wantKey: "#tag",
		},
		{
			name:    "key_with_dot",
			tomlKey: `"a.b"`,
			wantKey: "a.b",
		},
		{
			// The empty key exercises the extreme lower boundary of the charset rule:
			// a key with zero characters is outside A-Za-z0-9_- regardless of content.
			name:    "empty_key",
			tomlKey: `""`,
			wantKey: "",
		},
		{
			// A key containing an escaped double quote tests that the escape character
			// survives round-trip through the marker name and the prior-bytes channel.
			name:    "key_with_escaped_double_quote",
			tomlKey: `"a\"b"`,
			wantKey: `a"b`,
		},
		{
			// Leading and trailing spaces are outside the bare-key charset.
			name:    "key_with_leading_trailing_spaces",
			tomlKey: `" spaced "`,
			wantKey: " spaced ",
		},
		{
			// A multi-byte unicode character (é, U+00E9) is outside the ASCII bare-key
			// charset. Marker carriage must preserve the full unicode code point.
			name:    "key_with_multibyte_unicode",
			tomlKey: `"key-with-é"`,
			wantKey: "key-with-é",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Build TOML with the quoted key.
			input := []byte("name = \"a\"\n" + tc.tomlKey + " = \"value\"\ndeveloper_instructions = \"body\"\n")
			canonical := decodeToml(t, input, "a")

			_, inValues := findCarriageValuePair(t, canonical, tc.wantKey)
			if inValues {
				t.Errorf("key %q appeared in mosaic_carriage values; bare-illegal keys must be marker-carried", tc.wantKey)
			}

			markers := carriageMarkerNames(t, canonical)
			found := false
			for _, name := range markers {
				if name == tc.wantKey {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("key %q absent from mosaic_carriage_markers; bare-illegal keys must be escalated to markers, got markers: %v", tc.wantKey, markers)
			}
		})
	}
}

// TestCarriageKey_BareIllegal_RoundTripsFromPriorBytes_Extended verifies byte-identical
// round-trip recovery for the expanded bare-illegal key corpus. For each key that is
// correctly escalated to a marker, encode with prior bytes must recover the key's original
// span and re-emit it such that the output is valid TOML and the key is present.
//
// This test mirrors TestCarriageKey_BareIllegal_RoundTripsFromPriorBytes (which covers
// "#tag") for the four additional hostile key cases introduced by the review.
func TestCarriageKey_BareIllegal_RoundTripsFromPriorBytes_Extended(t *testing.T) {
	cases := []struct {
		name        string
		tomlKey     string // TOML source representation of the key
		wantKey     string // decoded key name
		priorValue  string // TOML value for the key in prior bytes
	}{
		{
			name:       "empty_key",
			tomlKey:    `""`,
			wantKey:    "",
			priorValue: `"empty-key-value"`,
		},
		{
			name:       "key_with_escaped_double_quote",
			tomlKey:    `"a\"b"`,
			wantKey:    `a"b`,
			priorValue: `"escaped-quote-value"`,
		},
		{
			name:       "key_with_leading_trailing_spaces",
			tomlKey:    `" spaced "`,
			wantKey:    " spaced ",
			priorValue: `"spaced-value"`,
		},
		{
			name:       "key_with_multibyte_unicode",
			tomlKey:    `"key-with-é"`,
			wantKey:    "key-with-é",
			priorValue: `"unicode-value"`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\n" +
				tc.tomlKey + " = " + tc.priorValue + "\ndeveloper_instructions = \"body\"\n")

			canonical := decodeToml(t, priorBytes, "a")

			// Verify the key is in markers before attempting the round-trip.
			markers := carriageMarkerNames(t, canonical)
			found := false
			for _, name := range markers {
				if name == tc.wantKey {
					found = true
					break
				}
			}
			if !found {
				t.Skipf("key %q not in markers yet (implementation pending); got markers: %v", tc.wantKey, markers)
			}

			// Encode with the prior bytes and verify the output contains the key.
			tr := codexTranslator(t)
			ctx := agentformat.ArtifactContext{
				AgentKey:      "a",
				Op:            agentformat.OpUpdate,
				PriorDeployed: priorBytes,
			}

			out, _, err := tr.Encode(canonical, ctx)
			if err != nil {
				t.Fatalf("Encode returned error for bare-illegal key %q marker recovery: %v", tc.wantKey, err)
			}

			// The re-emitted output must be parseable and contain the key.
			var m map[string]interface{}
			if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
				t.Fatalf("output is not valid TOML after bare-illegal key re-emission for %q: %v\nOutput:\n%s", tc.wantKey, parseErr, out)
			}
			if _, exists := m[tc.wantKey]; !exists {
				t.Errorf("key %q absent from re-emitted TOML; bare-illegal key must be recovered from prior bytes\nOutput:\n%s", tc.wantKey, out)
			}
		})
	}
}

// TestCarriageKey_ControlCharacter_ReturnsErrMarkerUnrecoverable verifies that a
// top-level TOML key containing a control character (which cannot be represented as a
// double-quoted list item) causes encode to return ErrMarkerUnrecoverable. The artifact
// must not be written with the user key silently missing.
//
// AC4.14 names raw newline, carriage return, tab, and any other control character as
// unrepresentable cases. This test covers all three named control characters as
// separate sub-tests.
//
// These cases cannot be decoded naturally from TOML (TOML does not allow control
// characters in quoted keys), so we simulate by constructing a canonical that has
// a marker entry with a control character in the name. YAML double-quoted scalars
// interpret escape sequences (\n, \r, \t), so the marker name ends up containing
// actual control-character bytes after YAML parsing.
func TestCarriageKey_ControlCharacter_ReturnsErrMarkerUnrecoverable(t *testing.T) {
	cases := []struct {
		name        string
		yamlEscaped string // YAML double-quote escape sequence for the control character
	}{
		{
			name:        "newline",
			yamlEscaped: "key\\nwith\\nnewline",
		},
		{
			// Carriage return (U+000D): named explicitly in AC4.14.
			name:        "carriage_return",
			yamlEscaped: "key\\rwith\\rCR",
		},
		{
			// Horizontal tab (U+0009): named explicitly in AC4.14.
			name:        "tab",
			yamlEscaped: "key\\twith\\ttab",
		},
	}

	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"body\"\n")

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Build a canonical with the marker name containing the control character.
			// The YAML parser interprets the escape sequence in a double-quoted scalar,
			// so the marker name ends up with the actual control-character byte(s).
			fmText := "name: a\ndescription: d\n" + agentformat.CarriageMarkersKey + ":\n  - \"" + tc.yamlEscaped + "\"\n"
			canonicalFull := []byte("---\n" + fmText + "---\nbody\n")

			tr := codexTranslator(t)
			ctx := agentformat.ArtifactContext{
				AgentKey:      "a",
				Op:            agentformat.OpUpdate,
				PriorDeployed: priorBytes,
			}

			_, _, err := tr.Encode(canonicalFull, ctx)
			if err == nil {
				t.Fatalf("Encode succeeded for marker with %s in key name; want ErrMarkerUnrecoverable", tc.name)
			}
			if !errors.Is(err, agentformat.ErrMarkerUnrecoverable) {
				t.Errorf("Encode returned error %v; want error wrapping ErrMarkerUnrecoverable for %s case", err, tc.name)
			}
		})
	}
}

// TestCarriageKey_BareIllegal_RoundTripsFromPriorBytes verifies that a bare-illegal
// key that is correctly escalated to a marker is re-emitted byte-identically from the
// prior deployed bytes.
func TestCarriageKey_BareIllegal_RoundTripsFromPriorBytes(t *testing.T) {
	// Prior bytes contain the bare-illegal key.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\n\"#tag\" = \"special\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, priorBytes, "a")

	// Verify #tag is in markers.
	markers := carriageMarkerNames(t, canonical)
	found := false
	for _, name := range markers {
		if name == "#tag" {
			found = true
			break
		}
	}
	if !found {
		t.Skipf("\"#tag\" not in markers yet (implementation pending); got markers: %v", markers)
	}

	// Encode with the prior bytes and verify the output contains #tag.
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned error for bare-illegal key marker recovery: %v", err)
	}

	// The re-emitted output must contain the original key span. Since "\"#tag\"" is
	// a TOML-valid quoted key, the output should be parseable and contain the key.
	var m map[string]interface{}
	if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
		t.Fatalf("output is not valid TOML after bare-illegal key re-emission: %v", parseErr)
	}
	if _, exists := m["#tag"]; !exists {
		t.Errorf("\"#tag\" absent from re-emitted TOML; bare-illegal key must be recovered from prior bytes")
	}
}

// ---------------------------------------------------------------------------
// T4.4: Multi-round-trip lossless invariant for marker-carried user keys
// ---------------------------------------------------------------------------

// TestCarriageLossless_MarkerCarried_MultipleRoundTrips verifies that the lossless
// invariant holds for marker-carried user keys over two full encode-decode passes.
//
// The sequence is:
//   decode(input) -> canonical1
//   encode(canonical1, OpUpdate, prior=input) -> toml2
//   decode(toml2) -> canonical2
//   encode(canonical2, OpUpdate, prior=toml2) -> toml3
//
// Assertions:
//   - canonical1 and canonical2 carry the same marker names (stable carriage representation)
//   - toml2 == toml3 byte for byte (stable encoded output)
//
// A marker carriage bug that only manifests on the second encode (for example, subtly
// changed whitespace altering the span on re-extraction) would go undetected by a
// single-pass test.
func TestCarriageLossless_MarkerCarried_MultipleRoundTrips(t *testing.T) {
	const agentKey = "multi-rt-agent"
	// Input contains a user-added nested table (marker-carried) and a user string key
	// (value-carried), so both carriage paths are exercised across the round trips.
	// developer_instructions must appear BEFORE [mcp_servers] so that it is a
	// top-level key. Any key placed after a TOML section header belongs to that
	// section; placing developer_instructions after [mcp_servers] would make it
	// mcp_servers.developer_instructions (nested), causing ErrEmptyBody.
	input := []byte("name = \"" + agentKey + "\"\ndescription = \"desc\"\nsandbox_mode = \"read-only\"\ncustom_key = \"my-value\"\ndeveloper_instructions = \"body\"\n\n[mcp_servers]\nurl = \"https://api.example.com\"\n")

	tr := codexTranslator(t)

	// Pass 1: decode input, verify the nested table is marker-carried.
	canonical1 := decodeToml(t, input, agentKey)

	markers1 := carriageMarkerNames(t, canonical1)
	foundMCP := false
	for _, name := range markers1 {
		if name == "mcp_servers" {
			foundMCP = true
			break
		}
	}
	if !foundMCP {
		t.Fatalf("mcp_servers absent from mosaic_carriage_markers after first decode; got: %v", markers1)
	}

	// Pass 1 encode: produce toml2 from canonical1 with prior=input.
	ctx1 := agentformat.ArtifactContext{
		AgentKey:      agentKey,
		Op:            agentformat.OpUpdate,
		PriorDeployed: input,
	}
	toml2, _, err := tr.Encode(canonical1, ctx1)
	if err != nil {
		t.Fatalf("first Encode returned error: %v", err)
	}

	// Pass 2: decode toml2, verify canonical form is stable.
	canonical2 := decodeToml(t, toml2, agentKey)

	markers2 := carriageMarkerNames(t, canonical2)
	if len(markers1) != len(markers2) {
		t.Errorf("marker count changed across round trips: first=%d %v, second=%d %v",
			len(markers1), markers1, len(markers2), markers2)
	} else {
		for i := range markers1 {
			if markers1[i] != markers2[i] {
				t.Errorf("marker[%d] changed across round trips: first=%q, second=%q", i, markers1[i], markers2[i])
			}
		}
	}

	// The value-carried key must also be stable across both passes.
	fv1, ok1 := findCarriageValuePair(t, canonical1, "custom_key")
	fv2, ok2 := findCarriageValuePair(t, canonical2, "custom_key")
	if ok1 != ok2 {
		t.Errorf("custom_key presence changed across round trips: first=%v, second=%v", ok1, ok2)
	} else if ok1 && ok2 && (fv1.Scalar != fv2.Scalar || fv1.Quote != fv2.Quote) {
		t.Errorf("custom_key changed across round trips: first scalar=%q quote=%q, second scalar=%q quote=%q",
			fv1.Scalar, fv1.Quote, fv2.Scalar, fv2.Quote)
	}

	// Pass 2 encode: produce toml3 from canonical2 with prior=toml2.
	ctx2 := agentformat.ArtifactContext{
		AgentKey:      agentKey,
		Op:            agentformat.OpUpdate,
		PriorDeployed: toml2,
	}
	toml3, _, err := tr.Encode(canonical2, ctx2)
	if err != nil {
		t.Fatalf("second Encode returned error: %v", err)
	}

	// toml2 and toml3 must be byte-for-byte identical: the encoded form is stable.
	// A whitespace shift or span-extraction change that only surfaces on the second
	// encode would break this assertion.
	if !bytes.Equal(toml2, toml3) {
		t.Errorf("encoded TOML not stable after two round trips:\nfirst encode:\n%s\nsecond encode:\n%s", toml2, toml3)
	}
}

// ---------------------------------------------------------------------------
// T4.4: Exotic-spelled scalar recovery from prior bytes via marker channel
// ---------------------------------------------------------------------------

// TestCarriageLossless_ExoticSpelledScalar_MarkerRecovery verifies that TOML scalars
// with exotic spellings that cannot be preserved through the canonical form (because
// the TOML library normalises them on decode) are correctly recovered from prior bytes
// via the marker channel on an OpUpdate encode.
//
// When a scalar such as pi = 3.14, hex_val = 0x1F, or count = 1_000 is escalated to a
// marker (because go-toml/v2 does not recover the original source spelling), encode
// with OpUpdate + prior bytes must re-emit the key from the marker span, producing
// valid TOML output that contains the key. The test uses t.Skip when the scalar is
// value-carried rather than marker-carried, since either outcome is acceptable per the
// design; the test targets the marker-carried path specifically.
func TestCarriageLossless_ExoticSpelledScalar_MarkerRecovery(t *testing.T) {
	cases := []struct {
		name      string
		tomlValue string // unquoted TOML literal as written in the file
	}{
		// A standard float that go-toml/v2 decodes to float64: may be value-carried
		// if the library preserves the decimal representation, or escalated to a marker.
		{name: "pi_float", tomlValue: "3.14"},
		// Hex integer: go-toml/v2 normalises 0x1F to 31, losing the original spelling.
		{name: "hex_int", tomlValue: "0x1F"},
		// Underscore integer: go-toml/v2 normalises 1_000 to 1000.
		{name: "underscore_int", tomlValue: "1_000"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\nexotic_val = " + tc.tomlValue + "\ndeveloper_instructions = \"body\"\n")
			canonical := decodeToml(t, priorBytes, "a")

			markers := carriageMarkerNames(t, canonical)
			inMarkers := false
			for _, name := range markers {
				if name == "exotic_val" {
					inMarkers = true
					break
				}
			}

			if !inMarkers {
				// Value-carried: the scalar spelling was preserved through the canonical
				// form without escalation. Skip the marker recovery assertion; value-carried
				// recovery is covered by the carriage value round-trip tests.
				_, inValues := findCarriageValuePair(t, canonical, "exotic_val")
				if inValues {
					t.Skipf("exotic_val (%s) is value-carried; marker recovery test applies only when escalation occurs", tc.name)
				}
				t.Fatalf("exotic_val absent from both carriage containers for %q; must be carried by one route", tc.name)
			}

			// Recovery assertion: encode with OpUpdate + prior bytes must re-emit
			// exotic_val from the marker span. The output must be valid TOML containing
			// the key. The re-emitted text comes verbatim from prior bytes, preserving
			// the original exotic spelling.
			tr := codexTranslator(t)
			ctx := agentformat.ArtifactContext{
				AgentKey:      "a",
				Op:            agentformat.OpUpdate,
				PriorDeployed: priorBytes,
			}
			out, _, err := tr.Encode(canonical, ctx)
			if err != nil {
				t.Fatalf("Encode returned error for exotic-spelled marker recovery of %q: %v", tc.name, err)
			}
			var m map[string]interface{}
			if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
				t.Fatalf("re-encoded output is not valid TOML for %q: %v\nOutput:\n%s", tc.name, parseErr, out)
			}
			if _, exists := m["exotic_val"]; !exists {
				t.Errorf("exotic_val absent from re-encoded output for %q; escalated marker must be re-emitted from prior bytes\nOutput:\n%s", tc.name, out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AC4.10: EntryDroppedComment report entries
// ---------------------------------------------------------------------------

// TestCarriageDroppedComment_ValueCarried_ProducesEntry verifies that when a
// value-carried user key had an attached TOML comment in the prior deployed bytes,
// encoding with those prior bytes produces an EntryDroppedComment report entry naming
// the key and containing the comment text.
//
// The attached-comment escalation rule (I4.5) states that a user key with an attached
// comment should escalate to marker carriage so the comment survives. When the key is
// already value-carried in the canonical (either because escalation was not possible, or
// because this canonical was built before the rule existed), the encode path must detect
// that the comment cannot be preserved and produce EntryDroppedComment to signal the loss.
//
// In RED, no such detection occurs: the encode path re-emits the value-carried key
// without examining comments in the prior bytes, so no EntryDroppedComment is produced
// and the test fails.
func TestCarriageDroppedComment_ValueCarried_ProducesEntry(t *testing.T) {
	const comment = "# my attached comment"
	const key = "my_key"

	// Construct a canonical with my_key as value-carried (bypassing decode, which in
	// a correct implementation would escalate a commented key to marker carriage).
	// This directly tests the encode path's comment-detection responsibility.
	pairs := []mosaic.FieldPair{
		{Key: key, Value: mosaic.ScalarValue("value", mosaic.QuoteDouble)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")

	// Prior bytes contain my_key with an attached TOML comment on the preceding line.
	// The comment is "attached" because it immediately precedes the key with no blank line.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\n" +
		comment + "\n" +
		key + " = \"value\"\n" +
		"developer_instructions = \"body\"\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	_, report, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	// The report must contain EntryDroppedComment for my_key, because the comment
	// could not be preserved through value carriage (the frontmatter cannot carry
	// TOML comments). The entry's Detail must contain the comment text.
	var dropped *agentformat.ReportEntry
	for i := range report.Entries {
		e := &report.Entries[i]
		if e.Kind == agentformat.EntryDroppedComment && e.Key == key {
			dropped = e
			break
		}
	}
	if dropped == nil {
		t.Errorf("report has no EntryDroppedComment for key %q; a value-carried user key with an attached comment must produce EntryDroppedComment when the comment cannot be preserved", key)
		return
	}
	if !bytes.Contains([]byte(dropped.Detail), []byte(comment)) {
		t.Errorf("EntryDroppedComment.Detail = %q; want it to contain the comment text %q", dropped.Detail, comment)
	}
}

// TestCarriageDroppedComment_MarkerEscalated_NoEntry verifies that when a user key
// with an attached TOML comment is correctly escalated to marker carriage (because the
// comment drove escalation), the comment survives in the re-emitted marker span and no
// EntryDroppedComment is produced.
//
// The attached-comment escalation rule (I4.5) states: when a value-shaped user key has
// an attached comment, it is escalated to marker carriage. The marker's byte span includes
// the comment, so the comment is re-emitted verbatim from the prior bytes. No
// EntryDroppedComment is produced because the comment was not lost.
//
// In RED, decode does not escalate a commented key to marker carriage: the key ends up
// value-carried in the canonical, so the assertion that the key is in markers fails.
func TestCarriageDroppedComment_MarkerEscalated_NoEntry(t *testing.T) {
	const comment = "# escalate me"
	const key = "commented_key"

	// Prior bytes contain the user key with an attached comment immediately above it.
	priorBytes := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\n" +
		comment + "\n" +
		key + " = \"value\"\n" +
		"developer_instructions = \"body\"\n")

	// Decode: the attached-comment escalation rule must place commented_key in
	// mosaic_carriage_markers, not mosaic_carriage. A decode that ignores comments
	// would leave the key value-carried, causing this assertion to fail in RED.
	canonical := decodeToml(t, priorBytes, "a")

	markers := carriageMarkerNames(t, canonical)
	found := false
	for _, name := range markers {
		if name == key {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("key %q absent from mosaic_carriage_markers after decode; a key with an attached comment must be escalated to marker carriage so the comment survives, got markers: %v", key, markers)
	}

	// Encode with OpUpdate + prior bytes: the marker is recovered from the prior bytes,
	// and the byte span includes the attached comment. No EntryDroppedComment must be
	// produced because the comment is preserved through the marker channel.
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, report, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	// No EntryDroppedComment must appear for this key: the comment survived in the span.
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryDroppedComment && entry.Key == key {
			t.Errorf("report has EntryDroppedComment for %q; a marker-escalated key with an attached comment must NOT produce EntryDroppedComment because the comment travels with the marker span", key)
		}
	}

	// The re-emitted output must be valid TOML and contain the key.
	var m map[string]interface{}
	if parseErr := toml.Unmarshal(out, &m); parseErr != nil {
		t.Fatalf("re-encoded output is not valid TOML: %v\nOutput:\n%s", parseErr, out)
	}
	if _, exists := m[key]; !exists {
		t.Errorf("key %q absent from re-encoded output; marker-carried key must be re-emitted from prior bytes\nOutput:\n%s", key, out)
	}
}

// ---------------------------------------------------------------------------
// AC4.9a: Foreign header-comment re-emission
// ---------------------------------------------------------------------------

// TestCarriageHeaderComment_ForeignComments_ReEmittedAfterStampBlock verifies that
// foreign header comments present in the prior deployed bytes are re-emitted in the
// encoded output after the stamp block. A "foreign header comment" is a TOML comment
// line that appears above the first non-comment TOML key in the prior bytes and is not
// itself a stamp line (a stamp line has the form "# <deployed_name>: <value>").
//
// This behaviour preserves user comments that a user placed at the top of a deployed
// Codex file (e.g., a description comment or a TODO note). The comments must survive
// redeploy by being re-emitted from the prior bytes via the header-comment scan (I4.4a).
//
// In RED, no header-comment scan is implemented: the encode path discards non-stamp
// leading comments from the prior bytes and the output contains only the stamp block.
// The assertion that the foreign comment appears in the output fails.
func TestCarriageHeaderComment_ForeignComments_ReEmittedAfterStampBlock(t *testing.T) {
	const foreignComment1 = "# User note: this agent processes documents."
	const foreignComment2 = "# TODO: update model after next evaluation."

	// Prior bytes contain two foreign header comments above the first TOML key.
	// These are non-stamp comments (they do not match "# <deployed_name>: <value>").
	priorBytes := []byte(
		foreignComment1 + "\n" +
			foreignComment2 + "\n" +
			"name = \"a\"\n" +
			"description = \"d\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"body\"\n",
	)

	// Build a canonical that represents the same document (no foreign comments in
	// the canonical; comments are recovered from prior bytes during encode).
	canonical := []byte("---\nname: a\ndescription: d\nsandbox_mode: read-only\n---\nbody\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	// Both foreign header comments must appear in the output. They must be present
	// as raw comment lines (the exact text from the prior bytes).
	if !bytes.Contains(out, []byte(foreignComment1)) {
		t.Errorf("output does not contain foreign header comment %q; prior-bytes header comments must be re-emitted after the stamp block\nOutput:\n%s", foreignComment1, out)
	}
	if !bytes.Contains(out, []byte(foreignComment2)) {
		t.Errorf("output does not contain foreign header comment %q; prior-bytes header comments must be re-emitted after the stamp block\nOutput:\n%s", foreignComment2, out)
	}

	// The foreign comments must appear BEFORE the first native TOML key. Find the
	// byte offset of the first foreign comment and the first non-comment, non-empty line.
	commentPos := bytes.Index(out, []byte(foreignComment1))
	firstKeyPos := -1
	for _, line := range bytes.Split(out, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 && !bytes.HasPrefix(trimmed, []byte("#")) {
			// Use the position of this line's first byte in the output.
			firstKeyPos = bytes.Index(out, line)
			break
		}
	}
	if commentPos >= 0 && firstKeyPos >= 0 && commentPos >= firstKeyPos {
		t.Errorf("foreign header comment appeared AFTER the first TOML key; comments must come before native TOML keys\nOutput:\n%s", out)
	}

	// Ordering assertion (AC4.9a sub-clause): foreign comments must appear AFTER the
	// last stamp-block line. A stamp-block line has the form "# mosaic_<key>: <value>".
	// MOSAIC provenance metadata must sit at the head of the file; user annotations
	// must follow. An implementation that emits foreign comments before the stamp block
	// satisfies the presence and key-position assertions above but violates this sub-clause.
	//
	// We track the byte offset at which the last stamp line ends (including its newline)
	// by scanning all output lines. If the output contains no stamp lines (e.g., because
	// no implementation has been provided), lastStampEnd is zero and the ordering check
	// is not activated -- the presence assertion above already fails for the correct reason.
	lastStampEnd := 0
	scanOffset := 0
	for _, line := range bytes.Split(out, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("# mosaic_")) {
			// Record the byte offset immediately after this stamp line (including newline).
			lastStampEnd = scanOffset + len(line) + 1
		}
		scanOffset += len(line) + 1
	}
	// Determine the position of the first foreign comment in the output.
	firstForeignPos := bytes.Index(out, []byte(foreignComment1))
	if fc2 := bytes.Index(out, []byte(foreignComment2)); fc2 >= 0 && (firstForeignPos < 0 || fc2 < firstForeignPos) {
		firstForeignPos = fc2
	}
	if lastStampEnd > 0 && firstForeignPos >= 0 && firstForeignPos < lastStampEnd {
		t.Errorf("foreign header comment begins at byte %d, before the stamp block ends at byte %d; "+
			"AC4.9a requires foreign comments to appear AFTER the stamp block so MOSAIC metadata "+
			"remains at the head of the file\nOutput:\n%s", firstForeignPos, lastStampEnd, out)
	}
}

// TestCarriageHeaderComment_NoComments_NoneInvented verifies that when the prior
// deployed bytes contain no leading comments, the encode output contains no
// invented comments. Only stamp-block lines (from the agentfields stamp block) are
// permitted as leading comments; the implementation must not invent user-style
// comment lines that were absent from the prior bytes.
//
// This test passes trivially in RED (no implementation) because the encode path does
// not invent comments. It documents the constraint and ensures future changes to the
// header-comment path do not accidentally invent comments.
func TestCarriageHeaderComment_NoComments_NoneInvented(t *testing.T) {
	// Prior bytes have no leading comments: the first line is the stamp block (no,
	// in practice the stamp block is generated by encode from the canonical, not read
	// from the prior bytes). This prior-bytes file has no comments at all.
	priorBytes := []byte(
		"name = \"a\"\n" +
			"description = \"d\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			"developer_instructions = \"body\"\n",
	)

	canonical := []byte("---\nname: a\ndescription: d\nsandbox_mode: read-only\n---\nbody\n")

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	// Every leading comment in the output must be a stamp line of the form
	// "# <key>: <value>". No non-stamp comment may be present in the output when
	// the prior bytes had no foreign header comments.
	for _, rawLine := range bytes.Split(out, []byte("\n")) {
		line := bytes.TrimSpace(rawLine)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("#")) {
			// First non-comment line: we're past the header block.
			break
		}
		// This is a comment line. It must be a stamp line: "# <key>: <value>".
		rest := bytes.TrimPrefix(line, []byte("#"))
		rest = bytes.TrimSpace(rest)
		if !bytes.Contains(rest, []byte(":")) {
			t.Errorf("output contains a non-stamp comment line %q; no foreign comments must be invented when the prior bytes had none\nOutput:\n%s", rawLine, out)
		}
	}
}

// ---------------------------------------------------------------------------
// AC4.11: Re-emission verification pass (fault-injection)
// ---------------------------------------------------------------------------

// TestCarriageVerificationPass_FaultInjection_ReturnsErrMarkerUnrecoverable is the
// fault-injection test proving that the re-emission verification pass fires (AC4.11).
//
// The scenario: a deliberately inconsistent canonical that has the same key in BOTH
// mosaic_carriage (value-carried) AND mosaic_carriage_markers. When encode processes
// this canonical:
//
//  1. The value-carry path emits "conflict_key = <value>" as a native TOML key.
//  2. The marker path extracts the span for "conflict_key" from the prior bytes and
//     re-emits it, producing a second "conflict_key = ..." assignment.
//
// The resulting TOML has a duplicate top-level key. The verification pass, which
// re-parses the emitted TOML and checks for value-tree integrity, must detect this
// and return ErrMarkerUnrecoverable rather than writing a corrupt file.
//
// In RED, no verification pass exists: encode does not re-parse the output. It returns
// nil error (or possibly a different error from something else), so the assertion that
// the error wraps ErrMarkerUnrecoverable fails. A dead-branch verification pass is
// indistinguishable from no pass, which is exactly what this test prevents.
func TestCarriageVerificationPass_FaultInjection_ReturnsErrMarkerUnrecoverable(t *testing.T) {
	const conflictKey = "conflict_key"

	// Build a canonical that has conflict_key in BOTH containers: value-carried AND
	// marker-carried. This is an inconsistent canonical that bypasses the normal
	// decode path (which would place a key in at most one container). It is valid
	// as a fault-injection input because we are testing the encode path's verification
	// backstop, not the decode path's container assignment.
	fmText := "name: a\n" +
		"description: d\n" +
		agentformat.CarriageValuesKey + ":\n" +
		"  " + conflictKey + ": \"value-from-canon\"\n" +
		agentformat.CarriageMarkersKey + ": [\"" + conflictKey + "\"]\n"
	canonical := []byte("---\n" + fmText + "---\nbody\n")

	// Prior bytes contain conflict_key as a top-level scalar. The marker path will
	// extract this span and re-emit it alongside the value-carry path's emission,
	// producing a duplicate-key TOML document.
	priorBytes := []byte(
		"name = \"a\"\n" +
			"description = \"d\"\n" +
			"sandbox_mode = \"read-only\"\n" +
			conflictKey + " = \"value-from-prior\"\n" +
			"developer_instructions = \"body\"\n",
	)

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{
		AgentKey:      "a",
		Op:            agentformat.OpUpdate,
		PriorDeployed: priorBytes,
	}

	_, _, err := tr.Encode(canonical, ctx)
	if err == nil {
		t.Fatal("Encode succeeded for a canonical that would produce a duplicate TOML key; " +
			"the verification pass must detect the reordering hazard and return ErrMarkerUnrecoverable")
	}
	if !errors.Is(err, agentformat.ErrMarkerUnrecoverable) {
		t.Errorf("Encode returned error %v; want error wrapping ErrMarkerUnrecoverable -- "+
			"a duplicate-key conflict detected by the verification pass must use this sentinel", err)
	}
}
