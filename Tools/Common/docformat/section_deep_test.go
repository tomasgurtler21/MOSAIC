package docformat_test

// Tests for depth-first section lookup: Body.SectionDeep and Body.SectionsDeep.
//
// Coverage:
//   - SectionDeep finds a top-level section (same behaviour as Section).
//   - SectionDeep finds a section nested inside another section.
//   - SectionDeep finds a section at three levels of nesting depth.
//   - SectionDeep finds a section nested inside an injection.
//   - SectionDeep returns false for a name not present anywhere in the document.
//   - SectionDeep name matching is case-sensitive (no false positives on wrong case).
//   - SectionDeep returns the first match in document order when multiple nested
//     sections share the same name (top-level wins over same-named nested section).
//   - SectionDeep on a compound name (e.g. "Workflow:embedded") finds the node exactly.
//   - SectionsDeep includes all top-level sections.
//   - SectionsDeep includes sections nested inside other sections.
//   - SectionsDeep returns sections in pre-order (parent before children).
//   - SectionsDeep includes sections nested inside injections.
//   - SectionsDeep returns sections from all independent injection subtrees.
//   - SectionsDeep on a document with no sections returns an empty result.

import (
	"testing"

	"mosaic-common/docformat"
)

// parsedDeepFixture parses a boundary fixture and returns the Document.
// It reuses parsedBoundaryFixture defined in boundary_test.go (same package).

// --- SectionDeep: finds top-level sections ---

func TestBody_SectionDeep_TopLevelSection_Found(t *testing.T) {
	// SectionDeep must find a section that is at the top level of the body,
	// behaving like Section() for sections that are not nested.
	doc := parsedBoundaryFixture(t, "simple-section.md")

	node, ok := doc.Body().SectionDeep("Identity")

	if !ok {
		t.Fatal("SectionDeep(\"Identity\") returned false; top-level sections must be found")
	}
	if node == nil {
		t.Fatal("SectionDeep(\"Identity\") returned nil node with ok == true")
	}
	if node.Name() != "Identity" {
		t.Errorf("Name: want %q, got %q", "Identity", node.Name())
	}
}

func TestBody_SectionDeep_TopLevelSection_KindIsSection(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().SectionDeep("Identity")
	if !ok {
		t.Fatal("SectionDeep(\"Identity\") not found")
	}

	if node.Kind() != docformat.NodeSection {
		t.Errorf("Kind: want NodeSection, got %q", node.Kind())
	}
}

// --- SectionDeep: finds nested sections ---

func TestBody_SectionDeep_SectionNestedInsideSection_Found(t *testing.T) {
	// A section directly nested inside another section must be found by SectionDeep.
	// Section() would not find it because it only checks top-level items.
	doc := parsedBoundaryFixture(t, "nested-sections.md")

	node, ok := doc.Body().SectionDeep("Inner")

	if !ok {
		t.Fatal("SectionDeep(\"Inner\") returned false; nested sections must be found at any depth")
	}
	if node.Name() != "Inner" {
		t.Errorf("Name: want %q, got %q", "Inner", node.Name())
	}
}

func TestBody_SectionDeep_OuterSection_StillFound_WhenInnerSectionPresent(t *testing.T) {
	// The outer (parent) section must also be found by SectionDeep when searching for it.
	doc := parsedBoundaryFixture(t, "nested-sections.md")

	node, ok := doc.Body().SectionDeep("Outer")

	if !ok {
		t.Fatal("SectionDeep(\"Outer\") returned false; parent sections must be found")
	}
	if node.Name() != "Outer" {
		t.Errorf("Name: want %q, got %q", "Outer", node.Name())
	}
}

func TestBody_SectionDeep_SectionNestedInsideSection_TopLevelSectionNotFound_ByNestedName(t *testing.T) {
	// Searching for the nested section name must not accidentally return the outer section.
	doc := parsedBoundaryFixture(t, "nested-sections.md")

	node, ok := doc.Body().SectionDeep("Inner")
	if !ok {
		t.Fatal("SectionDeep(\"Inner\") not found")
	}

	// The found node must be the inner section, not the outer one.
	if node.Name() == "Outer" {
		t.Error("SectionDeep(\"Inner\") returned the outer section instead of the inner one")
	}
}

