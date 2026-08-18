package transform_test

// injection_merge_test.go covers Stage 10 injection merge behaviour:
//
//   - New deployment: harness injections filled from descriptor, project injections left empty
//     and recorded as GapEmptyInjection gaps.
//   - Update: project injection content lifted byte-identically from the deployed file;
//     harness injections refreshed from the descriptor regardless of deployed content.
//   - Section refresh: section body prose is taken from the generic source, not the deployed
//     file, even when the deployed file has stale content.
//   - Nested injection survival: when a section is refreshed, a nested project injection's
//     user content is preserved.
//   - Added/removed injection points: InjectionAdded for new injection points in the source;
//     InjectionOrphaned and GapRemovedInjection for points gone from the source.
//   - Purity: body prose outside injection regions is byte-identical to the generic source
//     in every scenario.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-common/docformat"
	"mosaic-deploy/internal/harness/descriptor"
	"mosaic-deploy/internal/transform"
)

// newInjectionFixtureModule returns a domain.HarnessModule backed by
// testdata/transform/injection_fixture.yaml. This descriptor is identical to the base
// fixture.yaml but includes HarnessConstraints injection content. It exists as a separate
// file so that the Stage 8 body-preservation tests (which rely on the fixture producing no
// injection content) remain unaffected.
func newInjectionFixtureModule(t *testing.T) domain.HarnessModule {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(testdataDir, "injection_fixture.yaml"))
	if err != nil {
		t.Fatalf("read injection fixture descriptor: %v", err)
	}
	desc, err := descriptor.Parse(src, testdataDir+"/injection_fixture.yaml")
	if err != nil {
		t.Fatalf("parse injection fixture descriptor: %v", err)
	}
	return &fixtureModule{desc: desc}
}

// ---------------------------------------------------------------------------
// Shared fixture documents for injection merge tests.
//
// The injection vocabulary used in these fixtures:
//   HarnessConstraints  — InjectionHarness (filled from descriptor every time)
//   CodebaseContext     — InjectionProject (preserved on update, empty on create)
//   IdentityExtension   — InjectionProject (preserved on update, empty on create)
//
// HarnessConstraints is the sole InjectionHarness canonical name. CustomConstraints, which
// this fixture set formerly used as a second harness-class region to exercise the
// "declared but left empty by the module" path, has been removed from the canonical
// vocabulary entirely and can no longer appear under managed region.
// ---------------------------------------------------------------------------

// mergeSourceBase is a generic source that has one harness injection (HarnessConstraints)
// and one project injection (IdentityExtension) across two sections.
const mergeSourceBase = `---
id: 10
version: 2.0.0
name: merge-test
description: Agent for injection merge testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: merge testing
required_skills: []
---

<Identity type="core">
# MergeTest Agent

You are the MergeTest agent.

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

// mergeDeployedFilled is the deployed version of mergeSourceBase with user content in
// the project injections and old (stale) content in the harness injection.
const mergeDeployedFilled = `---
id: 10
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for injection merge testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# MergeTest Agent

You are the MergeTest agent.

<IdentityExtension type="project">
User identity extension content line one.
User identity extension content line two.
</IdentityExtension>
</Identity>

<Constraints type="core">
## Constraints

Always be helpful.

<HarnessConstraints type="managed">
Old harness constraint text that should be replaced on every transform.
</HarnessConstraints>
</Constraints>
`

// sourceWithSingleHarnessInjection is a source with only one harness injection
// (HarnessConstraints), used by tests that need to verify the InjectionFilled path
// without triggering the adjacent-injection SetContent interaction.
const sourceWithSingleHarnessInjection = `---
id: 11
version: 1.0.0
name: harness-fill-test
description: Agent for harness injection fill testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: harness fill testing
required_skills: []
---

<Constraints type="core">
## Constraints

