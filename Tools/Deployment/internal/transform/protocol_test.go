package transform_test

// protocol_test.go covers protocol region delivery in the transform (Stage 4):
//
//   - Role-based variant selection: an orchestrator receives the orchestrator block;
//     a worker or utility receives the subagent block; neither block bleeds across roles.
//   - Version marker: the deployed region opens with <!-- protocol-version: X --> matching
//     the same comment shape used by workflow and infrastructure blocks.
//   - Missing-content failure: absent, empty, or whitespace-only protocol content for the
//     agent's role fails Apply with a distinct error wrapping ErrProtocolContentMissing,
//     and no partial output is returned.

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// sourceWithProtocol is a minimal valid agent source that declares a top-level
// [[DEPLOYED:CommunicationProtocol]] region, as specified by the agent source file contract.
// The region is intentionally empty in the source; the transform fills it from the request.
const sourceWithProtocol = `---
id: 99
version: 1.0.0
name: protocol-test
description: Agent for protocol region testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

[[SECTION:Identity]]
## Identity

Protocol test agent.

[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]
`

// orchestratorBlockContent and subagentBlockContent are the distinct, non-overlapping
// payloads for each protocol variant. Using content-unique strings lets each test assert
// that the correct block — and only the correct block — appears in the output.
const orchestratorBlockContent = "## Communication Protocol (Orchestrator)\n\nOrchestrator-specific protocol content.\n"
const subagentBlockContent = "## Communication Protocol (Subagent)\n\nSubagent-specific protocol content.\n"

// ---------------------------------------------------------------------------
// T4.1: Role-based variant selection
// ---------------------------------------------------------------------------

// TestProtocol_OrchestratorRoleReceivesOrchestratorBlock verifies that when the request
// carries RoleOrchestrator, the deployed CommunicationProtocol region contains the
// orchestrator protocol block and does not contain the subagent block.
func TestProtocol_OrchestratorRoleReceivesOrchestratorBlock(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleOrchestrator,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	regionContent := extractProtocolRegionContent(t, result.Output)

	if !bytes.Contains(regionContent, []byte(orchestratorBlockContent)) {
		t.Errorf("orchestrator role: protocol region does not contain orchestrator block;\ncontent: %q", regionContent)
	}
	if bytes.Contains(regionContent, []byte(subagentBlockContent)) {
		t.Errorf("orchestrator role: protocol region must not contain subagent block;\ncontent: %q", regionContent)
	}
}

// TestProtocol_WorkerRoleReceivesSubagentBlock verifies that when the request carries
// RoleWorker, the deployed CommunicationProtocol region contains the subagent block and
// does not contain the orchestrator block.
func TestProtocol_WorkerRoleReceivesSubagentBlock(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	regionContent := extractProtocolRegionContent(t, result.Output)

	if !bytes.Contains(regionContent, []byte(subagentBlockContent)) {
		t.Errorf("worker role: protocol region does not contain subagent block;\ncontent: %q", regionContent)
	}
	if bytes.Contains(regionContent, []byte(orchestratorBlockContent)) {
		t.Errorf("worker role: protocol region must not contain orchestrator block;\ncontent: %q", regionContent)
	}
}

// TestProtocol_UtilityRoleReceivesSubagentBlock verifies that the utility role maps to the
// subagent variant, not the orchestrator variant. Both worker and utility are non-orchestrator
// roles and share the subagent block.
func TestProtocol_UtilityRoleReceivesSubagentBlock(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleUtility,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	regionContent := extractProtocolRegionContent(t, result.Output)

	if !bytes.Contains(regionContent, []byte(subagentBlockContent)) {
		t.Errorf("utility role: protocol region does not contain subagent block;\ncontent: %q", regionContent)
	}
	if bytes.Contains(regionContent, []byte(orchestratorBlockContent)) {
		t.Errorf("utility role: protocol region must not contain orchestrator block;\ncontent: %q", regionContent)
	}
}

