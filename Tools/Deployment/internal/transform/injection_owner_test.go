package transform_test

// injection_owner_test.go covers gap owner attribution (T8.4).
//
// Every domain.Gap produced by transform.Apply for a project-region event must carry Owner
// equal to req.Key, so the TODO renderer can group items by the agent they belong to.
//
// The gaps covered are:
//
//   - GapEmptyInjection: new deploy, project region is empty in source.
//   - GapDefaultContentDeployed: new deploy, project region has non-empty default in source.
//   - GapRemovedInjection: update deploy, injection was in the deployed file but absent from source.
//   - GapInjectionReparented: update deploy, injection moved to a different parent section.
//   - GapParkedCustomRegion: update deploy, schema reorder orphans a top-level custom region.
//   - GapCustomRegionFallthrough: update deploy, custom region's parent section is removed.
//   - GapDeployedRegionContentChanged: update deploy, managed region content changed with nested user regions.
//
// In all cases the test asserts gap.Owner == req.Key. These tests fail (RED) until the
// implementation sets Owner: req.Key at each gap production site.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Source document fixtures for owner tests
// ---------------------------------------------------------------------------

// ownerTestAgentKey is the req.Key value used throughout the owner attribution tests. Every
// produced gap must carry exactly this value as Owner.
const ownerTestAgentKey = "my-attribution-agent"

// srcWithEmptyProject is a minimal source document with one empty project region
// (CodebaseContext). On first deploy (Deployed == nil) this produces a GapEmptyInjection.
const srcWithEmptyProject = `---
id: 100
version: 1.0.0
name: owner-attribution-test
description: Agent for owner attribution testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Capabilities type="core">
## Capabilities

<CodebaseContext type="project">
</CodebaseContext>
</Capabilities>
`

// srcWithNonEmptyProject is a minimal source document with a non-empty project region.
// On first deploy this produces GapDefaultContentDeployed.
const srcWithNonEmptyProject = `---
id: 100
version: 1.0.0
name: owner-attribution-test
description: Agent for owner attribution testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Capabilities type="core">
## Capabilities

<CodebaseContext type="project">
Default content authored by the agent designer.
</CodebaseContext>
</Capabilities>
`

// srcWithoutCodebaseContext is a source that has no CodebaseContext region. When paired with
// a deployed file that HAS CodebaseContext, this produces GapRemovedInjection.
const srcWithoutCodebaseContext = `---
id: 100
version: 1.0.0
name: owner-attribution-test
description: Agent for owner attribution testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Capabilities type="core">
## Capabilities

</Capabilities>
`

// deployedWithCodebaseContextUserContent is a deployed file containing a user-populated
// CodebaseContext project region. Paired with srcWithoutCodebaseContext this triggers
// GapRemovedInjection for CodebaseContext.
const deployedWithCodebaseContextUserContent = `---
id: 100
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for owner attribution testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Capabilities type="core">
## Capabilities

<CodebaseContext type="project">
User-authored content that belongs to this agent.
</CodebaseContext>
</Capabilities>
`

// srcWithReparentedCodebaseContext is a source where CodebaseContext has moved from being
// nested inside Capabilities to being at the top level of the body. Paired with
// deployedWithCodebaseContextUserContent this triggers GapInjectionReparented.
const srcWithReparentedCodebaseContext = `---
id: 100
version: 1.0.0
name: owner-attribution-test
description: Agent for owner attribution testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<CodebaseContext type="project">
</CodebaseContext>

<Capabilities type="core">
## Capabilities

</Capabilities>
`

// ---------------------------------------------------------------------------
// T8.4: GapEmptyInjection carries Owner == req.Key
// ---------------------------------------------------------------------------

// TestGapOwner_EmptyInjection_CarriesReqKey verifies that when a project region is empty on
// first deploy, the GapEmptyInjection produced has Owner == req.Key.
func TestGapOwner_EmptyInjection_CarriesReqKey(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithEmptyProject),
		Kind:   domain.ArtifactAgent,
		Key:    ownerTestAgentKey,
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		// Deployed is nil — first deployment.
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapEmptyInjection)
	if gap == nil {
		t.Fatalf("expected GapEmptyInjection; gaps produced: %+v", result.Report.Gaps)
	}
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("GapEmptyInjection.Owner = %q; want %q (req.Key)", gap.Owner, ownerTestAgentKey)
	}
}

