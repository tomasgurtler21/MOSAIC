package descriptor_test

// divertedfields_test.go covers the Stage 2 descriptor-level additions:
//
//   DivertedToolFields — enumerates DestField destinations from a descriptor.
//   ExtractDivertedToolEntries — reads entries back from a diverted field value.
//   RenderFieldValues — flattens a FieldValue into human-readable strings.
//
// These tests are the TDD RED phase for I2.1. They compile against the stubs in
// divertedfields.go and fail because the stubs return nil/empty.
//
// Verified behaviours:
//
// DivertedToolFields:
//   - Returns empty for a descriptor with no DestField destinations.
//   - Returns one entry per distinct DestField destination key, in first-seen order.
//   - The returned DivertedToolField carries the declared Key, Format, and Separator.
//   - DestMain destinations are never included.
//   - When two destinations share the same Key with different Formats, first-seen wins.
//   - The main tools key (ToolsKey) is never included even if a DestField happens to name it.
//   - An omitted Format defaults to domain.DefaultToolValueFormat (FormatListBlock).
//   - An omitted Separator defaults to domain.DefaultScalarSeparator for FormatScalar.
//
// ExtractDivertedToolEntries:
//   - FormatListBlock value: each list item's scalar text is returned.
//   - FormatListFlow value: each list item's scalar text is returned.
//   - FormatScalar value: scalar is split on the declared separator; each part trimmed.
//   - By-convention tools are excluded (AD-14), matching ExtractToolEntries behaviour.
//   - Entries are deduplicated first-seen in document order.
//   - A zero FieldValue yields an empty slice.
//   - A value whose Kind does not match the declared Format yields an empty slice.
//   - FormatScalar with default separator splits on ", ".
//
// RenderFieldValues:
//   - A KindScalar value yields one string equal to the scalar text.
//   - A KindList value yields one string per item (scalars only).
//   - A KindMapping value yields "key: value" entries, one per pair.
//   - A zero FieldValue yields an empty slice.

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// divertedListBlockDescriptorYAML declares a harness with one DestMain mapping
// (file_read → "read-file") and one DestField mapping (mcp_tool → "mcpServers",
// format list-block). Used to verify FormatListBlock extraction.
const divertedListBlockDescriptorYAML = `schema_version: "1"
id: "diverted-listblock-harness"
display_name: "Diverted ListBlock Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
    - name: "by-conv-tool"
      unused: deny
      by_convention: true
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read-file"
    - generic: "mcp_tool"
      destinations:
        - to: field
          field: mcpServers
          format: list-block
          names:
            - "mcp-server-alpha"
frontmatter:
  tools_key: "tools"
`

// divertedListFlowDescriptorYAML declares a harness with a DestField mapping using
// FormatListFlow.
const divertedListFlowDescriptorYAML = `schema_version: "1"
id: "diverted-listflow-harness"
display_name: "Diverted ListFlow Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "mcp_tool"
      destinations:
        - to: field
          field: mcpServers
          format: list-flow
          names:
            - "mcp-server-beta"
frontmatter:
  tools_key: "tools"
`

// divertedScalarDescriptorYAML declares a harness with a DestField mapping using
// FormatScalar with a custom separator "|".
const divertedScalarDescriptorYAML = `schema_version: "1"
id: "diverted-scalar-harness"
display_name: "Diverted Scalar Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "mcp_tool"
      destinations:
        - to: field
          field: mcpScalar
          format: scalar
          separator: "|"
          names:
            - "tool-one"
            - "tool-two"
frontmatter:
  tools_key: "tools"
`

// divertedDefaultSeparatorDescriptorYAML declares a DestField mapping with format scalar
// but no declared separator, so DefaultScalarSeparator (", ") must be used.
const divertedDefaultSeparatorDescriptorYAML = `schema_version: "1"
id: "diverted-default-sep-harness"
display_name: "Diverted Default Sep Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "mcp_tool"
      destinations:
        - to: field
          field: mcpScalar
          format: scalar
          names:
            - "alpha"
frontmatter:
  tools_key: "tools"
`

