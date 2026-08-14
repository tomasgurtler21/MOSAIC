package transform_test

// nested_reorder_interaction_test.go verifies the interaction between Stage 6
// (nested-region preservation inside managed region regions) and Stage 7 (parking
// on schema reorder), which neither stage covers alone.
//
// The specific gap: Stage 6 tests verify that nested user-owned regions survive
// managed region regeneration on a *normal* update. Stage 7 tests verify that
// custom-region regions outside managed region are anchored or parked on a reorder.
// Neither stage tests what happens when a managed region region with nested user-owned
// content is processed during a reorder.
//
// Properties verified:
//   - A custom-region region nested inside a managed region region in the deployed file
//     is re-emitted via the Stage 6 capture-reemit sequence even when a schema reorder
//     is detected, with content byte-identical.
//   - A project-injection region region nested inside a managed region region in the source is
//     preserved inside the parent after the reorder, with content byte-identical.
//   - Neither of these nested regions is parked at end of file (they are re-emitted
//     by the capture-reemit sequence before the parking decision runs).
//   - No GapParkedCustomRegion is emitted for a region that is handled by
//     the capture-reemit sequence.
//   - The reorder is detected (fixture guard).

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Fixture: deployed region with nested custom + schema reorder
//
// Source order:   Identity → Capabilities → Constraints  (with HarnessConstraints)
// Deployed order: Identity → Constraints → Capabilities  (reorder)
//
// In the deployed file, HarnessConstraints contains:
//   - Canonical tool-managed content (regenerated on every transform)
//   - <NestedTeamNote type="custom"> added by the project (Stage 6 must re-emit it)
//
// After Apply:
//   - HarnessConstraints is regenerated at its source-declared position (inside Constraints)
//   - NestedTeamNote is re-emitted inside HarnessConstraints via Stage 6 capture-reemit
//   - NestedTeamNote content is byte-identical
//   - NestedTeamNote is NOT parked at end of file
//   - No GapParkedCustomRegion for NestedTeamNote
// ---------------------------------------------------------------------------

const nestedReorderSource = `---
id: 960
version: 2.0.0
name: nested-reorder-test
description: Agent for nested-region-preservation-under-reorder testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested reorder interaction testing
required_skills: []
---

<Identity type="core">
# NestedReorder Agent

You are the NestedReorder agent.
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

// nestedReorderDeployed: section order Identity → Constraints → Capabilities.
// Source has Identity → Capabilities → Constraints → reorder detected.
// HarnessConstraints holds a nested <NestedTeamNote type="custom"> added by the project.
const nestedReorderDeployed = `---
id: 960
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested-region-preservation-under-reorder testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# NestedReorder Agent

You are the NestedReorder agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Old canonical harness content (regenerated each transform).
<NestedTeamNote type="custom">
Project-added note nested inside HarnessConstraints.
This content must survive the reorder byte-identically.
It must be placed inside HarnessConstraints, not parked at end of file.
</NestedTeamNote>
</HarnessConstraints>
</Constraints>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>
`

// ---------------------------------------------------------------------------
// Fixture B: source injection nested in deployed region + schema reorder
//
// Source order:   Identity → Constraints (HarnessConstraints with nested injection) → Capabilities
// Deployed order: Identity → Capabilities → Constraints  (reorder)
//
// The source HarnessConstraints has a nested <LocalPolicy type="project">.
// LocalPolicy has content in the deployed file (lifted by the region map).
//
// After Apply:
//   - HarnessConstraints regenerated inside Constraints (source position)
//   - LocalPolicy content preserved inside HarnessConstraints (Stage 6 re-emit)
//   - LocalPolicy NOT parked (Stage 6 handles it before parking decision)
// ---------------------------------------------------------------------------

const nestedInjectionReorderSource = `---
id: 961
version: 2.0.0
name: nested-injection-reorder-test
description: Agent for nested-injection-reorder testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested injection reorder testing
required_skills: []
---

<Identity type="core">
# NestedInjectionReorder Agent

You are the NestedInjectionReorder agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
<LocalPolicy type="project">
</LocalPolicy>
</HarnessConstraints>
</Constraints>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>
`

// nestedInjectionReorderDeployed: section order Identity → Capabilities → Constraints.
// Source has Identity → Constraints → Capabilities → reorder detected.
// LocalPolicy has non-empty content inside HarnessConstraints.
const nestedInjectionReorderDeployed = `---
id: 961
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested-injection-reorder testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# NestedInjectionReorder Agent

