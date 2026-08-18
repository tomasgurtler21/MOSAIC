package catalog_test

// workflow_source_type_test.go covers the Stage 4 requirement that Catalog/ workflow
// source files keep type="core" and that WorkflowSection extraction still resolves them
// correctly after the assembler starts rewriting type="core" to type="managed" in output.
//
// These are regression guards: Stage 4 implementation must not inadvertently modify
// catalog source files or change how WorkflowSection extracts blocks from them.
//
// Coverage:
//   - WorkflowSection extracted bytes begin with an opening <Workflow type="core" ...> tag.
//   - WorkflowSection extracted bytes do NOT begin with type="managed" — the source is
//     unchanged; only the assembler output is rewritten.
//   - WorkflowSection returns the workflow id in the name attribute of the extracted block.
//   - WorkflowSection returns the complete block including the closing </Workflow> tag.
//   - WorkflowSection still resolves a workflow by id after Stage 4.
//   - The raw source bytes on disk are unaffected: the workflow source file still contains
//     type="core" on the <Workflow> boundary tag after a catalog load.

import (
	"bytes"
	"testing"
)

// ---------------------------------------------------------------------------
// WorkflowSection extraction — type="core" preserved in source
// ---------------------------------------------------------------------------

// TestWorkflowSection_Stage4_ExtractedBytesHaveTypeCoreOnOpeningTag verifies that the bytes
// returned by WorkflowSection begin with a <Workflow type="core" ...> opening tag. The
// assembler rewrites the type to "managed" in its output; it must not affect what
// WorkflowSection returns from the catalog source file.
func TestWorkflowSection_Stage4_ExtractedBytesHaveTypeCoreOnOpeningTag(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)

	extracted, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}

	// The extracted block must start with the opening tag carrying type="core".
	if !bytes.HasPrefix(extracted, []byte("<Workflow type=\"core\"")) {
		t.Errorf("WorkflowSection returned bytes not starting with <Workflow type=\"core\"; "+
			"catalog source must keep type=\"core\" — only the assembler output is retyped to type=\"managed\".\n"+
			"first 120 bytes: %q", truncateBytes(extracted, 120))
	}
}

// TestWorkflowSection_Stage4_ExtractedBytesDoNotHaveTypeManagedOnOpeningTag verifies that
// WorkflowSection does not return type="managed" on the Workflow opening tag. The managed
// type is applied by the assembler during injection; it must not appear in the source.
func TestWorkflowSection_Stage4_ExtractedBytesDoNotHaveTypeManagedOnOpeningTag(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)

	extracted, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}

	// Specifically check the opening tag line — not the whole block, since the body might
	// coincidentally contain the word "managed".
	firstLine := firstLineOf(extracted)
	if bytes.Contains(firstLine, []byte("type=\"managed\"")) || bytes.Contains(firstLine, []byte("type='managed'")) {
		t.Errorf("WorkflowSection returned opening tag with type=\"managed\"; "+
			"catalog source files must keep type=\"core\".\nopening line: %q", firstLine)
	}
}

// TestWorkflowSection_Stage4_NameAttributeMatchesID verifies that the name attribute in
// the extracted opening tag matches the workflow ID used for the lookup. This is the existing
// WorkflowSection contract and must remain intact after Stage 4.
func TestWorkflowSection_Stage4_NameAttributeMatchesID(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)

	extracted, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}

	if !bytes.Contains(firstLineOf(extracted), []byte("name=\"sample-workflow\"")) {
		t.Errorf("WorkflowSection extracted block does not carry name=\"sample-workflow\" on opening tag.\n"+
			"opening line: %q", firstLineOf(extracted))
	}
}

// TestWorkflowSection_Stage4_ExtractedBytesContainClosingTag verifies that the extracted
// bytes include the closing </Workflow> tag. The Stage 4 assembler retyping must not affect
// what WorkflowSection extracts.
func TestWorkflowSection_Stage4_ExtractedBytesContainClosingTag(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)

	extracted, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\"): %v", err)
	}

	if !bytes.Contains(extracted, []byte("</Workflow>")) {
		t.Errorf("WorkflowSection extracted bytes do not contain </Workflow> closing tag.\nextracted: %q", extracted)
	}
}

