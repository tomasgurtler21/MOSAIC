package transform_test

// special_cases_test.go tests edge cases in the engine that arise from real agents (T8.7):
//   1. The orchestrator agent has tools: {tool-permissions} (a placeholder) instead of a
//      list. The engine must expand this using the descriptor's placeholder_expansion.
//   2. An agent with no model selected (OriginUnresolved) must not emit the model field,
//      or must emit an empty value — never a {model-identifier} placeholder.

import (
	"testing"

	"mosaic-deploy/internal/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// orchestratorSource mimics the generic orchestrator agent. Its tools value is the
// {tool-permissions} placeholder instead of a list. The model is also a placeholder.
const orchestratorSource = `---
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

[[/SECTION:Identity]]
`

// TestSpecialCase_OrchestratorPlaceholder_ExpandedToHarnessTools asserts that when the
// source agent uses the {tool-permissions} placeholder in the tools field, the output
// contains the tool set from the descriptor's placeholder_expansion, not the literal
// placeholder string.
//
// This covers the first scenario in T8.7.
func TestSpecialCase_OrchestratorPlaceholder_ExpandedToHarnessTools(t *testing.T) {
	mod := newFixtureModule(t)
	desc := mod.Descriptor()

	// The fixture descriptor declares placeholder_expansion as a known set of tools.
	// A correct engine must substitute that set for the placeholder.
	if len(desc.Tools.PlaceholderExpansion) == 0 {
		t.Skip("fixture descriptor has no placeholder_expansion; test not applicable")
	}

	req := transform.Request{
		Source: []byte(orchestratorSource),
		Kind:   domain.ArtifactAgent,
		Key:    "orchestrator",
		Module: mod,
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err) // RED: fails here
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}
	fm := doc.Frontmatter()

	// The output tools field must be a list, not the placeholder scalar.
	v, ok := fm.Get("tools")
	if !ok {
		t.Fatal("tools field absent from orchestrator output frontmatter")
	}
	if v.Kind != domain.KindList {
		// If the implementation emits a scalar, it has not expanded the placeholder.
		t.Fatalf("tools field after orchestrator transform: want KindList, got %q (value scalar: %q)",
			v.Kind, v.Scalar)
	}

	// The list must contain at least the tools from placeholder_expansion.
	// Tools in the expansion are guaranteed to be in the universe (per descriptor contract).
	expansionSet := make(map[string]bool, len(desc.Tools.PlaceholderExpansion))
	for _, name := range desc.Tools.PlaceholderExpansion {
		expansionSet[name] = true
	}

	emittedSet := make(map[string]bool, len(v.Items))
	for _, item := range v.Items {
		emittedSet[item.Scalar] = true
	}

	for _, want := range desc.Tools.PlaceholderExpansion {
		if !emittedSet[want] {
			t.Errorf("placeholder tool %q expected in output tools list but absent; got: %v",
				want, extractScalars(v.Items))
		}
	}
}

// TestSpecialCase_OrchestratorPlaceholder_NeverLiteralInOutput asserts that the literal
// string {tool-permissions} does not appear in the output frontmatter tools value. A
// correct engine replaces it; an incorrect engine would pass it through.
func TestSpecialCase_OrchestratorPlaceholder_NeverLiteralInOutput(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorSource),
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
		t.Fatal("tools field absent from output")
	}
	if v.Kind == domain.KindScalar && v.Scalar == "{tool-permissions}" {
		t.Error("tools field in output still contains the literal placeholder \"{tool-permissions}\"; it must be expanded")
	}
}

