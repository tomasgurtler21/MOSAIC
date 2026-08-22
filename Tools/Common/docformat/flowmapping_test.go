package docformat_test

// Tests for flow-mapping tolerance in docformat frontmatter parsing.
//
// Coverage:
//   - Single-line flow mapping at top level parses as an opaque scalar (KindScalar).
//   - Single-line flow mapping round-trips byte-exactly via Parse then Bytes.
//   - Multi-line flow mapping inside a block mapping parses as an opaque scalar.
//   - Multi-line flow mapping round-trips byte-exactly via Parse then Bytes.
//   - A permission: block containing a bash: sub-key with a multi-line flow mapping
//     parses successfully, preserves sibling keys, and round-trips the full document.
//   - A continuation line containing a tab character does not trigger the
//     tab-indentation error (tab check applies only to key:value lines, not to
//     opaque content lines consumed inside a multi-line flow mapping span).
//   - Existing scalar, list, and block-mapping parsing is unaffected (regression guard).
//   - The {tool-permissions} placeholder continues to parse as a plain scalar.
//
// Fixture files live in Tools/Common/testdata/frontmatter/.
// New fixtures:
//   flow-mapping-single-line.md       -- top-level key with single-line {…} value
//   flow-mapping-permission-multi.md  -- permission: block with multi-line bash: value
//   flow-mapping-tab-in-content.md    -- multi-line flow mapping whose continuation
//                                        line starts with a tab character

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// --- Single-line flow mapping at top level ---
//
// The value {"*": "deny", "dotnet *": "allow"} on a single line must be stored
// as an opaque scalar — not interpreted as a nested YAML mapping.

func TestParse_TopLevel_SingleLineFlowMapping_ParsesSuccessfully(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-single-line.md")

	doc, err := docformat.Parse(src)

	if err != nil {
		t.Fatalf("Parse returned unexpected error for single-line flow mapping: %v", err)
	}
	if doc == nil {
		t.Fatal("Parse returned nil Document for a valid single-line flow mapping")
	}
}

func TestParse_TopLevel_SingleLineFlowMapping_ValueIsScalar(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-single-line.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	v, ok := doc.Frontmatter().Get("bash")
	if !ok {
		t.Fatal("Get(\"bash\") returned false; key absent from parsed frontmatter")
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("expected KindScalar for single-line flow-mapping value, got %q", v.Kind)
	}
}

func TestParse_TopLevel_SingleLineFlowMapping_ScalarPreservesBraces(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-single-line.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	v, ok := doc.Frontmatter().Get("bash")
	if !ok {
		t.Fatal("Get(\"bash\") returned false")
	}
	// Braces must be preserved verbatim in the scalar text.
	if len(v.Scalar) == 0 || v.Scalar[0] != '{' {
		t.Errorf("scalar does not start with '{': %q", v.Scalar)
	}
	if len(v.Scalar) == 0 || v.Scalar[len(v.Scalar)-1] != '}' {
		t.Errorf("scalar does not end with '}': %q", v.Scalar)
	}
}

func TestRoundTrip_SingleLineFlowMapping_ByteExact(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-single-line.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

// --- Multi-line flow mapping inside a block mapping ---
//
// A bash: key inside a permission: block whose value starts with { on the key line
// and whose closing } appears on a subsequent line must parse as a single opaque
// scalar, not trigger a parse error and not be split across two key:value pairs.

func TestParse_BlockMapping_MultiLineFlowMapping_ParsesSuccessfully(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-permission-multi.md")

	doc, err := docformat.Parse(src)

	if err != nil {
		t.Fatalf("Parse returned unexpected error for multi-line flow mapping: %v", err)
	}
	if doc == nil {
		t.Fatal("Parse returned nil Document")
	}
}

func TestParse_BlockMapping_MultiLineFlowMapping_PermissionBlockPresent(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-permission-multi.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	permVal, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("Get(\"permission\") returned false; permission block not found")
	}
	if permVal.Kind != domain.KindMapping {
		t.Errorf("expected permission value to be KindMapping, got %q", permVal.Kind)
	}
}

