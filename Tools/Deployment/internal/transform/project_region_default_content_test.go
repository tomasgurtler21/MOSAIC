package transform_test

// project_region_default_content_test.go covers the TDD RED phase for Stage 7: project-region
// default content. Tests cover three deployment paths for project regions:
//
//  1. New deployment (req.Deployed == nil): three source shapes — non-empty default, empty, and
//     whitespace-only.
//  2. Update where the region is newly added to the source (absent from the deployed file):
//     the same three source shapes.
//  3. Update where the region already exists in the deployed file: byte-identical preservation,
//     including when the source default has changed and when the deployed content is empty.
//
// This file references transform.RegionDefaulted and domain.GapDefaultContentDeployed, which are
// not yet declared. The file will not compile until implementation (I7.1-I7.3) adds those
// constants — that compile failure is the intended TDD RED state.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Source document fixtures
// ---------------------------------------------------------------------------

// srcWithNonEmptyProjectDefault is a source document where the CodebaseContext project region
// carries non-empty default content authored by the agent designer.
const srcWithNonEmptyProjectDefault = `---
id: 1
version: 1.0.0
name: project-region-default-test
description: Agent for project region default content testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
This is the default codebase context provided by the agent author.
It describes the repository structure and coding conventions.
</CodebaseContext>
</Capabilities>
`

// srcWithEmptyProjectRegion is a source document where the CodebaseContext project region is
// empty (no content between the tags). Existing behavior must be unchanged for this case.
const srcWithEmptyProjectRegion = `---
id: 1
version: 1.0.0
name: project-region-default-test
description: Agent for project region default content testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
</CodebaseContext>
</Capabilities>
`

// srcWithWhitespaceOnlyProjectDefault is a source document where the CodebaseContext project
// region contains only whitespace. A whitespace-only body must be treated as empty.
const srcWithWhitespaceOnlyProjectDefault = `---
id: 1
version: 1.0.0
name: project-region-default-test
description: Agent for project region default content testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">

</CodebaseContext>
</Capabilities>
`

// srcWithChangedProjectDefault is a source document where the CodebaseContext project region
// has been updated to different default content since the initial deployment. Used in the T7.3
// regression tests to verify that the deployed file's content wins regardless.
const srcWithChangedProjectDefault = `---
id: 1
version: 1.0.0
name: project-region-default-test
description: Agent for project region default content testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
UPDATED DEFAULT: this content was changed in the source after the first deployment.
The deployed file's content must win regardless of what the source says now.
</CodebaseContext>
</Capabilities>
`

// ---------------------------------------------------------------------------
// Deployed document fixtures
// ---------------------------------------------------------------------------

// deployedWithoutCodebaseContext is a deployed document where the CodebaseContext project
// region does not exist — it was added to the source after this file was last deployed.
// Used in T7.2 tests where the region is "newly added to the source".
const deployedWithoutCodebaseContext = `---
id: 1
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for project region default content testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Capabilities type="core">
## Capabilities

Core capabilities.
</Capabilities>
`

// deployedWithCodebaseContextContent is a deployed document where the user has populated the
// CodebaseContext project region with content that differs from the source default.
// Used in T7.3 tests to verify that the deployed file's content wins over a changed source default.
const deployedWithCodebaseContextContent = `---
id: 1
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for project region default content testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
Original deployed content — written by the user after initial deployment.
This differs from both the source's initial default and its later update.
</CodebaseContext>
</Capabilities>
`

// deployedWithEmptyCodebaseContext is a deployed document where the CodebaseContext project
// region exists but has been cleared by the user (empty content).
// Used in T7.3 tests to verify that even an empty deployed state is preserved verbatim.
const deployedWithEmptyCodebaseContext = `---
id: 1
version: 1.0.0
transform_version: 3.0.0
injections_version: 1.2.0
description: Agent for project region default content testing
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Capabilities type="core">
## Capabilities

Core capabilities.

<CodebaseContext type="project">
</CodebaseContext>
</Capabilities>
`

