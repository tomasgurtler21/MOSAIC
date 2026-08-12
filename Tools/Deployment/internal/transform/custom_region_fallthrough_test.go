package transform_test

// custom_region_fallthrough_test.go covers GapCustomRegionFallthrough emission when a
// [[CUSTOM:]] region's recorded parent cannot be resolved in the output document and the
// region falls through to body-level placement — regardless of whether a schema reorder
// was also detected.
//
// Key invariants under test:
//   - A custom region whose parent is absent from the output (section removed from source,
//     no reorder among surviving sections) produces GapCustomRegionFallthrough.
//   - The region's content is still placed at body level and is byte-identical to the deployed file.
//   - Fallthrough names are listed in ascending sorted order in one gap per transform.
//   - A custom region whose parent still resolves produces no GapCustomRegionFallthrough.
//   - A top-level custom region (ParentName = "") is never treated as a fallthrough.
//   - Regions routed to the parking path (unanchored + reorder detected) do not produce
//     GapCustomRegionFallthrough; the existing GapParkedCustomRegion behavior is unchanged.
//
// All tests are in the TDD RED phase: the fallthrough gap emission in processRegions and the
// FallthroughCustomRegionsDetail formatter are not yet implemented. Tests compile but fail at
// runtime until Stage 3 implementation is complete.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Fixture 800 — Multiple fallthrough names: sorted order test
//
// Source removes OldSectionA and OldSectionB.
// Common sections: Identity, Capabilities — same relative order in both → no reorder.
//
// Three custom regions in the deployed file, each anchored to a removed section:
//   ZetaNote  in OldSectionA  (first in document order)
//   BetaNote  in OldSectionA  (second in document order)
//   AlphaNote in OldSectionB  (third in document order)
//
// All three fall through. Gap detail must list them in sorted order: AlphaNote, BetaNote, ZetaNote.
// ---------------------------------------------------------------------------

const multipleFallthroughSource = `---
id: 800
version: 2.0.0
name: multiple-fallthrough-test
description: Agent for multiple-fallthrough sorted order tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: multiple fallthrough testing
required_skills: []
---

[[SECTION:Identity]]
# MultipleFallthrough Agent

You are the MultipleFallthrough agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.
[[/SECTION:Capabilities]]
`

// multipleFallthroughDeployed has Identity → OldSectionA → OldSectionB → Capabilities.
// OldSectionA and OldSectionB are removed from the source.
// Common sections Identity and Capabilities are in the same relative order in both
// source and deployed → no reorder.
const multipleFallthroughDeployed = `---
id: 800
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for multiple-fallthrough sorted order tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# MultipleFallthrough Agent

You are the MultipleFallthrough agent.

[[INJECTION:IdentityExtension]]
Saved identity content that must survive.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:OldSectionA]]
## Old Section A

This section is removed from the source.

[[CUSTOM:ZetaNote]]
Content of ZetaNote — parent OldSectionA is gone, must fall through to body level.
[[/CUSTOM:ZetaNote]]

[[CUSTOM:BetaNote]]
Content of BetaNote — parent OldSectionA is gone, must fall through to body level.
[[/CUSTOM:BetaNote]]
[[/SECTION:OldSectionA]]

[[SECTION:OldSectionB]]
## Old Section B

This section is also removed from the source.

[[CUSTOM:AlphaNote]]
Content of AlphaNote — parent OldSectionB is gone, must fall through to body level.
[[/CUSTOM:AlphaNote]]
[[/SECTION:OldSectionB]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.
[[/SECTION:Capabilities]]
`

// ---------------------------------------------------------------------------
// Fixture 801 — Mixed: one fallthrough region, one resolving-parent region, no reorder
//
// Source removes OldSection. Common sections: Identity, Capabilities — same order → no reorder.
//
// Custom regions:
//   OldNote          in OldSection   (parent gone → falls through to body level)
//   CapabilitiesNote in Capabilities (parent still in source and output → no fallthrough)
// ---------------------------------------------------------------------------

const mixedFallthroughSource = `---
id: 801
version: 2.0.0
name: mixed-fallthrough-test
description: Agent for mixed fallthrough tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: mixed fallthrough testing
required_skills: []
---

[[SECTION:Identity]]
# MixedFallthrough Agent

You are the MixedFallthrough agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.
[[/SECTION:Capabilities]]
`

