package docformat_test

// Tests for Stage 2 marker-classification behaviour that survives the removal of the
// canonical-injections allowlist, and exact table-pinning tests for drift detection.
//
// Coverage (T2.3 — surviving marker-classification behaviour):
//   - A tool-managed name declared under [[INJECTION:]] is still a marker mismatch.
//     This applies to both existing names (HarnessConstraints) and the new bundle names
//     (AuthorityHierarchy, ClosingProcedure, etc.).
//   - An unrecognised [[DEPLOYED:]] name is still an error (unknown-deployed).
//   - An unrecognised [[INJECTION:]] name produces no error — injection names are open.
//   - ProtocolExtension under [[INJECTION:]] produces no error and returns InjectionProject.
//   - The validate layer raises "wrong-marker" for a tool-managed bundle name declared
//     under [[INJECTION:]] (fixture-based test).
//   - The validate layer raises "unknown-deployed" for a name not in CanonicalDeployed
//     regardless of whether it was formerly a user-owned injection name.
//   - The validate layer raises no "unknown-injection" code for any [[INJECTION:]] name
//     (the code and the AllowUnknownInjections option are both removed).
//
// Coverage (T2.4 — exact table pinning for cross-copy drift detection):
//   - CanonicalSections exact 6-entry sequence.
//   - CanonicalOrder exact 7-slot sequence (ArtifactProvenance absent).
//   - CanonicalDeployed exact 9-name sequence (LanguagePatterns and CustomConstraints
//     removed from the tool-managed vocabulary).
//   - DeployedParent exact 9-entry map (values are the primary drift signal).
//   - InjectionParent exact 9-entry advisory map (gains LanguagePatterns under Capabilities).

import (
	"errors"
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// ---------------------------------------------------------------------------
// T2.3 — Surviving marker-classification: ClassifyRegion layer
// ---------------------------------------------------------------------------

func TestClassifyRegion_BundleName_UnderInjection_ReturnsMarkerMismatch(t *testing.T) {
	// AuthorityHierarchy is a bundle-sourced tool-managed name. Declaring it under
	// [[INJECTION:]] must produce ErrMarkerMismatch, just as HarnessConstraints does.
	// This verifies the mismatch check survives the removal of CanonicalInjections.
	_, err := docformat.ClassifyRegion(docformat.NodeInjection, "AuthorityHierarchy")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeInjection, \"AuthorityHierarchy\"): want error wrapping ErrMarkerMismatch, got nil")
	}
	if !errors.Is(err, docformat.ErrMarkerMismatch) {
		t.Errorf(
			"ClassifyRegion(NodeInjection, \"AuthorityHierarchy\"): error does not wrap ErrMarkerMismatch: %v", err,
		)
	}
}

func TestClassifyRegion_AllBundleNames_UnderInjection_ReturnMarkerMismatch(t *testing.T) {
	// Every new bundle-sourced name must produce ErrMarkerMismatch when declared under
	// [[INJECTION:]], confirming that bundle names are tool-managed and the mismatch
	// check covers the extended CanonicalDeployed set.
	bundleNames := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	for _, name := range bundleNames {
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

func TestClassifyRegion_UnknownDeployedName_ReturnsUnknownDeployedError(t *testing.T) {
	// An unrecognised [[DEPLOYED:]] name still produces ErrUnknownDeployedName.
	// This behaviour survives the removal of CanonicalInjections.
	_, err := docformat.ClassifyRegion(docformat.NodeDeployed, "TotallyUnknownDeployedName")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, unknown): want error wrapping ErrUnknownDeployedName, got nil")
	}
	if !errors.Is(err, docformat.ErrUnknownDeployedName) {
		t.Errorf(
			"ClassifyRegion(NodeDeployed, unknown): error does not wrap ErrUnknownDeployedName: %v", err,
		)
	}
}

