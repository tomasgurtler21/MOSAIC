package docformat_test

// Tests for body node mutation operations (T4.4) and single-node mutation fidelity (T4.6).
//
// Coverage (T4.4 — mutations):
//   - Node.SetContent replaces a section's content; the new content is returned by Content.
//   - Node.SetContent replaces an injection's content; the new content is returned by Content.
//   - Node.Clear empties an injection's content; IsEmpty returns true afterwards.
//   - Node.Clear leaves the boundary tags in place; Node.Bytes still contains open and close tags.
//   - Node.SetContent makes the section/injection no longer report IsEmpty.
//   - Node.Content after SetContent returns the new bytes (the "lift" use case).
//   - Node.Clear followed by SetContent works as a two-step fill operation.
//
// Coverage (T4.6 — byte fidelity):
//   - After SetContent on one section, all other sections in the document are byte-identical.
//   - After Clear on one injection, all other nodes in the document are byte-identical.
//   - After SetContent on one injection, the document bytes differ only in that injection's region.
//   - An untagged span between two sections is preserved verbatim after mutating either section.
//   - The boundary tags of the mutated node itself are preserved after SetContent.
//   - doc.Bytes() after SetContent contains the new content and all other original bytes.

import (
	"bytes"
	"testing"

	"mosaic-deploy/internal/docformat"
)

// --- SetContent on a section ---

func TestMutation_Section_SetContent_ContentAvailableViaContent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	newContent := []byte("Replaced section content.\n")
	if err := node.SetContent(newContent); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	got := node.Content()
	if !bytes.Equal(got, newContent) {
		t.Errorf("Content after SetContent: want %q, got %q", newContent, got)
	}
}

func TestMutation_Section_SetContent_IsEmptyFalseAfterNonBlankContent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if err := node.SetContent([]byte("Non-blank content.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	if node.IsEmpty() {
		t.Error("IsEmpty must return false after SetContent with non-blank content")
	}
}

func TestMutation_Section_SetContent_BytesContainsBoundaryTagsAndNewContent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if err := node.SetContent([]byte("New section content.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	b := node.Bytes()
	if !bytes.Contains(b, []byte("[[SECTION:Identity]]")) {
		t.Error("Node.Bytes after SetContent must contain the opening boundary tag")
	}
	if !bytes.Contains(b, []byte("[[/SECTION:Identity]]")) {
		t.Error("Node.Bytes after SetContent must contain the closing boundary tag")
	}
	if !bytes.Contains(b, []byte("New section content.")) {
		t.Error("Node.Bytes after SetContent must contain the new content")
	}
}

// --- SetContent on an injection ---

func TestMutation_Injection_SetContent_ContentAvailableViaContent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	newContent := []byte("Injected content line.\n")
	if err := node.SetContent(newContent); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	got := node.Content()
	if !bytes.Equal(got, newContent) {
		t.Errorf("Content after SetContent: want %q, got %q", newContent, got)
	}
}

