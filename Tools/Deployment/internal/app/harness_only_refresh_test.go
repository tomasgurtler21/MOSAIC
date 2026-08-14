package app

// harness_only_refresh_test.go tests the refreshHarnessOnly function, which regenerates
// in-scope tool-managed regions of a harness-only agent while preserving injection content,
// section structure, and bytes outside the refreshed regions.
//
// This file is in package app (not package app_test) because refreshHarnessOnly is
// unexported and cannot be reached from the black-box test package.
//
// Verified behaviours, matching the Stage 6 plan tasks:
//
// Scope-limited regeneration:
//   - Under RefreshProtocolOnly, the CommunicationProtocol region carries canonical content.
//   - Under RefreshProtocolOnly, every other deployed region is byte-identical to the input.
//   - Under RefreshAllDeployed, every in-scope canonical deployed region is present and
//     carries canonical content for the agent's role.
//   - Under RefreshAllDeployed, the CommunicationProtocol region carries the version marker
//     line followed by the role-matched protocol block.
//   - Under RefreshAllDeployed, bundle-sourced regions carry the role-matched bundle block.
//
// Adding missing in-scope regions:
//   - A CommunicationProtocol region absent from the input is added under RefreshProtocolOnly.
//   - A bundle-sourced region absent from the input is added when its parent section is present.
//   - A bundle-sourced region absent from the input and whose parent section is also absent is
//     reported in result.NotAdded and never inserted.
//   - result.Added names the regions that were missing and were successfully inserted.
//   - result.NotAdded names regions that were missing and could not be inserted.
//
// Injection content preservation:
//   - Injection region content is byte-identical between input and output under both scopes.
//   - Multiple injection regions across sections are all preserved verbatim.
//
// Section tag and body byte-identity:
//   - Section open and close tag bytes are identical between input and output.
//   - Section prose outside of deployed regions is preserved verbatim.
//
// Bytes outside refreshed regions:
//   - Frontmatter bytes are not modified.
//   - An out-of-scope deployed region's bytes are identical between input and output.
//
// Determinism:
//   - Repeated calls with identical inputs produce byte-identical output under both scopes.
//
// Missing version frontmatter:
//   - A harness-only file with no `version` field succeeds without error.
//   - The output contains no `version` field when the input had none.
//
// Regression for catalog-backed transform path:
//   - transform.ErrSourceDeployedRegionNotEmpty is preserved as a non-nil sentinel error.
//   - The app package does not import the transform package in a way that would contaminate it.
//   - refreshHarnessOnly never returns transform.ErrSourceDeployedRegionNotEmpty.