<HarnessConstraints type="managed">
</HarnessConstraints>
</Constraints>
`

// ---------------------------------------------------------------------------
// T10.1 — New deployment: harness injections filled, project injections empty + gaps
// ---------------------------------------------------------------------------

// TestNewDeployment_HarnessInjection_FilledFromDescriptor asserts that when Deployed is nil
// (new deployment), the harness-level injection HarnessConstraints is filled with the
// content declared in the harness descriptor and the action is InjectionFilled.
func TestNewDeployment_HarnessInjection_FilledFromDescriptor(t *testing.T) {
	// Use the injection-specific fixture module which declares HarnessConstraints content.
	// The base fixture has no injection content (so Stage 8 body-preservation tests remain
	// unaffected). This test verifies the Stage 8 InjectionFilled path is exercised when
	// the harness supplies content, as a precondition for Stage 10 merge correctness.
	mod := newInjectionFixtureModule(t)
	harnessContent, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: ""})
	if !ok || harnessContent == "" {
		t.Fatalf("injection_fixture.yaml must declare HarnessConstraints content but does not")
	}

	req := transform.Request{
		Source:   []byte(sourceWithSingleHarnessInjection),
		Deployed: nil, // new deployment
		Kind:     domain.ArtifactAgent,
		Key:      "harness-fill-test",
		Module:   mod,
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Locate the HarnessConstraints outcome.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "HarnessConstraints" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for HarnessConstraints; outcomes: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionFilled {
		t.Errorf("HarnessConstraints action on new deployment: want %q, got %q",
			transform.RegionFilled, outcome.Action)
	}
	if outcome.Class != domain.InjectionHarness {
		t.Errorf("HarnessConstraints class: want %q, got %q", domain.InjectionHarness, outcome.Class)
	}

	// Verify the content appears in the output document.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, nodeOK := outDoc.Body().Deployed("HarnessConstraints")
	if !nodeOK {
		t.Fatal("HarnessConstraints injection absent from output")
	}
	if !bytes.Contains(node.Content(), []byte(harnessContent)) {
		t.Errorf("HarnessConstraints output content does not contain descriptor content\nwant (in): %q\ngot: %q",
			harnessContent, node.Content())
	}
}

// TestNewDeployment_ProjectInjections_EmittedEmpty asserts that when Deployed is nil,
// every project-level injection in the source is left empty in the output (InjectionEmptied
// action). No user has had the opportunity to fill it on a first deployment.
func TestNewDeployment_ProjectInjections_EmittedEmpty(t *testing.T) {
	req := transform.Request{
		Source:   []byte(mergeSourceBase),
		Deployed: nil, // new deployment
		Kind:     domain.ArtifactAgent,
		Key:      "merge-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Project injections in mergeSourceBase that must be empty on first deployment.
	projectInjections := []string{"IdentityExtension"}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	for _, name := range projectInjections {
		name := name
		t.Run(name, func(t *testing.T) {
			// The outcome must report the injection as emptied.
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
			if outcome.Action != transform.RegionEmptied {
				t.Errorf("%s action on new deployment: want %q, got %q",
					name, transform.RegionEmptied, outcome.Action)
			}

			// The injection node itself must be empty (whitespace only).
			node, ok := outDoc.Body().Injection(name)
			if !ok {
				t.Fatalf("%s injection absent from output document", name)
			}
			if !node.IsEmpty() {
				t.Errorf("%s injection must be empty on new deployment, but content: %q",
					name, node.Content())
			}
		})
	}
}

// TestNewDeployment_ProjectInjections_GapsEmitted asserts that a GapEmptyInjection gap is
// produced for each project-level injection that is left empty on a new deployment. These
// gaps drive the TODO checklist that tells the user what to fill in.
func TestNewDeployment_ProjectInjections_GapsEmitted(t *testing.T) {
	req := transform.Request{
		Source:   []byte(mergeSourceBase),
		Deployed: nil, // new deployment
		Kind:     domain.ArtifactAgent,
		Key:      "merge-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Build a set of GapEmptyInjection subjects for easy lookup.
	emptyGaps := make(map[string]bool)
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapEmptyInjection {
			emptyGaps[g.Subject] = true
		}
	}

	// Only project-class injections produce GapEmptyInjection gaps.
	projectInjections := []string{"IdentityExtension"}
	for _, name := range projectInjections {
		if !emptyGaps[name] {
			t.Errorf("expected GapEmptyInjection for %q on new deployment, but none found; gaps: %v",
				name, result.Report.Gaps)
		}
	}

	// Harness-level injections must NOT produce GapEmptyInjection gaps (they are always filled
	// or intentionally empty from the harness).
	if emptyGaps["HarnessConstraints"] {
		t.Error("HarnessConstraints must not produce a GapEmptyInjection gap; it is a harness injection")
	}
}

// ---------------------------------------------------------------------------
// T10.2 — Update: injection content lifted byte-identically from the deployed file
// ---------------------------------------------------------------------------

// TestUpdate_ProjectInjection_ContentLiftedByteIdentically asserts that on an update
// (Deployed non-nil), every project injection's content is preserved verbatim from the
// deployed file. The lifted content must be byte-identical — no trimming, normalisation,
// or re-encoding.
func TestUpdate_ProjectInjection_ContentLiftedByteIdentically(t *testing.T) {
	req := transform.Request{
		Source:   []byte(mergeSourceBase),
		Deployed: []byte(mergeDeployedFilled),
		Kind:     domain.ArtifactAgent,
		Key:      "merge-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Parse both deployed and output docs to compare injection content by node.
	depDoc, err := docformat.Parse([]byte(mergeDeployedFilled))
	if err != nil {
		t.Fatalf("parse deployed: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// Verify each project injection in turn.
	cases := []struct {
		name       string
		wantAction transform.RegionAction
	}{
		{"IdentityExtension", transform.RegionPreserved},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			// Check the report outcome.
			var outcome *transform.RegionOutcome
			for i := range result.Report.Regions {
				if result.Report.Regions[i].Name == c.name {
					outcome = &result.Report.Regions[i]
					break
				}
			}
			if outcome == nil {
				t.Fatalf("RegionOutcome for %q absent from report", c.name)
			}
			if outcome.Action != c.wantAction {
				t.Errorf("%s action: want %q, got %q", c.name, c.wantAction, outcome.Action)
			}

			// Check byte-identity with the deployed content.
			depNode, depOK := depDoc.Body().Injection(c.name)
			if !depOK {
				t.Fatalf("%s injection absent from deployed fixture", c.name)
			}
			outNode, outOK := outDoc.Body().Injection(c.name)
			if !outOK {
				t.Fatalf("%s injection absent from output", c.name)
			}
			if !bytes.Equal(outNode.Content(), depNode.Content()) {
				t.Errorf("%s content not byte-identical to deployed:\ndeployed: %q\noutput:   %q",
					c.name, depNode.Content(), outNode.Content())
			}
			if outcome.Bytes != len(depNode.Content()) {
				t.Errorf("%s RegionOutcome.Bytes: want %d, got %d",
					c.name, len(depNode.Content()), outcome.Bytes)
			}
		})
	}
}

// deployedWithStaleHarnessContent is a deployed agent whose HarnessConstraints injection
// contains text that differs from the descriptor (simulating a stale harness version).
const deployedWithStaleHarnessContent = `---
id: 11
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for harness injection fill testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Constraints type="core">
## Constraints

