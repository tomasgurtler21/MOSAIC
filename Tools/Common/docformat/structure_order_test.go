package docformat_test

// Tests for the structure-order comparison functions: StructureNames, OrderDiffers, and
// ReorderDetected.
//
// All tests are in the TDD RED phase: StructureNames, OrderDiffers, and ReorderDetected do
// not yet exist. Tests are expected to fail to compile until the implementation is added to
// a new file (e.g. structure_order.go) in the docformat package.
//
// Coverage (OrderDiffers — pure string-slice comparison):
//   - Both lists empty → false.
//   - Either list empty, the other non-empty → false.
//   - Single common element, identical order → false.
//   - Multiple common elements, identical order → false.
//   - Extra element in first list; common elements in same relative order → false.
//   - Extra element in second list; common elements in same relative order → false.
//   - Element missing from first list; common elements in same relative order → false.
//   - Two common elements swapped → true.
//   - Three common elements, last two swapped → true.
//   - Three common elements, first two swapped → true.
//   - No common elements → false.
//   - Duplicate names in a list: first occurrence used; no panic.
//
// Coverage (StructureNames — extract ordered structural slot names from a Body):
//   - Document with only top-level sections: returns section names in document order.
//   - Document with top-level sections and top-level deployed regions interleaved: returns both.
//   - Nested deployed regions are excluded from StructureNames.
//   - Top-level custom and injection regions are not included.
//   - Empty body: returns nil or empty.
//
// Coverage (ReorderDetected — convenience wrapper):
//   - Documents with identical structural slot order → false.
//   - Source adds a section; common slots still in same relative order → false.
//   - Source removes a section; common slots still in same relative order → false.
//   - Source swaps two sections → true.
//   - Both documents have no structural slots → false.
//   - Top-level deployed regions participate in the comparison (AD-2).
//   - Nested deployed regions do not participate.