func TestClassifyRegion_ProtocolExtension_UnderInjection_ReturnsProjectClassAndNilError(t *testing.T) {
	// ProtocolExtension is now a top-level injection listed in InjectionParent (advisory).
	// Under [[INJECTION:]], it is open like any injection name and must return
	// InjectionProject with a nil error — not an error.
	class, err := docformat.ClassifyRegion(docformat.NodeInjection, "ProtocolExtension")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeInjection, \"ProtocolExtension\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeInjection, \"ProtocolExtension\"): want InjectionProject, got %q", class)
	}
}

func TestClassifyRegion_OpenInjectionName_ReturnsProjectClassAndNilError(t *testing.T) {
	// Any injection name not in CanonicalDeployed is accepted as InjectionProject.
	// The canonical-injections allowlist is removed; injection names are open.
	openNames := []string{
		"IdentityExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"SeverityThresholds",
		"SeverityDefinitions",
		"ErrorHandlingExtension",
		"ContextLimits",
		"ProtocolExtension",
		"SomeCompletelyNewInjectionName",
		"AnotherCustomExtension",
	}
	for _, name := range openNames {
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
// T2.3 — Surviving marker-classification: Validate layer
// ---------------------------------------------------------------------------

func TestValidate_BundleNameAsInjection_ReportsWrongMarkerIssue(t *testing.T) {
	// AuthorityHierarchy is a bundle-sourced tool-managed name (new in Stage 2).
	// Declaring it under [[INJECTION:]] must produce a "wrong-marker" issue.
	// This confirms the wrong-marker check applies to the extended CanonicalDeployed set.
	//
	// Fixture: malformed/bundle-name-as-injection.md contains
	//   [[SECTION:Identity]]
	//     [[INJECTION:AuthorityHierarchy]]
	//     [[/INJECTION:AuthorityHierarchy]]
	//   [[/SECTION:Identity]]
	doc := parsedBoundaryFixture(t, "malformed/bundle-name-as-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"expected a \"wrong-marker\" issue for [[INJECTION:AuthorityHierarchy]] "+
				"(tool-managed bundle name under the wrong marker), got issues: %v",
			issues,
		)
	}
}

func TestValidate_BundleNameAsInjection_WrongMarkerIssueSeverityIsError(t *testing.T) {
	doc := parsedBoundaryFixture(t, "malformed/bundle-name-as-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	iss := issueWithCode(issues, "wrong-marker")
	if iss == nil {
		t.Skip("no wrong-marker issue returned — severity cannot be checked")
	}
	if iss.Severity != docformat.SeverityError {
		t.Errorf("wrong-marker issue severity: want SeverityError, got %q", iss.Severity)
	}
}

func TestValidate_FormerUserOwnedNameAsDeployed_ReportsUnknownDeployedIssue(t *testing.T) {
	// In Stage 2, IdentityExtension is not in any canonical registry.
	// [[DEPLOYED:IdentityExtension]] must produce "unknown-deployed" — not "wrong-marker"
	// (which required a user-owned registry, now removed).
	//
	// Fixture: malformed/wrong-marker-user-as-deployed.md contains
	//   [[SECTION:Identity]]
	//     [[DEPLOYED:IdentityExtension]]
	//     [[/DEPLOYED:IdentityExtension]]
	//   [[/SECTION:Identity]]
	doc := parsedBoundaryFixture(t, "malformed/wrong-marker-user-as-deployed.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if !hasIssueWithCode(issues, "unknown-deployed") {
		t.Errorf(
			"expected an \"unknown-deployed\" issue for [[DEPLOYED:IdentityExtension]] "+
				"(name not in CanonicalDeployed), got issues: %v",
			issues,
		)
	}
}

func TestValidate_FormerUserOwnedNameAsDeployed_DoesNotReportWrongMarker(t *testing.T) {
	// After Stage 2, there is no user-owned registry. [[DEPLOYED:IdentityExtension]]
	// is treated as an unknown deployed name, not a marker mismatch.
	doc := parsedBoundaryFixture(t, "malformed/wrong-marker-user-as-deployed.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"expected no \"wrong-marker\" issue for [[DEPLOYED:IdentityExtension]] — "+
				"there is no user-owned registry in Stage 2; the issue code is \"unknown-deployed\", "+
				"got issues: %v",
			issues,
		)
	}
}

func TestValidate_OpenInjectionName_ProducesNoIssue(t *testing.T) {
	// An unrecognised [[INJECTION:]] name must produce no issue — injection names are open.
	// The "unknown-injection" code and AllowUnknownInjections option are both removed.
	//
	// Fixture: malformed/unknown-injection.md contains [[INJECTION:NonExistentName]].
	doc := parsedBoundaryFixture(t, "malformed/unknown-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "unknown-injection") {
		t.Errorf(
			"expected no \"unknown-injection\" issue — injection names are open in Stage 2; "+
				"the code is removed, got issues: %v",
			issues,
		)
	}
}

func TestValidate_OpenInjectionName_DoesNotProduceWrongMarker(t *testing.T) {
	// An open injection name must not produce "wrong-marker" (which is only for
	// tool-managed names under [[INJECTION:]]).
	doc := parsedBoundaryFixture(t, "malformed/unknown-injection.md")

	issues := docformat.Validate(doc, docformat.ValidateOptions{})

	if hasIssueWithCode(issues, "wrong-marker") {
		t.Errorf(
			"expected no \"wrong-marker\" for an open injection name, got issues: %v",
			issues,
		)
	}
}

// ---------------------------------------------------------------------------
// T2.4 — Exact table pinning (Go copy — catches drift from the Python mirror)
// ---------------------------------------------------------------------------

func TestVocabulary_CanonicalSections_ExactSixEntrySequence(t *testing.T) {
	// Authoritative 6-entry sequence. Any deviation is a drift from boundary_constants.py.
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
		t.Fatalf("CanonicalSections: want %d entries, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("CanonicalSections[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

func TestVocabulary_CanonicalOrder_ExactSevenSlotSequence(t *testing.T) {
	// Authoritative 7-slot sequence. Slot 1 is CommunicationProtocol (top-level DEPLOYED);
	// every other slot is a section name. ArtifactProvenance is absent.
	// Any deviation is a drift from boundary_constants.py.
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
		t.Fatalf("CanonicalOrder: want %d entries, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("CanonicalOrder[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

func TestVocabulary_CanonicalDeployed_ExactNineNameSequence(t *testing.T) {
	// Authoritative 9-name closed set. Order matters for cross-copy comparison.
	// LanguagePatterns and CustomConstraints are removed from the tool-managed
	// vocabulary; LanguagePatterns moves to the advisory InjectionParent catalogue,
	// CustomConstraints is deleted outright.
	// Any deviation is a drift from boundary_constants.py.
	want := []string{
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
	got := docformat.CanonicalDeployed
	if len(got) != len(want) {
		t.Fatalf("CanonicalDeployed: want %d entries, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("CanonicalDeployed[%d]: want %q, got %q", i, w, got[i])
		}
	}
}

func TestVocabulary_DeployedParent_ExactNineEntryMap(t *testing.T) {
	// Authoritative 9-entry parent map. Values are the primary drift signal: a wrong
	// parent silently misroutes validation without any other symptom.
	// LanguagePatterns and CustomConstraints are removed.
	// Any deviation is a drift from boundary_constants.py (where "" in Go is None in Python).
	want := map[string]string{
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
	if len(got) != len(want) {
		t.Errorf("DeployedParent: want %d entries, got %d", len(want), len(got))
	}
	for name, wantParent := range want {
		gotParent, ok := got[name]
		if !ok {
			t.Errorf("DeployedParent missing entry for %q", name)
		} else if gotParent != wantParent {
			t.Errorf("DeployedParent[%q]: want %q, got %q", name, wantParent, gotParent)
		}
	}
	// Check for unexpected extra entries.
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("DeployedParent has unexpected extra entry %q", name)
		}
	}
}

func TestVocabulary_InjectionParent_ExactEightEntryAdvisoryMap(t *testing.T) {
	// Authoritative 8-entry advisory map. InjectionParent is no longer an allowlist;
	// an absent name is preserved, never flagged. The values are the drift signal.
	// ProtocolExtension is removed in Stage 2: projects use [[CUSTOM:ProtocolExtension]]
	// instead — MOSAIC defines [[INJECTION:]] slots; projects invent [[CUSTOM:]] ones.
	// LanguagePatterns is present: it moved from the tool-managed CanonicalDeployed
	// set to this advisory catalogue, keeping its Capabilities parent.
	// Any deviation is a drift from boundary_constants.py (where "" in Go is None in Python).
	want := map[string]string{
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
	if len(got) != len(want) {
		t.Errorf("InjectionParent: want %d entries, got %d", len(want), len(got))
	}
	for name, wantParent := range want {
		gotParent, ok := got[name]
		if !ok {
			t.Errorf("InjectionParent missing advisory entry for %q", name)
		} else if gotParent != wantParent {
			t.Errorf("InjectionParent[%q]: want %q, got %q", name, wantParent, gotParent)
		}
	}
	// Check for unexpected extra entries.
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("InjectionParent has unexpected extra advisory entry %q", name)
		}
	}
}

func TestVocabulary_NoStaleArtifactProvenanceInAnyTable(t *testing.T) {
	// Regression: ArtifactProvenance and ArtifactProvenanceExtension must not appear
	// in any vocabulary table after Stage 2. A single stale entry in any table would
	// leave the three copies (Go, Python, Markdown) disagreeing.
	staleNames := []string{"ArtifactProvenance", "ArtifactProvenanceExtension"}

	for _, name := range staleNames {
		for _, n := range docformat.CanonicalSections {
			if n == name {
				t.Errorf("CanonicalSections contains stale name %q", name)
			}
		}
		for _, n := range docformat.CanonicalOrder {
			if n == name {
				t.Errorf("CanonicalOrder contains stale name %q", name)
			}
		}
		for _, n := range docformat.CanonicalDeployed {
			if n == name {
				t.Errorf("CanonicalDeployed contains stale name %q", name)
			}
		}
		if docformat.DeployedParent != nil {
			if _, ok := docformat.DeployedParent[name]; ok {
				t.Errorf("DeployedParent contains stale name %q", name)
			}
		}
		if docformat.InjectionParent != nil {
			if _, ok := docformat.InjectionParent[name]; ok {
				t.Errorf("InjectionParent contains stale name %q", name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Vocabulary correction — LanguagePatterns re-catalogued, CustomConstraints deleted
// ---------------------------------------------------------------------------

func TestVocabulary_InjectionParent_ContainsLanguagePatternsWithCapabilitiesParent(t *testing.T) {
	// Positive assertion, not just a re-pinned negative: LanguagePatterns must be
	// present in the advisory InjectionParent catalogue with parent Capabilities.
	// An absence-only assertion elsewhere would still pass if the name had been
	// dropped entirely instead of re-catalogued.
	got := docformat.InjectionParent
	if got == nil {
		t.Fatal("InjectionParent is nil")
	}
	parent, ok := got["LanguagePatterns"]
	if !ok {
		t.Fatal("InjectionParent must contain \"LanguagePatterns\" — it moved from the tool-managed " +
			"CanonicalDeployed set to the advisory injection catalogue")
	}
	if parent != "Capabilities" {
		t.Errorf("InjectionParent[\"LanguagePatterns\"]: want %q, got %q", "Capabilities", parent)
	}
}

func TestVocabulary_CanonicalDeployed_LanguagePatternsAndCustomConstraints_AreAbsent(t *testing.T) {
	// Neither removed name may remain in the tool-managed closed set.
	staleNames := []string{"LanguagePatterns", "CustomConstraints"}
	for _, name := range staleNames {
		for _, n := range docformat.CanonicalDeployed {
			if n == name {
				t.Errorf(
					"CanonicalDeployed must not contain %q — it is removed from the "+
						"nine-name tool-managed vocabulary", name,
				)
			}
		}
	}
}

func TestVocabulary_DeployedParent_LanguagePatternsAndCustomConstraints_AreAbsent(t *testing.T) {
	// Neither removed name may keep a required-parent entry.
	staleNames := []string{"LanguagePatterns", "CustomConstraints"}
	got := docformat.DeployedParent
	if got == nil {
		t.Fatal("DeployedParent is nil")
	}
	for _, name := range staleNames {
		if _, ok := got[name]; ok {
			t.Errorf(
				"DeployedParent must not contain %q — it is removed from the nine-name "+
					"tool-managed vocabulary", name,
			)
		}
	}
}

func TestVocabulary_InjectionParent_CustomConstraints_IsAbsent(t *testing.T) {
	// CustomConstraints is deleted outright, not re-catalogued as an injection name —
	// unlike LanguagePatterns, it must not appear in InjectionParent either.
	got := docformat.InjectionParent
	if got == nil {
		t.Fatal("InjectionParent is nil")
	}
	if _, ok := got["CustomConstraints"]; ok {
		t.Error("InjectionParent must not contain \"CustomConstraints\" — it is deleted outright, " +
			"not re-catalogued as an advisory injection name")
	}
}

// ---------------------------------------------------------------------------
// T9.2 — Custom-marker parity assertions (Go side of the cross-implementation table)
//
// Both the Go implementation (mosaic-common/docformat) and the Python implementation
// (Tools/OldAgentsTransform/boundary_constants.py) must satisfy the same four-marker
// parity table. This section pins the Go side; dedicated Python tests pin the Python side.
// Neither implementation's coverage stands in for the other's.
// ---------------------------------------------------------------------------

func TestParity_FourNodeKinds_AllDistinct(t *testing.T) {
	// The marker vocabulary has exactly four distinct kinds: SECTION, INJECTION,
	// DEPLOYED, CUSTOM. Any two of the four must be unequal.
	// This matches the Python requirement that BoundaryKind has exactly four members.
	kinds := []docformat.NodeKind{
		docformat.NodeSection,
		docformat.NodeInjection,
		docformat.NodeDeployed,
		docformat.NodeCustom,
	}
	for i := 0; i < len(kinds); i++ {
		for j := i + 1; j < len(kinds); j++ {
			if kinds[i] == kinds[j] {
				t.Errorf("NodeKind values are not distinct: kinds[%d]=%q == kinds[%d]=%q",
					i, kinds[i], j, kinds[j])
			}
		}
	}
}

func TestParity_NodeCustom_StringValue_IsCustomLowercase(t *testing.T) {
	// Go NodeCustom uses the string "custom". Python BoundaryKind.CUSTOM uses "CUSTOM".
	// The case difference is intentional (Go uses lowercase, Python uses uppercase enum values).
	// This test pins the Go value so both can be verified independently.
	if string(docformat.NodeCustom) != "custom" {
		t.Errorf("NodeCustom string value: want %q, got %q", "custom", string(docformat.NodeCustom))
	}
}

func TestParity_UserOwned_MatchesPythonProjectOwnerKinds(t *testing.T) {
	// The parity table says "content owner = project" for NodeInjection and NodeCustom,
	// and "content owner = tool" or "structure" for NodeDeployed and NodeSection.
	// Go's UserOwned() predicate must reflect this exactly.
	wantTrue := []docformat.NodeKind{docformat.NodeInjection, docformat.NodeCustom}
	for _, k := range wantTrue {
		if !k.UserOwned() {
			t.Errorf("NodeKind(%q).UserOwned() must return true — parity table: content owner = project", k)
		}
	}

	wantFalse := []docformat.NodeKind{docformat.NodeDeployed, docformat.NodeSection}
	for _, k := range wantFalse {
		if k.UserOwned() {
			t.Errorf("NodeKind(%q).UserOwned() must return false — parity table: content owner is not project", k)
		}
	}
}

func TestParity_CustomNameSet_IsOpen_NoErrorForAnyName(t *testing.T) {
	// Parity table: CUSTOM name set = open; unknown name is NOT an error.
	// This matches Python BoundaryKind.CUSTOM where the validator's canonical-name check
	// (E004-class) is not applied and any name is accepted.
	openNames := []string{
		"ProjectNotes",
		"CustomConstraints",
		"ArbitraryProjectFeature",
		"Workflow:quick-fix",
		"any-hyphenated-name",
		// Even names in CanonicalDeployed must not error under NodeCustom:
		"CommunicationProtocol",
		"HarnessConstraints",
		"AuthorityHierarchy",
	}
	for _, name := range openNames {
		_, err := docformat.ClassifyRegion(docformat.NodeCustom, name)
		if err != nil {
			t.Errorf("ClassifyRegion(NodeCustom, %q): must not return an error — "+
				"parity table: CUSTOM name set is open, unknown names are never errors, got: %v",
				name, err)
		}
	}
}

func TestParity_CustomClassification_AlwaysProject(t *testing.T) {
	// Parity table: CUSTOM classification = project, always.
	// A custom region is project-invented content; MOSAIC never claims tool ownership of it.
	// This is the Go-side assertion; the Python side has no equivalent classify call but the
	// same concept applies through the validator's canonical-name check being skipped for CUSTOM.
	names := []string{
		"ProjectNotes",
		"CustomConstraints",
		"CommunicationProtocol", // canonical deployed name — still project under CUSTOM
		"HarnessConstraints",
	}
	for _, name := range names {
		class, err := docformat.ClassifyRegion(docformat.NodeCustom, name)
		if err != nil {
			t.Errorf("ClassifyRegion(NodeCustom, %q): unexpected error: %v", name, err)
			continue
		}
		if class != domain.InjectionProject {
			t.Errorf("ClassifyRegion(NodeCustom, %q): want InjectionProject, got %q — "+
				"parity table: CUSTOM classification is always project", name, class)
		}
	}
}

func TestParity_InjectionDeployedNameSetRules_Unchanged(t *testing.T) {
	// Cross-check: INJECTION name set remains open (no error for unknown names) and
	// DEPLOYED name set remains closed (error for unknown names). These were the rules
	// before CUSTOM was added; adding CUSTOM must not alter them.
	_, injErr := docformat.ClassifyRegion(docformat.NodeInjection, "CompletelyUnknownInjectionName")
	if injErr != nil {
		t.Errorf("ClassifyRegion(NodeInjection, unknownName): must not error — injection name set is open, got: %v", injErr)
	}

	_, depErr := docformat.ClassifyRegion(docformat.NodeDeployed, "CompletelyUnknownDeployedName")
	if depErr == nil {
		t.Error("ClassifyRegion(NodeDeployed, unknownName): must return an error — deployed name set is closed")
	}
}

func TestParity_ValidateRejectsCustomRegionInSourceFile(t *testing.T) {
	// Parity table: legal in a source file = NO for CUSTOM.
	// The validator must emit a "custom-region-in-source" error for [[CUSTOM:]] in a source document.
	// This is the Go-side parity assertion; the Python validator does not distinguish source/deployed
	// documents in the same way, but the property is verified at the Go docformat layer.
	//
	// Fixture: validate_custom_test.go exercises this in depth; this test pins the rule for
	// the parity table rather than repeating the full validate_custom_test.go coverage.
	const srcDoc = `---
id: 999
version: 1.0.0
name: parity-source-custom-test
description: Parity test — custom in source must be rejected
model: {model-identifier}
tools: []
recommended_tier: LOW
tier_rationale: parity
required_skills: []
---

[[SECTION:Identity]]
# Parity Agent

[[CUSTOM:ProjectNotes]]
Custom content in a source file — must be rejected.
[[/CUSTOM:ProjectNotes]]
[[/SECTION:Identity]]
`
	doc, err := docformat.Parse([]byte(srcDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	issues := docformat.Validate(doc, docformat.ValidateOptions{Kind: docformat.DocumentSource})

	if !hasIssueWithCode(issues, "custom-region-in-source") {
		t.Errorf("Validate(DocumentSource) with [[CUSTOM:]] did not emit 'custom-region-in-source'; "+
			"parity table: CUSTOM is not legal in source files. Issues: %v", issues)
	}
}