<HarnessConstraints type="managed">
Old harness constraint text that should be replaced on every transform.
</HarnessConstraints>
</Constraints>
`

// TestUpdate_HarnessInjection_RefreshedFromDescriptorNotDeployed asserts that on an update,
// the harness injection HarnessConstraints is refreshed from the harness descriptor, not
// lifted from the deployed file. Stale harness content in the deployed file must not
// survive.
func TestUpdate_HarnessInjection_RefreshedFromDescriptorNotDeployed(t *testing.T) {
	mod := newInjectionFixtureModule(t)
	harnessContent, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: ""})
	if !ok || harnessContent == "" {
		t.Fatalf("injection_fixture.yaml must declare HarnessConstraints content but does not")
	}

	req := transform.Request{
		Source:   []byte(sourceWithSingleHarnessInjection),
		Deployed: []byte(deployedWithStaleHarnessContent), // has stale harness content
		Kind:     domain.ArtifactAgent,
		Key:      "harness-fill-test",
		Module:   mod,
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "HarnessConstraints" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for HarnessConstraints absent")
	}
	// On update, the harness injection must still be filled from the descriptor.
	if outcome.Action != transform.RegionFilled {
		t.Errorf("HarnessConstraints action on update: want %q, got %q",
			transform.RegionFilled, outcome.Action)
	}

	// The output must NOT contain the stale deployed text.
	if bytes.Contains(result.Output, []byte("Old harness constraint text that should be replaced")) {
		t.Error("output contains stale harness content from the deployed file; must be replaced from descriptor")
	}
	// The output must contain the current descriptor text.
	if !bytes.Contains(result.Output, []byte(harnessContent)) {
		t.Errorf("output does not contain current harness descriptor content %q", harnessContent)
	}
}

// ---------------------------------------------------------------------------
// T10.3 — Section refresh from generic source on update
// ---------------------------------------------------------------------------

// mergeSourceUpdatedProse is a source whose Identity section prose has been updated
// relative to the deployed file below.
const mergeSourceUpdatedProse = `---
id: 10
version: 2.1.0
name: prose-refresh-test
description: Agent for section prose refresh testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: prose refresh testing
required_skills: []
---

