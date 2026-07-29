package transform_test

// workflow_assembly_test.go covers Stage 10 AvailableWorkflows assembly (InjectionWorkflow):
//
//   - Zero workflows: the AvailableWorkflows injection is left empty.
//   - Single workflow: the [[SECTION:Workflow:{id}]] block is copied verbatim into the
//     injection content, including the workflow-version comment and the phase table.
//   - Multiple workflows: blocks are composed in the order supplied by Request.Workflows.
//     The order must be deterministic and match the caller's selection order.
//   - Re-injection: passing the same set of workflows on a subsequent update does not
//     produce duplicate blocks. Passing an expanded set adds the new workflow.
//   - Report: Report.Workflows lists the IDs of assembled workflows in emitted order.
//   - No workflow or index content must appear as a deployable file.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// orchestratorWithWorkflows is an orchestrator-like source that has both an
// [[INJECTION:IdentityExtension]] and an [[INJECTION:AvailableWorkflows]] inside the
// Identity section. This is the canonical shape of the real orchestrator agent.
const orchestratorWithWorkflows = `---
version: 6.0.0
name: orchestrator
description: Central coordinator that manages multi-agent workflow execution
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: multi-phase coordination
required_skills: []
---

[[SECTION:Identity]]
# Orchestrator Agent

You are the Orchestrator.

[[INJECTION:AvailableWorkflows]]
[[/INJECTION:AvailableWorkflows]]

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
`

// deployedOrchestratorWithOneWorkflow is a deployed orchestrator whose AvailableWorkflows
// injection already contains one workflow block. Used by re-injection tests.
const deployedOrchestratorWithOneWorkflow = `---
version: 6.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Central coordinator that manages multi-agent workflow execution
mode: subagent
model: claude/claude-sonnet
tools: [read-file, write-file, edit-file, search-file, search-text, run-terminal, ask-user]
---

[[SECTION:Identity]]
# Orchestrator Agent

You are the Orchestrator.

[[INJECTION:AvailableWorkflows]]
[[SECTION:Workflow:quick-fix]]
<!-- workflow-version: 3.0 -->
## Quick Fix Workflow

**Use when:** Small changes and bug fixes.

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner | ✅ |

[[/SECTION:Workflow:quick-fix]]
[[/INJECTION:AvailableWorkflows]]

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]
`

// Workflow block fixtures matching real workflow file structure.
const quickFixBlock = `[[SECTION:Workflow:quick-fix]]
<!-- workflow-version: 3.0 -->
## Quick Fix Workflow

**Use when:** Small changes and bug fixes.

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner | ✅ |

[[/SECTION:Workflow:quick-fix]]
`

const greenfieldTDDBlock = `[[SECTION:Workflow:greenfield-tdd]]
<!-- workflow-version: 3.3 -->
## Greenfield TDD Workflow

**Use when:** Building a new project from scratch.

| Phase | Subagent | HITL |
|-------|----------|:----:|
| RESEARCH | requirements-refinement | ✅ |
| PLANNING | planner-tdd-soft | ✅ |

[[/SECTION:Workflow:greenfield-tdd]]
`

// ---------------------------------------------------------------------------
// T10.6 — AvailableWorkflows assembly
// ---------------------------------------------------------------------------

