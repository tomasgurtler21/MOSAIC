package transform_test

// enclosing_section_changed_test.go covers GapEnclosingSectionChanged detection and
// SectionOwnContent helper behaviour.
//
// Key invariants under test:
//
//   - An update where the core section directly enclosing a custom region changed its
//     own content produces exactly one GapEnclosingSectionChanged gap, naming the section
//     and all enclosed custom region names in sorted order.
//   - No gap is produced when the enclosing section is unchanged, when an unrelated section
//     changed, when there is no custom region in the changed section, or on a create run.
//   - The managed-parent case continues to produce GapDeployedRegionContentChanged and
//     does not additionally produce GapEnclosingSectionChanged.
//   - A regenerated managed sub-region inside a core section does not on its own trigger
//     the new warning (SectionOwnContent excises managed children from both sides).
//   - One gap per affected section: two custom regions in the same changed section yield
//     one gap listing both names in sorted order.
//   - Two changed sections each holding a custom region yield two separate gaps.
//   - GapEnclosingSectionChanged maps to TodoInjections in the canonical gap-to-category map.
//
// SectionOwnContent unit tests verify the excision and trimming rules independently of
// the full transform so that the symmetric comparison contract can be tested in isolation.
//
// Tests compile but fail at runtime until the Stage 2 implementation is complete (TDD RED
// phase). Tests checking for gap absence pass vacuously in the RED phase and become
// regression guards once the implementation exists.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/todo"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Fixture 801-A: Positive case — changed enclosing section + custom region inside
//
// The source Identity section added "This line was added in the new version of the
// source." The deployed Identity section does not contain that line. The deployed file
// carries <IdentityNote type="custom"> directly inside Identity.
//
// Detection:
//   - IdentityNote.ParentKind = NodeSection, IdentityNote.ParentName = "Identity"
//   - SectionOwnContent(deployedIdentity) ≠ SectionOwnContent(outputIdentity)
//   - One GapEnclosingSectionChanged must be emitted for "Identity" listing "IdentityNote"
// ---------------------------------------------------------------------------

const enclosingSectionChangedSource = `---
id: 801
version: 2.0.0
name: enclosing-section-test
description: Agent for enclosing-section change detection tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: enclosing section change testing
required_skills: []
---

<Identity type="core">
# EnclosingSection Agent

You are the EnclosingSection agent.

This line was added in the new version of the source.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

const enclosingSectionChangedDeployed = `---
id: 801
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for enclosing-section change detection tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# EnclosingSection Agent

You are the EnclosingSection agent.

<IdentityNote type="custom">
Project-specific note inside Identity section — must be reviewed after section change.
</IdentityNote>
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture 801-B: Negative case (a) — unchanged section content + custom region inside
//
// The deployed Identity section has the SAME own content as the source will produce.
// SectionOwnContent of both sides will be equal → no gap, even though IdentityNote exists.
// ---------------------------------------------------------------------------

const enclosingSectionUnchangedDeployed = `---
id: 801
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for enclosing-section change detection tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# EnclosingSection Agent

You are the EnclosingSection agent.

This line was added in the new version of the source.

<IdentityNote type="custom">
Project-specific note inside Identity section — content unchanged, no gap expected.
</IdentityNote>
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture 802: Negative cases (b) and (d)
//
// Source: Identity section prose MATCHES the deployed file (unchanged).
//         Capabilities section prose DIFFERS from the deployed file (changed).
//
// Deployed: Identity section has same prose as source + IdentityNote custom region.
//           Capabilities section has old prose but NO custom region.
//
// Expected: no GapEnclosingSectionChanged.
//   (b) The enclosing section of the custom region (Identity) did not change.
//   (d) The section that changed (Capabilities) contains no custom region.
// ---------------------------------------------------------------------------

const enclosingSectionNegBDSource = `---
id: 802
version: 2.0.0
name: enclosing-section-negbd-test
description: Agent for enclosing-section negative-b-d tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: enclosing section negative testing
required_skills: []
---

<Identity type="core">
# NegBD Agent

You are the NegBD agent. This prose is the same in the deployed file.
</Identity>

<Capabilities type="core">
## Capabilities

NEW capabilities prose that changed from the deployed file.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

const enclosingSectionNegBDDeployed = `---
id: 802
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for enclosing-section negative-b-d tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# NegBD Agent

You are the NegBD agent. This prose is the same in the deployed file.

<IdentityNote type="custom">
User note inside Identity — the enclosing section did not change.
</IdentityNote>
</Identity>

<Capabilities type="core">
## Capabilities

OLD capabilities prose that will change in the output.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture 803: Multiplicity — two custom regions in the same changed section
//
// The source Identity section changed (added new prose). The deployed Identity section
// has the old prose plus two custom regions: AlphaNote and ZetaNote (intentionally not
// sorted in the deployed order). Expected: one gap listing ["AlphaNote", "ZetaNote"]
// in sorted ascending order.
// ---------------------------------------------------------------------------

const enclosingSectionMultiCustomSource = `---
id: 803
version: 2.0.0
name: enclosing-section-multi-custom-test
description: Agent for multiplicity tests: two customs in same section
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: enclosing section multiplicity testing
required_skills: []
---

<Identity type="core">
# MultiCustom Agent

You are the MultiCustom agent.

This new prose was added, making the Identity section own content differ from deployed.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