You are the NestedInjectionReorder agent.
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Old canonical harness content.
<LocalPolicy type="project">
Team-specific local policy content.
This injection is nested inside HarnessConstraints and must survive reorder.
</LocalPolicy>
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Shared helper for nested-reorder interaction tests
// ---------------------------------------------------------------------------

func nestedReorderReq(t *testing.T, source, deployed []byte, key string) transform.Request {
	t.Helper()
	return transform.Request{
		Source:    source,
		Deployed:  deployed,
		Kind:      domain.ArtifactAgent,
		Key:       key,
		Module:    newFixtureModule(t),
		Model:     fixtureModel(),
		Scope:     domain.ScopeProject,
		Timestamp: "2026-08-11T17:07:26Z",
	}
}

// ---------------------------------------------------------------------------
// T9.6 — Guard: reorder is actually detected
// ---------------------------------------------------------------------------

// TestNestedReorderInteraction_ReorderDetectedByFixture confirms the reorder condition
// is satisfied in the fixture so subsequent assertions are meaningful.
func TestNestedReorderInteraction_ReorderDetectedByFixture(t *testing.T) {
	depDoc, err := docformat.Parse([]byte(nestedReorderDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	srcDoc, err := docformat.Parse([]byte(nestedReorderSource))
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	if !docformat.ReorderDetected(depDoc, srcDoc) {
		t.Error("ReorderDetected must return true for this fixture — " +
			"source: Identity→Capabilities→Constraints; deployed: Identity→Constraints→Capabilities. " +
			"The nested-reorder interaction tests are only meaningful when a reorder is present.")
	}
}

// ---------------------------------------------------------------------------
// T9.6 — Nested custom-region survives managed region regeneration during reorder
// ---------------------------------------------------------------------------

// TestNestedCustom_SurvivesDeployedRegenerationOnReorder verifies that a custom-region
// region nested inside a managed region region in the deployed file is re-emitted
// inside the parent after regeneration, even when a schema reorder is detected.
func TestNestedCustom_SurvivesDeployedRegenerationOnReorder(t *testing.T) {
	req := nestedReorderReq(t,
		[]byte(nestedReorderSource),
		[]byte(nestedReorderDeployed),
		"nested-reorder-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	node, ok := outDoc.Body().Custom("NestedTeamNote")
	if !ok {
		t.Fatal("NestedTeamNote absent from output; " +
			"a custom-region region nested inside a managed region region must survive " +
			"the deployed region's regeneration, even when a schema reorder is detected")
	}
	_ = node
}

// TestNestedCustom_ContentByteIdenticalAfterReorder verifies that the content of a
// custom-region region nested inside a managed region region is preserved byte-identically,
// not truncated or reformatted, after the reorder update.
func TestNestedCustom_ContentByteIdenticalAfterReorder(t *testing.T) {
	req := nestedReorderReq(t,
		[]byte(nestedReorderSource),
		[]byte(nestedReorderDeployed),
		"nested-reorder-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(nestedReorderDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	depNode, depOK := depDoc.Body().Custom("NestedTeamNote")
	if !depOK {
		t.Fatal("NestedTeamNote not in deployed fixture; fixture is malformed")
	}
	outNode, outOK := outDoc.Body().Custom("NestedTeamNote")
	if !outOK {
		t.Skip("NestedTeamNote absent from output; content comparison skipped")
	}

	if !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("NestedTeamNote content not byte-identical after reorder:\n"+
			"deployed: %q\noutput:   %q",
			depNode.Content(), outNode.Content())
	}
}

// TestNestedCustom_NotParkedDuringReorder verifies that NestedTeamNote is NOT parked at
// end of file. The Stage 6 capture-reemit sequence handles it before the parking decision
// runs, so it ends up in reemittedNames and is excluded from parking consideration.
func TestNestedCustom_NotParkedDuringReorder(t *testing.T) {
	req := nestedReorderReq(t,
		[]byte(nestedReorderSource),
		[]byte(nestedReorderDeployed),
		"nested-reorder-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// No GapParkedCustomRegion for NestedTeamNote.
	pg := findParkingGap(result.Report.Gaps)
	if pg != nil {
		if strings.Contains(pg.Detail, "NestedTeamNote") {
			t.Errorf("NestedTeamNote appears in the parking gap detail; "+
				"a region inside a still-existing managed region parent must be handled by "+
				"the Stage 6 capture-reemit sequence, not the parking path: %s", pg.Detail)
		}
	}

	// No RegionParked action for NestedTeamNote.
	outcome := findOutcomeByName(result.Report.Regions, "NestedTeamNote")
	if outcome != nil && outcome.Action == transform.RegionParked {
		t.Errorf("NestedTeamNote has action RegionParked; "+
			"it must be handled by the capture-reemit sequence, not the parking path")
	}
}

// TestNestedCustom_RemainsInsideDeployedRegionAfterReorder verifies that NestedTeamNote
// is placed inside HarnessConstraints in the output, not promoted to body top level.
// The Stage 6 re-emit appends it inside the parent's regenerated content.
func TestNestedCustom_RemainsInsideDeployedRegionAfterReorder(t *testing.T) {
	req := nestedReorderReq(t,
		[]byte(nestedReorderSource),
		[]byte(nestedReorderDeployed),
		"nested-reorder-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outNode, outOK := outDoc.Body().Custom("NestedTeamNote")
	if !outOK {
		t.Fatal("NestedTeamNote absent from output")
	}

	parent := outNode.Parent()
	if parent == nil {
		t.Fatal("NestedTeamNote has no parent; it must remain nested inside HarnessConstraints")
	}
	if parent.Kind() != docformat.NodeDeployed {
		t.Errorf("NestedTeamNote parent kind: want NodeDeployed (HarnessConstraints), got %q", parent.Kind())
	}
	if parent.Name() != "HarnessConstraints" {
		t.Errorf("NestedTeamNote parent name: want %q, got %q", "HarnessConstraints", parent.Name())
	}
}

// ---------------------------------------------------------------------------
// T9.6 — Nested project-injection region in managed region survives reorder
// ---------------------------------------------------------------------------

// TestNestedInjection_SurvivesDeployedRegenerationOnReorder verifies that a project-injection region
// region declared in the source inside a managed region region has its content re-emitted
// inside the parent after regeneration during a reorder update.
func TestNestedInjection_SurvivesDeployedRegenerationOnReorder(t *testing.T) {
	req := nestedReorderReq(t,
		[]byte(nestedInjectionReorderSource),
		[]byte(nestedInjectionReorderDeployed),
		"nested-injection-reorder-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outInj, outOK := outDoc.Body().Injection("LocalPolicy")
	if !outOK {
		t.Fatal("LocalPolicy absent from output; " +
			"a project-injection region nested inside a managed region region must be re-emitted " +
			"inside the parent after regeneration, even when a schema reorder is detected")
	}
	_ = outInj
}

// TestNestedInjection_ContentByteIdenticalAfterReorder verifies that LocalPolicy's content
// is preserved byte-identically when the deployed region is regenerated during a reorder.
func TestNestedInjection_ContentByteIdenticalAfterReorder(t *testing.T) {
	req := nestedReorderReq(t,
		[]byte(nestedInjectionReorderSource),
		[]byte(nestedInjectionReorderDeployed),
		"nested-injection-reorder-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(nestedInjectionReorderDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	depInj, depOK := depDoc.Body().Injection("LocalPolicy")
	if !depOK {
		t.Fatal("LocalPolicy not in deployed fixture; fixture is malformed")
	}
	outInj, outOK := outDoc.Body().Injection("LocalPolicy")
	if !outOK {
		t.Skip("LocalPolicy absent from output; byte-identity check skipped")
	}

	if !bytes.Equal(outInj.Content(), depInj.Content()) {
		t.Errorf("LocalPolicy content not byte-identical after reorder:\n"+
			"deployed: %q\noutput:   %q",
			depInj.Content(), outInj.Content())
	}
}

// TestNestedInjection_NotParkedDuringReorder verifies that LocalPolicy does not produce
// a parking outcome. Injections are never parked; they follow their source-declared position.
func TestNestedInjection_NotParkedDuringReorder(t *testing.T) {
	req := nestedReorderReq(t,
		[]byte(nestedInjectionReorderSource),
		[]byte(nestedInjectionReorderDeployed),
		"nested-injection-reorder-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, out := range result.Report.Regions {
		if out.Name == "LocalPolicy" && out.Action == transform.RegionParked {
			t.Errorf("LocalPolicy has action RegionParked; injections are never parked on reorder")
		}
	}
}