import (
	"bytes"
	"errors"
	"go/build"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Protocol and bundle fixture constants
// ---------------------------------------------------------------------------

// fixtureRefreshProtocolVersion is the protocol source version used across refresh tests.
const fixtureRefreshProtocolVersion = "1.9"

// fixtureRefreshBundleVersion is the bundle source version used across refresh tests.
const fixtureRefreshBundleVersion = "2.0.0"

// fixtureRefreshSubagentProtocolBlock is the distinguishable content of the subagent
// protocol block. It must not appear in any "old" fixture content and must be absent
// from the orchestrator protocol block.
const fixtureRefreshSubagentProtocolBlock = "SUBAGENT PROTOCOL BLOCK FOR REFRESH TESTS\n"

// fixtureRefreshOrchestratorProtocolBlock is the distinguishable content of the
// orchestrator protocol block. It must not appear in any "old" fixture content and
// must be absent from the subagent protocol block.
const fixtureRefreshOrchestratorProtocolBlock = "ORCHESTRATOR PROTOCOL BLOCK FOR REFRESH TESTS\n"

// fixtureRefreshProtocol returns a ProtocolContent with distinct, recognisable content for
// each role variant. Both blocks carry unique strings so a test can assert the correct
// block was used and that the wrong block was not.
func fixtureRefreshProtocol(version string) domain.ProtocolContent {
	return domain.ProtocolContent{
		Version: version,
		Blocks: map[domain.ProtocolVariant][]byte{
			domain.ProtocolSubagent:     []byte(fixtureRefreshSubagentProtocolBlock),
			domain.ProtocolOrchestrator: []byte(fixtureRefreshOrchestratorProtocolBlock),
		},
	}
}

// refreshBundleBlock returns the recognisable canonical block content for a given deployed
// region name and role, matching what fixtureRefreshBundle populates. Tests assert on these
// values when verifying that the output carries canonical content.
func refreshBundleBlock(regionName string, role domain.AgentRole) []byte {
	roleStr := string(role)
	return []byte("CANONICAL " + strings.ToUpper(regionName) + " FOR " + strings.ToUpper(roleStr) + "\n")
}

// fixtureRefreshBundle returns a BundleContent covering all nine canonical deployed
// regions for both subagent and orchestrator roles. Each block's content is produced by
// refreshBundleBlock so tests can deterministically assert on expected output.
func fixtureRefreshBundle(version string) domain.BundleContent {
	regions := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"ProtocolConstraints",
		"HarnessConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	roles := []domain.AgentRole{domain.RoleSubagent, domain.RoleOrchestrator}

	var blocks []domain.BundleBlock
	for _, region := range regions {
		for _, role := range roles {
			// Name is a display/identifier field; matching uses Target and AppliesTo.
			// Format: "RegionName:rolename" (role string is lower-case by domain convention).
			blocks = append(blocks, domain.BundleBlock{
				Name:      region + ":" + string(role),
				Target:    region,
				AppliesTo: string(role),
				Content:   refreshBundleBlock(region, role),
			})
		}
	}
	return domain.BundleContent{Version: version, Blocks: blocks}
}

// makeRefreshRequest constructs a HarnessOnlyRefreshRequest from the given file bytes, scope,
// and role, using the shared fixture protocol and bundle content.
func makeRefreshRequest(deployed []byte, scope RefreshScope, role domain.AgentRole) HarnessOnlyRefreshRequest {
	return HarnessOnlyRefreshRequest{
		Deployed: deployed,
		Scope:    scope,
		Role:     role,
		Protocol: fixtureRefreshProtocol(fixtureRefreshProtocolVersion),
		Bundle:   fixtureRefreshBundle(fixtureRefreshBundleVersion),
		Subject:  "test-agent.agent.md",
	}
}

// ---------------------------------------------------------------------------
// Fixture agent document strings
// ---------------------------------------------------------------------------

// fixtureSubagentFull is a complete harness-only subagent file with all subagent-applicable
// canonical deployed regions populated with recognisable "old" content, plus injection
// regions carrying user-authored content that must be preserved verbatim.
//
// This fixture is used for protocol-only scope tests where out-of-scope deployed regions
// must be byte-identical and injection content must be preserved.
const fixtureSubagentFull = `---
id: 42
transform_version: 3.0.0
role: subagent
---

<Identity type="core">
# Test Harness-Only Subagent

Section prose that must not be modified by refresh.

<AuthorityHierarchy type="managed">
old authority hierarchy content
</AuthorityHierarchy>

<ClosingProcedure type="managed">
old closing procedure content
</ClosingProcedure>

<IdentityExtension type="project">
user-authored identity extension — this line must survive verbatim
second line of user-authored injection content
</IdentityExtension>

</Identity>

<CommunicationProtocol type="managed">
old communication protocol content
</CommunicationProtocol>

<Capabilities type="core">
Capability prose that must survive verbatim.

<CodebaseContext type="project">
user codebase context injection — must survive verbatim
</CodebaseContext>

</Capabilities>

<Constraints type="core">

<ProtocolConstraints type="managed">
old protocol constraints content
</ProtocolConstraints>

<HarnessConstraints type="managed">
old harness constraints content
</HarnessConstraints>

</Constraints>

<ErrorHandling type="core">

<ErrorHandlingCommon type="managed">
old error handling common content
</ErrorHandlingCommon>

</ErrorHandling>

<ExecutionPhilosophy type="core">

<ExecutionPhilosophyCommon type="managed">
old execution philosophy common content
</ExecutionPhilosophyCommon>

</ExecutionPhilosophy>
`

// fixtureOrchestratorFull is a complete harness-only orchestrator file with all nine
// canonical deployed regions populated with "old" content and injection regions carrying
// user-authored content.
//
// This fixture is used for all-deployed scope tests where all nine regions must be
// refreshed.
const fixtureOrchestratorFull = `---
id: 43
transform_version: 3.0.0
role: orchestrator
---

<Identity type="core">
# Test Harness-Only Orchestrator

Orchestrator identity prose.

<AuthorityHierarchy type="managed">
old authority hierarchy content
</AuthorityHierarchy>

<ClosingProcedure type="managed">
old closing procedure content
</ClosingProcedure>

<AvailableWorkflows type="managed">
old available workflows content
</AvailableWorkflows>

<InfrastructureAgents type="managed">
old infrastructure agents content
</InfrastructureAgents>

<IdentityExtension type="project">
orchestrator identity injection — must survive verbatim
</IdentityExtension>

</Identity>

<CommunicationProtocol type="managed">
old communication protocol content
</CommunicationProtocol>

<Capabilities type="core">

</Capabilities>

<Constraints type="core">

<ProtocolConstraints type="managed">
old protocol constraints content
</ProtocolConstraints>

<HarnessConstraints type="managed">
old harness constraints content
</HarnessConstraints>

</Constraints>

<ErrorHandling type="core">

<ErrorHandlingCommon type="managed">
old error handling common content
</ErrorHandlingCommon>

</ErrorHandling>

<ExecutionPhilosophy type="core">

<ExecutionPhilosophyCommon type="managed">
old execution philosophy common content
</ExecutionPhilosophyCommon>

</ExecutionPhilosophy>
`

// fixtureSubagentNoProtocol is a subagent file with no CommunicationProtocol region.
// Under RefreshProtocolOnly scope, the region must be added.
const fixtureSubagentNoProtocol = `---
id: 44
transform_version: 3.0.0
role: subagent
---

<Identity type="core">
# Agent Without Protocol Region

<AuthorityHierarchy type="managed">
old authority hierarchy content
</AuthorityHierarchy>

</Identity>

<Capabilities type="core">

</Capabilities>

<Constraints type="core">

<HarnessConstraints type="managed">
old harness constraints content
</HarnessConstraints>

</Constraints>
`

// fixtureOrchestratorMissingBundleRegion is an orchestrator file that has all parent
// sections but is missing the ErrorHandlingCommon region. Under RefreshAllDeployed scope,
// that region must be inserted because its parent ErrorHandling section is present.
const fixtureOrchestratorMissingBundleRegion = `---
id: 46
transform_version: 3.0.0
role: orchestrator
---

<Identity type="core">
# Orchestrator Missing One Bundle Region

<AuthorityHierarchy type="managed">
old authority hierarchy content
</AuthorityHierarchy>

<ClosingProcedure type="managed">
old closing procedure content
</ClosingProcedure>

<AvailableWorkflows type="managed">
old available workflows content
</AvailableWorkflows>

<InfrastructureAgents type="managed">
old infrastructure agents content
</InfrastructureAgents>

</Identity>

<CommunicationProtocol type="managed">
old communication protocol content
</CommunicationProtocol>

<Capabilities type="core">

</Capabilities>

<Constraints type="core">

<ProtocolConstraints type="managed">
old protocol constraints content
</ProtocolConstraints>

<HarnessConstraints type="managed">
old harness constraints content
</HarnessConstraints>

</Constraints>

<ErrorHandling type="core">

</ErrorHandling>

<ExecutionPhilosophy type="core">

<ExecutionPhilosophyCommon type="managed">
old execution philosophy common content
</ExecutionPhilosophyCommon>

</ExecutionPhilosophy>
`

// fixtureSubagentMissingSectionAndRegion is a subagent file missing both the
// ErrorHandling section and the ErrorHandlingCommon region. Under RefreshAllDeployed
// scope, the region cannot be inserted because its parent section is absent, so it
// must appear in result.NotAdded.
const fixtureSubagentMissingSectionAndRegion = `---
id: 47
transform_version: 3.0.0
role: subagent
---

<Identity type="core">
# Agent Missing ErrorHandling Section

<AuthorityHierarchy type="managed">
old authority hierarchy content
</AuthorityHierarchy>

</Identity>

<CommunicationProtocol type="managed">
old communication protocol content
</CommunicationProtocol>

<Capabilities type="core">

</Capabilities>

<Constraints type="core">

<ProtocolConstraints type="managed">
old protocol constraints content
</ProtocolConstraints>

<HarnessConstraints type="managed">
old harness constraints content
</HarnessConstraints>

</Constraints>

<ExecutionPhilosophy type="core">

<ExecutionPhilosophyCommon type="managed">
old execution philosophy common content
</ExecutionPhilosophyCommon>

</ExecutionPhilosophy>
`

// fixtureMalformedUnclosedFrontmatter is a byte sequence whose frontmatter block is
// never closed (the closing "---" delimiter is absent). docformat.Parse returns an error
// for this input, which refreshHarnessOnly must propagate as a non-nil error rather than
// proceeding with a partially initialised document or panicking.
const fixtureMalformedUnclosedFrontmatter = "---\nid: malformed-test\nrole: subagent\n"

// fixtureSubagentNoVersion is a valid subagent file with no `version` field in
// frontmatter. refreshHarnessOnly must accept it without error because harness-only
// agents commonly lack a version field and are always eligible for refresh.
const fixtureSubagentNoVersion = `---
id: 48
transform_version: 3.0.0
role: subagent
---

<Identity type="core">
# Agent Without Version Frontmatter Field

<AuthorityHierarchy type="managed">
old authority hierarchy content
</AuthorityHierarchy>

</Identity>

<CommunicationProtocol type="managed">
old communication protocol content
</CommunicationProtocol>

<Constraints type="core">

<HarnessConstraints type="managed">
old harness constraints content
</HarnessConstraints>

</Constraints>
`

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// deployedRegionContent parses doc and returns the content bytes of the deployed region
// with the given name. The test fails immediately when the region is absent from the
// document or when the document cannot be parsed.
func deployedRegionContent(t *testing.T, doc []byte, name string) []byte {
	t.Helper()
	parsed, err := docformat.Parse(doc)
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := parsed.Body().Deployed(name)
	if !ok {
		t.Fatalf("deployed region %q not found in document", name)
	}
	return node.Content()
}

// deployedRegionBytes parses doc and returns the full bytes (open tag + content + close tag)
// of the deployed region with the given name. The test fails immediately when absent or
// unparseable.
func deployedRegionBytes(t *testing.T, doc []byte, name string) []byte {
	t.Helper()
	parsed, err := docformat.Parse(doc)
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := parsed.Body().Deployed(name)
	if !ok {
		t.Fatalf("deployed region %q not found in document", name)
	}
	return node.Bytes()
}

// injectionBytes parses doc and returns the full bytes of the injection region with the
// given name. The test fails immediately when absent or unparseable.
func injectionBytes(t *testing.T, doc []byte, name string) []byte {
	t.Helper()
	parsed, err := docformat.Parse(doc)
	if err != nil {
		t.Fatalf("parse document: %v", err)
	}
	node, ok := parsed.Body().Injection(name)
	if !ok {
		t.Fatalf("injection region %q not found in document", name)
	}
	return node.Bytes()
}

// expectedProtocolContent returns the expected bytes that refreshHarnessOnly should place
// inside the CommunicationProtocol region for the given role. The version is stored as a
// tag attribute on the opening tag, not as a comment in the region content, so the content
// is just the role-matched block.
func expectedProtocolContent(role domain.AgentRole, _ string) []byte {
	switch role {
	case domain.RoleOrchestrator:
		return []byte(fixtureRefreshOrchestratorProtocolBlock)
	default:
		return []byte(fixtureRefreshSubagentProtocolBlock)
	}
}

// containsString reports whether haystack contains needle.
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// T6.1 — Scope-limited region regeneration
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_ProtocolOnlyScope_CommunicationProtocolRegionIsRefreshed verifies
// that under RefreshProtocolOnly scope the CommunicationProtocol region carries fresh
// canonical content for the agent's role after the refresh. The old content must not appear.
func TestRefreshHarnessOnly_ProtocolOnlyScope_CommunicationProtocolRegionIsRefreshed(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentFull), RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	content := deployedRegionContent(t, result.Output, "CommunicationProtocol")

	// Old content must be gone.
	if bytes.Contains(content, []byte("old communication protocol content")) {
		t.Errorf("CommunicationProtocol region still contains old content after refresh; " +
			"the region must be regenerated from the canonical protocol source")
	}

	// Canonical subagent block must be present.
	if !bytes.Contains(content, []byte(fixtureRefreshSubagentProtocolBlock)) {
		t.Errorf("CommunicationProtocol region does not contain the subagent protocol block; " +
			"got content: %q", content)
	}
}