const enclosingSectionMultiCustomDeployed = `---
id: 803
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for multiplicity tests: two customs in same section
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# MultiCustom Agent

You are the MultiCustom agent.

<ZetaNote type="custom">
Zeta custom content — listed second in the deployed file but must appear second (sorted) in the gap.
</ZetaNote>

<AlphaNote type="custom">
Alpha custom content — listed first alphabetically in the sorted output.
</AlphaNote>
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture 804: Multiplicity — two changed sections, one custom region each
//
// Both Identity and Capabilities sections changed their own content. Each contains
// exactly one custom region. Expected: two GapEnclosingSectionChanged gaps (one per
// section, each listing one custom region name).
// ---------------------------------------------------------------------------

const enclosingSectionMultiSectionSource = `---
id: 804
version: 2.0.0
name: enclosing-section-multi-section-test
description: Agent for multiplicity tests: two changed sections
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: enclosing section multi-section testing
required_skills: []
---

<Identity type="core">
# MultiSection Agent

You are the MultiSection agent.

New Identity prose that was added to this version.
</Identity>

<Capabilities type="core">
## Capabilities

New Capabilities prose that was added to this version.
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

const enclosingSectionMultiSectionDeployed = `---
id: 804
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for multiplicity tests: two changed sections
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# MultiSection Agent

You are the MultiSection agent.

<IdentityExt type="custom">
Identity custom content — must appear in the Identity-section gap.
</IdentityExt>
</Identity>

<Capabilities type="core">
## Capabilities

<CapabilitiesExt type="custom">
Capabilities custom content — must appear in the Capabilities-section gap.
</CapabilitiesExt>
</Capabilities>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Helper: build a minimal parseable section document for SectionOwnContent unit tests.
// sectionContent goes between the opening and closing section tags verbatim.
// ---------------------------------------------------------------------------

func minimalSectionDoc(sectionName, sectionContent string) []byte {
	return []byte("---\nid: 0\nversion: 1.0.0\ntransform_version: 3.0.0\ninjections_version: 1.0.0\n" +
		"description: section own content test\nmode: subagent\nmodel: claude/claude-sonnet\ntools: []\n---\n\n" +
		"<" + sectionName + " type=\"core\">\n" + sectionContent +
		"</" + sectionName + ">\n")
}

// ---------------------------------------------------------------------------
// Helper: collect all GapEnclosingSectionChanged gaps from a report.
// ---------------------------------------------------------------------------

func findEnclosingSectionChangedGaps(gaps []domain.Gap) []domain.Gap {
	var result []domain.Gap
	for _, g := range gaps {
		if g.Kind == domain.GapEnclosingSectionChanged {
			result = append(result, g)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// SectionOwnContent unit tests
//
// These tests exercise SectionOwnContent directly, without running a full transform,
// to verify the excision and trimming rules independently. They fail in the TDD RED
// phase until the Stage 2 implementation provides a working body.
// ---------------------------------------------------------------------------

// TestSectionOwnContent_NilNode_ReturnsNil verifies the nil-safety contract:
// SectionOwnContent(nil) returns nil without panicking.
func TestSectionOwnContent_NilNode_ReturnsNil(t *testing.T) {
	result := transform.SectionOwnContent(nil)
	if result != nil {
		t.Errorf("SectionOwnContent(nil): want nil, got %q", result)
	}
}

// TestSectionOwnContent_NodeWithNoChildren_ReturnsTrimmedContent verifies that a
// section node with no child regions returns its content bytes trimmed of leading
// and trailing whitespace.
func TestSectionOwnContent_NodeWithNoChildren_ReturnsTrimmedContent(t *testing.T) {
	const sectionBody = "## Heading\n\nProse line one.\nProse line two.\n"
	doc, err := docformat.Parse(minimalSectionDoc("TestSection", sectionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().SectionDeep("TestSection")
	if !ok {
		t.Fatal("TestSection not found in parsed document; test fixture is malformed")
	}

	got := transform.SectionOwnContent(node)

	if got == nil {
		t.Fatal("SectionOwnContent returned nil for a non-nil node with content; must return the trimmed content bytes")
	}
	want := []byte("## Heading\n\nProse line one.\nProse line two.")
	if !bytes.Equal(got, want) {
		t.Errorf("SectionOwnContent (no children): want %q, got %q", want, got)
	}
}

// TestSectionOwnContent_NodeWithCustomChild_ExcisesCustomBytes verifies that a section
// node with a direct custom-region child has the full marker block of the custom region
// removed from the returned bytes. The remaining prose is trimmed.
func TestSectionOwnContent_NodeWithCustomChild_ExcisesCustomBytes(t *testing.T) {
	const sectionBody = "## Heading\n\nProse line.\n\n<UserNote type=\"custom\">\nUser content.\n</UserNote>\n"
	doc, err := docformat.Parse(minimalSectionDoc("TestSection", sectionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().SectionDeep("TestSection")
	if !ok {
		t.Fatal("TestSection not found in parsed document")
	}

	got := transform.SectionOwnContent(node)

	if got == nil {
		t.Fatal("SectionOwnContent returned nil for a non-nil node; must return section prose with custom block excised")
	}
	if !bytes.Contains(got, []byte("Prose line.")) {
		t.Errorf("SectionOwnContent: prose content absent from result: %q", got)
	}
	if bytes.Contains(got, []byte("<UserNote type=\"custom\">")) {
		t.Errorf("SectionOwnContent: custom region open tag must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("User content.")) {
		t.Errorf("SectionOwnContent: custom region inner content must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("</UserNote>")) {
		t.Errorf("SectionOwnContent: custom region close tag must be excised; got: %q", got)
	}
}

// TestSectionOwnContent_NodeWithInjectionChild_ExcisesInjectionBytes verifies that a
// section node with a direct project-injection child has the injection's marker block
// removed. This is the injection-provenance counterpart to the custom test.
func TestSectionOwnContent_NodeWithInjectionChild_ExcisesInjectionBytes(t *testing.T) {
	const sectionBody = "## Heading\n\nProse.\n\n<UserInj type=\"project\">\nInjection content.\n</UserInj>\n"
	doc, err := docformat.Parse(minimalSectionDoc("TestSection", sectionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().SectionDeep("TestSection")
	if !ok {
		t.Fatal("TestSection not found in parsed document")
	}

	got := transform.SectionOwnContent(node)

	if got == nil {
		t.Fatal("SectionOwnContent returned nil for a non-nil node; must return prose with injection block excised")
	}
	if !bytes.Contains(got, []byte("Prose.")) {
		t.Errorf("SectionOwnContent: prose content absent from result: %q", got)
	}
	if bytes.Contains(got, []byte("<UserInj type=\"project\">")) {
		t.Errorf("SectionOwnContent: injection open tag must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("Injection content.")) {
		t.Errorf("SectionOwnContent: injection inner content must be excised; got: %q", got)
	}
}

// TestSectionOwnContent_NodeWithManagedChild_ExcisesManagedBytes verifies the KEY
// difference from CanonicalContent: SectionOwnContent also excises managed (NodeDeployed)
// children. This ensures that a regenerated managed sub-region does not masquerade as a
// change to the core section's own prose.
func TestSectionOwnContent_NodeWithManagedChild_ExcisesManagedBytes(t *testing.T) {
	const sectionBody = "## Heading\n\nProse.\n\n<HarnessRegion type=\"managed\">\nManaged content.\n</HarnessRegion>\n"
	doc, err := docformat.Parse(minimalSectionDoc("TestSection", sectionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().SectionDeep("TestSection")
	if !ok {
		t.Fatal("TestSection not found in parsed document")
	}

	got := transform.SectionOwnContent(node)

	if got == nil {
		t.Fatal("SectionOwnContent returned nil for a non-nil node; must return prose with managed block excised")
	}
	if !bytes.Contains(got, []byte("Prose.")) {
		t.Errorf("SectionOwnContent: prose content absent from result: %q", got)
	}
	if bytes.Contains(got, []byte("<HarnessRegion type=\"managed\">")) {
		t.Errorf("SectionOwnContent: managed region open tag must be excised (unlike CanonicalContent); got: %q", got)
	}
	if bytes.Contains(got, []byte("Managed content.")) {
		t.Errorf("SectionOwnContent: managed region inner content must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("</HarnessRegion>")) {
		t.Errorf("SectionOwnContent: managed region close tag must be excised; got: %q", got)
	}
}

// TestSectionOwnContent_AllChildKindsExcised verifies that when a section node has
// custom, injection, and managed children, all three are excised and only prose remains.
func TestSectionOwnContent_AllChildKindsExcised(t *testing.T) {
	const sectionBody = "Before.\n\n" +
		"<CustomOne type=\"custom\">\nCustom content.\n</CustomOne>\n\n" +
		"Middle.\n\n" +
		"<InjOne type=\"project\">\nInjection content.\n</InjOne>\n\n" +
		"End prose.\n\n" +
		"<ManagedOne type=\"managed\">\nManaged content.\n</ManagedOne>\n"

	doc, err := docformat.Parse(minimalSectionDoc("TestSection", sectionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().SectionDeep("TestSection")
	if !ok {
		t.Fatal("TestSection not found in parsed document")
	}

	got := transform.SectionOwnContent(node)

	if got == nil {
		t.Fatal("SectionOwnContent returned nil for a non-nil node")
	}
	if bytes.Contains(got, []byte("Custom content.")) {
		t.Errorf("SectionOwnContent: CustomOne content must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("Injection content.")) {
		t.Errorf("SectionOwnContent: InjOne content must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("Managed content.")) {
		t.Errorf("SectionOwnContent: ManagedOne content must be excised; got: %q", got)
	}
	if !bytes.Contains(got, []byte("Before.")) {
		t.Errorf("SectionOwnContent: prose 'Before.' must be retained; got: %q", got)
	}
	if !bytes.Contains(got, []byte("Middle.")) {
		t.Errorf("SectionOwnContent: prose 'Middle.' must be retained; got: %q", got)
	}
	if !bytes.Contains(got, []byte("End prose.")) {
		t.Errorf("SectionOwnContent: prose 'End prose.' must be retained; got: %q", got)
	}
}

// TestSectionOwnContent_SymmetricWhenManagedContentDiffers verifies that two section
// nodes with identical prose but different managed-child content produce identical
// SectionOwnContent results. This is the core guarantee that prevents a regenerated
// managed sub-region from triggering a spurious section-change gap.
func TestSectionOwnContent_SymmetricWhenManagedContentDiffers(t *testing.T) {
	const prose = "## Constraints\n\nAlways be helpful.\n"

	deployedBody := prose + "\n<HarnessConstraints type=\"managed\">\nStale managed content.\n</HarnessConstraints>\n"
	freshBody := prose + "\n<HarnessConstraints type=\"managed\">\nFresh managed content from harness.\n</HarnessConstraints>\n"

	deployedDoc, err := docformat.Parse(minimalSectionDoc("Constraints", deployedBody))
	if err != nil {
		t.Fatalf("parse deployed document: %v", err)
	}
	deployedNode, ok := deployedDoc.Body().SectionDeep("Constraints")
	if !ok {
		t.Fatal("Constraints not found in deployed document")
	}

	freshDoc, err := docformat.Parse(minimalSectionDoc("Constraints", freshBody))
	if err != nil {
		t.Fatalf("parse fresh document: %v", err)
	}
	freshNode, ok := freshDoc.Body().SectionDeep("Constraints")
	if !ok {
		t.Fatal("Constraints not found in fresh document")
	}

	deployedOwn := transform.SectionOwnContent(deployedNode)
	freshOwn := transform.SectionOwnContent(freshNode)

	if deployedOwn == nil || freshOwn == nil {
		t.Fatalf("SectionOwnContent returned nil for a non-nil node: deployed=%v fresh=%v",
			deployedOwn, freshOwn)
	}
	if !bytes.Equal(deployedOwn, freshOwn) {
		t.Errorf("SectionOwnContent is not symmetric when managed content differs:\n"+
			"deployed (stale managed excised): %q\n"+
			"fresh    (new managed excised):   %q\n"+
			"Managed child content must be excised from both sides so a regenerated managed "+
			"sub-region does not register as a core-prose change.",
			deployedOwn, freshOwn)
	}
}

// TestSectionOwnContent_LeadingTrailingWhitespaceTrimmed verifies that leading and
// trailing whitespace of the whole result is trimmed.
func TestSectionOwnContent_LeadingTrailingWhitespaceTrimmed(t *testing.T) {
	const prose = "Prose line."
	bodyWithWhitespace := "\n\n" + prose + "\n\n"

	doc, err := docformat.Parse(minimalSectionDoc("TestSection", bodyWithWhitespace))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().SectionDeep("TestSection")
	if !ok {
		t.Fatal("TestSection not found in parsed document")
	}

	got := transform.SectionOwnContent(node)
	want := []byte(prose)

	if got == nil {
		t.Fatal("SectionOwnContent returned nil; must return trimmed content")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("SectionOwnContent did not trim leading/trailing whitespace:\nwant: %q\ngot:  %q", want, got)
	}
}

// ---------------------------------------------------------------------------
// GapEnclosingSectionChanged category registration
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_GapCategoryIsInjections verifies that
// GapEnclosingSectionChanged is registered in the canonical gap-to-category map with
// category TodoInjections. When a gap kind has no entry in gapToCategory, AddGap silently
// falls back to TodoManual — a defect. This test catches that defect.
func TestEnclosingSectionChanged_GapCategoryIsInjections(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapEnclosingSectionChanged,
		Subject: "enclosing-section-test",
		Detail:  "test detail text",
	})

	items := c.Items()
	if len(items) == 0 {
		t.Fatal("AddGap(GapEnclosingSectionChanged) produced no TodoItem; expected one item")
	}
	if items[0].Category != domain.TodoInjections {
		t.Errorf("GapEnclosingSectionChanged category: want %q (TodoInjections), got %q; "+
			"add GapEnclosingSectionChanged → TodoInjections to gapToCategory in collect.go",
			domain.TodoInjections, items[0].Category)
	}
}

// ---------------------------------------------------------------------------
// T2.2 — Positive case: changed enclosing section + custom region inside → gap emitted
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_ChangedSection_WithCustom_EmitsGap verifies that when a
// core section directly enclosing a custom region has different own content in the
// deployed vs output file, exactly one GapEnclosingSectionChanged gap is produced.
func TestEnclosingSectionChanged_ChangedSection_WithCustom_EmitsGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("expected GapEnclosingSectionChanged for Identity section " +
			"(section own content changed, custom region IdentityNote directly inside); got no such gap")
	}
}

// TestEnclosingSectionChanged_ChangedSection_ExactlyOneGap verifies cardinality: when
// one section changed and contains one custom region, exactly one gap is produced.
func TestEnclosingSectionChanged_ChangedSection_ExactlyOneGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap; expected exactly one")
	}
	if len(gaps) > 1 {
		t.Errorf("expected exactly one GapEnclosingSectionChanged gap; got %d", len(gaps))
	}
}

// TestEnclosingSectionChanged_ChangedSection_GapSubjectIsArtifactKey verifies that the
// gap's Subject is the artifact key (req.Key), consistent with all other custom-region
// gaps (GapParkedCustomRegion, GapCustomRegionFallthrough).
func TestEnclosingSectionChanged_ChangedSection_GapSubjectIsArtifactKey(t *testing.T) {
	const key = "enclosing-section-test"
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		key,
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap")
	}
	if gaps[0].Subject != key {
		t.Errorf("gap Subject: want %q (artifact key), got %q; the subject must be the artifact key, consistent with all other custom-region gaps",
			key, gaps[0].Subject)
	}
}

// TestEnclosingSectionChanged_ChangedSection_GapOwnerIsArtifactKey verifies that the
// gap's Owner equals the artifact key.
func TestEnclosingSectionChanged_ChangedSection_GapOwnerIsArtifactKey(t *testing.T) {
	const key = "enclosing-section-test"
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		key,
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap")
	}
	if gaps[0].Owner != key {
		t.Errorf("gap Owner: want %q (artifact key), got %q", key, gaps[0].Owner)
	}
}

// TestEnclosingSectionChanged_ChangedSection_GapDetailContainsSectionName verifies that
// the gap's Detail field names the section whose own content changed.
func TestEnclosingSectionChanged_ChangedSection_GapDetailContainsSectionName(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap")
	}
	if !strings.Contains(gaps[0].Detail, "Identity") {
		t.Errorf("gap Detail does not contain the section name %q: %s", "Identity", gaps[0].Detail)
	}
}

// TestEnclosingSectionChanged_ChangedSection_GapDetailContainsCustomName verifies that
// the gap's Detail field names the custom region enclosed in the changed section.
func TestEnclosingSectionChanged_ChangedSection_GapDetailContainsCustomName(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap")
	}
	if !strings.Contains(gaps[0].Detail, "IdentityNote") {
		t.Errorf("gap Detail does not contain the custom region name %q: %s", "IdentityNote", gaps[0].Detail)
	}
}

// TestEnclosingSectionChanged_ChangedSection_GapDetailContainsTimestamp verifies that
// the gap's Detail field contains the caller-supplied timestamp from Request.Timestamp.
func TestEnclosingSectionChanged_ChangedSection_GapDetailContainsTimestamp(t *testing.T) {
	const ts = "2026-08-17T22:41:49Z"
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		ts,
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap")
	}
	if !strings.Contains(gaps[0].Detail, ts) {
		t.Errorf("gap Detail does not contain caller-supplied timestamp %q; "+
			"the timestamp must be embedded from Request.Timestamp: %s", ts, gaps[0].Detail)
	}
}

// TestEnclosingSectionChanged_ChangedSection_GapFragmentIsEmpty verifies that the gap's
// Fragment field is empty. The warning is advisory; no content needs to be pasted.
func TestEnclosingSectionChanged_ChangedSection_GapFragmentIsEmpty(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap")
	}
	if gaps[0].Fragment != "" {
		t.Errorf("gap Fragment must be empty (the warning is advisory; no paste fragment needed); got %q",
			gaps[0].Fragment)
	}
}

// TestEnclosingSectionChanged_ChangedSection_TransformSucceeds verifies that the
// transform succeeds when the section-change gap is emitted. The gap is informational
// only and must not abort the transform.
func TestEnclosingSectionChanged_ChangedSection_TransformSucceeds(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply returned error when section-change gap was emitted; the gap is informational "+
			"and must not abort the transform: %v", err)
	}
}

// TestEnclosingSectionChanged_ChangedSection_CustomContentPreserved verifies that the
// custom region's content is byte-identical to the deployed file's content despite the
// section change. The gap signals only; placement and content are unaffected.
func TestEnclosingSectionChanged_ChangedSection_CustomContentPreserved(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outCustom, ok := outDoc.Body().Custom("IdentityNote")
	if !ok {
		t.Fatal("<IdentityNote type=\"custom\"> absent from output; custom content must survive the section-change transform")
	}

	depDoc, err := docformat.Parse([]byte(enclosingSectionChangedDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depCustom, depOK := depDoc.Body().Custom("IdentityNote")
	if !depOK {
		t.Fatal("IdentityNote not found in deployed fixture; fixture is malformed")
	}

	if !bytes.Equal(outCustom.Content(), depCustom.Content()) {
		t.Errorf("IdentityNote content not byte-identical to deployed file:\n"+
			"deployed: %q\noutput:   %q", depCustom.Content(), outCustom.Content())
	}
}

// TestEnclosingSectionChanged_ChangedSection_Idempotent_SecondRunNoGap verifies idempotence:
// when the first run's output is used as the deployed input for a second run, the gap must
// not fire on the second run because the section own content is now already current.
func TestEnclosingSectionChanged_ChangedSection_Idempotent_SecondRunNoGap(t *testing.T) {
	req1 := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionChangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result1, err := transform.Apply(req1)
	if err != nil {
		t.Fatalf("Apply (first run): %v", err)
	}

	gaps1 := findEnclosingSectionChangedGaps(result1.Report.Gaps)
	if len(gaps1) == 0 {
		t.Fatal("first run: expected GapEnclosingSectionChanged; got none — " +
			"idempotence check cannot proceed without a valid first-run baseline")
	}

	req2 := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		result1.Output,
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result2, err := transform.Apply(req2)
	if err != nil {
		t.Fatalf("Apply (second run): %v", err)
	}

	gaps2 := findEnclosingSectionChangedGaps(result2.Report.Gaps)
	if len(gaps2) > 0 {
		t.Errorf("second run: GapEnclosingSectionChanged must not fire when section own content is unchanged; "+
			"the comparison must be symmetric so that a re-run on already-current output never fires: "+
			"got %d gap(s)", len(gaps2))
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Negative case (a): enclosing section content unchanged → no gap
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_UnchangedSection_WithCustom_NoGap verifies that no
// GapEnclosingSectionChanged gap is emitted when the enclosing section's own content
// is unchanged, even though a custom region is present inside it.
func TestEnclosingSectionChanged_UnchangedSection_WithCustom_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		[]byte(enclosingSectionUnchangedDeployed),
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) > 0 {
		t.Errorf("GapEnclosingSectionChanged emitted when the enclosing section is unchanged; "+
			"the gap must fire only when the section's own content actually changed: got %d gap(s)", len(gaps))
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Negative cases (b) and (d): unrelated section changed / no custom in changed section
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_UnrelatedSectionChanged_NoGap verifies that no
// GapEnclosingSectionChanged gap is emitted when a different core section (not the one
// containing the custom region) changed its own content. This is negative case (b):
// the enclosing section of IdentityNote did not change.
func TestEnclosingSectionChanged_UnrelatedSectionChanged_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionNegBDSource),
		[]byte(enclosingSectionNegBDDeployed),
		"enclosing-section-negbd-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) > 0 {
		t.Errorf("GapEnclosingSectionChanged emitted when the enclosing section of the custom region "+
			"did not change (only an unrelated Capabilities section changed); "+
			"detection must be gated on the custom region's immediate parent section: got %d gap(s)", len(gaps))
	}
}

// TestEnclosingSectionChanged_SectionWithNoCustomChanged_NoGap verifies that no
// GapEnclosingSectionChanged gap is emitted when a section that changed contains no
// directly-enclosed custom region. This is negative case (d): the Capabilities section
// changed but it has no custom region with ParentKind=NodeSection and ParentName=Capabilities.
func TestEnclosingSectionChanged_SectionWithNoCustomChanged_NoGap(t *testing.T) {
	// Same fixture as above — Capabilities changed but has no custom region.
	req := reorderReq(t,
		[]byte(enclosingSectionNegBDSource),
		[]byte(enclosingSectionNegBDDeployed),
		"enclosing-section-negbd-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Explicitly verify no gap mentions "Capabilities" (which has no custom region).
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapEnclosingSectionChanged && strings.Contains(g.Detail, "Capabilities") {
			t.Errorf("GapEnclosingSectionChanged emitted for Capabilities section even though "+
				"it contains no directly-enclosed custom region; the section must contain a custom "+
				"region with ParentKind=NodeSection for the gap to fire: detail=%s", g.Detail)
		}
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Negative case (c): create run (no deployed file) → no gap
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_CreateRun_NoGap verifies that a create run
// (req.Deployed == nil) produces no GapEnclosingSectionChanged gap. When there is no
// deployed file, there is no previous section content to compare against.
func TestEnclosingSectionChanged_CreateRun_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionChangedSource),
		nil, // create run — no deployed file
		"enclosing-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapEnclosingSectionChanged {
			t.Errorf("GapEnclosingSectionChanged emitted on a create run (Deployed == nil); "+
				"no previous section content exists to compare against on create: detail=%s", g.Detail)
		}
	}
}

// ---------------------------------------------------------------------------
// T2.4 — Coexistence: managed-parent custom region still uses GapDeployedRegionContentChanged
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_ManagedParentCustom_NoNewGap verifies that a custom region
// nested inside a managed region (ParentKind=NodeDeployed) continues to produce
// GapDeployedRegionContentChanged when the managed region's canonical content changes
// and does NOT additionally produce GapEnclosingSectionChanged.
//
// Reuses fixture 700 from deployed_region_content_changed_test.go, where ConstraintsNote
// is nested inside HarnessConstraints (managed), not directly inside Constraints (section).
func TestEnclosingSectionChanged_ManagedParentCustom_NoNewGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The existing GapDeployedRegionContentChanged must still fire for HarnessConstraints.
	managedGap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if managedGap == nil {
		t.Error("GapDeployedRegionContentChanged absent for HarnessConstraints; " +
			"the managed-region detection must continue to function alongside the new section detection")
	}

	// The new GapEnclosingSectionChanged must NOT fire: ConstraintsNote's parent is
	// HarnessConstraints (NodeDeployed), not the Constraints section (NodeSection).
	newGaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(newGaps) > 0 {
		t.Errorf("GapEnclosingSectionChanged produced for a custom region whose parent is a "+
			"managed region (NodeDeployed); this gap applies only to custom regions whose "+
			"immediate parent is a core section (NodeSection): got %d gap(s)", len(newGaps))
	}
}

// ---------------------------------------------------------------------------
// T2.4 — Coexistence: regenerated managed sub-region does not trigger section-change gap
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_ManagedSubRegionRegenerated_NoGap verifies that when a
// managed sub-region inside a core section is regenerated (its content changes), the
// core section's own content comparison remains equal — because SectionOwnContent excises
// the managed block from both sides — and no GapEnclosingSectionChanged fires.
//
// Reuses fixture 700-C from deployed_region_content_changed_test.go: HarnessConstraints
// has stale content and no nested user regions. Constraints section changes only via
// managed region regeneration; there is no custom region with ParentName=Constraints.
func TestEnclosingSectionChanged_ManagedSubRegionRegenerated_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedNoNestedDeployed),
		"content-changed-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	newGaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(newGaps) > 0 {
		t.Errorf("GapEnclosingSectionChanged fired when only a managed sub-region regenerated "+
			"inside a core section with no directly-enclosed custom regions; "+
			"SectionOwnContent must excise managed children from both sides so managed "+
			"regeneration does not register as a core-prose change: got %d gap(s)", len(newGaps))
	}
}

// ---------------------------------------------------------------------------
// T2.5 — Multiplicity: two custom regions in the same changed section → one gap
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_TwoCustomsInSameSection_OneGap verifies that two custom
// regions inside the same changed section produce exactly one GapEnclosingSectionChanged
// gap listing both custom region names.
func TestEnclosingSectionChanged_TwoCustomsInSameSection_OneGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionMultiCustomSource),
		[]byte(enclosingSectionMultiCustomDeployed),
		"enclosing-section-multi-custom-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap; expected one gap listing both custom region names")
	}
	if len(gaps) > 1 {
		t.Errorf("expected exactly one GapEnclosingSectionChanged gap for the Identity section "+
			"(two custom regions in the same section must yield one gap, not two); got %d gaps", len(gaps))
	}
}

// TestEnclosingSectionChanged_TwoCustomsInSameSection_BothNamedInDetail verifies that
// the single gap's Detail contains both custom region names.
func TestEnclosingSectionChanged_TwoCustomsInSameSection_BothNamedInDetail(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionMultiCustomSource),
		[]byte(enclosingSectionMultiCustomDeployed),
		"enclosing-section-multi-custom-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap; cannot verify Detail content")
	}
	detail := gaps[0].Detail
	if !strings.Contains(detail, "AlphaNote") {
		t.Errorf("gap Detail does not contain custom region name %q: %s", "AlphaNote", detail)
	}
	if !strings.Contains(detail, "ZetaNote") {
		t.Errorf("gap Detail does not contain custom region name %q: %s", "ZetaNote", detail)
	}
}

// TestEnclosingSectionChanged_TwoCustomsInSameSection_NamesSortedInDetail verifies that
// the custom region names in the gap's Detail appear in sorted ascending order. The
// fixture has ZetaNote before AlphaNote in the deployed file; the Detail must list
// AlphaNote before ZetaNote.
func TestEnclosingSectionChanged_TwoCustomsInSameSection_NamesSortedInDetail(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionMultiCustomSource),
		[]byte(enclosingSectionMultiCustomDeployed),
		"enclosing-section-multi-custom-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) == 0 {
		t.Fatal("no GapEnclosingSectionChanged gap; cannot verify name sort order")
	}
	detail := gaps[0].Detail
	alphaIdx := strings.Index(detail, "AlphaNote")
	zetaIdx := strings.Index(detail, "ZetaNote")

	if alphaIdx < 0 {
		t.Errorf("gap Detail does not contain %q: %s", "AlphaNote", detail)
	}
	if zetaIdx < 0 {
		t.Errorf("gap Detail does not contain %q: %s", "ZetaNote", detail)
	}
	if alphaIdx >= 0 && zetaIdx >= 0 && alphaIdx > zetaIdx {
		t.Errorf("AlphaNote at index %d appears after ZetaNote at index %d in the gap Detail; "+
			"names must be sorted ascending regardless of their order in the deployed file: %s",
			alphaIdx, zetaIdx, detail)
	}
}

// ---------------------------------------------------------------------------
// T2.5 — Multiplicity: two changed sections each with one custom region → two gaps
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_TwoSectionsChanged_TwoGaps verifies that when two core
// sections each changed their own content and each directly enclose a custom region,
// exactly two GapEnclosingSectionChanged gaps are produced — one per section.
func TestEnclosingSectionChanged_TwoSectionsChanged_TwoGaps(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionMultiSectionSource),
		[]byte(enclosingSectionMultiSectionDeployed),
		"enclosing-section-multi-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) < 2 {
		t.Errorf("expected two GapEnclosingSectionChanged gaps (one per changed section); got %d", len(gaps))
	}
	if len(gaps) > 2 {
		t.Errorf("expected exactly two GapEnclosingSectionChanged gaps; got %d", len(gaps))
	}
}

// TestEnclosingSectionChanged_TwoSectionsChanged_EachGapNamesSectionAndCustom verifies
// that each of the two gaps names its respective section and custom region in the Detail.
func TestEnclosingSectionChanged_TwoSectionsChanged_EachGapNamesSectionAndCustom(t *testing.T) {
	req := reorderReq(t,
		[]byte(enclosingSectionMultiSectionSource),
		[]byte(enclosingSectionMultiSectionDeployed),
		"enclosing-section-multi-section-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gaps := findEnclosingSectionChangedGaps(result.Report.Gaps)
	if len(gaps) < 2 {
		t.Fatalf("expected two GapEnclosingSectionChanged gaps; got %d — cannot verify individual gap content", len(gaps))
	}

	// Verify that the gaps collectively name Identity+IdentityExt and Capabilities+CapabilitiesExt.
	allDetails := gaps[0].Detail + " " + gaps[1].Detail

	if !strings.Contains(allDetails, "Identity") {
		t.Errorf("no gap Detail mentions the Identity section: details=%q", allDetails)
	}
	if !strings.Contains(allDetails, "IdentityExt") {
		t.Errorf("no gap Detail mentions the IdentityExt custom region: details=%q", allDetails)
	}
	if !strings.Contains(allDetails, "Capabilities") {
		t.Errorf("no gap Detail mentions the Capabilities section: details=%q", allDetails)
	}
	if !strings.Contains(allDetails, "CapabilitiesExt") {
		t.Errorf("no gap Detail mentions the CapabilitiesExt custom region: details=%q", allDetails)
	}
}

// TestEnclosingSectionChanged_TwoSectionsChanged_Deterministic verifies that two
// successive runs on the same inputs produce the same set of gaps in the same order,
// confirming deterministic output.
func TestEnclosingSectionChanged_TwoSectionsChanged_Deterministic(t *testing.T) {
	makeReq := func() transform.Request {
		return reorderReq(t,
			[]byte(enclosingSectionMultiSectionSource),
			[]byte(enclosingSectionMultiSectionDeployed),
			"enclosing-section-multi-section-test",
			"2026-08-17T22:41:49Z",
		)
	}

	result1, err := transform.Apply(makeReq())
	if err != nil {
		t.Fatalf("Apply (run 1): %v", err)
	}
	result2, err := transform.Apply(makeReq())
	if err != nil {
		t.Fatalf("Apply (run 2): %v", err)
	}

	gaps1 := findEnclosingSectionChangedGaps(result1.Report.Gaps)
	gaps2 := findEnclosingSectionChangedGaps(result2.Report.Gaps)

	if len(gaps1) != len(gaps2) {
		t.Errorf("non-deterministic gap count: run1=%d run2=%d", len(gaps1), len(gaps2))
		return
	}
	for i := range gaps1 {
		if gaps1[i].Detail != gaps2[i].Detail {
			t.Errorf("gap[%d] Detail differs between runs:\nrun1: %s\nrun2: %s",
				i, gaps1[i].Detail, gaps2[i].Detail)
		}
	}
}

// ---------------------------------------------------------------------------
// Fixture 805: Parking-coexistence negative test
//
// Source order:   Identity → Constraints
// Deployed order: Constraints → Identity → LegacyCapabilities
//
// Conditions:
//   - Identity and Constraints are swapped → reorder detected.
//   - LegacyCapabilities is present in the deployed file with a custom region
//     (LegacyNote) directly inside it. LegacyCapabilities is entirely absent
//     from the source document.
//   - Because LegacyCapabilities is absent from the source, the output body
//     has no LegacyCapabilities section; body.SectionDeep("LegacyCapabilities")
//     returns false. The custom region's parent is therefore absent from
//     sourceAnchorNames(body) and LegacyNote is parked.
//   - The LegacyCapabilities section prose in the deployed file is non-empty
//     and would differ from any output version — but since the output section
//     cannot be resolved, no comparison is performed.
//
// Expected after both Stage 7 (parking) and Stage 2 (section-change detection)
// are implemented:
//   - GapParkedCustomRegion is produced naming LegacyNote.
//   - GapEnclosingSectionChanged is NOT produced for LegacyCapabilities: the
//     detection must short-circuit when body.SectionDeep(parentName) returns
//     false, leaving only GapParkedCustomRegion in the gap list.
// ---------------------------------------------------------------------------

const parkCoexistSource = `---
id: 805
version: 2.0.0
name: park-coexist-test
description: Agent for parking-coexistence negative test
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: parking coexistence testing
required_skills: []
---

