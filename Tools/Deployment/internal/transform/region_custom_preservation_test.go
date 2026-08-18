package transform_test

// region_custom_preservation_test.go covers custom region provenance and preservation
// during a normal (no-reorder) update:
//
//   - The deployed region map must include custom-region regions alongside project-injection region
//     regions, keyed by name with byte-identical content, each entry carrying the
//     originating marker kind. managed region regions remain excluded from the map.
//   - A custom-region region present in the deployed file survives a normal update
//     byte-identically at its existing position — both at body top level and when nested
//     inside a section that still exists in the source.
//   - A genuinely removed project-injection region name still produces RegionOrphaned and
//     GapRemovedInjection, unchanged. A custom-region region never produces RegionOrphaned
//     and never routes to GapRemovedInjection.
//   - The RegionOutcome for a preserved custom region carries Marker = NodeCustom,
//     not NodeInjection.
//   - When no deployed file exists (new deployment), there are no custom regions to
//     preserve and existing injection behavior is unchanged.

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Shared fixture documents for custom region preservation tests.
//
// customPreservationSource is a generic source with Identity and Constraints sections.
// It declares one project injection (IdentityExtension) and one tool-managed deployed
// region (HarnessConstraints). It has no custom-region regions — custom regions exist only
// in deployed files, never in source files.
// ---------------------------------------------------------------------------

const customPreservationSource = `---
id: 100
version: 2.0.0
name: custom-preservation-test
description: Agent for custom region preservation testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: custom preservation testing
required_skills: []
---

<Identity type="core">
# CustomPreservationTest Agent

You are the CustomPreservationTest agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

// customPreservationDeployed is the deployed predecessor of customPreservationSource.
// It has filled injection content and two custom-region regions added by the project:
//   - <IdentityCustom type="custom"> nested inside <Identity type="core">, immediately after
//     <IdentityExtension type="project"> inside the Identity section
//   - <ProjectNotes type="custom"> at body top level, after the Constraints section
//
// Both custom regions must survive a normal update byte-identically.
const customPreservationDeployed = `---
id: 100
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for custom region preservation testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# CustomPreservationTest Agent

You are the CustomPreservationTest agent.

<IdentityExtension type="project">
User identity extension content that must survive byte-identically.
</IdentityExtension>

<IdentityCustom type="custom">
This is a project-specific custom note inside the Identity section.
It must survive the update and remain nested inside Identity.
</IdentityCustom>
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Old harness content that will be replaced on every transform.
</HarnessConstraints>
</Constraints>

<ProjectNotes type="custom">
These are project-specific notes at the top level.
They span multiple lines and must survive byte-identically.
</ProjectNotes>
`

// customOrphanTestSource is a minimal source with no injection or custom regions that
// correspond to the deployed file below. It is used to set up both an orphaned injection
// (which must still produce RegionOrphaned and GapRemovedInjection) and a custom region
// (which must be preserved, never orphaned).
const customOrphanTestSource = `---
id: 101
version: 2.0.0
name: custom-orphan-test
description: Agent for custom orphan-branch testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: custom orphan testing
required_skills: []
---

<Identity type="core">
# CustomOrphanTest Agent

You are the CustomOrphanTest agent.
</Identity>
`

// customOrphanTestDeployed is the deployed predecessor of customOrphanTestSource.
// It contains:
//   - <RemovedInjection type="project"> with user content, absent from the source (must orphan)
//   - <ProjectNotes type="custom"> with user content (must never orphan, must be preserved)
const customOrphanTestDeployed = `---
id: 101
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for custom orphan-branch testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# CustomOrphanTest Agent

You are the CustomOrphanTest agent.

<RemovedInjection type="project">
This injection content will be orphaned because RemovedInjection is gone from the source.
User content that must appear in the GapRemovedInjection fragment.
</RemovedInjection>
</Identity>