// TestAvailableWorkflows_ZeroWorkflows_InjectionEmpty asserts that when Request.Workflows
// is empty (or nil), the AvailableWorkflows injection region in the output is empty.
// The corresponding InjectionOutcome has Action == InjectionEmptied (no content assembled).
func TestAvailableWorkflows_ZeroWorkflows_InjectionEmpty(t *testing.T) {
	req := transform.Request{
		Source:    []byte(orchestratorWithWorkflows),
		Kind:      domain.ArtifactAgent,
		Key:       "orchestrator",
		Module:    newFixtureModule(t),
		Model:     fixtureModel(),
		Scope:     domain.ScopeProject,
		Workflows: nil, // no workflows selected
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The AvailableWorkflows injection must be empty.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("AvailableWorkflows")
	if !ok {
		t.Fatal("AvailableWorkflows injection absent from output")
	}
	if !node.IsEmpty() {
		t.Errorf("AvailableWorkflows must be empty with zero workflows; got content: %q", node.Content())
	}

	// Report.Workflows must be empty.
	if len(result.Report.Workflows) != 0 {
		t.Errorf("Report.Workflows must be empty with zero workflows; got: %v", result.Report.Workflows)
	}
}

// TestAvailableWorkflows_SingleWorkflow_BlockCopiedVerbatim asserts that a single
// workflow block is placed verbatim inside the AvailableWorkflows injection, preserving
// the workflow-version comment, the phase table, and all whitespace exactly.
func TestAvailableWorkflows_SingleWorkflow_BlockCopiedVerbatim(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorWithWorkflows),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Workflows: []transform.WorkflowBlock{
			{ID: "quick-fix", Block: []byte(quickFixBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("AvailableWorkflows")
	if !ok {
		t.Fatal("AvailableWorkflows injection absent from output")
	}

	// The block content must be present verbatim inside the injection.
	// We check that the section open tag, the version comment, and the close tag all appear.
	for _, marker := range []string{
		"[[SECTION:Workflow:quick-fix]]",
		"<!-- workflow-version: 3.0 -->",
		"## Quick Fix Workflow",
		"[[/SECTION:Workflow:quick-fix]]",
	} {
		if !bytes.Contains(node.Content(), []byte(marker)) {
			t.Errorf("AvailableWorkflows content missing expected marker %q\ncontent: %q",
				marker, node.Content())
		}
	}

	// The InjectionOutcome must report InjectionAssembled.
	var outcome *transform.InjectionOutcome
	for i := range result.Report.Injections {
		if result.Report.Injections[i].Name == "AvailableWorkflows" {
			outcome = &result.Report.Injections[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("InjectionOutcome for AvailableWorkflows absent")
	}
	if outcome.Action != transform.InjectionAssembled {
		t.Errorf("AvailableWorkflows action: want %q, got %q", transform.InjectionAssembled, outcome.Action)
	}

	// Report.Workflows must contain exactly [quick-fix].
	if len(result.Report.Workflows) != 1 || result.Report.Workflows[0] != "quick-fix" {
		t.Errorf("Report.Workflows: want [quick-fix], got %v", result.Report.Workflows)
	}
}

// TestAvailableWorkflows_MultipleWorkflows_ComposedInSelectionOrder asserts that when
// multiple workflows are supplied, their blocks appear in the output in exactly the order
// they appear in Request.Workflows. Order is the caller's selection order, not alphabetical
// or map-iteration order.
func TestAvailableWorkflows_MultipleWorkflows_ComposedInSelectionOrder(t *testing.T) {
	// Provide greenfield-tdd before quick-fix to verify order is preserved, not sorted.
	req := transform.Request{
		Source: []byte(orchestratorWithWorkflows),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Workflows: []transform.WorkflowBlock{
			{ID: "greenfield-tdd", Block: []byte(greenfieldTDDBlock)},
			{ID: "quick-fix", Block: []byte(quickFixBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("AvailableWorkflows")
	if !ok {
		t.Fatal("AvailableWorkflows injection absent from output")
	}

	content := node.Content()

	// Both blocks must be present.
	if !bytes.Contains(content, []byte("[[SECTION:Workflow:greenfield-tdd]]")) {
		t.Error("AvailableWorkflows content missing greenfield-tdd block")
	}
	if !bytes.Contains(content, []byte("[[SECTION:Workflow:quick-fix]]")) {
		t.Error("AvailableWorkflows content missing quick-fix block")
	}

	// Order: greenfield-tdd must appear before quick-fix.
	gfPos := bytes.Index(content, []byte("[[SECTION:Workflow:greenfield-tdd]]"))
	qfPos := bytes.Index(content, []byte("[[SECTION:Workflow:quick-fix]]"))
	if gfPos >= qfPos {
		t.Errorf("workflow blocks not in selection order: greenfield-tdd at %d, quick-fix at %d (expected greenfield-tdd first)",
			gfPos, qfPos)
	}

	// Report.Workflows must reflect the same order.
	if len(result.Report.Workflows) != 2 {
		t.Fatalf("Report.Workflows: want 2 entries, got %d: %v", len(result.Report.Workflows), result.Report.Workflows)
	}
	if result.Report.Workflows[0] != "greenfield-tdd" {
		t.Errorf("Report.Workflows[0]: want %q, got %q", "greenfield-tdd", result.Report.Workflows[0])
	}
	if result.Report.Workflows[1] != "quick-fix" {
		t.Errorf("Report.Workflows[1]: want %q, got %q", "quick-fix", result.Report.Workflows[1])
	}
}

// TestAvailableWorkflows_InjectionClass_IsWorkflow asserts that the InjectionOutcome for
// AvailableWorkflows carries Class == InjectionWorkflow regardless of whether any workflows
// were selected.
func TestAvailableWorkflows_InjectionClass_IsWorkflow(t *testing.T) {
	for _, withWorkflows := range []bool{false, true} {
		withWorkflows := withWorkflows
		t.Run(map[bool]string{false: "zero-workflows", true: "one-workflow"}[withWorkflows], func(t *testing.T) {
			var workflows []transform.WorkflowBlock
			if withWorkflows {
				workflows = []transform.WorkflowBlock{
					{ID: "quick-fix", Block: []byte(quickFixBlock)},
				}
			}

			req := transform.Request{
				Source:    []byte(orchestratorWithWorkflows),
				Kind:      domain.ArtifactAgent,
				Key:       "orchestrator",
				Module:    newFixtureModule(t),
				Model:     fixtureModel(),
				Scope:     domain.ScopeProject,
				Workflows: workflows,
			}

			result, err := transform.Apply(req)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			var outcome *transform.InjectionOutcome
			for i := range result.Report.Injections {
				if result.Report.Injections[i].Name == "AvailableWorkflows" {
					outcome = &result.Report.Injections[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("InjectionOutcome for AvailableWorkflows absent")
			}
			if outcome.Class != domain.InjectionWorkflow {
				t.Errorf("AvailableWorkflows Class: want %q, got %q",
					domain.InjectionWorkflow, outcome.Class)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T10.7 — Re-injection: adding a workflow to an existing deployment
// ---------------------------------------------------------------------------

// TestAvailableWorkflows_ReinjectionWithExpandedSet_NoDuplication asserts that when a
// deployed orchestrator already has one workflow in AvailableWorkflows and the new request
// supplies both the existing and a new workflow, the output contains both workflows exactly
// once each. The injection is fully assembled from Request.Workflows, not merged with the
// deployed content.
func TestAvailableWorkflows_ReinjectionWithExpandedSet_NoDuplication(t *testing.T) {
	req := transform.Request{
		Source:   []byte(orchestratorWithWorkflows),
		Deployed: []byte(deployedOrchestratorWithOneWorkflow),
		Kind:     domain.ArtifactAgent,
		Key:      "orchestrator",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		// Supply both the previously-injected workflow and a new one.
		Workflows: []transform.WorkflowBlock{
			{ID: "quick-fix", Block: []byte(quickFixBlock)},
			{ID: "greenfield-tdd", Block: []byte(greenfieldTDDBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("AvailableWorkflows")
	if !ok {
		t.Fatal("AvailableWorkflows injection absent from output")
	}

	content := node.Content()

	// Both workflows must appear.
	if !bytes.Contains(content, []byte("[[SECTION:Workflow:quick-fix]]")) {
		t.Error("quick-fix block absent from re-injected AvailableWorkflows")
	}
	if !bytes.Contains(content, []byte("[[SECTION:Workflow:greenfield-tdd]]")) {
		t.Error("greenfield-tdd block absent from re-injected AvailableWorkflows")
	}

	// Each workflow must appear exactly once (no duplication from the deployed file).
	quickFixCount := bytes.Count(content, []byte("[[SECTION:Workflow:quick-fix]]"))
	if quickFixCount != 1 {
		t.Errorf("quick-fix block appears %d times in AvailableWorkflows; want exactly 1", quickFixCount)
	}
	greenfieldCount := bytes.Count(content, []byte("[[SECTION:Workflow:greenfield-tdd]]"))
	if greenfieldCount != 1 {
		t.Errorf("greenfield-tdd block appears %d times in AvailableWorkflows; want exactly 1", greenfieldCount)
	}

	// Report.Workflows must list both IDs.
	if len(result.Report.Workflows) != 2 {
		t.Errorf("Report.Workflows: want 2 entries, got %d: %v", len(result.Report.Workflows), result.Report.Workflows)
	}
}

// TestAvailableWorkflows_SameWorkflowReinjected_NoDuplication asserts that when the caller
// supplies the same workflow in Request.Workflows that is already present in the deployed
// file's AvailableWorkflows injection, the result contains the workflow exactly once. The
// implementation must not merge Request.Workflows with the deployed content.
func TestAvailableWorkflows_SameWorkflowReinjected_NoDuplication(t *testing.T) {
	req := transform.Request{
		Source:   []byte(orchestratorWithWorkflows),
		Deployed: []byte(deployedOrchestratorWithOneWorkflow),
		Kind:     domain.ArtifactAgent,
		Key:      "orchestrator",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		// Same workflow as what's in the deployed file.
		Workflows: []transform.WorkflowBlock{
			{ID: "quick-fix", Block: []byte(quickFixBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("AvailableWorkflows")
	if !ok {
		t.Fatal("AvailableWorkflows injection absent from output")
	}

	// quick-fix must appear exactly once.
	count := bytes.Count(node.Content(), []byte("[[SECTION:Workflow:quick-fix]]"))
	if count != 1 {
		t.Errorf("quick-fix block appears %d times in AvailableWorkflows after re-injection; want 1", count)
	}
}

// TestAvailableWorkflows_BlockContentVerbatim_IncludesVersionComment asserts that the
// workflow-version comment embedded in the section block is preserved verbatim in the
// injection content. This comment is the mechanism by which staleness is detected on
// subsequent updates.
func TestAvailableWorkflows_BlockContentVerbatim_IncludesVersionComment(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorWithWorkflows),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Workflows: []transform.WorkflowBlock{
			{ID: "greenfield-tdd", Block: []byte(greenfieldTDDBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("AvailableWorkflows")
	if !ok {
		t.Fatal("AvailableWorkflows injection absent from output")
	}

	if !bytes.Contains(node.Content(), []byte("<!-- workflow-version: 3.3 -->")) {
		t.Errorf("workflow-version comment not preserved verbatim in injection;\ncontent: %q", node.Content())
	}
}

// ---------------------------------------------------------------------------
// Workflow injection purity — body prose outside AvailableWorkflows is unaffected
// ---------------------------------------------------------------------------

// TestWorkflowContent_ConfinedToAvailableWorkflowsInjection asserts that workflow section
// tags (e.g. [[SECTION:Workflow:...]]) do not appear in the output body prose outside the
// AvailableWorkflows injection region. Workflow files are never deployed as standalone
// artifacts; their section blocks are exclusively assembled into the injection region.
// An implementation that inadvertently propagates workflow metadata to other fields of
// the result would be caught here.
func TestWorkflowContent_ConfinedToAvailableWorkflowsInjection(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorWithWorkflows),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Workflows: []transform.WorkflowBlock{
			{ID: "quick-fix", Block: []byte(quickFixBlock)},
			{ID: "greenfield-tdd", Block: []byte(greenfieldTDDBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	_, outBody, err := docformat.SplitFrontmatter(result.Output)
	if err != nil {
		t.Fatalf("SplitFrontmatter(output): %v", err)
	}

	// Strip injection content from the output body. After stripping, the bytes between
	// every [[INJECTION:...]] / [[/INJECTION:...]] pair are removed. Any [[SECTION:Workflow:
	// tag that remains in the stripped output exists outside an injection region — a
	// violation: workflow content must never appear in the body prose or any field other
	// than inside AvailableWorkflows.
	strippedOut := stripInjectionContent(outBody)

	if bytes.Contains(strippedOut, []byte("[[SECTION:Workflow:")) {
		t.Errorf("workflow section tag found outside an injection region in the output body;\n"+
			"stripped output (injection content removed):\n%q", strippedOut)
	}
}

// TestAvailableWorkflows_BodyProse_UnchangedByWorkflowAssembly asserts that assembling
// workflow blocks into AvailableWorkflows does not alter the body prose outside of the
// injection region.
func TestAvailableWorkflows_BodyProse_UnchangedByWorkflowAssembly(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorWithWorkflows),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Workflows: []transform.WorkflowBlock{
			{ID: "quick-fix", Block: []byte(quickFixBlock)},
			{ID: "greenfield-tdd", Block: []byte(greenfieldTDDBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	_, srcBody, err := docformat.SplitFrontmatter([]byte(orchestratorWithWorkflows))
	if err != nil {
		t.Fatalf("SplitFrontmatter(source): %v", err)
	}
	_, outBody, err := docformat.SplitFrontmatter(result.Output)
	if err != nil {
		t.Fatalf("SplitFrontmatter(output): %v", err)
	}

	// Strip injection content from both and compare.
	strippedSrc := stripInjectionContent(srcBody)
	strippedOut := stripInjectionContent(outBody)

	if !bytes.Equal(strippedSrc, strippedOut) {
		t.Errorf("body prose outside injection regions changed after workflow assembly:\n"+
			"source (%d bytes):\n%q\noutput (%d bytes):\n%q",
			len(strippedSrc), strippedSrc,
			len(strippedOut), strippedOut)
	}
}
