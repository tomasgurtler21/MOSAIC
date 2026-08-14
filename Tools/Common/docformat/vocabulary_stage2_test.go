package docformat_test

// Tests for Stage 2 vocabulary: the seven-slot canonical order, nine-name deployed
// set, revised parent tables, and classification behaviour after CanonicalInjections
// is removed.
//
// Coverage (CanonicalSections — unchanged in Stage 2):
//   - CanonicalSections contains exactly 6 entries in the correct order.
//   - CommunicationProtocol and ArtifactProvenance are absent from CanonicalSections.
//
// Coverage (CanonicalOrder — 7 slots, ArtifactProvenance removed):
//   - CanonicalOrder contains exactly 7 slots.
//   - CanonicalOrder[0] is Identity, CanonicalOrder[1] is CommunicationProtocol.
//   - ArtifactProvenance is absent from CanonicalOrder.
//   - Every CanonicalSection appears in CanonicalOrder.
//
// Coverage (CanonicalDeployed — 9 names, ArtifactProvenance/LanguagePatterns/CustomConstraints removed):
//   - CanonicalDeployed contains exactly 9 names.
//   - CanonicalDeployed includes all expected names including AuthorityHierarchy,
//     ClosingProcedure, ProtocolConstraints, ErrorHandlingCommon, ExecutionPhilosophyCommon.
//   - ArtifactProvenance is absent from CanonicalDeployed.
//
// Coverage (DeployedParent — 9 entries):
//   - DeployedParent maps all 9 tool-managed names to their required parents.
//   - ArtifactProvenance is absent from DeployedParent.
//   - New bundle names map to their required parent sections.
//
// Coverage (InjectionParent — advisory, ArtifactProvenanceExtension and ProtocolExtension
// removed, LanguagePatterns added):
//   - ProtocolExtension is absent from InjectionParent (removed in Stage 2; use <ProtocolExtension type="custom">).
//   - ArtifactProvenanceExtension is absent from InjectionParent.
//   - Tool-managed names are absent from InjectionParent.
//
// Coverage (ExpectedMarker — only CanonicalDeployed names are known):
//   - Every CanonicalDeployed name returns (NodeDeployed, true).
//   - Injection-only names (IdentityExtension, CodebaseContext, etc.) return ("", false).
//   - ArtifactProvenance returns ("", false) — no longer a canonical name.
//   - Unknown names return ("", false).
//
// Coverage (ClassifyRegion — no user-owned registry, bundle class for new names):
//   - CommunicationProtocol under NodeDeployed → InjectionProtocol.
//   - AvailableWorkflows under NodeDeployed → InjectionWorkflow.
//   - InfrastructureAgents under NodeDeployed → InjectionInfrastructure.
//   - HarnessConstraints under NodeDeployed → InjectionHarness.
//   - AuthorityHierarchy, ClosingProcedure, ProtocolConstraints, ErrorHandlingCommon,
//     ExecutionPhilosophyCommon under NodeDeployed → InjectionBundle.
//   - Any tool-managed name under NodeInjection → ErrMarkerMismatch (new bundle names included).
//   - Any name not in CanonicalDeployed under NodeDeployed → ErrUnknownDeployedName.
//   - Any name not in CanonicalDeployed under NodeInjection → InjectionProject, nil.
//   - A name registered in CanonicalDeployed but with no explicit classifier case →
//     error wrapping ErrUnclassifiedDeployedName.
//
// Coverage (InjectionBundle constant):
//   - InjectionBundle has the string value "bundle".

