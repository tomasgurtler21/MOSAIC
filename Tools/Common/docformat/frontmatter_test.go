package docformat_test

// Tests for frontmatter parsing with style fidelity (T3.2).
//
// Coverage:
//   - Key order is preserved in source order from Keys() and Fields().
//   - Inline comments are captured in the FieldValue.Comment field.
//   - Unquoted, single-quoted, and double-quoted scalars are distinguished by QuoteStyle.
//   - Flow-style and block-style lists are distinguished by ListStyle.
//   - Nested mapping values are parsed and their pairs preserve source order.
//   - A placeholder value like {tool-permissions} round-trips as a plain scalar.
//   - Present() distinguishes files with and without a frontmatter block.
//   - Get returns false for absent keys.

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// testdataDir is relative to this package's working directory (Common/docformat).
const testdataDir = "../testdata/frontmatter"

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdataDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func parsedFixture(t *testing.T, name string) *docformat.Document {
	t.Helper()
	src := fixtureBytes(t, name)
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return doc
}

// --- Present() ---

func TestParse_FileWithFrontmatter_PresentIsTrue(t *testing.T) {
	doc := parsedFixture(t, "lf-minimal.md")
	if !doc.Frontmatter().Present() {
		t.Error("Present() should be true for a file with frontmatter delimiters")
	}
}

func TestParse_FileWithoutFrontmatter_PresentIsFalse(t *testing.T) {
	doc := parsedFixture(t, "no-frontmatter.md")
	if doc.Frontmatter().Present() {
		t.Error("Present() should be false for a file without frontmatter delimiters")
	}
}

func TestParse_EmptyFrontmatterBlock_PresentIsTrue(t *testing.T) {
	doc := parsedFixture(t, "empty-frontmatter.md")
	if !doc.Frontmatter().Present() {
		t.Error("Present() should be true for a file with an empty frontmatter block (--- + ---)")
	}
}

// --- Key order ---

func TestParse_KeyOrder_MatchesSourceOrder(t *testing.T) {
	doc := parsedFixture(t, "preserve-key-order.md")

	// The fixture declares keys in this exact order:
	wantOrder := []string{
		"id", "version", "name", "description",
		"model", "tools", "recommended_tier", "tier_rationale", "required_skills",
	}

	gotKeys := doc.Frontmatter().Keys()
	if len(gotKeys) != len(wantOrder) {
		t.Fatalf("expected %d keys, got %d: %v", len(wantOrder), len(gotKeys), gotKeys)
	}
	for i, want := range wantOrder {
		if gotKeys[i] != want {
			t.Errorf("key[%d]: want %q, got %q", i, want, gotKeys[i])
		}
	}
}

func TestParse_Fields_MatchesSourceOrder(t *testing.T) {
	doc := parsedFixture(t, "preserve-key-order.md")

	fields := doc.Frontmatter().Fields()
	keys := doc.Frontmatter().Keys()

	if len(fields) != len(keys) {
		t.Fatalf("Fields() length %d != Keys() length %d", len(fields), len(keys))
	}
	for i, f := range fields {
		if f.Key != keys[i] {
			t.Errorf("Fields()[%d].Key = %q, want %q", i, f.Key, keys[i])
		}
	}
}

// --- Get ---

func TestParse_Get_ExistingKey_ReturnsValueAndTrue(t *testing.T) {
	doc := parsedFixture(t, "lf-minimal.md")

	v, ok := doc.Frontmatter().Get("name")

	if !ok {
		t.Fatal("Get(\"name\") returned false for a key that exists")
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("expected KindScalar, got %q", v.Kind)
	}
	if v.Scalar != "test-agent" {
		t.Errorf("expected scalar value %q, got %q", "test-agent", v.Scalar)
	}
}

func TestParse_Get_AbsentKey_ReturnsFalse(t *testing.T) {
	doc := parsedFixture(t, "lf-minimal.md")

	_, ok := doc.Frontmatter().Get("nonexistent-key")

	if ok {
		t.Error("Get(\"nonexistent-key\") should return false for an absent key")
	}
}

// --- Inline comments ---