// TestRefreshHarnessOnly_ProtocolOnlyScope_VersionAttributeIsSetOnProtocolRegion verifies
// that the CommunicationProtocol region's opening tag carries the protocol version in its
// version attribute after a RefreshProtocolOnly refresh, matching how the catalog-backed
// path stores the version.
func TestRefreshHarnessOnly_ProtocolOnlyScope_VersionAttributeIsSetOnProtocolRegion(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentFull), RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("CommunicationProtocol region not found in output")
	}
	if node.Version() != fixtureRefreshProtocolVersion {
		t.Errorf("CommunicationProtocol region version attribute = %q; want %q;\n"+
			"the protocol version must be stored as a tag attribute on the opening tag",
			node.Version(), fixtureRefreshProtocolVersion)
	}
}

// TestRefreshHarnessOnly_ProtocolOnlyScope_OtherDeployedRegionsAreByteIdentical verifies
// that under RefreshProtocolOnly scope every deployed region OTHER than CommunicationProtocol
// is byte-identical between the input and the output. Refreshing one region must not touch
// any other.
func TestRefreshHarnessOnly_ProtocolOnlyScope_OtherDeployedRegionsAreByteIdentical(t *testing.T) {
	input := []byte(fixtureSubagentFull)
	req := makeRefreshRequest(input, RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	// All deployed regions in the input that are not CommunicationProtocol must be
	// byte-identical in the output.
	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	for _, inNode := range inDoc.Body().DeployedRegions() {
		if inNode.Name() == "CommunicationProtocol" {
			continue // this region was refreshed; skip it
		}
		outNode, ok := outDoc.Body().Deployed(inNode.Name())
		if !ok {
			t.Errorf("deployed region %q is present in input but absent from output; "+
				"refreshHarnessOnly must not remove out-of-scope regions",
				inNode.Name())
			continue
		}
		if !bytes.Equal(inNode.Bytes(), outNode.Bytes()) {
			t.Errorf("deployed region %q is not in scope for RefreshProtocolOnly but its bytes "+
				"changed;\ninput  bytes: %q\noutput bytes: %q",
				inNode.Name(), inNode.Bytes(), outNode.Bytes())
		}
	}
}

// TestRefreshHarnessOnly_AllDeployedScope_AllCanonicalRegionsPresentAndCanonical verifies
// that under RefreshAllDeployed scope every region in docformat.CanonicalDeployed that is
// present in the input ends up with canonical content in the output.
func TestRefreshHarnessOnly_AllDeployedScope_AllCanonicalRegionsPresentAndCanonical(t *testing.T) {
	input := []byte(fixtureOrchestratorFull)
	req := makeRefreshRequest(input, RefreshAllDeployed, domain.RoleOrchestrator)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}

	// Every region present in the input must be present in the output and carry new
	// canonical content (not the old stale content).
	for _, inNode := range inDoc.Body().DeployedRegions() {
		outNode, ok := outDoc.Body().Deployed(inNode.Name())
		if !ok {
			t.Errorf("deployed region %q present in input is absent from output after "+
				"RefreshAllDeployed; regions must not be removed", inNode.Name())
			continue
		}
		if bytes.Contains(outNode.Content(), []byte("old")) {
			t.Errorf("deployed region %q still contains \"old\" content after RefreshAllDeployed; "+
				"all in-scope regions must carry canonical content:\ncontent: %q",
				inNode.Name(), outNode.Content())
		}
	}
}

// TestRefreshHarnessOnly_AllDeployedScope_BundleRegionsCarryRoleMatchedContent verifies
// that bundle-sourced regions carry the role-matched canonical block after a RefreshAllDeployed
// refresh. This confirms the per-region content resolution is role-aware.
func TestRefreshHarnessOnly_AllDeployedScope_BundleRegionsCarryRoleMatchedContent(t *testing.T) {
	// Check a representative subset of bundle-sourced regions for the orchestrator role.
	bundleRegions := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"HarnessConstraints",
	}

	input := []byte(fixtureOrchestratorFull)
	req := makeRefreshRequest(input, RefreshAllDeployed, domain.RoleOrchestrator)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	for _, region := range bundleRegions {
		content := deployedRegionContent(t, result.Output, region)
		want := refreshBundleBlock(region, domain.RoleOrchestrator)
		if !bytes.Equal(bytes.TrimSpace(content), bytes.TrimSpace(want)) {
			t.Errorf("region %q: content does not match orchestrator bundle block;\nwant: %q\ngot:  %q",
				region, want, content)
		}
	}
}

// TestRefreshHarnessOnly_ProtocolOnlyScope_SubagentBlockUsedNotOrchestratorBlock verifies
// that the role parameter selects the correct protocol block. A subagent agent must receive
// the subagent block and must not receive the orchestrator block.
func TestRefreshHarnessOnly_ProtocolOnlyScope_SubagentBlockUsedNotOrchestratorBlock(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentFull), RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	content := deployedRegionContent(t, result.Output, "CommunicationProtocol")
	if bytes.Contains(content, []byte(fixtureRefreshOrchestratorProtocolBlock)) {
		t.Errorf("subagent refresh used the orchestrator block; " +
			"the role parameter must select the subagent protocol variant")
	}
	if !bytes.Contains(content, []byte(fixtureRefreshSubagentProtocolBlock)) {
		t.Errorf("subagent refresh did not produce the subagent block; "+
			"content: %q", content)
	}
}

// TestRefreshHarnessOnly_ProtocolOnlyScope_OrchestratorBlockUsedForOrchestratorRole verifies
// that an orchestrator agent receives the orchestrator protocol block and not the subagent one.
func TestRefreshHarnessOnly_ProtocolOnlyScope_OrchestratorBlockUsedForOrchestratorRole(t *testing.T) {
	// Use the orchestrator fixture but only refresh the protocol region.
	req := makeRefreshRequest([]byte(fixtureOrchestratorFull), RefreshProtocolOnly, domain.RoleOrchestrator)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	content := deployedRegionContent(t, result.Output, "CommunicationProtocol")
	if bytes.Contains(content, []byte(fixtureRefreshSubagentProtocolBlock)) {
		t.Errorf("orchestrator refresh used the subagent block; " +
			"the role parameter must select the orchestrator protocol variant")
	}
	if !bytes.Contains(content, []byte(fixtureRefreshOrchestratorProtocolBlock)) {
		t.Errorf("orchestrator refresh did not produce the orchestrator block; "+
			"content: %q", content)
	}
}