import (
	"errors"
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// ---------------------------------------------------------------------------
// CanonicalSections — 6 entries, unchanged in Stage 2
// ---------------------------------------------------------------------------

func TestCanonicalSections_Stage2_ContainsSixSections(t *testing.T) {
	got := docformat.CanonicalSections
	if len(got) != 6 {
		t.Fatalf("CanonicalSections length: want 6, got %d: %v", len(got), got)
	}
}

func TestCanonicalSections_Stage2_FullSixEntryOrder(t *testing.T) {
	// CanonicalSections is unchanged in Stage 2 (ArtifactProvenance was already
	// removed by Stage 1; it is now also absent from CanonicalDeployed).
	want := []string{
		"Identity",
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
// CanonicalOrder — 7 slots, ArtifactProvenance removed
// ---------------------------------------------------------------------------

func TestCanonicalOrder_Stage2_ContainsSevenSlots(t *testing.T) {
	// Stage 2 removes ArtifactProvenance from CanonicalOrder, reducing the slot
	// count from 8 to 7. Slot 1 is CommunicationProtocol (a top-level
	// <CommunicationProtocol type="managed"> boundary); every other entry is a section name.
	got := docformat.CanonicalOrder
	if len(got) != 7 {
		t.Fatalf("CanonicalOrder length: want 7, got %d: %v", len(got), got)
	}
}

func TestCanonicalOrder_Stage2_IndexZero_IsIdentity(t *testing.T) {
	got := docformat.CanonicalOrder
	if len(got) == 0 {
		t.Fatal("CanonicalOrder is empty")
	}
	if got[0] != "Identity" {
		t.Errorf("CanonicalOrder[0]: want %q, got %q", "Identity", got[0])
	}
}

func TestCanonicalOrder_Stage2_IndexOne_IsCommunicationProtocol(t *testing.T) {
	// Slot 1 is satisfied by a top-level <CommunicationProtocol type="managed"> boundary.
	got := docformat.CanonicalOrder
	if len(got) <= 1 {
		t.Fatalf("CanonicalOrder too short to have index 1: %v", got)
	}
	if got[1] != "CommunicationProtocol" {
		t.Errorf("CanonicalOrder[1]: want %q, got %q", "CommunicationProtocol", got[1])
	}
}

func TestCanonicalOrder_Stage2_ArtifactProvenance_IsAbsent(t *testing.T) {
	// ArtifactProvenance is removed from CanonicalOrder in Stage 2. It is also
	// removed from CanonicalDeployed — it ceases to be part of the vocabulary.
	for i, s := range docformat.CanonicalOrder {
		if s == "ArtifactProvenance" {
			t.Errorf(
				"CanonicalOrder[%d] is %q — ArtifactProvenance must be removed from "+
					"CanonicalOrder in Stage 2; the ArtifactProvenance slot is gone",
				i, s,
			)
		}
	}
}

func TestCanonicalOrder_Stage2_FullSevenSlotList(t *testing.T) {
	// Authoritative 7-slot canonical document order contract.
	// CommunicationProtocol occupies slot 1 as a top-level <CommunicationProtocol type="managed"> boundary;
	// every other slot is a canonical section name. ArtifactProvenance is absent.
	want := []string{
		"Identity",
		"CommunicationProtocol",
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

func TestCanonicalOrder_Stage2_AllCanonicalSectionsPresent(t *testing.T) {
	// Every CanonicalSection must appear in CanonicalOrder.
	orderSet := make(map[string]bool, len(docformat.CanonicalOrder))
	for _, n := range docformat.CanonicalOrder {
		orderSet[n] = true
	}
	for _, s := range docformat.CanonicalSections {
		if !orderSet[s] {
			t.Errorf("canonical section %q is not in CanonicalOrder: %v", s, docformat.CanonicalOrder)
		}
	}
}

// ---------------------------------------------------------------------------
// CanonicalDeployed — 9 names, ArtifactProvenance/LanguagePatterns/CustomConstraints removed
// ---------------------------------------------------------------------------

func TestCanonicalDeployed_Stage2_ContainsNineNames(t *testing.T) {
	// The vocabulary correction removes LanguagePatterns and CustomConstraints from
	// the tool-managed closed set (on top of the ArtifactProvenance removal and five
	// bundle-name additions from the earlier Stage 2 work). The closed set is now
	// 9 names.
	got := docformat.CanonicalDeployed
	if len(got) != 9 {
		t.Fatalf("CanonicalDeployed length: want 9, got %d: %v", len(got), got)
	}
}

func TestCanonicalDeployed_Stage2_IncludesAllExpectedNames(t *testing.T) {
	wantNames := []string{
		"CommunicationProtocol",
		"AuthorityHierarchy",
		"ClosingProcedure",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"ProtocolConstraints",
		"HarnessConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	nameSet := make(map[string]bool, len(docformat.CanonicalDeployed))
	for _, n := range docformat.CanonicalDeployed {
		nameSet[n] = true
	}
	for _, want := range wantNames {
		if !nameSet[want] {
			t.Errorf("CanonicalDeployed is missing expected name %q; got: %v", want, docformat.CanonicalDeployed)
		}
	}
}

func TestCanonicalDeployed_Stage2_ArtifactProvenance_IsAbsent(t *testing.T) {
	// ArtifactProvenance is removed from CanonicalDeployed in Stage 2.
	// It must not appear in any vocabulary copy after this stage.
	for _, n := range docformat.CanonicalDeployed {
		if n == "ArtifactProvenance" {
			t.Errorf(
				"CanonicalDeployed must not contain %q — ArtifactProvenance is removed "+
					"from the vocabulary entirely in Stage 2",
				n,
			)
		}
	}
}

func TestCanonicalDeployed_Stage2_IncludesNewBundleNames(t *testing.T) {
	// Five new bundle-sourced deployed names are added in Stage 2.
	bundleNames := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	nameSet := make(map[string]bool, len(docformat.CanonicalDeployed))
	for _, n := range docformat.CanonicalDeployed {
		nameSet[n] = true
	}
	for _, name := range bundleNames {
		if !nameSet[name] {
			t.Errorf("CanonicalDeployed is missing new bundle name %q; got: %v", name, docformat.CanonicalDeployed)
		}
	}
}

// ---------------------------------------------------------------------------
// DeployedParent — 9 entries, ArtifactProvenance absent
// ---------------------------------------------------------------------------

func TestDeployedParent_Stage2_IsNotNil(t *testing.T) {
	if docformat.DeployedParent == nil {
		t.Fatal("DeployedParent is nil — must be populated before first use")
	}
}

func TestDeployedParent_Stage2_MapsAllNineNames(t *testing.T) {
	// An empty string value means the boundary must appear at body top level.
	wantMap := map[string]string{
		"CommunicationProtocol":     "", // top level
		"AuthorityHierarchy":        "Identity",
		"ClosingProcedure":          "Identity",
		"AvailableWorkflows":        "Identity",
		"InfrastructureAgents":      "Identity",
		"ProtocolConstraints":       "Constraints",
		"HarnessConstraints":        "Constraints",
		"ErrorHandlingCommon":       "ErrorHandling",
		"ExecutionPhilosophyCommon": "ExecutionPhilosophy",
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

func TestDeployedParent_Stage2_ArtifactProvenance_IsAbsent(t *testing.T) {
	// ArtifactProvenance must not appear in DeployedParent after Stage 2.
	if got := docformat.DeployedParent; got != nil {
		if _, ok := got["ArtifactProvenance"]; ok {
			t.Error("DeployedParent must not contain \"ArtifactProvenance\" — it is removed from the vocabulary in Stage 2")
		}
	}
}

func TestDeployedParent_Stage2_ContainsExactlyNineEntries(t *testing.T) {
	got := docformat.DeployedParent
	if got == nil {
		t.Fatal("DeployedParent is nil")
	}
	if len(got) != 9 {
		t.Fatalf("DeployedParent length: want 9, got %d: %v", len(got), got)
	}
}

func TestDeployedParent_Stage2_NewBundleNames_HaveCorrectParents(t *testing.T) {
	got := docformat.DeployedParent
	if got == nil {
		t.Fatal("DeployedParent is nil")
	}
	cases := []struct {
		name       string
		wantParent string
	}{
		{"AuthorityHierarchy", "Identity"},
		{"ClosingProcedure", "Identity"},
		{"ProtocolConstraints", "Constraints"},
		{"ErrorHandlingCommon", "ErrorHandling"},
		{"ExecutionPhilosophyCommon", "ExecutionPhilosophy"},
	}
	for _, tc := range cases {
		p, ok := got[tc.name]
		if !ok {
			t.Errorf("DeployedParent missing entry for %q", tc.name)
			continue
		}
		if p != tc.wantParent {
			t.Errorf("DeployedParent[%q]: want %q, got %q", tc.name, tc.wantParent, p)
		}
	}
}

// ---------------------------------------------------------------------------
// InjectionParent — advisory map; ProtocolExtension added, ArtifactProvenanceExtension removed
// ---------------------------------------------------------------------------

func TestInjectionParent_Stage2_IsNotNil(t *testing.T) {
	if docformat.InjectionParent == nil {
		t.Fatal("InjectionParent is nil — must be populated before first use")
	}
}

func TestInjectionParent_Stage2_ProtocolExtension_IsAbsent(t *testing.T) {
	// ProtocolExtension is removed from InjectionParent in Stage 2. Projects needing to
	// extend the protocol use <ProtocolExtension type="custom"> instead — MOSAIC defines
	// <Name type="project"> slots; projects invent <Name type="custom"> ones. The name remains legal as
	// an open injection name and is never flagged, but it no longer carries an advisory parent.
	got := docformat.InjectionParent
	if got == nil {
		t.Fatal("InjectionParent is nil")
	}
	if _, ok := got["ProtocolExtension"]; ok {
		t.Error("InjectionParent must not contain \"ProtocolExtension\" — " +
			"it is removed from the advisory map in Stage 2; projects use <ProtocolExtension type=\"custom\"> instead")
	}
}

func TestInjectionParent_Stage2_ArtifactProvenanceExtension_IsAbsent(t *testing.T) {
	// ArtifactProvenanceExtension is removed from InjectionParent in Stage 2 because
	// ArtifactProvenance (its sibling) is removed from the vocabulary entirely.
	got := docformat.InjectionParent
	if got == nil {
		t.Fatal("InjectionParent is nil")
	}
	if _, ok := got["ArtifactProvenanceExtension"]; ok {
		t.Error("InjectionParent must not contain \"ArtifactProvenanceExtension\" — " +
			"ArtifactProvenance is removed from the vocabulary in Stage 2, so its extension is also removed")
	}
}

func TestInjectionParent_Stage2_FullAdvisoryMap(t *testing.T) {
	// InjectionParent is now advisory only (no enforcement of an allowlist).
	// The eight entries are the usual parents used for advisory reporting.
	// ProtocolExtension is removed in Stage 2 — projects use <ProtocolExtension type="custom">.
	// LanguagePatterns is included here after moving out of the tool-managed CanonicalDeployed set.
	wantMap := map[string]string{
		"IdentityExtension":      "Identity",
		"CodebaseContext":        "Capabilities",
		"LanguagePatterns":       "Capabilities", // moved from CanonicalDeployed
		"OutputArtifactTemplate": "Capabilities",
		"SeverityThresholds":     "Capabilities",
		"SeverityDefinitions":    "Capabilities",
		"ErrorHandlingExtension": "ErrorHandling",
		"ContextLimits":          "ExecutionPhilosophy",
	}
	got := docformat.InjectionParent
	if got == nil {
		t.Fatal("InjectionParent is nil")
	}
	for name, wantParent := range wantMap {
		gotParent, ok := got[name]
		if !ok {
			t.Errorf("InjectionParent missing entry for %q", name)
		} else if gotParent != wantParent {
			t.Errorf("InjectionParent[%q]: want %q, got %q", name, wantParent, gotParent)
		}
	}
	// ProtocolExtension must be absent from the advisory map after Stage 2.
	if _, ok := got["ProtocolExtension"]; ok {
		t.Error("InjectionParent must not contain \"ProtocolExtension\" — removed in Stage 2; use <ProtocolExtension type=\"custom\"> instead")
	}
}

func TestInjectionParent_Stage2_DoesNotContainToolManagedNames(t *testing.T) {
	// Tool-managed names must not appear in InjectionParent; they belong in DeployedParent.
	// LanguagePatterns is deliberately excluded from this list: it is no longer
	// tool-managed and is expected to appear in InjectionParent.
	toolManaged := []string{
		"CommunicationProtocol",
		"AuthorityHierarchy",
		"ClosingProcedure",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"ProtocolConstraints",
		"HarnessConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	got := docformat.InjectionParent
	if got == nil {
		t.Fatal("InjectionParent is nil")
	}
	for _, name := range toolManaged {
		if _, ok := got[name]; ok {
			t.Errorf("InjectionParent must not contain tool-managed name %q — it belongs in DeployedParent", name)
		}
	}
}

// ---------------------------------------------------------------------------
// ExpectedMarker — only CanonicalDeployed names return (NodeDeployed, true)
// ---------------------------------------------------------------------------

func TestExpectedMarker_Stage2_AllCanonicalDeployed_ReturnNodeDeployedAndKnownTrue(t *testing.T) {
	// Every name in CanonicalDeployed must be known and require the DEPLOYED marker.
	// This includes the five new bundle names added in Stage 2.
	for _, name := range docformat.CanonicalDeployed {
		kind, known := docformat.ExpectedMarker(name)
		if !known {
			t.Errorf("ExpectedMarker(%q): known=false, want true (all CanonicalDeployed names must be recognised)", name)
		}
		if kind != docformat.NodeDeployed {
			t.Errorf("ExpectedMarker(%q): kind=%q, want NodeDeployed", name, kind)
		}
	}
}

func TestExpectedMarker_Stage2_NewBundleNames_ReturnNodeDeployedAndKnownTrue(t *testing.T) {
	// The five new bundle names added in Stage 2 must be recognised.
	bundleNames := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	for _, name := range bundleNames {
		kind, known := docformat.ExpectedMarker(name)
		if !known {
			t.Errorf("ExpectedMarker(%q): known=false, want true (new bundle name must be recognised)", name)
		}
		if kind != docformat.NodeDeployed {
			t.Errorf("ExpectedMarker(%q): kind=%q, want NodeDeployed", name, kind)
		}
	}
}

func TestExpectedMarker_Stage2_InjectionNames_AreNotKnown(t *testing.T) {
	// Injection names are open in Stage 2 — they are not in any canonical registry.
	// ExpectedMarker returns ("", false) for all of them.
	injectionNames := []string{
		"IdentityExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"SeverityThresholds",
		"SeverityDefinitions",
		"ErrorHandlingExtension",
		"ContextLimits",
		"ProtocolExtension",
	}
	for _, name := range injectionNames {
		_, known := docformat.ExpectedMarker(name)
		if known {
			t.Errorf("ExpectedMarker(%q): known=true, want false — injection names are not in any canonical registry", name)
		}
	}
}

func TestExpectedMarker_Stage2_ArtifactProvenance_IsNotKnown(t *testing.T) {
	// ArtifactProvenance is removed from the vocabulary entirely in Stage 2.
	// ExpectedMarker must return ("", false) for it.
	_, known := docformat.ExpectedMarker("ArtifactProvenance")
	if known {
		t.Error("ExpectedMarker(\"ArtifactProvenance\"): known=true, want false — " +
			"ArtifactProvenance is removed from the vocabulary in Stage 2")
}
}

func TestExpectedMarker_Stage2_UnknownName_IsNotKnown(t *testing.T) {
	_, known := docformat.ExpectedMarker("SomeCompletelyUnknownBoundaryName")
	if known {
		t.Errorf("ExpectedMarker(\"SomeCompletelyUnknownBoundaryName\"): known=true, want false")
	}
}

// ---------------------------------------------------------------------------
// ClassifyRegion — deployed names classified correctly; no user-owned registry
// ---------------------------------------------------------------------------

func TestClassifyRegion_Stage2_CommunicationProtocol_ReturnsProtocolClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "CommunicationProtocol")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"CommunicationProtocol\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProtocol {
		t.Errorf("ClassifyRegion(NodeDeployed, \"CommunicationProtocol\"): want InjectionProtocol, got %q", class)
	}
}

func TestClassifyRegion_Stage2_AvailableWorkflows_ReturnsWorkflowClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "AvailableWorkflows")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"AvailableWorkflows\"): unexpected error: %v", err)
	}
	if class != domain.InjectionWorkflow {
		t.Errorf("ClassifyRegion(NodeDeployed, \"AvailableWorkflows\"): want InjectionWorkflow, got %q", class)
	}
}

func TestClassifyRegion_Stage2_InfrastructureAgents_ReturnsInfrastructureClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "InfrastructureAgents")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"InfrastructureAgents\"): unexpected error: %v", err)
	}
	if class != domain.InjectionInfrastructure {
		t.Errorf("ClassifyRegion(NodeDeployed, \"InfrastructureAgents\"): want InjectionInfrastructure, got %q", class)
	}
}

