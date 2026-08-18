package descriptor_test

// multidest_list_test.go verifies multi-destination mapping in the flat-list (ShapeList) shape.
//
// A single generic tool may declare an ordered set of destinations. Each destination
// independently specifies a kind (main or field), a field name, a value format, and the
// harness-side names it contributes. The field builder must:
//   - Append DestMain names to the harness's own tools field.
//   - Emit one separate FrontmatterField per DestField destination, keyed by the mapping's
//     declared field name (not a hardcoded key).
//   - Honour the declared Format (list-block, list-flow, scalar) for each DestField.
//   - Never place DestField names in the main tools field and never place DestMain names in
//     a separate field.
//
// All tests in this file are RED until MapTools is updated to read ToolMapping.Destinations
// instead of the deprecated HarnessTools and Field fields.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// makeListMultiDestDescriptor builds a list-shape descriptor with:
//   - file_read  → DestMain only: ["read/readFile"]
//   - user_interaction → DestMain: ["AskUserQuestion"] + DestField "mcpServers" (list-block): ["user-feedback"]
//   - skill → DestField "mcpServers" (list-block): ["mcp-skill"] only (no DestMain)
func makeListMultiDestDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "list-multidest-harness",
		DisplayName:   "List Multi-Dest Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "read/readFile", Unused: domain.Deny, ByConvention: false},
				{Name: "AskUserQuestion", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "file_read",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"read/readFile"}},
					},
				},
				{
					Generic: "user_interaction",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
						{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{"user-feedback"}},
					},
				},
				{
					Generic: "skill",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{"mcp-skill"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
}

// --- Main destination names appear in the main tools field ---

func TestMultiDestList_MainDest_NamesAppearInMainToolsField(t *testing.T) {
	// A DestMain destination must contribute its names to the harness's main tools field.
	// "AskUserQuestion" is declared as a DestMain name in the user_interaction mapping.
	d := makeListMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		t.Fatal("main tools field absent from Fields")
	}
	if !fieldContainsName(toolsField, "AskUserQuestion") {
		t.Errorf("main tools field does not contain %q; DestMain destination names must appear in the main tools field; got items: %v",
			"AskUserQuestion", fieldItemNames(toolsField))
	}
}

func TestMultiDestList_MainDest_MultipleGenericToolsContributeToMainField(t *testing.T) {
	// When multiple generic tools have DestMain destinations, all their names must appear
	// in the main tools field.
	d := makeListMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		t.Fatal("main tools field absent from Fields")
	}
	for _, wantName := range []string{"read/readFile", "AskUserQuestion"} {
		if !fieldContainsName(toolsField, wantName) {
			t.Errorf("main tools field missing %q (from DestMain destination); got: %v",
				wantName, fieldItemNames(toolsField))
		}
	}
}

// --- Separate field destination: field is created and populated ---

func TestMultiDestList_FieldDest_SeparateFieldExists(t *testing.T) {
	// A DestField destination must produce a separate FrontmatterField in the output,
	// keyed by the field name declared in the mapping.
	d := makeListMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if findField(t, result.Fields, "mcpServers") == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("separate field %q absent from Fields output; DestField destinations must produce a dedicated FrontmatterField; got keys: %v",
			"mcpServers", keys)
	}
}

func TestMultiDestList_FieldDest_SeparateFieldContainsDeclaredNames(t *testing.T) {
	// The separate field created for a DestField destination must contain the names
	// declared in that destination, not names from other destinations.
	d := makeListMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	mcpField := findField(t, result.Fields, "mcpServers")
	if mcpField == nil {
		t.Fatal("mcpServers field absent from Fields")
	}
	if !fieldContainsName(mcpField, "user-feedback") {
		t.Errorf("mcpServers field does not contain %q; DestField destination names must appear in the separate field; got: %v",
			"user-feedback", fieldItemNames(mcpField))
	}
}

func TestMultiDestList_FieldDest_FieldKeyComesFromMappingDeclaration(t *testing.T) {
	// The key of the separate field must match exactly what the mapping declaration specifies.
	// The algorithm must not hardcode any field name; it must use the value from the mapping.
	// This test uses a different field name to confirm the key is not hardcoded.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "custom-key-harness",
		DisplayName:   "Custom Key Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{
					Generic: "some_tool",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "customFieldKey", Names: []string{"toolA"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{AgentKey: "test-agent", Generic: []string{"some_tool"}}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if findField(t, result.Fields, "customFieldKey") == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("expected field with key %q from the mapping declaration; got keys: %v; "+
			"the field name must come from the mapping, not from hardcoded logic",
			"customFieldKey", keys)
	}
}