// ---------------------------------------------------------------------------
// Search helpers
// ---------------------------------------------------------------------------

// findRegionOutcomeByName searches result.Report.Regions for the first outcome with the
// given region name. Returns nil when the name is not found.
func findRegionOutcomeByName(result transform.Result, name string) *transform.RegionOutcome {
	for i := range result.Report.Regions {
		if result.Report.Regions[i].Name == name {
			return &result.Report.Regions[i]
		}
	}
	return nil
}

// findProjectGapOfKind searches result.Report.Gaps for the first gap of the given kind.
// Returns nil when no such gap exists.
func findProjectGapOfKind(result transform.Result, kind domain.GapKind) *domain.Gap {
	for i := range result.Report.Gaps {
		if result.Report.Gaps[i].Kind == kind {
			return &result.Report.Gaps[i]
		}
	}
	return nil
}

// injectionContent parses doc and returns the Content() bytes of the named injection node.
// Calls t.Fatal when the document cannot be parsed or the node is absent.
func injectionContent(t *testing.T, doc []byte, name string) []byte {
	t.Helper()
	parsed, err := docformat.Parse(doc)
	if err != nil {
		t.Fatalf("docformat.Parse: %v", err)
	}
	node, ok := parsed.Body().Injection(name)
	if !ok {
		t.Fatalf("injection %q absent from document", name)
	}
	return node.Content()
}

// ---------------------------------------------------------------------------
// T7.1: New deployment (req.Deployed == nil) — three source shapes
// ---------------------------------------------------------------------------

// TestProjectRegion_NewDeploy_NonEmptyDefault_ContentCarriedThrough asserts that when
// req.Deployed is nil and the source project region carries non-empty default content, that
// content appears byte-identically in the deployed output.
func TestProjectRegion_NewDeploy_NonEmptyDefault_ContentCarriedThrough(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithNonEmptyProjectDefault),
		Kind:   domain.ArtifactAgent,
		Key:    "project-region-default-test",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
		// Deployed is nil — first deployment.
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	wantContent := injectionContent(t, []byte(srcWithNonEmptyProjectDefault), "CodebaseContext")
	gotContent := injectionContent(t, result.Output, "CodebaseContext")

	if !bytes.Equal(gotContent, wantContent) {
		t.Errorf("CodebaseContext content not byte-identical to source default:\nwant: %q\ngot:  %q",
			wantContent, gotContent)
	}
}

// TestProjectRegion_NewDeploy_NonEmptyDefault_RegionDefaultedAction asserts that when
// req.Deployed is nil and the source project region is non-empty, the RegionOutcome for that
// region has Action == RegionDefaulted (not RegionEmptied).
func TestProjectRegion_NewDeploy_NonEmptyDefault_RegionDefaultedAction(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithNonEmptyProjectDefault),
		Kind:   domain.ArtifactAgent,
		Key:    "project-region-default-test",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionDefaulted {
		t.Errorf("Action = %q; want %q", outcome.Action, transform.RegionDefaulted)
	}
}

// TestProjectRegion_NewDeploy_NonEmptyDefault_BytesEqualsContentLength asserts that when the
// source project region is non-empty and deployed for the first time, RegionOutcome.Bytes
// equals the byte length of the source content — truthful to what was actually written.
func TestProjectRegion_NewDeploy_NonEmptyDefault_BytesEqualsContentLength(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithNonEmptyProjectDefault),
		Kind:   domain.ArtifactAgent,
		Key:    "project-region-default-test",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}

	wantBytes := len(injectionContent(t, []byte(srcWithNonEmptyProjectDefault), "CodebaseContext"))
	if outcome.Bytes != wantBytes {
		t.Errorf("Bytes = %d; want %d (source content length)", outcome.Bytes, wantBytes)
	}
}

