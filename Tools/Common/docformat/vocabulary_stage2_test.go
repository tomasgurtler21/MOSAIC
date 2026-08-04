package docformat_test

// Tests for Stage 2 vocabulary split: tool-managed vs user-owned registries,
// CanonicalOrder, CanonicalDeployed, DeployedParent, ExpectedMarker,
// ClassifyRegion, and the InjectionProtocol class constant.
//
// Coverage (split registries and classification):
//   - CanonicalSections contains 7 entries with CommunicationProtocol absent.
//   - CanonicalSections is the authoritative 7-name list in canonical order.
//   - CanonicalOrder contains 8 entries with CommunicationProtocol at index 1.
//   - CanonicalOrder is the authoritative 8-slot list in canonical document order.
//   - CanonicalDeployed lists all 6 tool-managed boundary names.
//   - CanonicalInjections lists exactly 8 user-owned names.
//   - CanonicalInjections includes all expected user-owned names.
//   - CanonicalInjections does not contain ProtocolExtension.
//   - CanonicalInjections does not contain any tool-managed name.
//   - The CanonicalDeployed and CanonicalInjections registries are disjoint.
//   - DeployedParent maps each tool-managed name to its required parent section.
//   - InjectionParent maps each user-owned name to its required parent section.
//   - InjectionParent does not contain ProtocolExtension.
//   - InjectionParent does not contain any tool-managed name.
//   - ExpectedMarker returns (NodeDeployed, true) for every tool-managed name.
//   - ExpectedMarker returns (NodeInjection, true) for every user-owned name.
//   - ExpectedMarker returns known=false for ProtocolExtension and unknown names.
//   - ClassifyRegion returns InjectionProtocol for CommunicationProtocol under NodeDeployed.
//   - ClassifyRegion returns InjectionHarness for harness names under NodeDeployed.
//   - ClassifyRegion returns InjectionWorkflow for AvailableWorkflows under NodeDeployed.
//   - ClassifyRegion returns InjectionInfrastructure for InfrastructureAgents under NodeDeployed.
//   - ClassifyRegion returns InjectionProject for all user-owned names under NodeInjection.
//   - ClassifyRegion returns an error wrapping ErrMarkerMismatch for a tool-managed name under
//     NodeInjection.
//   - ClassifyRegion returns an error wrapping ErrMarkerMismatch for a user-owned name under
//     NodeDeployed.
//   - ClassifyRegion returns an error wrapping ErrUnknownDeployedName for an unrecognised name
//     under NodeDeployed.
//   - ClassifyRegion returns InjectionProject and a nil error for an unrecognised name under
//     NodeInjection, preserving the existing default behaviour.
//   - InjectionProtocol constant has value "protocol".
//   - InfrastructureAgents is a fully declared tool-managed name with a required parent section
//     and is no longer reported as unknown by ExpectedMarker.

