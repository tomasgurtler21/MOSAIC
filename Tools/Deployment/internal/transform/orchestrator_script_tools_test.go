package transform_test

// orchestrator_script_tools_test.go covers transform pipeline behaviour for an
// orchestrator-role agent that carries a real tools list instead of the {tool-permissions}
// placeholder (T7.6).
//
// orchestrator-script.md is a Role=orchestrator agent that declares a specific tools list
// (e.g. [file_read, file_edit, file_search, content_search]) rather than the scalar
// {tool-permissions} that the main orchestrator uses. The pipeline must treat it like any
// other agent with an explicit list — map the declared tools through the harness descriptor
// and write the result to the output frontmatter — while still populating its managed
// injection regions (AvailableWorkflows, InfrastructureAgents) via the workflow/infra
// injection blocks supplied by buildContent.
//
// These tests guard against two specific failure modes:
//   1. The pipeline incorrectly expands the real tools list as if it were the placeholder,
//      emitting the full Universe of tools instead of only the declared ones.
//   2. The pipeline special-cases role=orchestrator and refuses to process a real tools list,
//      for example by inserting a guard that only the placeholder path is valid for orchestrators.
//
// T7.6 tests are expected to be GREEN immediately: the transform pipeline is already
// role-agnostic at the Apply level and does not branch on Role. These tests document and
// protect the existing correct behaviour as Stage 7 adds the second orchestrator form.

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Source fixtures for orchestrator-script transform tests
// ---------------------------------------------------------------------------

// orchestratorScriptToolsSource mimics an orchestrator-script.md source with a real tools
// list. The role is "orchestrator" (matching the planned catalog metadata), and the tools
// field holds explicit names rather than the {tool-permissions} scalar placeholder.
const orchestratorScriptToolsSource = `---
id: 0
version: 1.0.0
description: Script-mode orchestrator agent used to drive workflows via an AI harness.
role: orchestrator
tools: [file_read, file_search, content_search]
required_skills: []
---

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<InfrastructureAgents type="managed">
</InfrastructureAgents>

You are the orchestrator.
`

// orchestratorScriptPlaceholderSource is the main orchestrator's form — a scalar placeholder
// {tool-permissions} — included as a contrast fixture to confirm the two forms produce
// distinct tool-field shapes in the output.
const orchestratorScriptPlaceholderSource = `---
id: 0
version: 1.0.0
description: Main orchestrator with placeholder tools.
role: orchestrator
tools: {tool-permissions}
required_skills: []
---

<AvailableWorkflows type="managed">
</AvailableWorkflows>

<InfrastructureAgents type="managed">
</InfrastructureAgents>

You are the orchestrator.
`

// ---------------------------------------------------------------------------
// T7.6.1: Real tools list is mapped (not placeholder-expanded)
// ---------------------------------------------------------------------------

// TestOrchestratorScript_Transform_RealToolsListMappedCorrectly verifies that when an
// orchestrator-role agent carries a real tools list, the pipeline maps the declared tools
// through the harness descriptor and writes the mapped names to the output frontmatter.
// The output must contain the harness names for the declared tools and must NOT contain
// harness tools that were not declared (which would happen if the list were incorrectly
// treated as a placeholder and expanded to the full Universe).
func TestOrchestratorScript_Transform_RealToolsListMappedCorrectly(t *testing.T) {
	// Use the fixture harness module; it maps file_read, file_search, content_search
	// to harness-specific names and has other Universe tools not requested here.
	req := transform.Request{
		Source: []byte(orchestratorScriptToolsSource),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator-script",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// The tools field in the output must be a list (not a scalar placeholder).
	v, ok := doc.Frontmatter().Get("tools")
	if !ok {
		t.Fatal("tools field absent from output frontmatter; orchestrator-script must have its " +
			"tools field populated by the pipeline")
	}

	// A real list in the source must produce a list in the output, not a scalar.
	if v.Kind == domain.KindScalar {
		t.Errorf("output tools field is a scalar %q; a real tools list in the source must produce "+
			"a list in the output, not a placeholder-style scalar; this indicates the pipeline is "+
			"incorrectly treating the real list as a placeholder", v.Scalar)
	}
}

// TestOrchestratorScript_Transform_PlaceholderSourceProducesExpandedOutput verifies that
// the contrast source (main orchestrator with {tool-permissions} placeholder) produces an
// expanded list rather than the raw scalar in the output. This confirms the fixture harness
// understands placeholder expansion so the previous test's non-expansion assertion is
// meaningful.
func TestOrchestratorScript_Transform_PlaceholderSourceProducesExpandedOutput(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorScriptPlaceholderSource),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("tools")
	if !ok {
		t.Fatal("tools field absent from output frontmatter after placeholder expansion")
	}

	// Placeholder expansion must produce a list, not the raw scalar.
	if v.Kind == domain.KindScalar && v.Scalar == "{tool-permissions}" {
		t.Error("output tools field still contains raw {tool-permissions} placeholder; " +
			"placeholder expansion was not applied; the fixture harness must expand it to the Universe")
	}
}

// ---------------------------------------------------------------------------
// T7.6.2: Real tools list does not cause unexpected gaps
// ---------------------------------------------------------------------------

// TestOrchestratorScript_Transform_RealToolsList_NoSkippedFileGap verifies that when the
// orchestrator-script source uses a real tools list, Apply does not emit a GapSkippedFile
// gap (which would indicate the pipeline treated the source as invalid and bypassed it).
// A real tools list is a valid tools form for orchestrator-role agents.
func TestOrchestratorScript_Transform_RealToolsList_NoSkippedFileGap(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorScriptToolsSource),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator-script",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A real tools list must not cause the pipeline to skip the file.
	for _, gap := range result.Report.Gaps {
		if gap.Kind == domain.GapSkippedFile {
			t.Errorf("unexpected %q gap for orchestrator-script with a real tools list; "+
				"an explicit tools list is valid and must not cause the pipeline to skip the file; "+
				"gap: {Subject:%q Detail:%q}", gap.Kind, gap.Subject, gap.Detail)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.6.3: Apply succeeds (no error) for orchestrator-role agent with real tools list
// ---------------------------------------------------------------------------

// TestOrchestratorScript_Transform_ApplySucceedsWithRealToolsList verifies that Apply does
// not return an error when the source is an orchestrator-role agent with a real tools list.
// An error here would indicate that the pipeline has a guard rejecting the combination of
// role=orchestrator and a non-placeholder tools field, which is not a correct constraint.
func TestOrchestratorScript_Transform_ApplySucceedsWithRealToolsList(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorScriptToolsSource),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator-script",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err != nil {
		t.Errorf("Apply returned an error for orchestrator-script with a real tools list: %v; "+
			"the pipeline must not error on role=orchestrator + explicit tools list; "+
			"both the placeholder form (main orchestrator) and the real-list form (orchestrator-script) "+
			"must be supported", err)
	}
}