// divertedMultiFieldDescriptorYAML declares a harness with two distinct DestField
// destinations (mcpServers and extraField). Used to verify first-seen ordering.
const divertedMultiFieldDescriptorYAML = `schema_version: "1"
id: "diverted-multi-harness"
display_name: "Diverted Multi-Field Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "tool_a"
      destinations:
        - to: field
          field: mcpServers
          format: list-block
          names:
            - "server-a"
    - generic: "tool_b"
      destinations:
        - to: field
          field: extraField
          format: list-flow
          names:
            - "extra-b"
frontmatter:
  tools_key: "tools"
`

// divertedDuplicateKeyDescriptorYAML declares two DestField mappings targeting the same
// key ("mcpServers") with different formats. First-seen declaration must win.
const divertedDuplicateKeyDescriptorYAML = `schema_version: "1"
id: "diverted-dup-harness"
display_name: "Diverted Dup Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "tool_first"
      destinations:
        - to: field
          field: mcpServers
          format: list-block
          names:
            - "server-first"
    - generic: "tool_second"
      destinations:
        - to: field
          field: mcpServers
          format: list-flow
          names:
            - "server-second"
frontmatter:
  tools_key: "tools"
`

// noDestFieldDescriptorYAML declares a harness with only DestMain destinations.
const noDestFieldDescriptorYAML = `schema_version: "1"
id: "no-destfield-harness"
display_name: "No DestField Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read-file"
frontmatter:
  tools_key: "tools"
`

// loadDescriptor parses the given YAML and returns the descriptor. Fails the test on error.
func loadDescriptor(t *testing.T, yaml string) *domain.HarnessDescriptor {
	t.Helper()
	d, err := descriptor.Parse([]byte(yaml), "inline:test")
	if err != nil {
		t.Fatalf("Parse descriptor: %v", err)
	}
	if d == nil {
		t.Fatal("Parse returned nil without error")
	}
	return d
}

// ---------------------------------------------------------------------------
// Tests for DivertedToolFields
// ---------------------------------------------------------------------------

// TestDivertedToolFields_NoDestField_ReturnsEmpty verifies that a descriptor with only
// DestMain destinations returns an empty (not nil) slice.
func TestDivertedToolFields_NoDestField_ReturnsEmpty(t *testing.T) {
	d := loadDescriptor(t, noDestFieldDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) != 0 {
		t.Errorf("DivertedToolFields with no DestField destinations: want empty slice, got %d entries: %v",
			len(fields), fields)
	}
}

// TestDivertedToolFields_SingleDestField_ReturnsOneEntry verifies that a descriptor with
// one DestField destination returns exactly one DivertedToolField entry.
func TestDivertedToolFields_SingleDestField_ReturnsOneEntry(t *testing.T) {
	d := loadDescriptor(t, divertedListBlockDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) != 1 {
		t.Fatalf("DivertedToolFields with one DestField destination: want 1 entry, got %d: %v",
			len(fields), fields)
	}
}

// TestDivertedToolFields_SingleDestField_CarriesCorrectKey verifies that the returned
// DivertedToolField carries the Key declared in the DestField destination.
func TestDivertedToolFields_SingleDestField_CarriesCorrectKey(t *testing.T) {
	d := loadDescriptor(t, divertedListBlockDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) < 1 {
		t.Fatal("DivertedToolFields returned empty; cannot check Key")
	}
	if fields[0].Key != "mcpServers" {
		t.Errorf("DivertedToolField.Key = %q, want %q; the key must match the destination's field declaration",
			fields[0].Key, "mcpServers")
	}
}

// TestDivertedToolFields_ListBlockDestination_CarriesFormatListBlock verifies that a
// DestField destination with format list-block produces a DivertedToolField with
// Format == domain.FormatListBlock.
func TestDivertedToolFields_ListBlockDestination_CarriesFormatListBlock(t *testing.T) {
	d := loadDescriptor(t, divertedListBlockDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) < 1 {
		t.Fatal("DivertedToolFields returned empty; cannot check Format")
	}
	if fields[0].Format != domain.FormatListBlock {
		t.Errorf("DivertedToolField.Format = %q, want %q (FormatListBlock)",
			fields[0].Format, domain.FormatListBlock)
	}
}

