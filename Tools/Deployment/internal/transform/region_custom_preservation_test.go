package transform_test

// region_custom_preservation_test.go covers custom region provenance and preservation
// during a normal (no-reorder) update:
//
//   - The deployed region map must include [[CUSTOM:]] regions alongside [[INJECTION:]]
//     regions, keyed by name with byte-identical content, each entry carrying the
//     originating marker kind. [[DEPLOYED:]] regions remain excluded from the map.
//   - A [[CUSTOM:]] region present in the deployed file survives a normal update
//     byte-identically at its existing position — both at body top level and when nested
//     inside a section that still exists in the source.
//   - A genuinely removed [[INJECTION:]] name still produces RegionOrphaned and
//     GapRemovedInjection, unchanged. A [[CUSTOM:]] region never produces RegionOrphaned
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
// region (HarnessConstraints). It has no [[CUSTOM:]] regions — custom regions exist only
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

[[SECTION:Identity]]
# CustomPreservationTest Agent

You are the CustomPreservationTest agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// customPreservationDeployed is the deployed predecessor of customPreservationSource.
// It has filled injection content and two [[CUSTOM:]] regions added by the project:
//   - [[CUSTOM:IdentityCustom]] nested inside [[SECTION:Identity]], immediately after
//     [[INJECTION:IdentityExtension]] (its PrevSibling)
//   - [[CUSTOM:ProjectNotes]] at body top level, after the Constraints section
//
// Both custom regions must survive a normal update byte-identically at their existing positions.
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

[[SECTION:Identity]]
# CustomPreservationTest Agent

You are the CustomPreservationTest agent.

[[INJECTION:IdentityExtension]]
User identity extension content that must survive byte-identically.
[[/INJECTION:IdentityExtension]]

[[CUSTOM:IdentityCustom]]
This is a project-specific custom note inside the Identity section.
It must survive the update and remain nested inside Identity.
[[/CUSTOM:IdentityCustom]]
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Old harness content that will be replaced on every transform.
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]

[[CUSTOM:ProjectNotes]]
These are project-specific notes at the top level.
They span multiple lines and must survive byte-identically.
[[/CUSTOM:ProjectNotes]]
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

[[SECTION:Identity]]
# CustomOrphanTest Agent

You are the CustomOrphanTest agent.
[[/SECTION:Identity]]
`

// customOrphanTestDeployed is the deployed predecessor of customOrphanTestSource.
// It contains:
//   - [[INJECTION:RemovedInjection]] with user content, absent from the source (must orphan)
//   - [[CUSTOM:ProjectNotes]] with user content (must never orphan, must be preserved)
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

[[SECTION:Identity]]
# CustomOrphanTest Agent

You are the CustomOrphanTest agent.

[[INJECTION:RemovedInjection]]
This injection content will be orphaned because RemovedInjection is gone from the source.
User content that must appear in the GapRemovedInjection fragment.
[[/INJECTION:RemovedInjection]]
[[/SECTION:Identity]]

[[CUSTOM:ProjectNotes]]
Project-specific notes that must never be reported as orphaned.
This content must be preserved in the output, not discarded or gapped.
[[/CUSTOM:ProjectNotes]]
`

// ---------------------------------------------------------------------------
// Region map: custom regions are included with byte-identical content and provenance
// ---------------------------------------------------------------------------

// TestRegionMap_CustomRegion_IncludedWithByteIdenticalContent asserts that a [[CUSTOM:]]
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

// TestRegionMap_DeployedRegions_RemainsExcluded asserts that [[DEPLOYED:]] regions in the
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
			"[[DEPLOYED:]] regions must be regenerated, not lifted from the deployed map")
	}
}

// ---------------------------------------------------------------------------
// Normal update: custom regions survive byte-identically at their existing position
// ---------------------------------------------------------------------------

// TestNormalUpdate_CustomRegion_TopLevel_SurvivesByteIdenticallyAtTopLevel asserts that a
// [[CUSTOM:]] region at body top level in the deployed file is present in the output after a
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
// that a [[CUSTOM:]] region nested inside a [[SECTION:]] in the deployed file is present in
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
// Stage 5 provenance changes do not alter the behavior for a genuinely removed [[INJECTION:]]
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