// TestProjectRegion_NewDeploy_NonEmptyDefault_ContentIsVerbatimUntrimmed asserts that the
// default content is written byte-for-byte without trimming — every leading and trailing byte
// of the author's default is preserved unchanged.
func TestProjectRegion_NewDeploy_NonEmptyDefault_ContentIsVerbatimUntrimmed(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithNonEmptyProjectDefault),
		Kind:   domain.ArtifactAgent,
		Key:    "project-region-default-test",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	srcContent := injectionContent(t, []byte(srcWithNonEmptyProjectDefault), "CodebaseContext")
	outContent := injectionContent(t, result.Output, "CodebaseContext")

	if !bytes.Equal(outContent, srcContent) {
		t.Errorf("output content is not byte-identical to source default (content was trimmed or modified):\nwant: %q\ngot:  %q",
			srcContent, outContent)
	}
}

// TestProjectRegion_NewDeploy_NonEmptyDefault_GapDefaultContentDeployedEmitted asserts that
// when req.Deployed is nil and the source project region is non-empty, a gap of kind
// GapDefaultContentDeployed is emitted for that region, not GapEmptyInjection.
func TestProjectRegion_NewDeploy_NonEmptyDefault_GapDefaultContentDeployedEmitted(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithNonEmptyProjectDefault),
		Kind:   domain.ArtifactAgent,
		Key:    "project-region-default-test",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapDefaultContentDeployed)
	if gap == nil {
		t.Fatalf("expected a GapDefaultContentDeployed gap; got: %v", result.Report.Gaps)
	}
	if gap.Subject != "CodebaseContext" {
		t.Errorf("GapDefaultContentDeployed.Subject = %q; want %q", gap.Subject, "CodebaseContext")
	}

	// GapEmptyInjection must NOT be emitted for a non-empty source default.
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapEmptyInjection && g.Subject == "CodebaseContext" {
			t.Errorf("GapEmptyInjection emitted for non-empty CodebaseContext — should be GapDefaultContentDeployed")
		}
	}
}

// TestProjectRegion_NewDeploy_EmptySource_RegionEmptiedAction asserts that when req.Deployed
// is nil and the source project region is empty, the region is cleared (RegionEmptied) and
// Bytes is 0 — behavior unchanged from before this stage.
func TestProjectRegion_NewDeploy_EmptySource_RegionEmptiedAction(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithEmptyProjectRegion),
		Kind:   domain.ArtifactAgent,
		Key:    "project-region-default-test",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionEmptied {
		t.Errorf("Action = %q; want %q (empty source region must be cleared — unchanged behavior)", outcome.Action, transform.RegionEmptied)
	}
	if outcome.Bytes != 0 {
		t.Errorf("Bytes = %d; want 0 (empty source region writes 0 bytes)", outcome.Bytes)
	}
}

// TestProjectRegion_NewDeploy_EmptySource_GapEmptyInjectionEmitted asserts that when
// req.Deployed is nil and the source project region is empty, GapEmptyInjection is emitted
// for that region — behavior unchanged from before this stage.
func TestProjectRegion_NewDeploy_EmptySource_GapEmptyInjectionEmitted(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithEmptyProjectRegion),
		Kind:   domain.ArtifactAgent,
		Key:    "project-region-default-test",
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
		t.Fatalf("expected a GapEmptyInjection gap; got: %v", result.Report.Gaps)
	}
	if gap.Subject != "CodebaseContext" {
		t.Errorf("GapEmptyInjection.Subject = %q; want %q", gap.Subject, "CodebaseContext")
	}
}

