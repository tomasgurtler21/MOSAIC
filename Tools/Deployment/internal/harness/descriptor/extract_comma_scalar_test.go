package descriptor_test

// extract_comma_scalar_test.go covers Stage 6 ExtractToolEntries behaviour for comma-separated
// scalar tool values (T6.1). The claude-code built-in harness renders its tools field as a
// comma-separated scalar (e.g. "Read, Write, Edit, Bash") rather than a YAML list; this causes
// ExtractToolEntries to return nil today because the list-shape branch only handles KindList.
//
// These tests specify the widened contract:
//
// New behaviour (RED — fail before I6.1 is implemented):
//   - A KindScalar tools value in a list-shape harness is split on commas; the resulting
//     entries are indistinguishable from those produced by the equivalent KindList value.
//   - Surrounding whitespace is trimmed from each entry.
//   - By-convention tool names in a comma-scalar are excluded, exactly as for KindList.
//   - Duplicate entries in a comma-scalar are deduplicated first-seen.
//   - A comma-scalar value produces the same result as the equivalent KindList value.
//
// Edge cases (pass before and after):
//   - An empty scalar yields no entries.
//   - A whitespace-only (or comma-only) scalar yields no entries.
//
// Unchanged behaviour (regression guards — pass before and after):
//   - A permission-shape harness with a KindScalar value still returns nil; the fix is
//     restricted to the list-shape branch and must not change the permission-shape path.
//   - An empty ToolsKey still returns nil regardless of the value kind.
//   - A genuine KindList value continues to work exactly as before.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// buildScalarValue constructs a KindScalar FieldValue from a plain string, using QuotePlain
// style. It mirrors the docformat representation of a YAML scalar field like:
//
//	tools: Read, Write, Edit, Bash
func buildScalarValue(scalar string) domain.FieldValue {
	return domain.ScalarValue(scalar, domain.QuotePlain)
}

// =============================================================================
// T6.1 — New behaviour: comma-scalar yields entries (RED before I6.1)
// =============================================================================

// TestExtractToolEntries_ListShape_CommaScalar_YieldsEntries verifies that a comma-separated
// scalar tools value produces extracted entries in a list-shape harness. Before I6.1 the
// list-shape branch guards on KindList and returns nil for KindScalar — all three assertions
// below fail on nil.
func TestExtractToolEntries_ListShape_CommaScalar_YieldsEntries(t *testing.T) {
	// Arrange: list-extract descriptor has "read/readFile" and "write/editFile" as non-by-
	// convention Universe tools. The tools value is a comma-separated scalar.
	d := loadListExtractDescriptor(t)
	toolsValue := buildScalarValue("read/readFile,write/editFile")

	// Act
	got := descriptor.ExtractToolEntries(d, toolsValue)

	// Assert: both entries must be present — equivalent to feeding a KindList with the
	// same two items. A nil result (the current behaviour) fails here.
	if len(got) == 0 {
		t.Fatal("ExtractToolEntries with comma-scalar tools value: want entries extracted, got none; " +
			"a KindScalar value must be split on commas and processed like a KindList " +
			"(the list-shape branch must accept both KindList and KindScalar)")
	}
	for _, want := range []string{"read/readFile", "write/editFile"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ExtractToolEntries comma-scalar: entry %q absent from result %v", want, got)
		}
	}
}

// TestExtractToolEntries_ListShape_CommaScalar_WhitespaceTrimmed verifies that surrounding
// whitespace around comma-separated entries is trimmed. The YAML scalar
// "  read/readFile , write/editFile  " must yield the same entries as the unpadded version.
func TestExtractToolEntries_ListShape_CommaScalar_WhitespaceTrimmed(t *testing.T) {
	d := loadListExtractDescriptor(t)
	toolsValue := buildScalarValue("  read/readFile , write/editFile  ")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) == 0 {
		t.Fatal("ExtractToolEntries with whitespace-padded comma-scalar: want entries extracted, " +
			"got none; each comma-separated entry must be trimmed of surrounding whitespace")
	}
	for _, want := range []string{"read/readFile", "write/editFile"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ExtractToolEntries comma-scalar whitespace: entry %q absent from result %v "+
				"(check that strings.TrimSpace is applied per entry, not only globally)", want, got)
		}
	}
}