import (
	"errors"
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// ---------------------------------------------------------------------------
// CanonicalSections — 7 entries, CommunicationProtocol absent
// ---------------------------------------------------------------------------

func TestCanonicalSections_Stage2_ContainsSevenSections(t *testing.T) {
	// After Stage 2 CommunicationProtocol is removed from CanonicalSections and
	// becomes a tool-managed boundary name, leaving exactly 7 section slots.
	got := docformat.CanonicalSections
	if len(got) != 7 {
		t.Fatalf("CanonicalSections length: want 7, got %d: %v", len(got), got)
	}
}

func TestCanonicalSections_Stage2_DoesNotContainCommunicationProtocol(t *testing.T) {
	// CommunicationProtocol must not appear in CanonicalSections after Stage 2;
	// it is a tool-managed deployed boundary, not a section.
	for i, s := range docformat.CanonicalSections {
		if s == "CommunicationProtocol" {
			t.Errorf(
				"CanonicalSections[%d] is %q — CommunicationProtocol must not be in CanonicalSections; it is a tool-managed boundary name declared with [[DEPLOYED:]]",
				i, s,
			)
		}
	}
}

func TestCanonicalSections_Stage2_FullSevenEntryOrder(t *testing.T) {
	// This is the authoritative lockstep contract with boundary_constants.py after Stage 2.
	// CommunicationProtocol has been removed; the remaining seven sections keep their
	// relative order.
	want := []string{
		"Identity",
		"ArtifactProvenance",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}
	got := docformat.CanonicalSections
	if len(got) != len(want) {
		t.Fatalf("CanonicalSections length: want %d, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("CanonicalSections[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

// ---------------------------------------------------------------------------
// CanonicalOrder — 8 slots, CommunicationProtocol at index 1
// ---------------------------------------------------------------------------

func TestCanonicalOrder_ContainsEightSlots(t *testing.T) {
	// CanonicalOrder lists the eight document positions in required order.
	// Slot 1 is CommunicationProtocol (a top-level deployed boundary); every other
	// slot is a section name.
	got := docformat.CanonicalOrder
	if len(got) != 8 {
		t.Fatalf("CanonicalOrder length: want 8, got %d: %v", len(got), got)
	}
}

func TestCanonicalOrder_IndexZero_IsIdentity(t *testing.T) {
	got := docformat.CanonicalOrder
	if len(got) == 0 {
		t.Fatal("CanonicalOrder is empty")
	}
	if got[0] != "Identity" {
		t.Errorf("CanonicalOrder[0]: want %q, got %q", "Identity", got[0])
	}
}

func TestCanonicalOrder_IndexOne_IsCommunicationProtocol(t *testing.T) {
	// Slot 1 is satisfied by a top-level [[DEPLOYED:CommunicationProtocol]] boundary.
	got := docformat.CanonicalOrder
	if len(got) <= 1 {
		t.Fatalf("CanonicalOrder too short to have index 1: %v", got)
	}
	if got[1] != "CommunicationProtocol" {
		t.Errorf("CanonicalOrder[1]: want %q, got %q", "CommunicationProtocol", got[1])
	}
}

func TestCanonicalOrder_FullEightSlotList(t *testing.T) {
	// This is the authoritative 8-slot canonical document order contract.
	want := []string{
		"Identity",
		"CommunicationProtocol",
		"ArtifactProvenance",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}
	got := docformat.CanonicalOrder
	if len(got) != len(want) {
		t.Fatalf("CanonicalOrder length: want %d, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("CanonicalOrder[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

// ---------------------------------------------------------------------------
// CanonicalDeployed — 6 tool-managed names
// ---------------------------------------------------------------------------

func TestCanonicalDeployed_ContainsSixToolManagedNames(t *testing.T) {
	got := docformat.CanonicalDeployed
	if len(got) != 6 {
		t.Fatalf("CanonicalDeployed length: want 6, got %d: %v", len(got), got)
	}
}

func TestCanonicalDeployed_IncludesAllExpectedToolManagedNames(t *testing.T) {
	wantNames := []string{
		"CommunicationProtocol",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"LanguagePatterns",
		"HarnessConstraints",
		"CustomConstraints",
	}
	nameSet := make(map[string]bool, len(docformat.CanonicalDeployed))
	for _, n := range docformat.CanonicalDeployed {
		nameSet[n] = true
	}
	for _, want := range wantNames {
		if !nameSet[want] {
			t.Errorf("CanonicalDeployed is missing expected tool-managed name %q; got: %v", want, docformat.CanonicalDeployed)
		}
	}
}

func TestCanonicalDeployed_DoesNotContainProtocolExtension(t *testing.T) {
	// ProtocolExtension is removed entirely from all registries in Stage 2.
	for _, n := range docformat.CanonicalDeployed {
		if n == "ProtocolExtension" {
			t.Errorf("CanonicalDeployed must not contain ProtocolExtension — it is removed from all registries")
		}
	}
}

// ---------------------------------------------------------------------------
// CanonicalInjections — 8 user-owned names
// ---------------------------------------------------------------------------

func TestCanonicalInjections_Stage2_ContainsEightUserOwnedNames(t *testing.T) {
	// Tool-managed names and ProtocolExtension are removed from CanonicalInjections,
	// leaving exactly the 8 user-owned names.
	got := docformat.CanonicalInjections
	if len(got) != 8 {
		t.Fatalf("CanonicalInjections length: want 8, got %d: %v", len(got), got)
	}
}

func TestCanonicalInjections_Stage2_IncludesAllUserOwnedNames(t *testing.T) {
	// All 8 user-owned names must be present in CanonicalInjections.
	wantNames := []string{
		"IdentityExtension",
		"ArtifactProvenanceExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"SeverityThresholds",
		"SeverityDefinitions",
		"ErrorHandlingExtension",
		"ContextLimits",
	}
	nameSet := make(map[string]bool, len(docformat.CanonicalInjections))
	for _, n := range docformat.CanonicalInjections {
		nameSet[n] = true
	}
	for _, want := range wantNames {
		if !nameSet[want] {
			t.Errorf("CanonicalInjections is missing expected user-owned name %q; got: %v", want, docformat.CanonicalInjections)
		}
	}
}

func TestCanonicalInjections_Stage2_DoesNotContainProtocolExtension(t *testing.T) {
	// ProtocolExtension is removed entirely from all registries in Stage 2.
	for _, n := range docformat.CanonicalInjections {
		if n == "ProtocolExtension" {
			t.Errorf("CanonicalInjections must not contain ProtocolExtension — it is removed from all registries")
		}
	}
}

func TestCanonicalInjections_Stage2_DoesNotContainToolManagedNames(t *testing.T) {
	// Tool-managed names must not appear in the user-owned registry.
	toolManaged := []string{
		"HarnessConstraints",
		"CustomConstraints",
		"LanguagePatterns",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"CommunicationProtocol",
	}
	injSet := make(map[string]bool, len(docformat.CanonicalInjections))
	for _, n := range docformat.CanonicalInjections {
		injSet[n] = true
	}
	for _, toolName := range toolManaged {
		if injSet[toolName] {
			t.Errorf("CanonicalInjections must not contain tool-managed name %q (it belongs in CanonicalDeployed)", toolName)
		}
	}
}

// ---------------------------------------------------------------------------
// Disjointness — no name in both registries
// ---------------------------------------------------------------------------

func TestRegistries_Stage2_AreDisjoint_NoNameInBoth(t *testing.T) {
	// The tool-managed and user-owned registries must not share any name.
	deployedSet := make(map[string]bool, len(docformat.CanonicalDeployed))
	for _, n := range docformat.CanonicalDeployed {
		deployedSet[n] = true
	}
	for _, injName := range docformat.CanonicalInjections {
		if deployedSet[injName] {
			t.Errorf(
				"name %q appears in both CanonicalDeployed and CanonicalInjections — registries must be disjoint",
				injName,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// DeployedParent — tool-managed parent mapping
// ---------------------------------------------------------------------------

func TestDeployedParent_IsNotNil(t *testing.T) {
	if docformat.DeployedParent == nil {
		t.Fatal("DeployedParent is nil — must be populated before first use")
	}
}

func TestDeployedParent_MapsEachToolManagedNameToItsRequiredParent(t *testing.T) {
	// An empty string value means the boundary must appear at body top level.
	wantMap := map[string]string{
		"CommunicationProtocol": "", // top level — empty means body top level
		"AvailableWorkflows":    "Identity",
		"InfrastructureAgents": "Identity",
		"LanguagePatterns":     "Capabilities",
		"HarnessConstraints":   "Constraints",
		"CustomConstraints":    "Constraints",
	}
	got := docformat.DeployedParent
	if got == nil {
		t.Fatal("DeployedParent is nil")
	}
	for name, wantParent := range wantMap {
		gotParent, ok := got[name]
		if !ok {
			t.Errorf("DeployedParent missing entry for %q", name)
		} else if gotParent != wantParent {
			t.Errorf("DeployedParent[%q]: want %q, got %q", name, wantParent, gotParent)
		}
	}
}

// ---------------------------------------------------------------------------
// InjectionParent — user-owned parent mapping (Stage 2 values)
// ---------------------------------------------------------------------------

func TestInjectionParent_Stage2_MapsEachUserOwnedNameToItsRequiredParent(t *testing.T) {
	// Only user-owned names may appear in InjectionParent after Stage 2.
	wantMap := map[string]string{
		"IdentityExtension":           "Identity",
		"ArtifactProvenanceExtension": "ArtifactProvenance",
		"CodebaseContext":             "Capabilities",
		"OutputArtifactTemplate":     "Capabilities",
		"SeverityThresholds":         "Capabilities",
		"SeverityDefinitions":        "Capabilities",
		"ErrorHandlingExtension":     "ErrorHandling",
		"ContextLimits":              "ExecutionPhilosophy",
	}
	got := docformat.InjectionParent
	if got == nil {
		t.Fatal("InjectionParent is nil — must be populated before first use")
	}
	for name, wantParent := range wantMap {
		gotParent, ok := got[name]
		if !ok {
			t.Errorf("InjectionParent missing entry for %q", name)
		} else if gotParent != wantParent {
			t.Errorf("InjectionParent[%q]: want %q, got %q", name, wantParent, gotParent)
		}
	}
}

func TestInjectionParent_Stage2_DoesNotContainProtocolExtension(t *testing.T) {
	// ProtocolExtension is removed entirely from all registries in Stage 2.
	if _, ok := docformat.InjectionParent["ProtocolExtension"]; ok {
		t.Error("InjectionParent must not contain ProtocolExtension — it is removed from all registries")
	}
}

func TestInjectionParent_Stage2_DoesNotContainToolManagedNames(t *testing.T) {
	// Tool-managed names move to DeployedParent; they must not remain in InjectionParent.
	toolManaged := []string{
		"HarnessConstraints",
		"CustomConstraints",
		"LanguagePatterns",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"CommunicationProtocol",
	}
	for _, name := range toolManaged {
		if _, ok := docformat.InjectionParent[name]; ok {
			t.Errorf(
				"InjectionParent must not contain tool-managed name %q — it belongs in DeployedParent",
				name,
			)
		}
	}
}

// ---------------------------------------------------------------------------
// ExpectedMarker — canonical name to required marker kind
// ---------------------------------------------------------------------------

func TestExpectedMarker_ToolManagedNames_ReturnNodeDeployed(t *testing.T) {
	// Every tool-managed name must be known and require the DEPLOYED marker.
	toolManaged := []string{
		"CommunicationProtocol",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"LanguagePatterns",
		"HarnessConstraints",
		"CustomConstraints",
	}
	for _, name := range toolManaged {
		kind, known := docformat.ExpectedMarker(name)
		if !known {
			t.Errorf("ExpectedMarker(%q): known=false, want true (tool-managed name must be recognised)", name)
		}
		if kind != docformat.NodeDeployed {
			t.Errorf("ExpectedMarker(%q): kind=%q, want NodeDeployed", name, kind)
		}
	}
}

func TestExpectedMarker_UserOwnedNames_ReturnNodeInjection(t *testing.T) {
	// Every user-owned name must be known and require the INJECTION marker.
	userOwned := []string{
		"IdentityExtension",
		"ArtifactProvenanceExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"SeverityThresholds",
		"SeverityDefinitions",
		"ErrorHandlingExtension",
		"ContextLimits",
	}
	for _, name := range userOwned {
		kind, known := docformat.ExpectedMarker(name)
		if !known {
			t.Errorf("ExpectedMarker(%q): known=false, want true (user-owned name must be recognised)", name)
		}
		if kind != docformat.NodeInjection {
			t.Errorf("ExpectedMarker(%q): kind=%q, want NodeInjection", name, kind)
		}
	}
}

func TestExpectedMarker_ProtocolExtension_IsNotKnown(t *testing.T) {
	// ProtocolExtension is removed from all registries; it must not be known.
	_, known := docformat.ExpectedMarker("ProtocolExtension")
	if known {
		t.Error("ExpectedMarker(\"ProtocolExtension\"): known=true, want false — ProtocolExtension is removed from all registries")
	}
}

func TestExpectedMarker_UnknownName_IsNotKnown(t *testing.T) {
	_, known := docformat.ExpectedMarker("SomeCompletelyUnknownBoundaryName")
	if known {
		t.Errorf("ExpectedMarker(\"SomeCompletelyUnknownBoundaryName\"): known=true, want false")
	}
}

// ---------------------------------------------------------------------------
// ClassifyRegion — marker-aware classification for each tool-managed class
// ---------------------------------------------------------------------------

func TestClassifyRegion_Deployed_CommunicationProtocol_ReturnsProtocolClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "CommunicationProtocol")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"CommunicationProtocol\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProtocol {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"CommunicationProtocol\"): want InjectionProtocol (%q), got %q",
			domain.InjectionProtocol, class,
		)
	}
}

func TestClassifyRegion_Deployed_HarnessConstraints_ReturnsHarnessClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "HarnessConstraints")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"HarnessConstraints\"): unexpected error: %v", err)
	}
	if class != domain.InjectionHarness {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"HarnessConstraints\"): want InjectionHarness, got %q",
			class,
		)
	}
}

func TestClassifyRegion_Deployed_LanguagePatterns_ReturnsHarnessClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "LanguagePatterns")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"LanguagePatterns\"): unexpected error: %v", err)
	}
	if class != domain.InjectionHarness {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"LanguagePatterns\"): want InjectionHarness, got %q",
			class,
		)
	}
}