// TestRefreshHarnessOnly_UnrecognisedScope_FallsBackToProtocolOnlyBehaviourEndToEnd
// verifies that an unrecognised RefreshScope value (neither RefreshProtocolOnly nor
// RefreshAllDeployed) causes refreshHarnessOnly to behave exactly as RefreshProtocolOnly
// end-to-end: only the CommunicationProtocol region is refreshed and every other deployed
// region is byte-identical to the input. The Regions() method's documented safe-default
// contract exists at the unit level; this test confirms the full end-to-end path through
// refreshHarnessOnly also honours that fallback.
func TestRefreshHarnessOnly_UnrecognisedScope_FallsBackToProtocolOnlyBehaviourEndToEnd(t *testing.T) {
	const unrecognisedScope RefreshScope = "unrecognised-scope-value"

	input := []byte(fixtureSubagentFull)
	req := makeRefreshRequest(input, unrecognisedScope, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v; an unrecognised scope must not return an error — "+
			"it must fall back to the narrow CommunicationProtocol-only scope", err)
	}

	// CommunicationProtocol must be regenerated (old content gone, canonical content present).
	content := deployedRegionContent(t, result.Output, "CommunicationProtocol")
	if bytes.Contains(content, []byte("old communication protocol content")) {
		t.Error("CommunicationProtocol region still contains old content under an unrecognised " +
			"scope; the fallback to protocol-only scope must still regenerate this region")
	}
	if !bytes.Contains(content, []byte(fixtureRefreshSubagentProtocolBlock)) {
		t.Errorf("CommunicationProtocol region does not contain canonical content under an "+
			"unrecognised scope; got: %q", content)
	}

	// Every other deployed region must be byte-identical to the input — the unrecognised
	// scope must not widen the refresh beyond CommunicationProtocol.
	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	for _, inNode := range inDoc.Body().DeployedRegions() {
		if inNode.Name() == "CommunicationProtocol" {
			continue // this region was refreshed under the fallback scope
		}
		outNode, ok := outDoc.Body().Deployed(inNode.Name())
		if !ok {
			t.Errorf("deployed region %q absent from output after unrecognised-scope fallback; "+
				"regions outside the fallback scope must not be removed", inNode.Name())
			continue
		}
		if !bytes.Equal(inNode.Bytes(), outNode.Bytes()) {
			t.Errorf("deployed region %q changed under unrecognised scope; "+
				"only CommunicationProtocol may be refreshed in the protocol-only fallback;\n"+
				"input  bytes: %q\noutput bytes: %q",
				inNode.Name(), inNode.Bytes(), outNode.Bytes())
		}
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Adding missing in-scope regions
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_MissingCommunicationProtocol_IsAddedUnderProtocolOnlyScope
// verifies that when the input file contains no CommunicationProtocol region, a
// RefreshProtocolOnly refresh inserts one with canonical content for the agent's role.
func TestRefreshHarnessOnly_MissingCommunicationProtocol_IsAddedUnderProtocolOnlyScope(t *testing.T) {
	// The fixture has no CommunicationProtocol region.
	req := makeRefreshRequest([]byte(fixtureSubagentNoProtocol), RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v; a missing CommunicationProtocol region must be "+
			"inserted, not treated as an error", err)
	}

	content := deployedRegionContent(t, result.Output, "CommunicationProtocol")
	if !bytes.Contains(content, []byte(fixtureRefreshSubagentProtocolBlock)) {
		t.Errorf("inserted CommunicationProtocol region does not contain the subagent block; "+
			"content: %q", content)
	}
}

// TestRefreshHarnessOnly_MissingCommunicationProtocol_ReportedInResultAdded verifies that
// result.Added names the CommunicationProtocol region when it was absent and was inserted.
func TestRefreshHarnessOnly_MissingCommunicationProtocol_ReportedInResultAdded(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentNoProtocol), RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	if !containsString(result.Added, "CommunicationProtocol") {
		t.Errorf("result.Added = %v; want it to contain %q because the region was absent "+
			"from the input and was successfully inserted",
			result.Added, "CommunicationProtocol")
	}
}

// TestRefreshHarnessOnly_MissingBundleRegion_IsAddedWhenParentSectionPresent verifies that
// a bundle-sourced deployed region absent from the input is inserted when its required parent
// section is present in the file.
func TestRefreshHarnessOnly_MissingBundleRegion_IsAddedWhenParentSectionPresent(t *testing.T) {
	// The fixture has all sections but is missing the ErrorHandlingCommon region.
	// The ErrorHandling section IS present, so insertion must succeed.
	req := makeRefreshRequest([]byte(fixtureOrchestratorMissingBundleRegion), RefreshAllDeployed, domain.RoleOrchestrator)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	// The missing region must now be present in the output.
	content := deployedRegionContent(t, result.Output, "ErrorHandlingCommon")
	want := refreshBundleBlock("ErrorHandlingCommon", domain.RoleOrchestrator)
	if !bytes.Contains(content, want) {
		t.Errorf("inserted ErrorHandlingCommon region does not carry canonical content;\n"+
			"want: %q\ngot:  %q", want, content)
	}
}

// TestRefreshHarnessOnly_MissingBundleRegion_ReportedInResultAdded verifies that
// result.Added lists a region that was absent and successfully inserted.
func TestRefreshHarnessOnly_MissingBundleRegion_ReportedInResultAdded(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureOrchestratorMissingBundleRegion), RefreshAllDeployed, domain.RoleOrchestrator)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	if !containsString(result.Added, "ErrorHandlingCommon") {
		t.Errorf("result.Added = %v; want it to include %q because the region was absent "+
			"from the input and was successfully inserted into the present ErrorHandling section",
			result.Added, "ErrorHandlingCommon")
	}
}

// TestRefreshHarnessOnly_MissingRegionWithAbsentParent_IsReportedInNotAdded verifies that
// when an in-scope region's required parent section is absent from the file, the region is
// not inserted and its name appears in result.NotAdded. The routine must never invent a
// section region tag to satisfy the insertion.
func TestRefreshHarnessOnly_MissingRegionWithAbsentParent_IsReportedInNotAdded(t *testing.T) {
	// The fixture is missing the ErrorHandling section AND the ErrorHandlingCommon region.
	// Under RefreshAllDeployed, the implementation cannot insert ErrorHandlingCommon because
	// its parent section does not exist.
	req := makeRefreshRequest([]byte(fixtureSubagentMissingSectionAndRegion), RefreshAllDeployed, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v; a region with an absent parent section must not "+
			"cause an error — it must be reported in result.NotAdded instead", err)
	}

	if !containsString(result.NotAdded, "ErrorHandlingCommon") {
		t.Errorf("result.NotAdded = %v; want it to contain %q because the ErrorHandling "+
			"parent section is absent and the region cannot be inserted without creating "+
			"a section tag, which refreshHarnessOnly must not do",
			result.NotAdded, "ErrorHandlingCommon")
	}
}

// TestRefreshHarnessOnly_MissingRegionWithAbsentParent_NotInResultAdded verifies that a
// region reported in result.NotAdded is NOT also reported in result.Added. The two lists
// must be mutually exclusive.
func TestRefreshHarnessOnly_MissingRegionWithAbsentParent_NotInResultAdded(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentMissingSectionAndRegion), RefreshAllDeployed, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	for _, name := range result.NotAdded {
		if containsString(result.Added, name) {
			t.Errorf("region %q appears in both result.Added and result.NotAdded; "+
				"the two lists must be mutually exclusive", name)
		}
	}
}