func TestBody_SectionDeep_SectionAtDepthThree_Found(t *testing.T) {
	// A section at nesting depth three (grandchild of a top-level section) must be found.
	doc := parsedBoundaryFixture(t, "deep-nested-sections.md")

	node, ok := doc.Body().SectionDeep("Level3")

	if !ok {
		t.Fatal("SectionDeep(\"Level3\") returned false; sections at depth three must be found")
	}
	if node.Name() != "Level3" {
		t.Errorf("Name: want %q, got %q", "Level3", node.Name())
	}
}

func TestBody_SectionDeep_SectionAtDepthTwo_Found(t *testing.T) {
	doc := parsedBoundaryFixture(t, "deep-nested-sections.md")

	node, ok := doc.Body().SectionDeep("Level2")

	if !ok {
		t.Fatal("SectionDeep(\"Level2\") returned false")
	}
	if node.Name() != "Level2" {
		t.Errorf("Name: want %q, got %q", "Level2", node.Name())
	}
}

func TestBody_SectionDeep_SiblingTopLevelSection_Found(t *testing.T) {
	// A top-level sibling section that appears after a deeply-nested group must still be found.
	doc := parsedBoundaryFixture(t, "deep-nested-sections.md")

	node, ok := doc.Body().SectionDeep("Sibling")

	if !ok {
		t.Fatal("SectionDeep(\"Sibling\") returned false; top-level sibling after nested group must be found")
	}
	if node.Name() != "Sibling" {
		t.Errorf("Name: want %q, got %q", "Sibling", node.Name())
	}
}

// --- SectionDeep: finds sections inside injections ---

func TestBody_SectionDeep_SectionInsideInjection_Found(t *testing.T) {
	// A section nested inside an injection must be found by SectionDeep.
	// This is the primary use case for the depth-first accessor: workflow sections
	// embedded inside injection slots of an orchestrator agent file.
	doc := parsedBoundaryFixture(t, "section-inside-injection.md")

	node, ok := doc.Body().SectionDeep("Workflow:embedded")

	if !ok {
		t.Fatal("SectionDeep(\"Workflow:embedded\") returned false; sections inside injections must be found")
	}
	if node.Name() != "Workflow:embedded" {
		t.Errorf("Name: want %q, got %q", "Workflow:embedded", node.Name())
	}
}

func TestBody_SectionDeep_SectionInsideInjection_OuterShellSection_AlsoFound(t *testing.T) {
	// The outer section that contains the injection must also be found by SectionDeep.
	doc := parsedBoundaryFixture(t, "section-inside-injection.md")

	node, ok := doc.Body().SectionDeep("Shell")

	if !ok {
		t.Fatal("SectionDeep(\"Shell\") returned false")
	}
	if node.Name() != "Shell" {
		t.Errorf("Name: want %q, got %q", "Shell", node.Name())
	}
}

// --- SectionDeep: absent name ---

func TestBody_SectionDeep_AbsentName_ReturnsFalse(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")

	_, ok := doc.Body().SectionDeep("Capabilities")

	if ok {
		t.Error("SectionDeep(\"Capabilities\") returned true for a name not present anywhere in the document")
	}
}

func TestBody_SectionDeep_AbsentName_InNestedDocument_ReturnsFalse(t *testing.T) {
	// Absent name in a deeply-nested document must also return false.
	doc := parsedBoundaryFixture(t, "deep-nested-sections.md")

	_, ok := doc.Body().SectionDeep("Constraints")

	if ok {
		t.Error("SectionDeep(\"Constraints\") returned true for a name not present in the document")
	}
}

// --- SectionDeep: case-sensitive matching ---

func TestBody_SectionDeep_NameMatchIsCaseSensitive(t *testing.T) {
	// SectionDeep must not match a section name that differs only in case.
	doc := parsedBoundaryFixture(t, "nested-sections.md")

	_, ok := doc.Body().SectionDeep("inner")

	if ok {
		t.Error("SectionDeep(\"inner\") returned true; section name matching must be case-sensitive")
	}
}

// --- SectionsDeep: returns all sections at any depth ---