<ProjectNotes type="custom">
Project-specific notes that must never be reported as orphaned.
This content must be preserved in the output, not discarded or gapped.
</ProjectNotes>
`

// ---------------------------------------------------------------------------
// Region map: custom regions are included with byte-identical content and provenance
// ---------------------------------------------------------------------------

// TestRegionMap_CustomRegion_IncludedWithByteIdenticalContent asserts that a custom-region
// region present in the deployed file is included in the deployed region map and its content
// is carried into the output byte-identically. Since buildDeployedRegionMap is unexported,
// this is observed through the transform output: the custom region must appear in the output
// with content identical to the deployed file.
func TestRegionMap_CustomRegion_IncludedWithByteIdenticalContent(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: []byte(customPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Parse the deployed file to obtain the expected content for each custom region.
	depDoc, err := docformat.Parse([]byte(customPreservationDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}

	// Parse the output to verify custom regions survived.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	customNames := []string{"IdentityCustom", "ProjectNotes"}
	for _, name := range customNames {
		name := name
		t.Run(name, func(t *testing.T) {
			depNode, depOK := depDoc.Body().Custom(name)
			if !depOK {
				t.Fatalf("Custom(%q) not found in deployed fixture; fixture is malformed", name)
			}

			outNode, outOK := outDoc.Body().Custom(name)
			if !outOK {
				t.Fatalf("Custom(%q) absent from output; custom regions from the deployed file must be carried into the output", name)
			}

			if !bytes.Equal(outNode.Content(), depNode.Content()) {
				t.Errorf("Custom(%q) content is not byte-identical to the deployed file:\ndeployed: %q\noutput:   %q",
					name, depNode.Content(), outNode.Content())
			}
		})
	}
}

// TestRegionMap_DeployedRegions_RemainsExcluded asserts that managed region regions in the
// deployed file are NOT lifted into the output — they are regenerated on every transform.
// This verifies that the map's exclusion of tool-managed content is unchanged by the Stage 5
// change that adds custom region inclusion.
func TestRegionMap_DeployedRegions_RemainsExcluded(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: []byte(customPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The deployed HarnessConstraints contained stale content; the output must not carry it.
	// Tool-managed regions are regenerated, never lifted from the deployed file.
	if bytes.Contains(result.Output, []byte("Old harness content that will be replaced on every transform.")) {
		t.Error("output contains stale deployed-region content from the deployed file; " +
			"managed region regions must be regenerated, not lifted from the deployed map")
	}
}

// ---------------------------------------------------------------------------
// Normal update: custom regions survive byte-identically at their existing position
// ---------------------------------------------------------------------------

// TestNormalUpdate_CustomRegion_TopLevel_SurvivesByteIdenticallyAtTopLevel asserts that a
// custom-region region at body top level in the deployed file is present in the output after a
// normal update, with byte-identical content, and remains at the top level of the body (not
// moved inside a section).
func TestNormalUpdate_CustomRegion_TopLevel_SurvivesByteIdenticallyAtTopLevel(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: []byte(customPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(customPreservationDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// Verify content byte-identity.
	depNode, depOK := depDoc.Body().Custom("ProjectNotes")
	if !depOK {
		t.Fatal("ProjectNotes not found in deployed fixture; fixture is malformed")
	}
	outNode, outOK := outDoc.Body().Custom("ProjectNotes")
	if !outOK {
		t.Fatal("ProjectNotes absent from output; top-level custom region must be preserved on a normal update")
	}
	if !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("ProjectNotes content not byte-identical to deployed:\ndeployed: %q\noutput:   %q",
			depNode.Content(), outNode.Content())
	}

	// Verify position: a top-level custom region must have a nil parent.
	if outNode.Parent() != nil {
		t.Errorf("ProjectNotes is at body top level in the deployed file but was placed inside a %q node in the output; "+
			"its position must not change on a normal update", outNode.Parent().Name())
	}
}

// TestNormalUpdate_CustomRegion_NestedInSection_SurvivesByteIdenticallyInsideSection asserts
// that a custom-region region nested inside a core-section region in the deployed file is present in
// the output after a normal update, with byte-identical content, and remains nested inside
// the same section by name (not relocated to the body top level or another section).
func TestNormalUpdate_CustomRegion_NestedInSection_SurvivesByteIdenticallyInsideSection(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: []byte(customPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(customPreservationDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// Verify content byte-identity.
	depNode, depOK := depDoc.Body().Custom("IdentityCustom")
	if !depOK {
		t.Fatal("IdentityCustom not found in deployed fixture; fixture is malformed")
	}
	outNode, outOK := outDoc.Body().Custom("IdentityCustom")
	if !outOK {
		t.Fatal("IdentityCustom absent from output; section-nested custom region must be preserved on a normal update")
	}
	if !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("IdentityCustom content not byte-identical to deployed:\ndeployed: %q\noutput:   %q",
			depNode.Content(), outNode.Content())
	}

	// Verify position: IdentityCustom must remain nested inside the Identity section.
	parent := outNode.Parent()
	if parent == nil {
		t.Fatal("IdentityCustom is at body top level in the output; it must remain nested inside the Identity section")
	}
	if parent.Kind() != docformat.NodeSection {
		t.Errorf("IdentityCustom parent kind: want NodeSection, got %q", parent.Kind())
	}
	if parent.Name() != "Identity" {
		t.Errorf("IdentityCustom parent name: want %q, got %q", "Identity", parent.Name())
	}
}

// ---------------------------------------------------------------------------
// Orphan branch: removed injection still orphans; custom region never does
// ---------------------------------------------------------------------------

// TestOrphanBranch_RemovedInjection_StillProducesOrphanedOutcomeAndGap asserts that the
// Stage 5 provenance changes do not alter the behavior for a genuinely removed project-injection region
// region. It must still produce RegionOrphaned and GapRemovedInjection, unchanged.
func TestOrphanBranch_RemovedInjection_StillProducesOrphanedOutcomeAndGap(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customOrphanTestSource),
		Deployed: []byte(customOrphanTestDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-orphan-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A GapRemovedInjection gap must be emitted for the removed injection.
	var removedGap *domain.Gap
	for i := range result.Report.Gaps {
		if result.Report.Gaps[i].Kind == domain.GapRemovedInjection &&
			result.Report.Gaps[i].Subject == "RemovedInjection" {
			removedGap = &result.Report.Gaps[i]
			break
		}
	}
	if removedGap == nil {
		t.Fatalf("expected GapRemovedInjection for RemovedInjection; gaps: %v", result.Report.Gaps)
	}
	if removedGap.Fragment == "" {
		t.Error("GapRemovedInjection.Fragment must carry the orphaned content; it is empty")
	}

	// The report must record RegionOrphaned for the removed injection.
	var orphanedOutcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "RemovedInjection" {
			orphanedOutcome = &result.Report.Regions[i]
			break
		}
	}
	if orphanedOutcome == nil {
		t.Fatalf("RegionOutcome for RemovedInjection absent; report: %v", result.Report.Regions)
	}
	if orphanedOutcome.Action != transform.RegionOrphaned {
		t.Errorf("RemovedInjection action: want %q, got %q", transform.RegionOrphaned, orphanedOutcome.Action)
	}
}

// TestOrphanBranch_CustomRegion_NeverProducesOrphanedOutcome asserts that a custom-region
// region in the deployed file never produces a RegionOrphaned outcome. Custom regions
// are preserved in the output at their anchored position, not discarded via the orphan path.
func TestOrphanBranch_CustomRegion_NeverProducesOrphanedOutcome(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customOrphanTestSource),
		Deployed: []byte(customOrphanTestDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-orphan-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// No RegionOrphaned outcome for ProjectNotes.
	for _, outcome := range result.Report.Regions {
		if outcome.Name == "ProjectNotes" && outcome.Action == transform.RegionOrphaned {
			t.Errorf("custom region ProjectNotes must never produce a RegionOrphaned outcome; "+
				"custom regions are preserved, not discarded: %+v", outcome)
		}
	}

	// No GapRemovedInjection gap for ProjectNotes.
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapRemovedInjection && g.Subject == "ProjectNotes" {
			t.Errorf("custom region ProjectNotes must not produce a GapRemovedInjection gap; "+
				"only genuinely removed project-injection region names route to that gap: %+v", g)
		}
	}

	// ProjectNotes must have RegionCustomPreserved action (the positive preservation assertion).
	var customOutcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "ProjectNotes" {
			customOutcome = &result.Report.Regions[i]
			break
		}
	}
	if customOutcome == nil {
		t.Fatal("RegionOutcome for ProjectNotes absent from report; a preserved custom region must appear in the report")
	}
	if customOutcome.Action != transform.RegionCustomPreserved {
		t.Errorf("ProjectNotes action: want %q, got %q", transform.RegionCustomPreserved, customOutcome.Action)
	}
}

// TestOrphanBranch_MixedDeployed_InjectionOrphansAndCustomPreserved asserts that when the
// deployed file contains both a removed injection and a custom region, the injection is
// orphaned and the custom region is preserved — the two paths do not interfere.
func TestOrphanBranch_MixedDeployed_InjectionOrphansAndCustomPreserved(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customOrphanTestSource),
		Deployed: []byte(customOrphanTestDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-orphan-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Verify the injection is orphaned (GapRemovedInjection exists).
	var hasInjectionGap bool
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapRemovedInjection && g.Subject == "RemovedInjection" {
			hasInjectionGap = true
			break
		}
	}
	if !hasInjectionGap {
		t.Error("removed injection RemovedInjection must produce a GapRemovedInjection gap")
	}

	// Verify the custom region is preserved in the output (not orphaned, not gapped).
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	_, customOK := outDoc.Body().Custom("ProjectNotes")
	if !customOK {
		t.Error("custom region ProjectNotes must appear in the output when the deployed file contains it; " +
			"it must not be dropped even when a sibling injection is orphaned")
	}

	// Verify no injection gap was created for the custom region.
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapRemovedInjection && g.Subject == "ProjectNotes" {
			t.Errorf("custom region ProjectNotes must not produce a GapRemovedInjection gap; gap: %+v", g)
		}
	}
}

// ---------------------------------------------------------------------------
// Outcome reporting: custom region outcome carries the custom marker kind
// ---------------------------------------------------------------------------

// TestOutcomeMarker_CustomRegion_ReportsNodeCustomNotNodeInjection asserts that the
// RegionOutcome for a preserved custom-region region carries Marker = NodeCustom. The
// orphan branch must not hard-code NodeInjection for all map entries; it must use the
// entry's recorded marker kind.
func TestOutcomeMarker_CustomRegion_ReportsNodeCustomNotNodeInjection(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: []byte(customPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	customNames := []string{"IdentityCustom", "ProjectNotes"}
	for _, name := range customNames {
		name := name
		t.Run(name, func(t *testing.T) {
			var outcome *transform.RegionOutcome
			for i := range result.Report.Regions {
				if result.Report.Regions[i].Name == name {
					outcome = &result.Report.Regions[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("RegionOutcome for %q absent from report; a preserved custom region must appear in the report", name)
			}
			if outcome.Marker != docformat.NodeCustom {
				t.Errorf("%s outcome Marker: want NodeCustom (%q), got %q",
					name, docformat.NodeCustom, outcome.Marker)
			}
			if outcome.Marker == docformat.NodeInjection {
				t.Errorf("%s outcome Marker is NodeInjection; the orphan branch must not hard-code NodeInjection — "+
					"it must use the entry's recorded kind from the region map", name)
			}
		})
	}
}

// TestOutcomeMarker_CustomRegion_ActionIsCustomPreserved asserts that the action reported
// for a preserved custom region is RegionCustomPreserved, not RegionPreserved (which is for
// lifted injection content) and not RegionOrphaned.
func TestOutcomeMarker_CustomRegion_ActionIsCustomPreserved(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: []byte(customPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, name := range []string{"IdentityCustom", "ProjectNotes"} {
		name := name
		t.Run(name, func(t *testing.T) {
			var outcome *transform.RegionOutcome
			for i := range result.Report.Regions {
				if result.Report.Regions[i].Name == name {
					outcome = &result.Report.Regions[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("RegionOutcome for %q absent from report", name)
			}
			if outcome.Action != transform.RegionCustomPreserved {
				t.Errorf("%s action: want %q, got %q", name, transform.RegionCustomPreserved, outcome.Action)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// New deployment: no custom regions to preserve, existing behavior unchanged
// ---------------------------------------------------------------------------

// TestNewDeployment_NilDeployed_NoCustomRegionsInOutput asserts that when there is no
// deployed file (new deployment), no custom regions appear in the output. Custom regions
// exist only in deployed files; a source-only deployment produces none.
func TestNewDeployment_NilDeployed_NoCustomRegionsInOutput(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: nil, // new deployment — no deployed file
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply returned an error on a new deployment with no custom regions: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	customs := outDoc.Body().CustomRegions()
	if len(customs) != 0 {
		names := make([]string, len(customs))
		for i, n := range customs {
			names[i] = n.Name()
		}
		t.Errorf("new deployment must produce no custom regions in the output; found: %v", names)
	}
}

// TestOutcomeBytes_CustomPreservedRegion_CarriesContentLength asserts that the Bytes field
// of a RegionOutcome for a preserved custom-region region equals the byte length of the content
// that was placed in the output. Every other outcome-producing path in injection.go sets Bytes,
// and the custom preservation path must do the same.
func TestOutcomeBytes_CustomPreservedRegion_CarriesContentLength(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: []byte(customPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Parse the deployed file to get expected content lengths.
	depDoc, err := docformat.Parse([]byte(customPreservationDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}

	customNames := []string{"IdentityCustom", "ProjectNotes"}
	for _, name := range customNames {
		name := name
		t.Run(name, func(t *testing.T) {
			depNode, depOK := depDoc.Body().Custom(name)
			if !depOK {
				t.Fatalf("Custom(%q) not found in deployed fixture; fixture is malformed", name)
			}
			expectedBytes := len(depNode.Content())

			var outcome *transform.RegionOutcome
			for i := range result.Report.Regions {
				if result.Report.Regions[i].Name == name {
					outcome = &result.Report.Regions[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("RegionOutcome for %q absent from report", name)
			}
			if outcome.Bytes != expectedBytes {
				t.Errorf("%s outcome Bytes: want %d (content length from deployed file), got %d; "+
					"every outcome-producing path must record the byte length of the placed content",
					name, expectedBytes, outcome.Bytes)
			}
		})
	}
}

// TestNewDeployment_NilDeployed_ExistingInjectionBehaviorUnchanged asserts that a new
// deployment with no deployed file continues to produce empty project injections with
// GapEmptyInjection gaps, and does not regress due to Stage 5 changes.
func TestNewDeployment_NilDeployed_ExistingInjectionBehaviorUnchanged(t *testing.T) {
	req := transform.Request{
		Source:   []byte(customPreservationSource),
		Deployed: nil, // new deployment
		Kind:     domain.ArtifactAgent,
		Key:      "custom-preservation-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// IdentityExtension must be empty on a new deployment (no user has filled it yet).
	injNode, injOK := outDoc.Body().Injection("IdentityExtension")
	if !injOK {
		t.Fatal("IdentityExtension injection absent from output; it must be present since it is in the source")
	}
	if !injNode.IsEmpty() {
		t.Errorf("IdentityExtension must be empty on a new deployment; got content: %q", injNode.Content())
	}

	// GapEmptyInjection must be present for IdentityExtension.
	var hasGap bool
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapEmptyInjection && g.Subject == "IdentityExtension" {
			hasGap = true
			break
		}
	}
	if !hasGap {
		t.Errorf("expected GapEmptyInjection for IdentityExtension on new deployment; gaps: %v", result.Report.Gaps)
	}
}

// ---------------------------------------------------------------------------
// Name-collision hard error: both collision shapes abort with ErrRegionNameCollision
// ---------------------------------------------------------------------------
//
// Two shapes must abort the transform with ErrRegionNameCollision:
//
//   Shape A — duplicate custom-region name: the same name appears twice in the deployed file.
//   Shape B — custom-vs-injection: a name appears as custom-region in the deployed file and
//             project-injection region in the source.
//
// In both cases the transform must:
//   - Return an error wrapping ErrRegionNameCollision.
//   - Include the colliding name in the error text so the user can locate it.
//   - Produce no output (mutate nothing before the error is raised).
//
// A non-colliding document where a custom-region name and an project-injection region name are merely
// different must succeed without error.
// ---------------------------------------------------------------------------

// collisionSourceMinimal is a minimal source document used as the paired source for
// the duplicate-custom collision fixture. It declares no injection named OverlapName,
// so the only collision comes from the deployed file's duplicate custom-region declarations.
const collisionSourceMinimal = `---
id: 200
version: 2.0.0
name: collision-duplicate-custom-test
description: Agent for duplicate-custom collision testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: collision testing
required_skills: []
---