// mixedFallthroughDeployed has Identity → OldSection → Capabilities.
// OldSection is removed from the source. Common sections Identity and Capabilities are in
// the same relative order in both → no reorder.
const mixedFallthroughDeployed = `---
id: 801
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for mixed fallthrough tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# MixedFallthrough Agent

You are the MixedFallthrough agent.

[[INJECTION:IdentityExtension]]
Saved identity content that must survive.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:OldSection]]
## Old Section

This section is removed from the source.

[[CUSTOM:OldNote]]
Content anchored to OldSection — parent gone, must fall through to body level.
[[/CUSTOM:OldNote]]
[[/SECTION:OldSection]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.

[[CUSTOM:CapabilitiesNote]]
A note anchored to the Capabilities section — parent still in source and output, no fallthrough.
[[/CUSTOM:CapabilitiesNote]]
[[/SECTION:Capabilities]]
`

// ---------------------------------------------------------------------------
// Fixture 802 — Deployed-region parent fallthrough: custom region whose recorded parent
// is a [[DEPLOYED:]] region absent from the output document.
//
// Source has Identity and Capabilities sections but does NOT declare OldHarnessRegion.
// Deployed file has [[DEPLOYED:OldHarnessRegion]] nested inside [[SECTION:Capabilities]],
// and [[CUSTOM:HarnessNote]] nested inside [[DEPLOYED:OldHarnessRegion]]. Because the source
// no longer declares OldHarnessRegion, the output will not contain it. When processing
// HarnessNote (recorded parent "OldHarnessRegion", parentKind NodeDeployed), the caller-side
// detection probes body.SectionDeep("OldHarnessRegion") — not found — then
// body.Deployed("OldHarnessRegion") — not found — and concludes the parent is unresolvable.
// The fallthrough gap must be emitted and HarnessNote must land at body level.
//
// This fixture exists specifically to exercise the body.Deployed lookup branch: a test
// implementation that checked only body.SectionDeep would still pass all section-parent
// fixtures but would silently miss the deployed-region-parent case.
//
// Common sections Identity and Capabilities appear in the same relative order in both
// source and deployed → no reorder.
// ---------------------------------------------------------------------------

const deployedRegionFallthroughSource = `---
id: 802
version: 2.0.0
name: deployed-region-fallthrough-test
description: Agent for deployed-region-parent fallthrough tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: deployed region fallthrough testing
required_skills: []
---

[[SECTION:Identity]]
# DeployedRegionFallthrough Agent

You are the DeployedRegionFallthrough agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.
[[/SECTION:Capabilities]]
`

// deployedRegionFallthroughDeployed has HarnessNote nested inside [[DEPLOYED:OldHarnessRegion]],
// which in turn is nested inside [[SECTION:Capabilities]]. The source no longer declares
// OldHarnessRegion, so the output will not contain it. HarnessNote's recorded parent is
// "OldHarnessRegion" with parentKind NodeDeployed; when neither body.SectionDeep nor
// body.Deployed resolves this name in the output, the fallthrough gap must be emitted and
// HarnessNote must be placed at body level.
const deployedRegionFallthroughDeployed = `---
id: 802
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for deployed-region-parent fallthrough tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# DeployedRegionFallthrough Agent

You are the DeployedRegionFallthrough agent.

[[INJECTION:IdentityExtension]]
Saved identity content.
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Capabilities]]
## Capabilities

This agent has various capabilities.

[[DEPLOYED:OldHarnessRegion]]
This deployed region is no longer declared in the source.

[[CUSTOM:HarnessNote]]
Content anchored to OldHarnessRegion — a deployed-region parent absent from the output.
[[/CUSTOM:HarnessNote]]
[[/DEPLOYED:OldHarnessRegion]]
[[/SECTION:Capabilities]]
`

// ---------------------------------------------------------------------------
// Helper: find the GapCustomRegionFallthrough gap in a report (nil when absent)
// ---------------------------------------------------------------------------