// TestProtocol_OrchestratorAndSubagentOutputsDiffer verifies that applying the transform
// with two different roles produces two different protocol region contents. This is the
// cross-contamination guard: the same source document with different roles must produce
// variant-specific, non-overlapping protocol payloads.
func TestProtocol_OrchestratorAndSubagentOutputsDiffer(t *testing.T) {
	makeReq := func(role domain.AgentRole) transform.Request {
		return transform.Request{
			Source:   []byte(sourceWithProtocol),
			Kind:     domain.ArtifactAgent,
			Key:      "protocol-test",
			Module:   newFixtureModule(t),
			Model:    fixtureModel(),
			Scope:    domain.ScopeProject,
			Role:     role,
			Protocol: fixtureProtocol("1.9"),
		}
	}

	orchestratorResult, err := transform.Apply(makeReq(domain.RoleOrchestrator))
	if err != nil {
		t.Fatalf("Apply (orchestrator): %v", err)
	}
	workerResult, err := transform.Apply(makeReq(domain.RoleWorker))
	if err != nil {
		t.Fatalf("Apply (worker): %v", err)
	}

	orchestratorContent := extractProtocolRegionContent(t, orchestratorResult.Output)
	workerContent := extractProtocolRegionContent(t, workerResult.Output)

	if bytes.Equal(orchestratorContent, workerContent) {
		t.Error("orchestrator and worker roles produced identical protocol region content; variant selection is not operating")
	}
}

// ---------------------------------------------------------------------------
// T4.2: Version marker
// ---------------------------------------------------------------------------

// TestProtocol_VersionMarkerAppearsAsFirstLineOfRegion verifies that the protocol version
// comment is the first content line of the deployed CommunicationProtocol region, before
// the protocol block text. This mirrors the established <!-- infra-version: X --> and
// <!-- workflow-version: X --> conventions.
func TestProtocol_VersionMarkerAppearsAsFirstLineOfRegion(t *testing.T) {
	const version = "1.9"

	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol(version),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	regionContent := extractProtocolRegionContent(t, result.Output)

	// The version comment must be present somewhere in the region.
	expectedComment := transform.ProtocolVersionComment(version)
	if !bytes.Contains(regionContent, []byte(expectedComment)) {
		t.Errorf("protocol region does not contain version comment %q;\ncontent: %q",
			expectedComment, regionContent)
	}

	// The version comment must appear as the first line (the region content starts with it).
	if !bytes.HasPrefix(regionContent, []byte(expectedComment)) {
		t.Errorf("version comment is not the first line of the protocol region;\nwant prefix: %q\ngot content: %q",
			expectedComment, regionContent)
	}
}

// TestProtocol_VersionMarkerReflectsSuppliedVersion verifies that the version stamped into
// the region matches Protocol.Version from the request — not a hardcoded default.
func TestProtocol_VersionMarkerReflectsSuppliedVersion(t *testing.T) {
	cases := []string{"1.0", "2.3", "1.11", "42"}
	for _, version := range cases {
		t.Run("version="+version, func(t *testing.T) {
			req := transform.Request{
				Source:   []byte(sourceWithProtocol),
				Kind:     domain.ArtifactAgent,
				Key:      "protocol-test",
				Module:   newFixtureModule(t),
				Model:    fixtureModel(),
				Scope:    domain.ScopeProject,
				Role:     domain.RoleWorker,
				Protocol: fixtureProtocol(version),
			}

			result, err := transform.Apply(req)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			regionContent := extractProtocolRegionContent(t, result.Output)
			expectedComment := transform.ProtocolVersionComment(version)

			if !bytes.Contains(regionContent, []byte(expectedComment)) {
				t.Errorf("protocol region does not contain expected version comment %q;\ncontent: %q",
					expectedComment, regionContent)
			}
		})
	}
}

// TestProtocol_VersionMarkerMatchesProtocolVersionCommentHelper verifies the round-trip
// symmetry between the emitter (ProtocolVersionComment) and the expected comment shape.
// This pins the exact output format so a future change to the shape is caught by both
// the emitter test and any reader that parses the same pattern.
func TestProtocol_VersionMarkerMatchesProtocolVersionCommentHelper(t *testing.T) {
	const version = "1.9"
	want := "<!-- protocol-version: 1.9 -->\n"
	got := transform.ProtocolVersionComment(version)
	if got != want {
		t.Errorf("ProtocolVersionComment(%q): want %q, got %q", version, want, got)
	}
}

// ---------------------------------------------------------------------------
// T4.4: Missing-content failure mode
// ---------------------------------------------------------------------------

// TestProtocol_AbsentBlockForRoleFailsWithErrProtocolContentMissing verifies that when
// ProtocolContent.Blocks does not contain an entry for the agent's role variant, Apply
// returns an error wrapping ErrProtocolContentMissing and no partial output.
func TestProtocol_AbsentBlockForRoleFailsWithErrProtocolContentMissing(t *testing.T) {
	// Supply only the subagent block; the orchestrator block is absent.
	protocol := domain.ProtocolContent{
		Version: "1.9",
		Blocks: map[domain.ProtocolVariant][]byte{
			domain.ProtocolSubagent: []byte(subagentBlockContent),
			// ProtocolOrchestrator is intentionally missing.
		},
	}

	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleOrchestrator,
		Protocol: protocol,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply: expected error for absent orchestrator block, got nil")
	}
	if !errors.Is(err, transform.ErrProtocolContentMissing) {
		t.Errorf("Apply error does not wrap ErrProtocolContentMissing; got: %v", err)
	}
}