<Identity type="core">
# CollisionTest Agent

You are the CollisionTest agent.
</Identity>
`

// collisionDuplicateCustomDeployed is a deployed file that contains the same
// custom-region name ("OverlapName") twice. docformat.Parse accepts this; the duplicate
// is a non-fatal validation issue, not a parse error. The transform must detect the
// duplicate while building the region map and abort with ErrRegionNameCollision.
const collisionDuplicateCustomDeployed = `---
id: 200
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for duplicate-custom collision testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# CollisionTest Agent

You are the CollisionTest agent.
</Identity>

<OverlapName type="custom">
First occurrence of the colliding custom content.
</OverlapName>

<OverlapName type="custom">
Second occurrence of the colliding custom content.
</OverlapName>
`

// collisionCustomVsInjectionSource declares <OverlapName type="project"> in the source.
// Its paired deployed file contains <OverlapName type="custom">. This is Shape B: a name
// claimed by custom-region in the deployed file and project-injection region in the source.
const collisionCustomVsInjectionSource = `---
id: 201
version: 2.0.0
name: collision-cvi-test
description: Agent for custom-vs-injection collision testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: collision testing
required_skills: []
---

<Identity type="core">
# CollisionCVI Agent

You are the CollisionCVI agent.

<OverlapName type="project">
</OverlapName>
</Identity>
`

// collisionCustomVsInjectionDeployed is the deployed predecessor of
// collisionCustomVsInjectionSource. It contains <OverlapName type="custom"> rather than the
// <OverlapName type="project"> that the source declares. This represents a scenario where a
// project invented a custom region under a name that was later (or concurrently) given an
// injection slot in the source — a name collision that must abort the transform.
const collisionCustomVsInjectionDeployed = `---
id: 201
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for custom-vs-injection collision testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# CollisionCVI Agent