func TestClassifyRegion_Stage2_HarnessConstraints_ReturnsHarnessClass(t *testing.T) {
	// HarnessConstraints is the only remaining name with an explicit InjectionHarness
	// case: LanguagePatterns and CustomConstraints are removed from CanonicalDeployed
	// entirely and no longer reach this classification path.
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "HarnessConstraints")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"HarnessConstraints\"): unexpected error: %v", err)
	}
	if class != domain.InjectionHarness {
		t.Errorf("ClassifyRegion(NodeDeployed, \"HarnessConstraints\"): want InjectionHarness, got %q", class)
	}
}

func TestClassifyRegion_Stage2_UnclassifiedCanonicalName_ReturnsErrorWrappingSentinel(t *testing.T) {
	// A name registered in CanonicalDeployed but with no explicit case in the
	// classifier's switch must return an error wrapping ErrUnclassifiedDeployedName,
	// never a class by default. This is the defect being corrected: the classifier's
	// old permissive default branch silently answered InjectionHarness for any
	// unrecognised tool-managed name.
	//
	// This row is unreachable through ClassifyRegion while CanonicalDeployed and the
	// classifier's case list agree, so the test reaches it by temporarily extending
	// CanonicalDeployed with a name the classifier has no case for.
	const bogusName = "SomeCanonicalButUnclassifiedName"

	original := docformat.CanonicalDeployed
	extended := make([]string, len(original), len(original)+1)
	copy(extended, original)
	docformat.CanonicalDeployed = append(extended, bogusName)
	defer func() { docformat.CanonicalDeployed = original }()

	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, bogusName)

	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, bogusName): want error wrapping ErrUnclassifiedDeployedName, got nil")
	}
	if !errors.Is(err, docformat.ErrUnclassifiedDeployedName) {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, bogusName): error does not wrap ErrUnclassifiedDeployedName: %v", err,
		)
	}
	if class != "" {
		t.Errorf("ClassifyRegion(NodeDeployed, bogusName): want zero-value class on error, got %q", class)
	}
}