// TestProtocol_EmptyBlockForRoleFailsWithErrProtocolContentMissing verifies that an
// explicitly empty byte slice for the agent's role variant is treated as missing and
// causes Apply to fail with ErrProtocolContentMissing.
func TestProtocol_EmptyBlockForRoleFailsWithErrProtocolContentMissing(t *testing.T) {
	protocol := domain.ProtocolContent{
		Version: "1.9",
		Blocks: map[domain.ProtocolVariant][]byte{
			domain.ProtocolSubagent:     []byte{}, // empty — treated as absent
			domain.ProtocolOrchestrator: []byte(orchestratorBlockContent),
		},
	}

	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	}

	_, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply: expected error for empty subagent block, got nil")
	}
	if !errors.Is(err, transform.ErrProtocolContentMissing) {
		t.Errorf("Apply error does not wrap ErrProtocolContentMissing; got: %v", err)
	}
}

// TestProtocol_WhitespaceOnlyBlockForRoleFailsWithErrProtocolContentMissing verifies that
// a block containing only whitespace is treated as empty and causes Apply to fail with
// ErrProtocolContentMissing. Whitespace-only content would produce an invisible region.
func TestProtocol_WhitespaceOnlyBlockForRoleFailsWithErrProtocolContentMissing(t *testing.T) {
	cases := []string{" ", "\n", "\t", "  \n  \t  "}
	for _, ws := range cases {
		t.Run("whitespace="+strings.TrimSpace(ws+"_"), func(t *testing.T) {
			protocol := domain.ProtocolContent{
				Version: "1.9",
				Blocks: map[domain.ProtocolVariant][]byte{
					domain.ProtocolSubagent:     []byte(ws),
					domain.ProtocolOrchestrator: []byte(orchestratorBlockContent),
				},
			}

			req := transform.Request{
				Source:   []byte(sourceWithProtocol),
				Kind:     domain.ArtifactAgent,
				Key:      "protocol-test",
				Module:   newFixtureModule(t),
				Model:    fixtureModel(),
				Scope:    domain.ScopeProject,
				Role:     domain.RoleWorker,
				Protocol: protocol,
			}

			_, err := transform.Apply(req)
			if err == nil {
				t.Fatalf("Apply: expected error for whitespace-only block %q, got nil", ws)
			}
			if !errors.Is(err, transform.ErrProtocolContentMissing) {
				t.Errorf("Apply error does not wrap ErrProtocolContentMissing for %q; got: %v", ws, err)
			}
		})
	}
}