func TestClassifyRegion_Deployed_CustomConstraints_ReturnsHarnessClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "CustomConstraints")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"CustomConstraints\"): unexpected error: %v", err)
	}
	if class != domain.InjectionHarness {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"CustomConstraints\"): want InjectionHarness, got %q",
			class,
		)
	}
}

func TestClassifyRegion_Deployed_AvailableWorkflows_ReturnsWorkflowClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "AvailableWorkflows")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"AvailableWorkflows\"): unexpected error: %v", err)
	}
	if class != domain.InjectionWorkflow {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"AvailableWorkflows\"): want InjectionWorkflow, got %q",
			class,
		)
	}
}

func TestClassifyRegion_Deployed_InfrastructureAgents_ReturnsInfrastructureClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "InfrastructureAgents")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"InfrastructureAgents\"): unexpected error: %v", err)
	}
	if class != domain.InjectionInfrastructure {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"InfrastructureAgents\"): want InjectionInfrastructure, got %q",
			class,
		)
	}
}

func TestClassifyRegion_Injection_AllUserOwnedNames_ReturnProjectClass(t *testing.T) {
	// All user-owned names declared under INJECTION return InjectionProject.
	userOwned := []string{
		"IdentityExtension",
		"ArtifactProvenanceExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"SeverityThresholds",
		"SeverityDefinitions",
		"ErrorHandlingExtension",
		"ContextLimits",
	}
	for _, name := range userOwned {
		class, err := docformat.ClassifyRegion(docformat.NodeInjection, name)
		if err != nil {
			t.Errorf("ClassifyRegion(NodeInjection, %q): unexpected error: %v", name, err)
			continue
		}
		if class != domain.InjectionProject {
			t.Errorf("ClassifyRegion(NodeInjection, %q): want InjectionProject, got %q", name, class)
		}
	}
}