// TestGapOwner_EmptyInjection_SubjectIsRegionName verifies that the GapEmptyInjection's
// Subject still names the region (not the agent), confirming Owner and Subject carry
// different information.
func TestGapOwner_EmptyInjection_SubjectIsRegionName(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithEmptyProject),
		Kind:   domain.ArtifactAgent,
		Key:    ownerTestAgentKey,
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapEmptyInjection)
	if gap == nil {
		t.Fatalf("expected GapEmptyInjection")
	}
	if gap.Subject != "CodebaseContext" {
		t.Errorf("GapEmptyInjection.Subject = %q; want %q (region name)", gap.Subject, "CodebaseContext")
	}
	// Owner must still be set to the agent key.
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("GapEmptyInjection.Owner = %q; want %q", gap.Owner, ownerTestAgentKey)
	}
}

// ---------------------------------------------------------------------------
// T8.4: GapDefaultContentDeployed carries Owner == req.Key
// ---------------------------------------------------------------------------

// TestGapOwner_DefaultContentDeployed_CarriesReqKey verifies that when a project region
// carries non-empty default content on first deploy, the GapDefaultContentDeployed produced
// has Owner == req.Key.
func TestGapOwner_DefaultContentDeployed_CarriesReqKey(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithNonEmptyProject),
		Kind:   domain.ArtifactAgent,
		Key:    ownerTestAgentKey,
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		// Deployed is nil — first deployment.
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapDefaultContentDeployed)
	if gap == nil {
		t.Fatalf("expected GapDefaultContentDeployed; gaps produced: %+v", result.Report.Gaps)
	}
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("GapDefaultContentDeployed.Owner = %q; want %q (req.Key)", gap.Owner, ownerTestAgentKey)
	}
}

// ---------------------------------------------------------------------------
// T8.4: GapRemovedInjection carries Owner == req.Key
// ---------------------------------------------------------------------------

// TestGapOwner_RemovedInjection_CarriesReqKey verifies that when a project region that
// existed in the deployed file is absent from the source, the GapRemovedInjection produced
// has Owner == req.Key.
func TestGapOwner_RemovedInjection_CarriesReqKey(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithoutCodebaseContext),
		Deployed: []byte(deployedWithCodebaseContextUserContent),
		Kind:     domain.ArtifactAgent,
		Key:      ownerTestAgentKey,
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapRemovedInjection)
	if gap == nil {
		t.Fatalf("expected GapRemovedInjection; gaps produced: %+v", result.Report.Gaps)
	}
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("GapRemovedInjection.Owner = %q; want %q (req.Key)", gap.Owner, ownerTestAgentKey)
	}
}

// ---------------------------------------------------------------------------
// T8.4: GapInjectionReparented carries Owner == req.Key
// ---------------------------------------------------------------------------

// TestGapOwner_InjectionReparented_CarriesReqKey verifies that when an injection region
// moves to a different parent section between deploys, the GapInjectionReparented produced
// has Owner == req.Key.
func TestGapOwner_InjectionReparented_CarriesReqKey(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithReparentedCodebaseContext),
		Deployed: []byte(deployedWithCodebaseContextUserContent),
		Kind:     domain.ArtifactAgent,
		Key:      ownerTestAgentKey,
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapInjectionReparented)
	if gap == nil {
		t.Fatalf("expected GapInjectionReparented; gaps produced: %+v", result.Report.Gaps)
	}
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("GapInjectionReparented.Owner = %q; want %q (req.Key)", gap.Owner, ownerTestAgentKey)
	}
}

// ---------------------------------------------------------------------------
// T8.4: All project-region gaps carry the same Owner from one Apply call
// ---------------------------------------------------------------------------