func TestBody_SectionsDeep_TopLevelSections_Included(t *testing.T) {
	// SectionsDeep must include all top-level sections, like Sections() does.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	sections := doc.Body().SectionsDeep()

	if len(sections) < 2 {
		t.Fatalf("SectionsDeep: want at least 2 sections, got %d", len(sections))
	}
	// Verify both top-level sections are present.
	names := make(map[string]bool, len(sections))
	for _, s := range sections {
		names[s.Name()] = true
	}
	if !names["Identity"] {
		t.Error("SectionsDeep: top-level section \"Identity\" not included")
	}
	if !names["CommunicationProtocol"] {
		t.Error("SectionsDeep: top-level section \"CommunicationProtocol\" not included")
	}
}

func TestBody_SectionsDeep_NestedSection_Included(t *testing.T) {
	// SectionsDeep must include sections nested inside other sections.
	doc := parsedBoundaryFixture(t, "nested-sections.md")

	sections := doc.Body().SectionsDeep()

	if len(sections) != 2 {
		t.Fatalf("SectionsDeep: want 2 sections (Outer + Inner), got %d", len(sections))
	}
	names := make(map[string]bool, len(sections))
	for _, s := range sections {
		names[s.Name()] = true
	}
	if !names["Outer"] {
		t.Error("SectionsDeep: outer section not included")
	}
	if !names["Inner"] {
		t.Error("SectionsDeep: nested inner section not included")
	}
}

func TestBody_SectionsDeep_ReturnsInPreOrder_ParentBeforeChildren(t *testing.T) {
	// SectionsDeep must visit the parent section before its nested children.
	// Pre-order traversal: parent is listed before any of its descendants.
	doc := parsedBoundaryFixture(t, "nested-sections.md")

	sections := doc.Body().SectionsDeep()

	if len(sections) != 2 {
		t.Fatalf("SectionsDeep: want 2 sections, got %d", len(sections))
	}
	if sections[0].Name() != "Outer" {
		t.Errorf("sections[0]: want %q (parent before child), got %q", "Outer", sections[0].Name())
	}
	if sections[1].Name() != "Inner" {
		t.Errorf("sections[1]: want %q (child after parent), got %q", "Inner", sections[1].Name())
	}
}

func TestBody_SectionsDeep_ThreeLevels_ReturnsInPreOrder(t *testing.T) {
	// With three levels of nesting, pre-order must be Level1, Level2, Level3, then Sibling.
	doc := parsedBoundaryFixture(t, "deep-nested-sections.md")

	sections := doc.Body().SectionsDeep()

	if len(sections) != 4 {
		t.Fatalf("SectionsDeep: want 4 sections, got %d (names: %v)", len(sections), sectionNames(sections))
	}
	want := []string{"Level1", "Level2", "Level3", "Sibling"}
	for i, s := range sections {
		if s.Name() != want[i] {
			t.Errorf("sections[%d]: want %q, got %q", i, want[i], s.Name())
		}
	}
}

func TestBody_SectionsDeep_SectionInsideInjection_Included(t *testing.T) {
	// SectionsDeep must include sections that are nested inside injection nodes.
	// The injection node itself is not a section, but its child sections must appear.
	doc := parsedBoundaryFixture(t, "section-inside-injection.md")

	sections := doc.Body().SectionsDeep()

	// The document has: Shell (top-level), Workflow:embedded (inside injection inside Shell).
	if len(sections) != 2 {
		t.Fatalf("SectionsDeep: want 2 sections, got %d (names: %v)", len(sections), sectionNames(sections))
	}
	names := make(map[string]bool, len(sections))
	for _, s := range sections {
		names[s.Name()] = true
	}
	if !names["Shell"] {
		t.Error("SectionsDeep: outer \"Shell\" section not included")
	}
	if !names["Workflow:embedded"] {
		t.Error("SectionsDeep: \"Workflow:embedded\" section inside injection not included")
	}
}

func TestBody_SectionsDeep_SectionInsideInjection_PreOrder(t *testing.T) {
	// Shell (outer) must appear before Workflow:embedded (nested inside injection inside Shell).
	doc := parsedBoundaryFixture(t, "section-inside-injection.md")

	sections := doc.Body().SectionsDeep()

	if len(sections) != 2 {
		t.Fatalf("SectionsDeep: want 2 sections, got %d", len(sections))
	}
	if sections[0].Name() != "Shell" {
		t.Errorf("sections[0]: want %q, got %q", "Shell", sections[0].Name())
	}
	if sections[1].Name() != "Workflow:embedded" {
		t.Errorf("sections[1]: want %q, got %q", "Workflow:embedded", sections[1].Name())
	}
}

