package app_test

// e2e_update_contract_test.go verifies the update-mode marker ownership contract:
//
//   (a) On create, [[DEPLOYED:]] regions are filled by the appropriate generator and
//       [[INJECTION:]] regions are left empty.
//
//   (b) On update, [[DEPLOYED:]] regions are regenerated (replacing any previous content),
//       and [[INJECTION:]] regions from the previously-deployed file survive byte-identically
//       even when the user placed content in them by hand.
//
// This is the mechanised form of the promise "tool-managed regions are replaced on every run;
// user-owned regions are yours to keep." The correctness of this separation is the foundation
// of the whole marker vocabulary split.
//
// These tests call transform.Apply directly using a synthetic agent source and a minimal
// harness stub so the assertions focus on the update-mode region routing invariant, not on
// harness-specific field shaping.

import (
	"bytes"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Local harness stub for update contract tests
// ---------------------------------------------------------------------------

// updateContractModule is a minimal domain.HarnessModule that fills HarnessConstraints and
// LanguagePatterns as tool-managed injections but leaves all user-owned injections unfilled.
// Using a local stub avoids depending on the real claudecode module's runtime content.
type updateContractModule struct{}

func (m *updateContractModule) Ref() domain.HarnessRef {
	return domain.HarnessRef{ID: "update-contract-test", Tier: domain.TierBuiltin, Usable: true}
}
func (m *updateContractModule) Descriptor() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{ID: "update-contract-test"}
}
func (m *updateContractModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (m *updateContractModule) Frontmatter(_ domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *updateContractModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (m *updateContractModule) Injection(req domain.InjectionRequest) (string, bool) {
	switch req.Name {
	case "HarnessConstraints":
		// Harness-managed: fill with a known string so assertions can check regeneration.
		return "Harness constraint: do not make assumptions.", true
	case "LanguagePatterns":
		// Harness-managed: fill with empty content (declared but not populated).
		return "", true
	default:
		// User-owned and anything else: not filled by the harness.
		return "", false
	}
}
func (m *updateContractModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "test harness"}, nil
}
func (m *updateContractModule) Close() error { return nil }

// ---------------------------------------------------------------------------
// Synthetic agent source for update contract tests
// ---------------------------------------------------------------------------

// updateContractSource is a minimal agent source file that carries one [[DEPLOYED:]] region
// (HarnessConstraints, tool-managed) and one [[INJECTION:]] region (IdentityExtension,
// user-owned). The arrangement matches the real generic agent structure so the test exercises
// the same update-mode code path that production deploys use.
const updateContractSource = `---
id: 99
version: 1.0.0
name: update-contract-agent
description: Synthetic agent for update contract testing
model: {model-identifier}
tools: [file_read]
recommended_tier: LOW
tier_rationale: minimal
required_skills: []
---

[[SECTION:Identity]]
Static prose that must not change.
[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[/SECTION:Identity]]

[[SECTION:Constraints]]
Static constraints prose.
[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[/SECTION:Constraints]]
`

// updateContractModel is a fixed model selection for the update contract tests.
var updateContractModel = domain.ModelSelection{
	ModelID: "test-model",
	Origin:  domain.OriginHarnessList,
}

// applyUpdateContract calls transform.Apply for the synthetic source with the given deployed
// (previously-deployed) bytes. Pass nil for deployed to trigger create mode.
func applyUpdateContract(t *testing.T, deployed []byte) []byte {
	t.Helper()
	req := transform.Request{
		Source:   []byte(updateContractSource),
		Kind:     domain.ArtifactAgent,
		Key:      "update-contract-agent",
		Module:   &updateContractModule{},
		Model:    updateContractModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: domain.ProtocolContent{Version: "1.9"}, // protocol present but no [[DEPLOYED:CommunicationProtocol]] in source
		Deployed: deployed,
	}
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}
	return result.Output
}

// ---------------------------------------------------------------------------
// (a) Create mode — [[DEPLOYED:]] filled, [[INJECTION:]] empty
// ---------------------------------------------------------------------------

// TestE2E_UpdateContract_CreateMode_DeployedRegionIsFilled verifies that on a first deploy
// (no previously-deployed file), the [[DEPLOYED:HarnessConstraints]] region is filled with
// content from the harness module. The empty protocol content is acceptable here because the
// synthetic source has no [[DEPLOYED:CommunicationProtocol]] region.
func TestE2E_UpdateContract_CreateMode_DeployedRegionIsFilled(t *testing.T) {
	output := applyUpdateContract(t, nil /* create mode */)

	wantContent := []byte("Harness constraint: do not make assumptions.")
	if !bytes.Contains(output, wantContent) {
		t.Errorf("create mode: [[DEPLOYED:HarnessConstraints]] not filled with harness content;\n"+
			"want %q in output\n\noutput (first 800 bytes):\n%s",
			wantContent, e2eProtocolTruncate(output, 800))
	}
}