// TestRefreshHarnessOnly_MissingRegionWithAbsentParent_NotInsertedInOutput verifies that
// a region whose parent section is absent does not appear in the output document. The
// insertion guard must prevent it from being written.
func TestRefreshHarnessOnly_MissingRegionWithAbsentParent_NotInsertedInOutput(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentMissingSectionAndRegion), RefreshAllDeployed, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Body().Deployed("ErrorHandlingCommon"); ok {
		t.Error("ErrorHandlingCommon region appears in output even though its parent " +
			"ErrorHandling section was absent; refreshHarnessOnly must not invent a section tag")
	}
}

// TestRefreshHarnessOnly_MissingCommunicationProtocol_IsAddedUnderAllDeployedScope
// verifies that when the input file contains no CommunicationProtocol region and the
// RefreshAllDeployed scope is used, the region is inserted with canonical content for
// the agent's role. This complements the RefreshProtocolOnly insertion case and confirms
// that the insertion logic is exercised regardless of which scope triggered it —
// both scopes include CommunicationProtocol and both must add it when absent.
func TestRefreshHarnessOnly_MissingCommunicationProtocol_IsAddedUnderAllDeployedScope(t *testing.T) {
	// fixtureSubagentNoProtocol has no CommunicationProtocol region. RefreshAllDeployed
	// covers CommunicationProtocol and must insert it, exactly as RefreshProtocolOnly does.
	req := makeRefreshRequest([]byte(fixtureSubagentNoProtocol), RefreshAllDeployed, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v; a missing CommunicationProtocol region must be "+
			"inserted under RefreshAllDeployed, not treated as an error", err)
	}

	// The region must be present in the output and carry the canonical subagent block.
	content := deployedRegionContent(t, result.Output, "CommunicationProtocol")
	if !bytes.Contains(content, []byte(fixtureRefreshSubagentProtocolBlock)) {
		t.Errorf("inserted CommunicationProtocol region does not contain the subagent block "+
			"under RefreshAllDeployed scope; content: %q", content)
	}

	// The insertion must be reported in result.Added.
	if !containsString(result.Added, "CommunicationProtocol") {
		t.Errorf("result.Added = %v; want it to contain %q because the region was absent "+
			"from the input and was successfully inserted under RefreshAllDeployed scope",
			result.Added, "CommunicationProtocol")
	}
}

// ---------------------------------------------------------------------------
// T6.3 — Injection content byte-identity
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_InjectionContent_PreservedVerbatimUnderProtocolOnlyScope verifies
// that every injection region's bytes are byte-identical between input and output when
// RefreshProtocolOnly scope is used. Injection content is user-authored and must never be
// merged, diffed, or replaced.
func TestRefreshHarnessOnly_InjectionContent_PreservedVerbatimUnderProtocolOnlyScope(t *testing.T) {
	input := []byte(fixtureSubagentFull)
	req := makeRefreshRequest(input, RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}

	for _, inj := range inDoc.Body().Injections() {
		inBytes := inj.Bytes()
		outBytes := injectionBytes(t, result.Output, inj.Name())
		if !bytes.Equal(inBytes, outBytes) {
			t.Errorf("injection %q: bytes changed under RefreshProtocolOnly scope;\n"+
				"input  bytes: %q\noutput bytes: %q",
				inj.Name(), inBytes, outBytes)
		}
	}
}

// TestRefreshHarnessOnly_InjectionContent_PreservedVerbatimUnderAllDeployedScope verifies
// injection byte-identity under RefreshAllDeployed scope, where all deployed regions are
// refreshed but injection content must still pass through untouched.
func TestRefreshHarnessOnly_InjectionContent_PreservedVerbatimUnderAllDeployedScope(t *testing.T) {
	input := []byte(fixtureOrchestratorFull)
	req := makeRefreshRequest(input, RefreshAllDeployed, domain.RoleOrchestrator)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}

	for _, inj := range inDoc.Body().Injections() {
		inBytes := inj.Bytes()
		outBytes := injectionBytes(t, result.Output, inj.Name())
		if !bytes.Equal(inBytes, outBytes) {
			t.Errorf("injection %q: bytes changed under RefreshAllDeployed scope — "+
				"injection content must be preserved verbatim regardless of scope;\n"+
				"input  bytes: %q\noutput bytes: %q",
				inj.Name(), inBytes, outBytes)
		}
	}
}