import (
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// OrderDiffers — pure string-slice comparison
// ---------------------------------------------------------------------------

func TestOrderDiffers_BothNil_ReturnsFalse(t *testing.T) {
	if docformat.OrderDiffers(nil, nil) {
		t.Error("OrderDiffers(nil, nil): want false, got true")
	}
}

func TestOrderDiffers_BothEmpty_ReturnsFalse(t *testing.T) {
	if docformat.OrderDiffers([]string{}, []string{}) {
		t.Error("OrderDiffers([], []): want false, got true")
	}
}

func TestOrderDiffers_FirstNilSecondNonEmpty_ReturnsFalse(t *testing.T) {
	// No common elements → no reorder possible.
	if docformat.OrderDiffers(nil, []string{"A"}) {
		t.Error("OrderDiffers(nil, [A]): want false, got true — no common elements")
	}
}

func TestOrderDiffers_FirstNonEmptySecondNil_ReturnsFalse(t *testing.T) {
	if docformat.OrderDiffers([]string{"A"}, nil) {
		t.Error("OrderDiffers([A], nil): want false, got true — no common elements")
	}
}

func TestOrderDiffers_SingleCommonElement_ReturnsFalse(t *testing.T) {
	// A single shared element cannot be reordered relative to itself.
	if docformat.OrderDiffers([]string{"A"}, []string{"A"}) {
		t.Error("OrderDiffers([A], [A]): want false, got true")
	}
}

func TestOrderDiffers_IdenticalTwoElements_ReturnsFalse(t *testing.T) {
	if docformat.OrderDiffers([]string{"A", "B"}, []string{"A", "B"}) {
		t.Error("OrderDiffers([A, B], [A, B]): want false, got true")
	}
}

func TestOrderDiffers_IdenticalThreeElements_ReturnsFalse(t *testing.T) {
	if docformat.OrderDiffers([]string{"A", "B", "C"}, []string{"A", "B", "C"}) {
		t.Error("OrderDiffers([A, B, C], [A, B, C]): want false, got true")
	}
}

func TestOrderDiffers_ExtraElementInFirstList_CommonInSameOrder_ReturnsFalse(t *testing.T) {
	// X is present only in the first list. Common elements A and B remain in the same
	// relative order. A pure addition must not trigger a reorder verdict.
	if docformat.OrderDiffers([]string{"A", "X", "B"}, []string{"A", "B"}) {
		t.Error("OrderDiffers([A, X, B], [A, B]): want false — X is an addition, not a reorder")
	}
}

func TestOrderDiffers_ExtraElementInSecondList_CommonInSameOrder_ReturnsFalse(t *testing.T) {
	// Y is present only in the second list. Common elements A and C remain in the same order.
	if docformat.OrderDiffers([]string{"A", "C"}, []string{"A", "Y", "C"}) {
		t.Error("OrderDiffers([A, C], [A, Y, C]): want false — Y is an addition, not a reorder")
	}
}

func TestOrderDiffers_ElementMissingFromFirstList_CommonInSameOrder_ReturnsFalse(t *testing.T) {
	// C is present only in the second list (absent from first). Common elements A and B
	// remain in the same relative order. A pure removal must not trigger a reorder verdict.
	if docformat.OrderDiffers([]string{"A", "B"}, []string{"A", "B", "C"}) {
		t.Error("OrderDiffers([A, B], [A, B, C]): want false — C is a removal from first, not a reorder")
	}
}

func TestOrderDiffers_TwoCommonElementsSwapped_ReturnsTrue(t *testing.T) {
	// A appears before B in the first list; B appears before A in the second.
	if !docformat.OrderDiffers([]string{"A", "B"}, []string{"B", "A"}) {
		t.Error("OrderDiffers([A, B], [B, A]): want true — elements are swapped")
	}
}

func TestOrderDiffers_ThreeElements_LastTwoSwapped_ReturnsTrue(t *testing.T) {
	if !docformat.OrderDiffers([]string{"A", "B", "C"}, []string{"A", "C", "B"}) {
		t.Error("OrderDiffers([A, B, C], [A, C, B]): want true — B and C are swapped")
	}
}

func TestOrderDiffers_ThreeElements_FirstTwoSwapped_ReturnsTrue(t *testing.T) {
	if !docformat.OrderDiffers([]string{"A", "B", "C"}, []string{"B", "A", "C"}) {
		t.Error("OrderDiffers([A, B, C], [B, A, C]): want true — A and B are swapped")
	}
}

func TestOrderDiffers_NoCommonElements_ReturnsFalse(t *testing.T) {
	// Completely disjoint lists share no elements; no relative order can differ.
	if docformat.OrderDiffers([]string{"A", "B"}, []string{"C", "D"}) {
		t.Error("OrderDiffers([A, B], [C, D]): want false — no common elements")
	}
}

func TestOrderDiffers_DuplicatesInFirstList_FirstOccurrenceUsed_NoReorder(t *testing.T) {
	// Duplicate names are compared at their first occurrence; later occurrences are ignored.
	// [A, A, B] vs [A, B]: treating the first A as representative, common elements are in order.
	if docformat.OrderDiffers([]string{"A", "A", "B"}, []string{"A", "B"}) {
		t.Error("OrderDiffers([A, A, B], [A, B]): want false — first A is representative, order matches")
	}
}

// ---------------------------------------------------------------------------
// StructureNames — extract structural slot names from a Body
// ---------------------------------------------------------------------------

func TestStructureNames_OnlySections_ReturnsSectionNamesInDocumentOrder(t *testing.T) {
	// multiple-sections.md has <Identity type="core"> then <CommunicationProtocol type="core">.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	names := docformat.StructureNames(doc.Body())

	if len(names) != 2 {
		t.Fatalf("StructureNames: want 2 names, got %d: %v", len(names), names)
	}
	if names[0] != "Identity" {
		t.Errorf("names[0]: want %q, got %q", "Identity", names[0])
	}
	if names[1] != "CommunicationProtocol" {
		t.Errorf("names[1]: want %q, got %q", "CommunicationProtocol", names[1])
	}
}

func TestStructureNames_SectionsAndTopLevelDeployed_ReturnsAllInterleaved(t *testing.T) {
	// mixed-markers.md top-level items in document order:
	//   <Identity type="core">
	//   <CommunicationProtocol type="managed">
	//   <Constraints type="core"> (contains <ProtocolConstraints type="managed"> — nested, excluded)
	// StructureNames must return all three top-level structural slots.
	doc := parsedBoundaryFixture(t, "mixed-markers.md")

	names := docformat.StructureNames(doc.Body())

	if len(names) != 3 {
		t.Fatalf("StructureNames: want 3 names (Identity, CommunicationProtocol, Constraints), got %d: %v", len(names), names)
	}
	if names[0] != "Identity" {
		t.Errorf("names[0]: want %q, got %q", "Identity", names[0])
	}
	if names[1] != "CommunicationProtocol" {
		t.Errorf("names[1]: want %q, got %q", "CommunicationProtocol", names[1])
	}
	if names[2] != "Constraints" {
		t.Errorf("names[2]: want %q, got %q", "Constraints", names[2])
	}
}

func TestStructureNames_NestedDeployedRegion_NotIncluded(t *testing.T) {
	// mixed-markers.md has <ProtocolConstraints type="managed"> nested inside
	// <Constraints type="core">. It must not appear in StructureNames.
	doc := parsedBoundaryFixture(t, "mixed-markers.md")

	names := docformat.StructureNames(doc.Body())

	for _, name := range names {
		if name == "ProtocolConstraints" {
			t.Error("StructureNames must not include the nested deployed region ProtocolConstraints")
		}
	}
}

func TestStructureNames_TopLevelCustomRegion_NotIncluded(t *testing.T) {
	// mixed-markers-with-custom.md has <ProjectNotes type="custom"> at body top level.
	// Custom regions are not structural slots and must not appear in StructureNames.
	doc := parsedBoundaryFixture(t, "mixed-markers-with-custom.md")

	names := docformat.StructureNames(doc.Body())

	for _, name := range names {
		if name == "ProjectNotes" {
			t.Errorf("StructureNames must not include the top-level custom region ProjectNotes; got names: %v", names)
		}
	}
}

func TestStructureNames_InjectionRegion_NotIncluded(t *testing.T) {
	// mixed-markers-with-custom.md has <IdentityExtension type="project"> inside Identity.
	// Injection regions are not structural slots and must not appear in StructureNames.
	doc := parsedBoundaryFixture(t, "mixed-markers-with-custom.md")

	names := docformat.StructureNames(doc.Body())

	for _, name := range names {
		if name == "IdentityExtension" {
			t.Errorf("StructureNames must not include injection region IdentityExtension; got names: %v", names)
		}
	}
}

func TestStructureNames_EmptyBody_ReturnsNilOrEmpty(t *testing.T) {
	// A document with no sections and no top-level deployed regions.
	doc := parseInlineDoc(t, "Plain text with no boundary tags.\n")

	names := docformat.StructureNames(doc.Body())

	if len(names) != 0 {
		t.Errorf("StructureNames on body with no structural slots: want empty, got %v", names)
	}
}

// ---------------------------------------------------------------------------
// ReorderDetected — convenience wrapper over StructureNames + OrderDiffers
// ---------------------------------------------------------------------------

func TestReorderDetected_IdenticalSectionOrder_ReturnsFalse(t *testing.T) {
	deployed := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<Constraints type=\"core\">\n</Constraints>\n")
	source := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<Constraints type=\"core\">\n</Constraints>\n")

	if docformat.ReorderDetected(deployed, source) {
		t.Error("ReorderDetected: want false for documents with identical structural slot order")
	}
}

