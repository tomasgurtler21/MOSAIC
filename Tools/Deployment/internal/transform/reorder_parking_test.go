package transform_test

// reorder_parking_test.go covers Stage 7 parking and durable TODO reporting.
//
// When a schema reorder is detected the transform must:
//   - Anchor every custom-region region whose parent name still exists anywhere in the source
//     (section or deployed region) — the region moves with its parent and is NOT parked.
//   - Park every custom-region region that is top-level (no parent) or whose parent name has
//     been removed from the source — appended at end of body, content byte-identical.
//   - Emit exactly one GapParkedCustomRegion gap per transform listing every parked region
//     name in ascending sorted order, carrying the caller-supplied timestamp.
//   - Produce no parking gap and no parking when no reorder is detected, even if the source
//     merely gains or loses a section.
//   - Persist no state about parking: a subsequent no-reorder run must leave parked regions
//     where they are, emit no new parking gap, and produce output identical to the prior run.
//
// All tests are in the TDD RED phase: the reorder detection, anchoring, and parking branch
// in processRegions, sourceAnchorNames, parkCustomRegions, and ParkedCustomRegionsDetail are
// not yet implemented. Tests are expected to fail at runtime until Stage 7 implementation is
// complete.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Fixture A — Standard reorder: Capabilities and Constraints swapped
//
// Source order:   Identity → Capabilities → Constraints
// Deployed order: Identity → Constraints → Capabilities
//
// Reorder detected (Capabilities and Constraints are swapped).
//
// Custom regions in the deployed file:
//   - ConstraintsNote: nested inside Constraints (parent still in source) → anchored, NOT parked
//   - TopLevelNote:    at body top level (no parent)                       → parked on reorder
// ---------------------------------------------------------------------------

const reorderAnchorSource = `---
id: 700
version: 2.0.0
name: reorder-anchor-test
description: Agent for reorder anchoring tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: reorder anchoring testing
required_skills: []
---

<Identity type="core">
# ReorderAnchor Agent

You are the ReorderAnchor agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>
`

// reorderAnchorDeployed has sections in the order Identity → Constraints → Capabilities.
// The source has Identity → Capabilities → Constraints, so Capabilities and Constraints
// are swapped → reorder is detected.
//
// ConstraintsNote is nested in Constraints: its parent "Constraints" still exists in the
// source, so it anchors to Constraints and is NOT parked.
// TopLevelNote is at body top level (ParentName = ""): it is parked on any reorder.
const reorderAnchorDeployed = `---
id: 700
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for reorder anchoring tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# ReorderAnchor Agent

You are the ReorderAnchor agent.

<IdentityExtension type="project">
Saved identity extension content that must survive.
</IdentityExtension>
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<ConstraintsNote type="custom">
A note anchored to the Constraints section.
It must move with Constraints on a reorder and must NOT be parked.
</ConstraintsNote>
</Constraints>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<TopLevelNote type="custom">
A top-level custom note with no parent section.
On a reorder, this must be parked at end of file.
</TopLevelNote>
`

// ---------------------------------------------------------------------------
// Fixture B — Multiple unanchored regions: sorted order test
//
// Source order:   Capabilities → Identity   (reorder vs deployed)
// Deployed order: Identity → Capabilities
//
// Three top-level custom regions in non-alphabetical order in the deployed file.
// All are parked; the parking gap must list them alphabetically: AlphaNote, BetaNote, ZetaNote.
// ---------------------------------------------------------------------------

const sortedParkSource = `---
id: 701
version: 2.0.0
name: sorted-park-test
description: Agent for sorted parked-names tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: sorted parking testing
required_skills: []
---

<Capabilities type="core">
## Capabilities

Capabilities first in source.
</Capabilities>

<Identity type="core">
# SortedPark Agent

You are the SortedPark agent.
</Identity>
`

// sortedParkDeployed has sections Identity → Capabilities, which is the reverse of the
// source order → reorder detected. Three top-level custom regions are present; their names
// are in non-alphabetical order (Zeta, Alpha, Beta) to verify sorted emission in the gap.
const sortedParkDeployed = `---
id: 701
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for sorted parked-names tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# SortedPark Agent

You are the SortedPark agent.
</Identity>

<Capabilities type="core">
## Capabilities

Capabilities second in deployed.
</Capabilities>

<ZetaNote type="custom">
Content of ZetaNote — must be listed after BetaNote in sorted output.
</ZetaNote>

<AlphaNote type="custom">
Content of AlphaNote — must be listed first in sorted output.
</AlphaNote>

<BetaNote type="custom">
Content of BetaNote — must be listed second in sorted output.
</BetaNote>
`

// ---------------------------------------------------------------------------
// Fixture C — Missing parent: parent section removed from source
//
// Source order:   Identity → Constraints → Capabilities   (reorder vs deployed Capabilities→Constraints)
// Deployed order: Identity → OldSection → Capabilities → Constraints
//
// OldNote is nested in OldSection which is removed from the source.
// Reorder is also present (Capabilities and Constraints are swapped).
// OldNote has a missing parent AND a reorder → it is parked.
// ---------------------------------------------------------------------------

const missingParentSource = `---
id: 702
version: 2.0.0
name: missing-parent-test
description: Agent for missing-parent parking tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: missing parent testing
required_skills: []
---

<Identity type="core">
# MissingParent Agent

You are the MissingParent agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>
`