func TestParse_BlockMapping_MultiLineFlowMapping_BashValueIsScalar(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-permission-multi.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	permVal, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("Get(\"permission\") returned false")
	}

	var bashVal domain.FieldValue
	var found bool
	for _, pair := range permVal.Pairs {
		if pair.Key == "bash" {
			bashVal = pair.Value
			found = true
			break
		}
	}
	if !found {
		t.Fatal("bash key not found inside permission block")
	}
	if bashVal.Kind != domain.KindScalar {
		t.Errorf("expected KindScalar for multi-line flow-mapping value, got %q", bashVal.Kind)
	}
}

func TestParse_BlockMapping_MultiLineFlowMapping_ScalarPreservesBraces(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-permission-multi.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	permVal, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("Get(\"permission\") returned false")
	}

	var bashVal domain.FieldValue
	for _, pair := range permVal.Pairs {
		if pair.Key == "bash" {
			bashVal = pair.Value
		}
	}

	// The scalar must start with '{' and end with '}' — the full multi-line span
	// is stored as one opaque string.
	if len(bashVal.Scalar) == 0 || bashVal.Scalar[0] != '{' {
		t.Errorf("multi-line flow-mapping scalar does not start with '{': %q", bashVal.Scalar)
	}
	if len(bashVal.Scalar) == 0 || bashVal.Scalar[len(bashVal.Scalar)-1] != '}' {
		t.Errorf("multi-line flow-mapping scalar does not end with '}': %q", bashVal.Scalar)
	}
}

func TestParse_BlockMapping_MultiLineFlowMapping_SiblingScalarsCorrect(t *testing.T) {
	// Sibling keys in the permission block (plain scalar values) must parse correctly
	// after the multi-line brace-consuming path exits.
	src := fixtureBytes(t, "flow-mapping-permission-multi.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	permVal, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("Get(\"permission\") returned false")
	}

	var foundRead, foundWrite bool
	for _, pair := range permVal.Pairs {
		switch pair.Key {
		case "read":
			foundRead = true
			if pair.Value.Kind != domain.KindScalar || pair.Value.Scalar != "allow" {
				t.Errorf("read: expected scalar %q, got kind=%q scalar=%q",
					"allow", pair.Value.Kind, pair.Value.Scalar)
			}
		case "write":
			foundWrite = true
			if pair.Value.Kind != domain.KindScalar || pair.Value.Scalar != "allow" {
				t.Errorf("write: expected scalar %q, got kind=%q scalar=%q",
					"allow", pair.Value.Kind, pair.Value.Scalar)
			}
		}
	}
	if !foundRead {
		t.Error("read key not found in permission block after multi-line flow mapping consumed bash")
	}
	if !foundWrite {
		t.Error("write key not found in permission block after multi-line flow mapping consumed bash")
	}
}

func TestRoundTrip_MultiLineFlowMapping_ByteExact(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-permission-multi.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

// --- Tab character in multi-line flow mapping continuation line ---
//
// A continuation line that is part of a multi-line flow mapping value may begin
// with a tab character as opaque content. The tab-indentation error that guards
// key:value lines must NOT fire for continuation lines consumed inside a brace span.

func TestParse_MultiLineFlowMapping_TabInContinuationLine_DoesNotError(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-tab-in-content.md")

	doc, err := docformat.Parse(src)

	if err != nil {
		t.Fatalf("Parse returned error for multi-line flow mapping with tab in continuation line: %v", err)
	}
	if doc == nil {
		t.Fatal("Parse returned nil Document")
	}
}

func TestParse_MultiLineFlowMapping_TabInContinuationLine_BashValueIsScalar(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-tab-in-content.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	permVal, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("Get(\"permission\") returned false")
	}

	var bashVal domain.FieldValue
	var found bool
	for _, pair := range permVal.Pairs {
		if pair.Key == "bash" {
			bashVal = pair.Value
			found = true
			break
		}
	}
	if !found {
		t.Fatal("bash key not found inside permission block")
	}
	if bashVal.Kind != domain.KindScalar {
		t.Errorf("expected KindScalar for bash flow-mapping value with tab, got %q", bashVal.Kind)
	}
}

