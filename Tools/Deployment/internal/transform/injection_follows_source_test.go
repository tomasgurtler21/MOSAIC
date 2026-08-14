package transform_test

// injection_follows_source_test.go contains regression tests verifying that
// project-injection region content follows its source-declared position across a schema
// reorder, byte-identically, with no parking and no TODO entry.
//
// Context: applyProjectRegion fills injection content from the deployed region
// map into the source-declared node, so the injection always lands at its
// source-declared position — the transform never consults the deployed file
// for where to put an injection, only for what to put inside it. This property
// must hold when a reorder is also detected, because the two mechanisms
// (injection fill and reorder detection) operate on separate code paths and
// the Stages 5–8 changes must not have introduced any coupling.
//
// What is verified:
//   - Injection content is lifted byte-identically from the deployed file.
//   - The injection node lands inside the section where the source declares it,
//     even when a reorder is detected.
//   - No GapParkedCustomRegion gap is produced for injection regions.
//   - No RegionParked action is produced for injection regions.
//   - Custom regions and injection regions coexist correctly across a reorder.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Fixture A — Injection inside a reordered section
//
// Source order:   Identity → Capabilities   (IdentityExtension inside Identity)
// Deployed order: Capabilities → Identity   (reorder detected)
//
// After Apply: IdentityExtension is inside Identity, content byte-identical, no parking gap.
// ---------------------------------------------------------------------------

const injectionFollowsSourceSource = `---
id: 910
version: 2.0.0
name: injection-follows-source-test
description: Agent for injection-follows-source regression testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: injection position regression testing
required_skills: []
---

<Identity type="core">
# InjectionFollowsSource Agent

You are the InjectionFollowsSource agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.

<CodebaseContext type="project">
</CodebaseContext>
</Capabilities>
`

// injectionFollowsSourceDeployed: Capabilities first, Identity second → reorder vs source.
// Both injections have user content.
const injectionFollowsSourceDeployed = `---
id: 910
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for injection-follows-source regression testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Capabilities type="core">
## Capabilities

This agent has various capabilities.

<CodebaseContext type="project">
Go monorepo. Architecture is hexagonal; business logic lives in internal/domain.
</CodebaseContext>
</Capabilities>

<Identity type="core">
# InjectionFollowsSource Agent

You are the InjectionFollowsSource agent.

<IdentityExtension type="project">
User-supplied identity extension: this agent handles schema evolution verification.
It was written by the TDD agent and must survive all reorder scenarios byte-identically.
</IdentityExtension>
</Identity>
`

// ---------------------------------------------------------------------------
// Fixture B — Injection and custom region coexist across a reorder
//
// Source order:   Identity → Constraints → Capabilities  (reorder vs deployed)
// Deployed order: Identity → Capabilities → Constraints
//
// IdentityExtension declared inside Identity.
// CapabilitiesNote anchored to Capabilities (still exists → preserved, not parked).
// TopNote top-level (no parent → parked on reorder).
//
// After Apply: IdentityExtension preserved at source-declared position; parking gap
//              produced for TopNote only; IdentityExtension not in the gap detail.
// ---------------------------------------------------------------------------

const coexistSource = `---
id: 911
version: 2.0.0
name: coexist-test
description: Agent for injection+custom coexistence testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: injection and custom region coexistence testing
required_skills: []
---

<Identity type="core">
# Coexist Agent

You are the Coexist agent.

<IdentityExtension type="project">
</IdentityExtension>
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

// coexistDeployed: section order Identity → Capabilities → Constraints.
// Source has Identity → Constraints → Capabilities → reorder detected.
const coexistDeployed = `---
id: 911
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for injection+custom coexistence testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# Coexist Agent

You are the Coexist agent.

<IdentityExtension type="project">
Identity extension content that must survive the reorder byte-identically.
</IdentityExtension>
</Identity>

<Capabilities type="core">
## Capabilities

This agent has various capabilities.

<CapabilitiesNote type="custom">
A note anchored to Capabilities. Capabilities still exists in the source.
</CapabilitiesNote>
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>