You are the CollisionCVI agent.

<OverlapName type="custom">
User content stored under a name that now collides with a source injection.
</OverlapName>
</Identity>
`

// nonCollidingMixSource declares <InjectionOnly type="project"> — a name not present as a
// custom region in the paired deployed file. The deployed file's <CustomOnly type="custom"> has
// a name absent from the source injections. This exercises the guard case: a document
// where a custom name and an injection name merely coexist without sharing a name must
// succeed without error.
const nonCollidingMixSource = `---
id: 202
version: 2.0.0
name: non-colliding-mix-test
description: Agent for non-colliding mixed-name testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: collision testing
required_skills: []
---

<Identity type="core">
# NonCollidingMix Agent

You are the NonCollidingMix agent.

<InjectionOnly type="project">
</InjectionOnly>
</Identity>
`

// nonCollidingMixDeployed is the deployed predecessor of nonCollidingMixSource.
// Its <CustomOnly type="custom"> name is different from the source's <InjectionOnly type="project">
// name: no collision, so the transform must succeed.
const nonCollidingMixDeployed = `---
id: 202
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for non-colliding mixed-name testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# NonCollidingMix Agent

You are the NonCollidingMix agent.

<InjectionOnly type="project">
Lifted injection content that has no naming conflict with the custom region.
</InjectionOnly>
</Identity>

