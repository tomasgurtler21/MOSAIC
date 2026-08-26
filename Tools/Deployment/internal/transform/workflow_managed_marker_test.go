package transform_test

// workflow_managed_marker_test.go covers the Stage 4 requirement that workflow blocks
// assembled into the AvailableWorkflows region carry type="managed" on their opening
// <Workflow ...> tags, with name, version, and body otherwise preserved.
//
// Coverage:
//   - A single workflow block assembled by Apply carries type="managed" on its opening tag.
//   - The name attribute value is preserved after the type rewrite.
//   - The version attribute value is preserved after the type rewrite.
//   - The block body (content between the opening and closing tags) is preserved verbatim.
//   - The closing </Workflow> tag is unaffected by the type rewrite.
//   - Multiple workflow blocks in one assembly each carry type="managed".
//   - Attribute order is preserved when type is not the first attribute.
//   - A CRLF-terminated opening tag has its CRLF preserved after the type rewrite.
//   - The source catalog block (the input to assembly) still declares type="core", confirming
//     the rewrite is applied only during assembly and does not mutate the caller's slice.
//
// All assertions are made through the public transform.Apply API and the AvailableWorkflows
// region in the output document; assembleWorkflowBlocks is an unexported function.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// Workflow source blocks for managed marker tests.
// These represent catalog-sourced blocks (type="core") that the assembler must rewrite.
const quickFixBlockCore = `<Workflow type="core" name="quick-fix" version="3.0">
## Quick Fix Workflow

**Use when:** Small, well-scoped changes.

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner | TRUE |

</Workflow>
`

const greenfieldTDDBlockCore = `<Workflow type="core" name="greenfield-tdd" version="3.3">
## Greenfield TDD Workflow

**Use when:** Building a new component from scratch.

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner-tdd-soft | TRUE |

</Workflow>
`

// crlfWorkflowBlock is a catalog-sourced block using CRLF line endings.
// The opening tag must keep CRLF after the type rewrite.
const crlfWorkflowBlock = "<Workflow type=\"core\" name=\"crlf-workflow\" version=\"1.0\">\r\nBody line.\r\n</Workflow>\r\n"

// attrOrderBlock is a catalog-sourced block where the type attribute is not the first.
// Attribute order must be preserved in the output.
const attrOrderBlock = "<Workflow name=\"attr-order\" version=\"2.0\" type=\"core\">\nBody.\n</Workflow>\n"

// orchestratorForManagedMarker is a minimal orchestrator source document.
const orchestratorForManagedMarker = `---
version: 6.0.0
name: orchestrator
description: Orchestrator for managed marker tests
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: managed marker test
required_skills: []
---

<Identity type="core">
# Orchestrator

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`

// availableWorkflowsContent returns the Content() bytes of the AvailableWorkflows region
// in the Apply result. Fatally fails if the region is absent.
func availableWorkflowsContent(t *testing.T, result transform.Result) []byte {
	t.Helper()
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse Apply output: %v", err)
	}
	node, ok := outDoc.Body().Deployed("AvailableWorkflows")
	if !ok {
		t.Fatal("AvailableWorkflows region absent from Apply output")
	}
	return node.Content()
}

// applyWithWorkflows runs transform.Apply with the given workflow blocks and returns the result.
func applyWithWorkflows(t *testing.T, workflows []transform.WorkflowBlock) transform.Result {
	t.Helper()
	req := transform.Request{
		Source:    []byte(orchestratorForManagedMarker),
		Kind:      domain.ArtifactAgent,
		Key:       "orchestrator",
		Module:    newFixtureModule(t),
		Model:     fixtureModel(),
		Scope:     domain.ScopeProject,
		Workflows: workflows,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	return result
}

// ---------------------------------------------------------------------------
// type="managed" on assembled workflow blocks
// ---------------------------------------------------------------------------

func TestWorkflowManagedMarker_SingleBlock_OpeningTagCarriesTypeManaged(t *testing.T) {
	// After Stage 4, a single workflow block assembled into AvailableWorkflows must carry
	// type="managed" on its opening <Workflow ...> tag. Currently the source block has
	// type="core" and the assembler emits it verbatim — this test is RED until Stage 4.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	if !bytes.Contains(content, []byte(`type="managed"`)) {
		t.Errorf("AvailableWorkflows content does not contain type=\"managed\" on the Workflow block; "+
			"after Stage 4, assembled blocks must carry type=\"managed\".\ncontent: %q", content)
	}
}