func TestParse_InlineComment_CapturedInCommentField(t *testing.T) {
	// The fixture has: model: claude-3-5-sonnet # preferred model for this agent
	// The Comment field must capture the exact comment text including the leading '#'.
	// Asserting only != "" would accept a parse that captures only '#' without the text.
	doc := parsedFixture(t, "preserve-inline-comment.md")

	v, ok := doc.Frontmatter().Get("model")
	if !ok {
		t.Fatal("Get(\"model\") returned false")
	}
	const wantComment = "# preferred model for this agent"
	if v.Comment != wantComment {
		t.Errorf("Comment: want %q, got %q", wantComment, v.Comment)
	}
}

func TestParse_KeyWithoutComment_HasEmptyCommentField(t *testing.T) {
	doc := parsedFixture(t, "preserve-inline-comment.md")

	v, ok := doc.Frontmatter().Get("version")
	if !ok {
		t.Fatal("Get(\"version\") returned false")
	}
	if v.Comment != "" {
		t.Errorf("expected empty Comment for a key without an inline comment, got %q", v.Comment)
	}
}

// --- Quote styles ---

func TestParse_DoubleQuotedScalar_HasQuoteDouble(t *testing.T) {
	doc := parsedFixture(t, "preserve-quoting.md")

	v, ok := doc.Frontmatter().Get("version")
	if !ok {
		t.Fatal("Get(\"version\") returned false")
	}
	if v.Quote != domain.QuoteDouble {
		t.Errorf("expected QuoteDouble for double-quoted scalar, got %q", v.Quote)
	}
	if v.Scalar != "3.0" {
		t.Errorf("expected scalar text %q (without quotes), got %q", "3.0", v.Scalar)
	}
}

func TestParse_SingleQuotedScalar_HasQuoteSingle(t *testing.T) {
	doc := parsedFixture(t, "preserve-quoting.md")

	v, ok := doc.Frontmatter().Get("hint")
	if !ok {
		t.Fatal("Get(\"hint\") returned false")
	}
	if v.Quote != domain.QuoteSingle {
		t.Errorf("expected QuoteSingle for single-quoted scalar, got %q", v.Quote)
	}
}

func TestParse_UnquotedScalar_HasQuotePlain(t *testing.T) {
	doc := parsedFixture(t, "preserve-quoting.md")

	v, ok := doc.Frontmatter().Get("plain")
	if !ok {
		t.Fatal("Get(\"plain\") returned false")
	}
	if v.Quote != domain.QuotePlain {
		t.Errorf("expected QuotePlain for unquoted scalar, got %q", v.Quote)
	}
}

// --- List styles ---

func TestParse_FlowStyleList_HasListFlow(t *testing.T) {
	doc := parsedFixture(t, "preserve-flow-list.md")

	v, ok := doc.Frontmatter().Get("tools")
	if !ok {
		t.Fatal("Get(\"tools\") returned false")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("expected KindList, got %q", v.Kind)
	}
	if v.List != domain.ListFlow {
		t.Errorf("expected ListFlow for a flow-style list, got %q", v.List)
	}
}

func TestParse_FlowStyleList_ItemsHaveCorrectScalars(t *testing.T) {
	doc := parsedFixture(t, "preserve-flow-list.md")

	v, _ := doc.Frontmatter().Get("tools")
	if len(v.Items) == 0 {
		t.Fatal("expected non-empty Items for a flow list")
	}
	if v.Items[0].Scalar != "file_read" {
		t.Errorf("expected first item %q, got %q", "file_read", v.Items[0].Scalar)
	}
}

func TestParse_BlockStyleList_HasListBlock(t *testing.T) {
	doc := parsedFixture(t, "preserve-block-list.md")

	v, ok := doc.Frontmatter().Get("referenced_agents")
	if !ok {
		t.Fatal("Get(\"referenced_agents\") returned false")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("expected KindList, got %q", v.Kind)
	}
	if v.List != domain.ListBlock {
		t.Errorf("expected ListBlock for a block-style list, got %q", v.List)
	}
}

func TestParse_BlockStyleList_ItemsHaveCorrectScalars(t *testing.T) {
	doc := parsedFixture(t, "preserve-block-list.md")

	v, _ := doc.Frontmatter().Get("referenced_agents")
	if len(v.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(v.Items))
	}
	if v.Items[0].Scalar != "planner-tdd-soft" {
		t.Errorf("expected first item %q, got %q", "planner-tdd-soft", v.Items[0].Scalar)
	}
}