<Identity type="core">
# ParkCoexist Agent

You are the ParkCoexist agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>
`

// parkCoexistDeployed has sections in the order Constraints → Identity → LegacyCapabilities.
// The source has Identity → Constraints, so Identity and Constraints are swapped → reorder
// is detected.
//
// LegacyCapabilities is present in the deployed file but absent from the source.
// LegacyNote is nested in LegacyCapabilities: because its parent section is absent from
// sourceAnchorNames (the output body has no LegacyCapabilities), it is parked.
//
// The prose inside LegacyCapabilities is non-empty and would differ from any output version
// of that section (none exists). This simulates the scenario where both conditions apply
// simultaneously: the section has a custom region AND its own prose would differ — but the
// parking path must suppress GapEnclosingSectionChanged because the output section is
// unresolvable.
const parkCoexistDeployed = `---
id: 805
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for parking-coexistence negative test
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Constraints type="core">
## Constraints

Always be helpful.
</Constraints>

<Identity type="core">
# ParkCoexist Agent

You are the ParkCoexist agent.
</Identity>

<LegacyCapabilities type="core">
## Legacy Capabilities

This section describes legacy capabilities that were removed from the source.
This prose is present in the deployed file but LegacyCapabilities is entirely
absent from the source, so the output body has no LegacyCapabilities section.

