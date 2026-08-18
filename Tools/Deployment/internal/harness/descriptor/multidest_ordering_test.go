package descriptor_test

// multidest_ordering_test.go verifies that multi-destination emission is deterministic
// and stable across runs, per the emission algorithm contract:
//
//   - Main-list entries are sorted by Universe position; names absent from the Universe
//     appear after all Universe entries.
//   - Separate fields (DestField) emit in first-seen order across resolutions: the first
//     generic tool to contribute a name to a given field key establishes that key's
//     position in the Fields slice and its Format/Separator.
//   - Names within a separate field are deduplicated and sorted lexicographically.
//   - When two generic tools both contribute names to the same separate field key, those
//     names are merged into the same FrontmatterField entry; the field does not appear twice.
//   - A second generic tool targeting a field that was established by an earlier generic tool
//     contributes names but does not change the field's Format.
//
// All tests are RED until MapTools is updated to read ToolMapping.Destinations.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// makeOrderingDescriptor builds a list-shape descriptor designed to exercise ordering rules.
// Universe: alpha(0), beta(1), gamma(2). All unused: deny, by_convention: false.
// Mappings:
//   - tool_a: main["gamma"] — universe position 2
//   - tool_b: main["alpha", "beta"] — universe positions 0 and 1
//   - tool_c: field "shared" (list-block) ["cherry", "apple"]
//   - tool_d: field "shared" (list-flow) ["banana"]  — same key; format must not change; name merged
//   - tool_e: field "second" (list-block) ["z1"]     — establishes second key in order
//   - tool_f: main["extra"] — "extra" is not in Universe; must sort after all Universe tools
func makeOrderingDescriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "ordering-harness",
		DisplayName:   "Ordering Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "alpha", Unused: domain.Deny},
				{Name: "beta", Unused: domain.Deny},
				{Name: "gamma", Unused: domain.Deny},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_a",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"gamma"}},
					},
				},
				{
					Generic: "tool_b",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"alpha", "beta"}},
					},
				},
				{
					Generic: "tool_c",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "shared", Format: domain.FormatListBlock, Names: []string{"cherry", "apple"}},
					},
				},
				{
					Generic: "tool_d",
					Destinations: []domain.ToolDestination{
						// Same field key as tool_c; Format here is different (list-flow) but must be ignored
						// because tool_c established the field first. Names are merged.
						{Kind: domain.DestField, Field: "shared", Format: domain.FormatListFlow, Names: []string{"banana"}},
					},
				},
				{
					Generic: "tool_e",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "second", Format: domain.FormatListBlock, Names: []string{"z1"}},
					},
				},
				{
					Generic: "tool_f",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"extra"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
}

// --- Main-list: sorted by Universe position ---

func TestMultiDestOrdering_MainList_SortedByUniversePosition(t *testing.T) {
	// Main-list names must be emitted in Universe declaration order, regardless of the
	// order in which generic tools contribute them. Here tool_a contributes "gamma" (index 2)
	// before tool_b contributes "alpha" (index 0) and "beta" (index 1); output must be
	// [alpha, beta, gamma], not [gamma, alpha, beta].
	d := makeOrderingDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"tool_a", "tool_b"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		t.Fatal("main tools field absent from Fields")
	}
	items := toolsField.Value.Items
	if len(items) < 3 {
		t.Fatalf("expected at least 3 items in main tools field, got %d: %v", len(items), items)
	}
	wantOrder := []string{"alpha", "beta", "gamma"}
	for i, want := range wantOrder {
		if items[i].Scalar != want {
			t.Errorf("main tools item[%d]: want %q (universe order), got %q; "+
				"main-list names must be sorted by universe position", i, want, items[i].Scalar)
		}
	}
}

func TestMultiDestOrdering_MainList_UnknownUniverseNameAppearsAfterAllUniverseNames(t *testing.T) {
	// A name contributed by a DestMain destination that is not in the Universe must be
	// appended after all Universe-position-sorted entries.
	// "extra" is not in the Universe; it must follow "alpha", "beta", "gamma".
	d := makeOrderingDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"tool_a", "tool_b", "tool_f"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		t.Fatal("main tools field absent from Fields")
	}
	items := toolsField.Value.Items
	if len(items) < 4 {
		t.Fatalf("expected at least 4 items, got %d", len(items))
	}
	// "extra" must be last; "alpha", "beta", "gamma" must precede it.
	lastIdx := len(items) - 1
	if items[lastIdx].Scalar != "extra" {
		t.Errorf("unknown-universe name %q must appear after all universe-positioned names; "+
			"got last item %q; full list: %v", "extra", items[lastIdx].Scalar, items)
	}
}

// --- Separate fields: first-seen order across resolutions ---

func TestMultiDestOrdering_SeparateFields_EmittedInFirstSeenOrder(t *testing.T) {
	// When multiple generic tools contribute to distinct separate fields, those fields
	// must appear in the order the first contributing tool was encountered in req.Generic.
	// tool_c establishes "shared" before tool_e establishes "second"; Fields must have
	// [tools, shared, second] in that order.
	d := makeOrderingDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"tool_c", "tool_e"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	// Find positions of "shared" and "second" in result.Fields.
	sharedIdx, secondIdx := -1, -1
	for i, f := range result.Fields {
		switch f.Key {
		case "shared":
			sharedIdx = i
		case "second":
			secondIdx = i
		}
	}
	if sharedIdx < 0 {
		t.Fatal("shared field absent from Fields")
	}
	if secondIdx < 0 {
		t.Fatal("second field absent from Fields")
	}
	if sharedIdx >= secondIdx {
		t.Errorf("separate fields not in first-seen order: shared (index %d) must precede second (index %d); "+
			"first-seen order is determined by the first generic tool to contribute to each key", sharedIdx, secondIdx)
	}
}