<Identity type="core">
# ProseRefreshTest Agent

You are the ProseRefreshTest agent.

NEW GENERIC PROSE THAT DID NOT EXIST IN THE DEPLOYED VERSION.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`

// mergeDeployedStaleProse is a deployed agent with stale Identity section prose
// and filled IdentityExtension injection.
const mergeDeployedStaleProse = `---
id: 10
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for section prose refresh testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# ProseRefreshTest Agent

You are the ProseRefreshTest agent.

STALE PROSE FROM AN OLD GENERIC SOURCE VERSION.

<IdentityExtension type="project">
User identity extension: TypeScript/Node.js project, Jest test framework.
</IdentityExtension>
</Identity>
`

// TestUpdate_SectionProse_RefreshedFromSource asserts that when the generic source changes
// the body prose inside a section, the update produces output that contains the new prose
// from the source, not the stale prose from the deployed file. Sections are refreshed from
// the generic source; only injection content is lifted from the deployed file.
func TestUpdate_SectionProse_RefreshedFromSource(t *testing.T) {
	req := transform.Request{
		Source:   []byte(mergeSourceUpdatedProse),
		Deployed: []byte(mergeDeployedStaleProse),
		Kind:     domain.ArtifactAgent,
		Key:      "prose-refresh-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The new generic prose must be present.
	if !bytes.Contains(result.Output, []byte("NEW GENERIC PROSE THAT DID NOT EXIST IN THE DEPLOYED VERSION.")) {
		t.Error("output does not contain new generic prose from source; section was not refreshed")
	}
	// The stale prose must not appear.
	if bytes.Contains(result.Output, []byte("STALE PROSE FROM AN OLD GENERIC SOURCE VERSION.")) {
		t.Error("output contains stale prose from deployed file; section was not refreshed from source")
	}
}

// ---------------------------------------------------------------------------
// T10.4 — Nested injection survives a section refresh
// ---------------------------------------------------------------------------

// TestUpdate_NestedInjection_SurvivesSectionRefresh asserts that when a section's body
// prose is refreshed from the generic source, a project injection nested inside that
// section retains the user's content from the deployed file. The refresh replaces prose,
// not injections.
func TestUpdate_NestedInjection_SurvivesSectionRefresh(t *testing.T) {
	const userExtensionContent = "User identity extension: TypeScript/Node.js project, Jest test framework.\n"

	req := transform.Request{
		Source:   []byte(mergeSourceUpdatedProse),
		Deployed: []byte(mergeDeployedStaleProse),
		Kind:     domain.ArtifactAgent,
		Key:      "prose-refresh-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The injection content from the deployed file must survive in the output.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("IdentityExtension injection absent from output")
	}
	if !bytes.Contains(node.Content(), []byte(userExtensionContent)) {
		t.Errorf("IdentityExtension content was lost during section refresh\nwant (in): %q\ngot: %q",
			userExtensionContent, node.Content())
	}

	// And the section prose must have been refreshed (verified by T10.3 above, but
	// asserting both in one test makes the combined invariant explicit).
	if !bytes.Contains(result.Output, []byte("NEW GENERIC PROSE THAT DID NOT EXIST IN THE DEPLOYED VERSION.")) {
		t.Error("section prose not refreshed; only injection content should survive from deployed file")
	}
	if bytes.Contains(result.Output, []byte("STALE PROSE FROM AN OLD GENERIC SOURCE VERSION.")) {
		t.Error("stale prose still present; section must be refreshed from source")
	}
}

// ---------------------------------------------------------------------------
// T10.5 — Added and removed injection points across versions
// ---------------------------------------------------------------------------

// sourceWithAddedInjection has a new injection (ErrorHandlingExtension) that does not exist
// in the corresponding deployed document below.
const sourceWithAddedInjection = `---
id: 10
version: 3.0.0
name: version-change-test
description: Agent for added-injection testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: version change testing
required_skills: []
---