func TestReorderDetected_SourceAddsSection_CommonSlotsUnchanged_ReturnsFalse(t *testing.T) {
	// Source gains ExecutionPhilosophy between Identity and Constraints.
	// The relative order of common slots (Identity, Constraints) is unchanged.
	// A pure addition must not trigger a reorder.
	deployed := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<Constraints type=\"core\">\n</Constraints>\n")
	source := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<ExecutionPhilosophy type=\"core\">\n</ExecutionPhilosophy>\n<Constraints type=\"core\">\n</Constraints>\n")

	if docformat.ReorderDetected(deployed, source) {
		t.Error("ReorderDetected: want false — source adds a section but common slots retain their relative order")
	}
}

func TestReorderDetected_SourceRemovesSection_CommonSlotsUnchanged_ReturnsFalse(t *testing.T) {
	// Source removes ExecutionPhilosophy. Common slots (Identity, Constraints) retain
	// their relative order. A pure removal must not trigger a reorder.
	deployed := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<ExecutionPhilosophy type=\"core\">\n</ExecutionPhilosophy>\n<Constraints type=\"core\">\n</Constraints>\n")
	source := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<Constraints type=\"core\">\n</Constraints>\n")

	if docformat.ReorderDetected(deployed, source) {
		t.Error("ReorderDetected: want false — source removes a section but common slots retain their relative order")
	}
}