// TestExtractToolEntries_ListShape_CommaScalar_ByConventionExcluded verifies that by-convention
// tool names in a comma-scalar value are excluded, exactly as they are for KindList. The list-
// extract descriptor marks "search/listDirectory" as by_convention: true; it must not appear in
// the result even when present in the comma-scalar.
func TestExtractToolEntries_ListShape_CommaScalar_ByConventionExcluded(t *testing.T) {
	d := loadListExtractDescriptor(t) // search/listDirectory is by_convention: true
	toolsValue := buildScalarValue("read/readFile,search/listDirectory")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	// "read/readFile" is not by-convention: must be present.
	if len(got) == 0 {
		t.Fatalf("ExtractToolEntries comma-scalar by-convention exclusion: want 1 entry "+
			"(read/readFile), got none; by-convention exclusion applies per-entry during "+
			"comma-splitting, not only for KindList values")
	}
	if len(got) != 1 {
		t.Errorf("ExtractToolEntries comma-scalar by-convention exclusion: want 1 entry, got %d: %v; "+
			"\"search/listDirectory\" is by_convention: true and must be excluded", len(got), got)
	}
	if len(got) == 1 && got[0] != "read/readFile" {
		t.Errorf("ExtractToolEntries comma-scalar by-convention exclusion: want [read/readFile], got %v", got)
	}
	// Explicit check that by-convention entry did not slip through.
	for _, g := range got {
		if g == "search/listDirectory" {
			t.Errorf("ExtractToolEntries comma-scalar: by-convention entry %q must not appear in result %v",
				"search/listDirectory", got)
		}
	}
}

// TestExtractToolEntries_ListShape_CommaScalar_DeduplicatedFirstSeen verifies that duplicate
// entries in a comma-scalar value are collapsed to the first occurrence, exactly as for KindList.
// "read/readFile,write/editFile,read/readFile" must yield ["read/readFile", "write/editFile"]
// in first-seen order.
func TestExtractToolEntries_ListShape_CommaScalar_DeduplicatedFirstSeen(t *testing.T) {
	d := loadListExtractDescriptor(t)
	toolsValue := buildScalarValue("read/readFile,write/editFile,read/readFile")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 2 {
		t.Fatalf("ExtractToolEntries comma-scalar deduplication: want 2 entries (first-seen), "+
			"got %d: %v; duplicate comma-separated entries must be deduplicated using the "+
			"same first-seen logic applied for KindList values", len(got), got)
	}
	if got[0] != "read/readFile" {
		t.Errorf("ExtractToolEntries comma-scalar deduplication: got[0] want %q, got %q",
			"read/readFile", got[0])
	}
	if got[1] != "write/editFile" {
		t.Errorf("ExtractToolEntries comma-scalar deduplication: got[1] want %q, got %q",
			"write/editFile", got[1])
	}
}

// TestExtractToolEntries_ListShape_CommaScalar_EquivalentToKindList verifies the core contract:
// a comma-separated scalar produces results that are entry-for-entry identical to those produced
// by the equivalent KindList value. This is the definitive equivalence assertion.
func TestExtractToolEntries_ListShape_CommaScalar_EquivalentToKindList(t *testing.T) {
	d := loadListExtractDescriptor(t)
	// Build the same tools as both a comma-scalar and an equivalent KindList.
	scalarValue := buildScalarValue("read/readFile, write/editFile")
	listValue := buildListValue("read/readFile", "write/editFile")

	gotScalar := descriptor.ExtractToolEntries(d, scalarValue)
	gotList := descriptor.ExtractToolEntries(d, listValue)

	// Entry count must match.
	if len(gotScalar) != len(gotList) {
		t.Fatalf("ExtractToolEntries comma-scalar vs KindList equivalence: "+
			"comma-scalar returned %d entries %v, KindList returned %d entries %v; "+
			"they must be identical", len(gotScalar), gotScalar, len(gotList), gotList)
	}
	for i, want := range gotList {
		if gotScalar[i] != want {
			t.Errorf("ExtractToolEntries equivalence: result[%d] want %q (from KindList), got %q (from scalar)",
				i, want, gotScalar[i])
		}
	}
}

// =============================================================================
// T6.1 — Edge cases: empty / whitespace-only scalar (pass before and after)
// =============================================================================