func TestWorkflowManagedMarker_SingleBlock_NameAttributePreserved(t *testing.T) {
	// Regression guard: the name attribute value must survive the type rewrite byte-for-byte.
	// This test passes pre-implementation because the current assembler emits blocks verbatim
	// (name is preserved trivially). Its value activates as a regression guard once the type
	// rewrite is in place — it will catch any implementation that accidentally drops or alters
	// the name attribute.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	if !bytes.Contains(content, []byte(`name="quick-fix"`)) {
		t.Errorf("AvailableWorkflows content is missing name=\"quick-fix\" after type rewrite.\ncontent: %q", content)
	}
}

func TestWorkflowManagedMarker_SingleBlock_VersionAttributePreserved(t *testing.T) {
	// Regression guard: the version attribute value must survive the type rewrite byte-for-byte.
	// This test passes pre-implementation because the current assembler emits blocks verbatim
	// (version is preserved trivially). Its value activates once the type rewrite is in place.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	if !bytes.Contains(content, []byte(`version="3.0"`)) {
		t.Errorf("AvailableWorkflows content is missing version=\"3.0\" after type rewrite.\ncontent: %q", content)
	}
}

func TestWorkflowManagedMarker_SingleBlock_OpeningTagComplete(t *testing.T) {
	// The complete retyped opening tag (type=managed, name, version) must appear in output.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	want := `<Workflow type="managed" name="quick-fix" version="3.0">`
	if !bytes.Contains(content, []byte(want)) {
		t.Errorf("AvailableWorkflows content is missing complete retyped opening tag %q.\ncontent: %q",
			want, content)
	}
}

func TestWorkflowManagedMarker_SingleBlock_SourceBlockUnmutated(t *testing.T) {
	// Regression guard: the type rewrite must happen during assembly only; the input
	// WorkflowBlock.Block slice must remain byte-identical to what was passed in.
	// This test passes pre-implementation because the verbatim assembler never modifies
	// the caller's data. Its value activates once the type rewrite is in place — it will
	// catch any implementation that rewrites in-place rather than producing a new slice.
	block := []byte(quickFixBlockCore)
	blockCopy := make([]byte, len(block))
	copy(blockCopy, block)

	applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: block},
	})

	if !bytes.Equal(block, blockCopy) {
		t.Errorf("WorkflowBlock.Block was mutated by Apply; want byte-identical, "+
			"got different bytes after the call.\nbefore: %q\nafter:  %q", blockCopy, block)
	}
}

func TestWorkflowManagedMarker_SingleBlock_BodyPreservedVerbatim(t *testing.T) {
	// Regression guard: lines after the opening tag (the block body) must appear verbatim in
	// the output. This test passes pre-implementation because the verbatim assembler preserves
	// the body trivially. Its value activates once the type rewrite is in place — it will catch
	// any implementation that accidentally truncates or corrupts the body lines.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	bodyLines := []string{
		"## Quick Fix Workflow",
		"**Use when:** Small, well-scoped changes.",
		"| PLANNING | planner | TRUE |",
	}
	for _, line := range bodyLines {
		if !bytes.Contains(content, []byte(line)) {
			t.Errorf("AvailableWorkflows content is missing expected body line %q after type rewrite.\ncontent: %q",
				line, content)
		}
	}
}

func TestWorkflowManagedMarker_SingleBlock_ClosingTagUnchanged(t *testing.T) {
	// Regression guard: the closing </Workflow> tag must be emitted unchanged.
	// RetypeOpenTagLine must never alter closing tags. This test passes pre-implementation
	// because the verbatim assembler always preserves the closing tag. Its value activates
	// once the type rewrite is in place — it will catch any implementation that accidentally
	// modifies closing tags.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	if !bytes.Contains(content, []byte("</Workflow>")) {
		t.Errorf("AvailableWorkflows content is missing </Workflow> closing tag.\ncontent: %q", content)
	}
}

