package transform_test

// bundle_region_test.go covers bundle region delivery in the transform:
//
//   T6.3 — Verbatim filling of bundle regions:
//     - A subagent source file with the five bundle regions filled byte-for-byte from the
//       bundle content for its role. The test asserts the exact bytes, verifying "verbatim"
//       rather than just "non-empty".
//     - A bundle block present in the bundle but absent from the agent source is never an
//       error — the absent region simply does not appear in the output.
//
//   T6.4 — Hard error when role has no matching block:
//     - A bundle-sourced region present in the source, with no block in the bundle matching
//       the file's role, causes Apply to return an error wrapping ErrBundleBlockMissingForRole.
//     - No partial output is returned on this error.
//
//   T6.5 — Hard error for populated deployed region in source file:
//     - A source file carrying non-empty content between a [[DEPLOYED:]] region's tags causes
//       Apply to return an error wrapping ErrSourceDeployedRegionNotEmpty.
//     - No partial output is returned on this error.
//
//   T6.6 — mosaic_bundle_version stamp in deployed frontmatter; orchestrator receives no bundle:
//     - A subagent transformed with a populated bundle carries the bundle's version in the
//       deployed file's frontmatter under the key `mosaic_bundle_version`.
//     - The stamp value equals BundleContent.Version verbatim.
//     - An orchestrator-role source with no bundle regions, transformed with a bundle whose
//       blocks all apply to subagent, produces no error and no mosaic_bundle_version stamp.
//
//   Ordering:
//     - [[INJECTION:]] regions are resolved AFTER all [[DEPLOYED:]] bundle regions are filled,
//       so a regenerated injection immediately following a bundle region does not discard the
//       freshly placed content.

