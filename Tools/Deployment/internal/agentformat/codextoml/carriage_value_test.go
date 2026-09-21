package codextoml_test

// carriage_value_test.go covers value-carried user keys (the decode-to-encode path
// for scalars and simple arrays), TOML type fidelity, the value-versus-marker
// boundary, and the "Legacy name is user-owned" assertion.
//
// These tests define the contracts that the carriage decode/encode implementation
// must satisfy. In the RED phase they fail because the decode path still applies
// the Stage 3 interim rule (non-owned keys are absent from canonical output), and
// the encode path does not yet consume the carriage container.

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"mosaic-common/docformat"
	"mosaic-common/mosaic"

	"mosaic-deploy/internal/agentformat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findCarriageValuePair locates a key in the mosaic_carriage mapping of canonical
// output. It returns the FieldValue and true if found, or zero value and false if
// the container is absent or the key is missing.
func findCarriageValuePair(t *testing.T, canonical []byte, key string) (mosaic.FieldValue, bool) {
	t.Helper()
	doc := parseCanonical(t, canonical)
	container, ok := doc.Frontmatter().Get(agentformat.CarriageValuesKey)
	if !ok {
		return mosaic.FieldValue{}, false
	}
	if container.Kind != mosaic.KindMapping {
		t.Fatalf("mosaic_carriage has kind %q; want mapping", container.Kind)
	}
	for _, pair := range container.Pairs {
		if pair.Key == key {
			return pair.Value, true
		}
	}
	return mosaic.FieldValue{}, false
}

// carriageMarkerNames returns the list of marker names from mosaic_carriage_markers,
// or nil if the key is absent.
func carriageMarkerNames(t *testing.T, canonical []byte) []string {
	t.Helper()
	doc := parseCanonical(t, canonical)
	markers, ok := doc.Frontmatter().Get(agentformat.CarriageMarkersKey)
	if !ok {
		return nil
	}
	if markers.Kind != mosaic.KindList {
		t.Fatalf("mosaic_carriage_markers has kind %q; want list", markers.Kind)
	}
	var names []string
	for _, item := range markers.Items {
		names = append(names, item.Scalar)
	}
	return names
}

// buildCanonicalWithCarriage constructs a minimal canonical document with a
// mosaic_carriage mapping. pairs defines the carried key/value pairs. Additional
// owned keys (name, description) are included so the encode path can produce valid
// output. body is the document body.
func buildCanonicalWithCarriage(t *testing.T, pairs []mosaic.FieldPair, body string) []byte {
	t.Helper()
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue("test-agent", mosaic.QuotePlain)},
		{Key: "description", Value: mosaic.ScalarValue("A test agent.", mosaic.QuotePlain)},
		{Key: agentformat.CarriageValuesKey, Value: mosaic.MappingValue(pairs)},
	}
	fmText := docformat.RenderFrontmatter(fields, "\n")
	return []byte("---\n" + fmText + "---\n" + body)
}

// buildCanonicalWithMarkers constructs a minimal canonical document with
// mosaic_carriage_markers listing the given top-level key names.
func buildCanonicalWithMarkers(t *testing.T, markerKeys []string, body string) []byte {
	t.Helper()
	items := make([]mosaic.FieldValue, len(markerKeys))
	for i, k := range markerKeys {
		items[i] = mosaic.ScalarValue(k, mosaic.QuoteDouble)
	}
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue("test-agent", mosaic.QuotePlain)},
		{Key: "description", Value: mosaic.ScalarValue("A test agent.", mosaic.QuotePlain)},
		{Key: agentformat.CarriageMarkersKey, Value: mosaic.ListValue(items, mosaic.ListFlow)},
	}
	fmText := docformat.RenderFrontmatter(fields, "\n")
	return []byte("---\n" + fmText + "---\n" + body)
}

// parseTomlMap parses TOML bytes into a map, failing the test on error.
func parseTomlMap(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("TOML parse failed: %v\nBytes:\n%s", err, b)
	}
	return m
}

// ---------------------------------------------------------------------------
// Decode side: user keys land in mosaic_carriage
// ---------------------------------------------------------------------------