// ---------------------------------------------------------------------------
// ClassifyRegion — marker mismatch errors
// ---------------------------------------------------------------------------

func TestClassifyRegion_Injection_ToolManagedName_ReturnsMarkerMismatchError(t *testing.T) {
	// A tool-managed name declared under INJECTION is a marker mismatch.
	// HarnessConstraints requires [[DEPLOYED:]], so using [[INJECTION:HarnessConstraints]]
	// must return an error wrapping ErrMarkerMismatch.
	_, err := docformat.ClassifyRegion(docformat.NodeInjection, "HarnessConstraints")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeInjection, \"HarnessConstraints\"): want error wrapping ErrMarkerMismatch, got nil")
	}
	if !errors.Is(err, docformat.ErrMarkerMismatch) {
		t.Errorf(
			"ClassifyRegion(NodeInjection, \"HarnessConstraints\"): error does not wrap ErrMarkerMismatch: %v",
			err,
		)
	}
}

func TestClassifyRegion_Deployed_UserOwnedName_ReturnsMarkerMismatchError(t *testing.T) {
	// A user-owned name declared under DEPLOYED is a marker mismatch.
	// IdentityExtension requires [[INJECTION:]], so using [[DEPLOYED:IdentityExtension]]
	// must return an error wrapping ErrMarkerMismatch.
	_, err := docformat.ClassifyRegion(docformat.NodeDeployed, "IdentityExtension")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, \"IdentityExtension\"): want error wrapping ErrMarkerMismatch, got nil")
	}
	if !errors.Is(err, docformat.ErrMarkerMismatch) {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"IdentityExtension\"): error does not wrap ErrMarkerMismatch: %v",
			err,
		)
	}
}