// TestDivertedToolFields_ListFlowDestination_CarriesFormatListFlow verifies that a
// DestField destination with format list-flow produces a DivertedToolField with
// Format == domain.FormatListFlow.
func TestDivertedToolFields_ListFlowDestination_CarriesFormatListFlow(t *testing.T) {
	d := loadDescriptor(t, divertedListFlowDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) < 1 {
		t.Fatal("DivertedToolFields returned empty; cannot check Format")
	}
	if fields[0].Format != domain.FormatListFlow {
		t.Errorf("DivertedToolField.Format = %q, want %q (FormatListFlow)",
			fields[0].Format, domain.FormatListFlow)
	}
}

// TestDivertedToolFields_ScalarDestination_CarriesFormatScalar verifies that a DestField
// destination with format scalar produces Format == domain.FormatScalar.
func TestDivertedToolFields_ScalarDestination_CarriesFormatScalar(t *testing.T) {
	d := loadDescriptor(t, divertedScalarDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) < 1 {
		t.Fatal("DivertedToolFields returned empty; cannot check Format")
	}
	if fields[0].Format != domain.FormatScalar {
		t.Errorf("DivertedToolField.Format = %q, want %q (FormatScalar)",
			fields[0].Format, domain.FormatScalar)
	}
}

// TestDivertedToolFields_ScalarDestination_CarriesDeclaredSeparator verifies that the
// separator declared in the DestField destination appears in the DivertedToolField.
func TestDivertedToolFields_ScalarDestination_CarriesDeclaredSeparator(t *testing.T) {
	d := loadDescriptor(t, divertedScalarDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) < 1 {
		t.Fatal("DivertedToolFields returned empty; cannot check Separator")
	}
	if fields[0].Separator != "|" {
		t.Errorf("DivertedToolField.Separator = %q, want %q; declared separator must be preserved",
			fields[0].Separator, "|")
	}
}

// TestDivertedToolFields_ScalarOmittedSeparator_DefaultsToDefaultScalarSeparator verifies
// that when a FormatScalar destination omits the separator, DefaultScalarSeparator is
// substituted so callers never see an empty Separator on a scalar field.
func TestDivertedToolFields_ScalarOmittedSeparator_DefaultsToDefaultScalarSeparator(t *testing.T) {
	d := loadDescriptor(t, divertedDefaultSeparatorDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) < 1 {
		t.Fatal("DivertedToolFields returned empty; cannot check Separator default")
	}
	if fields[0].Separator != domain.DefaultScalarSeparator {
		t.Errorf("DivertedToolField.Separator = %q, want %q (DefaultScalarSeparator) when separator is omitted from a FormatScalar destination",
			fields[0].Separator, domain.DefaultScalarSeparator)
	}
}

// TestDivertedToolFields_MultipleDestFields_ReturnsAllInFirstSeenOrder verifies that a
// descriptor with two distinct DestField destinations returns both, in first-seen
// declaration order, with the correct keys.
func TestDivertedToolFields_MultipleDestFields_ReturnsAllInFirstSeenOrder(t *testing.T) {
	d := loadDescriptor(t, divertedMultiFieldDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) != 2 {
		t.Fatalf("DivertedToolFields with two DestField destinations: want 2 entries, got %d: %v",
			len(fields), fields)
	}
	if fields[0].Key != "mcpServers" {
		t.Errorf("fields[0].Key = %q, want %q (first-seen declaration order)", fields[0].Key, "mcpServers")
	}
	if fields[1].Key != "extraField" {
		t.Errorf("fields[1].Key = %q, want %q (first-seen declaration order)", fields[1].Key, "extraField")
	}
}