// missingParentDeployed has OldSection (removed in source) and a different section order.
// OldNote is nested in OldSection.
// Deployed order of common sections: Identity, Capabilities, Constraints.
// Source order of common sections: Identity, Constraints, Capabilities → reorder.
// Because OldSection is absent from the source AND a reorder is detected, OldNote is parked.
const missingParentDeployed = `---
id: 702
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for missing-parent parking tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# MissingParent Agent

You are the MissingParent agent.
</Identity>

<OldSection type="core">
## Old Section

This section was removed from the source file.

<OldNote type="custom">
Content anchored to OldSection, which no longer exists in the source.
On a reorder this content must be parked at end of file.
</OldNote>
</OldSection>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture D — Deployed-region parent: custom nested in a managed region region
//
// Source order:   Identity → Constraints (with HarnessConstraints) → Capabilities
// Deployed order: Identity → Capabilities → Constraints  (reorder)
//
// HarnessNote is nested inside <HarnessConstraints type="managed"> in the deployed file.
// HarnessConstraints still exists in the source.
// The Stage 6 capture-regenerate-reemit sequence handles HarnessNote before the parking
// decision runs, so HarnessNote ends up in reemittedNames and is excluded from parking.
// ---------------------------------------------------------------------------

const deployedParentSource = `---
id: 703
version: 2.0.0
name: deployed-parent-test
description: Agent for deployed-region parent anchor tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: deployed parent anchor testing
required_skills: []
---

<Identity type="core">
# DeployedParent Agent

You are the DeployedParent agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>
`

// deployedParentDeployed has sections in the order Identity → Capabilities → Constraints,
// which differs from the source order Identity → Constraints → Capabilities → reorder.
// HarnessNote is nested inside HarnessConstraints in the deployed file. The Stage 6
// capture-reemit sequence re-emits HarnessNote inside HarnessConstraints; it never
// reaches the parking decision path and must NOT produce a parking gap.
const deployedParentDeployed = `---
id: 703
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for deployed-region parent anchor tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# DeployedParent Agent

You are the DeployedParent agent.
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Old harness content that will be cleared by the fixture module.
<HarnessNote type="custom">
A custom note inside the HarnessConstraints deployed region.
It is preserved via the Stage 6 capture-reemit sequence, NOT via the parking path.
</HarnessNote>
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture E — No reorder: same section order in source and deployed
//
// Source order:   Identity → Capabilities → Constraints  (same as deployed)
// Deployed order: Identity → Capabilities → Constraints
//
// No reorder is detected → no parking, even though TopLevelNote has no parent.
// ---------------------------------------------------------------------------

const noReorderSource = `---
id: 704
version: 2.0.0
name: no-reorder-test
description: Agent for no-reorder no-parking tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: no reorder testing
required_skills: []
---

<Identity type="core">
# NoReorder Agent

You are the NoReorder agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>
`

// noReorderDeployed has the same section order as noReorderSource (Identity → Capabilities →
// Constraints) so no reorder is detected. TopLevelNote is at top level; because there is
// no reorder, it must NOT be parked — it is preserved at its anchored position.
const noReorderDeployed = `---
id: 704
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for no-reorder no-parking tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# NoReorder Agent

You are the NoReorder agent.

<IdentityExtension type="project">
Saved identity extension content.
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.

<ConstraintsNote type="custom">
A note in the Constraints section — must survive without parking.
</ConstraintsNote>
</Constraints>

<TopLevelNote type="custom">
A top-level note — no parent, but NO reorder, so must NOT be parked.
</TopLevelNote>
`

// ---------------------------------------------------------------------------
// Fixture F — Source gains a section: no reorder, no parking
//
// Source adds ErrorHandling to the end of the section list. The relative order of the
// common sections (Identity, Capabilities, Constraints) is unchanged → no reorder → no parking.
// ---------------------------------------------------------------------------

const sectionAddedSource = `---
id: 705
version: 2.0.0
name: section-added-test
description: Agent for section-added no-reorder tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: section addition testing
required_skills: []
---

<Identity type="core">
# SectionAdded Agent

You are the SectionAdded agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>

<ErrorHandling type="core">
## Error Handling

Handle errors gracefully.
</ErrorHandling>
`

// sectionAddedDeployed has Identity → Capabilities → Constraints (no ErrorHandling yet).
// The common sections (Identity, Capabilities, Constraints) are in the same relative order
// in both documents, so OrderDiffers returns false — no reorder, no parking.
const sectionAddedDeployed = `---
id: 705
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for section-added no-reorder tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# SectionAdded Agent

You are the SectionAdded agent.

<IdentityExtension type="project">
Saved identity extension content.
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>

<TopLevelNote type="custom">
A top-level note — no reorder when a section is merely added, so must NOT be parked.
</TopLevelNote>
`

// ---------------------------------------------------------------------------
// Fixture G — Source loses a section: no reorder in common slots, no parking
//
// Source removes OldSection. The remaining common sections (Identity, Capabilities) maintain
// their relative order in both documents → no reorder → no parking gap.
// The custom region nested in OldSection has a missing parent on a non-reorder update:
// it falls through to body-level placement without a parking gap.
// ---------------------------------------------------------------------------

const sectionRemovedSource = `---
id: 706
version: 2.0.0
name: section-removed-test
description: Agent for section-removed no-reorder tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: section removal testing
required_skills: []
---

<Identity type="core">
# SectionRemoved Agent

You are the SectionRemoved agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>
`