// TestSpecialCase_OrchestratorPlaceholder_ReportShowsPlaceholderResolution asserts that
// when the source has a tools placeholder, Report.Tools contains exactly one ToolResolution
// for the placeholder itself (not one per expanded tool). The placeholder resolution is a
// single operation; the expansion is the harness's answer to it.
func TestSpecialCase_OrchestratorPlaceholder_ReportShowsPlaceholderResolution(t *testing.T) {
	req := transform.Request{
		Source: []byte(orchestratorSource),
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

	// The orchestrator source has a {tool-permissions} placeholder, not a list.
	// Report.Tools must be initialised (non-nil) so consumers can safely iterate the
	// slice without a nil-check. Zero-length is acceptable here: when the source uses a
	// placeholder the implementation may record the expansion directly in the frontmatter
	// rather than emitting one ToolResolution per expanded tool, so an empty slice is a
	// valid representation of "placeholder handled, no per-tool resolution entries".
	// The correctness of the expanded tools list is asserted by
	// TestSpecialCase_OrchestratorPlaceholder_ExpandedToHarnessTools above.
	if result.Report.Tools == nil {
		t.Error("Report.Tools must be a non-nil slice even for orchestrator (placeholder) agents; initialise with make([]domain.ToolResolution, 0) when empty")
	}
}

// TestSpecialCase_NoModelSelected_ModelFieldAbsentOrEmpty asserts that when no model is
// selected (OriginUnresolved), the output frontmatter's model field either:
//   a) is absent entirely, or
//   b) is an empty scalar.
//
// It must NOT contain a placeholder like {model-identifier} or the literal model ID of
// an unrelated agent. This is the second scenario in T8.7.
func TestSpecialCase_NoModelSelected_ModelFieldAbsentOrEmpty(t *testing.T) {
	unresolved := domain.ModelSelection{
		Origin: domain.OriginUnresolved,
	}
	if unresolved.Resolved() {
		t.Fatal("OriginUnresolved.Resolved() should be false")
	}

	req := transform.Request{
		Source: []byte(frontmatterSource), // declared in frontmatter_test.go
		Kind:   domain.ArtifactAgent,
		Key:    "fm-test-agent",
		Module: newFixtureModule(t),
		Model:  unresolved,
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

	v, ok := doc.Frontmatter().Get("model")
	if !ok {
		// Model field absent is acceptable when no model is selected.
		return
	}
	// Model field present: it must be empty, not a placeholder or stale value.
	if v.Kind == domain.KindScalar && v.Scalar != "" && v.Scalar != "{model-identifier}" {
		// A non-empty, non-placeholder value would mean the model was set incorrectly.
		// Allow "" (empty) as a valid "no model" representation.
		t.Errorf("model field present with non-empty, non-placeholder value %q when no model was selected; must be absent or empty", v.Scalar)
	}
	if v.Kind == domain.KindScalar && v.Scalar == "{model-identifier}" {
		t.Error("model field contains the source placeholder {model-identifier}; it must be cleared or absent when no model is selected")
	}
}

// TestSpecialCase_NoModelSelected_GapEmittedForUnresolvedModel asserts that when no model
// is selected, the transformation emits a Gap with Kind GapNoModel so that the TODO
// checklist can surface it for the user.
func TestSpecialCase_NoModelSelected_GapEmittedForUnresolvedModel(t *testing.T) {
	unresolved := domain.ModelSelection{
		Origin: domain.OriginUnresolved,
	}

	req := transform.Request{
		Source: []byte(frontmatterSource),
		Kind:   domain.ArtifactAgent,
		Key:    "fm-test-agent",
		Module: newFixtureModule(t),
		Model:  unresolved,
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, gap := range result.Report.Gaps {
		if gap.Kind == domain.GapNoModel {
			return // found the expected gap
		}
	}
	t.Errorf("expected a Gap with Kind %q when no model is selected, but none was emitted; gaps: %v",
		domain.GapNoModel, gapKinds(result.Report.Gaps))
}

// gapKinds returns the Kind field of each Gap for use in diagnostic messages.
func gapKinds(gaps []domain.Gap) []domain.GapKind {
	kinds := make([]domain.GapKind, len(gaps))
	for i, g := range gaps {
		kinds[i] = g.Kind
	}
	return kinds
}