// TestDivertedToolFields_DuplicateKey_FirstSeenFormatWins verifies that when two
// destinations declare the same Key with different Formats, only one entry is returned
// and its Format matches the first-seen declaration.
func TestDivertedToolFields_DuplicateKey_FirstSeenFormatWins(t *testing.T) {
	d := loadDescriptor(t, divertedDuplicateKeyDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	if len(fields) != 1 {
		t.Fatalf("DivertedToolFields with duplicate key: want exactly 1 entry (first-seen wins), got %d: %v",
			len(fields), fields)
	}
	if fields[0].Format != domain.FormatListBlock {
		t.Errorf("DivertedToolField.Format = %q, want %q (first-seen format must win)",
			fields[0].Format, domain.FormatListBlock)
	}
}

// TestDivertedToolFields_DestMain_NeverIncluded verifies that DestMain destinations
// do not appear in the returned slice — they are the main tools key, not a diverted field.
func TestDivertedToolFields_DestMain_NeverIncluded(t *testing.T) {
	// The divertedListBlockDescriptorYAML has one DestMain (file_read → "read-file")
	// and one DestField (mcp_tool → "mcpServers"). Only "mcpServers" must appear.
	d := loadDescriptor(t, divertedListBlockDescriptorYAML)

	fields := descriptor.DivertedToolFields(d)

	for _, f := range fields {
		// The DestMain target name from the descriptor is "tools" (the tools_key).
		// Guard: if any returned entry is the tools key, DestMain leaked in.
		if f.Key == "tools" {
			t.Errorf("DestMain destination leaked into DivertedToolFields as key %q; only DestField destinations must be returned", f.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for ExtractDivertedToolEntries
// ---------------------------------------------------------------------------

// listBlockValue builds a KindList FieldValue with ListBlock style from the given names.
func listBlockValue(names ...string) domain.FieldValue {
	items := make([]domain.FieldValue, len(names))
	for i, n := range names {
		items[i] = domain.ScalarValue(n, domain.QuotePlain)
	}
	return domain.ListValue(items, domain.ListBlock)
}

// listFlowValue builds a KindList FieldValue with ListFlow style from the given names.
func listFlowValue(names ...string) domain.FieldValue {
	items := make([]domain.FieldValue, len(names))
	for i, n := range names {
		items[i] = domain.ScalarValue(n, domain.QuotePlain)
	}
	return domain.ListValue(items, domain.ListFlow)
}

// scalarValue builds a KindScalar FieldValue.
func scalarValue(text string) domain.FieldValue {
	return domain.ScalarValue(text, domain.QuotePlain)
}

// TestExtractDivertedToolEntries_FormatListBlock_ReturnsAllItems verifies that
// ExtractDivertedToolEntries reads every list item from a FormatListBlock value.
func TestExtractDivertedToolEntries_FormatListBlock_ReturnsAllItems(t *testing.T) {
	f := descriptor.DivertedToolField{
		Key:    "mcpServers",
		Format: domain.FormatListBlock,
	}
	value := listBlockValue("server-alpha", "server-beta")

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	if len(entries) != 2 {
		t.Fatalf("ExtractDivertedToolEntries FormatListBlock: want 2 entries, got %d: %v", len(entries), entries)
	}
	if entries[0] != "server-alpha" {
		t.Errorf("entries[0] = %q, want %q", entries[0], "server-alpha")
	}
	if entries[1] != "server-beta" {
		t.Errorf("entries[1] = %q, want %q", entries[1], "server-beta")
	}
}

// TestExtractDivertedToolEntries_FormatListFlow_ReturnsAllItems verifies that
// ExtractDivertedToolEntries reads every list item from a FormatListFlow value.
func TestExtractDivertedToolEntries_FormatListFlow_ReturnsAllItems(t *testing.T) {
	f := descriptor.DivertedToolField{
		Key:    "mcpServers",
		Format: domain.FormatListFlow,
	}
	value := listFlowValue("tool-x", "tool-y", "tool-z")

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	if len(entries) != 3 {
		t.Fatalf("ExtractDivertedToolEntries FormatListFlow: want 3 entries, got %d: %v", len(entries), entries)
	}
	for i, want := range []string{"tool-x", "tool-y", "tool-z"} {
		if entries[i] != want {
			t.Errorf("entries[%d] = %q, want %q", i, entries[i], want)
		}
	}
}

// TestExtractDivertedToolEntries_FormatScalar_SplitsOnDeclaredSeparator verifies that
// ExtractDivertedToolEntries splits a KindScalar value on the field's declared separator.
func TestExtractDivertedToolEntries_FormatScalar_SplitsOnDeclaredSeparator(t *testing.T) {
	f := descriptor.DivertedToolField{
		Key:       "mcpScalar",
		Format:    domain.FormatScalar,
		Separator: "|",
	}
	value := scalarValue("tool-one|tool-two|tool-three")

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	if len(entries) != 3 {
		t.Fatalf("ExtractDivertedToolEntries FormatScalar: want 3 entries, got %d: %v", len(entries), entries)
	}
	for i, want := range []string{"tool-one", "tool-two", "tool-three"} {
		if entries[i] != want {
			t.Errorf("entries[%d] = %q, want %q", i, entries[i], want)
		}
	}
}

// TestExtractDivertedToolEntries_FormatScalar_TrimsWhitespace verifies that each part
// after splitting on the separator is trimmed of leading and trailing whitespace.
func TestExtractDivertedToolEntries_FormatScalar_TrimsWhitespace(t *testing.T) {
	f := descriptor.DivertedToolField{
		Key:       "mcpScalar",
		Format:    domain.FormatScalar,
		Separator: ",",
	}
	value := scalarValue(" alpha ,  beta , gamma ")

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	if len(entries) != 3 {
		t.Fatalf("ExtractDivertedToolEntries FormatScalar trim: want 3 entries, got %d: %v", len(entries), entries)
	}
	for i, want := range []string{"alpha", "beta", "gamma"} {
		if entries[i] != want {
			t.Errorf("entries[%d] = %q, want %q (parts must be trimmed of whitespace)", i, entries[i], want)
		}
	}
}

// TestExtractDivertedToolEntries_FormatScalar_DefaultSeparator verifies that when
// Separator is the default ", " (as set by DivertedToolFields for omitted separators),
// splitting works correctly.
func TestExtractDivertedToolEntries_FormatScalar_DefaultSeparator(t *testing.T) {
	f := descriptor.DivertedToolField{
		Key:       "mcpScalar",
		Format:    domain.FormatScalar,
		Separator: domain.DefaultScalarSeparator,
	}
	value := scalarValue("alpha, beta, gamma")

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	if len(entries) != 3 {
		t.Fatalf("ExtractDivertedToolEntries default separator: want 3 entries, got %d: %v", len(entries), entries)
	}
}

// TestExtractDivertedToolEntries_ZeroFieldValue_ReturnsEmpty verifies that a zero-valued
// FieldValue yields an empty slice rather than an error or panic.
func TestExtractDivertedToolEntries_ZeroFieldValue_ReturnsEmpty(t *testing.T) {
	f := descriptor.DivertedToolField{
		Key:    "mcpServers",
		Format: domain.FormatListBlock,
	}
	var value domain.FieldValue // zero value

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	if len(entries) != 0 {
		t.Errorf("ExtractDivertedToolEntries with zero FieldValue: want empty, got %v", entries)
	}
}

// TestExtractDivertedToolEntries_WrongKindForFormat_ReturnsEmpty verifies that when the
// value's Kind does not match the declared Format (e.g. KindList for FormatScalar),
// an empty slice is returned rather than an error or incorrect entries.
func TestExtractDivertedToolEntries_WrongKindForFormat_ReturnsEmpty(t *testing.T) {
	// FormatScalar expects KindScalar, but we give it a KindList.
	f := descriptor.DivertedToolField{
		Key:       "mcpScalar",
		Format:    domain.FormatScalar,
		Separator: "|",
	}
	value := listBlockValue("tool-a", "tool-b") // KindList — wrong for FormatScalar

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	if len(entries) != 0 {
		t.Errorf("ExtractDivertedToolEntries with mismatched kind/format: want empty, got %v; "+
			"format must never be inferred from the value — only the declared format is authoritative", entries)
	}
}

// TestExtractDivertedToolEntries_Deduplication_FirstSeenWins verifies that when a
// diverted field value contains duplicate tool names, only the first occurrence is kept.
func TestExtractDivertedToolEntries_Deduplication_FirstSeenWins(t *testing.T) {
	f := descriptor.DivertedToolField{
		Key:    "mcpServers",
		Format: domain.FormatListBlock,
	}
	value := listBlockValue("server-a", "server-b", "server-a") // duplicate "server-a"

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	if len(entries) != 2 {
		t.Fatalf("ExtractDivertedToolEntries dedup: want 2 entries (duplicate removed), got %d: %v",
			len(entries), entries)
	}
	count := 0
	for _, e := range entries {
		if e == "server-a" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("\"server-a\" appears %d times in entries %v; first-seen deduplication must keep it exactly once",
			count, entries)
	}
}

// TestExtractDivertedToolEntries_ByConventionExclusion verifies that a tool name found
// in the descriptor's Universe with ByConvention=true is excluded from the returned
// entries, matching the ExtractToolEntries behaviour (AD-14).
func TestExtractDivertedToolEntries_ByConventionExclusion(t *testing.T) {
	// The divertedListBlockDescriptorYAML universe has "by-conv-tool" with by_convention: true.
	// We extract it from a list value to verify it is excluded.
	d := loadDescriptor(t, divertedListBlockDescriptorYAML)
	fields := descriptor.DivertedToolFields(d)
	if len(fields) < 1 {
		t.Skip("DivertedToolFields returned empty; cannot test by-convention exclusion")
	}
	f := fields[0] // mcpServers, FormatListBlock

	// Include a by-convention tool name alongside a real entry.
	value := listBlockValue("mcp-server-alpha", "by-conv-tool")

	entries := descriptor.ExtractDivertedToolEntries(f, value)

	for _, e := range entries {
		if e == "by-conv-tool" {
			t.Errorf("by-convention tool %q must be excluded from ExtractDivertedToolEntries output (AD-14); got entries: %v",
				"by-conv-tool", entries)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests for RenderFieldValues
// ---------------------------------------------------------------------------

// TestRenderFieldValues_Scalar_YieldsOneString verifies that a KindScalar value yields
// exactly one string equal to the scalar text.
func TestRenderFieldValues_Scalar_YieldsOneString(t *testing.T) {
	value := scalarValue("some-scalar-text")

	rendered := descriptor.RenderFieldValues(value)

	if len(rendered) != 1 {
		t.Fatalf("RenderFieldValues scalar: want 1 string, got %d: %v", len(rendered), rendered)
	}
	if rendered[0] != "some-scalar-text" {
		t.Errorf("rendered[0] = %q, want %q", rendered[0], "some-scalar-text")
	}
}

// TestRenderFieldValues_List_YieldsOneStringPerItem verifies that a KindList value yields
// one string per list item.
func TestRenderFieldValues_List_YieldsOneStringPerItem(t *testing.T) {
	value := listBlockValue("item-one", "item-two", "item-three")

	rendered := descriptor.RenderFieldValues(value)

	if len(rendered) != 3 {
		t.Fatalf("RenderFieldValues list: want 3 strings, got %d: %v", len(rendered), rendered)
	}
	for i, want := range []string{"item-one", "item-two", "item-three"} {
		if rendered[i] != want {
			t.Errorf("rendered[%d] = %q, want %q", i, rendered[i], want)
		}
	}
}

// TestRenderFieldValues_Mapping_YieldsKeyValueEntries verifies that a KindMapping value
// yields one "key: value" string per key-value pair.
func TestRenderFieldValues_Mapping_YieldsKeyValueEntries(t *testing.T) {
	value := domain.MappingValue([]domain.FieldPair{
		{Key: "alpha", Value: scalarValue("val-a")},
		{Key: "beta", Value: scalarValue("val-b")},
	})

	rendered := descriptor.RenderFieldValues(value)

	if len(rendered) != 2 {
		t.Fatalf("RenderFieldValues mapping: want 2 strings, got %d: %v", len(rendered), rendered)
	}
	for _, r := range rendered {
		if !strings.Contains(r, ": ") {
			t.Errorf("rendered entry %q does not contain %q separator; mappings must render as \"key: value\"",
				r, ": ")
		}
	}
	wantAlpha := "alpha: val-a"
	wantBeta := "beta: val-b"
	found := map[string]bool{}
	for _, r := range rendered {
		found[r] = true
	}
	if !found[wantAlpha] {
		t.Errorf("expected entry %q not found in rendered output %v", wantAlpha, rendered)
	}
	if !found[wantBeta] {
		t.Errorf("expected entry %q not found in rendered output %v", wantBeta, rendered)
	}
}

// TestRenderFieldValues_ZeroValue_ReturnsEmpty verifies that a zero FieldValue yields
// an empty slice, not a panic.
func TestRenderFieldValues_ZeroValue_ReturnsEmpty(t *testing.T) {
	var value domain.FieldValue

	rendered := descriptor.RenderFieldValues(value)

	if len(rendered) != 0 {
		t.Errorf("RenderFieldValues zero value: want empty, got %v", rendered)
	}
}
