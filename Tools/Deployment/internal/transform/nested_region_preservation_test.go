package transform_test

// nested_region_preservation_test.go covers the bug fix where regenerating a [[DEPLOYED:]]
// region destroys any [[INJECTION:]] or [[CUSTOM:]] region nested inside it.
//
// Root cause: every deployed-region generator calls SetContent or Clear on the parent node,
// which replaces the node's item list, detaching any nested child nodes. The nested nodes
// are then written into a detached (orphaned) subtree, so nothing appears in the output.
//
// The fix requires a mandatory ordering:
//
//  1. Capture nested user-owned regions from the source node (injections) and the deployed
//     file's tree (custom regions), BEFORE any SetContent/Clear call.
//  2. Call the generator to regenerate the parent with canonical content.
//  3. Re-emit the captured regions inside the parent, immediately after the canonical content.
//
// Reversing steps 2 and 3 reintroduces the bug. The ordering is the contract.
//
// Two provenances:
//   - Source-declared: [[INJECTION:]] nodes nested in the source's deployed region; their
//     content is resolved from the deployed region map.
//   - Deployed-only: [[CUSTOM:]] nodes nested in the deployed file's matching deployed region;
//     they have no source declaration and are discovered by walking the deployed tree.
//
// Source file relaxation: the ErrSourceDeployedRegionNotEmpty guard is relaxed so that a
// source deployed region containing only empty nested user-owned markers is not rejected.
// Any other non-whitespace content still errors.