func TestClassifyRegion_Injection_AllToolManagedNames_ReturnMarkerMismatchError(t *testing.T) {
	// Every tool-managed name declared under INJECTION must produce a marker mismatch error.
	toolManaged := []string{
		"CommunicationProtocol",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"LanguagePatterns",
		"HarnessConstraints",
		"CustomConstraints",
	}
	for _, name := range toolManaged {
		_, err := docformat.ClassifyRegion(docformat.NodeInjection, name)
		if err == nil {
			t.Errorf("ClassifyRegion(NodeInjection, %q): want error wrapping ErrMarkerMismatch, got nil", name)
			continue
		}
		if !errors.Is(err, docformat.ErrMarkerMismatch) {
			t.Errorf("ClassifyRegion(NodeInjection, %q): error does not wrap ErrMarkerMismatch: %v", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// ClassifyRegion — unrecognised name under DEPLOYED
// ---------------------------------------------------------------------------

func TestClassifyRegion_Deployed_UnknownName_ReturnsUnknownDeployedError(t *testing.T) {
	// An unrecognised name under DEPLOYED has no generator and cannot be filled.
	// It must return an error wrapping ErrUnknownDeployedName.
	_, err := docformat.ClassifyRegion(docformat.NodeDeployed, "SomeCompletelyUnknownBoundaryName")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, unknown): want error wrapping ErrUnknownDeployedName, got nil")
	}
	if !errors.Is(err, docformat.ErrUnknownDeployedName) {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, unknown): error does not wrap ErrUnknownDeployedName: %v",
			err,
		)
	}
}