<TopNote type="custom">
A top-level custom region with no parent. Parked on reorder.
</TopNote>
`

// ---------------------------------------------------------------------------
// Shared helper for injection-follows-source tests
// ---------------------------------------------------------------------------

func injectionFollowsReq(t *testing.T, source, deployed []byte, key string) transform.Request {
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
// T9.1 — Injection content preserved byte-identically across a reorder
// ---------------------------------------------------------------------------

// TestInjectionFollowsSource_ContentPreservedByteIdentically verifies that when a schema
// reorder is detected, the content of a project injection in the deployed file is lifted
// byte-identically into the output. Reorder detection must not affect injection fill.
func TestInjectionFollowsSource_ContentPreservedByteIdentically(t *testing.T) {
	req := injectionFollowsReq(t,
		[]byte(injectionFollowsSourceSource),
		[]byte(injectionFollowsSourceDeployed),
		"injection-follows-source-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	depDoc, err := docformat.Parse([]byte(injectionFollowsSourceDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	depInj, depOK := depDoc.Body().Injection("IdentityExtension")
	if !depOK {
		t.Fatal("IdentityExtension not found in deployed fixture; fixture is malformed")
	}
	outInj, outOK := outDoc.Body().Injection("IdentityExtension")
	if !outOK {
		t.Fatal("IdentityExtension absent from output; injection content must be preserved across a reorder")
	}

	if !bytes.Equal(outInj.Content(), depInj.Content()) {
		t.Errorf("IdentityExtension content is not byte-identical after reorder:\n"+
			"deployed: %q\n"+
			"output:   %q",
			depInj.Content(), outInj.Content())
	}
}

// TestInjectionFollowsSource_LandsInsideSourceDeclaredSection verifies that a project
// injection declared inside a section in the source lands inside that same section in the
// output, even when a reorder is detected and that section moves relative to others.
func TestInjectionFollowsSource_LandsInsideSourceDeclaredSection(t *testing.T) {
	req := injectionFollowsReq(t,
		[]byte(injectionFollowsSourceSource),
		[]byte(injectionFollowsSourceDeployed),
		"injection-follows-source-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outInj, outOK := outDoc.Body().Injection("IdentityExtension")
	if !outOK {
		t.Fatal("IdentityExtension absent from output")
	}

	parent := outInj.Parent()
	if parent == nil {
		t.Fatal("IdentityExtension has no parent in output; it must be nested inside the Identity section")
	}
	if parent.Kind() != docformat.NodeSection {
		t.Errorf("IdentityExtension parent kind: want %q, got %q", docformat.NodeSection, parent.Kind())
	}
	if parent.Name() != "Identity" {
		t.Errorf("IdentityExtension parent name: want %q, got %q", "Identity", parent.Name())
	}
}

// TestInjectionFollowsSource_NoParkingGapForInjection verifies that a reorder does not
// produce a GapParkedCustomRegion gap listing the injection name. Injections follow their
// source-declared position by construction and are never parked.
func TestInjectionFollowsSource_NoParkingGapForInjection(t *testing.T) {
	req := injectionFollowsReq(t,
		[]byte(injectionFollowsSourceSource),
		[]byte(injectionFollowsSourceDeployed),
		"injection-follows-source-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapParkedCustomRegion {
			t.Errorf("GapParkedCustomRegion gap produced for an injection-only document; "+
				"injections are never parked. Gap detail: %s", g.Detail)
		}
	}
}

// TestInjectionFollowsSource_NoRegionParkedActionForInjection verifies that no RegionOutcome
// for an injection carries Action == RegionParked. Parking applies only to custom-region regions
// on reorder; project-injection region regions are filled at their source-declared position.
func TestInjectionFollowsSource_NoRegionParkedActionForInjection(t *testing.T) {
	req := injectionFollowsReq(t,
		[]byte(injectionFollowsSourceSource),
		[]byte(injectionFollowsSourceDeployed),
		"injection-follows-source-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, out := range result.Report.Regions {
		if out.Marker == docformat.NodeInjection && out.Action == transform.RegionParked {
			t.Errorf("injection region %q has action RegionParked; "+
				"only custom-region regions are parked on reorder", out.Name)
		}
	}
}

// TestInjectionFollowsSource_MultipleInjectionsEachFollowsSource verifies that when a
// deployed file has multiple injections, each lands inside its source-declared section.
func TestInjectionFollowsSource_MultipleInjectionsEachFollowsSource(t *testing.T) {
	req := injectionFollowsReq(t,
		[]byte(injectionFollowsSourceSource),
		[]byte(injectionFollowsSourceDeployed),
		"injection-follows-source-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// IdentityExtension must be inside Identity.
	idExt, idOK := outDoc.Body().Injection("IdentityExtension")
	if !idOK {
		t.Fatal("IdentityExtension absent from output")
	}
	if idExt.Parent() == nil || idExt.Parent().Name() != "Identity" {
		parentName := ""
		if idExt.Parent() != nil {
			parentName = idExt.Parent().Name()
		}
		t.Errorf("IdentityExtension parent: want Identity, got %q", parentName)
	}

	// CodebaseContext must be inside Capabilities.
	cbCtx, cbOK := outDoc.Body().Injection("CodebaseContext")
	if !cbOK {
		t.Fatal("CodebaseContext absent from output")
	}
	if cbCtx.Parent() == nil || cbCtx.Parent().Name() != "Capabilities" {
		parentName := ""
		if cbCtx.Parent() != nil {
			parentName = cbCtx.Parent().Name()
		}
		t.Errorf("CodebaseContext parent: want Capabilities, got %q", parentName)
	}
}

// TestInjectionFollowsSource_ReorderDetectedByFixture confirms the reorder is actually
// detected in the fixture, ensuring subsequent assertions exercise the intended scenario.
func TestInjectionFollowsSource_ReorderDetectedByFixture(t *testing.T) {
	depDoc, err := docformat.Parse([]byte(injectionFollowsSourceDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	srcDoc, err := docformat.Parse([]byte(injectionFollowsSourceSource))
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	if !docformat.ReorderDetected(depDoc, srcDoc) {
		t.Error("ReorderDetected(deployed, source) returned false; the fixture must have a reorder " +
			"(source: Identity→Capabilities; deployed: Capabilities→Identity) for regression tests to be meaningful")
	}
}

// ---------------------------------------------------------------------------
// T9.1 — Coexistence: injection follows source while custom region is parked
// ---------------------------------------------------------------------------

// TestInjectionFollowsSource_CoexistsWithParkedCustomRegion verifies that when a reorder
// causes some custom regions to be parked, the injection regions are unaffected — they still
// land at their source-declared position with byte-identical content.
func TestInjectionFollowsSource_CoexistsWithParkedCustomRegion(t *testing.T) {
	req := injectionFollowsReq(t,
		[]byte(coexistSource),
		[]byte(coexistDeployed),
		"coexist-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A parking gap must be produced (TopNote has no parent, reorder detected).
	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("expected a GapParkedCustomRegion gap for TopNote (top-level custom, reorder detected)")
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// IdentityExtension must be inside Identity, content byte-identical.
	outInj, outOK := outDoc.Body().Injection("IdentityExtension")
	if !outOK {
		t.Fatal("IdentityExtension absent from output; it must survive even when custom regions are parked")
	}
	if outInj.Parent() == nil || outInj.Parent().Name() != "Identity" {
		parentName := ""
		if outInj.Parent() != nil {
			parentName = outInj.Parent().Name()
		}
		t.Errorf("IdentityExtension parent in coexist scenario: want Identity, got %q", parentName)
	}

	depDoc, err := docformat.Parse([]byte(coexistDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depInj, depOK := depDoc.Body().Injection("IdentityExtension")
	if !depOK {
		t.Fatal("IdentityExtension not in deployed fixture; fixture is malformed")
	}
	if !bytes.Equal(outInj.Content(), depInj.Content()) {
		t.Errorf("IdentityExtension content not byte-identical when coexisting with parked custom regions:\n"+
			"deployed: %q\noutput: %q",
			depInj.Content(), outInj.Content())
	}
}

// TestInjectionFollowsSource_InjectionNotListedInParkingGap verifies that the parking gap
// detail lists only custom-region region names, not any injection name.
func TestInjectionFollowsSource_InjectionNotListedInParkingGap(t *testing.T) {
	req := injectionFollowsReq(t,
		[]byte(coexistSource),
		[]byte(coexistDeployed),
		"coexist-test",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Skip("no parking gap produced; cannot verify injection exclusion from gap detail")
	}

	if strings.Contains(pg.Detail, "IdentityExtension") {
		t.Errorf("parking gap detail contains injection name IdentityExtension; "+
			"only custom region names must be listed in the parking gap: %s", pg.Detail)
	}
}
