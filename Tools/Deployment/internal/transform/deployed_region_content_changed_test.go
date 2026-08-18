package transform_test

// deployed_region_content_changed_test.go covers GapDeployedRegionContentChanged emission
// when a managed region region's canonical content changes relative to the previously
// deployed file and the region contains nested user-owned regions (direct children).
//
// Key invariants under test:
//
//   - A deployed region whose canonical content changed AND which contains nested user-owned
//     regions produces exactly one GapDeployedRegionContentChanged gap naming the region
//     and all affected nested region names.
//   - No gap is emitted when canonical content is unchanged, regardless of nested content.
//   - No gap is emitted when the region has no nested user regions, regardless of content change.
//   - A top-level sibling user region adjacent to a changed deployed region produces no gap;
//     sibling regions are out of scope.
//   - A create run (no deployed file) produces no gap.
//   - Nested user content is preserved byte-identically across the content-change transform.
//   - Running the same transform twice does not produce the gap on the second (unchanged) run.
//   - GapDeployedRegionContentChanged maps to TodoInjections in the canonical gap-to-category map.
//
// CanonicalContent unit tests verify the excision and trimming rules independently of the
// full transform, so that the symmetric comparison contract can be tested in isolation.
//
// DeployedRegionContentChangedDetail formatter tests verify the rendered text produced by
// the pure formatter, including timestamp-optional variants and multi-name sorted output.
//
// Tests compile but fail at runtime until the Stage 5 implementation is complete (TDD RED
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
// Shared source fixture for content-change tests (fixture ID 700).
//
// All fixtures 700-702 share this source. The variation is in the deployed file's
// canonical content and whether nested user regions are present.
// ---------------------------------------------------------------------------

const contentChangedSource = `---
id: 700
version: 2.0.0
name: content-changed-test
description: Agent for deployed-region content-change detection tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: content change detection testing
required_skills: []
---

<Identity type="core">
# ContentChanged Agent

You are the ContentChanged agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture 700-A — Positive case: stale canonical content + nested custom-region region.
//
// The deployed HarnessConstraints region contains content that differs from what the
// fixture harness generates on a fresh transform. A <ConstraintsNote type="custom"> is nested
// directly inside it. Both conditions are met → gap must fire.
// ---------------------------------------------------------------------------

const contentChangedDeployed = `---
id: 700
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for deployed-region content-change detection tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# ContentChanged Agent

You are the ContentChanged agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Stale harness content that does not match the current fixture harness output.
This canonical content will be replaced, making the comparison unequal.

<ConstraintsNote type="custom">
User note nested inside HarnessConstraints — must survive regeneration and trigger the TODO.
</ConstraintsNote>
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture 700-B — Negative case 1: CURRENT canonical content + nested custom-region region.
//
// The deployed HarnessConstraints region contains the SAME canonical content that the
// fixture harness will regenerate. CanonicalContent of both sides compares equal →
// no gap, even though nested content is present.
//
// The canonical text matches the fixture harness output in testdata/transform/fixture.yaml.
// ---------------------------------------------------------------------------

const contentUnchangedDeployed = `---
id: 700
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for deployed-region content-change detection tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# ContentChanged Agent

You are the ContentChanged agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.

<ConstraintsNote type="custom">
User note nested inside HarnessConstraints — unchanged content so no TODO should fire.
</ConstraintsNote>
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture 700-C — Negative case 2: stale canonical content, NO nested user regions.
//
// HarnessConstraints has stale content (will change on regeneration) but no nested
// custom-region or project-injection region regions. The nested-regions condition is not met →
// no gap even though canonical content changed.
// ---------------------------------------------------------------------------

const contentChangedNoNestedDeployed = `---
id: 700
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for deployed-region content-change detection tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# ContentChanged Agent

You are the ContentChanged agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Stale harness content with no nested user regions present.
Content changes but the nested-regions condition is not met, so no gap fires.
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// Fixture 703 — Negative case 3: stale canonical content + SIBLING custom-region region.
//
// HarnessConstraints has stale content AND there is a <SiblingNote type="custom"> region inside
// <Constraints type="core"> but OUTSIDE <HarnessConstraints type="managed"> (a sibling, not a
// nested child). Sibling user regions are explicitly out of scope for gap detection.
// ---------------------------------------------------------------------------

const contentChangedWithSiblingSource = `---
id: 703
version: 2.0.0
name: content-changed-sibling-test
description: Agent for sibling-custom negative tests
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: sibling custom testing
required_skills: []
---

<Identity type="core">
# ContentChangedSibling Agent

You are the ContentChangedSibling agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

const contentChangedWithSiblingDeployed = `---
id: 703
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for sibling-custom negative tests
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# ContentChangedSibling Agent

You are the ContentChangedSibling agent.
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Stale harness content that will be replaced by the current fixture harness output.
</HarnessConstraints>