// TestProtocol_MissingContentFailsWithNoPartialOutput verifies that when Apply fails due
// to missing protocol content, the returned Result is the zero value — no partial output
// is returned. This upholds the contract that Apply either fully succeeds or fails clean.
func TestProtocol_MissingContentFailsWithNoPartialOutput(t *testing.T) {
	protocol := domain.ProtocolContent{
		Version: "1.9",
		Blocks:  map[domain.ProtocolVariant][]byte{
			// Neither variant is provided — any role will fail.
		},
	}

	req := transform.Request{
		Source:   []byte(sourceWithProtocol),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	}

	result, err := transform.Apply(req)
	if err == nil {
		t.Fatal("Apply: expected error, got nil")
	}
	if result.Output != nil {
		t.Errorf("Apply returned non-nil Output on error; partial output must not be returned (got %d bytes)", len(result.Output))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// extractProtocolRegionContent parses the output document and returns the inner content
// bytes of the [[DEPLOYED:CommunicationProtocol]] region. The test is failed if the
// region cannot be located.
func extractProtocolRegionContent(t *testing.T, output []byte) []byte {
	t.Helper()
	doc, err := docformat.Parse(output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("[[DEPLOYED:CommunicationProtocol]] region absent from output")
	}
	return node.Content()
}

// ---------------------------------------------------------------------------
// Document order: CommunicationProtocol region interleaved with other regions
// ---------------------------------------------------------------------------

// sourceWithProtocolInterleaved is an agent source that declares a CommunicationProtocol
// region interleaved between two user-owned injection regions. The order in the document is:
//
//  1. [[INJECTION:IdentityExtension]] — user-owned, inside the Identity section
//  2. [[DEPLOYED:CommunicationProtocol]] — tool-managed, at the top level between sections
//  3. [[INJECTION:ArtifactProvenanceExtension]] — user-owned, inside the ArtifactProvenance section
//
// This layout mirrors the canonical MOSAIC agent structure and pins the expected document
// ordering of the protocol region in Report.Regions.
const sourceWithProtocolInterleaved = `---
id: 88
version: 1.0.0
name: protocol-order-test
description: Agent for testing CommunicationProtocol region ordering
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: ordering test
required_skills: []
---

[[SECTION:Identity]]
## Identity

Protocol order test agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]

[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]

[[SECTION:ArtifactProvenance]]
## ArtifactProvenance

[[INJECTION:ArtifactProvenanceExtension]]
[[/INJECTION:ArtifactProvenanceExtension]]

[[/SECTION:ArtifactProvenance]]
`

// TestProtocol_DocumentOrderProtocolRegionInterleaved verifies that the CommunicationProtocol
// RegionOutcome appears in Report.Regions at the position matching its location in the source
// document — after IdentityExtension and before ArtifactProvenanceExtension. This pins the
// protocol region's ordering so that a future refactor of the region-processing loop is caught
// by a test that directly owns the protocol stage's ordering contract.
func TestProtocol_DocumentOrderProtocolRegionInterleaved(t *testing.T) {
	req := transform.Request{
		Source:   []byte(sourceWithProtocolInterleaved),
		Kind:     domain.ArtifactAgent,
		Key:      "protocol-order-test",
		Module:   newFixtureModule(t),
		Model:    fixtureModel(),
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: fixtureProtocol("1.9"),
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	regions := result.Report.Regions
	if len(regions) < 3 {
		t.Fatalf("Report.Regions: want at least 3 entries (IdentityExtension, CommunicationProtocol, ArtifactProvenanceExtension), got %d: %v",
			len(regions), regionNames(regions))
	}

	// Find each region's position in the report.
	idxIdentity := -1
	idxProtocol := -1
	idxProvenance := -1
	for i, r := range regions {
		switch r.Name {
		case "IdentityExtension":
			idxIdentity = i
		case "CommunicationProtocol":
			idxProtocol = i
		case "ArtifactProvenanceExtension":
			idxProvenance = i
		}
	}

	if idxIdentity == -1 {
		t.Error("Report.Regions does not contain IdentityExtension")
	}
	if idxProtocol == -1 {
		t.Error("Report.Regions does not contain CommunicationProtocol")
	}
	if idxProvenance == -1 {
		t.Error("Report.Regions does not contain ArtifactProvenanceExtension")
	}

	if idxIdentity == -1 || idxProtocol == -1 || idxProvenance == -1 {
		t.FailNow() // positions unknown; ordering assertions below are meaningless
	}

	// Document order: IdentityExtension < CommunicationProtocol < ArtifactProvenanceExtension.
	if idxProtocol <= idxIdentity {
		t.Errorf("CommunicationProtocol (index %d) must appear after IdentityExtension (index %d) in Report.Regions",
			idxProtocol, idxIdentity)
	}
	if idxProtocol >= idxProvenance {
		t.Errorf("CommunicationProtocol (index %d) must appear before ArtifactProvenanceExtension (index %d) in Report.Regions",
			idxProtocol, idxProvenance)
	}
}

// ---------------------------------------------------------------------------
// VariantForRole: fallback for unknown roles
// ---------------------------------------------------------------------------

// TestProtocol_VariantForRole_UnknownRoleFallsBackToSubagent verifies that VariantForRole
// maps any role value outside the three known constants (RoleOrchestrator, RoleWorker,
// RoleUtility) to ProtocolSubagent. This exercises the fallback branch and confirms that
// the many-to-one role→variant mapping is resilient to hypothetical future role additions:
// any new role will receive the subagent variant until an explicit mapping is added.
func TestProtocol_VariantForRole_UnknownRoleFallsBackToSubagent(t *testing.T) {
	// Use a role value that is not one of the three declared constants. This simulates a
	// hypothetical future role that has not yet been given an explicit protocol mapping.
	unknownRole := domain.AgentRole("hypothetical-future-role")

	got := domain.VariantForRole(unknownRole)
	if got != domain.ProtocolSubagent {
		t.Errorf("VariantForRole(%q): want %q (fallback for unknown roles), got %q",
			unknownRole, domain.ProtocolSubagent, got)
	}
}