func TestClassifyRegion_Stage2_BundleNames_ReturnBundleClass(t *testing.T) {
	// The five new bundle-sourced names must return InjectionBundle.
	// The bundle branch is an explicit case list — a new tool-managed name falls to
	// InjectionHarness and must be classified deliberately to avoid silently producing
	// empty deployed regions.
	bundleNames := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	for _, name := range bundleNames {
		class, err := docformat.ClassifyRegion(docformat.NodeDeployed, name)
		if err != nil {
			t.Errorf("ClassifyRegion(NodeDeployed, %q): unexpected error: %v", name, err)
			continue
		}
		if class != domain.InjectionBundle {
			t.Errorf(
				"ClassifyRegion(NodeDeployed, %q): want InjectionBundle, got %q — "+
					"bundle-sourced names must have InjectionBundle class, not fall through to InjectionHarness",
				name, class,
			)
		}
	}
}

func TestClassifyRegion_Stage2_AllToolManagedNames_UnderInjection_ReturnMarkerMismatch(t *testing.T) {
	// Every CanonicalDeployed name declared under a project-type boundary must produce a marker
	// mismatch error. This includes the new bundle names added in Stage 2.
	for _, name := range docformat.CanonicalDeployed {
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

func TestClassifyRegion_Stage2_UnknownName_UnderDeployed_ReturnsUnknownDeployedError(t *testing.T) {
	// An unrecognised name under a managed-type boundary must return ErrUnknownDeployedName.
	// This applies to any name not in CanonicalDeployed, including names that were
	// formerly in CanonicalInjections (the user-owned registry no longer exists).
	_, err := docformat.ClassifyRegion(docformat.NodeDeployed, "SomeCompletelyUnknownBoundaryName")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, unknown): want error wrapping ErrUnknownDeployedName, got nil")
	}
	if !errors.Is(err, docformat.ErrUnknownDeployedName) {
		t.Errorf("ClassifyRegion(NodeDeployed, unknown): error does not wrap ErrUnknownDeployedName: %v", err)
	}
}