// TestGapOwner_AllGapsCarryReqKey verifies that every gap produced by a single Apply call
// has Owner == req.Key. This is the aggregate assertion: regardless of gap kind, the
// owning agent is always the one identified by req.Key.
func TestGapOwner_AllGapsCarryReqKey(t *testing.T) {
	// Use a source that produces multiple gap kinds in one call: the empty-project-region
	// fixture on first deploy produces exactly GapEmptyInjection.
	req := transform.Request{
		Source: []byte(srcWithEmptyProject),
		Kind:   domain.ArtifactAgent,
		Key:    "aggregate-owner-check",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for i, gap := range result.Report.Gaps {
		if gap.Owner != "aggregate-owner-check" {
			t.Errorf("gap[%d] (kind=%q, subject=%q): Owner = %q; want %q (req.Key)",
				i, gap.Kind, gap.Subject, gap.Owner, "aggregate-owner-check")
		}
	}
}

// TestGapOwner_DifferentAgentKeys_CarryDistinctOwners verifies that two Apply calls with
// different req.Key values produce gaps with correspondingly different Owner values. This
// confirms Owner is derived from req.Key, not from a global or shared state.
func TestGapOwner_DifferentAgentKeys_CarryDistinctOwners(t *testing.T) {
	makeReq := func(key string) transform.Request {
		return transform.Request{
			Source: []byte(srcWithEmptyProject),
			Kind:   domain.ArtifactAgent,
			Key:    key,
			Module: newFixtureModule(t),
			Model:  fixtureModel(),
			Scope:  domain.ScopeProject,
		}
	}

	resultA, err := transform.Apply(makeReq("agent-alpha"))
	if err != nil {
		t.Fatalf("Apply (agent-alpha): %v", err)
	}
	resultB, err := transform.Apply(makeReq("agent-beta"))
	if err != nil {
		t.Fatalf("Apply (agent-beta): %v", err)
	}

	gapA := findProjectGapOfKind(resultA, domain.GapEmptyInjection)
	gapB := findProjectGapOfKind(resultB, domain.GapEmptyInjection)

	if gapA == nil {
		t.Fatal("expected GapEmptyInjection from agent-alpha apply")
	}
	if gapB == nil {
		t.Fatal("expected GapEmptyInjection from agent-beta apply")
	}
	if gapA.Owner != "agent-alpha" {
		t.Errorf("agent-alpha: GapEmptyInjection.Owner = %q; want %q", gapA.Owner, "agent-alpha")
	}
	if gapB.Owner != "agent-beta" {
		t.Errorf("agent-beta: GapEmptyInjection.Owner = %q; want %q", gapB.Owner, "agent-beta")
	}
}

// ---------------------------------------------------------------------------
// Source/deployed fixtures for GapParkedCustomRegion owner test
// ---------------------------------------------------------------------------

// ownerParkSource has sections Constraints → Capabilities. The deployed file has them
// reversed (Capabilities → Constraints), so a schema reorder is detected. The top-level
// custom region in the deployed file (no parent section) is then parked.
const ownerParkSource = `---
id: 200
version: 2.0.0
name: owner-park-test
description: Agent for owner attribution parking tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>
`

// ownerParkDeployed has sections Capabilities → Constraints (reversed vs ownerParkSource),
// triggering reorder detection. AgentNotes is a top-level custom region with no parent;
// it will be parked when a reorder is detected, producing GapParkedCustomRegion.
const ownerParkDeployed = `---
id: 200
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for owner attribution parking tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>

<AgentNotes type="custom">
Top-level custom note with no parent section. Parked when a schema reorder is detected.
</AgentNotes>
`

// ---------------------------------------------------------------------------
// T8.4: GapParkedCustomRegion carries Owner == req.Key
// ---------------------------------------------------------------------------

// TestGapOwner_ParkedCustomRegion_CarriesReqKey verifies that when a schema reorder orphans
// a top-level custom region (no surviving anchor), the GapParkedCustomRegion produced has
// Owner == req.Key.
func TestGapOwner_ParkedCustomRegion_CarriesReqKey(t *testing.T) {
	req := reorderReq(t,
		[]byte(ownerParkSource),
		[]byte(ownerParkDeployed),
		ownerTestAgentKey,
		"",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapParkedCustomRegion)
	if gap == nil {
		t.Fatalf("expected GapParkedCustomRegion; gaps produced: %+v", result.Report.Gaps)
	}
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("GapParkedCustomRegion.Owner = %q; want %q (req.Key)", gap.Owner, ownerTestAgentKey)
	}
}

// ---------------------------------------------------------------------------
// Source/deployed fixtures for GapCustomRegionFallthrough owner test
// ---------------------------------------------------------------------------

// ownerFallthroughSource removes OldSection from the document. The remaining sections
// Identity and Capabilities appear in the same relative order as in ownerFallthroughDeployed
// (Identity first, Capabilities second), so no reorder is detected. OldNote's parent is
// gone → custom-region fallthrough.
const ownerFallthroughSource = `---
id: 201
version: 2.0.0
name: owner-fallthrough-test
description: Agent for owner attribution fallthrough tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Identity type="core">
# OwnerFallthrough Agent

You are the OwnerFallthrough agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>
`