import (
	"bytes"
	"errors"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

// bundleBlockContent returns distinct, identifiable content for each of the five bundle
// targets. Using unique phrases per target lets tests assert that the correct block —
// and only the correct block — appears in the corresponding region.
func bundleBlockContent(target string) []byte {
	return []byte("### " + target + "\n\nThis is the verbatim content for " + target + ".\n")
}

// fixtureBundle returns a BundleContent carrying all five canonical subagent blocks,
// each with identifiable content, and a known version string.
func fixtureBundle(version string) domain.BundleContent {
	targets := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	blocks := make([]domain.BundleBlock, 0, len(targets))
	for _, target := range targets {
		blocks = append(blocks, domain.BundleBlock{
			Name:        target + ":Subagent",
			Target:      target,
			AppliesTo:   "subagent",
			SpecifiedIn: "Development/Designs/AgentTemplateArchitecture.md",
			Content:     bundleBlockContent(target),
		})
	}
	return domain.BundleContent{
		Version: version,
		Blocks:  blocks,
	}
}

// sourceWithAllBundleRegions is a minimal valid subagent source that declares all five
// canonical bundle-sourced [[DEPLOYED:]] regions, each empty (as required for source files).
const sourceWithAllBundleRegions = `---
id: 42
version: 1.0.0
name: bundle-test
description: Agent for bundle region testing
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

[[SECTION:Identity]]
## Identity

Bundle test agent.

[[DEPLOYED:AuthorityHierarchy]]
[[/DEPLOYED:AuthorityHierarchy]]

[[DEPLOYED:ClosingProcedure]]
[[/DEPLOYED:ClosingProcedure]]

[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]

[[SECTION:Constraints]]
## Constraints

[[DEPLOYED:ProtocolConstraints]]
[[/DEPLOYED:ProtocolConstraints]]

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]

[[/SECTION:Constraints]]

[[SECTION:ErrorHandling]]
## Error Handling

[[DEPLOYED:ErrorHandlingCommon]]
[[/DEPLOYED:ErrorHandlingCommon]]

[[/SECTION:ErrorHandling]]

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy

[[DEPLOYED:ExecutionPhilosophyCommon]]
[[/DEPLOYED:ExecutionPhilosophyCommon]]

[[INJECTION:ContextLimits]]
[[/INJECTION:ContextLimits]]

[[/SECTION:ExecutionPhilosophy]]
`

// sourceWithOnlyTwoBundleRegions is a subagent source with only two of the five bundle
// regions. Used to verify that absent regions are not an error — only present regions
// are filled.
const sourceWithOnlyTwoBundleRegions = `---
id: 43
version: 1.0.0
name: partial-bundle-test
description: Agent for partial bundle region testing
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

[[SECTION:Identity]]
## Identity

Partial bundle test agent.

[[DEPLOYED:AuthorityHierarchy]]
[[/DEPLOYED:AuthorityHierarchy]]

[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]

[[SECTION:ErrorHandling]]
## Error Handling

[[DEPLOYED:ErrorHandlingCommon]]
[[/DEPLOYED:ErrorHandlingCommon]]

[[/SECTION:ErrorHandling]]
`

// sourceOrchestratorNoBundleRegions is a minimal orchestrator source with no bundle-sourced
// [[DEPLOYED:]] regions. Used to verify that an orchestrator-role file produces no error and
// no mosaic_bundle_version stamp.
const sourceOrchestratorNoBundleRegions = `---
version: 1.0.0
name: orchestrator
description: Orchestrator agent with no bundle regions
role: orchestrator
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

[[SECTION:Identity]]
## Identity

Orchestrator agent.

[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]
`

// ---------------------------------------------------------------------------
// T6.3 — Verbatim bundle region filling
// ---------------------------------------------------------------------------

// TestBundle_SubagentSource_BundleRegionsFilledVerbatim verifies that when a subagent source
// declares the five canonical bundle-sourced regions, each region is filled byte-for-byte
// with the matching block from the bundle. "Verbatim" means the exact bytes — not trimmed,
// not reformatted, not preceded by a version marker.
func TestBundle_SubagentSource_BundleRegionsFilledVerbatim(t *testing.T) {
	bundle := fixtureBundle("1.0.0")
	req := transform.Request{
		Source:   []byte(sourceWithAllBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   bundle,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Verify that each bundle region's content is byte-identical to the fixture block.
	targets := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}

	doc, parseErr := docformat.Parse(result.Output)
	if parseErr != nil {
		t.Fatalf("parse output: %v", parseErr)
	}

	for _, target := range targets {
		node, ok := doc.Body().Deployed(target)
		if !ok {
			t.Errorf("[[DEPLOYED:%s]] region absent from output", target)
			continue
		}
		got := node.Content()
		want := bundleBlockContent(target)
		if !bytes.Equal(got, want) {
			t.Errorf("[[DEPLOYED:%s]] region content: want %q, got %q; "+
				"bundle regions must be filled byte-for-byte from the bundle block", target, want, got)
		}
	}
}

// TestBundle_AbsentRegionIsNotAnError verifies that when a bundle block has no matching
// region in the agent source, Apply does not return an error. The absent region simply does
// not appear in the output — this is the documented "never an error" case.
func TestBundle_AbsentRegionIsNotAnError(t *testing.T) {
	bundle := fixtureBundle("1.0.0")

	// sourceWithOnlyTwoBundleRegions declares only AuthorityHierarchy and ErrorHandlingCommon;
	// the bundle carries all five. The missing three must not cause an error.
	req := transform.Request{
		Source:   []byte(sourceWithOnlyTwoBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "partial-bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   bundle,
	}

	_, err := transform.Apply(req)

	if err != nil {
		t.Errorf("Apply returned error when bundle block has no matching region in source: %v; "+
			"a bundle block with no matching region must not be an error", err)
	}
}

// TestBundle_AbsentRegionFilled_ContentIsVerbatim verifies that the two regions present in
// sourceWithOnlyTwoBundleRegions are each filled with the correct verbatim content, even
// when other bundle blocks in the fixture have no matching region.
func TestBundle_AbsentRegionFilled_ContentIsVerbatim(t *testing.T) {
	bundle := fixtureBundle("1.0.0")
	req := transform.Request{
		Source:   []byte(sourceWithOnlyTwoBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "partial-bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   bundle,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, parseErr := docformat.Parse(result.Output)
	if parseErr != nil {
		t.Fatalf("parse output: %v", parseErr)
	}

	for _, target := range []string{"AuthorityHierarchy", "ErrorHandlingCommon"} {
		node, ok := doc.Body().Deployed(target)
		if !ok {
			t.Errorf("[[DEPLOYED:%s]] absent from output even though it was in the source", target)
			continue
		}
		got := node.Content()
		want := bundleBlockContent(target)
		if !bytes.Equal(got, want) {
			t.Errorf("[[DEPLOYED:%s]] content mismatch: want %q, got %q",
				target, want, got)
		}
	}
}

// TestBundle_RegionReport_BundleFilledAction verifies that bundle-filled regions appear in
// Report.Regions with the RegionBundleFilled action. The report is the audit trail that
// operators use to verify what happened; an incorrect action would mislead them.
//
// The test explicitly asserts that all five expected bundle targets appear in the report.
// Without this completeness check, an implementation that forgets to append any outcome for
// bundle-sourced regions would cause the assertion loop to iterate over zero entries and
// report a false PASS.
func TestBundle_RegionReport_BundleFilledAction(t *testing.T) {
	bundle := fixtureBundle("1.0.0")
	req := transform.Request{
		Source:   []byte(sourceWithAllBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   bundle,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Build a lookup map from the report so we can assert each expected target individually.
	// Iterating the report and filtering is insufficient: if no bundle outcomes are appended
	// (no InjectionBundle case in processRegions), the loop body never runs and all assertions
	// are silently skipped — a vacuous pass.
	reportByName := make(map[string]transform.RegionOutcome, len(result.Report.Regions))
	for _, outcome := range result.Report.Regions {
		reportByName[outcome.Name] = outcome
	}

	bundleTargets := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}

	// Assert each expected bundle target is present in the report with the correct action and class.
	for _, target := range bundleTargets {
		outcome, found := reportByName[target]
		if !found {
			t.Errorf("Report.Regions does not contain an entry for %q; "+
				"every bundle-filled region must appear in the report", target)
			continue
		}
		if outcome.Action != transform.RegionBundleFilled {
			t.Errorf("Report.Regions[%q].Action = %q, want RegionBundleFilled (%q)",
				target, outcome.Action, transform.RegionBundleFilled)
		}
		if outcome.Class != domain.InjectionBundle {
			t.Errorf("Report.Regions[%q].Class = %q, want InjectionBundle (%q)",
				target, outcome.Class, domain.InjectionBundle)
		}
	}
}

// ---------------------------------------------------------------------------
// T6.4 — Hard error: region with no role-matching block
// ---------------------------------------------------------------------------

// TestBundle_RegionWithNoMatchingBlock_ReturnsErrBundleBlockMissingForRole verifies that
// when a bundle-sourced region is present in the source but the bundle has no block matching
// the file's role, Apply returns an error wrapping ErrBundleBlockMissingForRole. Deploying
// it empty would ship an agent that appears complete and instructs nothing.
func TestBundle_RegionWithNoMatchingBlock_ReturnsErrBundleBlockMissingForRole(t *testing.T) {
	// Bundle with only orchestrator blocks — no subagent blocks.
	orchestratorOnlyBundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:      "AuthorityHierarchy:Orchestrator",
				Target:    "AuthorityHierarchy",
				AppliesTo: "orchestrator",
				Content:   []byte("Orchestrator authority hierarchy content.\n"),
			},
		},
	}

	// Source is a subagent with an AuthorityHierarchy region. The bundle has no subagent
	// block for AuthorityHierarchy — this is the hard-error case.
	req := transform.Request{
		Source:   []byte(sourceWithOnlyTwoBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "partial-bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   orchestratorOnlyBundle,
	}

	_, err := transform.Apply(req)

	if err == nil {
		t.Fatal("Apply returned nil error when no bundle block matches the file's role; " +
			"a missing role-matched block must be a hard error (ErrBundleBlockMissingForRole)")
	}
	if !errors.Is(err, transform.ErrBundleBlockMissingForRole) {
		t.Errorf("error = %v; want an error wrapping transform.ErrBundleBlockMissingForRole", err)
	}
}

// TestBundle_RegionWithNoMatchingBlock_NoPartialOutput verifies that when Apply fails due to
// a missing role-matched block, the returned Result is the zero value — no partial output
// is returned. The transform is all-or-nothing.
func TestBundle_RegionWithNoMatchingBlock_NoPartialOutput(t *testing.T) {
	emptyBundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks:  nil, // no blocks at all
	}

	req := transform.Request{
		Source:   []byte(sourceWithOnlyTwoBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "partial-bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   emptyBundle,
	}

	result, err := transform.Apply(req)

	if err == nil {
		t.Fatal("Apply: expected error for absent role-matched block, got nil")
	}
	if result.Output != nil {
		t.Errorf("Apply returned non-nil Output on error; partial output must not be returned (got %d bytes)",
			len(result.Output))
	}
}

// ---------------------------------------------------------------------------
// T6.5 — Hard error: populated deployed region in source file
// ---------------------------------------------------------------------------

// sourceWithPopulatedDeployedRegion is a source file that carries non-empty content inside
// a [[DEPLOYED:AuthorityHierarchy]] region. Source regions must be empty by definition;
// content here is either a hand-edit about to be discarded or a deployed file committed
// as source.
const sourceWithPopulatedDeployedRegion = `---
id: 44
version: 1.0.0
name: populated-region-test
description: Agent for populated region testing
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

[[SECTION:Identity]]
## Identity

Populated region test agent.

[[DEPLOYED:AuthorityHierarchy]]
THIS CONTENT SHOULD NOT BE HERE IN A SOURCE FILE.
[[/DEPLOYED:AuthorityHierarchy]]

[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]
`

// TestBundle_SourceFileWithPopulatedDeployedRegion_ReturnsErrSourceDeployedRegionNotEmpty
// verifies that when a source file carries non-empty content inside a [[DEPLOYED:]] region,
// Apply returns an error wrapping ErrSourceDeployedRegionNotEmpty. A populated source region
// is either a hand-edit about to be discarded or a deployed file mistakenly committed as
// source — both are hard errors.
func TestBundle_SourceFileWithPopulatedDeployedRegion_ReturnsErrSourceDeployedRegionNotEmpty(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithPopulatedDeployedRegion),
		Kind:     domain.ArtifactAgent,
		Key:      "populated-region-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   fixtureBundle("1.0.0"),
	}

	_, err := transform.Apply(req)

	if err == nil {
		t.Fatal("Apply returned nil error when source file carries a populated [[DEPLOYED:]] region; " +
			"this must be a hard error (ErrSourceDeployedRegionNotEmpty)")
	}
	if !errors.Is(err, transform.ErrSourceDeployedRegionNotEmpty) {
		t.Errorf("error = %v; want an error wrapping transform.ErrSourceDeployedRegionNotEmpty", err)
	}
}

// TestBundle_SourceFileWithPopulatedDeployedRegion_NoPartialOutput verifies that Apply
// returns no partial output when it rejects a populated source deployed region.
func TestBundle_SourceFileWithPopulatedDeployedRegion_NoPartialOutput(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithPopulatedDeployedRegion),
		Kind:     domain.ArtifactAgent,
		Key:      "populated-region-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   fixtureBundle("1.0.0"),
	}

	result, err := transform.Apply(req)

	if err == nil {
		t.Fatal("Apply: expected error, got nil")
	}
	if result.Output != nil {
		t.Errorf("Apply returned non-nil Output on error; no partial output must be returned (got %d bytes)",
			len(result.Output))
	}
}