<CustomOnly type="custom">
A custom region whose name does not match any source injection.
It must survive byte-identically and not trigger a collision error.
</CustomOnly>
`

// TestCollision_DuplicateCustomNamesInDeployed_ReturnsErrRegionNameCollision asserts that
// when the deployed file contains the same custom-region name twice (Shape A), Apply returns
// an error wrapping ErrRegionNameCollision. The duplicate is detected while building the
// region map, before any document mutation.
func TestCollision_DuplicateCustomNamesInDeployed_ReturnsErrRegionNameCollision(t *testing.T) {
	req := transform.Request{
		Source:   []byte(collisionSourceMinimal),
		Deployed: []byte(collisionDuplicateCustomDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "collision-duplicate-custom-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when the deployed file contains two custom-region regions with the same name; got nil")
	}
	if !errors.Is(err, transform.ErrRegionNameCollision) {
		t.Errorf("expected error wrapping transform.ErrRegionNameCollision for duplicate custom name; got: %v", err)
	}
}

// TestCollision_CustomInDeployedAndInjectionInSource_ReturnsErrRegionNameCollision asserts
// that when a name appears as custom-region in the deployed file and project-injection region in the
// source (Shape B), Apply returns an error wrapping ErrRegionNameCollision. The collision
// is detected after source injection names are collected, before any document mutation.
func TestCollision_CustomInDeployedAndInjectionInSource_ReturnsErrRegionNameCollision(t *testing.T) {
	req := transform.Request{
		Source:   []byte(collisionCustomVsInjectionSource),
		Deployed: []byte(collisionCustomVsInjectionDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "collision-cvi-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when a name is custom-region in the deployed file and project-injection region in the source; got nil")
	}
	if !errors.Is(err, transform.ErrRegionNameCollision) {
		t.Errorf("expected error wrapping transform.ErrRegionNameCollision for custom-vs-injection name collision; got: %v", err)
	}
}

// TestCollision_ErrorTextContainsCollidingName asserts that the error returned for each
// collision shape includes the colliding name ("OverlapName") in its message, so the user
// can locate the conflicting declarations without parsing the error type.
func TestCollision_ErrorTextContainsCollidingName(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		deployed string
		key      string
	}{
		{
			name:     "duplicate-custom",
			source:   collisionSourceMinimal,
			deployed: collisionDuplicateCustomDeployed,
			key:      "collision-duplicate-custom-test",
		},
		{
			name:     "custom-vs-injection",
			source:   collisionCustomVsInjectionSource,
			deployed: collisionCustomVsInjectionDeployed,
			key:      "collision-cvi-test",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := transform.Request{
				Source:   []byte(tc.source),
				Deployed: []byte(tc.deployed),
				Kind:     domain.ArtifactAgent,
				Key:      tc.key,
				Module:   newFixtureModule(t),
				Model:    fixtureModel(),
				Scope:    domain.ScopeProject,
			}

			_, err := transform.Apply(req)
			if err == nil {
				t.Fatal("Apply must return an error for a name collision; got nil")
			}
			if !strings.Contains(err.Error(), "OverlapName") {
				t.Errorf("collision error must name the colliding region (%q) in its message so the user can locate it; got: %v",
					"OverlapName", err)
			}
		})
	}
}

// TestCollision_NonCollidingMixedNames_Unaffected asserts that a document where a
// custom-region region and an project-injection region region merely coexist with different names is
// unaffected by the collision check. The transform must succeed and preserve both regions.
func TestCollision_NonCollidingMixedNames_Unaffected(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nonCollidingMixSource),
		Deployed: []byte(nonCollidingMixDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "non-colliding-mix-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply must succeed when custom and injection names do not overlap; got error: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// The injection must survive in the output.
	_, injOK := outDoc.Body().Injection("InjectionOnly")
	if !injOK {
		t.Error("InjectionOnly absent from output; a non-colliding injection must be preserved on a normal update")
	}

	// The custom region must survive in the output byte-identically.
	outCustom, customOK := outDoc.Body().Custom("CustomOnly")
	if !customOK {
		t.Fatal("CustomOnly absent from output; a non-colliding custom region must be preserved on a normal update")
	}

	depDoc, err := docformat.Parse([]byte(nonCollidingMixDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depCustom, depOK := depDoc.Body().Custom("CustomOnly")
	if !depOK {
		t.Fatal("CustomOnly not found in deployed fixture; fixture is malformed")
	}
	if !bytes.Equal(outCustom.Content(), depCustom.Content()) {
		t.Errorf("CustomOnly content not byte-identical to deployed:\ndeployed: %q\noutput:   %q",
			depCustom.Content(), outCustom.Content())
	}
}

// ---------------------------------------------------------------------------
// Placement normalization: custom region is always appended at end of parent section
// ---------------------------------------------------------------------------
//
// The placement rule is unconditional append-at-end: a custom region is always placed
// at the end of its resolved parent's content, regardless of where it sat in the
// deployed file. When the deployed file has a custom region followed by other content
// in the same section (e.g. trailing prose), the region moves past that content in the
// output. Three deployed-position variants (start, middle, end of section) must all
// produce identical output, and a re-run over already-normalized output must be
// byte-identical (no separator drift).
// ---------------------------------------------------------------------------

// positionFidelitySource is a source document whose Identity section contains an injection
// followed by a trailing prose paragraph. The source has no custom region — custom regions
// appear only in deployed files. The trailing paragraph is non-empty static content that
// the transform preserves verbatim from the source.
//
// The trailing text "TRAILING-POSITION-MARKER" is a unique sentinel used by the position
// fidelity test to locate this paragraph in the output without ambiguity.
const positionFidelitySource = `---
id: 300
version: 2.0.0
name: position-fidelity-test
description: Agent for custom region position fidelity testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: position fidelity testing
required_skills: []
---

