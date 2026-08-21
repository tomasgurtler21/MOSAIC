package transform_test

// injection_version_tag_test.go verifies that harness injection regions carry a version
// attribute on their opening XML tag (Stage 2: injection version relocation to tag attributes).
//
//   T2.1 — Non-orchestrator harness injection region version tag:
//     After transform.Apply on a non-orchestrator agent, InjectionHarness-class regions
//     (e.g. HarnessConstraints) carry a version attribute matching req.InjectionsVersion.
//     Verified via docformat.Parse + Node.Version() on the output.
//
//   T2.2 — Orchestrator harness injection region version tag (role-conditional):
//     After transform.Apply when req.Key == "orchestrator", harness-class regions carry
//     a version attribute matching req.OrchestratorInjectionsVersion (not InjectionsVersion).
//
//   T2.3 — Injection version fields absent from frontmatter:
//     mosaic_injections_version and mosaic_orchestrator_injections_version are absent from
//     frontmatter in the transform output for all agents (both create and update scenarios).
//
//   T2.4 — Backwards-compatible migration relocation path:
//     When a deployed file has mosaic_injections_version or mosaic_orchestrator_injections_version
//     as frontmatter fields (pre-migration), after Update the fields are gone from frontmatter
//     AND the version appears on the tag attribute instead.
//
// All tests in this file FAIL until the corresponding Stage 2 implementation tasks are complete.

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Shared fixtures for injection version tag tests
// ---------------------------------------------------------------------------

// sourceWithHarnessInjectionRegion is a minimal generic agent source that declares a
// HarnessConstraints managed region. The fixture module (fixture.yaml) provides non-empty
// content for this region via its injections list, ensuring the RegionFilled path is
// exercised (SetVersion is only called when content is non-empty).
const sourceWithHarnessInjectionRegion = `---
id: 70
version: 2.0.0
name: injection-version-tag-test
description: Agent for testing injection version tag attributes
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

<Identity type="core">
# InjectionVersionTagTest Agent

You are the InjectionVersionTagTest agent.

<HarnessConstraints type="managed">
</HarnessConstraints>
</Identity>
`

// preTagMigrationDeployed is a pre-Stage 2 deployed agent file that carries injection
// version values as frontmatter fields. It simulates a file deployed before the tag
// relocation. After transform.Apply (Update scenario), these frontmatter fields must be
// absent and the version must appear on the HarnessConstraints tag instead.
const preTagMigrationDeployed = `---
mosaic_id: 70
mosaic_version: 1.9.0
mosaic_harness_version: 3.0.0
mosaic_injections_version: 0.9.0
mosaic_orchestrator_injections_version: 0.8.0
description: Agent for testing injection version tag attributes
mode: subagent
model: claude/claude-sonnet
tools: [read-file]
---

<Identity type="core">
# InjectionVersionTagTest Agent

You are the InjectionVersionTagTest agent.

<HarnessConstraints type="managed">
Previously deployed harness constraint content.
</HarnessConstraints>
</Identity>
`

// applyWithHarnessRegion is a helper that calls transform.Apply on
// sourceWithHarnessInjectionRegion with the given key, InjectionsVersion, and
// OrchestratorInjectionsVersion. Returns the parsed output document and result.
// Fails the test on any Apply or Parse error.
func applyWithHarnessRegion(t *testing.T, key, injectionsVersion, orchestratorInjectionsVersion string, deployed []byte) (*docformat.Document, transform.Result) {
	t.Helper()
	req := transform.Request{
		Source:                        []byte(sourceWithHarnessInjectionRegion),
		Kind:                          domain.ArtifactAgent,
		Key:                           key,
		Module:                        newFixtureModule(t),
		Model:                         fixtureModel(),
		Scope:                         domain.ScopeProject,
		Deployed:                      deployed,
		InjectionsVersion:             injectionsVersion,
		OrchestratorInjectionsVersion: orchestratorInjectionsVersion,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("docformat.Parse output: %v", err)
	}
	return doc, result
}

// findHarnessConstraintsNode parses the output doc and returns the HarnessConstraints
// deployed region node. Fails the test if the region is absent.
func findHarnessConstraintsNode(t *testing.T, doc *docformat.Document) *docformat.Node {
	t.Helper()
	node, ok := doc.Body().Deployed("HarnessConstraints")
	if !ok {
		t.Fatal("<HarnessConstraints type=\"managed\"> region absent from output; cannot verify version attribute")
	}
	return node
}