func TestReorderDetected_SourceSwapsCommonSections_ReturnsTrue(t *testing.T) {
	// Source swaps Identity and Constraints. The relative order of common slots differs.
	deployed := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<Constraints type=\"core\">\n</Constraints>\n")
	source := parseInlineDoc(t, "<Constraints type=\"core\">\n</Constraints>\n<Identity type=\"core\">\n</Identity>\n")

	if !docformat.ReorderDetected(deployed, source) {
		t.Error("ReorderDetected: want true — source swaps Identity and Constraints")
	}
}

func TestReorderDetected_BothDocumentsEmpty_ReturnsFalse(t *testing.T) {
	deployed := parseInlineDoc(t, "No boundary tags here.\n")
	source := parseInlineDoc(t, "No boundary tags here either.\n")

	if docformat.ReorderDetected(deployed, source) {
		t.Error("ReorderDetected: want false for documents with no structural slots")
	}
}

func TestReorderDetected_TopLevelDeployedParticipatesInComparison_Swapped_ReturnsTrue(t *testing.T) {
	// AD-2: top-level deployed regions participate in the comparison.
	// deployed has: <Identity type="core"> then <CommunicationProtocol type="managed">.
	// source has:   <CommunicationProtocol type="managed"> then <Identity type="core">.
	// The relative order of common slots is swapped → reorder.
	deployed := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<CommunicationProtocol type=\"managed\">\n</CommunicationProtocol>\n")
	source := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\n</CommunicationProtocol>\n<Identity type=\"core\">\n</Identity>\n")

	if !docformat.ReorderDetected(deployed, source) {
		t.Error("ReorderDetected: want true — top-level deployed region moved before the section (AD-2: deployed regions participate)")
	}
}

func TestReorderDetected_TopLevelDeployedAndSectionAddition_NoReorder(t *testing.T) {
	// deployed has: <Identity type="core">, <CommunicationProtocol type="managed">.
	// source has:   <Identity type="core">, <NewSection type="core">, <CommunicationProtocol type="managed">.
	// NewSection is an addition; common slots remain in the same order.
	deployed := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<CommunicationProtocol type=\"managed\">\n</CommunicationProtocol>\n")
	source := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n<NewSection type=\"core\">\n</NewSection>\n<CommunicationProtocol type=\"managed\">\n</CommunicationProtocol>\n")

	if docformat.ReorderDetected(deployed, source) {
		t.Error("ReorderDetected: want false — NewSection is an addition; common slots Identity and CommunicationProtocol remain in the same order")
	}
}

func TestReorderDetected_NestedDeployedRegionDoesNotParticipate(t *testing.T) {
	// Nested deployed regions are content of their enclosing section, not top-level structural
	// slots. Moving them relative to one another within a section must not trigger a reorder.
	//
	// Both documents have the same top-level structure: Identity, then Constraints.
	// The nested deployed regions inside each section are different (swapped contents),
	// but that is irrelevant — only the top-level structural order matters.
	const deployedSrc = "<Identity type=\"core\">\n<NestedA type=\"managed\">\n</NestedA>\n</Identity>\n<Constraints type=\"core\">\n<NestedB type=\"managed\">\n</NestedB>\n</Constraints>\n"
	const sourceSrc = "<Identity type=\"core\">\n<NestedB type=\"managed\">\n</NestedB>\n</Identity>\n<Constraints type=\"core\">\n<NestedA type=\"managed\">\n</NestedA>\n</Constraints>\n"

	deployed := parseInlineDoc(t, deployedSrc)
	source := parseInlineDoc(t, sourceSrc)

	if docformat.ReorderDetected(deployed, source) {
		t.Error("ReorderDetected: want false — nested deployed regions do not participate; top-level slots Identity and Constraints are in the same order in both documents")
	}
}