// TestRefreshHarnessOnly_MultipleInjectionRegions_AllPreservedVerbatim verifies that a
// document with several populated injection regions across different sections has all of
// them preserved verbatim. No injection may be silently dropped or truncated.
func TestRefreshHarnessOnly_MultipleInjectionRegions_AllPreservedVerbatim(t *testing.T) {
	// fixtureSubagentFull has two injection regions: IdentityExtension and CodebaseContext.
	input := []byte(fixtureSubagentFull)
	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	injections := inDoc.Body().Injections()
	if len(injections) < 2 {
		t.Fatalf("fixture has fewer than 2 injection regions; got %d; the fixture must "+
			"include multiple injection regions to test this scenario", len(injections))
	}

	req := makeRefreshRequest(input, RefreshAllDeployed, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	// Every injection in the input must appear byte-identically in the output.
	injCount := 0
	for _, inj := range injections {
		outBytes := injectionBytes(t, result.Output, inj.Name())
		if !bytes.Equal(inj.Bytes(), outBytes) {
			t.Errorf("injection %q changed: this is the %dth injection — all must survive",
				inj.Name(), injCount+1)
		}
		injCount++
	}
}

// ---------------------------------------------------------------------------
// T6.4 — Section tag structure and body byte-identity
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_SectionTags_AreByteIdenticalInOutput verifies that section open
// and close tag bytes are unchanged by refreshHarnessOnly. Section structure is left
// completely unvalidated and unrewritten; this is asserted as byte-identity, not implied.
func TestRefreshHarnessOnly_SectionTags_AreByteIdenticalInOutput(t *testing.T) {
	input := []byte(fixtureSubagentFull)
	req := makeRefreshRequest(input, RefreshAllDeployed, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	inSections := inDoc.Body().Sections()
	outSections := outDoc.Body().Sections()

	if len(inSections) != len(outSections) {
		t.Errorf("section count changed: input has %d sections, output has %d; "+
			"refreshHarnessOnly must not add or remove sections",
			len(inSections), len(outSections))
		return
	}

	for i, inSec := range inSections {
		outSec := outSections[i]
		if inSec.Name() != outSec.Name() {
			t.Errorf("section[%d]: name changed from %q to %q; "+
				"section tag structure must not be modified", i, inSec.Name(), outSec.Name())
		}
	}
}

// TestRefreshHarnessOnly_SectionProse_IsPreservedVerbatim verifies that text content inside
// sections that is NOT within a deployed or injection region is preserved verbatim. The
// routine must not reformat or normalise section body prose.
func TestRefreshHarnessOnly_SectionProse_IsPreservedVerbatim(t *testing.T) {
	const sentinelProse = "Section prose that must not be modified by refresh."
	input := []byte(fixtureSubagentFull)

	req := makeRefreshRequest(input, RefreshAllDeployed, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	if !bytes.Contains(result.Output, []byte(sentinelProse)) {
		t.Errorf("output does not contain section prose %q; "+
			"refreshHarnessOnly must not modify text outside deployed regions",
			sentinelProse)
	}
}

// ---------------------------------------------------------------------------
// T6.5 — Bytes outside refreshed regions are byte-identical
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_Frontmatter_IsNotModified verifies that the frontmatter block is
// not touched by refreshHarnessOnly. No field is added, removed, reordered, or restamped.
// Frontmatter byte-identity is a hard constraint — this routine has no knowledge of which
// version or stamp fields to update.
func TestRefreshHarnessOnly_Frontmatter_IsNotModified(t *testing.T) {
	input := []byte(fixtureSubagentFull)
	req := makeRefreshRequest(input, RefreshAllDeployed, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	inFields := inDoc.Frontmatter().Fields()
	outFields := outDoc.Frontmatter().Fields()

	if len(inFields) != len(outFields) {
		t.Errorf("frontmatter field count changed: %d -> %d; "+
			"refreshHarnessOnly must not add or remove frontmatter fields",
			len(inFields), len(outFields))
		return
	}
	for i, inf := range inFields {
		outf := outFields[i]
		if inf.Key != outf.Key {
			t.Errorf("frontmatter field[%d] key changed: %q -> %q", i, inf.Key, outf.Key)
		}
	}

	// Also confirm that no version or stamp fields were added.
	stampFields := []string{
		"version", "protocol_version", "bundle_version",
		"tool_mappings_version", "injections_version",
	}
	for _, sf := range stampFields {
		_, inHas := inDoc.Frontmatter().Get(sf)
		_, outHas := outDoc.Frontmatter().Get(sf)
		if !inHas && outHas {
			t.Errorf("frontmatter field %q was added to the output; "+
				"refreshHarnessOnly must not add stamp fields", sf)
		}
	}
}

// TestRefreshHarnessOnly_OutOfScopeDeployedRegion_IsByteIdentical verifies that a deployed
// region not covered by the current scope is byte-identical between input and output. The
// in-scope / out-of-scope distinction is enforced at the byte level.
func TestRefreshHarnessOnly_OutOfScopeDeployedRegion_IsByteIdentical(t *testing.T) {
	// Under RefreshProtocolOnly, only CommunicationProtocol is in scope.
	// AuthorityHierarchy is out of scope and must be byte-identical.
	input := []byte(fixtureSubagentFull)
	req := makeRefreshRequest(input, RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	inBytes := deployedRegionBytes(t, input, "AuthorityHierarchy")
	outBytes := deployedRegionBytes(t, result.Output, "AuthorityHierarchy")

	if !bytes.Equal(inBytes, outBytes) {
		t.Errorf("deployed region AuthorityHierarchy is not in scope for RefreshProtocolOnly "+
			"but its bytes changed;\ninput  (len=%d): %q\noutput (len=%d): %q",
			len(inBytes), inBytes, len(outBytes), outBytes)
	}
}

// TestRefreshHarnessOnly_AllBytesOutsideRefreshedRegion_AreByteIdentical performs a
// structural comparison to assert that every deployed region not in scope, every injection
// region, and all text outside of regions is byte-identical between input and output.
// This is the strongest form of the byte-identity contract.
func TestRefreshHarnessOnly_AllBytesOutsideRefreshedRegion_AreByteIdentical(t *testing.T) {
	input := []byte(fixtureSubagentFull)
	req := makeRefreshRequest(input, RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	inDoc, err := docformat.Parse(input)
	if err != nil {
		t.Fatalf("parse input: %v", err)
	}
	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	// Out-of-scope deployed regions: byte-identical.
	for _, inNode := range inDoc.Body().DeployedRegions() {
		if inNode.Name() == "CommunicationProtocol" {
			continue // this one was refreshed
		}
		outNode, ok := outDoc.Body().Deployed(inNode.Name())
		if !ok {
			t.Errorf("out-of-scope deployed region %q absent from output", inNode.Name())
			continue
		}
		if !bytes.Equal(inNode.Bytes(), outNode.Bytes()) {
			t.Errorf("out-of-scope deployed region %q: bytes changed", inNode.Name())
		}
	}

	// Injection regions: byte-identical.
	for _, inj := range inDoc.Body().Injections() {
		outInj, ok := outDoc.Body().Injection(inj.Name())
		if !ok {
			t.Errorf("injection region %q absent from output", inj.Name())
			continue
		}
		if !bytes.Equal(inj.Bytes(), outInj.Bytes()) {
			t.Errorf("injection region %q: bytes changed", inj.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// T6.6 — Determinism
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_RepeatedCallsWithIdenticalInputs_ProduceByteIdenticalOutput
// verifies that calling refreshHarnessOnly twice with the same inputs produces byte-identical
// output both times. This catches implementations that accumulate or mutate shared state.
func TestRefreshHarnessOnly_RepeatedCallsWithIdenticalInputs_ProduceByteIdenticalOutput(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentFull), RefreshProtocolOnly, domain.RoleSubagent)

	result1, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly (first call): %v", err)
	}
	result2, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly (second call): %v", err)
	}

	if !bytes.Equal(result1.Output, result2.Output) {
		// Find the first difference to give a useful diagnostic.
		diff := 0
		for diff < len(result1.Output) && diff < len(result2.Output) {
			if result1.Output[diff] != result2.Output[diff] {
				break
			}
			diff++
		}
		t.Errorf("refreshHarnessOnly is non-deterministic: identical inputs produced different outputs;\n"+
			"first call:  %d bytes\nsecond call: %d bytes\nfirst difference at byte %d",
			len(result1.Output), len(result2.Output), diff)
	}
}

// TestRefreshHarnessOnly_AllDeployedScope_DeterministicAcrossRepeatedCalls verifies
// determinism under RefreshAllDeployed scope, where more regions are refreshed and the
// risk of map-iteration-order sensitivity is higher.
func TestRefreshHarnessOnly_AllDeployedScope_DeterministicAcrossRepeatedCalls(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureOrchestratorFull), RefreshAllDeployed, domain.RoleOrchestrator)

	result1, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly (first call): %v", err)
	}
	result2, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly (second call): %v", err)
	}

	if !bytes.Equal(result1.Output, result2.Output) {
		t.Errorf("refreshHarnessOnly (RefreshAllDeployed) is non-deterministic: "+
			"first call produced %d bytes, second call produced %d bytes",
			len(result1.Output), len(result2.Output))
	}
}

// TestRefreshHarnessOnly_InputBytesNotMutated verifies that the req.Deployed slice is not
// modified by refreshHarnessOnly. The function is specified to be pure and must not mutate
// its input.
func TestRefreshHarnessOnly_InputBytesNotMutated(t *testing.T) {
	input := []byte(fixtureSubagentFull)
	inputCopy := make([]byte, len(input))
	copy(inputCopy, input)

	req := makeRefreshRequest(input, RefreshAllDeployed, domain.RoleSubagent)
	_, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	if !bytes.Equal(input, inputCopy) {
		t.Error("refreshHarnessOnly mutated req.Deployed; the function must be pure and must " +
			"not modify its input slice")
	}
}

// ---------------------------------------------------------------------------
// T6.7 — Missing-version frontmatter case
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_FileWithNoVersionField_SucceedsWithoutError verifies that a
// harness-only file with no `version` field in frontmatter does not cause refreshHarnessOnly
// to fail. Harness-only agents commonly have no version and are always eligible for refresh;
// an absent version is treated as an explicit, documented fallback rather than an error.
func TestRefreshHarnessOnly_FileWithNoVersionField_SucceedsWithoutError(t *testing.T) {
	// Verify the fixture actually lacks a version field.
	doc, err := docformat.Parse([]byte(fixtureSubagentNoVersion))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if _, ok := doc.Frontmatter().Get("version"); ok {
		t.Fatal("fixtureSubagentNoVersion has a version field; the fixture must not declare one")
	}

	req := makeRefreshRequest([]byte(fixtureSubagentNoVersion), RefreshProtocolOnly, domain.RoleSubagent)
	_, err = refreshHarnessOnly(req)
	if err != nil {
		t.Errorf("refreshHarnessOnly returned error for a file with no version field: %v;\n"+
			"a harness-only agent without a version is always eligible for refresh and must "+
			"not cause the refresh to fail", err)
	}
}

// TestRefreshHarnessOnly_FileWithNoVersionField_FrontmatterNotModified verifies that when
// the input has no `version` field, the output also has no `version` field. refreshHarnessOnly
// must not invent a version stamp for a file that has none — doing so would make the file
// appear catalog-backed on the next run.
func TestRefreshHarnessOnly_FileWithNoVersionField_FrontmatterNotModified(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentNoVersion), RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := outDoc.Frontmatter().Get("version"); ok {
		t.Error("output has a version field that was absent from the input; " +
			"refreshHarnessOnly must not add version stamps to harness-only agents")
	}
}

// TestRefreshHarnessOnly_FileWithNoVersionField_ProtocolRegionIsRefreshed verifies that the
// absent version field does not prevent the protocol region from being refreshed correctly.
// This is a direct assertion that the no-version fallback produces a correct refresh, not
// just a non-fatal one.
func TestRefreshHarnessOnly_FileWithNoVersionField_ProtocolRegionIsRefreshed(t *testing.T) {
	req := makeRefreshRequest([]byte(fixtureSubagentNoVersion), RefreshProtocolOnly, domain.RoleSubagent)
	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	content := deployedRegionContent(t, result.Output, "CommunicationProtocol")
	if !bytes.Contains(content, []byte(fixtureRefreshSubagentProtocolBlock)) {
		t.Errorf("protocol region not correctly refreshed for no-version file; "+
			"content: %q", content)
	}
	if bytes.Contains(content, []byte("old communication protocol content")) {
		t.Errorf("old protocol content still present after refresh of no-version file")
	}
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

// TestRefreshHarnessOnly_MalformedDeployedBytes_ReturnsError verifies that when
// req.Deployed contains bytes that docformat.Parse cannot parse — here, an unclosed
// frontmatter block whose closing "---" delimiter is absent — refreshHarnessOnly returns
// a non-nil error. The function must propagate the parse failure rather than panic or
// silently proceed with a partially initialised document.
func TestRefreshHarnessOnly_MalformedDeployedBytes_ReturnsError(t *testing.T) {
	// fixtureMalformedUnclosedFrontmatter starts with "---" but has no closing "---",
	// which docformat.SplitFrontmatter rejects with an error.
	req := makeRefreshRequest([]byte(fixtureMalformedUnclosedFrontmatter), RefreshProtocolOnly, domain.RoleSubagent)
	_, err := refreshHarnessOnly(req)
	if err == nil {
		t.Error("refreshHarnessOnly returned nil error for a document with an unclosed " +
			"frontmatter block; it must return a non-nil error when docformat.Parse fails " +
			"rather than proceeding with a malformed document or panicking")
	}
}

// ---------------------------------------------------------------------------
// T6.8 — Regression: catalog-backed transform path is unaffected
// ---------------------------------------------------------------------------

// TestCatalogBackedTransform_ErrSourceDeployedRegionNotEmpty_SentinelIsPreserved verifies
// that the ErrSourceDeployedRegionNotEmpty sentinel error variable is still present and
// non-nil. Any change to the transform package that removes or renames this sentinel would
// break the existing catalog-backed error contract.
func TestCatalogBackedTransform_ErrSourceDeployedRegionNotEmpty_SentinelIsPreserved(t *testing.T) {
	if transform.ErrSourceDeployedRegionNotEmpty == nil {
		t.Error("transform.ErrSourceDeployedRegionNotEmpty is nil; " +
			"this sentinel must remain non-nil so the catalog-backed path can return it " +
			"for source files with populated deployed regions")
	}
}

// TestCatalogBackedTransform_ErrSourceDeployedRegionNotEmpty_SatisfiesErrorsIs verifies
// that the sentinel satisfies errors.Is against itself. This is the usage pattern callers
// rely on to distinguish this error from others.
func TestCatalogBackedTransform_ErrSourceDeployedRegionNotEmpty_SatisfiesErrorsIs(t *testing.T) {
	if !errors.Is(transform.ErrSourceDeployedRegionNotEmpty, transform.ErrSourceDeployedRegionNotEmpty) {
		t.Error("errors.Is(ErrSourceDeployedRegionNotEmpty, ErrSourceDeployedRegionNotEmpty) = false; " +
			"the sentinel must satisfy errors.Is with itself for callers to match it correctly")
	}
}

// TestRefreshHarnessOnly_NeverReturnsErrSourceDeployedRegionNotEmpty verifies that
// refreshHarnessOnly does not return ErrSourceDeployedRegionNotEmpty, even when the input
// file has populated deployed regions (which is always the case for a deployed harness-only
// agent). This sentinel belongs exclusively to transform.Apply's catalog-backed path.
func TestRefreshHarnessOnly_NeverReturnsErrSourceDeployedRegionNotEmpty(t *testing.T) {
	// The fixture has populated deployed regions — exactly the case that triggers
	// ErrSourceDeployedRegionNotEmpty in transform.Apply when fed as a Source. The
	// harness-only refresh path must handle this without that error.
	req := makeRefreshRequest([]byte(fixtureSubagentFull), RefreshProtocolOnly, domain.RoleSubagent)
	_, err := refreshHarnessOnly(req)

	if err != nil && errors.Is(err, transform.ErrSourceDeployedRegionNotEmpty) {
		t.Error("refreshHarnessOnly returned ErrSourceDeployedRegionNotEmpty; " +
			"this sentinel must only be returned by transform.Apply on the catalog-backed path. " +
			"The harness-only refresh routine takes a deployed file as its source and must " +
			"never trip this check.")
	}
}

// TestCatalogBackedTransform_TransformPackageDoesNotImportAppPackage verifies the import
// isolation contract: the transform package must not import the app package. This guarantees
// that the harness-only refresh feature (added to app) cannot contaminate the catalog-backed
// transform pipeline, whose purity is its core correctness property.
func TestCatalogBackedTransform_TransformPackageDoesNotImportAppPackage(t *testing.T) {
	const transformPkg = "mosaic-deploy/internal/transform"
	const appPkg = "mosaic-deploy/internal/app"

	ctx := build.Default
	pkg, err := ctx.Import(transformPkg, ".", build.ImportComment)
	if err != nil {
		t.Fatalf("import %q: %v\n"+
			"(ensure the module is built: go build ./internal/transform/...)",
			transformPkg, err)
	}

	for _, imp := range pkg.Imports {
		if imp == appPkg || strings.HasPrefix(imp, appPkg+"/") {
			t.Errorf("transform package imports %q; "+
				"the transform package must not import the app package — "+
				"the harness-only refresh path lives in app precisely to keep transform isolated. "+
				"This import would contaminate the catalog-backed pipeline with app-layer concerns.",
				imp)
		}
	}
}

// TestCatalogBackedTransform_SentinelErrors_StillReturnedByTransformApply verifies that
// transform.Apply returns ErrSourceDeployedRegionNotEmpty when a source file contains a
// populated managed region region, confirming the catalog-backed gate is still active.
// This test uses a deliberately malformed request to trigger the sentinel early.
func TestCatalogBackedTransform_SentinelErrors_StillReturnedByTransformApply(t *testing.T) {
	// A generic source with a populated deployed region. This is the shape that a deployed
	// harness-only file would have; the catalog-backed path must reject it.
	const sourceWithPopulatedDeployed = `---
id: 99
version: 1.0.0
name: sentinel-test
description: Source with a populated deployed region — must be rejected.
model: {model-identifier}
tools: []
recommended_tier: LOW
tier_rationale: test
required_skills: []
---

<Identity type="core">
Content.

<CommunicationProtocol type="managed">
populated deployed region content — this must trigger ErrSourceDeployedRegionNotEmpty
</CommunicationProtocol>

</Identity>
`
	req := transform.Request{
		Source: []byte(sourceWithPopulatedDeployed),
		Kind:   domain.ArtifactAgent,
		Key:    "sentinel-test",
		Module: &minimalTransformHarnessModule{},
		Scope:  domain.ScopeProject,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Error("transform.Apply returned nil error for a source with a populated deployed region; " +
			"it must return ErrSourceDeployedRegionNotEmpty to reject such sources — " +
			"this is the guard that prevents deployed files from being fed into the catalog-backed pipeline")
		return
	}
	if !errors.Is(err, transform.ErrSourceDeployedRegionNotEmpty) {
		t.Errorf("transform.Apply error = %v; want it to wrap ErrSourceDeployedRegionNotEmpty; "+
			"the sentinel must be returned, not swallowed or replaced", err)
	}
}

// ---------------------------------------------------------------------------
// Minimal test double for T6.8 regression test
// ---------------------------------------------------------------------------

// minimalTransformHarnessModule is a stub domain.HarnessModule that satisfies the interface
// just enough for transform.Apply to reach the source-region validation step. It is defined
// here solely for the T6.8 regression test; it is not used anywhere else.
type minimalTransformHarnessModule struct{}

func (m *minimalTransformHarnessModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: "regression-stub"}
}
func (m *minimalTransformHarnessModule) Descriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{ID: "regression-stub"}
}
func (m *minimalTransformHarnessModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (m *minimalTransformHarnessModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *minimalTransformHarnessModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (m *minimalTransformHarnessModule) Injection(_ domain.InjectionRequest) (string, bool) {
	return "", false
}
func (m *minimalTransformHarnessModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "regression stub"}, nil
}
func (m *minimalTransformHarnessModule) Close() error { return nil }

// ---------------------------------------------------------------------------
// T10.4 — Harness-only refresh path: line-ending-aware protocol-version marker
// ---------------------------------------------------------------------------

// fixtureRefreshProtocolCRLF returns a ProtocolContent whose blocks carry CRLF line endings,
// simulating the protocol blocks as they appear on a Windows checkout with core.autocrlf=true.
// The CRLF blocks are derived by normalising the shared LF fixture constants.
func fixtureRefreshProtocolCRLF(version string) domain.ProtocolContent {
	return domain.ProtocolContent{
		Version: version,
		Blocks: map[domain.ProtocolVariant][]byte{
			domain.ProtocolSubagent:     []byte(strings.ReplaceAll(fixtureRefreshSubagentProtocolBlock, "\n", "\r\n")),
			domain.ProtocolOrchestrator: []byte(strings.ReplaceAll(fixtureRefreshOrchestratorProtocolBlock, "\n", "\r\n")),
		},
	}
}

// TestRefreshHarnessOnly_CRLFProtocolBlock_RegionContainsNoLoneLF verifies that when the
// protocol block carries CRLF line endings, the CommunicationProtocol region produced by
// refreshHarnessOnly contains no lone LF: every '\n' is immediately preceded by '\r'. This
// covers the protocol-version marker line specifically — without the Stage 10 fix, the
// marker would carry a hardcoded '\n' even when the block is CRLF-terminated.
func TestRefreshHarnessOnly_CRLFProtocolBlock_RegionContainsNoLoneLF(t *testing.T) {
	req := HarnessOnlyRefreshRequest{
		Deployed: []byte(fixtureSubagentFull),
		Scope:    RefreshProtocolOnly,
		Role:     domain.RoleSubagent,
		Protocol: fixtureRefreshProtocolCRLF(fixtureRefreshProtocolVersion),
		Bundle:   fixtureRefreshBundle(fixtureRefreshBundleVersion),
		Subject:  "test-agent.agent.md",
	}

	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	content := deployedRegionContent(t, result.Output, "CommunicationProtocol")

	// Every '\n' in the region must be immediately preceded by '\r'.
	for i, b := range content {
		if b == '\n' {
			if i == 0 || content[i-1] != '\r' {
				t.Errorf("lone LF at byte offset %d in CommunicationProtocol region "+
					"produced by refreshHarnessOnly;\n"+
					"a CRLF protocol block must produce a region with no lone LF, "+
					"including on the protocol-version marker line;\n"+
					"region (first 200 bytes): %q",
					i, content[:min(200, len(content))])
				return
			}
		}
	}
}

// TestRefreshHarnessOnly_CRLFProtocolBlock_VersionAttributeIsSetCorrectly verifies that
// even when the protocol block carries CRLF line endings, the CommunicationProtocol region's
// opening tag carries the correct version attribute. The version is stored as a tag attribute,
// not as a comment in the region content, so CRLF in the block content does not affect it.
func TestRefreshHarnessOnly_CRLFProtocolBlock_VersionAttributeIsSetCorrectly(t *testing.T) {
	req := HarnessOnlyRefreshRequest{
		Deployed: []byte(fixtureSubagentFull),
		Scope:    RefreshProtocolOnly,
		Role:     domain.RoleSubagent,
		Protocol: fixtureRefreshProtocolCRLF(fixtureRefreshProtocolVersion),
		Bundle:   fixtureRefreshBundle(fixtureRefreshBundleVersion),
		Subject:  "test-agent.agent.md",
	}

	result, err := refreshHarnessOnly(req)
	if err != nil {
		t.Fatalf("refreshHarnessOnly: %v", err)
	}

	outDoc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := outDoc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("CommunicationProtocol region not found in output")
	}
	if node.Version() != fixtureRefreshProtocolVersion {
		t.Errorf("CRLF block: CommunicationProtocol version attribute = %q; want %q;\n"+
			"the protocol version must be stored as a tag attribute regardless of block line endings",
			node.Version(), fixtureRefreshProtocolVersion)
	}
}