// TestProjectRegion_NewDeploy_WhitespaceOnlySource_TreatedAsEmpty asserts that a
// whitespace-only project region on new deployment is treated exactly as an empty region:
// Action is RegionEmptied, Bytes is 0, and GapEmptyInjection (not GapDefaultContentDeployed)
// is emitted. A whitespace-only body must never trigger the default-content deployment path.
func TestProjectRegion_NewDeploy_WhitespaceOnlySource_TreatedAsEmpty(t *testing.T) {
	req := transform.Request{
		Source: []byte(srcWithWhitespaceOnlyProjectDefault),
		Kind:   domain.ArtifactAgent,
		Key:    "project-region-default-test",
		Module: newFixtureModule(t),
		Model:  fixtureModel(),
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionEmptied {
		t.Errorf("Action = %q; want %q (whitespace-only must be treated as empty)", outcome.Action, transform.RegionEmptied)
	}
	if outcome.Bytes != 0 {
		t.Errorf("Bytes = %d; want 0 (whitespace-only region writes 0 bytes)", outcome.Bytes)
	}

	gap := findProjectGapOfKind(result, domain.GapEmptyInjection)
	if gap == nil {
		t.Fatalf("expected a GapEmptyInjection gap for whitespace-only source; got: %v", result.Report.Gaps)
	}
	if gap.Subject != "CodebaseContext" {
		t.Errorf("GapEmptyInjection.Subject = %q; want %q", gap.Subject, "CodebaseContext")
	}

	// GapDefaultContentDeployed must NOT be emitted for whitespace-only source.
	if defaultGap := findProjectGapOfKind(result, domain.GapDefaultContentDeployed); defaultGap != nil && defaultGap.Subject == "CodebaseContext" {
		t.Errorf("GapDefaultContentDeployed emitted for whitespace-only CodebaseContext — whitespace-only must not trigger default-content deployment")
	}
}

// ---------------------------------------------------------------------------
// T7.2: Update — region newly added to source, absent from deployed file
// ---------------------------------------------------------------------------

// TestProjectRegion_Update_RegionAbsent_NonEmptyDefault_ContentCarriedThrough asserts that
// when req.Deployed is non-nil but the project region is absent from the deployed file, and
// the source carries non-empty default content, that content appears in the output.
// "First deploy" applies per-region regardless of whether the run is a create or an update.
func TestProjectRegion_Update_RegionAbsent_NonEmptyDefault_ContentCarriedThrough(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithNonEmptyProjectDefault),
		Deployed: []byte(deployedWithoutCodebaseContext),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	wantContent := injectionContent(t, []byte(srcWithNonEmptyProjectDefault), "CodebaseContext")
	gotContent := injectionContent(t, result.Output, "CodebaseContext")

	if !bytes.Equal(gotContent, wantContent) {
		t.Errorf("CodebaseContext content not byte-identical to source default:\nwant: %q\ngot:  %q",
			wantContent, gotContent)
	}
}

// TestProjectRegion_Update_RegionAbsent_NonEmptyDefault_RegionDefaultedAction asserts that
// when a region is absent from the deployed file and the source carries non-empty default
// content, the RegionOutcome has Action == RegionDefaulted (not RegionAdded).
func TestProjectRegion_Update_RegionAbsent_NonEmptyDefault_RegionDefaultedAction(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithNonEmptyProjectDefault),
		Deployed: []byte(deployedWithoutCodebaseContext),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionDefaulted {
		t.Errorf("Action = %q; want %q (non-empty source default in newly-added region must be RegionDefaulted)", outcome.Action, transform.RegionDefaulted)
	}
	wantBytes := len(injectionContent(t, []byte(srcWithNonEmptyProjectDefault), "CodebaseContext"))
	if outcome.Bytes != wantBytes {
		t.Errorf("Bytes = %d; want %d (source content length)", outcome.Bytes, wantBytes)
	}
}

// TestProjectRegion_Update_RegionAbsent_NonEmptyDefault_GapDefaultContentDeployedEmitted
// asserts that when a region is absent from the deployed file and the source carries non-empty
// default content, a GapDefaultContentDeployed gap is emitted.
func TestProjectRegion_Update_RegionAbsent_NonEmptyDefault_GapDefaultContentDeployedEmitted(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithNonEmptyProjectDefault),
		Deployed: []byte(deployedWithoutCodebaseContext),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	gap := findProjectGapOfKind(result, domain.GapDefaultContentDeployed)
	if gap == nil {
		t.Fatalf("expected a GapDefaultContentDeployed gap; got: %v", result.Report.Gaps)
	}
	if gap.Subject != "CodebaseContext" {
		t.Errorf("GapDefaultContentDeployed.Subject = %q; want %q", gap.Subject, "CodebaseContext")
	}
}