func TestClassifyRegion_Deployed_ProtocolExtension_ReturnsUnknownDeployedError(t *testing.T) {
	// ProtocolExtension is absent from all registries. Using it under [[DEPLOYED:]] must
	// return ErrUnknownDeployedName (not ErrMarkerMismatch, since the name is not canonical
	// at all).
	_, err := docformat.ClassifyRegion(docformat.NodeDeployed, "ProtocolExtension")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, \"ProtocolExtension\"): want error wrapping ErrUnknownDeployedName, got nil")
	}
	if !errors.Is(err, docformat.ErrUnknownDeployedName) {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"ProtocolExtension\"): error does not wrap ErrUnknownDeployedName: %v",
			err,
		)
	}
}

// ---------------------------------------------------------------------------
// ClassifyRegion — unrecognised name under INJECTION defaults to project
// ---------------------------------------------------------------------------

func TestClassifyRegion_Injection_UnknownName_ReturnsProjectClassAndNilError(t *testing.T) {
	// An unrecognised name under INJECTION returns InjectionProject and a nil error,
	// preserving the existing "unknown injections default to project" behaviour that
	// AllowUnknownInjections governs at validation time.
	class, err := docformat.ClassifyRegion(docformat.NodeInjection, "SomeCompletelyUnknownBoundaryName")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeInjection, unknown): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf(
			"ClassifyRegion(NodeInjection, unknown): want InjectionProject, got %q",
			class,
		)
	}
}