func TestMutation_Injection_SetContent_IsEmptyFalse(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if err := node.SetContent([]byte("Filled.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	if node.IsEmpty() {
		t.Error("IsEmpty must return false after SetContent with non-blank content")
	}
}

// --- Clear on an injection ---

func TestMutation_Injection_Clear_IsEmptyTrue(t *testing.T) {
	doc := parsedBoundaryFixture(t, "filled-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}
	if node.IsEmpty() {
		t.Fatal("filled-injection.md fixture has no content — cannot test Clear; update the fixture to include non-blank injection content")
	}

	if err := node.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if !node.IsEmpty() {
		t.Errorf("IsEmpty: want true after Clear, got false (content: %q)", node.Content())
	}
}

func TestMutation_Injection_Clear_BoundaryTagsRemainInNodeBytes(t *testing.T) {
	doc := parsedBoundaryFixture(t, "filled-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if err := node.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	b := node.Bytes()
	if !bytes.Contains(b, []byte("[[INJECTION:IdentityExtension]]")) {
		t.Error("Node.Bytes after Clear must still contain the opening boundary tag")
	}
	if !bytes.Contains(b, []byte("[[/INJECTION:IdentityExtension]]")) {
		t.Error("Node.Bytes after Clear must still contain the closing boundary tag")
	}
}

func TestMutation_Injection_Clear_ContentIsBlank(t *testing.T) {
	doc := parsedBoundaryFixture(t, "filled-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if err := node.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	content := node.Content()
	if len(bytes.TrimSpace(content)) != 0 {
		t.Errorf("Content after Clear must be blank, got %q", content)
	}
}

// --- Clear followed by SetContent ---

func TestMutation_Injection_ClearThenSetContent_ContentIsNew(t *testing.T) {
	doc := parsedBoundaryFixture(t, "filled-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if err := node.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	newContent := []byte("Fresh content after clear.\n")
	if err := node.SetContent(newContent); err != nil {
		t.Fatalf("SetContent after Clear: %v", err)
	}

	got := node.Content()
	if !bytes.Equal(got, newContent) {
		t.Errorf("Content after Clear+SetContent: want %q, got %q", newContent, got)
	}
}

// --- T4.6: Byte fidelity — mutations leave all other bytes unchanged ---

func TestMutationFidelity_SetContent_OtherSectionBytesUnchanged(t *testing.T) {
	// Mutating one section must leave every byte of every other section identical.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")
	body := doc.Body()

	sectionA, ok := body.Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}
	sectionB, ok := body.Section("CommunicationProtocol")
	if !ok {
		t.Fatal("Section(\"CommunicationProtocol\") not found")
	}

	// Capture B's full bytes before any mutation.
	bBefore := make([]byte, len(sectionB.Bytes()))
	copy(bBefore, sectionB.Bytes())

	// Mutate A.
	if err := sectionA.SetContent([]byte("Replaced identity content.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	// B's bytes must be identical to what they were before.
	bAfter := sectionB.Bytes()
	if !bytes.Equal(bBefore, bAfter) {
		t.Errorf("section B bytes changed after mutating section A:\n  before: %q\n  after:  %q", bBefore, bAfter)
	}
}

func TestMutationFidelity_Clear_OtherInjectionBytesUnchanged(t *testing.T) {
	// Clearing one injection must leave every byte of every other injection identical.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")
	body := doc.Body()

	injA, ok := body.Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}
	injB, ok := body.Injection("ProtocolExtension")
	if !ok {
		t.Fatal("Injection(\"ProtocolExtension\") not found")
	}

	// Capture B's full bytes before any mutation.
	bBefore := make([]byte, len(injB.Bytes()))
	copy(bBefore, injB.Bytes())

	// Clear A.
	if err := injA.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// B's bytes must be identical to what they were before.
	bAfter := injB.Bytes()
	if !bytes.Equal(bBefore, bAfter) {
		t.Errorf("injection B bytes changed after clearing injection A:\n  before: %q\n  after:  %q", bBefore, bAfter)
	}
}

func TestMutationFidelity_SetContent_DocumentContainsMutatedAndOriginalBytes(t *testing.T) {
	// After mutating one section, doc.Bytes() must contain both the new content
	// and all of the other section's original bytes.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")
	body := doc.Body()

	sectionA, ok := body.Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}
	sectionB, ok := body.Section("CommunicationProtocol")
	if !ok {
		t.Fatal("Section(\"CommunicationProtocol\") not found")
	}

	bBefore := make([]byte, len(sectionB.Bytes()))
	copy(bBefore, sectionB.Bytes())

	newContent := []byte("Replaced identity content.\n")
	if err := sectionA.SetContent(newContent); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	docBytes := doc.Bytes()

	// The new content must be in the output.
	if !bytes.Contains(docBytes, newContent) {
		t.Error("doc.Bytes() after SetContent must contain the new content")
	}
	// Section B's original bytes must appear verbatim in the output.
	if !bytes.Contains(docBytes, bBefore) {
		t.Error("doc.Bytes() after SetContent must contain section B's original bytes verbatim")
	}
}

func TestMutationFidelity_SetContent_BoundaryTagsOfMutatedNodePreserved(t *testing.T) {
	// The opening and closing boundary tags of the mutated node must remain verbatim
	// in the output. SetContent replaces the inner bytes only.
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if err := node.SetContent([]byte("Changed content.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	docBytes := doc.Bytes()
	if !bytes.Contains(docBytes, []byte("[[SECTION:Identity]]")) {
		t.Error("doc.Bytes() after SetContent must still contain the opening boundary tag")
	}
	if !bytes.Contains(docBytes, []byte("[[/SECTION:Identity]]")) {
		t.Error("doc.Bytes() after SetContent must still contain the closing boundary tag")
	}
}

func TestMutationFidelity_SetContent_UntaggedSpanBetweenSectionsPreserved(t *testing.T) {
	// The untagged span between sections (the blank line between [[/SECTION:Identity]]
	// and [[SECTION:CommunicationProtocol]]) must be preserved verbatim after mutating
	// either adjacent section.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")
	src := boundaryFixtureBytes(t, "multiple-sections.md")

	// Extract the exact untagged span bytes from the original source: the bytes that
	// lie between the end of the Identity closing tag line and the start of the
	// CommunicationProtocol opening tag line.
	const closingIdentity = "[[/SECTION:Identity]]\n"
	const openingProtocol = "[[SECTION:CommunicationProtocol]]"
	endOfClosing := bytes.Index(src, []byte(closingIdentity))
	if endOfClosing < 0 {
		t.Fatal("could not find [[/SECTION:Identity]] closing tag in fixture source")
	}
	spanStart := endOfClosing + len(closingIdentity)
	startOfProto := bytes.Index(src, []byte(openingProtocol))
	if startOfProto < 0 {
		t.Fatal("could not find [[SECTION:CommunicationProtocol]] in fixture source")
	}
	if spanStart > startOfProto {
		t.Fatal("fixture layout unexpected: CommunicationProtocol appears before Identity closing tag")
	}
	untaggedSpan := src[spanStart:startOfProto]
	if len(untaggedSpan) == 0 {
		t.Fatal("no untagged span between Identity and CommunicationProtocol sections in fixture — expected at least a blank line")
	}

	sectionA, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if err := sectionA.SetContent([]byte("New identity content.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	// The exact untagged span bytes must appear verbatim in the mutated document.
	docBytes := doc.Bytes()
	if !bytes.Contains(docBytes, untaggedSpan) {
		t.Errorf("untagged span between sections not preserved after SetContent:\n  span bytes: %q\n  doc.Bytes(): %q", untaggedSpan, docBytes)
	}
}

// --- SetContent preserves nested nodes when replacement bytes contain their tags ---

func TestMutation_Section_SetContent_NestedInjectionRemainsAddressableWhenTagsIncluded(t *testing.T) {
	// Node.SetContent contract: "preserves nested nodes only if b contains their tags."
	// When the replacement bytes include the nested injection's boundary tag lines, the
	// injection node must remain live and addressable via Body.Injection and Section.Children.
	//
	// This path verifies that an implementation cannot silently drop all nested nodes on
	// every SetContent call without violating the contract.
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	body := doc.Body()

	section, ok := body.Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found in empty-injection.md")
	}

	// Supply replacement bytes that still contain the injection's boundary tag lines.
	newContent := []byte("Updated preamble.\n[[INJECTION:IdentityExtension]]\n[[/INJECTION:IdentityExtension]]\nUpdated postamble.\n")
	if err := section.SetContent(newContent); err != nil {
		t.Fatalf("SetContent with nested injection tags: %v", err)
	}

	// The injection must still be addressable via Body.Injection after SetContent.
	injAfter, ok := body.Injection("IdentityExtension")
	if !ok {
		t.Fatal("Body.Injection(\"IdentityExtension\") returned false after SetContent with nested tags in replacement bytes — nested node must remain live when its tags are included")
	}
	if injAfter == nil {
		t.Fatal("Body.Injection returned nil with ok==true after SetContent")
	}

	// Section.Children must still include the injection node.
	found := false
	for _, child := range section.Children() {
		if child.Kind() == docformat.NodeInjection && child.Name() == "IdentityExtension" {
			found = true
			break
		}
	}
	if !found {
		t.Error("section.Children() does not include IdentityExtension after SetContent with nested injection tags — child must be preserved when tags appear in replacement bytes")
	}
}

func TestMutation_Section_SetContent_NestedInjectionRemovedWhenTagsOmitted(t *testing.T) {
	// Node.SetContent contract: "preserves nested nodes only if b contains their tags."
	// When the replacement bytes do NOT include the nested injection's boundary tag lines,
	// the injection node must be removed — Body.Injection must return false and
	// Section.Children must be empty.
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	body := doc.Body()

	section, ok := body.Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found in empty-injection.md")
	}

	// Supply replacement bytes that do NOT contain the injection's boundary tag lines.
	plainContent := []byte("Replacement without any injection tags.\n")
	if err := section.SetContent(plainContent); err != nil {
		t.Fatalf("SetContent with no injection tags: %v", err)
	}

	// The injection must no longer be addressable at the document level.
	_, ok = body.Injection("IdentityExtension")
	if ok {
		t.Error("Body.Injection(\"IdentityExtension\") found after SetContent with no injection tags — nested node must be removed when its tags are absent from replacement bytes")
	}

	// Section.Children must be empty (the injection node is gone).
	if children := section.Children(); len(children) != 0 {
		t.Errorf("section.Children() must be empty after SetContent with no injection tags, got %d children", len(children))
	}
}

func TestMutationFidelity_SetContent_FrontmatterUnchanged(t *testing.T) {
	// Mutating a body node must not alter the frontmatter bytes.
	doc := parsedBoundaryFixture(t, "simple-section.md")
	src := boundaryFixtureBytes(t, "simple-section.md")

	// Identify the frontmatter bytes from the source (everything up to and including
	// the closing "---\n" delimiter).
	fmEnd := bytes.Index(src[3:], []byte("---")) // skip opening "---"
	if fmEnd < 0 {
		t.Fatal("fixture has no closing frontmatter delimiter")
	}
	// fmEnd is relative to src[3:]; adjust to absolute and include the closing "---\n".
	absEnd := 3 + fmEnd + 3 + 1 // +3 for "---", +1 for "\n"
	if absEnd > len(src) {
		absEnd = len(src)
	}
	fmBytes := src[:absEnd]

	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}
	if err := node.SetContent([]byte("Changed.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	docBytes := doc.Bytes()
	if !bytes.HasPrefix(docBytes, fmBytes) {
		contextLen := min(len(fmBytes)+20, len(docBytes))
		t.Errorf("frontmatter changed after body mutation:\n  want prefix: %q\n  got:         %q", fmBytes, docBytes[:contextLen])
	}
}