// TestProjectRegion_Update_RegionAbsent_EmptySource_RegionAddedAction asserts that when a
// region is absent from the deployed file and the source region is empty, the behavior is
// unchanged: Action is RegionAdded (not RegionDefaulted), Bytes is 0, and no
// GapDefaultContentDeployed is emitted.
func TestProjectRegion_Update_RegionAbsent_EmptySource_RegionAddedAction(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithEmptyProjectRegion),
		Deployed: []byte(deployedWithoutCodebaseContext),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionAdded {
		t.Errorf("Action = %q; want %q (empty source default in newly-added region must remain RegionAdded)", outcome.Action, transform.RegionAdded)
	}
	if outcome.Bytes != 0 {
		t.Errorf("Bytes = %d; want 0", outcome.Bytes)
	}

	// GapDefaultContentDeployed must not be emitted for an empty newly-added region.
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapDefaultContentDeployed && g.Subject == "CodebaseContext" {
			t.Errorf("unexpected GapDefaultContentDeployed for empty newly-added region (RegionAdded path)")
		}
	}
}

// TestProjectRegion_Update_RegionAbsent_WhitespaceOnlySource_TreatedAsEmpty asserts that when
// a region is absent from the deployed file and the source carries only whitespace, the
// whitespace is treated as empty: Action is RegionAdded, Bytes is 0, and no
// GapDefaultContentDeployed is emitted.
func TestProjectRegion_Update_RegionAbsent_WhitespaceOnlySource_TreatedAsEmpty(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithWhitespaceOnlyProjectDefault),
		Deployed: []byte(deployedWithoutCodebaseContext),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionAdded {
		t.Errorf("Action = %q; want %q (whitespace-only source must be treated as empty on update)", outcome.Action, transform.RegionAdded)
	}
	if outcome.Bytes != 0 {
		t.Errorf("Bytes = %d; want 0", outcome.Bytes)
	}

	// GapDefaultContentDeployed must not be emitted for a whitespace-only newly-added region.
	for _, g := range result.Report.Gaps {
		if g.Kind == domain.GapDefaultContentDeployed && g.Subject == "CodebaseContext" {
			t.Errorf("unexpected GapDefaultContentDeployed for whitespace-only source on update — whitespace-only must be treated as empty")
		}
	}
}

// ---------------------------------------------------------------------------
// T7.3: Update — region already exists in the deployed file (regression pins)
// ---------------------------------------------------------------------------

// TestProjectRegion_Update_RegionPresent_ContentPreservedByteIdentically asserts that when
// the project region exists in the deployed file, its content is lifted verbatim into the
// output. This is the core FR-10 contract: the deployed file wins forever.
// This test pins existing behavior that must not be disrupted by Stage 7 changes.
func TestProjectRegion_Update_RegionPresent_ContentPreservedByteIdentically(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithEmptyProjectRegion),
		Deployed: []byte(deployedWithCodebaseContextContent),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("Action = %q; want %q", outcome.Action, transform.RegionPreserved)
	}

	wantContent := injectionContent(t, []byte(deployedWithCodebaseContextContent), "CodebaseContext")
	gotContent := injectionContent(t, result.Output, "CodebaseContext")

	if !bytes.Equal(gotContent, wantContent) {
		t.Errorf("CodebaseContext content not byte-identical to deployed:\nwant: %q\ngot:  %q",
			wantContent, gotContent)
	}
	if outcome.Bytes != len(wantContent) {
		t.Errorf("Bytes = %d; want %d (deployed content length)", outcome.Bytes, len(wantContent))
	}
}