<SiblingNote type="custom">
A custom region adjacent to HarnessConstraints — a sibling, not a direct nested child.
Sibling regions are out of scope and must never produce GapDeployedRegionContentChanged.
</SiblingNote>
</Constraints>
`

// ---------------------------------------------------------------------------
// Helper: find a GapDeployedRegionContentChanged gap by deployed region name.
// Returns nil when no matching gap is found.
// ---------------------------------------------------------------------------

func findContentChangedGap(gaps []domain.Gap, regionName string) *domain.Gap {
	for i := range gaps {
		if gaps[i].Kind == domain.GapDeployedRegionContentChanged && gaps[i].Subject == regionName {
			return &gaps[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper: build a minimal parseable document containing one managed region region.
// regionContent goes between the region opening and closing tags verbatim.
// Used to construct Node arguments for CanonicalContent unit tests.
// ---------------------------------------------------------------------------

func minimalDeployedDoc(regionName, regionContent string) []byte {
	return []byte("---\nid: 0\nversion: 1.0.0\ntransform_version: 3.0.0\ninjections_version: 1.0.0\n" +
		"description: canonical content test\nmode: subagent\nmodel: claude/claude-sonnet\ntools: []\n---\n\n" +
		"<" + regionName + " type=\"managed\">\n" + regionContent +
		"</" + regionName + ">\n")
}

// ---------------------------------------------------------------------------
// CanonicalContent unit tests
//
// These tests exercise CanonicalContent directly, without running a full transform,
// to verify the excision and trimming rules independently. They fail in the TDD RED
// phase until Stage 5 task I5.3 is implemented.
// ---------------------------------------------------------------------------

// TestCanonicalContent_NilNode_ReturnsNil verifies the nil-safety contract:
// CanonicalContent(nil) returns nil without panicking.
func TestCanonicalContent_NilNode_ReturnsNil(t *testing.T) {
	result := transform.CanonicalContent(nil)
	if result != nil {
		t.Errorf("CanonicalContent(nil): want nil, got %q", result)
	}
}

// TestCanonicalContent_NodeWithNoNestedRegions_ReturnsTrimmedContent verifies that a
// deployed node with no user-owned child regions returns its content bytes trimmed of
// leading and trailing whitespace.
func TestCanonicalContent_NodeWithNoNestedRegions_ReturnsTrimmedContent(t *testing.T) {
	const regionBody = "Some canonical content here.\nSecond line.\n"
	doc, err := docformat.Parse(minimalDeployedDoc("TestRegion", regionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in parsed document; test fixture is malformed")
	}

	got := transform.CanonicalContent(node)

	if got == nil {
		t.Fatal("CanonicalContent returned nil for a non-nil node with content; must return the trimmed content bytes")
	}
	want := []byte("Some canonical content here.\nSecond line.")
	if !bytes.Equal(got, want) {
		t.Errorf("CanonicalContent (no nested regions): want %q, got %q", want, got)
	}
}

// TestCanonicalContent_NodeWithCustomChild_ExcisesCustomBytes verifies that a deployed
// node with a direct custom-region child has the full marker block of the custom region
// removed from the returned canonical bytes. The remaining content is trimmed.
func TestCanonicalContent_NodeWithCustomChild_ExcisesCustomBytes(t *testing.T) {
	const regionBody = "Canonical line one.\n\n<UserNote type=\"custom\">\nUser content here.\n</UserNote>\n"
	doc, err := docformat.Parse(minimalDeployedDoc("TestRegion", regionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in parsed document")
	}

	got := transform.CanonicalContent(node)

	if got == nil {
		t.Fatal("CanonicalContent returned nil for a non-nil node; must return canonical bytes with custom block excised")
	}
	if !bytes.Contains(got, []byte("Canonical line one.")) {
		t.Errorf("CanonicalContent: canonical content absent from result: %q", got)
	}
	if bytes.Contains(got, []byte("<UserNote type=\"custom\">")) {
		t.Errorf("CanonicalContent: custom region open tag must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("User content here.")) {
		t.Errorf("CanonicalContent: custom region inner content must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("</UserNote>")) {
		t.Errorf("CanonicalContent: custom region close tag must be excised; got: %q", got)
	}
}

// TestCanonicalContent_NodeWithInjectionChild_ExcisesInjectionBytes verifies that a
// deployed node with a direct project-injection region child has the injection's full marker block
// removed. This is the injection-provenance counterpart to the custom test.
func TestCanonicalContent_NodeWithInjectionChild_ExcisesInjectionBytes(t *testing.T) {
	const regionBody = "Canonical content.\n\n<UserInj type=\"project\">\nInjection content.\n</UserInj>\n"
	doc, err := docformat.Parse(minimalDeployedDoc("TestRegion", regionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in parsed document")
	}

	got := transform.CanonicalContent(node)

	if got == nil {
		t.Fatal("CanonicalContent returned nil for a non-nil node; must return canonical bytes with injection block excised")
	}
	if !bytes.Contains(got, []byte("Canonical content.")) {
		t.Errorf("CanonicalContent: canonical content absent from result: %q", got)
	}
	if bytes.Contains(got, []byte("<UserInj type=\"project\">")) {
		t.Errorf("CanonicalContent: injection open tag must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("Injection content.")) {
		t.Errorf("CanonicalContent: injection inner content must be excised; got: %q", got)
	}
}

// TestCanonicalContent_SymmetricOnBothSides verifies the core symmetry contract: applying
// CanonicalContent to a deployed region (with nested user bytes) and to a freshly generated
// region (canonical bytes only) yields equal results when the canonical content did not
// change. Asymmetry would cause a permanently-firing gap or a never-firing gap.
func TestCanonicalContent_SymmetricOnBothSides(t *testing.T) {
	const canonicalLines = "Canonical line A.\nCanonical line B.\n"

	// Deployed side: canonical lines + nested custom-region region.
	deployedBody := canonicalLines + "\n<SideNote type=\"custom\">\nUser side note.\n</SideNote>\n"
	deployedDoc, err := docformat.Parse(minimalDeployedDoc("TestRegion", deployedBody))
	if err != nil {
		t.Fatalf("parse deployed document: %v", err)
	}
	deployedNode, ok := deployedDoc.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in deployed document")
	}

	// Fresh side: canonical lines only (no nested user regions, as the generator produces).
	freshDoc, err := docformat.Parse(minimalDeployedDoc("TestRegion", canonicalLines))
	if err != nil {
		t.Fatalf("parse fresh document: %v", err)
	}
	freshNode, ok := freshDoc.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in fresh document")
	}

	deployedCanonical := transform.CanonicalContent(deployedNode)
	freshCanonical := transform.CanonicalContent(freshNode)

	if deployedCanonical == nil || freshCanonical == nil {
		t.Fatalf("CanonicalContent returned nil for a non-nil node with content: "+
			"deployed=%v fresh=%v; must return canonical bytes so that the comparison is meaningful",
			deployedCanonical, freshCanonical)
	}
	if !bytes.Equal(deployedCanonical, freshCanonical) {
		t.Errorf("CanonicalContent is not symmetric:\n"+
			"deployed canonical (nested excised): %q\n"+
			"fresh canonical (no nested):          %q\n"+
			"Asymmetry would cause a permanently-firing gap on every run with nested content.",
			deployedCanonical, freshCanonical)
	}
}

// TestCanonicalContent_LeadingTrailingWhitespaceTrimmed verifies the normalisation rule:
// leading and trailing whitespace of the whole result is trimmed. This absorbs separator
// and newline noise introduced at the re-emission boundary without treating it as a
// content change.
func TestCanonicalContent_LeadingTrailingWhitespaceTrimmed(t *testing.T) {
	const baseContent = "Line one.\nLine two."
	// Region body with extra leading and trailing whitespace/newlines.
	bodyWithWhitespace := "\n\n" + baseContent + "\n\n"

	doc, err := docformat.Parse(minimalDeployedDoc("TestRegion", bodyWithWhitespace))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in parsed document")
	}

	got := transform.CanonicalContent(node)
	want := []byte(baseContent)

	if got == nil {
		t.Fatal("CanonicalContent returned nil; must return trimmed content")
	}
	if !bytes.Equal(got, want) {
		t.Errorf("CanonicalContent did not trim leading/trailing whitespace:\nwant: %q\ngot:  %q", want, got)
	}
}

// TestCanonicalContent_InteriorWhitespaceDifference_CountsAsChange verifies that two
// deployed regions whose canonical content differs only by interior whitespace produce
// distinct CanonicalContent results. Interior whitespace-only differences are NOT
// normalised away — only leading/trailing whitespace of the whole result is trimmed.
//
// This guards against over-eager normalisation (e.g. collapsing all runs of whitespace,
// stripping every line, or trimming mid-region) that would cause a real whitespace-only
// content change to silently never fire a gap.
func TestCanonicalContent_InteriorWhitespaceDifference_CountsAsChange(t *testing.T) {
	// Two regions with identical canonical text, differing only by an interior blank line.
	const bodyWithoutBlankLine = "Line one.\nLine two.\n"
	const bodyWithBlankLine = "Line one.\n\nLine two.\n"

	docA, err := docformat.Parse(minimalDeployedDoc("TestRegion", bodyWithoutBlankLine))
	if err != nil {
		t.Fatalf("parse document A: %v", err)
	}
	nodeA, ok := docA.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in document A; test fixture is malformed")
	}

	docB, err := docformat.Parse(minimalDeployedDoc("TestRegion", bodyWithBlankLine))
	if err != nil {
		t.Fatalf("parse document B: %v", err)
	}
	nodeB, ok := docB.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in document B; test fixture is malformed")
	}

	canonA := transform.CanonicalContent(nodeA)
	canonB := transform.CanonicalContent(nodeB)

	if canonA == nil {
		t.Fatal("CanonicalContent(nodeA) returned nil; must return canonical bytes")
	}
	if canonB == nil {
		t.Fatal("CanonicalContent(nodeB) returned nil; must return canonical bytes")
	}
	if bytes.Equal(canonA, canonB) {
		t.Errorf("CanonicalContent produced identical results for two regions whose content "+
			"differs by an interior blank line; interior whitespace differences must count as a "+
			"change and must NOT be normalised away:\n"+
			"without blank line: %q\n"+
			"with blank line:    %q", canonA, canonB)
	}
}

// TestCanonicalContent_MultipleUserOwnedChildren_AllExcised verifies that when a deployed
// node has multiple direct user-owned children (a mix of custom-region and project-injection region),
// all of them are excised from the canonical result.
func TestCanonicalContent_MultipleUserOwnedChildren_AllExcised(t *testing.T) {
	const regionBody = "Before.\n\n" +
		"<CustomOne type=\"custom\">\nCustom one content.\n</CustomOne>\n\n" +
		"Middle.\n\n" +
		"<InjOne type=\"project\">\nInjection one content.\n</InjOne>\n\n" +
		"After.\n"

	doc, err := docformat.Parse(minimalDeployedDoc("TestRegion", regionBody))
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := doc.Body().Deployed("TestRegion")
	if !ok {
		t.Fatal("TestRegion not found in parsed document")
	}

	got := transform.CanonicalContent(node)

	if got == nil {
		t.Fatal("CanonicalContent returned nil for a non-nil node")
	}
	if bytes.Contains(got, []byte("Custom one content.")) {
		t.Errorf("CanonicalContent: CustomOne content must be excised; got: %q", got)
	}
	if bytes.Contains(got, []byte("Injection one content.")) {
		t.Errorf("CanonicalContent: InjOne content must be excised; got: %q", got)
	}
	if !bytes.Contains(got, []byte("Before.")) {
		t.Errorf("CanonicalContent: canonical text 'Before.' must be retained; got: %q", got)
	}
	if !bytes.Contains(got, []byte("Middle.")) {
		t.Errorf("CanonicalContent: canonical text 'Middle.' must be retained; got: %q", got)
	}
	if !bytes.Contains(got, []byte("After.")) {
		t.Errorf("CanonicalContent: canonical text 'After.' must be retained; got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// DeployedRegionContentChangedDetail formatter tests
//
// These tests exercise the pure formatter directly. They pass in the TDD RED phase once
// the formatter is implemented (pure formatters are contract files, not implementation).
// They serve as permanent regression guards for the formatter's rendered text contract.
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChangedDetail_WithTimestamp verifies that the formatter produces
// text naming the deployed region, all nested region names, and the timestamp when all
// parameters are supplied.
func TestDeployedRegionContentChangedDetail_WithTimestamp(t *testing.T) {
	detail := transform.DeployedRegionContentChangedDetail(
		"CommunicationProtocol",
		[]string{"ProtocolExtension", "TeamNotes"},
		"2026-08-12T14:25:33Z",
	)

	if !strings.Contains(detail, "CommunicationProtocol") {
		t.Errorf("detail does not name the deployed region %q: %s", "CommunicationProtocol", detail)
	}
	if !strings.Contains(detail, "ProtocolExtension") {
		t.Errorf("detail does not name nested region %q: %s", "ProtocolExtension", detail)
	}
	if !strings.Contains(detail, "TeamNotes") {
		t.Errorf("detail does not name nested region %q: %s", "TeamNotes", detail)
	}
	if !strings.Contains(detail, "2026-08-12T14:25:33Z") {
		t.Errorf("detail does not contain the caller-supplied timestamp %q: %s", "2026-08-12T14:25:33Z", detail)
	}
}

// TestDeployedRegionContentChangedDetail_EmptyTimestamp_OmitsClause verifies that when the
// timestamp is empty, the "(detected …)" clause is omitted. The rest of the message must
// still name the region and all nested region names.
func TestDeployedRegionContentChangedDetail_EmptyTimestamp_OmitsClause(t *testing.T) {
	detail := transform.DeployedRegionContentChangedDetail(
		"CommunicationProtocol",
		[]string{"ProtocolExtension"},
		"",
	)

	if !strings.Contains(detail, "CommunicationProtocol") {
		t.Errorf("detail does not name the deployed region with empty timestamp: %s", detail)
	}
	if strings.Contains(detail, "(detected") {
		t.Errorf("detail contains a timestamp clause when timestamp is empty; the clause must be omitted: %s", detail)
	}
}

// TestDeployedRegionContentChangedDetail_SingleNestedName verifies the formatter handles a
// single nested region name, which is the common case.
func TestDeployedRegionContentChangedDetail_SingleNestedName(t *testing.T) {
	detail := transform.DeployedRegionContentChangedDetail("MyRegion", []string{"MyInjection"}, "")

	if !strings.Contains(detail, "MyRegion") {
		t.Errorf("detail does not name the deployed region: %s", detail)
	}
	if !strings.Contains(detail, "MyInjection") {
		t.Errorf("detail does not name the nested region: %s", detail)
	}
}

// TestDeployedRegionContentChangedDetail_MultipleNestedNames_RelativeOrder verifies that
// the formatter renders nested names in the order supplied. Names must be pre-sorted by the
// caller (the formatter does not sort internally); this test supplies a sorted list and
// verifies that their relative byte order in the output is preserved.
func TestDeployedRegionContentChangedDetail_MultipleNestedNames_RelativeOrder(t *testing.T) {
	names := []string{"AlphaExt", "BetaExt", "ZetaExt"}
	detail := transform.DeployedRegionContentChangedDetail("MyRegion", names, "")

	alphaIdx := strings.Index(detail, "AlphaExt")
	betaIdx := strings.Index(detail, "BetaExt")
	zetaIdx := strings.Index(detail, "ZetaExt")

	if alphaIdx < 0 {
		t.Errorf("detail does not contain %q: %s", "AlphaExt", detail)
	}
	if betaIdx < 0 {
		t.Errorf("detail does not contain %q: %s", "BetaExt", detail)
	}
	if zetaIdx < 0 {
		t.Errorf("detail does not contain %q: %s", "ZetaExt", detail)
	}
	if alphaIdx >= 0 && betaIdx >= 0 && alphaIdx > betaIdx {
		t.Errorf("AlphaExt at index %d appears after BetaExt at %d; names must appear in the order supplied (sorted ascending by caller)",
			alphaIdx, betaIdx)
	}
	if betaIdx >= 0 && zetaIdx >= 0 && betaIdx > zetaIdx {
		t.Errorf("BetaExt at index %d appears after ZetaExt at %d; names must appear in the order supplied",
			betaIdx, zetaIdx)
	}
}

// TestDeployedRegionContentChangedDetail_NonEmpty verifies that the formatter always returns
// a non-empty string for valid inputs.
func TestDeployedRegionContentChangedDetail_NonEmpty(t *testing.T) {
	detail := transform.DeployedRegionContentChangedDetail("MyRegion", []string{"MyInj"}, "")
	if len(detail) == 0 {
		t.Error("DeployedRegionContentChangedDetail returned empty string for valid inputs; must return non-empty detail text")
	}
}

// TestDeployedRegionContentChangedDetail_EmptyTimestamp_NoTrailingSpace verifies that when
// the timestamp is empty, the detail text does not end with a trailing space or an incomplete
// clause. The text must end cleanly after the name list.
func TestDeployedRegionContentChangedDetail_EmptyTimestamp_NoTrailingSpace(t *testing.T) {
	detail := transform.DeployedRegionContentChangedDetail("MyRegion", []string{"MyInj"}, "")
	if len(detail) == 0 {
		t.Fatal("DeployedRegionContentChangedDetail returned empty string")
	}
	last := detail[len(detail)-1]
	if last == ' ' {
		t.Errorf("detail ends with a trailing space when timestamp is empty; detail: %q", detail)
	}
}

// TestDeployedRegionContentChangedDetail_DifferentTimestamps_ProduceDifferentOutputs verifies
// that two calls differing only in timestamp produce different output. The timestamp must be
// reflected in the rendered text.
func TestDeployedRegionContentChangedDetail_DifferentTimestamps_ProduceDifferentOutputs(t *testing.T) {
	ts1 := "2026-08-12T14:25:33Z"
	ts2 := "2026-08-12T18:00:00Z"

	d1 := transform.DeployedRegionContentChangedDetail("MyRegion", []string{"MyInj"}, ts1)
	d2 := transform.DeployedRegionContentChangedDetail("MyRegion", []string{"MyInj"}, ts2)

	if !strings.Contains(d1, ts1) {
		t.Errorf("detail with timestamp %q does not contain the timestamp: %s", ts1, d1)
	}
	if !strings.Contains(d2, ts2) {
		t.Errorf("detail with timestamp %q does not contain the timestamp: %s", ts2, d2)
	}
	if d1 == d2 {
		t.Errorf("DeployedRegionContentChangedDetail produced identical output for different timestamps:\nd1: %s\nd2: %s", d1, d2)
	}
}

// TestDeployedRegionContentChangedDetail_StatesThatContentChanged verifies that the detail
// text communicates that canonical content changed and the user should review their nested
// content. The exact wording is flexible; the test checks for key semantic indicators.
func TestDeployedRegionContentChangedDetail_StatesThatContentChanged(t *testing.T) {
	detail := transform.DeployedRegionContentChangedDetail("MyRegion", []string{"MyInj"}, "")

	// The detail must communicate a content change and an actionable review instruction.
	hasMeaningfulContent := strings.Contains(detail, "changed") ||
		strings.Contains(detail, "updated") ||
		strings.Contains(detail, "review") ||
		strings.Contains(detail, "Review")
	if !hasMeaningfulContent {
		t.Errorf("detail does not communicate a content change or review instruction; "+
			"users need actionable guidance from the detail text: %s", detail)
	}
}

// ---------------------------------------------------------------------------
// Gap-to-category registration (AC5.7)
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChanged_GapCategoryIsInjections verifies that
// GapDeployedRegionContentChanged is registered in the canonical gap-to-category map with
// category TodoInjections. When a gap kind has no entry in gapToCategory, AddGap silently
// falls back to TodoManual — a defect. This test catches that defect.
func TestDeployedRegionContentChanged_GapCategoryIsInjections(t *testing.T) {
	c := todo.NewCollector()
	c.AddGap(domain.Gap{
		Kind:    domain.GapDeployedRegionContentChanged,
		Subject: "TestRegion",
		Detail:  "test detail text",
	})

	items := c.Items()
	if len(items) == 0 {
		t.Fatal("AddGap(GapDeployedRegionContentChanged) produced no TodoItem; expected one item")
	}
	if items[0].Category != domain.TodoInjections {
		t.Errorf("GapDeployedRegionContentChanged category: want %q (TodoInjections), got %q; "+
			"add GapDeployedRegionContentChanged → TodoInjections to gapToCategory in collect.go",
			domain.TodoInjections, items[0].Category)
	}
}

// ---------------------------------------------------------------------------
// T5.1 — Positive case: changed canonical content + nested user region emits gap
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChanged_ChangedContent_WithNested_EmitsGap verifies that when a
// deployed region's canonical content changes and the region contains nested user-owned
// regions, exactly one GapDeployedRegionContentChanged gap is produced.
func TestDeployedRegionContentChanged_ChangedContent_WithNested_EmitsGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap == nil {
		t.Fatal("expected GapDeployedRegionContentChanged for HarnessConstraints " +
			"(canonical content changed, nested user region present); got no such gap")
	}
}

// TestDeployedRegionContentChanged_ChangedContent_ExactlyOneGap verifies cardinality: when
// the conditions are met, exactly one GapDeployedRegionContentChanged gap is produced for
// the affected region — not zero, not two.
func TestDeployedRegionContentChanged_ChangedContent_ExactlyOneGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var matching []domain.Gap
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapDeployedRegionContentChanged && g.Subject == "HarnessConstraints" {
			matching = append(matching, g)
		}
	}
	if len(matching) == 0 {
		t.Fatal("no GapDeployedRegionContentChanged gap for HarnessConstraints; expected exactly one")
	}
	if len(matching) > 1 {
		t.Errorf("expected exactly one GapDeployedRegionContentChanged gap for HarnessConstraints; got %d", len(matching))
	}
}

// TestDeployedRegionContentChanged_ChangedContent_GapSubjectIsRegionName verifies that the
// gap's Subject is the deployed region name — not the artifact key. Content-change gaps are
// per-region, so the region name is the relevant identifier for subject-based grouping.
func TestDeployedRegionContentChanged_ChangedContent_GapSubjectIsRegionName(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap == nil {
		t.Fatal("no GapDeployedRegionContentChanged gap for HarnessConstraints")
	}
	if gap.Subject != "HarnessConstraints" {
		t.Errorf("gap Subject: want %q (deployed region name), got %q; subject must be the region name, not the artifact key",
			"HarnessConstraints", gap.Subject)
	}
}

// TestDeployedRegionContentChanged_ChangedContent_GapDetailContainsRegionName verifies that
// the gap's Detail field names the deployed region whose canonical content changed.
func TestDeployedRegionContentChanged_ChangedContent_GapDetailContainsRegionName(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap == nil {
		t.Fatal("no GapDeployedRegionContentChanged gap")
	}
	if !strings.Contains(gap.Detail, "HarnessConstraints") {
		t.Errorf("gap Detail does not contain the deployed region name %q: %s",
			"HarnessConstraints", gap.Detail)
	}
}

// TestDeployedRegionContentChanged_ChangedContent_GapDetailContainsNestedName verifies that
// the gap's Detail field names the nested user region(s) that may be affected by the change.
func TestDeployedRegionContentChanged_ChangedContent_GapDetailContainsNestedName(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap == nil {
		t.Fatal("no GapDeployedRegionContentChanged gap")
	}
	if !strings.Contains(gap.Detail, "ConstraintsNote") {
		t.Errorf("gap Detail does not contain nested region name %q: %s",
			"ConstraintsNote", gap.Detail)
	}
}

// TestDeployedRegionContentChanged_ChangedContent_GapDetailContainsTimestamp verifies that
// the gap's Detail field contains the caller-supplied timestamp from Request.Timestamp.
func TestDeployedRegionContentChanged_ChangedContent_GapDetailContainsTimestamp(t *testing.T) {
	const ts = "2026-08-12T14:25:33Z"
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		ts,
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap == nil {
		t.Fatal("no GapDeployedRegionContentChanged gap")
	}
	if !strings.Contains(gap.Detail, ts) {
		t.Errorf("gap Detail does not contain caller-supplied timestamp %q; "+
			"the formatter must embed the timestamp from Request.Timestamp: %s", ts, gap.Detail)
	}
}

// TestDeployedRegionContentChanged_ChangedContent_GapFragmentIsEmpty verifies that the gap's
// Fragment field is empty. The user's content is already present in the output inside the
// deployed region; no literal paste-fragment is needed.
func TestDeployedRegionContentChanged_ChangedContent_GapFragmentIsEmpty(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap == nil {
		t.Fatal("no GapDeployedRegionContentChanged gap")
	}
	if gap.Fragment != "" {
		t.Errorf("gap Fragment must be empty (content is already in the output at the nested location); got %q",
			gap.Fragment)
	}
}

// TestDeployedRegionContentChanged_ChangedContent_TransformSucceeds verifies that the
// transform still succeeds when the content-change gap is emitted. The gap is informational
// only and must not abort the transform.
func TestDeployedRegionContentChanged_ChangedContent_TransformSucceeds(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	_, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply returned error when content-change gap was emitted; the gap is informational "+
			"and must not abort the transform: %v", err)
	}
}

// TestDeployedRegionContentChanged_ChangedContent_NestedContentByteIdentical verifies that
// the nested custom-region region's content is byte-identical to the deployed file's content
// despite the canonical content change. The gap is signalling only; placement and content
// are unaffected.
func TestDeployedRegionContentChanged_ChangedContent_NestedContentByteIdentical(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outCustom, ok := outDoc.Body().Custom("ConstraintsNote")
	if !ok {
		t.Fatal("<ConstraintsNote type=\"custom\"> absent from output; nested content must survive the content-change transform")
	}

	depDoc, err := docformat.Parse([]byte(contentChangedDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depCustom, depOK := depDoc.Body().Custom("ConstraintsNote")
	if !depOK {
		t.Fatal("ConstraintsNote not found in deployed fixture; fixture is malformed")
	}

	if !bytes.Equal(outCustom.Content(), depCustom.Content()) {
		t.Errorf("ConstraintsNote content not byte-identical to deployed file:\n"+
			"deployed: %q\noutput:   %q", depCustom.Content(), outCustom.Content())
	}
}

// TestDeployedRegionContentChanged_ChangedContent_NestedContentInsideDeployedParent verifies
// that the nested custom-region region remains inside the <HarnessConstraints type="managed"> parent
// in the output, not promoted to body level.
func TestDeployedRegionContentChanged_ChangedContent_NestedContentInsideDeployedParent(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	outCustom, ok := outDoc.Body().Custom("ConstraintsNote")
	if !ok {
		t.Fatal("<ConstraintsNote type=\"custom\"> absent from output")
	}

	parent := outCustom.Parent()
	if parent == nil {
		t.Fatal("ConstraintsNote is at body top level in the output; it must remain nested inside <HarnessConstraints type=\"managed\">")
	}
	if parent.Kind() != docformat.NodeDeployed {
		t.Errorf("ConstraintsNote parent kind: want NodeDeployed, got %q", parent.Kind())
	}
	if parent.Name() != "HarnessConstraints" {
		t.Errorf("ConstraintsNote parent name: want %q, got %q", "HarnessConstraints", parent.Name())
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Negative case 1: unchanged canonical content + nested region → no gap
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChanged_UnchangedContent_WithNested_NoGap verifies that no
// GapDeployedRegionContentChanged gap is emitted when the deployed region's canonical
// content is unchanged, even though nested user regions are present. The like-for-like
// comparison must produce equal results.
func TestDeployedRegionContentChanged_UnchangedContent_WithNested_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentUnchangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap != nil {
		t.Errorf("GapDeployedRegionContentChanged emitted for HarnessConstraints even though canonical content is unchanged; "+
			"the gap must fire only when canonical content actually changed: detail=%s", gap.Detail)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Negative case 2: changed canonical content + no nested regions → no gap
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChanged_ChangedContent_NoNested_NoGap verifies that no
// GapDeployedRegionContentChanged gap is emitted when canonical content changes but the
// region contains no nested user-owned regions. Both conditions must hold simultaneously.
func TestDeployedRegionContentChanged_ChangedContent_NoNested_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedNoNestedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap != nil {
		t.Errorf("GapDeployedRegionContentChanged emitted for HarnessConstraints even though no nested user regions are present; "+
			"changed canonical content alone is not sufficient — nested user regions must also be present: detail=%s", gap.Detail)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Negative case 3: top-level sibling user region → no gap
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChanged_ChangedContent_SiblingCustom_NoGap verifies that a user
// region adjacent to (sibling of) the changed deployed region does not produce a gap.
// Only direct nested children are in scope; top-level sibling regions are out of scope.
func TestDeployedRegionContentChanged_ChangedContent_SiblingCustom_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedWithSiblingSource),
		[]byte(contentChangedWithSiblingDeployed),
		"content-changed-sibling-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findContentChangedGap(result.Report.Gaps, "HarnessConstraints")
	if gap != nil {
		t.Errorf("GapDeployedRegionContentChanged emitted for HarnessConstraints when the only user region "+
			"(SiblingNote) is a section-level sibling, not a direct nested child of the deployed region; "+
			"sibling regions are explicitly out of scope: detail=%s", gap.Detail)
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Negative case 4: create run (no deployed file) → no gap
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChanged_CreateRun_NoGap verifies that a create run
// (req.Deployed == nil) produces no GapDeployedRegionContentChanged gap. When there is no
// deployed file, there is no previous canonical content to compare against.
func TestDeployedRegionContentChanged_CreateRun_NoGap(t *testing.T) {
	req := reorderReq(t,
		[]byte(contentChangedSource),
		nil, // create run — no deployed file
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapDeployedRegionContentChanged {
			t.Errorf("GapDeployedRegionContentChanged emitted on a create run (Deployed == nil); "+
				"no previous canonical content exists to compare against on create: subject=%q detail=%s",
				g.Subject, g.Detail)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.3 — Idempotence: repeat transform on unchanged input emits no gap on second run
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChanged_Idempotent_FirstRunEmitsGap_SecondRunDoesNot verifies the
// idempotence requirement: when the same transform is applied twice in succession using the
// output of the first run as the deployed input for the second, the gap fires only on the
// first run (canonical content changed from stale to current) and not on the second (canonical
// content is already current and unchanged).
//
// This test guards against asymmetric comparison: a permanently-firing gap (CanonicalContent
// does not excise nested bytes from the deployed side, so it never equals the fresh side) or a
// never-firing gap (comparison is wrong in the opposite direction).
func TestDeployedRegionContentChanged_Idempotent_FirstRunEmitsGap_SecondRunDoesNot(t *testing.T) {
	// First run: stale deployed content + nested custom → canonical content changes.
	req1 := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result1, err := transform.Apply(req1)
	if err != nil {
		t.Fatalf("Apply (first run): %v", err)
	}

	// First run must emit the gap (stale content replaced by fresh content).
	gap1 := findContentChangedGap(result1.Report.Gaps, "HarnessConstraints")
	if gap1 == nil {
		t.Fatal("first run: expected GapDeployedRegionContentChanged for HarnessConstraints " +
			"(stale deployed content → fresh canonical content); got no gap — " +
			"idempotence second-run check cannot proceed without a valid first-run baseline")
	}

	// Second run: use the first run's output as the deployed input. The canonical content
	// in the output now matches what the harness regenerates, so the comparison must
	// yield equal results and no gap must fire.
	req2 := reorderReq(t,
		[]byte(contentChangedSource),
		result1.Output, // first run output: canonical content is now current
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)

	result2, err := transform.Apply(req2)
	if err != nil {
		t.Fatalf("Apply (second run): %v", err)
	}

	gap2 := findContentChangedGap(result2.Report.Gaps, "HarnessConstraints")
	if gap2 != nil {
		t.Errorf("second run: GapDeployedRegionContentChanged must not fire when canonical content is unchanged; "+
			"the CanonicalContent function must excise nested user region bytes from the deployed side before "+
			"comparing, so that the presence of nested bytes never makes the comparison unequal on an "+
			"unchanged run: detail=%s", gap2.Detail)
	}
}

// TestDeployedRegionContentChanged_Idempotent_NestedContentPreservedAcrossBothRuns verifies
// that the nested custom-region region's content survives byte-identically across both runs
// of the idempotence test. Content must not be corrupted or lost on either run.
func TestDeployedRegionContentChanged_Idempotent_NestedContentPreservedAcrossBothRuns(t *testing.T) {
	req1 := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)
	result1, err := transform.Apply(req1)
	if err != nil {
		t.Fatalf("Apply (first run): %v", err)
	}

	req2 := reorderReq(t,
		[]byte(contentChangedSource),
		result1.Output,
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)
	result2, err := transform.Apply(req2)
	if err != nil {
		t.Fatalf("Apply (second run): %v", err)
	}

	outDoc1, err := docformat.Parse(result1.Output)
	if err != nil {
		t.Fatalf("parse first run output: %v", err)
	}
	outDoc2, err := docformat.Parse(result2.Output)
	if err != nil {
		t.Fatalf("parse second run output: %v", err)
	}

	custom1, ok1 := outDoc1.Body().Custom("ConstraintsNote")
	custom2, ok2 := outDoc2.Body().Custom("ConstraintsNote")

	if !ok1 {
		t.Fatal("ConstraintsNote absent from first run output; nested content must survive the content-change run")
	}
	if !ok2 {
		t.Fatal("ConstraintsNote absent from second run output; nested content must survive the unchanged run")
	}
	if !bytes.Equal(custom1.Content(), custom2.Content()) {
		t.Errorf("ConstraintsNote content differs between run 1 and run 2:\nrun1: %q\nrun2: %q",
			custom1.Content(), custom2.Content())
	}
}

// ---------------------------------------------------------------------------
// AC5.5 — No-nested-regions no-op invariant must survive Stage 5's changes
// ---------------------------------------------------------------------------

// TestDeployedRegionContentChanged_NoNestedRegions_NoOpInvariantIntact verifies that when a
// deployed region has no nested user-owned regions, Stage 5's changes do not perturb the
// parent's content or introduce spurious whitespace. Two successive transforms on the same
// no-nested inputs must produce byte-identical output.
func TestDeployedRegionContentChanged_NoNestedRegions_NoOpInvariantIntact(t *testing.T) {
	req1 := reorderReq(t,
		[]byte(contentChangedSource),
		[]byte(contentChangedNoNestedDeployed),
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)
	result1, err := transform.Apply(req1)
	if err != nil {
		t.Fatalf("Apply (first run): %v", err)
	}

	req2 := reorderReq(t,
		[]byte(contentChangedSource),
		result1.Output,
		"content-changed-test",
		"2026-08-12T14:25:33Z",
	)
	result2, err := transform.Apply(req2)
	if err != nil {
		t.Fatalf("Apply (second run): %v", err)
	}

	if !bytes.Equal(result1.Output, result2.Output) {
		diff := firstDiff(result1.Output, result2.Output)
		t.Errorf("output differs between run 1 and run 2 for a no-nested-regions agent; "+
			"the no-op invariant on parent content must be intact (no spurious whitespace added); "+
			"first difference at byte %d", diff)
	}
}