func TestClassifyRegion_Stage2_InjectionName_UnderDeployed_ReturnsUnknownDeployedError(t *testing.T) {
	// After Stage 2, IdentityExtension is not in any tool-managed registry, so
	// <IdentityExtension type="managed"> produces ErrUnknownDeployedName — not
	// ErrMarkerMismatch (which requires a user-owned registry, which no longer exists).
	_, err := docformat.ClassifyRegion(docformat.NodeDeployed, "IdentityExtension")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, \"IdentityExtension\"): want error wrapping ErrUnknownDeployedName, got nil")
	}
	if !errors.Is(err, docformat.ErrUnknownDeployedName) {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"IdentityExtension\"): error does not wrap ErrUnknownDeployedName: %v", err,
		)
	}
}

func TestClassifyRegion_Stage2_ArtifactProvenance_UnderDeployed_ReturnsUnknownDeployedError(t *testing.T) {
	// ArtifactProvenance is removed from CanonicalDeployed in Stage 2.
	// <ArtifactProvenance type="managed"> must produce ErrUnknownDeployedName.
	_, err := docformat.ClassifyRegion(docformat.NodeDeployed, "ArtifactProvenance")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, \"ArtifactProvenance\"): want error wrapping ErrUnknownDeployedName, got nil")
	}
	if !errors.Is(err, docformat.ErrUnknownDeployedName) {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, \"ArtifactProvenance\"): error does not wrap ErrUnknownDeployedName: %v", err,
		)
	}
}