// ---------------------------------------------------------------------------
// T2.1 — Non-orchestrator: harness region carries InjectionsVersion on tag
// ---------------------------------------------------------------------------

// TestHarnessRegionVersion_NonOrchestrator_CarriesVersionAttribute verifies that after
// transform.Apply on a non-orchestrator agent (key != "orchestrator"), the HarnessConstraints
// region's opening tag carries a version attribute matching req.InjectionsVersion. This is the
// primary assertion for Stage 2's applyHarnessRegion SetVersion call.
//
// This test FAILS until I2.1 adds node.SetVersion(injVersion) in applyHarnessRegion.
func TestHarnessRegionVersion_NonOrchestrator_CarriesVersionAttribute(t *testing.T) {
	const wantVersion = "1.2.0"
	doc, _ := applyWithHarnessRegion(t, "injection-version-tag-test", wantVersion, "99.0.0", nil)

	node := findHarnessConstraintsNode(t, doc)
	if got := node.Version(); got != wantVersion {
		t.Errorf("HarnessConstraints version attribute: want %q, got %q; "+
			"applyHarnessRegion must call node.SetVersion(req.InjectionsVersion) for non-orchestrator agents",
			wantVersion, got)
	}
}

// TestHarnessRegionVersion_NonOrchestrator_UsesInjectionsVersionNotOrchestratorVersion verifies
// that for a non-orchestrator agent, the harness region tag version matches InjectionsVersion
// and NOT OrchestratorInjectionsVersion. The role-conditional selection must pick InjectionsVersion
// when req.Key != "orchestrator".
//
// This test FAILS until I2.1 implements the role-conditional selection.
func TestHarnessRegionVersion_NonOrchestrator_UsesInjectionsVersionNotOrchestratorVersion(t *testing.T) {
	const injectionsVersion = "1.2.0"
	const orchestratorVersion = "5.9.0"
	doc, _ := applyWithHarnessRegion(t, "some-worker-agent", injectionsVersion, orchestratorVersion, nil)

	node := findHarnessConstraintsNode(t, doc)
	got := node.Version()
	if got == orchestratorVersion {
		t.Errorf("HarnessConstraints version attribute = %q (OrchestratorInjectionsVersion); "+
			"non-orchestrator agents must use InjectionsVersion (%q), not OrchestratorInjectionsVersion",
			got, injectionsVersion)
	}
	if got != injectionsVersion {
		t.Errorf("HarnessConstraints version attribute: want %q (InjectionsVersion), got %q",
			injectionsVersion, got)
	}
}