// --- Cross-contamination: destinations must not bleed into each other ---

func TestMultiDestList_FieldDest_NamesAbsentFromMainToolsField(t *testing.T) {
	// Names declared in a DestField destination must NOT appear in the main tools field.
	// "user-feedback" is a DestField name; it must not leak into the main tools list.
	d := makeListMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		return // no main field → DestField name definitely absent
	}
	if fieldContainsName(toolsField, "user-feedback") {
		t.Errorf("DestField name %q must not appear in the main tools field; only DestMain names belong there",
			"user-feedback")
	}
}

func TestMultiDestList_MainDest_NamesAbsentFromSeparateField(t *testing.T) {
	// Names declared in a DestMain destination must NOT appear in the separate DestField bucket.
	// "AskUserQuestion" is a DestMain name; it must not appear in the mcpServers field.
	d := makeListMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	mcpField := findField(t, result.Fields, "mcpServers")
	if mcpField == nil {
		return // no separate field → DestMain name definitely absent
	}
	if fieldContainsName(mcpField, "AskUserQuestion") {
		t.Errorf("DestMain name %q must not appear in the separate mcpServers field; "+
			"only DestField names targeting that field belong there", "AskUserQuestion")
	}
}

func TestMultiDestList_FieldOnlyDest_NameAbsentFromMainField(t *testing.T) {
	// When a generic tool has ONLY DestField destinations (no DestMain), its names must
	// not appear in the main tools field under any form.
	// "skill" maps only to DestField "mcpServers". "mcp-skill" must not reach the main field.
	d := makeListMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"skill"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		return
	}
	if fieldContainsName(toolsField, "mcp-skill") {
		t.Errorf("DestField-only name %q must not appear in the main tools field", "mcp-skill")
	}
}

func TestMultiDestList_FieldOnlyDest_SeparateFieldContainsName(t *testing.T) {
	// The counterpart to the above: the DestField-only name must appear in its declared field.
	d := makeListMultiDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"skill"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	mcpField := findField(t, result.Fields, "mcpServers")
	if mcpField == nil {
		t.Fatal("mcpServers field absent from Fields; DestField-only tool must still produce a separate field")
	}
	if !fieldContainsName(mcpField, "mcp-skill") {
		t.Errorf("mcpServers field does not contain %q; got: %v",
			"mcp-skill", fieldItemNames(mcpField))
	}
}

// --- Value format is taken from the mapping declaration ---