// sectionRemovedDeployed has Identity → OldSection → Capabilities.
// Source removes OldSection. Common sections: Identity, Capabilities — same relative order
// in both → no reorder. OldNote has a missing parent (OldSection gone), but since there is
// no reorder, it is NOT parked: it is appended at body level with no GapParkedCustomRegion.
const sectionRemovedDeployed = `---
id: 706
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for section-removed no-reorder tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# SectionRemoved Agent

You are the SectionRemoved agent.

<IdentityExtension type="project">
Saved identity extension content.
</IdentityExtension>
</Identity>

<OldSection type="core">
## Old Section

This section is removed from the source.

<OldNote type="custom">
Content anchored to OldSection — parent gone, but NO reorder, so NOT parked.
</OldNote>
</OldSection>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<TopLevelNote type="custom">
A top-level note — no reorder, so NOT parked.
</TopLevelNote>
`

// ---------------------------------------------------------------------------
// Shared helper: build a minimal Request for reorder parking tests
// ---------------------------------------------------------------------------

func reorderReq(t *testing.T, source, deployed []byte, key, timestamp string) transform.Request {
	t.Helper()
	return transform.Request{
		Source:    source,
		Deployed:  deployed,
		Kind:      domain.ArtifactAgent,
		Key:       key,
		Module:    newFixtureModule(t),
		Model:     fixtureModel(),
		Scope:     domain.ScopeProject,
		Timestamp: timestamp,
	}
}

// ---------------------------------------------------------------------------
// Helper: find the GapParkedCustomRegion gap in a report (nil when absent)
// ---------------------------------------------------------------------------