func TestClassifyRegion_Injection_ProtocolExtension_ReturnsProjectClassAndNilError(t *testing.T) {
	// ProtocolExtension is absent from all registries. Using it under [[INJECTION:]] must
	// return InjectionProject and a nil error (it falls through to the unknown-injection
	// default; AllowUnknownInjections gates whether a validation issue is raised).
	class, err := docformat.ClassifyRegion(docformat.NodeInjection, "ProtocolExtension")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeInjection, \"ProtocolExtension\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf(
			"ClassifyRegion(NodeInjection, \"ProtocolExtension\"): want InjectionProject, got %q",
			class,
		)
	}
}

// ---------------------------------------------------------------------------
// InjectionProtocol constant
// ---------------------------------------------------------------------------

func TestInjectionProtocol_ValueIsProtocolString(t *testing.T) {
	// The string value "protocol" is the class identifier used in the transform and plan layers.
	const wantValue domain.InjectionClass = "protocol"
	if domain.InjectionProtocol != wantValue {
		t.Errorf("InjectionProtocol: want %q, got %q", wantValue, domain.InjectionProtocol)
	}
}

// ---------------------------------------------------------------------------
// InfrastructureAgents — fully declared tool-managed name (no longer unknown)
// ---------------------------------------------------------------------------

func TestInfrastructureAgents_IsInCanonicalDeployed(t *testing.T) {
	// InfrastructureAgents was previously classified by ClassifyInjection but absent from
	// CanonicalInjections and InjectionParent. After Stage 2 it must be fully declared as
	// a tool-managed name in CanonicalDeployed.
	found := false
	for _, n := range docformat.CanonicalDeployed {
		if n == "InfrastructureAgents" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf(
			"InfrastructureAgents must be in CanonicalDeployed — it was previously only classified but not registered as canonical; got CanonicalDeployed: %v",
			docformat.CanonicalDeployed,
		)
	}
}

func TestInfrastructureAgents_IsInDeployedParentWithIdentityParent(t *testing.T) {
	if docformat.DeployedParent == nil {
		t.Fatal("DeployedParent is nil")
	}
	parent, ok := docformat.DeployedParent["InfrastructureAgents"]
	if !ok {
		t.Error("DeployedParent missing entry for \"InfrastructureAgents\"")
	} else if parent != "Identity" {
		t.Errorf("DeployedParent[\"InfrastructureAgents\"]: want %q, got %q", "Identity", parent)
	}
}

func TestInfrastructureAgents_ExpectedMarker_ReturnsNodeDeployedAndKnownTrue(t *testing.T) {
	// ExpectedMarker must recognise InfrastructureAgents as a tool-managed (DEPLOYED) name.
	kind, known := docformat.ExpectedMarker("InfrastructureAgents")
	if !known {
		t.Error("ExpectedMarker(\"InfrastructureAgents\"): known=false — it must be recognised as a tool-managed name and no longer reported as unknown")
	}
	if kind != docformat.NodeDeployed {
		t.Errorf("ExpectedMarker(\"InfrastructureAgents\"): kind=%q, want NodeDeployed", kind)
	}
}

func TestInfrastructureAgents_IsNotInInjectionParent(t *testing.T) {
	// InfrastructureAgents moves from ClassifyInjection classification to the tool-managed
	// registry; it must not appear in the user-owned InjectionParent map.
	if _, ok := docformat.InjectionParent["InfrastructureAgents"]; ok {
		t.Error("InjectionParent must not contain \"InfrastructureAgents\" — it is a tool-managed name and belongs in DeployedParent")
	}
}
