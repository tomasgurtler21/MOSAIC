package transform_test

// injection_test.go contains tests for the region processing path of Apply. The full
// region behaviour spans two implementation stages:
//
//   - Stage 8: regions are filled when the fixture harness has harness-level content
//     (RegionFilled), or left empty when the harness supplies nothing (RegionEmptied,
//     currently the default for the fixture).
//
//   - Stage 10: RegionPreserved (lifting content from Deployed for project regions)
//     and RegionAssembled (assembling AvailableWorkflows from Request.Workflows) are
//     implemented.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// sourceWithProjectInjection is a source document that has a project-level injection
// (CodebaseContext, InjectionProject class) embedded inside a section.
const sourceWithProjectInjection = `---
id: 1
version: 1.0.0
name: preserve-test
description: Agent for injection-preservation testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
</CodebaseContext>
</Capabilities>
`

// deployedWithProjectInjectionContent is the deployed version of sourceWithProjectInjection
// with user content inside the CodebaseContext injection.
const deployedWithProjectInjectionContent = `---
id: 1
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for injection-preservation testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
User-supplied codebase context: Go monorepo, hexagonal architecture.
Domain logic lives in internal/domain; no third-party imports allowed there.
</CodebaseContext>
</Capabilities>
`

// TestInjectionPreserved_ProjectInjectionContentLiftedByteIdentically asserts that when
// Request.Deployed is non-nil, the content of a project-level injection (InjectionProject
// class) from the deployed file is carried verbatim into the corresponding region of the
// output. The corresponding RegionOutcome has Action == RegionPreserved and Bytes equal
// to the length of the lifted content.
//
// This is the core contract of the injection merge policy: project-specific fill is never
// discarded on an update, only on an explicit new deployment.
func TestInjectionPreserved_ProjectInjectionContentLiftedByteIdentically(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProjectInjection),
		Deployed: []byte(deployedWithProjectInjectionContent),
		Kind:     domain.ArtifactAgent,
		Key:      "preserve-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Locate the CodebaseContext injection outcome.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "CodebaseContext" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("CodebaseContext action: want %q, got %q", transform.RegionPreserved, outcome.Action)
	}

	// Parse the output and check that the injection content is byte-identical to the
	// deployed content.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("CodebaseContext")
	if !ok {
		t.Fatal("CodebaseContext injection absent from output")
	}

	// Parse the deployed file to extract the expected content.
	depDoc, err := docformat.Parse([]byte(deployedWithProjectInjectionContent))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depNode, ok := depDoc.Body().Injection("CodebaseContext")
	if !ok {
		t.Fatal("CodebaseContext injection absent from deployed fixture")
	}

	if !bytes.Equal(node.Content(), depNode.Content()) {
		t.Errorf("CodebaseContext content not byte-identical to deployed:\nwant: %q\ngot:  %q",
			depNode.Content(), node.Content())
	}
	if outcome.Bytes != len(depNode.Content()) {
		t.Errorf("RegionOutcome.Bytes: want %d, got %d", len(depNode.Content()), outcome.Bytes)
	}
}

// sourceWithAvailableWorkflows is an orchestrator-like source that contains a
// <AvailableWorkflows type="managed"> region inside the Identity section.
const sourceWithAvailableWorkflows = `---
version: 6.0.0
name: orchestrator
description: Central coordinator that manages multi-agent workflow execution
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: multi-phase coordination
required_skills: []
---

<Identity type="core">
# Orchestrator Agent

You are the Orchestrator.

<AvailableWorkflows type="managed">
</AvailableWorkflows>
</Identity>
`

// TestInjectionAssembled_WorkflowsAssembledIntoAvailableWorkflows asserts that when
// Request.Workflows is non-empty, the AvailableWorkflows region of the output contains
// the assembled workflow blocks and the corresponding RegionOutcome in Report.Regions
// has Action == RegionAssembled.
//
// Expected behaviour:
//   - Each WorkflowBlock in Request.Workflows is appended verbatim to the
//     <AvailableWorkflows type="managed"> region in the output, in the order provided.
//   - Report.Regions contains an outcome with Action == RegionAssembled for the
//     AvailableWorkflows region.
//   - Report.Workflows lists the IDs of each assembled workflow block, in emitted order.
func TestInjectionAssembled_WorkflowsAssembledIntoAvailableWorkflows(t *testing.T) {
	const workflowBlock = "<Workflow type=\"core\" name=\"tdd-workflow\" version=\"1.0\">\n## TDD Workflow\n\nPhase table here.\n</Workflow>\n"

	req := transform.Request{
		Source: []byte(sourceWithAvailableWorkflows),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		Workflows: []transform.WorkflowBlock{
			{ID: "tdd-workflow", Block: []byte(workflowBlock)},
		},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Assert RegionAssembled outcome for AvailableWorkflows.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "AvailableWorkflows" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for AvailableWorkflows; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionAssembled {
		t.Errorf("AvailableWorkflows action: want %q, got %q", transform.RegionAssembled, outcome.Action)
	}

	// The workflow ID must appear in Report.Workflows.
	found := false
	for _, id := range result.Report.Workflows {
		if id == "tdd-workflow" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("workflow ID %q expected in Report.Workflows but absent; got: %v",
			"tdd-workflow", result.Report.Workflows)
	}

	// The assembled block must appear verbatim inside the AvailableWorkflows injection.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Deployed("AvailableWorkflows")
	if !ok {
		t.Fatal("AvailableWorkflows injection absent from output")
	}
	if !bytes.Contains(node.Content(), []byte(`<Workflow type="managed" name="tdd-workflow"`)) {
		t.Errorf("AvailableWorkflows injection content does not contain the workflow block opening tag;\ncontent: %q", node.Content())
	}
	if !bytes.Contains(node.Content(), []byte("</Workflow>")) {
		t.Errorf("AvailableWorkflows injection content does not contain the workflow block closing tag;\ncontent: %q", node.Content())
	}
}