// TestWorkflowSection_Stage4_StillResolvesWorkflowByID verifies that WorkflowSection can
// still resolve a workflow by its ID after Stage 4 changes. This is a basic reachability
// regression guard.
func TestWorkflowSection_Stage4_StillResolvesWorkflowByID(t *testing.T) {
	cat, _ := makeWorkflowWithSectionFixture(t)

	extracted, err := cat.WorkflowSection("sample-workflow")
	if err != nil {
		t.Fatalf("WorkflowSection(\"sample-workflow\") returned error after Stage 4: %v", err)
	}
	if len(extracted) == 0 {
		t.Error("WorkflowSection(\"sample-workflow\") returned empty bytes; want non-empty block")
	}
}

// TestWorkflowSection_Stage4_SourceFileBytesHaveTypeCoreAfterLoad verifies that the raw
// bytes of the workflow source file still contain type="core" after a catalog load. Stage 4
// must not modify catalog source files on disk — only the assembler output is affected.
func TestWorkflowSection_Stage4_SourceFileBytesHaveTypeCoreAfterLoad(t *testing.T) {
	cat, rawSource := makeWorkflowWithSectionFixture(t)

	// Load the catalog again to get the source path. We use the raw source bytes from the
	// fixture builder, which represent what was written to disk, to confirm no mutation.
	if !bytes.Contains(rawSource, []byte("type=\"core\"")) {
		t.Fatal("fixture source bytes do not contain type=\"core\" — test setup error")
	}

	// Re-read via the catalog's ReadSource to confirm on-disk bytes are unchanged.
	wf, ok := cat.Workflow("sample-workflow")
	if !ok {
		t.Fatal("catalog.Workflow(\"sample-workflow\"): not found after load")
	}
	onDisk, err := cat.ReadSource(wf.SourcePath)
	if err != nil {
		t.Fatalf("catalog.ReadSource(%q): %v", wf.SourcePath, err)
	}

	if !bytes.Contains(onDisk, []byte("type=\"core\"")) {
		t.Errorf("catalog source file at %q does not contain type=\"core\" after load; "+
			"Stage 4 must not modify catalog source files on disk.\non-disk bytes (first 200): %q",
			wf.SourcePath, truncateBytes(onDisk, 200))
	}
}

// TestWorkflowSection_Stage4_RealRepoCatalogSourcesHaveTypeCore verifies that every
// real workflow source file found under the real Catalog/Workflows/ tree declares
// type="core" on its <Workflow> block boundary tag. This is the on-disk invariant that
// must survive Stage 4 unchanged.
func TestWorkflowSection_Stage4_RealRepoCatalogSourcesHaveTypeCore(t *testing.T) {
	cat := loadRealCatalog(t)

	for _, wf := range cat.Workflows() {
		wf := wf // capture
		t.Run(wf.ID, func(t *testing.T) {
			extracted, err := cat.WorkflowSection(wf.ID)
			if err != nil {
				t.Fatalf("WorkflowSection(%q): %v", wf.ID, err)
			}
			firstLine := firstLineOf(extracted)
			if !bytes.Contains(firstLine, []byte("type=\"core\"")) {
				t.Errorf("workflow source %q: WorkflowSection opening tag does not carry type=\"core\".\n"+
					"opening line: %q", wf.ID, firstLine)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// firstLineOf returns the first line (up to and including the first \n, or the whole
// slice if no \n is present) from b.
func firstLineOf(b []byte) []byte {
	if idx := bytes.IndexByte(b, '\n'); idx >= 0 {
		return b[:idx+1]
	}
	return b
}

// truncateBytes returns the first n bytes of b, or all of b if len(b) <= n.
func truncateBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}