// TestHarnessRegionVersion_NonOrchestrator_VersionRoundTrips verifies that the version
// attribute written by applyHarnessRegion is readable via Node.Version() — i.e., SetVersion
// and Version() form a correct round-trip on the serialised output. The output is parsed
// independently of the write path to confirm the attribute survives serialization.
//
// This test FAILS until I2.1 adds the SetVersion call and I2.4 ensures the fields are
// populated by the app layer.
func TestHarnessRegionVersion_NonOrchestrator_VersionRoundTrips(t *testing.T) {
	cases := []string{"1.2.0", "2.0.0", "10.3.1"}
	for _, ver := range cases {
		t.Run("version="+ver, func(t *testing.T) {
			doc, _ := applyWithHarnessRegion(t, "round-trip-agent", ver, "ignored", nil)
			node := findHarnessConstraintsNode(t, doc)
			if got := node.Version(); got != ver {
				t.Errorf("round-trip: HarnessConstraints version attribute: want %q, got %q", ver, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// T2.2 — Orchestrator: harness region carries OrchestratorInjectionsVersion on tag
// ---------------------------------------------------------------------------

// TestHarnessRegionVersion_Orchestrator_CarriesOrchestratorVersionAttribute verifies that
// after transform.Apply when req.Key == "orchestrator", the HarnessConstraints region's
// opening tag carries a version attribute matching req.OrchestratorInjectionsVersion.
//
// This test FAILS until I2.1 implements the req.Key == "orchestrator" branch in applyHarnessRegion.
func TestHarnessRegionVersion_Orchestrator_CarriesOrchestratorVersionAttribute(t *testing.T) {
	const injectionsVersion = "1.2.0"
	const orchestratorVersion = "2.7.0"
	doc, _ := applyWithHarnessRegion(t, "orchestrator", injectionsVersion, orchestratorVersion, nil)

	node := findHarnessConstraintsNode(t, doc)
	if got := node.Version(); got != orchestratorVersion {
		t.Errorf("HarnessConstraints version attribute for orchestrator: want %q (OrchestratorInjectionsVersion), got %q; "+
			"applyHarnessRegion must call node.SetVersion(req.OrchestratorInjectionsVersion) when req.Key == \"orchestrator\"",
			orchestratorVersion, got)
	}
}

// TestHarnessRegionVersion_Orchestrator_DoesNotUseInjectionsVersion verifies that for an
// orchestrator agent (req.Key == "orchestrator"), the harness region tag version does NOT
// match InjectionsVersion and DOES match OrchestratorInjectionsVersion. Both assertions are
// required: the negative assertion alone is trivially satisfied before implementation because
// node.Version() returns "" which != InjectionsVersion; the positive assertion ensures the
// test fails in the RED phase and only passes once the role-conditional branch is implemented.
//
// This test FAILS until I2.1 implements the role-conditional selection.
func TestHarnessRegionVersion_Orchestrator_DoesNotUseInjectionsVersion(t *testing.T) {
	const injectionsVersion = "1.2.0"
	const orchestratorVersion = "2.7.0"
	doc, _ := applyWithHarnessRegion(t, "orchestrator", injectionsVersion, orchestratorVersion, nil)

	node := findHarnessConstraintsNode(t, doc)
	got := node.Version()
	if got == injectionsVersion {
		t.Errorf("HarnessConstraints version attribute for orchestrator = %q (InjectionsVersion); "+
			"orchestrator agents must use OrchestratorInjectionsVersion (%q), not InjectionsVersion",
			got, orchestratorVersion)
	}
	if got != orchestratorVersion {
		t.Errorf("HarnessConstraints version attribute for orchestrator: want %q (OrchestratorInjectionsVersion), got %q; "+
			"applyHarnessRegion must call node.SetVersion(req.OrchestratorInjectionsVersion) when req.Key == \"orchestrator\"",
			orchestratorVersion, got)
	}
}

// TestHarnessRegionVersion_OrchestratorVsNonOrchestratorDiffer verifies that the same source
// document applied with req.Key == "orchestrator" vs a non-orchestrator key produces different
// version attributes on the harness region. This is the cross-contamination guard: distinct
// InjectionsVersion and OrchestratorInjectionsVersion values must remain isolated to their
// respective agent roles.
//
// This test FAILS until I2.1 implements the role-conditional version selection.
func TestHarnessRegionVersion_OrchestratorVsNonOrchestratorDiffer(t *testing.T) {
	const injectionsVersion = "1.2.0"
	const orchestratorVersion = "2.7.0"

	orchDoc, _ := applyWithHarnessRegion(t, "orchestrator", injectionsVersion, orchestratorVersion, nil)
	workerDoc, _ := applyWithHarnessRegion(t, "some-worker", injectionsVersion, orchestratorVersion, nil)

	orchNode := findHarnessConstraintsNode(t, orchDoc)
	workerNode := findHarnessConstraintsNode(t, workerDoc)

	if orchNode.Version() == workerNode.Version() {
		t.Errorf("orchestrator and worker produced identical HarnessConstraints version attribute %q; "+
			"they must differ when InjectionsVersion (%q) != OrchestratorInjectionsVersion (%q)",
			orchNode.Version(), injectionsVersion, orchestratorVersion)
	}
}

// ---------------------------------------------------------------------------
// T2.3 — Injection version fields absent from frontmatter
// ---------------------------------------------------------------------------

// TestInjectionVersionsAbsentFromFrontmatter_Create verifies that mosaic_injections_version
// is absent from frontmatter in the transform output on a new deployment (req.Deployed == nil).
// Stage 2's I2.2 removes the applyVersionStamp call that was writing this field. After Stage 2,
// the version lives on region tags only.
//
// This test FAILS until I2.2 removes the applyVersionStamp call for injections_version.
func TestInjectionVersionsAbsentFromFrontmatter_Create(t *testing.T) {
	doc, _ := applyWithHarnessRegion(t, "injection-version-tag-test", "1.2.0", "", nil)
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_injections_version"); ok {
		t.Error("output frontmatter carries \"mosaic_injections_version\" on create; " +
			"Stage 2 removes this field from frontmatter — the version is now stored on the region tag attribute only")
	}
}

// TestInjectionVersionsAbsentFromFrontmatter_Update verifies that mosaic_injections_version
// is absent from frontmatter in the transform output on an Update scenario. Even when the
// previously-deployed file carries this field, the output must not contain it (stripped by
// migration in Stage 1; never written in Stage 2).
//
// This test FAILS until I2.2 removes the applyVersionStamp call for injections_version.
func TestInjectionVersionsAbsentFromFrontmatter_Update(t *testing.T) {
	doc, _ := applyWithHarnessRegion(t, "injection-version-tag-test", "1.2.0", "", []byte(preTagMigrationDeployed))
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_injections_version"); ok {
		t.Error("output frontmatter carries \"mosaic_injections_version\" on update; " +
			"Stage 2 removes this field from frontmatter — the version is now stored on the region tag attribute only")
	}
}

// TestOrchestratorInjectionVersionAbsentFromFrontmatter_NonOrchestrator verifies that
// mosaic_orchestrator_injections_version is absent from the frontmatter output for a
// non-orchestrator agent. Stage 2's I2.3 removes the Frontmatter() stamping from all four
// builtin harness modules. The fixture module never stamped this field, so this test
// also serves as a regression guard.
//
// This test FAILS until I2.3 ensures that no code path writes this field for non-orchestrators.
func TestOrchestratorInjectionVersionAbsentFromFrontmatter_NonOrchestrator(t *testing.T) {
	doc, _ := applyWithHarnessRegion(t, "some-worker-agent", "1.2.0", "2.7.0", nil)
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_orchestrator_injections_version"); ok {
		t.Error("output frontmatter carries \"mosaic_orchestrator_injections_version\" for a non-orchestrator agent; " +
			"Stage 2 removes this field from frontmatter entirely — it must be absent for all agents")
	}
}

// TestOrchestratorInjectionVersionAbsentFromFrontmatter_Orchestrator verifies that
// mosaic_orchestrator_injections_version is absent from the frontmatter output even for an
// orchestrator agent (req.Key == "orchestrator"). Stage 2 moves this version to the region
// tag; it must not appear in frontmatter.
//
// This test FAILS until I2.3 removes the orchestrator-conditional frontmatter stamping from
// all four builtin harness modules' Frontmatter() methods.
func TestOrchestratorInjectionVersionAbsentFromFrontmatter_Orchestrator(t *testing.T) {
	doc, _ := applyWithHarnessRegion(t, "orchestrator", "1.2.0", "2.7.0", nil)
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_orchestrator_injections_version"); ok {
		t.Error("output frontmatter carries \"mosaic_orchestrator_injections_version\" for an orchestrator agent; " +
			"Stage 2 relocates this version to the region tag attribute — it must be absent from frontmatter")
	}
}

// ---------------------------------------------------------------------------
// T2.4 — Backwards-compatible migration: frontmatter → tag relocation
// ---------------------------------------------------------------------------

// TestMigration_TagRelocation_FrontmatterGone_NonOrchestrator verifies that when a
// previously-deployed file carries mosaic_injections_version as a frontmatter field (pre-Stage 2),
// after transform.Apply (Update scenario) that field is absent from the output frontmatter.
// The migration strip (Stage 1's I1.4) removes the field; Stage 2's I2.2 ensures it is never
// re-written.
//
// This test FAILS until both the migration strip (Stage 1) and the removal of the write call
// (Stage 2 I2.2) are implemented.
func TestMigration_TagRelocation_FrontmatterGone_NonOrchestrator(t *testing.T) {
	doc, _ := applyWithHarnessRegion(t,
		"injection-version-tag-test", "1.2.0", "", []byte(preTagMigrationDeployed))
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_injections_version"); ok {
		t.Error("output frontmatter still carries \"mosaic_injections_version\" after migration; " +
			"Update of a pre-Stage 2 file must strip this field from frontmatter (relocation to tag)")
	}
}

// TestMigration_TagRelocation_FrontmatterGone_Orchestrator verifies that when a
// previously-deployed file carries mosaic_orchestrator_injections_version as a frontmatter
// field (pre-Stage 2), after transform.Apply (Update scenario) that field is absent from the
// output frontmatter. This covers the orchestrator-specific injection version field.
//
// This test FAILS until I2.3 removes the write path for this field.
func TestMigration_TagRelocation_FrontmatterGone_Orchestrator(t *testing.T) {
	doc, _ := applyWithHarnessRegion(t,
		"orchestrator", "1.2.0", "2.7.0", []byte(preTagMigrationDeployed))
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_orchestrator_injections_version"); ok {
		t.Error("output frontmatter still carries \"mosaic_orchestrator_injections_version\" after migration; " +
			"Update of a pre-Stage 2 file must strip this field from frontmatter (relocation to tag)")
	}
}