// --- Names within a separate field: deduplicated and sorted ---

func TestMultiDestOrdering_SeparateField_NamesAreSortedLexicographically(t *testing.T) {
	// Names within a separate field must be deduplicated and sorted lexicographically.
	// tool_c contributes ["cherry", "apple"] to "shared"; the field must emit ["apple", "cherry"].
	d := makeOrderingDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"tool_c"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	sharedField := findField(t, result.Fields, "shared")
	if sharedField == nil {
		t.Fatal("shared field absent from Fields")
	}
	items := sharedField.Value.Items
	if len(items) < 2 {
		t.Fatalf("shared field: expected at least 2 items, got %d: %v", len(items), items)
	}
	if items[0].Scalar != "apple" || items[1].Scalar != "cherry" {
		t.Errorf("shared field names not in lexicographic order: want [apple, cherry], got [%s, %s]",
			items[0].Scalar, items[1].Scalar)
	}
}

func TestMultiDestOrdering_SeparateField_DuplicateNamesDeduplicatedAcrossGenericTools(t *testing.T) {
	// If two generic tools contribute the same name to the same separate field, it must
	// appear only once in the output.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "dedup-field-harness",
		DisplayName:   "Dedup Field Harness",
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_x",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "shared", Names: []string{"nameA"}},
					},
				},
				{
					Generic: "tool_y",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "shared", Names: []string{"nameA", "nameB"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{AgentKey: "test-agent", Generic: []string{"tool_x", "tool_y"}}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	sharedField := findField(t, result.Fields, "shared")
	if sharedField == nil {
		t.Fatal("shared field absent from Fields")
	}
	seen := make(map[string]int)
	for _, item := range sharedField.Value.Items {
		seen[item.Scalar]++
	}
	if seen["nameA"] > 1 {
		t.Errorf("nameA appears %d times in shared field; names within a separate field must be deduplicated", seen["nameA"])
	}
}

// --- Two generic tools sharing a separate field: names merged, format from first contributor ---

func TestMultiDestOrdering_SameFieldKey_NamesFromBothToolsMerged(t *testing.T) {
	// tool_c and tool_d both contribute to "shared". The merged field must contain
	// names from both tools: apple, banana, cherry (sorted).
	d := makeOrderingDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"tool_c", "tool_d"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	sharedField := findField(t, result.Fields, "shared")
	if sharedField == nil {
		t.Fatal("shared field absent from Fields")
	}
	for _, want := range []string{"apple", "banana", "cherry"} {
		if !fieldContainsName(sharedField, want) {
			t.Errorf("shared field missing %q (from merged contributions of tool_c and tool_d); got: %v",
				want, fieldItemNames(sharedField))
		}
	}
}

func TestMultiDestOrdering_SameFieldKey_FieldAppearsOnceNotTwice(t *testing.T) {
	// When two generic tools target the same separate field key, only one FrontmatterField
	// entry must be emitted with the merged names. Two entries for the same key is wrong.
	d := makeOrderingDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"tool_c", "tool_d"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	count := 0
	for _, f := range result.Fields {
		if f.Key == "shared" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("shared field appears %d times in Fields; it must appear exactly once even when "+
			"multiple generic tools contribute to it", count)
	}
}

func TestMultiDestOrdering_SameFieldKey_FormatFromFirstContributor(t *testing.T) {
	// tool_c (first to establish "shared") declares list-block; tool_d declares list-flow.
	// The resulting field must use list-block (from tool_c, the first contributor).
	d := makeOrderingDescriptor()
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"tool_c", "tool_d"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	sharedField := findField(t, result.Fields, "shared")
	if sharedField == nil {
		t.Fatal("shared field absent from Fields")
	}
	if sharedField.Value.Kind != domain.KindList {
		t.Fatalf("shared field value kind: want %q, got %q", domain.KindList, sharedField.Value.Kind)
	}
	if sharedField.Value.List != domain.ListBlock {
		t.Errorf("shared field list style: want %q (from first contributor tool_c), got %q; "+
			"a later generic tool targeting the same key must not override the established format",
			domain.ListBlock, sharedField.Value.List)
	}
}

// --- DestMain deduplication across multiple generic tools ---

func TestMultiDestOrdering_MainList_DuplicateNamesFromMultipleToolsDeduped(t *testing.T) {
	// If two generic tools both contribute the same harness tool name to the main field,
	// that name must appear only once in the main tools list.
	d := &domain.HarnessDescriptor{
		SchemaVersion: "1",
		ID:            "main-dedup-harness",
		DisplayName:   "Main Dedup Harness",
		Tools: domain.ToolSpec{
			Shape:    domain.ShapeList,
			Universe: []domain.HarnessTool{{Name: "sharedMainTool", Unused: domain.Deny}},
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_p",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"sharedMainTool"}},
					},
				},
				{
					Generic: "tool_q",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"sharedMainTool"}},
					},
				},
			},
		},
		Frontmatter: domain.FrontmatterSpec{ToolsKey: "tools"},
	}
	req := domain.ToolRequest{AgentKey: "test-agent", Generic: []string{"tool_p", "tool_q"}}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	toolsField := findField(t, result.Fields, "tools")
	if toolsField == nil {
		t.Fatal("tools field absent from Fields")
	}
	count := 0
	for _, item := range toolsField.Value.Items {
		if item.Scalar == "sharedMainTool" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("sharedMainTool appears %d times in main tools field; duplicate names from multiple "+
			"generic tools must be deduplicated to a single occurrence", count)
	}
}