func TestClassifyRegion_Stage2_UnknownName_UnderInjection_ReturnsProjectClassAndNilError(t *testing.T) {
	// Any name not in CanonicalDeployed under a project-type boundary returns InjectionProject
	// and a nil error. Injection names are open: an unlisted name is preserved like any
	// other and is never flagged or rejected.
	class, err := docformat.ClassifyRegion(docformat.NodeInjection, "SomeCompletelyUnknownBoundaryName")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeInjection, unknown): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeInjection, unknown): want InjectionProject, got %q", class)
	}
}

func TestClassifyRegion_Stage2_FormerlyUserOwnedName_UnderInjection_ReturnsProjectClass(t *testing.T) {
	// IdentityExtension was previously in the user-owned registry; after Stage 2
	// there is no user-owned registry. It is treated identically to any other
	// unknown injection name: InjectionProject, nil error.
	class, err := docformat.ClassifyRegion(docformat.NodeInjection, "IdentityExtension")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeInjection, \"IdentityExtension\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeInjection, \"IdentityExtension\"): want InjectionProject, got %q", class)
	}
}

func TestClassifyRegion_Stage2_ArtifactProvenance_UnderInjection_ReturnsProjectClass(t *testing.T) {
	// ArtifactProvenance is removed from all registries. Under a project-type boundary it falls
	// through to InjectionProject (open injection names are preserved).
	class, err := docformat.ClassifyRegion(docformat.NodeInjection, "ArtifactProvenance")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeInjection, \"ArtifactProvenance\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeInjection, \"ArtifactProvenance\"): want InjectionProject, got %q", class)
	}
}

// ---------------------------------------------------------------------------
// InjectionBundle constant
// ---------------------------------------------------------------------------

func TestInjectionBundle_ValueIsBundleString(t *testing.T) {
	const wantValue domain.InjectionClass = "bundle"
	if domain.InjectionBundle != wantValue {
		t.Errorf("InjectionBundle: want %q, got %q", wantValue, domain.InjectionBundle)
	}
}