// TestMigration_TagRelocation_VersionAppearsOnTag_NonOrchestrator verifies the complete
// relocation contract: after Update of a pre-Stage 2 file (which has mosaic_injections_version
// in frontmatter), the output must carry the version on the HarnessConstraints tag instead.
// This test pairs with TestMigration_TagRelocation_FrontmatterGone_NonOrchestrator to assert
// that the version is relocated (not merely discarded).
//
// This test FAILS until I2.1 adds the SetVersion call in applyHarnessRegion.
func TestMigration_TagRelocation_VersionAppearsOnTag_NonOrchestrator(t *testing.T) {
	const wantVersion = "1.2.0"
	doc, _ := applyWithHarnessRegion(t,
		"injection-version-tag-test", wantVersion, "", []byte(preTagMigrationDeployed))

	node := findHarnessConstraintsNode(t, doc)
	if got := node.Version(); got != wantVersion {
		t.Errorf("HarnessConstraints version attribute after migration: want %q, got %q; "+
			"the version must be relocated from frontmatter to the region tag attribute (not discarded)",
			wantVersion, got)
	}
}

// TestMigration_TagRelocation_VersionAppearsOnTag_Orchestrator verifies the complete
// relocation contract for orchestrator agents: after Update of a pre-Stage 2 orchestrator
// file (which has mosaic_orchestrator_injections_version in frontmatter), the output must
// carry the orchestrator version on the HarnessConstraints tag instead.
//
// This test FAILS until I2.1 adds the SetVersion call for the orchestrator branch.
func TestMigration_TagRelocation_VersionAppearsOnTag_Orchestrator(t *testing.T) {
	const injectionsVersion = "1.2.0"
	const wantVersion = "2.7.0"
	doc, _ := applyWithHarnessRegion(t,
		"orchestrator", injectionsVersion, wantVersion, []byte(preTagMigrationDeployed))

	node := findHarnessConstraintsNode(t, doc)
	if got := node.Version(); got != wantVersion {
		t.Errorf("HarnessConstraints version attribute for orchestrator after migration: want %q (OrchestratorInjectionsVersion), got %q; "+
			"the orchestrator version must be relocated from frontmatter to the region tag attribute",
			wantVersion, got)
	}
}

// ---------------------------------------------------------------------------
// Additional edge case: empty InjectionsVersion is a no-op
// ---------------------------------------------------------------------------

// TestHarnessRegionVersion_EmptyInjectionsVersion_NoVersionAttribute verifies that when
// InjectionsVersion is "" (empty string), the HarnessConstraints region's tag carries no
// version attribute. The planned implementation guards the SetVersion call with
// `if injVersion != ""`, so an empty version must result in no attribute being written.
// This test pins the guard condition and prevents regressions where a naive implementation
// might write an empty version attribute unconditionally.
func TestHarnessRegionVersion_EmptyInjectionsVersion_NoVersionAttribute(t *testing.T) {
	doc, _ := applyWithHarnessRegion(t, "injection-version-tag-test", "", "", nil)

	node := findHarnessConstraintsNode(t, doc)
	if got := node.Version(); got != "" {
		t.Errorf("HarnessConstraints version attribute with empty InjectionsVersion: want \"\" (no attribute), got %q; "+
			"applyHarnessRegion must not call node.SetVersion when InjectionsVersion is empty",
			got)
	}
}