func TestMultiDestList_FormatListBlock_EmitsKindListWithListBlockStyle(t *testing.T) {
	// A DestField destination declaring format: list-block must produce a FrontmatterField
	// whose value is KindList with ListBlock style. The format must come from the mapping
	// declaration, not from hardcoded logic.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "fmt-listblock-harness",
		DisplayName:   "Format ListBlock Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_a",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "myField", Format: domain.FormatListBlock, Names: []string{"nameA"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{AgentKey: "test-agent", Generic: []string{"tool_a"}}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	f := findField(t, result.Fields, "myField")
	if f == nil {
		t.Fatal("myField absent from Fields")
	}
	if f.Value.Kind != domain.KindList {
		t.Fatalf("myField value kind: want %q (FormatListBlock), got %q", domain.KindList, f.Value.Kind)
	}
	if f.Value.List != domain.ListBlock {
		t.Errorf("myField list style: want %q (FormatListBlock), got %q", domain.ListBlock, f.Value.List)
	}
}

func TestMultiDestList_FormatListFlow_EmitsKindListWithListFlowStyle(t *testing.T) {
	// A DestField destination declaring format: list-flow must produce a FrontmatterField
	// with KindList and ListFlow style.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "fmt-listflow-harness",
		DisplayName:   "Format ListFlow Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_b",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "flowField", Format: domain.FormatListFlow, Names: []string{"nameB"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{AgentKey: "test-agent", Generic: []string{"tool_b"}}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	f := findField(t, result.Fields, "flowField")
	if f == nil {
		t.Fatal("flowField absent from Fields")
	}
	if f.Value.Kind != domain.KindList {
		t.Fatalf("flowField value kind: want %q (FormatListFlow), got %q", domain.KindList, f.Value.Kind)
	}
	if f.Value.List != domain.ListFlow {
		t.Errorf("flowField list style: want %q (FormatListFlow), got %q", domain.ListFlow, f.Value.List)
	}
}

func TestMultiDestList_FormatScalar_EmitsKindScalarWithNamesSeparated(t *testing.T) {
	// A DestField destination declaring format: scalar must produce a FrontmatterField with
	// KindScalar whose value is the names joined by the declared separator.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "fmt-scalar-harness",
		DisplayName:   "Format Scalar Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_c",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "scalarField", Format: domain.FormatScalar, Separator: "|", Names: []string{"alpha", "beta"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{AgentKey: "test-agent", Generic: []string{"tool_c"}}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	f := findField(t, result.Fields, "scalarField")
	if f == nil {
		t.Fatal("scalarField absent from Fields")
	}
	if f.Value.Kind != domain.KindScalar {
		t.Fatalf("scalarField value kind: want %q (FormatScalar), got %q", domain.KindScalar, f.Value.Kind)
	}
	// Names joined by "|" separator: "alpha|beta".
	const wantScalar = "alpha|beta"
	if f.Value.Scalar != wantScalar {
		t.Errorf("scalarField scalar value: want %q (names joined by separator), got %q", wantScalar, f.Value.Scalar)
	}
}

func TestMultiDestList_FormatScalar_DefaultSeparatorAppliedWhenOmitted(t *testing.T) {
	// When Separator is empty on a FormatScalar destination, DefaultScalarSeparator is used.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "default-sep-harness",
		DisplayName:   "Default Separator Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_d",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "sepField", Format: domain.FormatScalar, Names: []string{"x", "y"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{AgentKey: "test-agent", Generic: []string{"tool_d"}}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	f := findField(t, result.Fields, "sepField")
	if f == nil {
		t.Fatal("sepField absent from Fields")
	}
	if f.Value.Kind != domain.KindScalar {
		t.Fatalf("sepField value kind: want %q, got %q", domain.KindScalar, f.Value.Kind)
	}
	// DefaultScalarSeparator == ", "; names "x" and "y" → "x, y".
	wantScalar := "x" + domain.DefaultScalarSeparator + "y"
	if f.Value.Scalar != wantScalar {
		t.Errorf("sepField scalar value with default separator: want %q, got %q", wantScalar, f.Value.Scalar)
	}
}

func TestMultiDestList_FormatDefault_ListBlockAppliedWhenFormatOmitted(t *testing.T) {
	// When Format is empty on a DestField destination, DefaultToolValueFormat (list-block) is used.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "default-fmt-harness",
		DisplayName:   "Default Format Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_e",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "defaultFmtField", Names: []string{"n1"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{AgentKey: "test-agent", Generic: []string{"tool_e"}}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	f := findField(t, result.Fields, "defaultFmtField")
	if f == nil {
		t.Fatal("defaultFmtField absent from Fields")
	}
	if f.Value.Kind != domain.KindList {
		t.Fatalf("defaultFmtField value kind: want %q (DefaultToolValueFormat=list-block), got %q",
			domain.KindList, f.Value.Kind)
	}
	if f.Value.List != domain.ListBlock {
		t.Errorf("defaultFmtField list style: want %q (DefaultToolValueFormat=list-block), got %q",
			domain.ListBlock, f.Value.List)
	}
}

// --- Helper functions ---

// findField returns the first FrontmatterField with the given key, or nil if absent.
// Unlike the inline helper used in maptools_test.go, this does not call t.Fatalf on absence
// so callers can produce their own informative messages.
func findField(_ *testing.T, fields []domain.FrontmatterField, key string) *domain.FrontmatterField {
	for i := range fields {
		if fields[i].Key == key {
			return &fields[i]
		}
	}
	return nil
}

// fieldContainsName returns true if any scalar item or pair key in the field's value
// equals name.
func fieldContainsName(f *domain.FrontmatterField, name string) bool {
	switch f.Value.Kind {
	case domain.KindList:
		for _, item := range f.Value.Items {
			if item.Kind == domain.KindScalar && item.Scalar == name {
				return true
			}
		}
	case domain.KindScalar:
		return f.Value.Scalar == name
	case domain.KindMapping:
		for _, p := range f.Value.Pairs {
			if p.Key == name {
				return true
			}
		}
	}
	return false
}

// fieldItemNames returns the scalar values of all KindScalar items in a KindList field,
// for use in diagnostic messages.
func fieldItemNames(f *domain.FrontmatterField) []string {
	if f.Value.Kind != domain.KindList {
		return []string{f.Value.Scalar}
	}
	names := make([]string, 0, len(f.Value.Items))
	for _, item := range f.Value.Items {
		if item.Kind == domain.KindScalar {
			names = append(names, item.Scalar)
		}
	}
	return names
}

// =============================================================================
// Custom tool default destinations: list-shape rendering
//
// When a descriptor declares a non-empty CustomToolDestinations list with a DestField
// entry and a custom name is supplied for an unmapped generic tool, buildListToolFields
// must route the name through the same DestField rendering path used for ToolMapped
// resolutions with DestField destinations. Specifically:
//   - The custom name must appear in the separate FrontmatterField declared by the
//     destination's field name.
//   - The custom name must NOT appear in the main tools field when the declared list
//     contains no DestMain destination.
//   - When a DestMain destination is also declared, the custom name appears in the
//     main tools field as well as in any DestField fields.
// =============================================================================

// makeListCustomDestDescriptor builds a list-shape descriptor with:
//   - file_read → DestMain ["read/readFile"]
//   - CustomToolDestinations: [{DestField, "mcpServers", FormatListBlock}]
//
// "terminal" has no mapping entry. A custom name for it should exercise the
// DestField custom-destination rendering path.
func makeListCustomDestDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "list-custdest-harness",
		DisplayName:   "List CustomDest Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "read/readFile", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "file_read",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"read/readFile"}},
					},
				},
			},
			CustomToolDestinations: []domain.ToolDestination{
				{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{}},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
}

// makeListCustomDestMainAndFieldDescriptor builds a list-shape descriptor with
// CustomToolDestinations declaring both DestMain and DestField destinations.
// Used to verify that a custom name fans out to both the main field and the separate field.
func makeListCustomDestMainAndFieldDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "list-custdest-main-and-field-harness",
		DisplayName:   "List CustomDest Main And Field Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "read/readFile", Unused: domain.Deny, ByConvention: false},
			},
			CustomToolDestinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{}},
				{Kind: domain.DestField, Field: "mcpServers", Format: domain.FormatListBlock, Names: []string{}},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
}