// TestOrphanBranch_CustomRegion_NeverProducesOrphanedOutcome asserts that a [[CUSTOM:]]
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
				"only genuinely removed [[INJECTION:]] names route to that gap: %+v", g)
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
// RegionOutcome for a preserved [[CUSTOM:]] region carries Marker = NodeCustom. The
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
// of a RegionOutcome for a preserved [[CUSTOM:]] region equals the byte length of the content
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
//   Shape A — duplicate [[CUSTOM:]] name: the same name appears twice in the deployed file.
//   Shape B — custom-vs-injection: a name appears as [[CUSTOM:]] in the deployed file and
//             [[INJECTION:]] in the source.
//
// In both cases the transform must:
//   - Return an error wrapping ErrRegionNameCollision.
//   - Include the colliding name in the error text so the user can locate it.
//   - Produce no output (mutate nothing before the error is raised).
//
// A non-colliding document where a [[CUSTOM:]] name and an [[INJECTION:]] name are merely
// different must succeed without error.
// ---------------------------------------------------------------------------

// collisionSourceMinimal is a minimal source document used as the paired source for
// the duplicate-custom collision fixture. It declares no injection named OverlapName,
// so the only collision comes from the deployed file's duplicate [[CUSTOM:]] declarations.
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

[[SECTION:Identity]]
# CollisionTest Agent

You are the CollisionTest agent.
[[/SECTION:Identity]]
`

// collisionDuplicateCustomDeployed is a deployed file that contains the same
// [[CUSTOM:]] name ("OverlapName") twice. docformat.Parse accepts this; the duplicate
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

[[SECTION:Identity]]
# CollisionTest Agent

You are the CollisionTest agent.
[[/SECTION:Identity]]

[[CUSTOM:OverlapName]]
First occurrence of the colliding custom content.
[[/CUSTOM:OverlapName]]

[[CUSTOM:OverlapName]]
Second occurrence of the colliding custom content.
[[/CUSTOM:OverlapName]]
`

// collisionCustomVsInjectionSource declares [[INJECTION:OverlapName]] in the source.
// Its paired deployed file contains [[CUSTOM:OverlapName]]. This is Shape B: a name
// claimed by [[CUSTOM:]] in the deployed file and [[INJECTION:]] in the source.
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

[[SECTION:Identity]]
# CollisionCVI Agent

You are the CollisionCVI agent.

[[INJECTION:OverlapName]]
[[/INJECTION:OverlapName]]
[[/SECTION:Identity]]
`

// collisionCustomVsInjectionDeployed is the deployed predecessor of
// collisionCustomVsInjectionSource. It contains [[CUSTOM:OverlapName]] rather than the
// [[INJECTION:OverlapName]] that the source declares. This represents a scenario where a
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

[[SECTION:Identity]]
# CollisionCVI Agent

You are the CollisionCVI agent.

[[CUSTOM:OverlapName]]
User content stored under a name that now collides with a source injection.
[[/CUSTOM:OverlapName]]
[[/SECTION:Identity]]
`

// nonCollidingMixSource declares [[INJECTION:InjectionOnly]] — a name not present as a
// custom region in the paired deployed file. The deployed file's [[CUSTOM:CustomOnly]] has
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

[[SECTION:Identity]]
# NonCollidingMix Agent

You are the NonCollidingMix agent.

[[INJECTION:InjectionOnly]]
[[/INJECTION:InjectionOnly]]
[[/SECTION:Identity]]
`

// nonCollidingMixDeployed is the deployed predecessor of nonCollidingMixSource.
// Its [[CUSTOM:CustomOnly]] name is different from the source's [[INJECTION:InjectionOnly]]
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

[[SECTION:Identity]]
# NonCollidingMix Agent

You are the NonCollidingMix agent.

[[INJECTION:InjectionOnly]]
Lifted injection content that has no naming conflict with the custom region.
[[/INJECTION:InjectionOnly]]
[[/SECTION:Identity]]

[[CUSTOM:CustomOnly]]
A custom region whose name does not match any source injection.
It must survive byte-identically and not trigger a collision error.
[[/CUSTOM:CustomOnly]]
`

// TestCollision_DuplicateCustomNamesInDeployed_ReturnsErrRegionNameCollision asserts that
// when the deployed file contains the same [[CUSTOM:]] name twice (Shape A), Apply returns
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
		t.Fatal("Apply must return an error when the deployed file contains two [[CUSTOM:]] regions with the same name; got nil")
	}
	if !errors.Is(err, transform.ErrRegionNameCollision) {
		t.Errorf("expected error wrapping transform.ErrRegionNameCollision for duplicate custom name; got: %v", err)
	}
}

