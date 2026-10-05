package transform_test

// infrastructure_merge_fixtures_test.go holds the shared builders for the infrastructure
// declaration reader, additive merge and workflow-preserve tests: block builders, section
// bytes written by AssembleInfrastructureBlocks, and deployed-orchestrator wrappers.

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// handWrittenSection is an infrastructure section whose content is not the assembler's table
// shape. It has no catalog counterpart and cannot be parsed into a block.
const handWrittenSection = `<InfrastructureAgent type="core" name="hand-made" version="0.1">

This agent was added by hand. It runs whenever the team lead asks.

</InfrastructureAgent>
`

// handAddedTableSection has the assembler's table shape but a key no catalog declares.
const handAddedTableSection = `<InfrastructureAgent type="core" name="team-extra" version="2.0">

| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | MANUAL | - | continue | Team specific check. |

</InfrastructureAgent>
`

// customRegionInInfra is a user-owned region nested inside the InfrastructureAgents region.
const customRegionInInfra = `<TeamNotes type="custom">
Keep this note. It belongs to the team.
</TeamNotes>
`

// newInfraBlock returns a single-trigger block with the given key and version.
func newInfraBlock(key, version string) transform.InfrastructureBlock {
	return transform.InfrastructureBlock{
		Key:         key,
		Version:     version,
		Class:       "review",
		Description: "Advisory checks for " + key + ".",
		OnFailure:   "continue",
		Triggers: []domain.InfrastructureTrigger{
			{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
		},
	}
}

// infraSection returns the bytes AssembleInfrastructureBlocks writes for the given blocks.
func infraSection(blocks ...transform.InfrastructureBlock) string {
	assembled, _ := transform.AssembleInfrastructureBlocks(blocks)
	return string(assembled)
}

// deployedFrontmatter is the frontmatter of a previously deployed orchestrator.
const deployedFrontmatter = `---
version: 6.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Central coordinator that manages multi-agent workflow execution
mode: subagent
model: claude/claude-sonnet
tools: [read-file, write-file, edit-file, search-file, search-text, run-terminal, ask-user]
---
`

// deployedOrchestrator returns a deployed orchestrator document whose InfrastructureAgents
// and AvailableWorkflows regions hold the given inner content.
func deployedOrchestrator(infraInner, workflowInner string) []byte {
	return []byte(deployedFrontmatter + `
<Identity type="core">
# Orchestrator Agent

You are the Orchestrator.

<InfrastructureAgents type="managed">
` + infraInner + `</InfrastructureAgents>

<AvailableWorkflows type="managed">
` + workflowInner + `</AvailableWorkflows>

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`)
}

// toCRLF converts every LF in s to CRLF.
func toCRLF(s string) string {
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// regionContent returns the inner bytes of a managed region of a document.
func regionContent(t *testing.T, doc []byte, name string) []byte {
	t.Helper()
	parsed, err := docformat.Parse(doc)
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := parsed.Body().Deployed(name)
	if !ok {
		t.Fatalf("region %q absent from document", name)
	}
	return node.Content()
}

// regionAction returns the RegionAction reported for the named managed region.
func regionAction(t *testing.T, result transform.Result, name string) transform.RegionAction {
	t.Helper()
	for _, r := range result.Report.Regions {
		if r.Name == name && r.Marker == docformat.NodeDeployed {
			return r.Action
		}
	}
	t.Fatalf("no region outcome for %q in report", name)
	return ""
}

// applyOrchestrator runs transform.Apply for the orchestrator source with the given request
// fields filled in by mutate.
func applyOrchestrator(t *testing.T, source string, mutate func(*transform.Request)) transform.Result {
	t.Helper()
	req := transform.Request{
		Source: []byte(source),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}
	mutate(&req)
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return result
}

// sameStrings reports whether a and b hold the same strings in the same order (nil equals empty).
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