// TestMultiDestList_CustomDest_FieldDestAppearsInFieldsOutput asserts that when
// CustomToolDestinations declares a DestField destination, a ToolCustom resolution
// produces a FrontmatterField with the declared key in the Fields output.
// Without this field the custom name is lost from the rendered frontmatter.
// This test is RED until MapTools reads CustomToolDestinations in the ToolCustom branch.
func TestMultiDestList_CustomDest_FieldDestAppearsInFieldsOutput(t *testing.T) {
	d := makeListCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if findField(t, result.Fields, "mcpServers") == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("mcpServers field absent from Fields; custom_tool_destination must route the "+
			"custom name to a separate FrontmatterField when to:field is declared; got keys: %v", keys)
	}
}

// TestMultiDestList_CustomDest_FieldContainsCustomName asserts that the separate field
// produced for the DestField custom destination contains the formatted custom-tool name.
// This test is RED until MapTools reads CustomToolDestinations.
func TestMultiDestList_CustomDest_FieldContainsCustomName(t *testing.T) {
	d := makeListCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	mcpField := findField(t, result.Fields, "mcpServers")
	if mcpField == nil {
		t.Fatal("mcpServers field absent from Fields")
	}
	if !fieldContainsName(mcpField, "my-server") {
		t.Errorf("mcpServers field does not contain %q; DestField custom destination must "+
			"route the custom name to the declared field; got: %v",
			"my-server", fieldItemNames(mcpField))
	}
}