// ownerFallthroughDeployed has Identity → OldSection → Capabilities. OldSection is absent
// from ownerFallthroughSource; Identity and Capabilities remain in the same relative order →
// no reorder. OldNote, nested in OldSection, loses its parent and falls through to body level,
// producing GapCustomRegionFallthrough.
const ownerFallthroughDeployed = `---
id: 201
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for owner attribution fallthrough tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# OwnerFallthrough Agent

You are the OwnerFallthrough agent.

<IdentityExtension type="project">
User-authored identity content.
</IdentityExtension>
</Identity>

<OldSection type="core">
## Old Section

This section is removed from ownerFallthroughSource.

<OldNote type="custom">
User note anchored to OldSection — falls through when parent section is removed.
</OldNote>
</OldSection>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.
</Capabilities>
`

// ---------------------------------------------------------------------------
// T8.4: GapCustomRegionFallthrough carries Owner == req.Key
// ---------------------------------------------------------------------------

// TestGapOwner_CustomRegionFallthrough_CarriesReqKey verifies that when a custom region's
// parent section is removed from the source (with no reorder among surviving sections), the
// GapCustomRegionFallthrough produced has Owner == req.Key.
func TestGapOwner_CustomRegionFallthrough_CarriesReqKey(t *testing.T) {
	req := reorderReq(t,
		[]byte(ownerFallthroughSource),
		[]byte(ownerFallthroughDeployed),
		ownerTestAgentKey,
		"",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapCustomRegionFallthrough)
	if gap == nil {
		t.Fatalf("expected GapCustomRegionFallthrough; gaps produced: %+v", result.Report.Gaps)
	}
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("GapCustomRegionFallthrough.Owner = %q; want %q (req.Key)", gap.Owner, ownerTestAgentKey)
	}
}

// ---------------------------------------------------------------------------
// T8.4: GapDeployedRegionContentChanged carries Owner == req.Key
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// T8.4: GapRemovedInjection from rename-collision path carries Owner == req.Key
// ---------------------------------------------------------------------------

// TestGapOwner_RenameCollision_RemovedInjection_CarriesReqKey verifies that when both the
// old and new name of a rename entry are present in the deployed file, the GapRemovedInjection
// produced for the old name's content carries Owner == req.Key. This exercises the rename-
// collision path inside buildDeployedRegionMap, which has no Request parameter; Owner must
// be patched at the Apply call site.
func TestGapOwner_RenameCollision_RemovedInjection_CarriesReqKey(t *testing.T) {
	req := transform.Request{
		Source:           []byte(renameSource),
		Deployed:         []byte(renameDeployedBothNames),
		Kind:             domain.ArtifactAgent,
		Key:              ownerTestAgentKey,
		Module:           newFixtureModule(t),
		Model:            fixtureModel(),
		Scope:            domain.ScopeProject,
		InjectionRenames: []transform.RenameEntry{{Old: "CapabilityDesc", New: "NewCapabilityDesc"}},
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapRemovedInjection)
	if gap == nil {
		t.Fatalf("expected GapRemovedInjection from rename-collision path; gaps produced: %+v", result.Report.Gaps)
	}
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("rename-collision GapRemovedInjection.Owner = %q; want %q (req.Key)", gap.Owner, ownerTestAgentKey)
	}
}

// TestGapOwner_DeployedRegionContentChanged_CarriesReqKey verifies that when a managed
// region's canonical content changes relative to the previously deployed file and the
// region contains nested user-owned regions, the GapDeployedRegionContentChanged produced
// has Owner == req.Key.
//
// This test uses the contentChangedSource and contentChangedDeployed fixtures from
// deployed_region_content_changed_test.go (same test package). contentChangedDeployed has
// stale content in <HarnessConstraints type="managed"> plus a nested <ConstraintsNote
// type="custom">, which satisfies both conditions for gap emission.
func TestGapOwner_DeployedRegionContentChanged_CarriesReqKey(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		ownerTestAgentKey,
		"",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapDeployedRegionContentChanged)
	if gap == nil {
		t.Fatalf("expected GapDeployedRegionContentChanged; gaps produced: %+v", result.Report.Gaps)
	}
	if gap.Owner != ownerTestAgentKey {
		t.Errorf("GapDeployedRegionContentChanged.Owner = %q; want %q (req.Key)", gap.Owner, ownerTestAgentKey)
	}
}