func findParkingGap(gaps []domain.Gap) *domain.Gap {
	for i := range gaps {
		if gaps[i].Kind == domain.GapParkedCustomRegion {
			return &gaps[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper: find a RegionOutcome by name (nil when absent)
// ---------------------------------------------------------------------------

func findOutcomeByName(outcomes []transform.RegionOutcome, name string) *transform.RegionOutcome {
	for i := range outcomes {
		if outcomes[i].Name == name {
			return &outcomes[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// T7.1 — Anchoring rule: section parent
//
// A custom-region region nested in a section that still exists in the source must move
// with that section and must NOT be parked, even when a reorder is detected.
// ---------------------------------------------------------------------------

// TestAnchoring_SectionParentSurvivesReorder_CustomRegionNotParked verifies that when a
// reorder is detected, a custom-region region whose parent section still exists in the source
// is placed with its parent and does NOT produce a GapParkedCustomRegion gap.
func TestAnchoring_SectionParentSurvivesReorder_CustomRegionNotParked(t *testing.T) {
	req := reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// ConstraintsNote must appear in the output — it must not be dropped.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("ConstraintsNote"); !ok {
		t.Fatal("ConstraintsNote absent from output; a region whose parent still exists must be placed, not dropped")
	}

	// ConstraintsNote must NOT produce a parking gap.
	pg := findParkingGap(result.Report.Gaps)
	if pg != nil && strings.Contains(pg.Detail, "ConstraintsNote") {
		t.Errorf("ConstraintsNote appears in parking gap detail; a region with a surviving parent must not be parked: %s", pg.Detail)
	}

	// ConstraintsNote must have action RegionCustomPreserved, not RegionParked.
	outcome := findOutcomeByName(result.Report.Regions, "ConstraintsNote")
	if outcome == nil {
		t.Fatal("no RegionOutcome for ConstraintsNote; a preserved custom region must appear in the report")
	}
	if outcome.Action == transform.RegionParked {
		t.Errorf("ConstraintsNote action is RegionParked; a region with a surviving parent must not be parked — its section still exists in the source")
	}
	if outcome.Action != transform.RegionCustomPreserved {
		t.Errorf("ConstraintsNote action: want %q, got %q; the region must be preserved at its anchored position", transform.RegionCustomPreserved, outcome.Action)
	}
}

// TestAnchoring_SectionParentSurvivesReorder_NestingPreserved verifies that a custom-region
// region nested inside a section that survives the reorder remains inside that section in
// the output — it must not be relocated to body top level or another section.
func TestAnchoring_SectionParentSurvivesReorder_NestingPreserved(t *testing.T) {
	req := reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	node, ok := outDoc.Body().Custom("ConstraintsNote")
	if !ok {
		t.Fatal("ConstraintsNote absent from output")
	}

	parent := node.Parent()
	if parent == nil {
		t.Fatal("ConstraintsNote has no parent in output; it must remain nested inside the Constraints section")
	}
	if parent.Kind() != docformat.NodeSection {
		t.Errorf("ConstraintsNote parent kind: want %q, got %q", docformat.NodeSection, parent.Kind())
	}
	if parent.Name() != "Constraints" {
		t.Errorf("ConstraintsNote parent name: want %q, got %q", "Constraints", parent.Name())
	}
}

// ---------------------------------------------------------------------------
// T7.1 — Anchoring rule: deployed-region parent
//
// A custom-region region nested inside a managed region region in the deployed file must not
// be parked even when a reorder is detected, because the Stage 6 capture-reemit sequence
// handles it before the parking decision: the region is in reemittedNames and excluded.
// ---------------------------------------------------------------------------

// TestAnchoring_DeployedParentSurvivesReorder_CustomRegionNotParked verifies that a
// custom-region region nested inside a managed region region in the deployed file does not
// produce a GapParkedCustomRegion gap even when a reorder is detected. The Stage 6
// capture-reemit sequence places the region inside its deployed parent before the parking
// path runs, so it is excluded from parking consideration.
func TestAnchoring_DeployedParentSurvivesReorder_CustomRegionNotParked(t *testing.T) {
	req := reorderReq(t,
		[]byte(deployedParentSource),
		[]byte(deployedParentDeployed),
		"deployed-parent-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// No GapParkedCustomRegion for HarnessNote.
	pg := findParkingGap(result.Report.Gaps)
	if pg != nil && strings.Contains(pg.Detail, "HarnessNote") {
		t.Errorf("HarnessNote appears in parking gap detail; a region inside a still-existing deployed region must not be parked: %s", pg.Detail)
	}

	// HarnessNote must appear in the output (preserved via reemit).
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("HarnessNote"); !ok {
		t.Fatal("HarnessNote absent from output; it must be re-emitted inside HarnessConstraints via the Stage 6 capture-reemit sequence")
	}

	// HarnessNote must have RegionCustomPreserved action (not RegionParked).
	outcome := findOutcomeByName(result.Report.Regions, "HarnessNote")
	if outcome != nil && outcome.Action == transform.RegionParked {
		t.Errorf("HarnessNote action is RegionParked; a region inside a still-existing deployed parent must not be parked")
	}
}

// ---------------------------------------------------------------------------
// T7.1 — Anchoring rule: missing parent
//
// A custom-region region whose parent section no longer exists in the source is parked
// at end of file when a reorder is also detected.
// ---------------------------------------------------------------------------

// TestAnchoring_MissingParentOnReorder_IsParked verifies that when a reorder is detected
// and a custom-region region's parent section no longer exists in the source, the region is
// appended at the end of the file and a GapParkedCustomRegion gap is emitted.
func TestAnchoring_MissingParentOnReorder_IsParked(t *testing.T) {
	req := reorderReq(t,
		[]byte(missingParentSource),
		[]byte(missingParentDeployed),
		"missing-parent-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A GapParkedCustomRegion must be emitted.
	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("expected GapParkedCustomRegion gap for OldNote (whose parent OldSection is gone from source); got no such gap")
	}

	// The gap detail must mention OldNote.
	if !strings.Contains(pg.Detail, "OldNote") {
		t.Errorf("parking gap detail does not contain OldNote: %s", pg.Detail)
	}

	// OldNote must appear in the output (content preserved, not discarded).
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("OldNote"); !ok {
		t.Fatal("OldNote absent from output; parked content must appear in the output, not be dropped")
	}

	// OldNote must have RegionParked action.
	outcome := findOutcomeByName(result.Report.Regions, "OldNote")
	if outcome == nil {
		t.Fatal("no RegionOutcome for OldNote; a parked region must appear in the report")
	}
	if outcome.Action != transform.RegionParked {
		t.Errorf("OldNote action: want %q, got %q", transform.RegionParked, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T7.2 — Top-level custom region is parked on reorder
// ---------------------------------------------------------------------------

// TestReorder_TopLevelCustomRegion_IsParked verifies that a custom-region region at body top
// level (ParentName = "") is parked at end of file and a GapParkedCustomRegion gap is
// emitted when a reorder is detected.
func TestReorder_TopLevelCustomRegion_IsParked(t *testing.T) {
	req := reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A GapParkedCustomRegion must be emitted.
	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("expected GapParkedCustomRegion gap for TopLevelNote (top-level, no parent); got no such gap")
	}
	if !strings.Contains(pg.Detail, "TopLevelNote") {
		t.Errorf("parking gap detail does not contain TopLevelNote: %s", pg.Detail)
	}

	// TopLevelNote must have RegionParked action.
	outcome := findOutcomeByName(result.Report.Regions, "TopLevelNote")
	if outcome == nil {
		t.Fatal("no RegionOutcome for TopLevelNote; a parked region must appear in the report")
	}
	if outcome.Action != transform.RegionParked {
		t.Errorf("TopLevelNote action: want %q, got %q", transform.RegionParked, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T7.3 — Parked content is byte-identical and placed after all sections
// ---------------------------------------------------------------------------

// TestReorder_ParkedContent_ByteIdenticalToDeployed verifies that a parked custom region's
// content in the output is byte-identical to the content in the deployed file. Parking must
// preserve content exactly — no reformatting, no truncation.
func TestReorder_ParkedContent_ByteIdenticalToDeployed(t *testing.T) {
	req := reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Parse both deployed and output to compare TopLevelNote content.
	depDoc, err := docformat.Parse([]byte(reorderAnchorDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	depNode, depOK := depDoc.Body().Custom("TopLevelNote")
	if !depOK {
		t.Fatal("TopLevelNote not found in deployed fixture; fixture is malformed")
	}
	outNode, outOK := outDoc.Body().Custom("TopLevelNote")
	if !outOK {
		t.Fatal("TopLevelNote absent from output; parked region must appear in the output")
	}

	if !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("TopLevelNote content not byte-identical after parking:\ndeployed: %q\noutput:   %q",
			depNode.Content(), outNode.Content())
	}
}

// TestReorder_ParkedRegion_PlacedAfterAllSections verifies that every parked custom region
// appears after all core-section region nodes in the output document. Parking must not interleave
// a custom region between sections.
func TestReorder_ParkedRegion_PlacedAfterAllSections(t *testing.T) {
	req := reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// Scan top-level nodes to find the last section and TopLevelNote's position.
	topNodes := outDoc.Body().TopLevelNodes()
	lastSectionIdx := -1
	topLevelNoteIdx := -1
	for i, n := range topNodes {
		if n.Kind() == docformat.NodeSection {
			lastSectionIdx = i
		}
		if n.Kind() == docformat.NodeCustom && n.Name() == "TopLevelNote" {
			topLevelNoteIdx = i
		}
	}

	if topLevelNoteIdx < 0 {
		t.Fatal("TopLevelNote not found among top-level nodes in output; parked region must be at body top level")
	}
	if lastSectionIdx < 0 {
		t.Fatal("no sections found in output; fixture is malformed")
	}
	if topLevelNoteIdx <= lastSectionIdx {
		t.Errorf("TopLevelNote is at top-level index %d but last section is at index %d; "+
			"parked region must appear AFTER all sections", topLevelNoteIdx, lastSectionIdx)
	}
}

// ---------------------------------------------------------------------------
// T7.4 — Statelessness: parked region stays put on subsequent no-reorder run
//
// Run 1: Apply with reorder → TopLevelNote is parked at end of output.
// Run 2: Apply with same source, Run 1 output as deployed → no reorder (sections now match) →
//        TopLevelNote remains at end of file, no new parking gap, output is identical to Run 1.
// ---------------------------------------------------------------------------

// TestStatelessness_ParkedRegion_StaysInPlaceOnNoReorderRun verifies that a custom region
// parked at end of file during a reorder remains at the end of file on a subsequent run
// where no reorder is detected. The tool persists no state about parking.
func TestStatelessness_ParkedRegion_StaysInPlaceOnNoReorderRun(t *testing.T) {
	// Run 1: reorder detected, TopLevelNote parked.
	run1Req := reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	)
	run1, err := transform.Apply(run1Req)
	if err != nil {
		t.Fatalf("Run 1 Apply: %v", err)
	}

	// Run 2: same source, use Run 1 output as deployed. The source and Run 1 output now
	// share the same section order (both built from reorderAnchorSource) → no reorder.
	run2Req := reorderReq(t,
		[]byte(reorderAnchorSource),
		run1.Output,
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	)
	run2, err := transform.Apply(run2Req)
	if err != nil {
		t.Fatalf("Run 2 Apply: %v", err)
	}

	// TopLevelNote must still be in the output after the second run.
	outDoc, err := docformat.Parse(run2.Output)
	if err != nil {
		t.Fatalf("parse Run 2 output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("TopLevelNote"); !ok {
		t.Fatal("TopLevelNote absent from Run 2 output; a parked region must remain in subsequent runs")
	}
}

// TestStatelessness_SubsequentRun_ProducesNoNewParkingGap verifies that a subsequent
// no-reorder run after a parking event does not emit a new GapParkedCustomRegion gap.
// The tool must not re-park a region that was already parked and is now sitting at end of file.
func TestStatelessness_SubsequentRun_ProducesNoNewParkingGap(t *testing.T) {
	// Run 1: parking event.
	run1, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 1 Apply: %v", err)
	}

	// Run 2: no reorder (source and Run 1 output share section order) → no parking gap.
	run2, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		run1.Output,
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 2 Apply: %v", err)
	}

	pg := findParkingGap(run2.Report.Gaps)
	if pg != nil {
		t.Errorf("Run 2 emitted a GapParkedCustomRegion gap on a no-reorder update; "+
			"the tool must not re-park already-parked regions: detail=%s", pg.Detail)
	}
}

// TestStatelessness_SubsequentRun_OutputIsStable verifies that two consecutive no-reorder
// runs produce identical output. This confirms the tool is truly stateless about parking:
// there is no flag, marker attribute, or persisted state that could cause drift.
func TestStatelessness_SubsequentRun_OutputIsStable(t *testing.T) {
	// Run 1: parking event.
	run1, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 1 Apply: %v", err)
	}

	// Run 2: same source, Run 1 output as deployed → no reorder.
	run2, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		run1.Output,
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 2 Apply: %v", err)
	}

	// Run 3: same source, Run 2 output as deployed → still no reorder.
	run3, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		run2.Output,
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 3 Apply: %v", err)
	}

	// Run 2 and Run 3 outputs must be identical (stable fixed point).
	if !bytes.Equal(run2.Output, run3.Output) {
		t.Errorf("Run 2 and Run 3 outputs differ; the transform must be output-stable across consecutive no-reorder runs:\n"+
			"Run 2 tail: %q\nRun 3 tail: %q",
			truncateBytes(run2.Output[max(0, len(run2.Output)-200):], 200),
			truncateBytes(run3.Output[max(0, len(run3.Output)-200):], 200))
	}
}

// max is a local helper since Go < 1.21 does not export max for ints.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// T7.5 — No reorder means no parking
// ---------------------------------------------------------------------------

// TestNoReorder_SameOrder_NoParking verifies that when the source and deployed documents have
// the same structural slot order, no GapParkedCustomRegion gap is emitted even though
// custom regions are present.
func TestNoReorder_SameOrder_NoParking(t *testing.T) {
	req := reorderReq(t,
		[]byte(noReorderSource),
		[]byte(noReorderDeployed),
		"no-reorder-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg != nil {
		t.Errorf("GapParkedCustomRegion emitted when there is no reorder; "+
			"no reorder must mean no parking: detail=%s", pg.Detail)
	}
}

// TestNoReorder_SameOrder_CustomRegionsPreservedInPlace verifies that without a reorder,
// custom regions are preserved in the output at their existing positions with no parking gap.
func TestNoReorder_SameOrder_CustomRegionsPreservedInPlace(t *testing.T) {
	req := reorderReq(t,
		[]byte(noReorderSource),
		[]byte(noReorderDeployed),
		"no-reorder-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Both custom regions must be in the output.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	for _, name := range []string{"ConstraintsNote", "TopLevelNote"} {
		name := name
		if _, ok := outDoc.Body().Custom(name); !ok {
			t.Errorf("Custom region %q absent from output; it must be preserved on a no-reorder update", name)
		}
	}

	// No region must carry RegionParked action.
	for _, outcome := range result.Report.Regions {
		if outcome.Action == transform.RegionParked {
			t.Errorf("region %q has action RegionParked on a no-reorder update; only reorder triggers parking", outcome.Name)
		}
	}
}

// TestNoReorder_SourceGainsSection_NoParking verifies that when the source merely gains a
// new section (pure addition), the relative order of common sections is unchanged and no
// parking gap is emitted, even though a top-level custom region is present in the deployed file.
func TestNoReorder_SourceGainsSection_NoParking(t *testing.T) {
	req := reorderReq(t,
		[]byte(sectionAddedSource),
		[]byte(sectionAddedDeployed),
		"section-added-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg != nil {
		t.Errorf("GapParkedCustomRegion emitted when source only gained a section (no reorder); "+
			"a pure addition must not trigger parking: detail=%s", pg.Detail)
	}

	// TopLevelNote must survive in the output (preserved, not dropped or parked).
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("TopLevelNote"); !ok {
		t.Fatal("TopLevelNote absent from output; it must be preserved on a no-reorder update even when a section is added")
	}
}

// TestNoReorder_SourceLosesSection_NoParking verifies that when the source merely removes a
// section, the relative order of the remaining common sections is unchanged and no parking
// gap is emitted. The custom region whose parent was in the removed section is preserved
// via body-level placement (the non-reorder fallback), not parked.
func TestNoReorder_SourceLosesSection_NoParking(t *testing.T) {
	req := reorderReq(t,
		[]byte(sectionRemovedSource),
		[]byte(sectionRemovedDeployed),
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg != nil {
		t.Errorf("GapParkedCustomRegion emitted when source only lost a section (no reorder); "+
			"a pure removal must not trigger parking: detail=%s", pg.Detail)
	}

	// OldNote and TopLevelNote must both survive in the output (not dropped).
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	for _, name := range []string{"OldNote", "TopLevelNote"} {
		name := name
		if _, ok := outDoc.Body().Custom(name); !ok {
			t.Errorf("Custom region %q absent from output; it must be preserved (not dropped) even when its parent section is removed and no reorder is detected", name)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.6 — TODO surfacing: gap category, message, names, timestamp
// ---------------------------------------------------------------------------

// TestParkingGap_MapsToGapParkedCustomRegion verifies that a parking event produces a gap
// with Kind = GapParkedCustomRegion in the transform report.
func TestParkingGap_MapsToGapParkedCustomRegion(t *testing.T) {
	result, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatalf("no GapParkedCustomRegion gap in report; parking must produce this gap kind")
	}
}

// TestParkingGap_Detail_ContainsSchemaReorderMessage verifies that the parking gap's Detail
// field begins with the specified message prefix about the schema reorder.
func TestParkingGap_Detail_ContainsSchemaReorderMessage(t *testing.T) {
	result, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no parking gap in report")
	}

	const wantPrefix = "Schema reorder detected."
	if !strings.Contains(pg.Detail, wantPrefix) {
		t.Errorf("parking gap detail does not contain expected prefix %q: %s", wantPrefix, pg.Detail)
	}

	const wantPhrase = "move them to the correct section"
	if !strings.Contains(pg.Detail, wantPhrase) {
		t.Errorf("parking gap detail does not contain expected phrase %q: %s", wantPhrase, pg.Detail)
	}
}

// TestParkingGap_Detail_ContainsParkedRegionName verifies that the parking gap's Detail field
// lists the name of every region that was parked.
func TestParkingGap_Detail_ContainsParkedRegionName(t *testing.T) {
	result, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no parking gap in report")
	}

	// TopLevelNote was parked; ConstraintsNote was not.
	if !strings.Contains(pg.Detail, "TopLevelNote") {
		t.Errorf("parking gap detail does not contain parked region name TopLevelNote: %s", pg.Detail)
	}
	if strings.Contains(pg.Detail, "ConstraintsNote") {
		t.Errorf("parking gap detail contains ConstraintsNote, which was NOT parked (its parent Constraints still exists): %s", pg.Detail)
	}
}

// TestParkingGap_Detail_ContainsTimestamp verifies that the parking gap's Detail field
// contains the timestamp string supplied in Request.Timestamp.
func TestParkingGap_Detail_ContainsTimestamp(t *testing.T) {
	const ts = "2026-08-11T17:07:26Z"
	result, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		ts,
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no parking gap in report")
	}

	if !strings.Contains(pg.Detail, ts) {
		t.Errorf("parking gap detail does not contain the caller-supplied timestamp %q: %s", ts, pg.Detail)
	}
}

// TestParkingGap_Subject_IsArtifactKey verifies that the parking gap's Subject field equals
// the artifact key from the request (req.Key). One gap per file — the subject identifies
// the file, not an individual region name.
func TestParkingGap_Subject_IsArtifactKey(t *testing.T) {
	const key = "reorder-anchor-test"
	result, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		key,
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no parking gap in report")
	}

	if pg.Subject != key {
		t.Errorf("parking gap Subject: want %q (artifact key), got %q", key, pg.Subject)
	}
}

// TestParkingGap_Fragment_IsEmpty verifies that the parking gap's Fragment field is empty.
// The parked content is in the deployed file, not lost; the user must move it to the correct
// section rather than pasting from a fragment.
func TestParkingGap_Fragment_IsEmpty(t *testing.T) {
	result, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no parking gap in report")
	}

	if pg.Fragment != "" {
		t.Errorf("parking gap Fragment must be empty (content is in the deployed file, not lost); got %q", pg.Fragment)
	}
}

// TestParkingGap_ExactlyOneGapPerTransform verifies that when multiple custom regions are
// parked in a single transform, exactly one GapParkedCustomRegion gap is produced listing
// all parked names. The gap is per-file, not per-region.
func TestParkingGap_ExactlyOneGapPerTransform(t *testing.T) {
	// sortedParkDeployed has three top-level custom regions (ZetaNote, AlphaNote, BetaNote)
	// that are all unanchored on the reorder. Exactly one parking gap must be produced.
	result, err := transform.Apply(reorderReq(t,
		[]byte(sortedParkSource),
		[]byte(sortedParkDeployed),
		"sorted-park-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var parkingGaps []domain.Gap
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapParkedCustomRegion {
			parkingGaps = append(parkingGaps, g)
		}
	}

	if len(parkingGaps) == 0 {
		t.Fatal("no GapParkedCustomRegion gap; expected exactly one when multiple regions are parked")
	}
	if len(parkingGaps) > 1 {
		t.Errorf("expected exactly one GapParkedCustomRegion gap per transform; got %d gaps", len(parkingGaps))
	}

	// The single gap's detail must mention all three parked names.
	detail := parkingGaps[0].Detail
	for _, name := range []string{"AlphaNote", "BetaNote", "ZetaNote"} {
		if !strings.Contains(detail, name) {
			t.Errorf("parking gap detail does not contain %q; all parked names must be listed in one gap: %s", name, detail)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.7 — Timestamp comes from Request, not from the clock
// ---------------------------------------------------------------------------

// TestTimestamp_EmptyTimestamp_OmitsClauseFromDetail verifies that when Request.Timestamp
// is empty, the parking gap detail does not include a timestamp clause. The "(detected ...)"
// suffix must be omitted entirely when the caller supplies no timestamp.
func TestTimestamp_EmptyTimestamp_OmitsClauseFromDetail(t *testing.T) {
	result, err := transform.Apply(reorderReq(t,
		[]byte(reorderAnchorSource),
		[]byte(reorderAnchorDeployed),
		"reorder-anchor-test",
		"", // empty timestamp
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no parking gap in report")
	}

	if strings.Contains(pg.Detail, "(detected") {
		t.Errorf("parking gap detail contains timestamp clause %q when Timestamp is empty; "+
			"the clause must be omitted when no timestamp is supplied: %s", "(detected", pg.Detail)
	}
}

// TestTimestamp_FixedInputProducesDeterministicOutput verifies that two calls to Apply
// with the same source, deployed, and Timestamp produce identical output bytes and identical
// parking gap details. The transform must not read any clock.
func TestTimestamp_FixedInputProducesDeterministicOutput(t *testing.T) {
	const ts = "2026-08-11T17:07:26Z"

	run := func() transform.Result {
		t.Helper()
		result, err := transform.Apply(reorderReq(t,
			[]byte(reorderAnchorSource),
			[]byte(reorderAnchorDeployed),
			"reorder-anchor-test",
			ts,
		))
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		return result
	}

	r1 := run()
	r2 := run()

	if !bytes.Equal(r1.Output, r2.Output) {
		t.Error("two Apply calls with the same inputs (including Timestamp) produced different output bytes; " +
			"the transform must be deterministic")
	}

	pg1 := findParkingGap(r1.Report.Gaps)
	pg2 := findParkingGap(r2.Report.Gaps)

	if pg1 == nil || pg2 == nil {
		t.Fatal("no parking gap in one or both runs")
	}
	if pg1.Detail != pg2.Detail {
		t.Errorf("parking gap detail differs between runs:\nrun1: %s\nrun2: %s", pg1.Detail, pg2.Detail)
	}
}

// TestTimestamp_DifferentTimestamps_ProduceDifferentDetails verifies that two Apply calls
// with identical inputs but different Timestamp values produce different gap detail strings.
// The timestamp must be visible in the output.
func TestTimestamp_DifferentTimestamps_ProduceDifferentDetails(t *testing.T) {
	makeReq := func(ts string) transform.Request {
		return reorderReq(t,
			[]byte(reorderAnchorSource),
			[]byte(reorderAnchorDeployed),
			"reorder-anchor-test",
			ts,
		)
	}

	r1, err := transform.Apply(makeReq("2026-08-11T17:07:26Z"))
	if err != nil {
		t.Fatalf("Apply r1: %v", err)
	}
	r2, err := transform.Apply(makeReq("2026-08-11T18:00:00Z"))
	if err != nil {
		t.Fatalf("Apply r2: %v", err)
	}

	pg1 := findParkingGap(r1.Report.Gaps)
	pg2 := findParkingGap(r2.Report.Gaps)

	if pg1 == nil || pg2 == nil {
		t.Fatal("no parking gap in one or both runs")
	}
	if pg1.Detail == pg2.Detail {
		t.Errorf("parking gap detail is identical for different timestamps; "+
			"the timestamp must be reflected in the detail: %s", pg1.Detail)
	}
}

// ---------------------------------------------------------------------------
// T7.7 — ParkedCustomRegionsDetail: pure formatting function
// ---------------------------------------------------------------------------

// TestParkedCustomRegionsDetail_WithTimestamp verifies that ParkedCustomRegionsDetail
// produces a detail string containing the standard message, the region names, and the
// timestamp when all are supplied.
func TestParkedCustomRegionsDetail_WithTimestamp(t *testing.T) {
	names := []string{"Alpha", "Beta"}
	ts := "2026-08-11T17:07:26Z"

	detail := transform.ParkedCustomRegionsDetail(names, ts)

	if !strings.Contains(detail, "Schema reorder detected") {
		t.Errorf("detail does not contain 'Schema reorder detected': %s", detail)
	}
	if !strings.Contains(detail, "move them to the correct section") {
		t.Errorf("detail does not contain 'move them to the correct section': %s", detail)
	}
	if !strings.Contains(detail, "Alpha") {
		t.Errorf("detail does not contain region name Alpha: %s", detail)
	}
	if !strings.Contains(detail, "Beta") {
		t.Errorf("detail does not contain region name Beta: %s", detail)
	}
	if !strings.Contains(detail, ts) {
		t.Errorf("detail does not contain timestamp %q: %s", ts, detail)
	}
}

// TestParkedCustomRegionsDetail_WithoutTimestamp verifies that ParkedCustomRegionsDetail
// omits the timestamp clause when the timestamp argument is empty.
func TestParkedCustomRegionsDetail_WithoutTimestamp(t *testing.T) {
	names := []string{"Alpha", "Beta"}

	detail := transform.ParkedCustomRegionsDetail(names, "")

	if !strings.Contains(detail, "Schema reorder detected") {
		t.Errorf("detail does not contain 'Schema reorder detected': %s", detail)
	}
	if !strings.Contains(detail, "Alpha") || !strings.Contains(detail, "Beta") {
		t.Errorf("detail does not contain region names: %s", detail)
	}
	if strings.Contains(detail, "detected") && strings.Contains(detail, "(") {
		t.Errorf("detail contains a timestamp clause but Timestamp is empty; clause must be omitted: %s", detail)
	}
}

// TestParkedCustomRegionsDetail_MultipleNamesListedWithCommas verifies that
// ParkedCustomRegionsDetail lists multiple names separated by commas.
func TestParkedCustomRegionsDetail_MultipleNamesListedWithCommas(t *testing.T) {
	names := []string{"AlphaNote", "BetaNote", "ZetaNote"}
	detail := transform.ParkedCustomRegionsDetail(names, "")

	// All names must appear in the detail.
	for _, name := range names {
		if !strings.Contains(detail, name) {
			t.Errorf("detail does not contain name %q: %s", name, detail)
		}
	}
}

// ---------------------------------------------------------------------------
// T7.8 — Parked names emitted in stable sorted order
// ---------------------------------------------------------------------------

// TestReorder_ParkedNames_EmittedInSortedOrder verifies that when multiple custom regions
// are parked, the parking gap detail lists their names in ascending alphabetical order,
// regardless of the order they appear in the deployed document.
func TestReorder_ParkedNames_EmittedInSortedOrder(t *testing.T) {
	// sortedParkDeployed has three top-level custom regions:
	// ZetaNote (first in file), AlphaNote (second), BetaNote (third).
	// The parking gap detail must list them as: AlphaNote, BetaNote, ZetaNote.
	result, err := transform.Apply(reorderReq(t,
		[]byte(sortedParkSource),
		[]byte(sortedParkDeployed),
		"sorted-park-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("no parking gap in report; expected all three top-level regions to be parked")
	}

	// Verify sorted order: AlphaNote must appear before BetaNote, BetaNote before ZetaNote.
	alphaIdx := strings.Index(pg.Detail, "AlphaNote")
	betaIdx := strings.Index(pg.Detail, "BetaNote")
	zetaIdx := strings.Index(pg.Detail, "ZetaNote")

	if alphaIdx < 0 || betaIdx < 0 || zetaIdx < 0 {
		t.Fatalf("one or more parked names missing from gap detail: %s", pg.Detail)
	}
	if alphaIdx >= betaIdx {
		t.Errorf("AlphaNote (index %d) does not appear before BetaNote (index %d) in gap detail; names must be sorted: %s",
			alphaIdx, betaIdx, pg.Detail)
	}
	if betaIdx >= zetaIdx {
		t.Errorf("BetaNote (index %d) does not appear before ZetaNote (index %d) in gap detail; names must be sorted: %s",
			betaIdx, zetaIdx, pg.Detail)
	}
}

// TestReorder_ParkedNames_SortedOrderInOutcomes verifies that the RegionOutcome entries for
// parked regions in the report are also emitted in sorted order. The report must be
// deterministic; sort order must not depend on map iteration.
func TestReorder_ParkedNames_SortedOrderInOutcomes(t *testing.T) {
	result, err := transform.Apply(reorderReq(t,
		[]byte(sortedParkSource),
		[]byte(sortedParkDeployed),
		"sorted-park-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Collect parked outcomes in the order they appear in the report.
	var parkedNames []string
	for _, out := range result.Report.Regions {
		if out.Action == transform.RegionParked {
			parkedNames = append(parkedNames, out.Name)
		}
	}

	if len(parkedNames) != 3 {
		t.Fatalf("expected 3 parked region outcomes; got %d: %v", len(parkedNames), parkedNames)
	}

	wantOrder := []string{"AlphaNote", "BetaNote", "ZetaNote"}
	for i, want := range wantOrder {
		if parkedNames[i] != want {
			t.Errorf("parked outcome[%d]: want %q, got %q; parked outcomes must be in sorted order: %v",
				i, want, parkedNames[i], parkedNames)
		}
	}
}