func TestBody_SectionsDeep_NoSections_ReturnsEmpty(t *testing.T) {
	// A document with no section boundary tags must return an empty slice from SectionsDeep.
	doc, err := docformat.Parse([]byte("---\nname: no-sections\n---\nPlain body text with no boundary tags.\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	sections := doc.Body().SectionsDeep()

	if len(sections) != 0 {
		t.Errorf("SectionsDeep on a document with no sections: want 0, got %d", len(sections))
	}
}

// --- SectionDeep: duplicate names at different depths ---

func TestBody_SectionDeep_DuplicateName_ReturnsFirstMatchInDocumentOrder(t *testing.T) {
	// When two sections share the same name — one at top level, one nested inside another
	// section — SectionDeep must return the top-level one (first in document order).
	// This verifies the ordering guarantee: "first section node" means the shallowest
	// occurrence encountered during pre-order traversal of the body's top-level items.
	doc := parsedBoundaryFixture(t, "duplicate-name-sections.md")

	node, ok := doc.Body().SectionDeep("Duplicate")

	if !ok {
		t.Fatal("SectionDeep(\"Duplicate\") returned false; at least one match exists at top level")
	}
	if node.Name() != "Duplicate" {
		t.Errorf("Name: want %q, got %q", "Duplicate", node.Name())
	}
	// The top-level section has no parent; the nested one has Container as its parent.
	// A nil Parent confirms the top-level (first in document order) section was returned.
	if node.Parent() != nil {
		t.Errorf("Parent: want nil (top-level section), got non-nil parent %q — nested section was returned instead of top-level", node.Parent().Name())
	}
}

// --- SectionsDeep: multiple independent injection subtrees ---

func TestBody_SectionsDeep_MultipleIndependentInjections_AllSectionsIncluded(t *testing.T) {
	// A document with two separate injection slots at different positions, each hosting a
	// section, must have all injection-hosted sections appear in SectionsDeep. This exercises
	// the traversal across multiple injection subtrees: a bug in visitSections that stops
	// after the first injection subtree would only be caught by this test.
	doc := parsedBoundaryFixture(t, "multiple-independent-injections.md")

	sections := doc.Body().SectionsDeep()

	// Expected pre-order: First, Workflow:first, Second, Workflow:second.
	if len(sections) != 4 {
		t.Fatalf("SectionsDeep: want 4 sections, got %d (names: %v)", len(sections), sectionNames(sections))
	}
	names := make(map[string]bool, len(sections))
	for _, s := range sections {
		names[s.Name()] = true
	}
	for _, want := range []string{"First", "Workflow:first", "Second", "Workflow:second"} {
		if !names[want] {
			t.Errorf("SectionsDeep: section %q not included in result (got: %v)", want, sectionNames(sections))
		}
	}

	// index returns the position of the named section in sections, or -1.
	index := func(name string) int {
		for i, s := range sections {
			if s.Name() == name {
				return i
			}
		}
		return -1
	}

	// Pre-order: each outer section must precede its injection-hosted child section.
	if index("First") >= index("Workflow:first") {
		t.Errorf("SectionsDeep: \"First\" (idx %d) must precede \"Workflow:first\" (idx %d)", index("First"), index("Workflow:first"))
	}
	if index("Second") >= index("Workflow:second") {
		t.Errorf("SectionsDeep: \"Second\" (idx %d) must precede \"Workflow:second\" (idx %d)", index("Second"), index("Workflow:second"))
	}
	// Both injection subtrees covered: First must appear before Second.
	if index("First") >= index("Second") {
		t.Errorf("SectionsDeep: \"First\" (idx %d) must precede \"Second\" (idx %d)", index("First"), index("Second"))
	}
}

// --- helpers ---

// sectionNames returns the names of the given nodes for use in test failure messages.
func sectionNames(nodes []*docformat.Node) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name()
	}
	return names
}