// TestExtractToolEntries_ListShape_CommaScalar_EmptyScalarYieldsNoEntries verifies that an
// empty scalar tools value yields no entries. This is correct behaviour both before and after
// I6.1: before, the list-shape branch returns nil for any non-KindList; after, splitting an
// empty string produces no non-empty elements.
func TestExtractToolEntries_ListShape_CommaScalar_EmptyScalarYieldsNoEntries(t *testing.T) {
	d := loadListExtractDescriptor(t)
	toolsValue := buildScalarValue("")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 0 {
		t.Errorf("ExtractToolEntries with empty scalar: want no entries, got %d: %v", len(got), got)
	}
}

// TestExtractToolEntries_ListShape_CommaScalar_WhitespaceOnlyYieldsNoEntries verifies that a
// scalar consisting only of whitespace and commas yields no entries. After trimming each
// comma-separated element, all elements are empty and are discarded.
func TestExtractToolEntries_ListShape_CommaScalar_WhitespaceOnlyYieldsNoEntries(t *testing.T) {
	d := loadListExtractDescriptor(t)
	toolsValue := buildScalarValue("  ,  ,   ")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 0 {
		t.Errorf("ExtractToolEntries with whitespace-only scalar: want no entries, got %d: %v",
			len(got), got)
	}
}

// =============================================================================
// T6.1 — Unchanged behaviour (regression guards — pass before and after)
// =============================================================================

// TestExtractToolEntries_PermissionShape_ScalarValue_StillReturnsNil verifies that the
// permission-shape branch is unchanged by the comma-scalar widening. The permission-shape
// path requires KindMapping; a KindScalar value must still produce a nil result.
// This guards against I6.1 accidentally routing scalar values through the permission path.
func TestExtractToolEntries_PermissionShape_ScalarValue_StillReturnsNil(t *testing.T) {
	d := loadPermExtractDescriptor(t) // shape: permission
	toolsValue := buildScalarValue("read/readFile, write/createFile")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 0 {
		t.Errorf("ExtractToolEntries permission-shape with scalar value: want nil/empty result "+
			"(permission shape requires KindMapping), got %d entries: %v; "+
			"the comma-scalar widening must be restricted to the list-shape branch",
			len(got), got)
	}
}

// TestExtractToolEntries_EmptyToolsKey_ScalarValue_StillReturnsNil verifies that an empty
// ToolsKey still causes an immediate nil return regardless of the value kind. A descriptor
// with ToolsKey="" declares no tools field; no parsing of any value is appropriate.
func TestExtractToolEntries_EmptyToolsKey_ScalarValue_StillReturnsNil(t *testing.T) {
	d, err := descriptor.Parse([]byte(noToolsKeyDescriptorYAML), "inline:no-tools-key-harness")
	if err != nil {
		t.Fatalf("Parse no-tools-key descriptor: %v", err)
	}
	toolsValue := buildScalarValue("read/readFile")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 0 {
		t.Errorf("ExtractToolEntries with empty ToolsKey and scalar value: want nil/empty, got %d: %v; "+
			"an empty ToolsKey must still cause early return regardless of value kind",
			len(got), got)
	}
}

// TestExtractToolEntries_ListShape_KindList_UnchangedByScalarSupport verifies that the
// existing KindList behaviour is preserved after the comma-scalar widening. A KindList
// value must produce the same result it always has; the new code path for KindScalar must
// not interfere with it.
func TestExtractToolEntries_ListShape_KindList_UnchangedByScalarSupport(t *testing.T) {
	d := loadListExtractDescriptor(t)
	// Provide a genuine KindList value: "read/readFile" and "write/editFile" (non-by-convention).
	toolsValue := buildListValue("read/readFile", "write/editFile")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 2 {
		t.Fatalf("ExtractToolEntries KindList (regression guard): want 2 entries, got %d: %v; "+
			"I6.1 must not change the existing KindList extraction path", len(got), got)
	}
	if got[0] != "read/readFile" {
		t.Errorf("ExtractToolEntries KindList: got[0] want %q, got %q", "read/readFile", got[0])
	}
	if got[1] != "write/editFile" {
		t.Errorf("ExtractToolEntries KindList: got[1] want %q, got %q", "write/editFile", got[1])
	}
}