<LegacyNote type="custom">
Project-specific note inside LegacyCapabilities — must be parked when the
reorder is detected and LegacyCapabilities is absent from the source.
</LegacyNote>
</LegacyCapabilities>
`

// ---------------------------------------------------------------------------
// Parking-coexistence: a parked section with a custom region must produce
// GapParkedCustomRegion only, not GapEnclosingSectionChanged.
// ---------------------------------------------------------------------------

// TestEnclosingSectionChanged_ParkedSection_NoEnclosingSectionChangedGap verifies
// the reorder/parking coexistence invariant documented in ContractsDesign.md:
// when a custom region's enclosing section is absent from the output body (the
// section was removed from the source, a reorder is detected, and the custom
// region is parked), no GapEnclosingSectionChanged is produced for that section.
//
// The implementation must short-circuit the section-change comparison when
// body.SectionDeep(parentName) returns false, so that a parked custom region
// cannot simultaneously produce both GapParkedCustomRegion and a spurious
// GapEnclosingSectionChanged.
//
// This test passes vacuously in the TDD RED phase (no implementation emits
// GapEnclosingSectionChanged) and becomes a binding regression guard once both
// Stage 7 parking and Stage 2 section-change detection are implemented.
func TestEnclosingSectionChanged_ParkedSection_NoEnclosingSectionChangedGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(parkCoexistSource),
		[]byte(parkCoexistDeployed),
		"park-coexist-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapEnclosingSectionChanged && strings.Contains(g.Detail, "LegacyCapabilities") {
			t.Errorf("GapEnclosingSectionChanged produced for LegacyCapabilities even "+
				"though that section is absent from the source and LegacyNote is parked; "+
				"when body.SectionDeep(parentName) returns false the comparison must be "+
				"skipped and only GapParkedCustomRegion must be emitted: detail=%s", g.Detail)
		}
	}
}

// TestEnclosingSectionChanged_ParkedSection_ParkingGapPresent verifies that the
// parking-coexistence fixture produces GapParkedCustomRegion naming LegacyNote.
// This positive assertion ensures the fixture exercises the parking path: if no
// parking gap fires, the coexistence invariant in the companion test above cannot
// be considered verified, because the fixture may not have triggered the relevant
// code path at all.
//
// This test passes immediately: parking was implemented in a prior stage and the
// fixture correctly triggers the parking path.
func TestEnclosingSectionChanged_ParkedSection_ParkingGapPresent(t *testing.T) {
	req := reorderReq(t,
		[]byte(parkCoexistSource),
		[]byte(parkCoexistDeployed),
		"park-coexist-test",
		"2026-08-17T22:41:49Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	parkingGap := findParkingGap(result.Report.Gaps)
	if parkingGap == nil {
		t.Fatal("no GapParkedCustomRegion produced; the fixture must trigger parking of " +
			"LegacyNote (LegacyCapabilities is absent from the source and a reorder is " +
			"detected) — if no parking gap is emitted, the coexistence invariant cannot " +
			"be verified because the parking path was not exercised")
	}
	if !strings.Contains(parkingGap.Detail, "LegacyNote") {
		t.Errorf("GapParkedCustomRegion Detail does not name LegacyNote: %s", parkingGap.Detail)
	}
}