func findFallthroughGap(gaps []domain.Gap) *domain.Gap {
	for i := range gaps {
		if gaps[i].Kind == domain.GapCustomRegionFallthrough {
			return &gaps[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// T3.1 — No-reorder fallthrough: GapCustomRegionFallthrough emitted
// ---------------------------------------------------------------------------

// TestFallthrough_NoReorder_MissingParent_EmitsGap verifies that when a [[CUSTOM:]] region's
// recorded parent is absent from the output document and no reorder is detected among the
// surviving structural slots, a GapCustomRegionFallthrough gap is emitted. This is the core
// Stage 3 requirement: the silence when a section is simply removed must be broken.
func TestFallthrough_NoReorder_MissingParent_EmitsGap(t *testing.T) {
	// sectionRemovedDeployed has OldNote nested in OldSection, which sectionRemovedSource
	// removes. Common sections Identity and Capabilities share the same relative order →
	// no reorder. OldNote must produce GapCustomRegionFallthrough.
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

	fg := findFallthroughGap(result.Report.Gaps)
	if fg == nil {
		t.Fatal("expected GapCustomRegionFallthrough for OldNote (parent OldSection absent from output, no reorder); got no such gap")
	}
}

// TestFallthrough_NoReorder_MissingParent_ContentAtBodyLevel verifies that when a custom
// region's parent cannot be resolved, its content is still placed at body level in the output.
// The fallthrough changes signalling only — content must never be silently discarded.
func TestFallthrough_NoReorder_MissingParent_ContentAtBodyLevel(t *testing.T) {
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

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("OldNote"); !ok {
		t.Fatal("OldNote absent from output; a custom region with a missing parent must still be placed at body level — content must not be dropped")
	}
}

// TestFallthrough_NoReorder_MissingParent_ContentByteIdentical verifies that when a custom
// region falls through to body-level placement, its content in the output is byte-identical
// to the content in the deployed file. The fallthrough changes signalling only, not content.
func TestFallthrough_NoReorder_MissingParent_ContentByteIdentical(t *testing.T) {
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

	depDoc, err := docformat.Parse([]byte(sectionRemovedDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	depNode, depOK := depDoc.Body().Custom("OldNote")
	if !depOK {
		t.Fatal("OldNote not found in deployed fixture; fixture is malformed")
	}
	outNode, outOK := outDoc.Body().Custom("OldNote")
	if !outOK {
		t.Fatal("OldNote absent from output; body-level placement must not discard content")
	}
	if !bytes.Equal(outNode.Content(), depNode.Content()) {
		t.Errorf("OldNote content not byte-identical after fallthrough placement:\ndeployed: %q\noutput:   %q",
			depNode.Content(), outNode.Content())
	}
}

// TestFallthrough_GapSubject_IsArtifactKey verifies that the GapCustomRegionFallthrough gap's
// Subject field equals the artifact key (req.Key). Like GapParkedCustomRegion, the fallthrough
// gap aggregates a whole set, so it is keyed to the artifact.
func TestFallthrough_GapSubject_IsArtifactKey(t *testing.T) {
	const key = "section-removed-test"
	req := reorderReq(t,
		[]byte(sectionRemovedSource),
		[]byte(sectionRemovedDeployed),
		key,
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	fg := findFallthroughGap(result.Report.Gaps)
	if fg == nil {
		t.Fatal("no GapCustomRegionFallthrough gap in report")
	}
	if fg.Subject != key {
		t.Errorf("GapCustomRegionFallthrough Subject: want %q (artifact key), got %q", key, fg.Subject)
	}
}

// TestFallthrough_GapDetail_ContainsFallthroughName verifies that the
// GapCustomRegionFallthrough gap's Detail field contains the name of the region that fell
// through to body-level placement.
func TestFallthrough_GapDetail_ContainsFallthroughName(t *testing.T) {
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

	fg := findFallthroughGap(result.Report.Gaps)
	if fg == nil {
		t.Fatal("no GapCustomRegionFallthrough gap in report")
	}
	if !strings.Contains(fg.Detail, "OldNote") {
		t.Errorf("GapCustomRegionFallthrough detail does not contain fallthrough region name OldNote: %s", fg.Detail)
	}
}

// TestFallthrough_GapDetail_ContainsTimestamp verifies that the GapCustomRegionFallthrough
// gap's Detail field contains the caller-supplied timestamp from Request.Timestamp.
func TestFallthrough_GapDetail_ContainsTimestamp(t *testing.T) {
	const ts = "2026-08-11T17:07:26Z"
	req := reorderReq(t,
		[]byte(sectionRemovedSource),
		[]byte(sectionRemovedDeployed),
		"section-removed-test",
		ts,
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	fg := findFallthroughGap(result.Report.Gaps)
	if fg == nil {
		t.Fatal("no GapCustomRegionFallthrough gap in report")
	}
	if !strings.Contains(fg.Detail, ts) {
		t.Errorf("GapCustomRegionFallthrough detail does not contain the caller-supplied timestamp %q: %s", ts, fg.Detail)
	}
}

// TestFallthrough_GapFragment_IsEmpty verifies that the GapCustomRegionFallthrough gap's
// Fragment field is empty. The content is in the deployed file at body level, not lost;
// no paste-fragment is needed.
func TestFallthrough_GapFragment_IsEmpty(t *testing.T) {
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

	fg := findFallthroughGap(result.Report.Gaps)
	if fg == nil {
		t.Fatal("no GapCustomRegionFallthrough gap in report")
	}
	if fg.Fragment != "" {
		t.Errorf("GapCustomRegionFallthrough Fragment must be empty (content is in the deployed file); got %q", fg.Fragment)
	}
}

// TestFallthrough_ExactlyOneGapPerTransform verifies that when multiple custom regions fall
// through in a single transform, exactly one GapCustomRegionFallthrough gap is produced,
// listing all fallthrough names. The gap is per-artifact, not per-region.
func TestFallthrough_ExactlyOneGapPerTransform(t *testing.T) {
	req := reorderReq(t,
		[]byte(multipleFallthroughSource),
		[]byte(multipleFallthroughDeployed),
		"multiple-fallthrough-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var fallthroughGaps []domain.Gap
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapCustomRegionFallthrough {
			fallthroughGaps = append(fallthroughGaps, g)
		}
	}

	if len(fallthroughGaps) == 0 {
		t.Fatal("no GapCustomRegionFallthrough gap; expected exactly one when multiple regions fall through")
	}
	if len(fallthroughGaps) > 1 {
		t.Errorf("expected exactly one GapCustomRegionFallthrough gap per transform; got %d gaps", len(fallthroughGaps))
	}

	// The single gap must mention all three fallthrough names.
	detail := fallthroughGaps[0].Detail
	for _, name := range []string{"AlphaNote", "BetaNote", "ZetaNote"} {
		if !strings.Contains(detail, name) {
			t.Errorf("GapCustomRegionFallthrough detail does not contain %q; all fallthrough names must appear in one gap: %s", name, detail)
		}
	}
}

// TestFallthrough_MultipleNames_SortedInDetail verifies that when multiple custom regions fall
// through, the gap detail lists their names in ascending alphabetical order, regardless of the
// order they appear in the deployed document.
func TestFallthrough_MultipleNames_SortedInDetail(t *testing.T) {
	// multipleFallthroughDeployed has: ZetaNote (first in file), BetaNote (second),
	// AlphaNote (third). The gap detail must list them sorted: AlphaNote, BetaNote, ZetaNote.
	req := reorderReq(t,
		[]byte(multipleFallthroughSource),
		[]byte(multipleFallthroughDeployed),
		"multiple-fallthrough-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	fg := findFallthroughGap(result.Report.Gaps)
	if fg == nil {
		t.Fatal("no GapCustomRegionFallthrough gap; expected all three regions to fall through")
	}

	alphaIdx := strings.Index(fg.Detail, "AlphaNote")
	betaIdx := strings.Index(fg.Detail, "BetaNote")
	zetaIdx := strings.Index(fg.Detail, "ZetaNote")

	if alphaIdx < 0 || betaIdx < 0 || zetaIdx < 0 {
		t.Fatalf("one or more fallthrough names missing from gap detail: %s", fg.Detail)
	}
	if alphaIdx >= betaIdx {
		t.Errorf("AlphaNote (index %d) does not appear before BetaNote (index %d) in gap detail; names must be sorted: %s",
			alphaIdx, betaIdx, fg.Detail)
	}
	if betaIdx >= zetaIdx {
		t.Errorf("BetaNote (index %d) does not appear before ZetaNote (index %d) in gap detail; names must be sorted: %s",
			betaIdx, zetaIdx, fg.Detail)
	}
}

// TestFallthrough_OutcomeAction_IsCustomPreserved verifies that a custom region that falls
// through to body-level placement reports RegionCustomPreserved in the transform report. The
// fallthrough changes signalling only; the region is placed successfully and its action is
// preserved, not a new action variant.
func TestFallthrough_OutcomeAction_IsCustomPreserved(t *testing.T) {
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

	outcome := findOutcomeByName(result.Report.Regions, "OldNote")
	if outcome == nil {
		t.Fatal("no RegionOutcome for OldNote; a placed custom region must appear in the report")
	}
	if outcome.Action != transform.RegionCustomPreserved {
		t.Errorf("OldNote action: want %q (fallthrough still places the region), got %q",
			transform.RegionCustomPreserved, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Negative case: parent resolves — no GapCustomRegionFallthrough
// ---------------------------------------------------------------------------

// TestFallthrough_ParentResolves_NoFallthroughGap verifies that a [[CUSTOM:]] region whose
// parent section still exists in the output document produces no GapCustomRegionFallthrough.
// The gap is emitted only when the parent cannot be resolved.
func TestFallthrough_ParentResolves_NoFallthroughGap(t *testing.T) {
	// mixedFallthroughDeployed has CapabilitiesNote whose parent Capabilities still exists in
	// mixedFallthroughSource. Only OldNote (parent OldSection removed) should fall through.
	req := reorderReq(t,
		[]byte(mixedFallthroughSource),
		[]byte(mixedFallthroughDeployed),
		"mixed-fallthrough-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	fg := findFallthroughGap(result.Report.Gaps)
	// If a fallthrough gap exists (for OldNote), CapabilitiesNote must not appear in it.
	if fg != nil && strings.Contains(fg.Detail, "CapabilitiesNote") {
		t.Errorf("GapCustomRegionFallthrough detail contains CapabilitiesNote, whose parent Capabilities still resolves; "+
			"only regions with an unresolvable parent must be listed: %s", fg.Detail)
	}
}

// TestFallthrough_TopLevel_NoFallthroughGap verifies that a top-level [[CUSTOM:]] region
// (ParentName = "") is never treated as a fallthrough. A top-level region was always at body
// level and has no missing parent.
func TestFallthrough_TopLevel_NoFallthroughGap(t *testing.T) {
	// sectionRemovedDeployed has TopLevelNote at body top level (ParentName = "").
	// It must not produce a GapCustomRegionFallthrough even though it ends up at body level.
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

	fg := findFallthroughGap(result.Report.Gaps)
	if fg != nil && strings.Contains(fg.Detail, "TopLevelNote") {
		t.Errorf("GapCustomRegionFallthrough detail contains TopLevelNote, which is top-level (ParentName = \"\") and is not a fallthrough; "+
			"only regions with a non-empty parent that cannot be resolved are fallthroughs: %s", fg.Detail)
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Combined: one transform with both a fallthrough and a resolving-parent region
// ---------------------------------------------------------------------------

// TestFallthrough_Mixed_OnlyFallthroughRegionNamedInGap verifies that when a single
// transform contains both a custom region with a missing parent (fallthrough) and a custom
// region whose parent still resolves, the GapCustomRegionFallthrough gap names only the
// fallthrough region — not the resolving one.
func TestFallthrough_Mixed_OnlyFallthroughRegionNamedInGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(mixedFallthroughSource),
		[]byte(mixedFallthroughDeployed),
		"mixed-fallthrough-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	fg := findFallthroughGap(result.Report.Gaps)
	if fg == nil {
		t.Fatal("expected GapCustomRegionFallthrough for OldNote (parent OldSection absent from output); got no fallthrough gap")
	}
	if !strings.Contains(fg.Detail, "OldNote") {
		t.Errorf("GapCustomRegionFallthrough detail must contain OldNote (the falling-through region): %s", fg.Detail)
	}
	if strings.Contains(fg.Detail, "CapabilitiesNote") {
		t.Errorf("GapCustomRegionFallthrough detail must not contain CapabilitiesNote (its parent Capabilities still resolves): %s", fg.Detail)
	}
}

// TestFallthrough_Mixed_ResolvedRegionPreservedInsideParent verifies that when one custom
// region falls through and another's parent resolves, the resolving-parent region is correctly
// placed inside its parent section in the output — not displaced to body level.
func TestFallthrough_Mixed_ResolvedRegionPreservedInsideParent(t *testing.T) {
	req := reorderReq(t,
		[]byte(mixedFallthroughSource),
		[]byte(mixedFallthroughDeployed),
		"mixed-fallthrough-test",
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

	capNote, ok := outDoc.Body().Custom("CapabilitiesNote")
	if !ok {
		t.Fatal("CapabilitiesNote absent from output; a custom region with a resolving parent must be preserved in the output")
	}
	parent := capNote.Parent()
	if parent == nil {
		t.Fatal("CapabilitiesNote has no parent in output; it must remain inside the Capabilities section")
	}
	if parent.Name() != "Capabilities" {
		t.Errorf("CapabilitiesNote parent name: want %q, got %q", "Capabilities", parent.Name())
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Regression: existing GapParkedCustomRegion behavior unchanged
// ---------------------------------------------------------------------------

// TestFallthrough_Regression_ReorderParkingGapUnchanged verifies that the existing
// GapParkedCustomRegion behavior is completely unchanged. When a reorder is detected and a
// custom region is unanchored, a GapParkedCustomRegion gap is still emitted exactly as before
// — Stage 3 does not replace or alter the reorder-parking path.
func TestFallthrough_Regression_ReorderParkingGapUnchanged(t *testing.T) {
	// reorderAnchorDeployed has TopLevelNote (no parent) that is parked on a reorder
	// between reorderAnchorSource and reorderAnchorDeployed (Capabilities/Constraints swapped).
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

	pg := findParkingGap(result.Report.Gaps)
	if pg == nil {
		t.Fatal("expected GapParkedCustomRegion for TopLevelNote (reorder detected, no parent); " +
			"got no parking gap — existing reorder-parking behavior must be unchanged by Stage 3")
	}
	if !strings.Contains(pg.Detail, "TopLevelNote") {
		t.Errorf("GapParkedCustomRegion detail does not contain TopLevelNote: %s", pg.Detail)
	}
}

// TestFallthrough_Regression_NoFallthroughGapFromParkingPath verifies that regions routed to
// the parking path (unanchored + reorder detected) do not produce GapCustomRegionFallthrough.
// The fallthrough gap is emitted only from the placement path; the parking path is disjoint.
func TestFallthrough_Regression_NoFallthroughGapFromParkingPath(t *testing.T) {
	// TopLevelNote in reorderAnchorDeployed takes the parking path (unanchored + reorder).
	// It must not produce a GapCustomRegionFallthrough.
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

	fg := findFallthroughGap(result.Report.Gaps)
	if fg != nil {
		t.Errorf("GapCustomRegionFallthrough emitted in a reorder transform; the fallthrough gap must only be emitted "+
			"from the placement path, never from the parking path: detail=%s", fg.Detail)
	}
}

// TestFallthrough_Regression_NoFallthroughOnCreate verifies that a new deployment
// (req.Deployed == nil) produces no GapCustomRegionFallthrough gap. When there is no deployed
// file, there are no custom regions to place, so no fallthrough can occur.
func TestFallthrough_Regression_NoFallthroughOnCreate(t *testing.T) {
	req := reorderReq(t,
		[]byte(sectionRemovedSource),
		nil, // new deployment — no deployed file
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	fg := findFallthroughGap(result.Report.Gaps)
	if fg != nil {
		t.Errorf("GapCustomRegionFallthrough emitted on a new deployment (Deployed == nil); "+
			"no custom regions exist on create, so no fallthrough gap must be produced: detail=%s", fg.Detail)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Deployed-region parent fallthrough: GapCustomRegionFallthrough emitted
// ---------------------------------------------------------------------------

// TestFallthrough_DeployedRegionParent_MissingParent_EmitsGap verifies that when a
// [[CUSTOM:]] region's recorded parent is a [[DEPLOYED:]] region that is absent from the
// output document (the source no longer declares it), a GapCustomRegionFallthrough gap is
// emitted. This exercises the body.Deployed lookup branch in the caller-side fallthrough
// detection — a buggy implementation that checked only body.SectionDeep would still pass
// all section-parent fixtures but would silently miss this case.
func TestFallthrough_DeployedRegionParent_MissingParent_EmitsGap(t *testing.T) {
	// deployedRegionFallthroughDeployed has HarnessNote nested inside [[DEPLOYED:OldHarnessRegion]].
	// deployedRegionFallthroughSource no longer declares OldHarnessRegion, so the output will
	// not contain it. HarnessNote (parentKind NodeDeployed, parent "OldHarnessRegion") must
	// produce GapCustomRegionFallthrough because neither body.SectionDeep nor body.Deployed
	// resolves the parent.
	req := reorderReq(t,
		[]byte(deployedRegionFallthroughSource),
		[]byte(deployedRegionFallthroughDeployed),
		"deployed-region-fallthrough-test",
		"2026-08-11T17:07:26Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	fg := findFallthroughGap(result.Report.Gaps)
	if fg == nil {
		t.Fatal("expected GapCustomRegionFallthrough for HarnessNote (parent OldHarnessRegion is a deployed region absent from the output); got no such gap")
	}
	if !strings.Contains(fg.Detail, "HarnessNote") {
		t.Errorf("GapCustomRegionFallthrough detail does not contain HarnessNote: %s", fg.Detail)
	}
}

// TestFallthrough_DeployedRegionParent_ContentAtBodyLevel verifies that when a [[CUSTOM:]]
// region's recorded parent is a [[DEPLOYED:]] region absent from the output, the region's
// content is still placed at body level. Fallthrough changes signalling only — content must
// not be discarded even when the missing parent is a deployed region rather than a section.
func TestFallthrough_DeployedRegionParent_ContentAtBodyLevel(t *testing.T) {
	req := reorderReq(t,
		[]byte(deployedRegionFallthroughSource),
		[]byte(deployedRegionFallthroughDeployed),
		"deployed-region-fallthrough-test",
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
	if _, ok := outDoc.Body().Custom("HarnessNote"); !ok {
		t.Fatal("HarnessNote absent from output; a custom region with a missing deployed-region parent must still be placed at body level — content must not be dropped")
	}
}

// ---------------------------------------------------------------------------
// FallthroughCustomRegionsDetail — pure formatting function tests
// ---------------------------------------------------------------------------

// TestFallthroughCustomRegionsDetail_WithTimestamp verifies that FallthroughCustomRegionsDetail
// produces detail text that describes the missing-parent condition, names the fallthrough regions,
// and includes the timestamp when all are supplied.
func TestFallthroughCustomRegionsDetail_WithTimestamp(t *testing.T) {
	names := []string{"Alpha", "Beta"}
	ts := "2026-08-11T17:07:26Z"

	detail := transform.FallthroughCustomRegionsDetail(names, ts)

	if !strings.Contains(detail, "Parent section") {
		t.Errorf("detail does not contain 'Parent section': %s", detail)
	}
	if !strings.Contains(detail, "body level") {
		t.Errorf("detail does not contain 'body level': %s", detail)
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

// TestFallthroughCustomRegionsDetail_WithoutTimestamp verifies that FallthroughCustomRegionsDetail
// omits the "(detected …)" clause when the timestamp argument is empty, while still producing
// the full message body including region names.
func TestFallthroughCustomRegionsDetail_WithoutTimestamp(t *testing.T) {
	names := []string{"Alpha", "Beta"}

	detail := transform.FallthroughCustomRegionsDetail(names, "")

	if !strings.Contains(detail, "Parent section") {
		t.Errorf("detail does not contain 'Parent section': %s", detail)
	}
	if !strings.Contains(detail, "Alpha") || !strings.Contains(detail, "Beta") {
		t.Errorf("detail does not contain region names: %s", detail)
	}
	// The timestamp clause must be absent when no timestamp is supplied.
	if strings.Contains(detail, "(detected") {
		t.Errorf("detail contains a timestamp clause but Timestamp is empty; the clause must be omitted: %s", detail)
	}
}

// TestFallthroughCustomRegionsDetail_SingleName verifies that FallthroughCustomRegionsDetail
// works correctly with a single fallthrough region name.
func TestFallthroughCustomRegionsDetail_SingleName(t *testing.T) {
	names := []string{"OldNote"}
	ts := "2026-08-11T17:07:26Z"

	detail := transform.FallthroughCustomRegionsDetail(names, ts)

	if !strings.Contains(detail, "Parent section") {
		t.Errorf("detail does not contain expected message prefix 'Parent section': %s", detail)
	}
	if !strings.Contains(detail, "OldNote") {
		t.Errorf("detail does not contain region name OldNote: %s", detail)
	}
	if !strings.Contains(detail, ts) {
		t.Errorf("detail does not contain timestamp %q: %s", ts, detail)
	}
}

// TestFallthroughCustomRegionsDetail_MultipleNamesListedWithCommas verifies that
// FallthroughCustomRegionsDetail lists multiple names separated by commas.
func TestFallthroughCustomRegionsDetail_MultipleNamesListedWithCommas(t *testing.T) {
	names := []string{"AlphaNote", "BetaNote", "ZetaNote"}
	detail := transform.FallthroughCustomRegionsDetail(names, "")

	if !strings.Contains(detail, "Parent section") {
		t.Errorf("detail does not contain expected message prefix: %s", detail)
	}
	for _, name := range names {
		if !strings.Contains(detail, name) {
			t.Errorf("detail does not contain name %q: %s", name, detail)
		}
	}
}

// TestFallthroughCustomRegionsDetail_EmptyTimestampOmitsParenthesizedClause verifies that
// when the timestamp is empty, the parenthesised "(detected …)" clause and its preceding
// space are omitted entirely. The text still ends with the period after the name list.
func TestFallthroughCustomRegionsDetail_EmptyTimestampOmitsParenthesizedClause(t *testing.T) {
	detail := transform.FallthroughCustomRegionsDetail([]string{"SomeName"}, "")

	// Must contain the main message (positive assertion — fails with empty stub).
	if !strings.Contains(detail, "Parent section") {
		t.Errorf("detail does not contain expected message 'Parent section': %s", detail)
	}
	// Must NOT contain the timestamp clause.
	if strings.Contains(detail, "(detected") {
		t.Errorf("detail contains a timestamp clause when Timestamp is empty; the clause must be omitted: %s", detail)
	}
}

// TestFallthroughCustomRegionsDetail_DifferentTimestamps_ProduceDifferentOutputs verifies that
// two calls with identical names but different timestamps produce different output strings.
// The timestamp must be visible in the output and must differentiate the two results.
func TestFallthroughCustomRegionsDetail_DifferentTimestamps_ProduceDifferentOutputs(t *testing.T) {
	names := []string{"OldNote"}
	ts1 := "2026-08-11T17:07:26Z"
	ts2 := "2026-08-11T18:00:00Z"

	d1 := transform.FallthroughCustomRegionsDetail(names, ts1)
	d2 := transform.FallthroughCustomRegionsDetail(names, ts2)

	if !strings.Contains(d1, ts1) {
		t.Errorf("FallthroughCustomRegionsDetail with timestamp %q does not contain the timestamp: %s", ts1, d1)
	}
	if !strings.Contains(d2, ts2) {
		t.Errorf("FallthroughCustomRegionsDetail with timestamp %q does not contain the timestamp: %s", ts2, d2)
	}
	if d1 == d2 {
		t.Errorf("FallthroughCustomRegionsDetail produced identical output for different timestamps; "+
			"the timestamp must be reflected in the detail:\nd1: %s\nd2: %s", d1, d2)
	}
}

// ---------------------------------------------------------------------------
// Statelessness: fallthrough region behavior across consecutive runs
//
// A custom region placed at body level due to fallthrough (parent section removed,
// no reorder) must remain at body level on subsequent runs without re-emitting the
// fallthrough gap. The tool persists no state: placement is recomputed from the
// deployed file each run. Once the region is at body level (ParentName = ""), no
// fallthrough detection applies and the output is idempotent.
// ---------------------------------------------------------------------------

// TestStatelessness_FallthroughRegion_StaysAtBodyLevel verifies that a custom region
// placed at body level due to fallthrough remains at body level on a subsequent no-reorder
// run. The tool persists no state about fallthrough placement.
func TestStatelessness_FallthroughRegion_StaysAtBodyLevel(t *testing.T) {
	// Run 1: sectionRemovedSource + sectionRemovedDeployed → OldNote (parent OldSection
	// absent, no reorder) falls through to body level.
	run1, err := transform.Apply(reorderReq(t,
		[]byte(sectionRemovedSource),
		[]byte(sectionRemovedDeployed),
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 1 Apply: %v", err)
	}

	// Run 2: same source, Run 1 output as deployed. OldNote is now at body level
	// (ParentName = "") in the deployed file; it is placed at body level again via the
	// normal top-level path, without triggering the fallthrough gap.
	run2, err := transform.Apply(reorderReq(t,
		[]byte(sectionRemovedSource),
		run1.Output,
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 2 Apply: %v", err)
	}

	outDoc, err := docformat.Parse(run2.Output)
	if err != nil {
		t.Fatalf("parse Run 2 output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("OldNote"); !ok {
		t.Fatal("OldNote absent from Run 2 output; a fallen-through region must remain at body level on subsequent runs")
	}
}

// TestStatelessness_FallthroughRegion_ProducesNoNewGapOnSubsequentRun verifies that a
// subsequent no-reorder run after a fallthrough event does not re-emit
// GapCustomRegionFallthrough. Once the region is at body level in the deployed file, its
// recorded ParentName is empty, so fallthrough detection does not fire on subsequent runs.
func TestStatelessness_FallthroughRegion_ProducesNoNewGapOnSubsequentRun(t *testing.T) {
	// Run 1: fallthrough event — OldNote placed at body level.
	run1, err := transform.Apply(reorderReq(t,
		[]byte(sectionRemovedSource),
		[]byte(sectionRemovedDeployed),
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 1 Apply: %v", err)
	}

	// Run 2: OldNote is now top-level (ParentName = "") in the deployed file. Fallthrough
	// detection only fires for non-empty ParentName values, so no GapCustomRegionFallthrough
	// must be emitted.
	run2, err := transform.Apply(reorderReq(t,
		[]byte(sectionRemovedSource),
		run1.Output,
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 2 Apply: %v", err)
	}

	fg := findFallthroughGap(run2.Report.Gaps)
	if fg != nil {
		t.Errorf("Run 2 emitted GapCustomRegionFallthrough on a subsequent run; "+
			"the tool must not re-emit the fallthrough gap once the region is at body level: detail=%s", fg.Detail)
	}
}

// TestStatelessness_FallthroughRegion_OutputIsStable verifies that two consecutive
// no-reorder runs after a fallthrough event produce identical output. The transform must
// reach a stable fixed point: body-level placement of a fallen-through region is idempotent.
func TestStatelessness_FallthroughRegion_OutputIsStable(t *testing.T) {
	// Run 1: fallthrough event — OldNote placed at body level.
	run1, err := transform.Apply(reorderReq(t,
		[]byte(sectionRemovedSource),
		[]byte(sectionRemovedDeployed),
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 1 Apply: %v", err)
	}

	// Run 2: same source, Run 1 output as deployed → no fallthrough (OldNote is top-level).
	run2, err := transform.Apply(reorderReq(t,
		[]byte(sectionRemovedSource),
		run1.Output,
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 2 Apply: %v", err)
	}

	// Run 3: same source, Run 2 output as deployed → still no fallthrough.
	run3, err := transform.Apply(reorderReq(t,
		[]byte(sectionRemovedSource),
		run2.Output,
		"section-removed-test",
		"2026-08-11T17:07:26Z",
	))
	if err != nil {
		t.Fatalf("Run 3 Apply: %v", err)
	}

	if !bytes.Equal(run2.Output, run3.Output) {
		t.Errorf("Run 2 and Run 3 outputs differ; the transform must reach a stable fixed point after fallthrough placement:\n"+
			"Run 2 tail: %q\nRun 3 tail: %q",
			truncateBytes(run2.Output[max(0, len(run2.Output)-200):], 200),
			truncateBytes(run3.Output[max(0, len(run3.Output)-200):], 200))
	}
}