// TestE2E_UpdateContract_CreateMode_InjectionRegionIsEmpty verifies that on a first deploy,
// the [[INJECTION:IdentityExtension]] region is left empty (no content between its tags).
func TestE2E_UpdateContract_CreateMode_InjectionRegionIsEmpty(t *testing.T) {
	output := applyUpdateContract(t, nil /* create mode */)

	// The entire injection pair should appear with nothing (or only whitespace) between them.
	openTag := []byte("[[INJECTION:IdentityExtension]]")
	closeTag := []byte("[[/INJECTION:IdentityExtension]]")

	openIdx := bytes.Index(output, openTag)
	if openIdx < 0 {
		t.Fatalf("create mode: [[INJECTION:IdentityExtension]] open tag not found in output")
	}
	afterOpen := output[openIdx+len(openTag):]
	closeIdx := bytes.Index(afterOpen, closeTag)
	if closeIdx < 0 {
		t.Fatalf("create mode: [[/INJECTION:IdentityExtension]] close tag not found after open tag")
	}
	between := afterOpen[:closeIdx]
	if len(bytes.TrimSpace(between)) > 0 {
		t.Errorf("create mode: [[INJECTION:IdentityExtension]] is not empty on create;\n"+
			"content between tags (first 200 bytes): %q\n"+
			"user-owned injections must be left empty on first deploy", e2eProtocolTruncate(between, 200))
	}
}

// ---------------------------------------------------------------------------
// (b) Update mode — [[DEPLOYED:]] regenerated, [[INJECTION:]] content preserved
// ---------------------------------------------------------------------------

// TestE2E_UpdateContract_UpdateMode_InjectionContentSurvivesUpdateByteIdentically verifies
// that content placed by the user inside [[INJECTION:IdentityExtension]] in a deployed file
// is preserved byte-identically when the agent is updated. The injection content must not be
// altered, trimmed, or reformatted in any way.
func TestE2E_UpdateContract_UpdateMode_InjectionContentSurvivesUpdateByteIdentically(t *testing.T) {
	// First deploy — produces an output with empty IdentityExtension.
	firstOutput := applyUpdateContract(t, nil)

	// Simulate the user adding personalised content to the injection region.
	const userContent = "\nMy custom identity extension — placed by hand.\n\nSecond paragraph.\n"
	deployed := simulateUserEdit(t, firstOutput,
		"[[INJECTION:IdentityExtension]]", "[[/INJECTION:IdentityExtension]]",
		userContent)

	// Second deploy (update) — the deployed file is the simulated user-edited version.
	secondOutput := applyUpdateContract(t, deployed)

	if !bytes.Contains(secondOutput, []byte(userContent)) {
		t.Errorf("update mode: user-placed injection content did not survive update;\n"+
			"want %q in [[INJECTION:IdentityExtension]]\n\noutput (first 1200 bytes):\n%s",
			userContent, e2eProtocolTruncate(secondOutput, 1200))
	}
}

// TestE2E_UpdateContract_UpdateMode_DeployedRegionIsRegeneratedOnUpdate verifies that
// [[DEPLOYED:HarnessConstraints]] is regenerated on update even when the deployed file
// already carried the correct harness content. The deployment tool always overwrites
// tool-managed regions to guarantee freshness.
func TestE2E_UpdateContract_UpdateMode_DeployedRegionIsRegeneratedOnUpdate(t *testing.T) {
	// First deploy.
	firstOutput := applyUpdateContract(t, nil)

	// Simulate a user (or attacker) overwriting the deployed HarnessConstraints content.
	const tampered = "\nThis HarnessConstraints content was tampered with by the user.\n"
	deployed := simulateUserEdit(t, firstOutput,
		"[[DEPLOYED:HarnessConstraints]]", "[[/DEPLOYED:HarnessConstraints]]",
		tampered)

	if !bytes.Contains(deployed, []byte(tampered)) {
		t.Fatal("simulation error: tampered content not present in the simulated deployed file")
	}

	// Second deploy — tool-managed content must be regenerated.
	secondOutput := applyUpdateContract(t, deployed)

	if bytes.Contains(secondOutput, []byte(tampered)) {
		t.Errorf("update mode: tampered [[DEPLOYED:HarnessConstraints]] content survived update;\n"+
			"tool-managed regions must be replaced on every deploy, not preserved from the deployed file")
	}
	wantContent := []byte("Harness constraint: do not make assumptions.")
	if !bytes.Contains(secondOutput, wantContent) {
		t.Errorf("update mode: [[DEPLOYED:HarnessConstraints]] not refilled with current harness content after update;\n"+
			"want %q in output\n\noutput (first 800 bytes):\n%s",
			wantContent, e2eProtocolTruncate(secondOutput, 800))
	}
}

// ---------------------------------------------------------------------------
// Helper — simulateUserEdit
// ---------------------------------------------------------------------------

// simulateUserEdit returns a copy of data where the bytes between the first occurrence of
// openTag and the matching closeTag are replaced by replacement. The tags themselves are
// preserved so the document structure remains valid for the next transform pass.
func simulateUserEdit(t *testing.T, data []byte, openTag, closeTag, replacement string) []byte {
	t.Helper()
	openBytes := []byte(openTag)
	closeBytes := []byte(closeTag)

	openIdx := bytes.Index(data, openBytes)
	if openIdx < 0 {
		t.Fatalf("simulateUserEdit: open tag %q not found in data", openTag)
	}
	afterOpen := data[openIdx+len(openBytes):]
	closeIdx := bytes.Index(afterOpen, closeBytes)
	if closeIdx < 0 {
		t.Fatalf("simulateUserEdit: close tag %q not found after open tag %q", closeTag, openTag)
	}

	var buf []byte
	buf = append(buf, data[:openIdx+len(openBytes)]...)
	buf = append(buf, []byte(replacement)...)
	buf = append(buf, afterOpen[closeIdx:]...)
	return buf
}