// TestCarriageValueDecode_UserString_LandsInContainer verifies that a user-added
// TOML string key ends up in mosaic_carriage with QuoteDouble, so the encode path
// can recover the string type from the quote style.
func TestCarriageValueDecode_UserString_LandsInContainer(t *testing.T) {
	input := []byte("name = \"a\"\nmy_key = \"hello\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "my_key")
	if !ok {
		t.Fatal("my_key absent from mosaic_carriage; user string key must be placed in carriage container on decode")
	}
	if fv.Kind != mosaic.KindScalar {
		t.Fatalf("my_key in mosaic_carriage has kind %q; want scalar", fv.Kind)
	}
	if fv.Quote != mosaic.QuoteDouble {
		t.Errorf("my_key quote style = %q; want QuoteDouble (TOML string type marker)", fv.Quote)
	}
	if fv.Scalar != "hello" {
		t.Errorf("my_key value = %q; want \"hello\"", fv.Scalar)
	}
}

// TestCarriageValueDecode_UserInteger_LandsInContainerUnquoted verifies that a
// user-added TOML integer decodes into mosaic_carriage as a QuotePlain scalar, so
// the encode path can re-emit it as a TOML integer rather than a string.
func TestCarriageValueDecode_UserInteger_LandsInContainerUnquoted(t *testing.T) {
	input := []byte("name = \"a\"\nmax_threads = 4\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "max_threads")
	if !ok {
		t.Fatal("max_threads absent from mosaic_carriage; user integer key must be placed in carriage container on decode")
	}
	if fv.Kind != mosaic.KindScalar {
		t.Fatalf("max_threads in mosaic_carriage has kind %q; want scalar", fv.Kind)
	}
	if fv.Quote != mosaic.QuotePlain {
		t.Errorf("max_threads quote style = %q; want QuotePlain (TOML integer type marker)", fv.Quote)
	}
	if fv.Scalar != "4" {
		t.Errorf("max_threads value = %q; want \"4\"", fv.Scalar)
	}
}

// TestCarriageValueDecode_UserBoolean_LandsInContainerUnquoted verifies that a
// user-added TOML boolean decodes into mosaic_carriage as a QuotePlain scalar.
func TestCarriageValueDecode_UserBoolean_LandsInContainerUnquoted(t *testing.T) {
	input := []byte("name = \"a\"\nflag = true\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "flag")
	if !ok {
		t.Fatal("flag absent from mosaic_carriage; user boolean key must be placed in carriage container on decode")
	}
	if fv.Quote != mosaic.QuotePlain {
		t.Errorf("flag quote style = %q; want QuotePlain (TOML boolean type marker)", fv.Quote)
	}
}

// TestCarriageValueDecode_StringShapedLikeInteger_StaysString verifies that a
// user-added TOML string whose content looks like an integer ("3") lands in
// mosaic_carriage as a QuoteDouble scalar. This is the key type-fidelity assertion:
// version = "3" must not come back as the integer 3.
func TestCarriageValueDecode_StringShapedLikeInteger_StaysString(t *testing.T) {
	// "version" is also a Legacy agentfields name -- this test simultaneously
	// pins the carriage behavior for Legacy names (see the separate test below).
	input := []byte("name = \"a\"\nversion = \"3\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "version")
	if !ok {
		t.Fatal("version absent from mosaic_carriage; string-shaped-like-integer user key must be placed in carriage container as a string")
	}
	if fv.Quote != mosaic.QuoteDouble {
		t.Errorf("version quote style = %q; want QuoteDouble (original value is a TOML string)", fv.Quote)
	}
	if fv.Scalar != "3" {
		t.Errorf("version value = %q; want \"3\"", fv.Scalar)
	}
}

// TestCarriageValueDecode_StringShapedLikeBoolean_StaysString verifies that a
// TOML string whose content is "true" or "false" is carried as QuoteDouble.
func TestCarriageValueDecode_StringShapedLikeBoolean_StaysString(t *testing.T) {
	input := []byte("name = \"a\"\npinned = \"true\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "pinned")
	if !ok {
		t.Fatal("pinned absent from mosaic_carriage")
	}
	if fv.Quote != mosaic.QuoteDouble {
		t.Errorf("pinned quote style = %q; want QuoteDouble (value is a TOML string)", fv.Quote)
	}
	if fv.Scalar != "true" {
		t.Errorf("pinned value = %q; want \"true\"", fv.Scalar)
	}
}

// TestCarriageValueDecode_StringShapedLikeDate_StaysString verifies that a TOML
// string whose content matches a date literal ("1979-05-27") is carried as QuoteDouble.
func TestCarriageValueDecode_StringShapedLikeDate_StaysString(t *testing.T) {
	input := []byte("name = \"a\"\nstamp = \"1979-05-27\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "stamp")
	if !ok {
		t.Fatal("stamp absent from mosaic_carriage")
	}
	if fv.Quote != mosaic.QuoteDouble {
		t.Errorf("stamp quote style = %q; want QuoteDouble (value is a TOML string)", fv.Quote)
	}
}

// TestCarriageValueDecode_LegacyName_version_IsUserOwned pins that a TOML key
// literally named "version" is user-owned and lands in mosaic_carriage, not dropped
// or treated as a MOSAIC stamp. "version" is a Legacy name in agentfields (its Deployed
// name is "mosaic_version"), and isMosaicOwned must not match Legacy names.
func TestCarriageValueDecode_LegacyName_version_IsUserOwned(t *testing.T) {
	input := []byte("name = \"a\"\nversion = \"user-version\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	_, ok := findCarriageValuePair(t, canonical, "version")
	if !ok {
		t.Fatal("version absent from mosaic_carriage; a key literally named \"version\" is a Legacy agentfields name and must be user-owned (carried), not dropped")
	}
}

// TestCarriageValueDecode_MOSAICOwned_NotInContainer verifies that MOSAIC-owned keys
// (name, description, sandbox_mode) are NOT placed in the carriage container.
func TestCarriageValueDecode_MOSAICOwned_NotInContainer(t *testing.T) {
	input := []byte("name = \"a\"\ndescription = \"d\"\nsandbox_mode = \"read-only\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	doc := parseCanonical(t, canonical)
	container, ok := doc.Frontmatter().Get(agentformat.CarriageValuesKey)
	if !ok {
		return // container absent is fine -- no user keys were present
	}
	for _, pair := range container.Pairs {
		switch pair.Key {
		case "name", "description", "sandbox_mode":
			t.Errorf("MOSAIC-owned key %q appeared in mosaic_carriage; owned keys must not be placed in the carriage container", pair.Key)
		}
	}
}

// TestCarriageValueDecode_sandbox_mode_NeverInContainer pins that sandbox_mode
// (MOSAIC-owned but not in agentfields) is never placed in the carriage container.
// This is the specific case the isMosaicOwned predicate must handle: sandbox_mode is
// not in agentfields, so an agentfields-only check would misclassify it as user-owned.
func TestCarriageValueDecode_sandbox_mode_NeverInContainer(t *testing.T) {
	input := []byte("name = \"a\"\nsandbox_mode = \"workspace-write\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	_, ok := findCarriageValuePair(t, canonical, "sandbox_mode")
	if ok {
		t.Error("sandbox_mode appeared in mosaic_carriage; it is MOSAIC-owned and must never be placed in the carriage container")
	}
}

// ---------------------------------------------------------------------------
// Value-vs-marker boundary: decode side
// ---------------------------------------------------------------------------

// TestCarriageValueDecode_DatetimeLiteral_IsMarkerCarried verifies that a TOML
// local-date literal (user_date = 1979-05-27, a bare TOML date, not a string)
// is classified as marker-carried and does not appear in mosaic_carriage values.
// Datetimes are always marker-carried by the shape rule in I4.1: the prior-bytes
// channel re-emits the user's original datetime text exactly, preserving precision
// and format, while the translator layer stays free of any time import.
func TestCarriageValueDecode_DatetimeLiteral_IsMarkerCarried(t *testing.T) {
	// 1979-05-27 is a TOML local-date literal, not a string.
	input := []byte("name = \"a\"\nuser_date = 1979-05-27\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	_, inValues := findCarriageValuePair(t, canonical, "user_date")
	if inValues {
		t.Error("user_date appeared in mosaic_carriage values; TOML datetime literals must be marker-carried by the shape rule")
	}

	markers := carriageMarkerNames(t, canonical)
	for _, name := range markers {
		if name == "user_date" {
			return // correctly marker-carried
		}
	}
	t.Errorf("user_date absent from mosaic_carriage_markers; TOML local-date literals must be marker-carried, got markers: %v", markers)
}

// TestCarriageValueDecode_UserFloat_LandsInContainerUnquoted verifies that a
// user-added TOML float with a plain-decimal spelling decodes into mosaic_carriage
// as a QuotePlain scalar. If the library cannot recover the source spelling (the
// exotic-spelling branch), escalation to a marker is also acceptable; what is
// forbidden is the float silently becoming a string.
func TestCarriageValueDecode_UserFloat_LandsInContainerUnquoted(t *testing.T) {
	input := []byte("name = \"a\"\npi = 3.14\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "pi")
	if !ok {
		// Escalation to a marker is an acceptable outcome for the exotic-spelling branch.
		markers := carriageMarkerNames(t, canonical)
		for _, name := range markers {
			if name == "pi" {
				return // escalated to marker -- acceptable
			}
		}
		t.Fatal("pi absent from both mosaic_carriage and mosaic_carriage_markers; a user float key must be carried in one of the two containers")
	}

	if fv.Kind != mosaic.KindScalar {
		t.Fatalf("pi in mosaic_carriage has kind %q; want scalar", fv.Kind)
	}
	if fv.Quote != mosaic.QuotePlain {
		t.Errorf("pi quote style = %q; want QuotePlain (TOML float type marker; QuoteDouble would convert it to a string on encode)", fv.Quote)
	}
}

// TestCarriageValueDecode_StringShapedLikeHex_StaysString verifies that a TOML
// string whose content looks like a hex literal ("0x1F") decodes as QuoteDouble.
// The encode path must re-emit it as a TOML string, not parse the content and
// emit the hex integer 31.
func TestCarriageValueDecode_StringShapedLikeHex_StaysString(t *testing.T) {
	input := []byte("name = \"a\"\nmask = \"0x1F\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "mask")
	if !ok {
		t.Fatal("mask absent from mosaic_carriage; a string-shaped-like-hex TOML string must be placed in the carriage container")
	}
	if fv.Quote != mosaic.QuoteDouble {
		t.Errorf("mask quote style = %q; want QuoteDouble (original value is a TOML string, not a hex integer)", fv.Quote)
	}
	if fv.Scalar != "0x1F" {
		t.Errorf("mask value = %q; want \"0x1F\"", fv.Scalar)
	}
}

// TestCarriageValueDecode_StringShapedLikeUnderscoreInt_StaysString verifies that a
// TOML string whose content looks like an underscore-separated integer ("1_000")
// decodes as QuoteDouble. The encode path must re-emit it as a TOML string.
func TestCarriageValueDecode_StringShapedLikeUnderscoreInt_StaysString(t *testing.T) {
	input := []byte("name = \"a\"\ncount = \"1_000\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	fv, ok := findCarriageValuePair(t, canonical, "count")
	if !ok {
		t.Fatal("count absent from mosaic_carriage; a string-shaped-like-underscore-int TOML string must be placed in the carriage container")
	}
	if fv.Quote != mosaic.QuoteDouble {
		t.Errorf("count quote style = %q; want QuoteDouble (original value is a TOML string, not an integer)", fv.Quote)
	}
	if fv.Scalar != "1_000" {
		t.Errorf("count value = %q; want \"1_000\"", fv.Scalar)
	}
}

// TestCarriageValueDecode_NestedTable_IsMarkerCarried verifies that a TOML nested
// table key is placed in mosaic_carriage_markers (not mosaic_carriage). Tables are
// always marker-carried regardless of depth, by the shape rule in I4.1.
func TestCarriageValueDecode_NestedTable_IsMarkerCarried(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\n[mcp_servers.github]\nurl = \"https://example.com\"\n")
	canonical := decodeToml(t, input, "a")

	// Must be in markers, not values.
	_, inValues := findCarriageValuePair(t, canonical, "mcp_servers")
	if inValues {
		t.Error("mcp_servers appeared in mosaic_carriage values; nested tables must be marker-carried")
	}

	markers := carriageMarkerNames(t, canonical)
	for _, name := range markers {
		if name == "mcp_servers" {
			return // found in markers as expected
		}
	}
	t.Errorf("mcp_servers absent from mosaic_carriage_markers; nested tables must be marker-carried, got markers: %v", markers)
}

// TestCarriageValueDecode_ArrayOfTables_IsMarkerCarried verifies that an array-of-tables
// header is marker-carried, never value-carried.
func TestCarriageValueDecode_ArrayOfTables_IsMarkerCarried(t *testing.T) {
	input := []byte("name = \"a\"\ndeveloper_instructions = \"body\"\n[[items]]\nid = 1\n")
	canonical := decodeToml(t, input, "a")

	_, inValues := findCarriageValuePair(t, canonical, "items")
	if inValues {
		t.Error("items appeared in mosaic_carriage values; array-of-tables must be marker-carried")
	}

	markers := carriageMarkerNames(t, canonical)
	for _, name := range markers {
		if name == "items" {
			return
		}
	}
	t.Errorf("items absent from mosaic_carriage_markers; array-of-tables must be marker-carried, got markers: %v", markers)
}

// TestCarriageValueDecode_MultipleUserKeys_BothPresent verifies that when a TOML file
// has multiple user-owned scalar keys alongside MOSAIC-owned keys, all user keys appear
// in mosaic_carriage and none appear in the top-level canonical frontmatter.
func TestCarriageValueDecode_MultipleUserKeys_BothPresent(t *testing.T) {
	input := []byte("name = \"a\"\ndescription = \"d\"\nnickname_candidates = \"buddy\"\nmax_threads = 4\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	// Both user keys must be in the carriage container.
	_, ok1 := findCarriageValuePair(t, canonical, "nickname_candidates")
	if !ok1 {
		t.Error("nickname_candidates absent from mosaic_carriage; user string key must be carried")
	}

	_, ok2 := findCarriageValuePair(t, canonical, "max_threads")
	if !ok2 {
		t.Error("max_threads absent from mosaic_carriage; user integer key must be carried")
	}

	// User keys must not appear at canonical top level (outside the container).
	doc := parseCanonical(t, canonical)
	for _, key := range []string{"nickname_candidates", "max_threads"} {
		fv, ok := doc.Frontmatter().Get(key)
		if ok && fv.Kind != mosaic.KindMapping { // the container itself is a mapping, skip
			t.Errorf("user key %q appeared at canonical top level; user keys must be inside mosaic_carriage only", key)
		}
	}
}

// ---------------------------------------------------------------------------
// Encode side: container consumed, native TOML keys emitted
// ---------------------------------------------------------------------------

// TestCarriageValueEncode_DoubleQuotedPair_EmitsTomlString verifies that a
// mosaic_carriage pair with QuoteDouble is re-emitted by encode as a TOML string.
// This is the fundamental type-recovery assertion: the encode path must read Quote==
// QuoteDouble and produce a quoted TOML string, never an integer.
func TestCarriageValueEncode_DoubleQuotedPair_EmitsTomlString(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "my_key", Value: mosaic.ScalarValue("hello", mosaic.QuoteDouble)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	m := parseTomlMap(t, out)
	val, exists := m["my_key"]
	if !exists {
		t.Fatal("my_key absent from encoded TOML; value-carried key must be re-emitted as a native TOML key")
	}
	sval, ok := val.(string)
	if !ok {
		t.Errorf("my_key has type %T; want string (QuoteDouble in container means TOML string)", val)
		return
	}
	if sval != "hello" {
		t.Errorf("my_key = %q; want \"hello\"", sval)
	}
}

// TestCarriageValueEncode_PlainQuotedInteger_EmitsTomlInteger verifies that a
// mosaic_carriage pair with QuotePlain is re-emitted by encode as a TOML integer,
// not a quoted string.
func TestCarriageValueEncode_PlainQuotedInteger_EmitsTomlInteger(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "max_threads", Value: mosaic.ScalarValue("4", mosaic.QuotePlain)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	m := parseTomlMap(t, out)
	val, exists := m["max_threads"]
	if !exists {
		t.Fatal("max_threads absent from encoded TOML; value-carried key must be re-emitted as a native TOML key")
	}
	if _, ok := val.(int64); !ok {
		t.Fatalf("max_threads has type %T; want int64 (QuotePlain in container means TOML integer)", val)
	}
}

// TestCarriageValueEncode_StringShapedLikeInteger_StaysString verifies end-to-end
// type fidelity: version = "3" (TOML string) → mosaic_carriage QuoteDouble → TOML
// string "3", not integer 3. This is the defect the quote-style type marker prevents.
func TestCarriageValueEncode_StringShapedLikeInteger_StaysString(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "version", Value: mosaic.ScalarValue("3", mosaic.QuoteDouble)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	m := parseTomlMap(t, out)
	val, exists := m["version"]
	if !exists {
		t.Fatal("version absent from encoded TOML; must be re-emitted from carriage container")
	}
	sval, ok := val.(string)
	if !ok {
		t.Errorf("version has type %T; want string -- version = \"3\" is a TOML string and must not silently become an integer", val)
		return
	}
	if sval != "3" {
		t.Errorf("version = %q; want \"3\"", sval)
	}
}

// TestCarriageValueEncode_ContainerKeyAbsentFromOutput verifies that the carriage
// container key itself never appears in the encoded TOML output. The container is
// consumed by encode and must not be written as a TOML key.
func TestCarriageValueEncode_ContainerKeyAbsentFromOutput(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "my_key", Value: mosaic.ScalarValue("hello", mosaic.QuoteDouble)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	m := parseTomlMap(t, out)
	if _, exists := m[agentformat.CarriageValuesKey]; exists {
		t.Errorf("encoded TOML contains %q; the carriage container must never appear in encoded output", agentformat.CarriageValuesKey)
	}
	if _, exists := m[agentformat.CarriageMarkersKey]; exists {
		t.Errorf("encoded TOML contains %q; the carriage container must never appear in encoded output", agentformat.CarriageMarkersKey)
	}
}

// TestCarriageValueEncode_Report_ContainsCarriedEntry verifies that the report
// produced by encode contains EntryCarriedContainer for each re-emitted user key.
// This proves container consumption is reported, not silent.
func TestCarriageValueEncode_Report_ContainsCarriedEntry(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "my_key", Value: mosaic.ScalarValue("hello", mosaic.QuoteDouble)},
		{Key: "count", Value: mosaic.ScalarValue("7", mosaic.QuotePlain)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	_, report, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	var carriedKeys []string
	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryCarriedContainer {
			carriedKeys = append(carriedKeys, entry.Key)
		}
	}
	for _, wantKey := range []string{"my_key", "count"} {
		found := false
		for _, k := range carriedKeys {
			if k == wantKey {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("report has no EntryCarriedContainer for key %q; container consumption must be reported for each re-emitted key", wantKey)
		}
	}
}

// TestCarriageValueEncode_PlainQuotedFloat_EmitsTomlFloat verifies that a
// mosaic_carriage pair with QuotePlain and a decimal float value is re-emitted by
// encode as a TOML float, not a quoted string. This tests the float branch of the
// type-recovery rule: QuotePlain means integer, float or boolean; encode must not
// wrap the value in quotes.
func TestCarriageValueEncode_PlainQuotedFloat_EmitsTomlFloat(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "pi", Value: mosaic.ScalarValue("3.14", mosaic.QuotePlain)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	m := parseTomlMap(t, out)
	val, exists := m["pi"]
	if !exists {
		t.Fatal("pi absent from encoded TOML; a value-carried float key must be re-emitted as a native TOML key")
	}
	if _, ok := val.(float64); !ok {
		t.Errorf("pi has type %T; want float64 (QuotePlain with decimal spelling means TOML float, not string)", val)
	}
}

// TestCarriageValueEncode_StringShapedLikeFloat_StaysString verifies that a
// mosaic_carriage pair with QuoteDouble and a float-shaped value ("3.14") is
// re-emitted as a TOML string, not a float. This is the mirror direction of
// TestCarriageValueEncode_PlainQuotedFloat_EmitsTomlFloat.
func TestCarriageValueEncode_StringShapedLikeFloat_StaysString(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "version", Value: mosaic.ScalarValue("3.14", mosaic.QuoteDouble)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	m := parseTomlMap(t, out)
	val, exists := m["version"]
	if !exists {
		t.Fatal("version absent from encoded TOML")
	}
	if sval, ok := val.(string); !ok {
		t.Errorf("version has type %T; want string -- version = \"3.14\" is a TOML string and must not become a float after encode", val)
	} else if sval != "3.14" {
		t.Errorf("version = %q; want \"3.14\"", sval)
	}
}

// TestCarriageValueEncode_StringShapedLikeHex_StaysString verifies the encode-side
// type-fidelity for a hex-shaped string: a QuoteDouble pair with scalar "0x1F" must
// be re-emitted as the TOML string "0x1F", not as the integer 31 (0x1F decoded).
func TestCarriageValueEncode_StringShapedLikeHex_StaysString(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "mask", Value: mosaic.ScalarValue("0x1F", mosaic.QuoteDouble)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	m := parseTomlMap(t, out)
	val, exists := m["mask"]
	if !exists {
		t.Fatal("mask absent from encoded TOML")
	}
	if sval, ok := val.(string); !ok {
		t.Errorf("mask has type %T; want string -- mask = \"0x1F\" is a TOML string and must not become an integer after encode", val)
	} else if sval != "0x1F" {
		t.Errorf("mask = %q; want \"0x1F\"", sval)
	}
}

// TestCarriageValueEncode_StringShapedLikeUnderscoreInt_StaysString verifies the
// encode-side type-fidelity for an underscore-int-shaped string: a QuoteDouble pair
// with scalar "1_000" must be re-emitted as the TOML string "1_000", not as the
// integer 1000.
func TestCarriageValueEncode_StringShapedLikeUnderscoreInt_StaysString(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "count", Value: mosaic.ScalarValue("1_000", mosaic.QuoteDouble)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	out, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	m := parseTomlMap(t, out)
	val, exists := m["count"]
	if !exists {
		t.Fatal("count absent from encoded TOML")
	}
	if sval, ok := val.(string); !ok {
		t.Errorf("count has type %T; want string -- count = \"1_000\" is a TOML string and must not become an integer after encode", val)
	} else if sval != "1_000" {
		t.Errorf("count = %q; want \"1_000\"", sval)
	}
}

// TestCarriageValueEncode_NoDroppedForeignKey_ForContainerKeys verifies that the
// encode path does NOT produce EntryDroppedForeignKey for the container key or its
// contents. EntryDroppedForeignKey is for truly foreign keys; the container is handled
// by the carriage path.
func TestCarriageValueEncode_NoDroppedForeignKey_ForContainerKeys(t *testing.T) {
	pairs := []mosaic.FieldPair{
		{Key: "my_key", Value: mosaic.ScalarValue("hello", mosaic.QuoteDouble)},
	}
	canonical := buildCanonicalWithCarriage(t, pairs, "body\n")
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: "test-agent", Op: agentformat.OpCreate}

	_, report, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode returned unexpected error: %v", err)
	}

	for _, entry := range report.Entries {
		if entry.Kind == agentformat.EntryDroppedForeignKey {
			if entry.Key == agentformat.CarriageValuesKey || entry.Key == agentformat.CarriageMarkersKey {
				t.Errorf("report has EntryDroppedForeignKey for %q; the carriage container must be handled by the carriage path, not as a foreign key", entry.Key)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Full round-trip: decode(encode(D)) preserves user key type
// ---------------------------------------------------------------------------

// TestCarriageValue_RoundTrip_UserStringKeyPreservesType verifies that a user TOML
// string key survives the full decode-encode-decode round trip as a TOML string.
// After encode, the TOML output must have the key as a string. After re-decode, the
// key must appear in mosaic_carriage with QuoteDouble.
func TestCarriageValue_RoundTrip_UserStringKeyPreservesType(t *testing.T) {
	const agentKey = "rt-agent"
	input := []byte("name = \"" + agentKey + "\"\ndescription = \"d\"\nmy_str = \"hello\"\ndeveloper_instructions = \"body\"\n")

	// First pass: decode to canonical.
	canonical := decodeToml(t, input, agentKey)

	// Verify user key is in container with correct type.
	fv, ok := findCarriageValuePair(t, canonical, "my_str")
	if !ok {
		t.Fatal("my_str absent from mosaic_carriage after decode; cannot proceed with round-trip")
	}
	if fv.Quote != mosaic.QuoteDouble {
		t.Errorf("my_str quote after decode = %q; want QuoteDouble", fv.Quote)
	}

	// Second pass: encode to TOML, verify type.
	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: agentKey, Op: agentformat.OpCreate}
	tomlBytes, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	m := parseTomlMap(t, tomlBytes)
	if _, ok := m["my_str"].(string); !ok {
		t.Errorf("my_str in re-encoded TOML has type %T; want string", m["my_str"])
	}
}

// TestCarriageValue_RoundTrip_UserIntegerKeyPreservesType verifies that a user TOML
// integer key survives the full round trip as a TOML integer.
func TestCarriageValue_RoundTrip_UserIntegerKeyPreservesType(t *testing.T) {
	const agentKey = "rt-agent2"
	input := []byte("name = \"" + agentKey + "\"\ndescription = \"d\"\nmax_threads = 4\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, agentKey)

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: agentKey, Op: agentformat.OpCreate}
	tomlBytes, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	m := parseTomlMap(t, tomlBytes)
	if _, ok := m["max_threads"].(int64); !ok {
		t.Errorf("max_threads in re-encoded TOML has type %T; want int64", m["max_threads"])
	}
}

// TestCarriageValue_RoundTrip_StringShapedLikeInteger_NeverBecomesInteger is the
// critical type-fidelity assertion over a full round trip: version = "3" (TOML string)
// must survive as a TOML string, never silently becoming the integer 3.
func TestCarriageValue_RoundTrip_StringShapedLikeInteger_NeverBecomesInteger(t *testing.T) {
	const agentKey = "rt-agent3"
	// "version" is also a Legacy agentfields name, confirming it is user-owned.
	input := []byte("name = \"" + agentKey + "\"\ndescription = \"d\"\nversion = \"3\"\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, agentKey)

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: agentKey, Op: agentformat.OpCreate}
	tomlBytes, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	m := parseTomlMap(t, tomlBytes)
	versionVal, exists := m["version"]
	if !exists {
		t.Fatal("version absent from re-encoded TOML; must be re-emitted from carriage container")
	}
	if _, ok := versionVal.(string); !ok {
		t.Errorf("version in re-encoded TOML has type %T; want string -- a TOML string must not silently become an integer after a round trip", versionVal)
		return
	}
	if versionVal.(string) != "3" {
		t.Errorf("version = %q; want \"3\"", versionVal.(string))
	}
}

// TestCarriageValue_RoundTrip_ExoticSpelling_EscalatedToMarker pins the exotic-
// spelling escalation rule (from the scalar spelling branch comment in carriage.go):
// a TOML integer with an exotic spelling (0x1F, 1_000, +5) is escalated to marker
// carriage because pelletier/go-toml/v2 does not recover the source spelling.
// After escalation, the key must appear in mosaic_carriage_markers, not in
// mosaic_carriage values.
//
// Note: testing the re-emission from prior bytes is in the marker channel tests.
// This test only asserts the placement decision.
func TestCarriageValue_RoundTrip_ExoticSpelling_EscalatedToMarker(t *testing.T) {
	// 0x1F is a hex integer spelling that the library decodes as int64(31); the
	// source spelling "0x1F" is lost. The carriage stage must escalate to markers.
	input := []byte("name = \"a\"\nhex_val = 0x1F\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	_, inValues := findCarriageValuePair(t, canonical, "hex_val")
	if inValues {
		t.Error("hex_val appeared in mosaic_carriage values; exotic-spelled integers must be escalated to marker carriage")
	}

	markers := carriageMarkerNames(t, canonical)
	for _, name := range markers {
		if name == "hex_val" {
			return // correctly escalated
		}
	}
	t.Errorf("hex_val absent from mosaic_carriage_markers; exotic-spelled integers must be escalated to markers, got markers: %v", markers)
}

// TestCarriageValue_RoundTrip_FloatPreservesType verifies that a user TOML float key
// survives the full decode-encode round trip as a TOML float (not a string). After
// encode, the TOML output must have the key as float64.
func TestCarriageValue_RoundTrip_FloatPreservesType(t *testing.T) {
	const agentKey = "rt-float-agent"
	input := []byte("name = \"" + agentKey + "\"\ndescription = \"d\"\npi = 3.14\ndeveloper_instructions = \"body\"\n")

	canonical := decodeToml(t, input, agentKey)

	// Acceptable: pi may be value-carried (QuotePlain) or marker-carried (escalated).
	// If marker-carried, skip the value-type assertion here -- the recovery of an
	// escalated float from prior bytes via the marker channel is covered by
	// TestCarriageLossless_ExoticSpelledScalar_MarkerRecovery in carriage_marker_test.go.
	_, inValues := findCarriageValuePair(t, canonical, "pi")
	if !inValues {
		markers := carriageMarkerNames(t, canonical)
		for _, name := range markers {
			if name == "pi" {
				t.Skip("pi escalated to marker carriage; marker recovery is tested in TestCarriageLossless_ExoticSpelledScalar_MarkerRecovery")
			}
		}
		t.Fatal("pi absent from both carriage containers after decode")
	}

	tr := codexTranslator(t)
	ctx := agentformat.ArtifactContext{AgentKey: agentKey, Op: agentformat.OpCreate}
	tomlBytes, _, err := tr.Encode(canonical, ctx)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	m := parseTomlMap(t, tomlBytes)
	if _, ok := m["pi"].(float64); !ok {
		t.Errorf("pi in re-encoded TOML has type %T; want float64 (a TOML float must not silently become a string after a round trip)", m["pi"])
	}
}

// TestCarriageValue_ExoticSpellings_EscalatedToMarker verifies that TOML integer and
// float values with exotic spellings (1_000, +5, inf, nan) are escalated to marker
// carriage. The pelletier/go-toml/v2 library does not recover source spellings for
// these forms (they decode to normalised Go values), so value-carrying would produce
// a silently renormalised spelling. Each exotic-spelled key must appear in
// mosaic_carriage_markers, not in mosaic_carriage values.
func TestCarriageValue_ExoticSpellings_EscalatedToMarker(t *testing.T) {
	cases := []struct {
		name     string
		tomlLine string // TOML assignment line to inject into the input
		key      string // key name to look up in the carriage containers
	}{
		{
			name:     "underscore_separated_integer",
			tomlLine: "count_val = 1_000",
			key:      "count_val",
		},
		{
			name:     "positive_signed_integer",
			tomlLine: "pos_val = +5",
			key:      "pos_val",
		},
		{
			name:     "float_inf",
			tomlLine: "inf_val = inf",
			key:      "inf_val",
		},
		{
			name:     "float_nan",
			tomlLine: "nan_val = nan",
			key:      "nan_val",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			input := []byte("name = \"a\"\n" + tc.tomlLine + "\ndeveloper_instructions = \"body\"\n")
			canonical := decodeToml(t, input, "a")

			_, inValues := findCarriageValuePair(t, canonical, tc.key)
			if inValues {
				t.Errorf("%s appeared in mosaic_carriage values; exotic-spelled integers and floats must be escalated to marker carriage because the source spelling is not recoverable", tc.key)
			}

			markers := carriageMarkerNames(t, canonical)
			for _, name := range markers {
				if name == tc.key {
					return // correctly escalated to markers
				}
			}
			t.Errorf("%s absent from mosaic_carriage_markers; exotic-spelled integers and floats must be escalated to markers, got markers: %v", tc.key, markers)
		})
	}
}

// ---------------------------------------------------------------------------
// AC4.12: docformat.Parse flow-list round-trip for hostile elements (I4.7)
// ---------------------------------------------------------------------------

// TestCarriageValue_DocformatParse_FlowListRoundTrip tests the docformat.Parse
// round-trip for a mosaic_carriage mapping that holds a flow-list pair with hostile
// elements: a comma-and-space sequence, a closing bracket, an escaped double quote,
// and an empty string. These are the elements named in I4.7 as the definitive probe
// for whether the flow-list shape can carry hostile array elements.
//
// If this test passes, arrays are value-carried via the flow-list path. If it fails,
// arrays escalate to markers and the "arrays escalate to markers" branch must be
// recorded in the carriage doc comment (carriage.go).
//
// This test operates at the docformat layer, not the translator layer: it renders a
// mosaic_carriage mapping with the hostile flow-list pair, parses the rendered bytes
// with docformat.Parse, and asserts the parsed items equal the originals element for
// element.
func TestCarriageValue_DocformatParse_FlowListRoundTrip(t *testing.T) {
	// The hostile elements from I4.7:
	//   - ", " : contains the flow-list delimiter sequence (comma+space)
	//   - "]"  : contains the flow-list closing bracket
	//   - '"'  : a double-quote character (the result of the TOML escape \")
	//   - ""   : an empty string
	wantItems := []mosaic.FieldValue{
		mosaic.ScalarValue(", ", mosaic.QuoteDouble),
		mosaic.ScalarValue("]", mosaic.QuoteDouble),
		mosaic.ScalarValue(`"`, mosaic.QuoteDouble),
		mosaic.ScalarValue("", mosaic.QuoteDouble),
	}

	pairs := []mosaic.FieldPair{
		{Key: "hostile_array", Value: mosaic.ListValue(wantItems, mosaic.ListFlow)},
	}
	fields := []mosaic.FrontmatterField{
		{Key: "name", Value: mosaic.ScalarValue("rt-agent", mosaic.QuotePlain)},
		{Key: agentformat.CarriageValuesKey, Value: mosaic.MappingValue(pairs)},
	}
	rendered := docformat.RenderFrontmatter(fields, "\n")
	src := []byte("---\n" + rendered + "---\nbody\n")

	// Parse the rendered bytes back.
	doc, parseErr := docformat.Parse(src)
	if parseErr != nil {
		t.Fatalf("docformat.Parse failed on rendered mosaic_carriage with hostile flow-list: %v\nSource:\n%s", parseErr, src)
	}

	// Retrieve the mosaic_carriage mapping.
	containerFV, ok := doc.Frontmatter().Get(agentformat.CarriageValuesKey)
	if !ok {
		t.Fatalf("mosaic_carriage absent from parsed document; the flow-list pair must survive the round-trip\nSource:\n%s", src)
	}
	if containerFV.Kind != mosaic.KindMapping {
		t.Fatalf("mosaic_carriage has kind %q after parse; want mapping", containerFV.Kind)
	}

	// Find the hostile_array pair.
	var parsedArray *mosaic.FieldValue
	for i := range containerFV.Pairs {
		if containerFV.Pairs[i].Key == "hostile_array" {
			v := containerFV.Pairs[i].Value
			parsedArray = &v
			break
		}
	}
	if parsedArray == nil {
		t.Fatalf("hostile_array absent from parsed mosaic_carriage; the array key must survive the round-trip\nSource:\n%s", src)
	}
	if parsedArray.Kind != mosaic.KindList {
		t.Fatalf("hostile_array has kind %q after parse; want list", parsedArray.Kind)
	}

	// Assert each element equals the original by text and quote style.
	if len(parsedArray.Items) != len(wantItems) {
		t.Fatalf("hostile_array has %d items after parse; want %d\nSource:\n%s", len(parsedArray.Items), len(wantItems), src)
	}
	for i, want := range wantItems {
		got := parsedArray.Items[i]
		if got.Scalar != want.Scalar {
			t.Errorf("hostile_array[%d] scalar = %q; want %q (element must survive flow-list round-trip unchanged)", i, got.Scalar, want.Scalar)
		}
		if got.Quote != want.Quote {
			t.Errorf("hostile_array[%d] quote = %q; want %q (all string elements must be QuoteDouble)", i, got.Quote, want.Quote)
		}
	}
}

// TestCarriageValue_ContainerPairs_AscendingByteOrder verifies that the pairs inside
// mosaic_carriage appear in ascending byte order of key, per the canonicalisation rule.
func TestCarriageValue_ContainerPairs_AscendingByteOrder(t *testing.T) {
	// Keys: "alpha", "beta", "gamma" -- in TOML they appear in reverse order.
	input := []byte("name = \"a\"\ngamma = \"g\"\nalpha = \"a\"\nbeta = \"b\"\ndeveloper_instructions = \"body\"\n")
	canonical := decodeToml(t, input, "a")

	doc := parseCanonical(t, canonical)
	container, ok := doc.Frontmatter().Get(agentformat.CarriageValuesKey)
	if !ok {
		t.Fatal("mosaic_carriage absent; cannot check pair order")
	}

	var pairKeys []string
	for _, pair := range container.Pairs {
		pairKeys = append(pairKeys, pair.Key)
	}

	// Verify strict ascending order.
	for i := 1; i < len(pairKeys); i++ {
		if strings.Compare(pairKeys[i-1], pairKeys[i]) >= 0 {
			t.Errorf("mosaic_carriage pairs not in ascending byte order: %v", pairKeys)
			return
		}
	}
}