// TestMultiDestList_CustomDest_FieldOnlyDest_CustomNameAbsentFromMainToolsField asserts
// that when CustomToolDestinations declares only DestField destinations (no DestMain),
// the custom name must NOT appear in the main tools field.
// The pre-change behaviour always puts the custom name in the main tools field via a
// hardcoded DestMain, so this test is RED until MapTools reads CustomToolDestinations.
func TestMultiDestList_CustomDest_FieldOnlyDest_CustomNameAbsentFromMainToolsField(t *testing.T) {
	d := makeListCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		return // no main tools field → custom name cannot be in it
	}
	if fieldContainsName(toolsField, "my-server") {
		t.Errorf("custom name %q must not appear in the main tools field when "+
			"CustomToolDestinations declares only DestField destinations; "+
			"the name belongs in the declared separate field (mcpServers) only",
			"my-server")
	}
}

// TestMultiDestList_CustomDest_MainAndFieldDest_CustomNameAppearsInBothFields asserts
// that when CustomToolDestinations declares both DestMain and DestField destinations,
// the custom name fans out to both: it appears in the main tools field AND in the
// separate DestField field.
// This test is RED until MapTools reads CustomToolDestinations.
func TestMultiDestList_CustomDest_MainAndFieldDest_CustomNameAppearsInBothFields(t *testing.T) {
	d := makeListCustomDestMainAndFieldDescriptor()
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	// Custom name must appear in the main tools field (via DestMain).
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		t.Error("main tools field absent from Fields; DestMain destination must produce a main tools entry")
	} else if !fieldContainsName(toolsField, "my-server") {
		t.Errorf("main tools field does not contain %q; DestMain destination must route the "+
			"custom name to the main tools field; got: %v",
			"my-server", fieldItemNames(toolsField))
	}
	// Custom name must also appear in the separate mcpServers field (via DestField).
	mcpField := findField(t, result.Fields, "mcpServers")
	if mcpField == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("mcpServers field absent from Fields; DestField destination must produce a "+
			"separate FrontmatterField; got keys: %v", keys)
	} else if !fieldContainsName(mcpField, "my-server") {
		t.Errorf("mcpServers field does not contain %q; DestField destination must route the "+
			"custom name to the declared field; got: %v",
			"my-server", fieldItemNames(mcpField))
	}
}

// TestMultiDestList_CustomDest_FieldFormat_ListBlockApplied asserts that the Format
// declared in the CustomToolDestinations entry is applied to the resulting FrontmatterField.
// A format:list-block entry must produce a KindList field with ListBlock style.
// This test is RED until MapTools reads CustomToolDestinations.
func TestMultiDestList_CustomDest_FieldFormat_ListBlockApplied(t *testing.T) {
	d := makeListCustomDestDescriptor() // declares format: list-block
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	mcpField := findField(t, result.Fields, "mcpServers")
	if mcpField == nil {
		t.Fatal("mcpServers field absent from Fields")
	}
	if mcpField.Value.Kind != domain.KindList {
		t.Errorf("mcpServers field value kind: want %q (format:list-block), got %q",
			domain.KindList, mcpField.Value.Kind)
	}
	if mcpField.Value.Kind == domain.KindList && mcpField.Value.List != domain.ListBlock {
		t.Errorf("mcpServers list style: want %q (format:list-block), got %q",
			domain.ListBlock, mcpField.Value.List)
	}
}

// TestMultiDestList_CustomDest_NormalMappedToolUnchanged asserts that the standard
// ToolMapped path (file_read → DestMain) is not affected by CustomToolDestinations
// being declared on the same descriptor.
func TestMultiDestList_CustomDest_NormalMappedToolUnchanged(t *testing.T) {
	d := makeListCustomDestDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	// file_read must appear in the main tools field as before.
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		t.Fatal("main tools field absent from Fields")
	}
	if !fieldContainsName(toolsField, "read/readFile") {
		t.Errorf("main tools field does not contain %q; ToolMapped path must be unaffected by CustomToolDestinations; got: %v",
			"read/readFile", fieldItemNames(toolsField))
	}
	// mcpServers must not be emitted when no custom tool is in the request.
	if mcpField := findField(t, result.Fields, "mcpServers"); mcpField != nil {
		t.Errorf("mcpServers field must not appear when no ToolCustom resolution is in the request; "+
			"CustomToolDestinations only applies to custom-tool resolutions")
	}
}