func TestRoundTrip_MultiLineFlowMapping_TabInContinuationLine_ByteExact(t *testing.T) {
	src := fixtureBytes(t, "flow-mapping-tab-in-content.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

// --- Regression: existing scalar/list/block-mapping parsing is unaffected ---
//
// These tests guard against the flow-mapping changes inadvertently breaking the
// existing parser paths for plain scalars and block mappings with scalar values.

func TestParse_Regression_PlainScalar_Unaffected(t *testing.T) {
	doc := parsedFixture(t, "lf-minimal.md")

	v, ok := doc.Frontmatter().Get("name")
	if !ok {
		t.Fatal("Get(\"name\") returned false")
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("expected KindScalar for plain scalar, got %q", v.Kind)
	}
	if v.Scalar != "test-agent" {
		t.Errorf("expected scalar %q, got %q", "test-agent", v.Scalar)
	}
}

func TestParse_Regression_BlockMapping_PlainScalarValues_Unaffected(t *testing.T) {
	// A permission block whose values are all plain scalars (no flow mappings)
	// must continue to parse correctly with all keys present and correct.
	doc := parsedFixture(t, "nested-mapping.md")

	permVal, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("Get(\"permission\") returned false for nested-mapping.md")
	}
	if permVal.Kind != domain.KindMapping {
		t.Errorf("expected KindMapping for permission block, got %q", permVal.Kind)
	}
	if len(permVal.Pairs) == 0 {
		t.Error("permission block has no pairs")
	}

	// Spot-check: bash key must have the scalar value "allow".
	var foundBash bool
	for _, pair := range permVal.Pairs {
		if pair.Key == "bash" {
			foundBash = true
			if pair.Value.Kind != domain.KindScalar || pair.Value.Scalar != "allow" {
				t.Errorf("bash: expected scalar %q, got kind=%q scalar=%q",
					"allow", pair.Value.Kind, pair.Value.Scalar)
			}
			break
		}
	}
	if !foundBash {
		t.Error("bash key not found in permission block of nested-mapping.md")
	}
}

func TestRoundTrip_Regression_BlockMapping_ByteExact(t *testing.T) {
	src := fixtureBytes(t, "nested-mapping.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned error for nested-mapping.md: %v", err)
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

// --- Regression: {tool-permissions} placeholder parses as plain scalar ---
//
// The {tool-permissions} value must continue to parse as an opaque scalar after
// the '{' detection change in parseGroupValue. Its closing '}' is on the same
// line as the opening '{', so the multi-line brace-consuming path must not trigger.

func TestParse_Regression_ToolPermissionsPlaceholder_ParsesAsScalar(t *testing.T) {
	doc := parsedFixture(t, "placeholder-value.md")

	v, ok := doc.Frontmatter().Get("tools")
	if !ok {
		t.Fatal("Get(\"tools\") returned false for placeholder-value.md")
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("expected KindScalar for {tool-permissions} placeholder, got %q", v.Kind)
	}
	const wantScalar = "{tool-permissions}"
	if v.Scalar != wantScalar {
		t.Errorf("expected scalar %q, got %q", wantScalar, v.Scalar)
	}
}

func TestRoundTrip_Regression_ToolPermissionsPlaceholder_ByteExact(t *testing.T) {
	src := fixtureBytes(t, "placeholder-value.md")

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse returned error for placeholder-value.md: %v", err)
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}