func TestWorkflowManagedMarker_SingleBlock_NoCoreTypeInOutput(t *testing.T) {
	// After the rewrite, type="core" must not appear on the Workflow opening tag in the
	// injection region. The source block has type="core"; it must be replaced, not preserved.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	// Looking for the Workflow block's opening tag specifically:
	if bytes.Contains(content, []byte("<Workflow type=\"core\"")) {
		t.Errorf("AvailableWorkflows content still contains type=\"core\" on a Workflow opening tag; "+
			"after Stage 4, the assembler must rewrite this to type=\"managed\".\ncontent: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Multiple workflow blocks — each carries type="managed"
// ---------------------------------------------------------------------------

func TestWorkflowManagedMarker_MultipleBlocks_AllCarryTypeManaged(t *testing.T) {
	// When multiple workflow blocks are assembled, each must carry type="managed".
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
		{ID: "greenfield-tdd", Block: []byte(greenfieldTDDBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	// Both Workflow blocks must carry type="managed". Since each occupies its own line,
	// count occurrences of the managed opening pattern.
	managedCount := bytes.Count(content, []byte("<Workflow type=\"managed\""))
	if managedCount < 2 {
		t.Errorf("expected at least 2 occurrences of <Workflow type=\"managed\" in AvailableWorkflows with two blocks, "+
			"got %d.\ncontent: %q", managedCount, content)
	}
	// No core-type Workflow blocks should remain.
	if bytes.Contains(content, []byte("<Workflow type=\"core\"")) {
		t.Errorf("AvailableWorkflows content still contains type=\"core\" on a Workflow opening tag after assembling two blocks.\ncontent: %q", content)
	}
}

func TestWorkflowManagedMarker_MultipleBlocks_NamesPreserved(t *testing.T) {
	// Regression guard: both name attribute values must survive the type rewrite in a
	// multi-block assembly. This test passes pre-implementation because the verbatim assembler
	// preserves names trivially. Its value activates once the type rewrite is in place.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "quick-fix", Block: []byte(quickFixBlockCore)},
		{ID: "greenfield-tdd", Block: []byte(greenfieldTDDBlockCore)},
	})
	content := availableWorkflowsContent(t, result)

	if !bytes.Contains(content, []byte(`name="quick-fix"`)) {
		t.Errorf("AvailableWorkflows content missing name=\"quick-fix\" after multi-block assembly.\ncontent: %q", content)
	}
	if !bytes.Contains(content, []byte(`name="greenfield-tdd"`)) {
		t.Errorf("AvailableWorkflows content missing name=\"greenfield-tdd\" after multi-block assembly.\ncontent: %q", content)
	}
}

// ---------------------------------------------------------------------------
// Attribute order and quoting style preservation
// ---------------------------------------------------------------------------

func TestWorkflowManagedMarker_AttributeOrder_PreservedWhenTypeNotFirst(t *testing.T) {
	// When type is not the first attribute, the attribute order must be preserved.
	// attrOrderBlock has: name first, then version, then type.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "attr-order", Block: []byte(attrOrderBlock)},
	})
	content := availableWorkflowsContent(t, result)

	// The retyped tag must preserve attribute order: name first, version second, type last.
	want := `<Workflow name="attr-order" version="2.0" type="managed">`
	if !bytes.Contains(content, []byte(want)) {
		t.Errorf("attribute order not preserved after type rewrite; want %q in content.\ncontent: %q",
			want, content)
	}
}

// ---------------------------------------------------------------------------
// Line-terminator preservation
// ---------------------------------------------------------------------------

func TestWorkflowManagedMarker_CRLFOpeningTag_TerminatorPreserved(t *testing.T) {
	// A CRLF-terminated opening tag must remain CRLF after the type rewrite.
	result := applyWithWorkflows(t, []transform.WorkflowBlock{
		{ID: "crlf-workflow", Block: []byte(crlfWorkflowBlock)},
	})
	content := availableWorkflowsContent(t, result)

	// The retyped opening line must end with CRLF.
	want := "<Workflow type=\"managed\" name=\"crlf-workflow\" version=\"1.0\">\r\n"
	if !bytes.Contains(content, []byte(want)) {
		t.Errorf("CRLF line terminator not preserved after type rewrite; want %q in content.\ncontent: %q",
			want, content)
	}
}