<Identity type="core">
# VersionChangeTest Agent

You are the VersionChangeTest agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>

<ErrorHandling type="core">
## Error Handling

<ErrorHandlingExtension type="project">
</ErrorHandlingExtension>
</ErrorHandling>
`

// deployedWithoutAddedInjection is the deployed version that predates the
// ErrorHandlingExtension injection point.
const deployedWithoutAddedInjection = `---
id: 10
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for added-injection testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# VersionChangeTest Agent

You are the VersionChangeTest agent.

<IdentityExtension type="project">
User identity extension content that must survive.
</IdentityExtension>
</Identity>
`

// sourceWithoutRemovedInjection has a source that no longer contains the CustomConstraints
// injection that was present in the deployed file below.
const sourceWithoutRemovedInjection = `---
id: 10
version: 3.0.0
name: removed-injection-test
description: Agent for removed-injection testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: removed injection testing
required_skills: []
---

<Identity type="core">
# RemovedInjectionTest Agent

You are the RemovedInjectionTest agent.

<IdentityExtension type="project">
</IdentityExtension>
</Identity>
`

// deployedWithRemovedInjection is the deployed version that still has CustomConstraints,
// which has since been removed from the generic source.
const deployedWithRemovedInjection = `---
id: 10
version: 2.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for removed-injection testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# RemovedInjectionTest Agent

You are the RemovedInjectionTest agent.

<IdentityExtension type="project">
User identity content.
</IdentityExtension>
</Identity>

<Constraints type="core">
## Constraints