<Identity type="core">
# PositionFidelity Agent

You are the PositionFidelity agent.

<IdentityExtension type="project">
</IdentityExtension>

TRAILING-POSITION-MARKER: this paragraph is static source content that follows the injection.
</Identity>
`

// positionFidelityDeployed is a deployed predecessor of positionFidelitySource.
// It places <MidNote type="custom"> between the injection and the trailing paragraph
// (the "middle" position):
//
//	<IdentityExtension type="project"> ... </IdentityExtension>
//	<MidNote type="custom"> ... </MidNote>
//	TRAILING-POSITION-MARKER: ...
//
// Under the append-at-end rule, MidNote must land at the end of the Identity section,
// after the trailing paragraph. This is positionFidelityDeployedMiddle — two additional
// deployed variants (at-start and at-end) are defined for the three-position test.
const positionFidelityDeployed = `---
id: 300
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for custom region position fidelity testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# PositionFidelity Agent

You are the PositionFidelity agent.

<IdentityExtension type="project">
User identity content that must survive byte-identically.
</IdentityExtension>

<MidNote type="custom">
A mid-section custom note that must remain between the injection and the trailing paragraph.
</MidNote>

TRAILING-POSITION-MARKER: this paragraph is static source content that follows the injection.
</Identity>
`

// TestNormalUpdate_CustomRegion_PositionFidelity_TrailingContentInSameParent asserts
// that when a custom-region region sits between an injection sibling and a trailing prose
// paragraph in the deployed file, the region appears after the trailing paragraph in the
// output. The append-at-end rule always places the custom region at the end of its
// resolved parent's content, regardless of where it sat in the deployed file.
func TestNormalUpdate_CustomRegion_PositionFidelity_TrailingContentInSameParent(t *testing.T) {
	req := transform.Request{
		Source:   []byte(positionFidelitySource),
		Deployed: []byte(positionFidelityDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "position-fidelity-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// MidNote must survive in the output.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	midNote, ok := outDoc.Body().Custom("MidNote")
	if !ok {
		t.Fatal("MidNote absent from output; custom region must be preserved on a normal update")
	}

	// Content byte-identity.
	depDoc, err := docformat.Parse([]byte(positionFidelityDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depMidNote, depOK := depDoc.Body().Custom("MidNote")
	if !depOK {
		t.Fatal("MidNote not found in deployed fixture; fixture is malformed")
	}
	if !bytes.Equal(midNote.Content(), depMidNote.Content()) {
		t.Errorf("MidNote content not byte-identical to deployed:\ndeployed: %q\noutput:   %q",
			depMidNote.Content(), midNote.Content())
	}

	// Placement assertion: MidNote must appear *after* the trailing paragraph.
	// Under the append-at-end rule the custom region is placed at the end of the
	// Identity section, past any static prose that the source contains. We verify
	// this by comparing byte offsets of MidNote's closing tag and the trailing
	// paragraph sentinel in the output.
	outputBytes := result.Output
	// The trailing paragraph comes from the source (static content is taken from the
	// source document), so the sentinel text matches positionFidelitySource.
	midNoteClose := []byte("</MidNote>")
	trailingMarker := []byte("TRAILING-POSITION-MARKER")

	closeIdx := bytes.Index(outputBytes, midNoteClose)
	trailingIdx := bytes.Index(outputBytes, trailingMarker)

	if closeIdx < 0 {
		t.Fatal("</MidNote> closing tag not found in output")
	}
	if trailingIdx < 0 {
		t.Fatal("TRAILING-POSITION-MARKER not found in output; static source content must be preserved")
	}
	if closeIdx < trailingIdx {
		t.Errorf("MidNote closing tag appears at byte %d but trailing paragraph appears at byte %d; "+
			"under append-at-end placement, MidNote must appear after the trailing paragraph, "+
			"not before it", closeIdx, trailingIdx)
	}
}

// positionFidelityDeployedAtStart is a deployed predecessor of positionFidelitySource
// where <MidNote type="custom"> is at the very start of the Identity section, preceded
// only by prose and before the IdentityExtension injection sibling.
//
// Under append-at-end placement, MidNote must still land at the end of the Identity
// section in the output — identical to the middle and end variants.
const positionFidelityDeployedAtStart = `---
id: 300
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for custom region position fidelity testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# PositionFidelity Agent