// TestCollision_CustomInDeployedAndInjectionInSource_ReturnsErrRegionNameCollision asserts
// that when a name appears as [[CUSTOM:]] in the deployed file and [[INJECTION:]] in the
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
		t.Fatal("Apply must return an error when a name is [[CUSTOM:]] in the deployed file and [[INJECTION:]] in the source; got nil")
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
// [[CUSTOM:]] region and an [[INJECTION:]] region merely coexist with different names is
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
// Position fidelity: custom region with trailing sibling content stays in place
// ---------------------------------------------------------------------------
//
// AC5.2 requires custom regions to survive at their *existing position*, not just
// byte-identically in content. The critical case is a custom region followed by other
// content within the same parent: if placement uses append-only, the region ends up
// after the trailing content instead of before it.
//
// These fixtures verify the PrevSibling anchor (ContractsDesign.md AD-1 step 3): the
// custom region must be inserted after its preceding sibling, not appended at the end
// of the parent.
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

[[SECTION:Identity]]
# PositionFidelity Agent

You are the PositionFidelity agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]

TRAILING-POSITION-MARKER: this paragraph is static source content that follows the injection.
[[/SECTION:Identity]]
`

// positionFidelityDeployed is the deployed predecessor of positionFidelitySource.
// It places [[CUSTOM:MidNote]] between the injection and the trailing paragraph:
//
//	[[INJECTION:IdentityExtension]] ... [[/INJECTION:IdentityExtension]]
//	[[CUSTOM:MidNote]] ... [[/CUSTOM:MidNote]]
//	TRAILING-POSITION-MARKER: ...
//
// A correct placement re-inserts MidNote after IdentityExtension (its PrevSibling),
// before the trailing paragraph. An append-only placement moves MidNote to the end
// of the section, after the trailing paragraph — a silent position violation of AC5.2.
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

[[SECTION:Identity]]
# PositionFidelity Agent

You are the PositionFidelity agent.

[[INJECTION:IdentityExtension]]
User identity content that must survive byte-identically.
[[/INJECTION:IdentityExtension]]

[[CUSTOM:MidNote]]
A mid-section custom note that must remain between the injection and the trailing paragraph.
[[/CUSTOM:MidNote]]

TRAILING-POSITION-MARKER: this paragraph is static source content that follows the injection.
[[/SECTION:Identity]]
`

// TestNormalUpdate_CustomRegion_PositionFidelity_TrailingContentInSameParent asserts
// that when a [[CUSTOM:]] region is followed by other content in the same parent, the
// region appears before that trailing content in the output — not appended after it.
//
// This is the regression test for the append-only placement bug identified in AC5.2:
// append-only always places the custom region at the end of its parent, silently
// relocating it past any trailing siblings. The correct placement uses the PrevSibling
// anchor (ContractsDesign.md AD-1) to insert the region after its preceding sibling.
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

	// Position fidelity: MidNote must appear *before* the trailing paragraph.
	// We verify this by asserting that the trailing paragraph text appears after MidNote's
	// closing tag in the output bytes, not before it.
	//
	// Strategy: scan the serialised Identity section for the byte offsets of MidNote's
	// closing tag and the trailing paragraph's first distinctive word.
	outputBytes := result.Output
	// Locate MidNote's closing tag and the trailing paragraph sentinel in the output.
	// The trailing paragraph comes from the source (static content is taken from the source
	// document, not the deployed file), so the sentinel text matches positionFidelitySource.
	midNoteClose := []byte("[[/CUSTOM:MidNote]]")
	trailingMarker := []byte("TRAILING-POSITION-MARKER")

	closeIdx := bytes.Index(outputBytes, midNoteClose)
	trailingIdx := bytes.Index(outputBytes, trailingMarker)

	if closeIdx < 0 {
		t.Fatal("[[/CUSTOM:MidNote]] closing tag not found in output")
	}
	if trailingIdx < 0 {
		t.Fatal("TRAILING-POSITION-MARKER not found in output; static source content must be preserved")
	}
	if trailingIdx < closeIdx {
		t.Errorf("MidNote closing tag appears at byte %d but trailing paragraph appears at byte %d; "+
			"MidNote must appear before the trailing paragraph (PrevSibling anchor must be honoured), "+
			"not appended after it", closeIdx, trailingIdx)
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