// --- Nested mapping ---

func TestParse_NestedMapping_HasKindMapping(t *testing.T) {
	doc := parsedFixture(t, "nested-mapping.md")

	v, ok := doc.Frontmatter().Get("permission")
	if !ok {
		t.Fatal("Get(\"permission\") returned false")
	}
	if v.Kind != domain.KindMapping {
		t.Fatalf("expected KindMapping, got %q", v.Kind)
	}
}

func TestParse_NestedMapping_PairsPreserveSourceOrder(t *testing.T) {
	doc := parsedFixture(t, "nested-mapping.md")

	v, _ := doc.Frontmatter().Get("permission")
	if len(v.Pairs) == 0 {
		t.Fatal("expected non-empty Pairs for nested mapping")
	}
	// The fixture lists "read" first.
	if v.Pairs[0].Key != "read" {
		t.Errorf("expected first pair key %q, got %q", "read", v.Pairs[0].Key)
	}
}

// --- Placeholder value ---

func TestParse_PlaceholderValue_ParsedAsPlainScalar(t *testing.T) {
	// tools: {tool-permissions} must round-trip without YAML treating the braces as a mapping.
	doc := parsedFixture(t, "placeholder-value.md")

	v, ok := doc.Frontmatter().Get("tools")
	if !ok {
		t.Fatal("Get(\"tools\") returned false")
	}
	if v.Kind != domain.KindScalar {
		t.Fatalf("expected KindScalar for placeholder value, got %q", v.Kind)
	}
	if v.Scalar != "{tool-permissions}" {
		t.Errorf("expected scalar %q, got %q", "{tool-permissions}", v.Scalar)
	}
}

// --- Single-quoted list with slashes ---

func TestParse_SingleQuotedListItems_PreserveQuoteStyle(t *testing.T) {
	doc := parsedFixture(t, "single-quoted-list.md")

	v, ok := doc.Frontmatter().Get("tools")
	if !ok {
		t.Fatal("Get(\"tools\") returned false")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("expected KindList, got %q", v.Kind)
	}
	if len(v.Items) == 0 {
		t.Fatal("expected non-empty list items")
	}
	// Each item in this fixture is single-quoted.
	for i, item := range v.Items {
		if item.Quote != domain.QuoteSingle {
			t.Errorf("item[%d]: expected QuoteSingle, got %q (scalar=%q)", i, item.Quote, item.Scalar)
		}
	}
}

func TestParse_SingleQuotedListItem_ScalarContainsSlash(t *testing.T) {
	doc := parsedFixture(t, "single-quoted-list.md")

	v, _ := doc.Frontmatter().Get("tools")
	if len(v.Items) == 0 {
		t.Fatal("expected non-empty list items")
	}
	// The first item is 'read/readFile' — the slash must be preserved in the scalar value.
	if v.Items[0].Scalar != "read/readFile" {
		t.Errorf("expected scalar %q, got %q", "read/readFile", v.Items[0].Scalar)
	}
}

// --- Artifacts list with special chars ---

func TestParse_BlockListWithGlobChars_ScalarsPreserved(t *testing.T) {
	// Values like "Stage-*/Plan.md" and "Stage-{StageNumber}/Plan.md" must pass through
	// without YAML treating * or {} as special syntax.
	doc := parsedFixture(t, "quoted-with-special-chars.md")

	v, ok := doc.Frontmatter().Get("artifacts")
	if !ok {
		t.Fatal("Get(\"artifacts\") returned false")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("expected KindList, got %q", v.Kind)
	}

	wantItems := []string{
		"Plan.md",
		"Stage-*/Plan.md",
		"Stage-*/PlanProgress.md",
		"plan-review.md",
		"Stage-{StageNumber}/Plan.md",
		"Stage-{StageNumber}/PlanProgress.md",
		"TestResults.md",
	}
	if len(v.Items) != len(wantItems) {
		t.Fatalf("expected %d items, got %d", len(wantItems), len(v.Items))
	}
	for i, want := range wantItems {
		if v.Items[i].Scalar != want {
			t.Errorf("items[%d]: want %q, got %q", i, want, v.Items[i].Scalar)
		}
	}
}