You are the PositionFidelity agent.

<MidNote type="custom">
A mid-section custom note that must remain between the injection and the trailing paragraph.
</MidNote>

<IdentityExtension type="project">
User identity content that must survive byte-identically.
</IdentityExtension>

TRAILING-POSITION-MARKER: this paragraph is static source content that follows the injection.
</Identity>
`

// positionFidelityDeployedAtEnd is a deployed predecessor of positionFidelitySource
// where <MidNote type="custom"> is already at the end of the Identity section —
// the normalized position. A re-run against this file (the "at-end" variant) must
// produce output with MidNote still at the end, with no separator drift.
const positionFidelityDeployedAtEnd = `---
id: 300
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for custom region position fidelity testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# PositionFidelity Agent

You are the PositionFidelity agent.

<IdentityExtension type="project">
User identity content that must survive byte-identically.
</IdentityExtension>

TRAILING-POSITION-MARKER: this paragraph is static source content that follows the injection.

<MidNote type="custom">
A mid-section custom note that must remain between the injection and the trailing paragraph.
</MidNote>
</Identity>
`

// TestNormalUpdate_CustomRegion_PlacementIndependentOfDeployedPosition asserts that
// placement is independent of where the custom region sat in the deployed file. For the
// same source, three deployed fixtures with MidNote at the start, in the middle, and
// already at the end of the Identity section must all produce identical output bytes.
// This covers the append-at-end rule exhaustively across all meaningful deployed positions.
func TestNormalUpdate_CustomRegion_PlacementIndependentOfDeployedPosition(t *testing.T) {
	cases := []struct {
		name     string
		deployed string
	}{
		{"at-start", positionFidelityDeployedAtStart},
		{"at-middle", positionFidelityDeployed},
		{"at-end", positionFidelityDeployedAtEnd},
	}

	outputs := make([][]byte, len(cases))
	for i, tc := range cases {
		tc := tc
		i := i
		req := transform.Request{
			Source:   []byte(positionFidelitySource),
			Deployed: []byte(tc.deployed),
			Kind:     domain.ArtifactAgent,
			Key:      "position-fidelity-test",
			Module:   newFixtureModule(t),
			Model:    fixtureModel(),
			Scope:    domain.ScopeProject,
		}
		result, err := transform.Apply(req)
		if err != nil {
			t.Fatalf("%s: Apply: %v", tc.name, err)
		}
		outputs[i] = result.Output

		// Verify MidNote is present in all outputs (content must survive).
		outDoc, err := docformat.Parse(result.Output)
		if err != nil {
			t.Fatalf("%s: parse output: %v", tc.name, err)
		}
		if _, ok := outDoc.Body().Custom("MidNote"); !ok {
			t.Errorf("%s: MidNote absent from output; custom region must survive regardless of deployed position", tc.name)
		}

		// Verify MidNote appears after the trailing paragraph in every variant.
		midNoteClose := []byte("</MidNote>")
		trailingMarker := []byte("TRAILING-POSITION-MARKER")
		closeIdx := bytes.Index(result.Output, midNoteClose)
		trailingIdx := bytes.Index(result.Output, trailingMarker)
		if closeIdx < 0 {
			t.Errorf("%s: </MidNote> not found in output", tc.name)
			continue
		}
		if trailingIdx < 0 {
			t.Errorf("%s: TRAILING-POSITION-MARKER not found in output", tc.name)
			continue
		}
		if closeIdx < trailingIdx {
			t.Errorf("%s: MidNote closing tag at byte %d appears before trailing paragraph at byte %d; "+
				"append-at-end placement must put MidNote after all source prose", tc.name, closeIdx, trailingIdx)
		}
	}

	// All three deployed variants must produce identical output bytes.
	for i := 1; i < len(cases); i++ {
		if !bytes.Equal(outputs[0], outputs[i]) {
			t.Errorf("output for %q differs from output for %q; "+
				"placement must be independent of deployed position — all three variants must produce identical output",
				cases[i].name, cases[0].name)
		}
	}
}

// TestNormalUpdate_CustomRegion_IdempotenceAfterNormalization asserts that a second
// update run over already-normalized output (MidNote at end of section) produces output
// byte-identical to the first run's output. No whitespace drift, no separator changes.
//
// This covers the steady-state case: once the custom region is at the end of its parent
// section, subsequent runs must not move it further or add stray bytes around it.
func TestNormalUpdate_CustomRegion_IdempotenceAfterNormalization(t *testing.T) {
	// Run 1: positionFidelityDeployed (MidNote in middle) → normalizes MidNote to end.
	run1, err := transform.Apply(transform.Request{
		Source:   []byte(positionFidelitySource),
		Deployed: []byte(positionFidelityDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "position-fidelity-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("Run 1 Apply: %v", err)
	}

	// Run 2: same source, Run 1 output as deployed. MidNote is now at end of section.
	// Output must be byte-identical to Run 1 output.
	run2, err := transform.Apply(transform.Request{
		Source:   []byte(positionFidelitySource),
		Deployed: run1.Output,
		Kind:     domain.ArtifactAgent,
		Key:      "position-fidelity-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("Run 2 Apply: %v", err)
	}

	if !bytes.Equal(run1.Output, run2.Output) {
		// Show a short tail of each to help diagnose drift without flooding the log.
		tail1 := run1.Output
		if len(tail1) > 300 {
			tail1 = tail1[len(tail1)-300:]
		}
		tail2 := run2.Output
		if len(tail2) > 300 {
			tail2 = tail2[len(tail2)-300:]
		}
		t.Errorf("Run 1 and Run 2 outputs differ; a re-run against already-normalized output must be byte-identical (no separator drift):\n"+
			"Run 1 tail: %q\nRun 2 tail: %q", tail1, tail2)
	}
}

// TestCollision_ErrorPath_MutatesNothing asserts that when Apply returns ErrRegionNameCollision,
// it produces no output — the transform mutates nothing before detecting the collision. The
// result must be the zero value when an error is returned.
func TestCollision_ErrorPath_MutatesNothing(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		deployed string
		key      string
	}{
		{
			name:     "duplicate-custom",
			source:   collisionSourceMinimal,
			deployed: collisionDuplicateCustomDeployed,
			key:      "collision-duplicate-custom-test",
		},
		{
			name:     "custom-vs-injection",
			source:   collisionCustomVsInjectionSource,
			deployed: collisionCustomVsInjectionDeployed,
			key:      "collision-cvi-test",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := transform.Request{
				Source:   []byte(tc.source),
				Deployed: []byte(tc.deployed),
				Kind:     domain.ArtifactAgent,
				Key:      tc.key,
				Module:   newFixtureModule(t),
				Model:    fixtureModel(),
				Scope:    domain.ScopeProject,
			}

			result, err := transform.Apply(req)
			if err == nil {
				t.Fatal("Apply must return an error for a name collision; got nil")
			}
			if !errors.Is(err, transform.ErrRegionNameCollision) {
				t.Errorf("expected ErrRegionNameCollision; got: %v", err)
			}
			// When Apply returns an error the result must carry no output: the transform
			// must not have partially mutated a document before discovering the collision.
			if len(result.Output) != 0 {
				t.Errorf("Apply returned an error but result.Output is non-empty (%d bytes); "+
					"the transform must produce no output on the collision error path", len(result.Output))
			}
		})
	}
}