import (
	"bytes"
	"errors"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Shared fixture documents
//
// nestedPreservationSource is a minimal source with a HarnessConstraints deployed region
// that is otherwise empty. The Constraints section is minimal. This source is used for
// tests where no nested injection is declared in the source (custom-only preservation).
// ---------------------------------------------------------------------------

const nestedPreservationSource = `---
id: 600
version: 2.0.0
name: nested-preservation-test
description: Agent for nested region preservation testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested region preservation testing
required_skills: []
---

[[SECTION:Identity]]
# NestedPreservation Agent

You are the NestedPreservation agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// nestedPreservationDeployed is a deployed file whose HarnessConstraints region contains:
//   - canonical harness content (what the tool wrote on the previous transform)
//   - [[CUSTOM:DeployedNestedCustom]] nested inside the region (project-added, no source decl)
//
// The [[CUSTOM:]] region must survive regeneration byte-identically, positioned immediately
// after the canonical content and inside the [[DEPLOYED:HarnessConstraints]] tags.
const nestedPreservationDeployed = `---
id: 600
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested region preservation testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedPreservation Agent

You are the NestedPreservation agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.

[[CUSTOM:DeployedNestedCustom]]
This is a project-specific constraint that was added inside HarnessConstraints.
It must survive regeneration of HarnessConstraints byte-identically.
[[/CUSTOM:DeployedNestedCustom]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// nestedInjectionSource is a source that declares [[INJECTION:NestedInjection]] as an empty
// marker nested inside the [[DEPLOYED:HarnessConstraints]] region. This tests the provenance
// where a nested injection is source-declared (its content comes from the deployed region map).
//
// This source will fail Apply with ErrSourceDeployedRegionNotEmpty under the current
// implementation because the nested marker makes the deployed region non-empty by the old
// len(TrimSpace(content)) > 0 check. After the fix, SourceDeployedRegionIsEmpty allows
// empty nested user-owned markers inside source deployed regions.
const nestedInjectionSource = `---
id: 601
version: 2.0.0
name: nested-injection-test
description: Agent for nested injection preservation testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested injection preservation testing
required_skills: []
---

[[SECTION:Identity]]
# NestedInjection Agent

You are the NestedInjection agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[INJECTION:NestedInjection]]
[[/INJECTION:NestedInjection]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// nestedInjectionDeployed is the deployed predecessor of nestedInjectionSource.
// [[INJECTION:NestedInjection]] carries user content inside HarnessConstraints.
// After regeneration, NestedInjection must survive with its content byte-identical,
// positioned after the canonical HarnessConstraints content inside the parent tags.
const nestedInjectionDeployed = `---
id: 601
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested injection preservation testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedInjection Agent

You are the NestedInjection agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.

[[INJECTION:NestedInjection]]
Project-specific injection content nested inside HarnessConstraints.
This content must survive regeneration byte-identically.
[[/INJECTION:NestedInjection]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// nestedOrderSource is a source that declares [[INJECTION:OrderSecond]] and
// [[INJECTION:OrderThirdSourceOnly]] as empty markers nested inside HarnessConstraints.
//   - OrderSecond is also in the deployed file (takes deployed-file position 1).
//   - OrderThirdSourceOnly is new in the source with no deployed counterpart (position n=2).
const nestedOrderSource = `---
id: 602
version: 2.0.0
name: nested-order-test
description: Agent for nested region ordering testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested region ordering testing
required_skills: []
---

[[SECTION:Identity]]
# NestedOrder Agent

You are the NestedOrder agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[INJECTION:OrderSecond]]
[[/INJECTION:OrderSecond]]
[[INJECTION:OrderThirdSourceOnly]]
[[/INJECTION:OrderThirdSourceOnly]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// nestedOrderDeployed is the deployed predecessor of nestedOrderSource.
// Inside HarnessConstraints:
//   - [[CUSTOM:OrderFirst]] at deployed-file position 0 (deployed-only, no source decl)
//   - [[INJECTION:OrderSecond]] at deployed-file position 1 (also in source)
//
// [[INJECTION:OrderThirdSourceOnly]] is not in this deployed file at all (source-only).
//
// Expected emission order after regeneration:
//   1. OrderFirst  (deployed-file position 0)
//   2. OrderSecond (deployed-file position 1, source-declared with deployed counterpart)
//   3. OrderThirdSourceOnly (source-only, no deployed counterpart — position n)
const nestedOrderDeployed = `---
id: 602
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested region ordering testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedOrder Agent

You are the NestedOrder agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.

[[CUSTOM:OrderFirst]]
First nested content, must appear before OrderSecond.
[[/CUSTOM:OrderFirst]]

[[INJECTION:OrderSecond]]
Second nested content, also source-declared, must appear after OrderFirst.
[[/INJECTION:OrderSecond]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// ---------------------------------------------------------------------------
// T6.1: Custom region nested in a deployed parent survives regeneration
// ---------------------------------------------------------------------------

// TestNestedRegionPreservation_Custom_SurvivesRegenerationByteIdentically asserts that a
// [[CUSTOM:]] region nested inside a [[DEPLOYED:]] region in the deployed file survives
// regeneration of the deployed parent byte-identically.
//
// The regenerated parent has new canonical content (from the harness module). The nested
// custom region must appear inside the parent, immediately after the canonical content.
// Reversing the regenerate-then-resolve ordering — resolving the nested region before
// regenerating the parent — would silently discard the nested content, which is the bug
// being fixed. This test fails immediately if that ordering is reversed.
func TestNestedRegionPreservation_Custom_SurvivesRegenerationByteIdentically(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedPreservationSource),
		Deployed: []byte(nestedPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-preservation-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The nested custom must be present in the output.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outCustom, outOK := outDoc.Body().Custom("DeployedNestedCustom")
	if !outOK {
		t.Fatal("[[CUSTOM:DeployedNestedCustom]] absent from output; a [[CUSTOM:]] region " +
			"nested inside a [[DEPLOYED:]] region must survive regeneration of the parent")
	}

	// Content must be byte-identical to the deployed file.
	depDoc, err := docformat.Parse([]byte(nestedPreservationDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depCustom, depOK := depDoc.Body().Custom("DeployedNestedCustom")
	if !depOK {
		t.Fatal("DeployedNestedCustom not found in deployed fixture; fixture is malformed")
	}
	if !bytes.Equal(outCustom.Content(), depCustom.Content()) {
		t.Errorf("DeployedNestedCustom content not byte-identical to deployed file:\n"+
			"deployed: %q\noutput:   %q", depCustom.Content(), outCustom.Content())
	}

	// Position: the custom region must be inside the HarnessConstraints deployed region.
	parent := outCustom.Parent()
	if parent == nil {
		t.Fatal("DeployedNestedCustom is at body top level in the output; it must remain " +
			"nested inside [[DEPLOYED:HarnessConstraints]]")
	}
	if parent.Kind() != docformat.NodeDeployed {
		t.Errorf("DeployedNestedCustom parent kind: want NodeDeployed, got %q", parent.Kind())
	}
	if parent.Name() != "HarnessConstraints" {
		t.Errorf("DeployedNestedCustom parent name: want %q, got %q", "HarnessConstraints", parent.Name())
	}
}

// TestNestedRegionPreservation_Custom_AppearsAfterCanonicalContent asserts that the
// preserved nested [[CUSTOM:]] region appears in the output immediately after the
// regenerated canonical content of the parent [[DEPLOYED:]] region — not before it.
//
// The canonical content is what the generator (harness, bundle, protocol, etc.) writes.
// The nested region is appended after that content, still inside the parent's tags.
func TestNestedRegionPreservation_Custom_AppearsAfterCanonicalContent(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedPreservationSource),
		Deployed: []byte(nestedPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-preservation-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The canonical harness content appears before the nested custom tag in the output.
	// The harness content is the first line of the expected canonical content.
	canonicalMarker := []byte("Fixture harness constraint")
	customOpenTag := []byte("[[CUSTOM:DeployedNestedCustom]]")

	canonicalIdx := bytes.Index(result.Output, canonicalMarker)
	customIdx := bytes.Index(result.Output, customOpenTag)

	if canonicalIdx < 0 {
		t.Fatal("canonical harness content absent from output; the generator must fill HarnessConstraints")
	}
	if customIdx < 0 {
		t.Fatal("[[CUSTOM:DeployedNestedCustom]] open tag absent from output; the nested custom must survive")
	}
	if customIdx < canonicalIdx {
		t.Errorf("[[CUSTOM:DeployedNestedCustom]] open tag at byte %d appears before canonical content at byte %d; "+
			"nested regions must be emitted after the regenerated canonical content, not before it",
			customIdx, canonicalIdx)
	}

	// The nested custom must also appear before the closing [[/DEPLOYED:HarnessConstraints]] tag,
	// confirming it is inside the parent's tags, not after them.
	parentCloseTag := []byte("[[/DEPLOYED:HarnessConstraints]]")
	parentCloseIdx := bytes.Index(result.Output, parentCloseTag)
	if parentCloseIdx < 0 {
		t.Fatal("[[/DEPLOYED:HarnessConstraints]] closing tag absent from output")
	}
	if customIdx > parentCloseIdx {
		t.Errorf("[[CUSTOM:DeployedNestedCustom]] open tag at byte %d appears after the parent's "+
			"closing tag at byte %d; the nested region must be inside the parent's tags",
			customIdx, parentCloseIdx)
	}
}

// TestNestedRegionPreservation_Custom_ReportOutcome asserts that a preserved nested
// [[CUSTOM:]] region produces a RegionOutcome with Action = RegionCustomPreserved and
// Marker = NodeCustom, not the parent's NodeDeployed kind.
func TestNestedRegionPreservation_Custom_ReportOutcome(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedPreservationSource),
		Deployed: []byte(nestedPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-preservation-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "DeployedNestedCustom" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatal("RegionOutcome for DeployedNestedCustom absent from report; " +
			"a re-emitted nested region must be reported with its true marker kind")
	}
	if outcome.Marker != docformat.NodeCustom {
		t.Errorf("DeployedNestedCustom outcome Marker: want NodeCustom (%q), got %q",
			docformat.NodeCustom, outcome.Marker)
	}
	if outcome.Action != transform.RegionCustomPreserved {
		t.Errorf("DeployedNestedCustom outcome Action: want %q, got %q",
			transform.RegionCustomPreserved, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T6.2: Injection nested in a deployed parent survives regeneration
// ---------------------------------------------------------------------------

// TestNestedRegionPreservation_Injection_SurvivesRegenerationByteIdentically asserts that a
// [[INJECTION:]] region declared empty in the source and nested inside a [[DEPLOYED:]] region
// survives regeneration of that parent, with its content lifted byte-identically from the
// deployed file.
//
// This test also exercises the source-guard relaxation: the source declares an empty nested
// [[INJECTION:]] inside a [[DEPLOYED:]] region. Before the fix, this produces
// ErrSourceDeployedRegionNotEmpty (the nested marker is non-whitespace). After the fix,
// SourceDeployedRegionIsEmpty permits empty nested user-owned markers.
func TestNestedRegionPreservation_Injection_SurvivesRegenerationByteIdentically(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedInjectionSource),
		Deployed: []byte(nestedInjectionDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-injection-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply returned an error; expected success after source-guard relaxation: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// NestedInjection must be present in the output.
	outInj, outOK := outDoc.Body().Injection("NestedInjection")
	if !outOK {
		t.Fatal("[[INJECTION:NestedInjection]] absent from output; a source-declared injection " +
			"nested inside a [[DEPLOYED:]] region must survive regeneration")
	}

	// Content must be byte-identical to the deployed file.
	depDoc, err := docformat.Parse([]byte(nestedInjectionDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depInj, depOK := depDoc.Body().Injection("NestedInjection")
	if !depOK {
		t.Fatal("NestedInjection not found in deployed fixture; fixture is malformed")
	}
	if !bytes.Equal(outInj.Content(), depInj.Content()) {
		t.Errorf("NestedInjection content not byte-identical to deployed file:\n"+
			"deployed: %q\noutput:   %q", depInj.Content(), outInj.Content())
	}

	// Position: NestedInjection must be nested inside HarnessConstraints.
	parent := outInj.Parent()
	if parent == nil {
		t.Fatal("NestedInjection is at body top level; it must remain nested inside " +
			"[[DEPLOYED:HarnessConstraints]]")
	}
	if parent.Kind() != docformat.NodeDeployed {
		t.Errorf("NestedInjection parent kind: want NodeDeployed, got %q", parent.Kind())
	}
	if parent.Name() != "HarnessConstraints" {
		t.Errorf("NestedInjection parent name: want %q, got %q", "HarnessConstraints", parent.Name())
	}
}

// TestNestedRegionPreservation_Injection_AppearsAfterCanonicalContent asserts that the
// preserved nested [[INJECTION:]] region appears after the parent's canonical content
// in the output, still inside the parent's tags.
func TestNestedRegionPreservation_Injection_AppearsAfterCanonicalContent(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedInjectionSource),
		Deployed: []byte(nestedInjectionDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-injection-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	canonicalMarker := []byte("Fixture harness constraint")
	injOpenTag := []byte("[[INJECTION:NestedInjection]]")
	parentCloseTag := []byte("[[/DEPLOYED:HarnessConstraints]]")

	canonicalIdx := bytes.Index(result.Output, canonicalMarker)
	injIdx := bytes.Index(result.Output, injOpenTag)
	parentCloseIdx := bytes.Index(result.Output, parentCloseTag)

	if canonicalIdx < 0 {
		t.Fatal("canonical harness content absent from output")
	}
	if injIdx < 0 {
		t.Fatal("[[INJECTION:NestedInjection]] open tag absent from output")
	}
	if parentCloseIdx < 0 {
		t.Fatal("[[/DEPLOYED:HarnessConstraints]] closing tag absent from output")
	}
	if injIdx < canonicalIdx {
		t.Errorf("[[INJECTION:NestedInjection]] at byte %d appears before canonical content at byte %d; "+
			"nested regions must appear after the regenerated canonical content",
			injIdx, canonicalIdx)
	}
	if injIdx > parentCloseIdx {
		t.Errorf("[[INJECTION:NestedInjection]] at byte %d appears after the parent's closing "+
			"tag at byte %d; the nested region must be inside the parent's tags",
			injIdx, parentCloseIdx)
	}
}

// TestNestedRegionPreservation_Injection_ReportOutcome asserts that a re-emitted nested
// [[INJECTION:]] region produces a RegionOutcome with Marker == NodeInjection and Action ==
// RegionPreserved, mirroring TestNestedRegionPreservation_Custom_ReportOutcome for the
// injection provenance.
//
// ContractsDesign.md's reemitNestedUserRegions contract is explicit that the returned
// RegionOutcome carries the region's true marker kind (NodeInjection or NodeCustom), never
// the parent's NodeDeployed. This test pins the Action choice for the injection path so that
// divergent implementations cannot silently disagree on what to report.
func TestNestedRegionPreservation_Injection_ReportOutcome(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedInjectionSource),
		Deployed: []byte(nestedInjectionDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-injection-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "NestedInjection" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatal("RegionOutcome for NestedInjection absent from report; " +
			"a re-emitted nested injection must be reported with its true marker kind")
	}
	if outcome.Marker != docformat.NodeInjection {
		t.Errorf("NestedInjection outcome Marker: want NodeInjection (%q), got %q; "+
			"reemitNestedUserRegions must carry the region's true marker kind, never the parent's NodeDeployed",
			docformat.NodeInjection, outcome.Marker)
	}
	// The injection's content is preserved from the deployed region map, so the correct
	// action is RegionPreserved ("preserved-from-deployed"). This matches the semantics of
	// a normal project injection whose content is lifted from the deployed file.
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("NestedInjection outcome Action: want %q (RegionPreserved), got %q; "+
			"a re-emitted nested injection whose content comes from the deployed region map "+
			"is preserved, not added or filled",
			transform.RegionPreserved, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T6.3: Multiple nested regions in one parent — survival and deterministic order
// ---------------------------------------------------------------------------

// TestNestedRegionPreservation_MultipleNested_AllSurviveRegenerationByteIdentically asserts
// that when a [[DEPLOYED:]] parent holds several nested user-owned regions, all of them
// survive regeneration byte-identically.
//
// Three regions are tested:
//   - [[CUSTOM:OrderFirst]]: deployed-only, at deployed-file position 0.
//   - [[INJECTION:OrderSecond]]: source-declared and in deployed file, at position 1.
//   - [[INJECTION:OrderThirdSourceOnly]]: source-declared only (no deployed counterpart);
//     appears at position n (after all deployed-file-origin regions).
func TestNestedRegionPreservation_MultipleNested_AllSurviveRegenerationByteIdentically(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedOrderSource),
		Deployed: []byte(nestedOrderDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-order-test",
		Module:   newInjectionFixtureModule(t),
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

	// All three nested regions must survive.
	if _, ok := outDoc.Body().Custom("OrderFirst"); !ok {
		t.Error("[[CUSTOM:OrderFirst]] absent from output; deployed-only nested custom must survive")
	}
	if _, ok := outDoc.Body().Injection("OrderSecond"); !ok {
		t.Error("[[INJECTION:OrderSecond]] absent from output; source-declared nested injection must survive")
	}
	outThird, outThirdOK := outDoc.Body().Injection("OrderThirdSourceOnly")
	if !outThirdOK {
		t.Error("[[INJECTION:OrderThirdSourceOnly]] absent from output; source-only nested injection must survive")
	} else if len(bytes.TrimSpace(outThird.Content())) != 0 {
		// OrderThirdSourceOnly is declared in the source but has no entry in the deployed region
		// map. Per the reemitNestedUserRegions contract, "a name absent from the map produces an
		// empty region". This pins that the implementation does not accidentally leak adjacent
		// content into the region.
		t.Errorf("OrderThirdSourceOnly must be empty (not in deployed file → empty region per the "+
			"\"absent from map → empty region\" contract); got: %q", outThird.Content())
	}

	// OrderFirst content must be byte-identical to the deployed file.
	depDoc, err := docformat.Parse([]byte(nestedOrderDeployed))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	depFirst, firstOK := depDoc.Body().Custom("OrderFirst")
	outFirst, _ := outDoc.Body().Custom("OrderFirst")
	if firstOK && outFirst != nil {
		if !bytes.Equal(outFirst.Content(), depFirst.Content()) {
			t.Errorf("OrderFirst content not byte-identical to deployed:\ndeployed: %q\noutput: %q",
				depFirst.Content(), outFirst.Content())
		}
	}

	// OrderSecond content must be byte-identical to the deployed file.
	depSecond, secondOK := depDoc.Body().Injection("OrderSecond")
	outSecond, _ := outDoc.Body().Injection("OrderSecond")
	if secondOK && outSecond != nil {
		if !bytes.Equal(outSecond.Content(), depSecond.Content()) {
			t.Errorf("OrderSecond content not byte-identical to deployed:\ndeployed: %q\noutput: %q",
				depSecond.Content(), outSecond.Content())
		}
	}
}

// TestNestedRegionPreservation_MultipleNested_DeterministicOrder pins the emission order
// of nested regions when a parent holds several. The order follows the contract:
//
//  1. Deployed-file-origin regions first, in their deployed-file order within the parent.
//  2. Source-declared regions with no deployed-file counterpart next.
//
// A source-declared injection that also has a deployed-file counterpart takes the
// deployed-file position (it is one region, not two).
//
// The fixture has:
//   - OrderFirst (deployed position 0, deployed-only)
//   - OrderSecond (deployed position 1, also in source)
//   - OrderThirdSourceOnly (source-only, position n)
//
// Expected byte order: OrderFirst < OrderSecond < OrderThirdSourceOnly.
func TestNestedRegionPreservation_MultipleNested_DeterministicOrder(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedOrderSource),
		Deployed: []byte(nestedOrderDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-order-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	firstTag := []byte("[[CUSTOM:OrderFirst]]")
	secondTag := []byte("[[INJECTION:OrderSecond]]")
	thirdTag := []byte("[[INJECTION:OrderThirdSourceOnly]]")

	firstIdx := bytes.Index(result.Output, firstTag)
	secondIdx := bytes.Index(result.Output, secondTag)
	thirdIdx := bytes.Index(result.Output, thirdTag)

	if firstIdx < 0 {
		t.Fatal("[[CUSTOM:OrderFirst]] open tag absent from output")
	}
	if secondIdx < 0 {
		t.Fatal("[[INJECTION:OrderSecond]] open tag absent from output")
	}
	if thirdIdx < 0 {
		t.Fatal("[[INJECTION:OrderThirdSourceOnly]] open tag absent from output")
	}

	if firstIdx >= secondIdx {
		t.Errorf("[[CUSTOM:OrderFirst]] at byte %d must appear before [[INJECTION:OrderSecond]] at byte %d; "+
			"deployed-file-origin regions must be emitted in deployed-file order",
			firstIdx, secondIdx)
	}
	if secondIdx >= thirdIdx {
		t.Errorf("[[INJECTION:OrderSecond]] at byte %d must appear before [[INJECTION:OrderThirdSourceOnly]] at byte %d; "+
			"deployed-file-origin regions must appear before source-only regions",
			secondIdx, thirdIdx)
	}
}

// ---------------------------------------------------------------------------
// T6.4: Nested region preservation across every deployed-region generator path
//
// Each generator (harness, bundle, protocol, assembled) calls SetContent or Clear on the
// parent node. All paths must apply the capture-regenerate-reemit sequence. A fix applied
// to only one generator will leave the others broken; these tests make that visible.
// ---------------------------------------------------------------------------

// nestedHarnessSource is used for the harness generator path (HarnessConstraints). The source
// has no nested injection — only a deployed custom region in the deployed file.
// (Same as nestedPreservationSource — reused via const reference above.)

// nestedBundleSource is a subagent source with [[DEPLOYED:AuthorityHierarchy]] in the
// Identity section. AuthorityHierarchy is a bundle-filled deployed region; its content
// is regenerated from req.Bundle on every transform. The nested custom must survive
// that regeneration.
const nestedBundleSource = `---
id: 603
version: 2.0.0
name: nested-bundle-test
description: Agent for nested region bundle-path testing
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested region bundle-path testing
required_skills: []
---

[[SECTION:Identity]]
# NestedBundle Agent

You are the NestedBundle agent.

[[DEPLOYED:AuthorityHierarchy]]
[[/DEPLOYED:AuthorityHierarchy]]

[[/SECTION:Identity]]
`

// nestedBundleDeployed is the deployed predecessor of nestedBundleSource. AuthorityHierarchy
// holds previously generated bundle content plus a nested [[CUSTOM:BundlePathCustom]] region.
const nestedBundleDeployed = `---
id: 603
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested region bundle-path testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedBundle Agent

You are the NestedBundle agent.

[[DEPLOYED:AuthorityHierarchy]]
### AuthorityHierarchy

This is the verbatim content for AuthorityHierarchy.

[[CUSTOM:BundlePathCustom]]
Project-specific content nested inside AuthorityHierarchy.
Must survive the bundle regeneration of that parent region.
[[/CUSTOM:BundlePathCustom]]
[[/DEPLOYED:AuthorityHierarchy]]

[[/SECTION:Identity]]
`

// nestedProtocolSource is a source with [[DEPLOYED:CommunicationProtocol]] at the top level.
// CommunicationProtocol is protocol-filled; a nested [[CUSTOM:]] must survive its regeneration.
const nestedProtocolSource = `---
id: 604
version: 2.0.0
name: nested-protocol-test
description: Agent for nested region protocol-path testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested region protocol-path testing
required_skills: []
---

[[SECTION:Identity]]
# NestedProtocol Agent

You are the NestedProtocol agent.
[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]
`

// nestedProtocolDeployed is the deployed predecessor of nestedProtocolSource.
// CommunicationProtocol holds previously generated protocol content plus a nested
// [[CUSTOM:ProtocolPathCustom]] region.
const nestedProtocolDeployed = `---
id: 604
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested region protocol-path testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedProtocol Agent

You are the NestedProtocol agent.
[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
<!-- protocol-version: 1.9 -->
## Communication Protocol (Subagent)

Subagent-specific protocol content.

[[CUSTOM:ProtocolPathCustom]]
Project-specific extension nested inside CommunicationProtocol.
Must survive the protocol regeneration of that parent region.
[[/CUSTOM:ProtocolPathCustom]]
[[/DEPLOYED:CommunicationProtocol]]
`

// nestedWorkflowSource is an orchestrator-like source with [[DEPLOYED:AvailableWorkflows]]
// in the Identity section. AvailableWorkflows is assembled from req.Workflows on each run;
// when req.Workflows is nil the region is cleared. A nested [[CUSTOM:]] must survive either
// path (assembled or cleared).
const nestedWorkflowSource = `---
version: 2.0.0
name: nested-workflow-test
description: Agent for nested region workflow-path testing
model: {model-identifier}
tools: [file_read]
recommended_tier: HIGH
tier_rationale: nested region workflow-path testing
required_skills: []
---

[[SECTION:Identity]]
# NestedWorkflow Agent

You are the NestedWorkflow agent.

[[DEPLOYED:AvailableWorkflows]]
[[/DEPLOYED:AvailableWorkflows]]

[[/SECTION:Identity]]
`

// nestedWorkflowDeployed is the deployed predecessor of nestedWorkflowSource.
// AvailableWorkflows holds previously assembled workflow content plus a nested
// [[CUSTOM:WorkflowPathCustom]] region.
const nestedWorkflowDeployed = `---
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested region workflow-path testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedWorkflow Agent

You are the NestedWorkflow agent.

[[DEPLOYED:AvailableWorkflows]]
[[SECTION:Workflow:sample]]
<!-- workflow-version: 1.0 -->
## Sample Workflow

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLAN | planner | ✅ |

[[/SECTION:Workflow:sample]]

[[CUSTOM:WorkflowPathCustom]]
Project-specific content nested inside AvailableWorkflows.
Must survive even when AvailableWorkflows is cleared (empty workflows).
[[/CUSTOM:WorkflowPathCustom]]
[[/DEPLOYED:AvailableWorkflows]]

[[/SECTION:Identity]]
`

// TestNestedRegionPreservation_GeneratorPath_Harness asserts that a [[CUSTOM:]] region
// nested inside [[DEPLOYED:HarnessConstraints]] survives regeneration via the harness
// generator path (applyHarnessRegion). This is the same assertion as T6.1 but exists
// explicitly to pin that the harness generator path is covered.
func TestNestedRegionPreservation_GeneratorPath_Harness(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedPreservationSource),
		Deployed: []byte(nestedPreservationDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-preservation-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (harness path): %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("DeployedNestedCustom"); !ok {
		t.Error("[[CUSTOM:DeployedNestedCustom]] absent after harness-path regeneration of " +
			"[[DEPLOYED:HarnessConstraints]]; the harness generator must apply the " +
			"capture-regenerate-reemit sequence")
	}
}

// TestNestedRegionPreservation_GeneratorPath_Bundle asserts that a [[CUSTOM:]] region nested
// inside [[DEPLOYED:AuthorityHierarchy]] survives regeneration via the bundle generator path
// (applyBundleRegion). The bundle regenerates the parent's content from req.Bundle; the
// nested custom must appear after that content, still inside the parent.
func TestNestedRegionPreservation_GeneratorPath_Bundle(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedBundleSource),
		Deployed: []byte(nestedBundleDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Bundle:   fixtureBundle("1.0.0"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (bundle path): %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("BundlePathCustom"); !ok {
		t.Error("[[CUSTOM:BundlePathCustom]] absent after bundle-path regeneration of " +
			"[[DEPLOYED:AuthorityHierarchy]]; the bundle generator must apply the " +
			"capture-regenerate-reemit sequence")
	}
}

// TestNestedRegionPreservation_GeneratorPath_Protocol asserts that a [[CUSTOM:]] region
// nested inside [[DEPLOYED:CommunicationProtocol]] survives regeneration via the protocol
// generator path (applyProtocolRegion).
func TestNestedRegionPreservation_GeneratorPath_Protocol(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedProtocolSource),
		Deployed: []byte(nestedProtocolDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.10"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (protocol path): %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("ProtocolPathCustom"); !ok {
		t.Error("[[CUSTOM:ProtocolPathCustom]] absent after protocol-path regeneration of " +
			"[[DEPLOYED:CommunicationProtocol]]; the protocol generator must apply the " +
			"capture-regenerate-reemit sequence")
	}
}

// TestNestedRegionPreservation_GeneratorPath_Workflow asserts that a [[CUSTOM:]] region
// nested inside [[DEPLOYED:AvailableWorkflows]] survives even when the workflow region is
// cleared (req.Workflows is nil, so applyWorkflowRegion calls Clear). Clearing the parent
// via Clear() also destroys nested child nodes — the fix must capture them first.
//
// Note: this test exercises the Clear() path for custom regions, which Stage 5's post-loop
// placeCustomRegion already handles. See TestNestedRegionPreservation_GeneratorPath_Workflow_NestedInjection
// for the Stage 6-specific injection path that requires capture-regenerate-reemit.
func TestNestedRegionPreservation_GeneratorPath_Workflow(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedWorkflowSource),
		Deployed: []byte(nestedWorkflowDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-workflow-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		// Workflows: nil — applyWorkflowRegion calls node.Clear(), which destroys nested nodes.
		// The fix must capture the nested custom BEFORE Clear is called.
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (workflow/clear path): %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("WorkflowPathCustom"); !ok {
		t.Error("[[CUSTOM:WorkflowPathCustom]] absent after workflow-path Clear of " +
			"[[DEPLOYED:AvailableWorkflows]]; the workflow generator must apply the " +
			"capture-regenerate-reemit sequence even when Clear is called (not just SetContent)")
	}
}

// ---------------------------------------------------------------------------
// T6.4 (continued): Injection regions nested in each generator path
//
// The custom-region variants above verify Stage 5's post-loop placeCustomRegion handles
// nested customs across all generators. These injection variants verify that Stage 6's
// capture-regenerate-reemit sequence is applied to all generators for injection regions,
// which requires both the source guard relaxation (T6.5) and the new sequence.
//
// All four tests below will fail until both fixes are in place.
// ---------------------------------------------------------------------------

// nestedBundleInjSource is a source with [[DEPLOYED:AuthorityHierarchy]] containing an
// empty [[INJECTION:BundleNestedInj]] marker. This source is currently rejected by
// ErrSourceDeployedRegionNotEmpty. After the source-guard relaxation and the
// capture-regenerate-reemit fix, the injection must survive the bundle generator's
// SetContent call.
const nestedBundleInjSource = `---
id: 610
version: 2.0.0
name: nested-bundle-inj-test
description: Agent for nested injection bundle-path testing
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested injection bundle-path testing
required_skills: []
---

[[SECTION:Identity]]
# NestedBundleInj Agent

You are the NestedBundleInj agent.

[[DEPLOYED:AuthorityHierarchy]]
[[INJECTION:BundleNestedInj]]
[[/INJECTION:BundleNestedInj]]
[[/DEPLOYED:AuthorityHierarchy]]

[[/SECTION:Identity]]
`

// nestedBundleInjDeployed is the deployed predecessor of nestedBundleInjSource.
// [[INJECTION:BundleNestedInj]] is populated with user content inside AuthorityHierarchy.
const nestedBundleInjDeployed = `---
id: 610
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested injection bundle-path testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedBundleInj Agent

You are the NestedBundleInj agent.

[[DEPLOYED:AuthorityHierarchy]]
### AuthorityHierarchy

This is the verbatim content for AuthorityHierarchy.

[[INJECTION:BundleNestedInj]]
User content nested inside AuthorityHierarchy via injection.
Must survive after the bundle generator regenerates the parent.
[[/INJECTION:BundleNestedInj]]
[[/DEPLOYED:AuthorityHierarchy]]

[[/SECTION:Identity]]
`

// nestedProtocolInjSource is a source with [[DEPLOYED:CommunicationProtocol]] containing
// an empty [[INJECTION:ProtocolNestedInj]] marker. Currently rejected by source guard.
const nestedProtocolInjSource = `---
id: 611
version: 2.0.0
name: nested-protocol-inj-test
description: Agent for nested injection protocol-path testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: nested injection protocol-path testing
required_skills: []
---

[[SECTION:Identity]]
# NestedProtocolInj Agent

You are the NestedProtocolInj agent.
[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[INJECTION:ProtocolNestedInj]]
[[/INJECTION:ProtocolNestedInj]]
[[/DEPLOYED:CommunicationProtocol]]
`

// nestedProtocolInjDeployed is the deployed predecessor of nestedProtocolInjSource.
// [[INJECTION:ProtocolNestedInj]] is populated inside CommunicationProtocol.
const nestedProtocolInjDeployed = `---
id: 611
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested injection protocol-path testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedProtocolInj Agent

You are the NestedProtocolInj agent.
[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
<!-- protocol-version: 1.9 -->
## Communication Protocol (Subagent)

Subagent-specific protocol content.

[[INJECTION:ProtocolNestedInj]]
User content nested inside CommunicationProtocol via injection.
Must survive after the protocol generator regenerates the parent.
[[/INJECTION:ProtocolNestedInj]]
[[/DEPLOYED:CommunicationProtocol]]
`

// nestedWorkflowInjSource is a source with [[DEPLOYED:AvailableWorkflows]] containing
// an empty [[INJECTION:WorkflowNestedInj]] marker. Currently rejected by source guard.
const nestedWorkflowInjSource = `---
version: 2.0.0
name: nested-workflow-inj-test
description: Agent for nested injection workflow-path testing
model: {model-identifier}
tools: [file_read]
recommended_tier: HIGH
tier_rationale: nested injection workflow-path testing
required_skills: []
---

[[SECTION:Identity]]
# NestedWorkflowInj Agent

You are the NestedWorkflowInj agent.

[[DEPLOYED:AvailableWorkflows]]
[[INJECTION:WorkflowNestedInj]]
[[/INJECTION:WorkflowNestedInj]]
[[/DEPLOYED:AvailableWorkflows]]

[[/SECTION:Identity]]
`

// nestedWorkflowInjDeployed is the deployed predecessor of nestedWorkflowInjSource.
// [[INJECTION:WorkflowNestedInj]] is populated inside AvailableWorkflows.
const nestedWorkflowInjDeployed = `---
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested injection workflow-path testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedWorkflowInj Agent

You are the NestedWorkflowInj agent.

[[DEPLOYED:AvailableWorkflows]]
[[INJECTION:WorkflowNestedInj]]
User content nested inside AvailableWorkflows via injection.
Must survive even when AvailableWorkflows is cleared (empty Workflows).
[[/INJECTION:WorkflowNestedInj]]
[[/DEPLOYED:AvailableWorkflows]]

[[/SECTION:Identity]]
`

// TestNestedRegionPreservation_GeneratorPath_Bundle_NestedInjection asserts that a
// [[INJECTION:]] region declared empty in source, nested inside [[DEPLOYED:AuthorityHierarchy]],
// survives regeneration via the bundle generator. This requires both the source guard
// relaxation (so the empty nested marker is accepted) and the capture-regenerate-reemit
// sequence (so the injection survives SetContent on the parent).
func TestNestedRegionPreservation_GeneratorPath_Bundle_NestedInjection(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedBundleInjSource),
		Deployed: []byte(nestedBundleInjDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-bundle-inj-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Bundle:   fixtureBundle("1.0.0"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (bundle path, injection): %v\n"+
			"If ErrSourceDeployedRegionNotEmpty: the source guard must be relaxed to allow empty nested markers.\n"+
			"If another error: the bundle generator must apply capture-regenerate-reemit.", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outInj, ok := outDoc.Body().Injection("BundleNestedInj")
	if !ok {
		t.Error("[[INJECTION:BundleNestedInj]] absent from output after bundle-path regeneration of " +
			"[[DEPLOYED:AuthorityHierarchy]]; the bundle generator must apply " +
			"capture-regenerate-reemit for nested injection regions, not only for nested custom regions")
	}

	if ok && outInj.Parent() != nil {
		if outInj.Parent().Kind() != docformat.NodeDeployed || outInj.Parent().Name() != "AuthorityHierarchy" {
			t.Errorf("BundleNestedInj must be nested inside [[DEPLOYED:AuthorityHierarchy]]; " +
				"got parent kind=%q name=%q", outInj.Parent().Kind(), outInj.Parent().Name())
		}
	}
}

// TestNestedRegionPreservation_GeneratorPath_Protocol_NestedInjection asserts that a
// [[INJECTION:]] region declared empty in source, nested inside [[DEPLOYED:CommunicationProtocol]],
// survives regeneration via the protocol generator.
func TestNestedRegionPreservation_GeneratorPath_Protocol_NestedInjection(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedProtocolInjSource),
		Deployed: []byte(nestedProtocolInjDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-protocol-inj-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.10"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (protocol path, injection): %v\n"+
			"If ErrSourceDeployedRegionNotEmpty: the source guard must be relaxed to allow empty nested markers.\n"+
			"If another error: the protocol generator must apply capture-regenerate-reemit.", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outInj, ok := outDoc.Body().Injection("ProtocolNestedInj")
	if !ok {
		t.Error("[[INJECTION:ProtocolNestedInj]] absent from output after protocol-path regeneration of " +
			"[[DEPLOYED:CommunicationProtocol]]; the protocol generator must apply " +
			"capture-regenerate-reemit for nested injection regions")
	}

	if ok && outInj.Parent() != nil {
		if outInj.Parent().Kind() != docformat.NodeDeployed || outInj.Parent().Name() != "CommunicationProtocol" {
			t.Errorf("ProtocolNestedInj must be nested inside [[DEPLOYED:CommunicationProtocol]]; " +
				"got parent kind=%q name=%q", outInj.Parent().Kind(), outInj.Parent().Name())
		}
	}
}

// TestNestedRegionPreservation_GeneratorPath_Workflow_NestedInjection asserts that a
// [[INJECTION:]] region declared empty in source, nested inside [[DEPLOYED:AvailableWorkflows]],
// survives even when AvailableWorkflows is cleared (req.Workflows is nil). The Clear path
// and the SetContent path both destroy nested nodes; the fix must handle both.
func TestNestedRegionPreservation_GeneratorPath_Workflow_NestedInjection(t *testing.T) {
	req := transform.Request{
		Source:   []byte(nestedWorkflowInjSource),
		Deployed: []byte(nestedWorkflowInjDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-workflow-inj-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		// Workflows: nil — applyWorkflowRegion calls Clear().
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (workflow path, injection): %v\n"+
			"If ErrSourceDeployedRegionNotEmpty: the source guard must be relaxed to allow empty nested markers.\n"+
			"If another error: the workflow generator must apply capture-regenerate-reemit.", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outInj, ok := outDoc.Body().Injection("WorkflowNestedInj")
	if !ok {
		t.Error("[[INJECTION:WorkflowNestedInj]] absent from output after workflow-path Clear of " +
			"[[DEPLOYED:AvailableWorkflows]]; the workflow generator must apply " +
			"capture-regenerate-reemit for nested injection regions, including the Clear path")
	}

	if ok && outInj.Parent() != nil {
		if outInj.Parent().Kind() != docformat.NodeDeployed || outInj.Parent().Name() != "AvailableWorkflows" {
			t.Errorf("WorkflowNestedInj must be nested inside [[DEPLOYED:AvailableWorkflows]]; " +
				"got parent kind=%q name=%q", outInj.Parent().Kind(), outInj.Parent().Name())
		}
	}
}

// nestedInfraSource is an orchestrator-like source with [[DEPLOYED:InfrastructureAgents]]
// inside the Identity section. InfrastructureAgents is regenerated by applyInfrastructureRegion
// from req.InfrastructureAgents on each transform. A nested [[CUSTOM:]] region must survive
// that regeneration.
const nestedInfraSource = `---
version: 7.0.0
name: nested-infra-test
description: Agent for nested region infrastructure-path testing
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: nested region infrastructure-path testing
required_skills: []
---

[[SECTION:Identity]]
# NestedInfra Agent

You are the NestedInfra agent.

[[DEPLOYED:InfrastructureAgents]]
[[/DEPLOYED:InfrastructureAgents]]

[[/SECTION:Identity]]
`

// nestedInfraDeployed is the deployed predecessor of nestedInfraSource. InfrastructureAgents
// holds previously assembled infrastructure content plus a nested [[CUSTOM:InfraPathCustom]]
// region. The nested custom must survive the infrastructure generator's SetContent call.
const nestedInfraDeployed = `---
version: 7.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested region infrastructure-path testing
mode: subagent
model: claude/claude-sonnet
tools: {tool-permissions}
---

[[SECTION:Identity]]
# NestedInfra Agent

You are the NestedInfra agent.

[[DEPLOYED:InfrastructureAgents]]
[[SECTION:InfrastructureAgent:orchestration-review]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 30 | continue | Advisory checks on run bookkeeping. |
[[/SECTION:InfrastructureAgent:orchestration-review]]

[[CUSTOM:InfraPathCustom]]
Project-specific content nested inside InfrastructureAgents.
Must survive the infrastructure generator's SetContent call.
[[/CUSTOM:InfraPathCustom]]
[[/DEPLOYED:InfrastructureAgents]]

[[/SECTION:Identity]]
`

// nestedInfraInjSource is a source with [[DEPLOYED:InfrastructureAgents]] containing an
// empty [[INJECTION:InfraNestedInj]] marker. Currently rejected by the source guard.
// After the relaxation and the capture-regenerate-reemit fix, the injection must survive
// the infrastructure generator's SetContent call.
const nestedInfraInjSource = `---
version: 7.0.0
name: nested-infra-inj-test
description: Agent for nested injection infrastructure-path testing
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: nested injection infrastructure-path testing
required_skills: []
---

[[SECTION:Identity]]
# NestedInfraInj Agent

You are the NestedInfraInj agent.

[[DEPLOYED:InfrastructureAgents]]
[[INJECTION:InfraNestedInj]]
[[/INJECTION:InfraNestedInj]]
[[/DEPLOYED:InfrastructureAgents]]

[[/SECTION:Identity]]
`

// nestedInfraInjDeployed is the deployed predecessor of nestedInfraInjSource.
// [[INJECTION:InfraNestedInj]] is populated with user content inside InfrastructureAgents.
const nestedInfraInjDeployed = `---
version: 7.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested injection infrastructure-path testing
mode: subagent
model: claude/claude-sonnet
tools: {tool-permissions}
---

[[SECTION:Identity]]
# NestedInfraInj Agent

You are the NestedInfraInj agent.

[[DEPLOYED:InfrastructureAgents]]
[[SECTION:InfrastructureAgent:orchestration-review]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 30 | continue | Advisory checks on run bookkeeping. |
[[/SECTION:InfrastructureAgent:orchestration-review]]

[[INJECTION:InfraNestedInj]]
User content nested inside InfrastructureAgents via injection.
Must survive after the infrastructure generator regenerates the parent.
[[/INJECTION:InfraNestedInj]]
[[/DEPLOYED:InfrastructureAgents]]

[[/SECTION:Identity]]
`

// fixtureInfraAgents returns a minimal InfrastructureBlock slice for use in the
// infrastructure-path nested region tests. Supplying a non-empty slice drives
// applyInfrastructureRegion to call SetContent, exercising the generator path that
// destroys nested children in the unfixed implementation.
func fixtureInfraAgents() []transform.InfrastructureBlock {
	return []transform.InfrastructureBlock{
		{
			Key:         "orchestration-review",
			Version:     "1.0.0",
			Class:       "review",
			Description: "Advisory checks on run bookkeeping.",
			OnFailure:   "continue",
			Triggers: []domain.InfrastructureTrigger{
				{Trigger: "INVOCATION_INTERVAL", TriggerParam: "30"},
			},
		},
	}
}

// TestNestedRegionPreservation_GeneratorPath_Infrastructure asserts that a [[CUSTOM:]] region
// nested inside [[DEPLOYED:InfrastructureAgents]] survives regeneration via the infrastructure
// generator path (applyInfrastructureRegion). This path calls SetContent with the assembled
// infrastructure blocks, which replaces the node's item list and detaches any nested child
// nodes. The fix must apply the capture-regenerate-reemit sequence here as well.
//
// AC6.4 ("Preservation holds across every deployed-region generator path, not just one")
// requires this path to be explicitly covered. An implementation that fixes the other four
// generators but forgets applyInfrastructureRegion will pass all other T6.4 tests and still
// ship the bug on this path.
func TestNestedRegionPreservation_GeneratorPath_Infrastructure(t *testing.T) {
	req := transform.Request{
		Source:               []byte(nestedInfraSource),
		Deployed:             []byte(nestedInfraDeployed),
		Kind:                 domain.ArtifactAgent,
		Key:                  "nested-infra-test",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		InfrastructureAgents: fixtureInfraAgents(),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (infrastructure path): %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Custom("InfraPathCustom"); !ok {
		t.Error("[[CUSTOM:InfraPathCustom]] absent after infrastructure-path regeneration of " +
			"[[DEPLOYED:InfrastructureAgents]]; the infrastructure generator (applyInfrastructureRegion) " +
			"must apply the capture-regenerate-reemit sequence, matching the other four generator paths")
	}
}

// TestNestedRegionPreservation_GeneratorPath_Infrastructure_NestedInjection asserts that a
// [[INJECTION:]] region declared empty in source, nested inside [[DEPLOYED:InfrastructureAgents]],
// survives regeneration via the infrastructure generator. This requires both the source guard
// relaxation (so the empty nested marker is accepted) and the capture-regenerate-reemit
// sequence (so the injection survives SetContent on the parent).
func TestNestedRegionPreservation_GeneratorPath_Infrastructure_NestedInjection(t *testing.T) {
	req := transform.Request{
		Source:               []byte(nestedInfraInjSource),
		Deployed:             []byte(nestedInfraInjDeployed),
		Kind:                 domain.ArtifactAgent,
		Key:                  "nested-infra-inj-test",
		Module:               newFixtureModule(t),
		Model:                fixtureModel(),
		Scope:                domain.ScopeProject,
		InfrastructureAgents: fixtureInfraAgents(),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply (infrastructure path, injection): %v\n"+
			"If ErrSourceDeployedRegionNotEmpty: the source guard must be relaxed to allow empty nested markers.\n"+
			"If another error: the infrastructure generator must apply capture-regenerate-reemit.", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	outInj, ok := outDoc.Body().Injection("InfraNestedInj")
	if !ok {
		t.Error("[[INJECTION:InfraNestedInj]] absent from output after infrastructure-path regeneration of " +
			"[[DEPLOYED:InfrastructureAgents]]; the infrastructure generator must apply " +
			"capture-regenerate-reemit for nested injection regions, not only for nested custom regions")
	}

	if ok && outInj.Parent() != nil {
		if outInj.Parent().Kind() != docformat.NodeDeployed || outInj.Parent().Name() != "InfrastructureAgents" {
			t.Errorf("InfraNestedInj must be nested inside [[DEPLOYED:InfrastructureAgents]]; "+
				"got parent kind=%q name=%q", outInj.Parent().Kind(), outInj.Parent().Name())
		}
	}
}

// ---------------------------------------------------------------------------
// T6.5: Source check relaxation
//
// The ErrSourceDeployedRegionNotEmpty guard changes from checking TrimSpace(content) > 0
// to !SourceDeployedRegionIsEmpty(node). The relaxed predicate accepts empty nested
// user-owned markers while still rejecting other non-whitespace content.
// ---------------------------------------------------------------------------

// sourceWithEmptyNestedMarkers is a source where a deployed region contains only an empty
// nested [[INJECTION:]] marker. By the relaxed predicate, this region IS empty (the nested
// marker contributes no content). The transform must succeed, not error.
//
// Under the old check (len(TrimSpace(content)) > 0), the nested marker bytes are non-empty
// after trimming, so the old check incorrectly returns ErrSourceDeployedRegionNotEmpty.
const sourceWithEmptyNestedMarkers = `---
id: 605
version: 2.0.0
name: source-empty-nested-test
description: Agent for source empty-nested markers testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: source check relaxation testing
required_skills: []
---

[[SECTION:Identity]]
# SourceEmptyNested Agent

You are the SourceEmptyNested agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[INJECTION:NestedMarker]]
[[/INJECTION:NestedMarker]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// sourceWithProseInDeployed is a source where a deployed region contains actual prose
// (not just nested markers). This must still return ErrSourceDeployedRegionNotEmpty even
// after the relaxation — the relaxation only permits empty nested user-owned markers.
const sourceWithProseInDeployed = `---
id: 606
version: 2.0.0
name: source-prose-test
description: Agent for source prose-in-deployed testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: source check testing
required_skills: []
---

[[SECTION:Identity]]
# SourceProse Agent

You are the SourceProse agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
This is prose content inside a source deployed region and must still be an error.
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// sourceWithNonEmptyNestedMarker is a source where a deployed region contains a nested
// [[INJECTION:]] marker that has non-whitespace content. This must also error, because
// the nested marker itself carries content — SourceDeployedRegionIsEmpty returns false
// for non-empty nested nodes.
const sourceWithNonEmptyNestedMarker = `---
id: 607
version: 2.0.0
name: source-nonempty-nested-test
description: Agent for source non-empty-nested marker testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: source check testing
required_skills: []
---

[[SECTION:Identity]]
# SourceNonEmptyNested Agent

You are the SourceNonEmptyNested agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[INJECTION:NestedWithContent]]
This injection has content in the source, making the parent non-empty.
[[/INJECTION:NestedWithContent]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// TestSourceCheck_EmptyNestedMarkers_DoesNotError asserts that a source deployed region
// containing only empty nested [[INJECTION:]] markers is accepted by the transform.
// This is the positive half of the source-guard relaxation: the old TrimSpace check
// incorrectly rejects this, because the marker bytes are non-whitespace even when the
// nested injection has no content between its tags.
func TestSourceCheck_EmptyNestedMarkers_DoesNotError(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithEmptyNestedMarkers),
		Deployed: nil, // new deployment — no deployed file needed for this check
		Kind:     domain.ArtifactAgent,
		Key:      "source-empty-nested-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err != nil {
		if errors.Is(err, transform.ErrSourceDeployedRegionNotEmpty) {
			t.Fatalf("Apply returned ErrSourceDeployedRegionNotEmpty for a source deployed region "+
				"containing only an empty nested [[INJECTION:]] marker; the relaxed "+
				"SourceDeployedRegionIsEmpty predicate must accept empty nested user-owned markers: %v", err)
		}
		t.Fatalf("Apply returned unexpected error: %v", err)
	}
}

// TestSourceCheck_ProseInDeployed_StillErrors asserts that a source deployed region
// containing prose text still returns ErrSourceDeployedRegionNotEmpty after the relaxation.
// The relaxation only allows empty nested user-owned markers, not arbitrary prose.
func TestSourceCheck_ProseInDeployed_StillErrors(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProseInDeployed),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "source-prose-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when a source deployed region contains prose text; " +
			"only empty nested user-owned markers are permitted — the relaxation must not weaken " +
			"the guard for prose content")
	}
	if !errors.Is(err, transform.ErrSourceDeployedRegionNotEmpty) {
		t.Errorf("expected error wrapping ErrSourceDeployedRegionNotEmpty for prose in source " +
			"deployed region; got: %v", err)
	}
}

// TestSourceCheck_NonEmptyNestedMarker_StillErrors asserts that a source deployed region
// containing a nested [[INJECTION:]] marker with non-whitespace content still errors.
// A nested user-owned marker with content makes the parent non-empty by the relaxed predicate,
// so it must still be rejected.
func TestSourceCheck_NonEmptyNestedMarker_StillErrors(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithNonEmptyNestedMarker),
		Deployed: nil,
		Kind:     domain.ArtifactAgent,
		Key:      "source-nonempty-nested-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply must return an error when a source deployed region contains a nested " +
			"[[INJECTION:]] with non-whitespace content; a non-empty nested marker makes the " +
			"parent non-empty under the relaxed SourceDeployedRegionIsEmpty predicate")
	}
	if !errors.Is(err, transform.ErrSourceDeployedRegionNotEmpty) {
		t.Errorf("expected error wrapping ErrSourceDeployedRegionNotEmpty for non-empty nested " +
			"marker; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// T6.6: Deployed region with no nested content regenerates byte-identically
//
// This is the no-harm regression test: adding the capture-regenerate-reemit sequence
// must not introduce stray separators or trailing whitespace in deployed regions that
// have no nested content. When captureNestedUserRegions returns an empty slice,
// reemitNestedUserRegions must not touch the parent at all.
// ---------------------------------------------------------------------------

// noNestedDeployed is a deployed file where [[DEPLOYED:HarnessConstraints]] has canonical
// content but no nested user-owned regions. The output's HarnessConstraints content must
// be exactly what the harness module supplies — nothing more, nothing less.
const noNestedDeployed = `---
id: 608
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for no-nested regression testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NoNested Agent

You are the NoNested agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// noNestedSource matches noNestedDeployed as its source predecessor. It has no nested
// injections inside HarnessConstraints — only the bare empty deployed region.
const noNestedSource = `---
id: 608
version: 2.0.0
name: no-nested-test
description: Agent for no-nested regression testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: no-nested regression testing
required_skills: []
---

[[SECTION:Identity]]
# NoNested Agent

You are the NoNested agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// ---------------------------------------------------------------------------
// Regression: nested [[INJECTION:]] fully absent from source is orphaned, not resurrected
//
// A [[INJECTION:]] region that existed inside a [[DEPLOYED:]] parent in the deployed file,
// but whose marker was entirely removed from the source (not left as an empty placeholder —
// removed outright), must be treated as a removed injection: it must NOT appear in the output
// and must produce exactly one RegionOrphaned outcome and one GapRemovedInjection gap.
//
// Before the fix, captureNestedUserRegions captured deployed NodeInjection children in Pass 1
// (alongside NodeCustom), causing two contradictory outcomes for the same name: one
// "preserved-from-deployed" (from re-emission) and one "orphaned" (from post-loop orphan
// detection), while the content was silently resurrected in the output.
// ---------------------------------------------------------------------------

// nestedInjectionRemovedDeployed is a deployed file whose HarnessConstraints region contains
// a populated [[INJECTION:RemovedNested]] that the source (nestedPreservationSource) no
// longer declares at all. The marker was fully removed from the source — not left empty —
// so it must be treated as a removed injection: orphaned, not resurrected.
const nestedInjectionRemovedDeployed = `---
id: 600
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for nested region preservation testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

[[SECTION:Identity]]
# NestedPreservation Agent

You are the NestedPreservation agent.
[[/SECTION:Identity]]

[[SECTION:Constraints]]
## Constraints

Always be helpful.

[[DEPLOYED:HarnessConstraints]]
Fixture harness constraint: always use the approved tool list.
Do not invoke tools not declared in the agent's tools field.

[[INJECTION:RemovedNested]]
This injection was fully deleted from the source. It must not appear in the output.
Resurrecting it would prevent the user from ever removing a nested injection.
[[/INJECTION:RemovedNested]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// TestNestedInjection_FullyRemovedFromSource_IsOrphanedNotResurrected asserts that a
// [[INJECTION:]] that existed inside a [[DEPLOYED:]] parent in the deployed file but is
// entirely absent from the source produces exactly one RegionOrphaned outcome and one
// GapRemovedInjection gap — and is NOT present in the output document.
//
// This is a regression test for the bug where captureNestedUserRegions captured deployed
// NodeInjection children in Pass 1, causing the injection to be re-emitted into the output
// while simultaneously being reported as orphaned, yielding two contradictory outcomes and
// making it impossible to actually delete a nested injection through source edits.
func TestNestedInjection_FullyRemovedFromSource_IsOrphanedNotResurrected(t *testing.T) {
	req := transform.Request{
		// Source declares HarnessConstraints with NO nested injection — the injection
		// marker was fully deleted from the source, not left as an empty placeholder.
		Source:   []byte(nestedPreservationSource),
		Deployed: []byte(nestedInjectionRemovedDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "nested-preservation-test",
		Module:   newInjectionFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The injection must NOT appear in the output.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Injection("RemovedNested"); ok {
		t.Error("[[INJECTION:RemovedNested]] is present in the output; a nested injection " +
			"fully deleted from the source must be removed, not resurrected from the deployed file. " +
			"captureNestedUserRegions must not capture NodeInjection children from the deployed tree.")
	}

	// Count outcomes for RemovedNested — must be exactly one, and it must be orphaned.
	var matchingOutcomes []transform.RegionOutcome
	for _, o := range result.Report.Regions {
		if o.Name == "RemovedNested" {
			matchingOutcomes = append(matchingOutcomes, o)
		}
	}
	if len(matchingOutcomes) == 0 {
		t.Fatal("no RegionOutcome for RemovedNested found; expected exactly one with Action=orphaned")
	}
	if len(matchingOutcomes) > 1 {
		actions := make([]string, len(matchingOutcomes))
		for i, o := range matchingOutcomes {
			actions[i] = string(o.Action)
		}
		t.Fatalf("got %d RegionOutcome entries for RemovedNested (actions: %v); "+
			"must be exactly one with Action=orphaned. "+
			"Two contradictory outcomes indicate the injection was both re-emitted (preserved) "+
			"and detected as an orphan in the same run.", len(matchingOutcomes), actions)
	}
	if matchingOutcomes[0].Action != transform.RegionOrphaned {
		t.Errorf("RemovedNested RegionOutcome Action: want %q (RegionOrphaned), got %q; "+
			"a nested injection fully removed from the source must produce an orphan outcome, "+
			"not a preserved or preserved-custom outcome",
			transform.RegionOrphaned, matchingOutcomes[0].Action)
	}

	// Count GapRemovedInjection gaps for RemovedNested — must be exactly one.
	var matchingGaps []domain.Gap
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapRemovedInjection && g.Subject == "RemovedNested" {
			matchingGaps = append(matchingGaps, g)
		}
	}
	if len(matchingGaps) == 0 {
		t.Fatal("no GapRemovedInjection gap for RemovedNested found; " +
			"a removed nested injection must produce a recovery gap so the user can retrieve the content")
	}
	if len(matchingGaps) > 1 {
		t.Fatalf("got %d GapRemovedInjection gaps for RemovedNested; must be exactly one", len(matchingGaps))
	}
}

// TestNoNestedContent_RegeneratesWithoutStrayContent asserts that a [[DEPLOYED:]] region
// with no nested user-owned content regenerates with exactly the canonical content the
// generator supplies — no trailing separator, no extra newline, no whitespace drift.
//
// This test protects against a regression in which reemitNestedUserRegions writes a
// trailing separator even when captured is empty. Per the design, when captured is empty
// the function must not touch the parent node at all.
func TestNoNestedContent_RegeneratesWithoutStrayContent(t *testing.T) {
	req := transform.Request{
		Source:   []byte(noNestedSource),
		Deployed: []byte(noNestedDeployed),
		Kind:     domain.ArtifactAgent,
		Key:      "no-nested-test",
		Module:   newInjectionFixtureModule(t),
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

	harnessNode, ok := outDoc.Body().Deployed("HarnessConstraints")
	if !ok {
		t.Fatal("HarnessConstraints absent from output; this fixture should always produce it")
	}

	// The content must consist of exactly the canonical harness text — nothing extra.
	// The harness content from the injection fixture ends with a newline (the generator
	// appends one if absent). No additional content should follow it inside the node.
	canonicalContent := []byte("Fixture harness constraint: always use the approved tool list.\n" +
		"Do not invoke tools not declared in the agent's tools field.\n")

	if !bytes.Equal(harnessNode.Content(), canonicalContent) {
		t.Errorf("HarnessConstraints content with no nested regions is not byte-identical to "+
			"the expected canonical content; reemitNestedUserRegions must not add stray "+
			"content when captured is empty.\n"+
			"want: %q\n got: %q", canonicalContent, harnessNode.Content())
	}

	// No custom regions must be present in the output (no nested content in this fixture).
	customs := outDoc.Body().CustomRegions()
	if len(customs) > 0 {
		names := make([]string, len(customs))
		for i, n := range customs {
			names[i] = n.Name()
		}
		t.Errorf("no custom regions expected in output for no-nested fixture; found: %v", names)
	}
}