// TestProjectRegion_Update_RegionPresent_SourceDefaultChangedStillUsesDeployed asserts that
// when the project region exists in the deployed file, the deployed content is preserved even
// when the source has since changed its default content. FR-10 — "deployed file wins forever"
// — is not weakened by source-side default changes; the source default is never consulted
// again once the region appears in the deployed file.
func TestProjectRegion_Update_RegionPresent_SourceDefaultChangedStillUsesDeployed(t *testing.T) {
	req := transform.Request{
		// The source has an updated default — different from what was originally deployed.
		Source:   []byte(srcWithChangedProjectDefault),
		Deployed: []byte(deployedWithCodebaseContextContent),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("Action = %q; want %q (deployed content must win over a changed source default)", outcome.Action, transform.RegionPreserved)
	}

	wantContent := injectionContent(t, []byte(deployedWithCodebaseContextContent), "CodebaseContext")
	gotContent := injectionContent(t, result.Output, "CodebaseContext")

	if !bytes.Equal(gotContent, wantContent) {
		t.Errorf("deployed content not preserved when source default changed:\nwant (deployed): %q\ngot:             %q",
			wantContent, gotContent)
	}

	// The updated source default must not appear in the output's CodebaseContext.
	updatedSrcDefault := injectionContent(t, []byte(srcWithChangedProjectDefault), "CodebaseContext")
	if bytes.Equal(gotContent, updatedSrcDefault) {
		t.Errorf("output contains updated source default instead of deployed content — FR-10 violated")
	}
}

// TestProjectRegion_Update_RegionPresent_EmptyDeployedContent_PreservedVerbatim asserts that
// when the project region exists in the deployed file but is empty (user cleared it), the
// empty state is preserved on update. The source's non-empty default must not re-populate a
// region the user deliberately cleared.
func TestProjectRegion_Update_RegionPresent_EmptyDeployedContent_PreservedVerbatim(t *testing.T) {
	req := transform.Request{
		// Source has non-empty default, but deployed content is empty.
		Source:   []byte(srcWithNonEmptyProjectDefault),
		Deployed: []byte(deployedWithEmptyCodebaseContext),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	outcome := findRegionOutcomeByName(result, "CodebaseContext")
	if outcome == nil {
		t.Fatalf("expected RegionOutcome for CodebaseContext; got: %v", result.Report.Regions)
	}
	if outcome.Action != transform.RegionPreserved {
		t.Errorf("Action = %q; want %q (empty deployed content must still be RegionPreserved)", outcome.Action, transform.RegionPreserved)
	}

	// The output must contain the deployed content (empty), not the source default.
	wantContent := injectionContent(t, []byte(deployedWithEmptyCodebaseContext), "CodebaseContext")
	gotContent := injectionContent(t, result.Output, "CodebaseContext")

	if !bytes.Equal(gotContent, wantContent) {
		t.Errorf("empty deployed content not preserved byte-identically:\nwant: %q\ngot:  %q",
			wantContent, gotContent)
	}
}

// TestProjectRegion_Update_RegionPresent_NoGapEmitted asserts that when the project region
// already exists in the deployed file, no gap is emitted for it — the content is preserved
// silently and requires no user action.
func TestProjectRegion_Update_RegionPresent_NoGapEmitted(t *testing.T) {
	req := transform.Request{
		Source:   []byte(srcWithEmptyProjectRegion),
		Deployed: []byte(deployedWithCodebaseContextContent),
		Kind:     domain.ArtifactAgent,
		Key:      "project-region-default-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// No injection-related gap should be emitted for a successfully preserved region.
	for _, gap := range result.Report.Gaps {
		switch gap.Subject {
		case "CodebaseContext":
			t.Errorf("unexpected gap emitted for preserved CodebaseContext region: kind=%q detail=%q",
				gap.Kind, gap.Detail)
		}
	}
}