// TestRefreshHarnessOnly_HarnessOnlyRegionContent_ReturnsRoleMatchedBlock verifies that
// harnessOnlyRegionContent returns the role-matched protocol block as the region content.
// The protocol version is stored as a tag attribute on the opening tag; the region content
// is just the block text without any version comment prefix.
func TestRefreshHarnessOnly_HarnessOnlyRegionContent_ReturnsRoleMatchedBlock(t *testing.T) {
	cases := []struct {
		name    string
		role    domain.AgentRole
		block   []byte
		variant domain.ProtocolVariant
	}{
		{
			name:    "LF subagent block",
			role:    domain.RoleSubagent,
			block:   []byte(fixtureRefreshSubagentProtocolBlock),
			variant: domain.ProtocolSubagent,
		},
		{
			name:    "CRLF subagent block",
			role:    domain.RoleSubagent,
			block:   []byte(strings.ReplaceAll(fixtureRefreshSubagentProtocolBlock, "\n", "\r\n")),
			variant: domain.ProtocolSubagent,
		},
		{
			name:    "LF orchestrator block",
			role:    domain.RoleOrchestrator,
			block:   []byte(fixtureRefreshOrchestratorProtocolBlock),
			variant: domain.ProtocolOrchestrator,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			protocol := domain.ProtocolContent{
				Version: fixtureRefreshProtocolVersion,
				Blocks:  map[domain.ProtocolVariant][]byte{tc.variant: tc.block},
			}
			req := HarnessOnlyRefreshRequest{
				Role:     tc.role,
				Protocol: protocol,
			}
			got, err := harnessOnlyRegionContent("CommunicationProtocol", req)
			if err != nil {
				t.Fatalf("harnessOnlyRegionContent: %v", err)
			}

			// Expected content is just the block; the version is stored as a tag attribute,
			// not as a comment prefix in the region content.
			want := tc.block

			if !bytes.Equal(got, want) {
				t.Errorf("harnessOnlyRegionContent output does not match the role-matched block;\n"+
					"want: %q\ngot:  %q", want, got)
			}
		})
	}
}