// ---------------------------------------------------------------------------
// T6.6 — mosaic_bundle_version stamp and orchestrator exemption
// ---------------------------------------------------------------------------

// TestBundle_SubagentOutput_CarriesBundleVersionStamp verifies that the deployed output
// frontmatter for a subagent carries a `mosaic_bundle_version` key whose value equals
// BundleContent.Version verbatim. The stamp enables staleness detection on subsequent runs.
//
// This test FAILS until I4.2 updates the bundle stamp write path to use the prefixed key name.
func TestBundle_SubagentOutput_CarriesBundleVersionStamp(t *testing.T) {
	const bundleVersion = "1.0.0"
	bundle := fixtureBundle(bundleVersion)

	req := transform.Request{
		Source:   []byte(sourceWithAllBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "bundle-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   bundle,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, parseErr := docformat.Parse(result.Output)
	if parseErr != nil {
		t.Fatalf("parse output: %v", parseErr)
	}

	stampVal, ok := doc.Frontmatter().Get("mosaic_bundle_version")
	if !ok {
		t.Fatal("deployed output frontmatter does not contain `mosaic_bundle_version`; " +
			"the bundle version must be stamped under the prefixed key into every deployed subagent")
	}
	if stampVal.Scalar != bundleVersion {
		t.Errorf("frontmatter `mosaic_bundle_version` = %q, want %q (BundleContent.Version verbatim)",
			stampVal.Scalar, bundleVersion)
	}
}

// TestBundle_SubagentOutput_StampValueMatchesBundleVersion verifies that when multiple
// bundle versions are used across runs, the stamp value reflects the specific version
// supplied in the request — not a hardcoded default.
//
// This test FAILS until I4.2 updates the bundle stamp write path to use the prefixed key name.
func TestBundle_SubagentOutput_StampValueMatchesBundleVersion(t *testing.T) {
	cases := []string{"1.0.0", "1.1.0", "2.0.0", "0.1.0-alpha"}
	for _, version := range cases {
		t.Run("version="+version, func(t *testing.T) {
			bundle := fixtureBundle(version)
			req := transform.Request{
				Source:   []byte(sourceWithAllBundleRegions),
				Kind:     domain.ArtifactAgent,
				Key:      "bundle-test",
				Module:   newFixtureModule(t),
				Model:    fixtureModel(),
				Scope:    domain.ScopeProject,
				Role:     domain.RoleSubagent,
				Protocol: fixtureProtocol("1.9"),
				Bundle:   bundle,
			}

			result, err := transform.Apply(req)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			doc, parseErr := docformat.Parse(result.Output)
			if parseErr != nil {
				t.Fatalf("parse output: %v", parseErr)
			}

			stampVal, ok := doc.Frontmatter().Get("mosaic_bundle_version")
			if !ok {
				t.Fatal("deployed output frontmatter does not contain `mosaic_bundle_version`; " +
					"the bundle stamp must use the prefixed field name")
			}
			if stampVal.Scalar != version {
				t.Errorf("`mosaic_bundle_version` = %q, want %q", stampVal.Scalar, version)
			}
		})
	}
}

// TestBundle_OrchestratorRoleWithNoBundleRegions_ReturnsNoError verifies that an
// orchestrator-role source file with no bundle-sourced [[DEPLOYED:]] regions, transformed
// with a bundle whose blocks all apply to subagent, produces no error. The orchestrator is
// exempt from bundle filling because it carries no bundle regions and the bundle carries no
// orchestrator blocks. A bundle block with no matching region must never be an error.
func TestBundle_OrchestratorRoleWithNoBundleRegions_ReturnsNoError(t *testing.T) {
	// Bundle carries only subagent blocks; orchestrator source has no bundle regions.
	bundle := fixtureBundle("1.0.0")
	req := transform.Request{
		Source:   []byte(sourceOrchestratorNoBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "orchestrator",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleOrchestrator,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   bundle,
	}

	_, err := transform.Apply(req)

	if err != nil {
		t.Errorf("Apply returned error for orchestrator with no bundle regions: %v; "+
			"an orchestrator-role file must deploy without error when the bundle has no orchestrator blocks",
			err)
	}
}

// TestBundle_OrchestratorRoleWithNoBundleRegions_NoVersionStamp verifies that the deployed
// output for an orchestrator with no bundle regions does NOT carry a `mosaic_bundle_version`
// frontmatter stamp. The stamp is written only when the agent's role receives bundle blocks;
// an orchestrator that receives none must remain untouched.
func TestBundle_OrchestratorRoleWithNoBundleRegions_NoVersionStamp(t *testing.T) {
	bundle := fixtureBundle("1.0.0")
	req := transform.Request{
		Source:   []byte(sourceOrchestratorNoBundleRegions),
		Kind:     domain.ArtifactAgent,
		Key:      "orchestrator",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleOrchestrator,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   bundle,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	doc, parseErr := docformat.Parse(result.Output)
	if parseErr != nil {
		t.Fatalf("parse output: %v", parseErr)
	}

	if _, ok := doc.Frontmatter().Get("mosaic_bundle_version"); ok {
		t.Error("deployed orchestrator output carries `mosaic_bundle_version` stamp; " +
			"an orchestrator-role file that receives no bundle blocks must not receive the stamp")
	}
}

// ---------------------------------------------------------------------------
// Ordering: injection resolution runs last
// ---------------------------------------------------------------------------

// sourceWithBundleAndInjection is an agent source where an [[INJECTION:]] region immediately
// follows a [[DEPLOYED:AuthorityHierarchy]] bundle region inside the same section. This tests
// the ordering contract: injection resolution must run AFTER all deployed bundle regions are
// filled, so the freshly placed bundle content is not discarded when the adjacent injection is
// processed.
const sourceWithBundleAndInjection = `---
id: 45
version: 1.0.0
name: order-test
description: Agent for ordering test
role: subagent
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

[[SECTION:Identity]]
## Identity

Order test agent.

[[DEPLOYED:AuthorityHierarchy]]
[[/DEPLOYED:AuthorityHierarchy]]

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]

[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]
`

// TestBundle_InjectionResolvesAfterBundleRegions verifies that in a source document where an
// [[INJECTION:]] region immediately follows a bundle-filled [[DEPLOYED:]] region, the bundle
// content placed in the deployed region is not overwritten or discarded during injection
// resolution. This pins the ordering contract from the design.
func TestBundle_InjectionResolvesAfterBundleRegions(t *testing.T) {
	bundle := domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:      "AuthorityHierarchy:Subagent",
				Target:    "AuthorityHierarchy",
				AppliesTo: "subagent",
				Content:   []byte("### Authority Hierarchy\n\nDo not overwrite this.\n"),
			},
		},
	}

	req := transform.Request{
		Source:   []byte(sourceWithBundleAndInjection),
		Kind:     domain.ArtifactAgent,
		Key:      "order-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleSubagent,
		Protocol: fixtureProtocol("1.9"),
		Bundle:   bundle,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The AuthorityHierarchy region must contain the bundle block content.
	doc, parseErr := docformat.Parse(result.Output)
	if parseErr != nil {
		t.Fatalf("parse output: %v", parseErr)
	}

	node, ok := doc.Body().Deployed("AuthorityHierarchy")
	if !ok {
		t.Fatal("[[DEPLOYED:AuthorityHierarchy]] region absent from output")
	}

	// The bundle content must be intact — the adjacent injection must not have discarded it.
	if !bytes.Contains(node.Content(), []byte("Do not overwrite this")) {
		t.Errorf("[[DEPLOYED:AuthorityHierarchy]] content was discarded or overwritten; "+
			"injection resolution must run AFTER bundle region filling.\ncontent: %q",
			node.Content())
	}

	// The report must show that the bundle region was filled BEFORE the injection was added.
	bundleIdx := -1
	injectionIdx := -1
	for i, r := range result.Report.Regions {
		switch r.Name {
		case "AuthorityHierarchy":
			bundleIdx = i
		case "IdentityExtension":
			injectionIdx = i
		}
	}
	if bundleIdx == -1 {
		t.Error("AuthorityHierarchy not in Report.Regions")
	}
	if injectionIdx == -1 {
		t.Error("IdentityExtension not in Report.Regions")
	}
	if bundleIdx != -1 && injectionIdx != -1 && bundleIdx > injectionIdx {
		t.Errorf("AuthorityHierarchy (index %d) appears after IdentityExtension (index %d) in Report.Regions; "+
			"bundle regions must be processed in document order, before injection resolution",
			bundleIdx, injectionIdx)
	}
}

