package docformat_test

// Tests for block-list-of-mappings serialization round-trip via the Set path.
//
// Coverage:
//   - A field shaped like `triggers` (block list where each item is a KindMapping with
//     key-value pairs) has each item still classified as KindMapping after
//     parse -> Set -> Bytes -> parse. (Fails RED until serializeListEntry dispatches on
//     item.Kind.)
//   - A block-list-of-mappings field has non-empty Pairs on every item after
//     parse -> Set -> Bytes -> parse. (Fails RED: the current serializer emits "  - \n"
//     for each mapping item, losing all pair content.)
//   - The specific pair key and value from the first trigger item survive round-trip.
//     (Fails RED for the same reason.)
//   - A plain scalar block list (tools field) still has the correct item scalars after
//     Set + Bytes. (Regression guard: must remain GREEN before and after the fix.)
//   - A flow-style list (permissions field) still has the correct items after
//     Set + Bytes. (Regression guard: must remain GREEN before and after the fix.)
//
// All tests exercise Frontmatter.Set, which marks the field dirty and forces
// re-serialization through serializeListEntry on the next call to Document.Bytes.

import (
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// parseListOfMappingsFixture is a convenience wrapper around parsedFixture for the
// block-list-of-mappings fixture. parsedFixture is defined in frontmatter_test.go and
// is shared across all files in this test package.
func parseListOfMappingsFixture(t *testing.T) *docformat.Document {
	t.Helper()
	return parsedFixture(t, "block-list-of-mappings.md")
}

// --- Block list of mappings: mapping item kind preserved after Set ---

// TestSerializeListEntry_BlockListOfMappings_MappingItemsRemainKindMappingAfterSet asserts
// that every item in a block-list-of-mappings field is still classified as KindMapping
// after the field is Set (marking it dirty) and the document is re-serialized and
// re-parsed.
//
// This test is RED: the current serializeListEntry block path routes every list item
// through serializeScalarInline regardless of Kind. For a KindMapping item,
// serializeScalarInline returns "" (the Scalar field is empty on mappings), so the
// output is "  - \n" for each item. When re-parsed, those empty lines are KindScalar,
// not KindMapping.
func TestSerializeListEntry_BlockListOfMappings_MappingItemsRemainKindMappingAfterSet(t *testing.T) {
	// Arrange
	doc := parseListOfMappingsFixture(t)
	fm := doc.Frontmatter()

	v, ok := fm.Get("triggers")
	if !ok {
		t.Fatal("Get(\"triggers\") returned false; fixture must declare a triggers field")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("triggers: expected KindList, got %q", v.Kind)
	}
	if v.List != domain.ListBlock {
		t.Fatalf("triggers: expected ListBlock, got %q", v.List)
	}
	if len(v.Items) < 2 {
		t.Fatalf("expected at least 2 trigger items in fixture, got %d", len(v.Items))
	}

	// Act: Set the same value back to mark the field dirty, then re-serialize.
	if err := fm.Set("triggers", v); err != nil {
		t.Fatalf("Set(\"triggers\"): %v", err)
	}
	out := doc.Bytes()

	// Verify the output can be re-parsed.
	doc2, err := docformat.Parse(out)
	if err != nil {
		t.Fatalf("Parse re-serialized output: %v", err)
	}

	// Assert
	v2, ok := doc2.Frontmatter().Get("triggers")
	if !ok {
		t.Fatal("triggers field absent from re-serialized frontmatter")
	}
	if v2.Kind != domain.KindList {
		t.Fatalf("triggers in output: expected KindList, got %q", v2.Kind)
	}
	if len(v2.Items) != len(v.Items) {
		t.Errorf("triggers item count: want %d, got %d", len(v.Items), len(v2.Items))
	}
	for i, item := range v2.Items {
		if item.Kind != domain.KindMapping {
			t.Errorf("triggers[%d]: want KindMapping, got %q (content was lost during serialization)", i, item.Kind)
		}
	}
}

// TestSerializeListEntry_BlockListOfMappings_MappingPairsNotEmptyAfterSet asserts that
// every item in a block-list-of-mappings field has at least one key-value pair after
// parse -> Set -> Bytes -> parse.
//
// This test is RED: the current serializer produces "  - \n" for each mapping item,
// losing all pair content. When re-parsed, the empty-string items are KindScalar with
// no pairs.
func TestSerializeListEntry_BlockListOfMappings_MappingPairsNotEmptyAfterSet(t *testing.T) {
	// Arrange
	doc := parseListOfMappingsFixture(t)
	fm := doc.Frontmatter()

	v, ok := fm.Get("triggers")
	if !ok {
		t.Fatal("Get(\"triggers\") returned false")
	}

	// Sanity: the fixture itself must have pairs on each item before any round-trip.
	for i, item := range v.Items {
		if item.Kind != domain.KindMapping {
			t.Fatalf("fixture triggers[%d]: expected KindMapping before round-trip, got %q", i, item.Kind)
		}
		if len(item.Pairs) == 0 {
			t.Fatalf("fixture triggers[%d]: expected non-empty Pairs before round-trip", i)
		}
	}

	// Act
	if err := fm.Set("triggers", v); err != nil {
		t.Fatalf("Set(\"triggers\"): %v", err)
	}
	out := doc.Bytes()

	doc2, err := docformat.Parse(out)
	if err != nil {
		t.Fatalf("Parse re-serialized output: %v", err)
	}

	// Assert
	v2, ok := doc2.Frontmatter().Get("triggers")
	if !ok {
		t.Fatal("triggers field absent from re-serialized frontmatter")
	}
	for i, item := range v2.Items {
		if item.Kind != domain.KindMapping {
			t.Errorf("triggers[%d]: want KindMapping, got %q; pairs cannot be checked on a non-mapping item", i, item.Kind)
			continue
		}
		if len(item.Pairs) == 0 {
			t.Errorf("triggers[%d]: Pairs is empty after Set + Bytes; mapping content was lost during serialization", i)
		}
	}
}

// TestSerializeListEntry_BlockListOfMappings_SpecificPairKeyValuePreservedAfterSet asserts
// that the first trigger item's "trigger" key is preserved with value "STAGE_END" after
// the full round-trip.
//
// This test is RED for the same reason as the tests above: the serializer drops all
// mapping pair content for block-list items.
func TestSerializeListEntry_BlockListOfMappings_SpecificPairKeyValuePreservedAfterSet(t *testing.T) {
	// Arrange
	doc := parseListOfMappingsFixture(t)
	fm := doc.Frontmatter()

	v, ok := fm.Get("triggers")
	if !ok {
		t.Fatal("Get(\"triggers\") returned false")
	}

	// Act
	if err := fm.Set("triggers", v); err != nil {
		t.Fatalf("Set(\"triggers\"): %v", err)
	}
	out := doc.Bytes()

	doc2, err := docformat.Parse(out)
	if err != nil {
		t.Fatalf("Parse re-serialized output: %v", err)
	}

	// Assert: first item must have key "trigger" with scalar value "STAGE_END".
	v2, ok := doc2.Frontmatter().Get("triggers")
	if !ok {
		t.Fatal("triggers field absent from re-serialized frontmatter")
	}
	if len(v2.Items) == 0 {
		t.Fatal("triggers has no items in re-serialized output")
	}
	first := v2.Items[0]
	if first.Kind != domain.KindMapping {
		t.Fatalf("triggers[0]: want KindMapping, got %q", first.Kind)
	}
	if len(first.Pairs) == 0 {
		t.Fatal("triggers[0]: Pairs is empty after Set + Bytes; expected key \"trigger\" to be present")
	}
	if first.Pairs[0].Key != "trigger" {
		t.Errorf("triggers[0].Pairs[0].Key: want %q, got %q", "trigger", first.Pairs[0].Key)
	}
	if first.Pairs[0].Value.Scalar != "STAGE_END" {
		t.Errorf("triggers[0].Pairs[0].Value.Scalar: want %q, got %q", "STAGE_END", first.Pairs[0].Value.Scalar)
	}
}

// --- Scalar block list: regression guard ---

// TestSerializeListEntry_ScalarBlockList_ItemScalarsPreservedAfterSet is a regression
// guard asserting that a block list of KindScalar items still has the correct scalar
// values after parse -> Set -> Bytes -> parse.
//
// This test is expected to be GREEN before the fix (scalars already work) and must
// remain GREEN after the fix is applied.
func TestSerializeListEntry_ScalarBlockList_ItemScalarsPreservedAfterSet(t *testing.T) {
	// Arrange
	doc := parseListOfMappingsFixture(t)
	fm := doc.Frontmatter()

	v, ok := fm.Get("tools")
	if !ok {
		t.Fatal("Get(\"tools\") returned false; fixture must declare a tools field")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("tools: expected KindList, got %q", v.Kind)
	}
	if v.List != domain.ListBlock {
		t.Fatalf("tools: expected ListBlock, got %q", v.List)
	}
	wantScalars := []string{"alpha", "beta"}
	if len(v.Items) != len(wantScalars) {
		t.Fatalf("tools: expected %d items in fixture, got %d", len(wantScalars), len(v.Items))
	}

	// Act
	if err := fm.Set("tools", v); err != nil {
		t.Fatalf("Set(\"tools\"): %v", err)
	}
	out := doc.Bytes()

	doc2, err := docformat.Parse(out)
	if err != nil {
		t.Fatalf("Parse re-serialized output: %v", err)
	}

	// Assert
	v2, ok := doc2.Frontmatter().Get("tools")
	if !ok {
		t.Fatal("tools field absent from re-serialized frontmatter")
	}
	if v2.Kind != domain.KindList {
		t.Fatalf("tools in output: expected KindList, got %q", v2.Kind)
	}
	if len(v2.Items) != len(wantScalars) {
		t.Fatalf("tools item count: want %d, got %d", len(wantScalars), len(v2.Items))
	}
	for i, want := range wantScalars {
		if v2.Items[i].Kind != domain.KindScalar {
			t.Errorf("tools[%d]: want KindScalar, got %q", i, v2.Items[i].Kind)
			continue
		}
		if v2.Items[i].Scalar != want {
			t.Errorf("tools[%d]: want scalar %q, got %q", i, want, v2.Items[i].Scalar)
		}
	}
}

// --- Flow-style list: regression guard ---

// TestSerializeListEntry_FlowListScalar_ItemsPreservedAfterSet is a regression guard
// asserting that a flow-style list of KindScalar items still has the correct items after
// parse -> Set -> Bytes -> parse.
//
// This test is expected to be GREEN before the fix (the flow path is a separate code
// branch) and must remain GREEN after the fix is applied.
func TestSerializeListEntry_FlowListScalar_ItemsPreservedAfterSet(t *testing.T) {
	// Arrange
	doc := parseListOfMappingsFixture(t)
	fm := doc.Frontmatter()

	v, ok := fm.Get("permissions")
	if !ok {
		t.Fatal("Get(\"permissions\") returned false; fixture must declare a flow-style permissions field")
	}
	if v.Kind != domain.KindList {
		t.Fatalf("permissions: expected KindList, got %q", v.Kind)
	}
	if v.List != domain.ListFlow {
		t.Fatalf("permissions: expected ListFlow, got %q", v.List)
	}
	wantScalars := []string{"read", "write"}
	if len(v.Items) != len(wantScalars) {
		t.Fatalf("permissions: expected %d items in fixture, got %d", len(wantScalars), len(v.Items))
	}

	// Act
	if err := fm.Set("permissions", v); err != nil {
		t.Fatalf("Set(\"permissions\"): %v", err)
	}
	out := doc.Bytes()

	doc2, err := docformat.Parse(out)
	if err != nil {
		t.Fatalf("Parse re-serialized output: %v", err)
	}

	// Assert
	v2, ok := doc2.Frontmatter().Get("permissions")
	if !ok {
		t.Fatal("permissions field absent from re-serialized frontmatter")
	}
	if v2.Kind != domain.KindList {
		t.Fatalf("permissions in output: expected KindList, got %q", v2.Kind)
	}
	if len(v2.Items) != len(wantScalars) {
		t.Fatalf("permissions item count: want %d, got %d", len(wantScalars), len(v2.Items))
	}
	for i, want := range wantScalars {
		if v2.Items[i].Kind != domain.KindScalar {
			t.Errorf("permissions[%d]: want KindScalar, got %q", i, v2.Items[i].Kind)
			continue
		}
		if v2.Items[i].Scalar != want {
			t.Errorf("permissions[%d]: want scalar %q, got %q", i, want, v2.Items[i].Scalar)
		}
	}
}