<CustomConstraints type="project">
User-defined constraint: never touch /etc/hosts.
</CustomConstraints>
</Constraints>
`

// TestUpdate_AddedInjectionPoint_StartsEmpty asserts that when the generic source gains a
// new injection point that did not exist in the deployed file, the injection is present in
// the output and empty (the user has not had the chance to fill it yet). The action is
// InjectionAdded.
func TestUpdate_AddedInjectionPoint_StartsEmpty(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithAddedInjection),
		Deployed: []byte(deployedWithoutAddedInjection),
		Kind:     domain.ArtifactAgent,
		Key:      "version-change-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The new injection must appear in the output.
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Injection("ErrorHandlingExtension")
	if !ok {
		t.Fatal("ErrorHandlingExtension injection absent from output; it must be present when the source has it")
	}
	if !node.IsEmpty() {
		t.Errorf("ErrorHandlingExtension should be empty (not in deployed file), got content: %q", node.Content())
	}

	// The report must record InjectionAdded for this injection.
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "ErrorHandlingExtension" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for ErrorHandlingExtension absent from report")
	}
	if outcome.Action != transform.RegionAdded {
		t.Errorf("ErrorHandlingExtension action: want %q, got %q", transform.RegionAdded, outcome.Action)
	}

	// Pre-existing project injection must still be preserved.
	extNode, extOK := outDoc.Body().Injection("IdentityExtension")
	if !extOK {
		t.Fatal("IdentityExtension absent from output after added-injection update")
	}
	if !bytes.Contains(extNode.Content(), []byte("User identity extension content that must survive.")) {
		t.Errorf("IdentityExtension content lost after update: %q", extNode.Content())
	}
}

// TestUpdate_RemovedInjectionPoint_GapEmitted asserts that when the generic source removes
// an injection point that still exists in the deployed file (and has user content), a
// GapRemovedInjection gap is emitted. The removed injection must not appear in the output
// (it is not part of the source any longer), but the gap record lets the user recover the
// content from the backup.
func TestUpdate_RemovedInjectionPoint_GapEmitted(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithoutRemovedInjection),
		Deployed: []byte(deployedWithRemovedInjection),
		Kind:     domain.ArtifactAgent,
		Key:      "removed-injection-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// A GapRemovedInjection gap must be emitted for CustomConstraints, carrying the orphaned
	// content so the user can recover it. This is the sole mechanism preventing silent data loss
	// when an injection point is removed from the generic source.
	var removedGap *domain.Gap
	for i := range result.Report.Gaps {
		if result.Report.Gaps[i].Kind == domain.GapRemovedInjection && result.Report.Gaps[i].Subject == "CustomConstraints" {
			removedGap = &result.Report.Gaps[i]
			break
		}
	}
	if removedGap == nil {
		t.Fatalf("expected GapRemovedInjection for CustomConstraints; gaps: %v", result.Report.Gaps)
	}
	// The gap's Fragment must carry the orphaned injection content. deployedWithRemovedInjection
	// contains "User-defined constraint: never touch /etc/hosts." inside CustomConstraints.
	// An implementation that records the gap with an empty Fragment silently discards the content
	// with no recovery path for the user.
	if removedGap.Fragment == "" || !strings.Contains(removedGap.Fragment, "User-defined constraint: never touch /etc/hosts.") {
		t.Errorf("GapRemovedInjection.Fragment must contain the orphaned injection content for user recovery;\nFragment: %q\nexpected to contain: %q",
			removedGap.Fragment, "User-defined constraint: never touch /etc/hosts.")
	}

	// The removed injection must not appear in the output (it is not in the source).
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, orphanOK := outDoc.Body().Injection("CustomConstraints"); orphanOK {
		t.Error("CustomConstraints injection must not appear in output because it is absent from the source")
	}

	// The report must record InjectionOrphaned (the deployed had content; source does not).
	var outcome *transform.RegionOutcome
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == "CustomConstraints" {
			outcome = &result.Report.Regions[i]
			break
		}
	}
	if outcome == nil {
		t.Fatalf("RegionOutcome for CustomConstraints absent; report: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionOrphaned {
		t.Errorf("CustomConstraints action: want %q, got %q", transform.RegionOrphaned, outcome.Action)
	}
}

// ---------------------------------------------------------------------------
// T10.8 — Purity: body prose outside injections is byte-identical to generic source
// ---------------------------------------------------------------------------

// TestPurity_BodyProseOutsideInjections_ByteIdenticalToSource asserts that body text that
// is not inside an injection region is byte-identical to the generic source in every
// scenario that Stage 10 introduces (new deployment, update, section refresh). Only
// injection content is allowed to differ from the source.
func TestPurity_BodyProseOutsideInjections_ByteIdenticalToSource(t *testing.T) {
	scenarios := []struct {
		name     string
		source   string
		deployed string // empty string = nil Deployed (new deployment)
	}{
		{
			name:     "new deployment",
			source:   mergeSourceBase,
			deployed: "",
		},
		{
			name:     "update with preserved injections",
			source:   mergeSourceBase,
			deployed: mergeDeployedFilled,
		},
		{
			name:     "update with section prose refresh",
			source:   mergeSourceUpdatedProse,
			deployed: mergeDeployedStaleProse,
		},
		{
			name:     "update with added injection point",
			source:   sourceWithAddedInjection,
			deployed: deployedWithoutAddedInjection,
		},
		{
			name:     "update with removed injection point",
			source:   sourceWithoutRemovedInjection,
			deployed: deployedWithRemovedInjection,
		},
	}

	for _, s := range scenarios {
		s := s
		t.Run(s.name, func(t *testing.T) {
			var deployed []byte
			if s.deployed != "" {
				deployed = []byte(s.deployed)
			}

			req := transform.Request{
				Source:   []byte(s.source),
				Deployed: deployed,
				Kind:     domain.ArtifactAgent,
				Key:      "purity-test",
				Module:   newFixtureModule(t),
				Model:    fixtureModel(),
				Scope:    domain.ScopeProject,
			}

			result, err := transform.Apply(req)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			// Extract body bytes (everything after the frontmatter delimiter).
			_, srcBody, err := docformat.SplitFrontmatter([]byte(s.source))
			if err != nil {
				t.Fatalf("SplitFrontmatter(source): %v", err)
			}
			_, outBody, err := docformat.SplitFrontmatter(result.Output)
			if err != nil {
				t.Fatalf("SplitFrontmatter(output): %v", err)
			}

			// Parse both bodies and compare all nodes that are NOT injection content.
			// The simplest correct check: every byte of the source body that is NOT
			// between project-injection or managed-region open and close tags must
			// appear at the same relative position in the output body.
			//
			// We use a structural approach: strip injection content from both bodies
			// and check that the remainder is identical.
			strippedSrc := stripInjectionContent(srcBody)
			strippedOut := stripInjectionContent(outBody)

			if !bytes.Equal(strippedSrc, strippedOut) {
				t.Errorf("body prose outside injection regions differs from source:\n"+
					"source (%d bytes):\n%q\n\noutput (%d bytes):\n%q",
					len(strippedSrc), truncateBytes(strippedSrc, 400),
					len(strippedOut), truncateBytes(strippedOut, 400))
			}
		})
	}
}

// stripInjectionContent replaces the content between every project-injection and
// managed-region tag pair with a fixed placeholder, so the surrounding prose can be compared
// independently of managed region content. Both user-owned and tool-managed regions are
// stripped because the purity invariant covers body bytes outside ALL managed regions.
//
// The helper is name-aware: it opens on <Name type="project|managed"> and closes only on
// </Name> for the same tag name, so nested tags of different names do not accidentally
// trigger an early close.
func stripInjectionContent(body []byte) []byte {
	var out []byte
	lines := bytes.Split(body, []byte("\n"))
	inside := false
	var openName []byte // tag name of the currently open managed/project region
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if !inside {
			// Detect <Name type="project"> or <Name type="managed"> open tags.
			if bytes.HasPrefix(trimmed, []byte("<")) &&
				!bytes.HasPrefix(trimmed, []byte("</")) &&
				(bytes.Contains(trimmed, []byte(`type="project"`)) ||
					bytes.Contains(trimmed, []byte(`type="managed"`))) {
				// Extract the tag name (first word after '<').
				rest := trimmed[1:] // strip '<'
				spaceIdx := bytes.IndexAny(rest, " \t>")
				if spaceIdx > 0 {
					inside = true
					openName = rest[:spaceIdx]
					out = append(out, line...)
					out = append(out, '\n')
					continue
				}
			}
		} else {
			// Detect </Name> where Name matches the open tag name.
			closeTag := append([]byte("</"), openName...)
			closeTag = append(closeTag, '>')
			if bytes.Equal(trimmed, closeTag) {
				inside = false
				openName = nil
				out = append(out, line...)
				out = append(out, '\n')
				continue
			}
			// Skip managed region content — this is what may differ.
			continue
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	// Remove the trailing newline added by the final iteration.
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out
}
