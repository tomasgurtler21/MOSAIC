package docformat_test

// Tests for CUSTOM marker vocabulary: classification always returns project class,
// and the NodeKind.UserOwned predicate correctly identifies user-owned kinds.
//
// All tests in this file target behaviour not yet implemented (TDD RED phase).
//
// Coverage (ClassifyRegion with NodeCustom):
//   - An arbitrary name not in any registry under NodeCustom → InjectionProject, nil error.
//   - A name in CanonicalDeployed under NodeCustom → InjectionProject, nil error.
//     The NodeCustom branch must be evaluated before the canonical-deployed check so that
//     a canonical name never triggers ErrMarkerMismatch.
//   - Every CanonicalDeployed name under NodeCustom → InjectionProject, nil error.
//   - ClassifyRegion(NodeCustom, ...) never returns an error for any name.
//
// Coverage (NodeKind.UserOwned predicate):
//   - NodeCustom.UserOwned() is true — custom regions are project-owned content.
//   - NodeInjection.UserOwned() is true — injection regions are project-owned content.
//   - NodeDeployed.UserOwned() is false — deployed regions are tool-managed.
//   - NodeSection.UserOwned() is false — sections define document structure, not user content.

import (
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// ---------------------------------------------------------------------------
// ClassifyRegion with NodeCustom
// ---------------------------------------------------------------------------

func TestClassifyRegion_Custom_ArbitraryName_ReturnsProjectClassAndNilError(t *testing.T) {
	// A custom region with an arbitrary name not in any registry must return
	// InjectionProject and a nil error. The custom name set is fully open.
	class, err := docformat.ClassifyRegion(docformat.NodeCustom, "ProjectSpecificSection")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeCustom, \"ProjectSpecificSection\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeCustom, \"ProjectSpecificSection\"): want InjectionProject, got %q", class)
	}
}

func TestClassifyRegion_Custom_CanonicalDeployedName_ReturnsProjectClassNotError(t *testing.T) {
	// A custom region named "CommunicationProtocol" — a name in the canonical deployed
	// registry — must still return InjectionProject and a nil error. The NodeCustom branch
	// must be evaluated before isCanonicalDeployed so that no canonical name under [[CUSTOM:]]
	// ever produces ErrMarkerMismatch.
	class, err := docformat.ClassifyRegion(docformat.NodeCustom, "CommunicationProtocol")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeCustom, \"CommunicationProtocol\"): want nil error (canonical deployed name is project class under [[CUSTOM:]]), got: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeCustom, \"CommunicationProtocol\"): want InjectionProject, got %q", class)
	}
}

func TestClassifyRegion_Custom_HarnessConstraints_ReturnsProjectClassNotError(t *testing.T) {
	// HarnessConstraints is in CanonicalDeployed; under [[CUSTOM:]] it must return
	// InjectionProject and a nil error, not ErrMarkerMismatch.
	class, err := docformat.ClassifyRegion(docformat.NodeCustom, "HarnessConstraints")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeCustom, \"HarnessConstraints\"): want nil error, got: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeCustom, \"HarnessConstraints\"): want InjectionProject, got %q", class)
	}
}

func TestClassifyRegion_Custom_AllCanonicalDeployedNames_ReturnProjectClass(t *testing.T) {
	// Every name in CanonicalDeployed declared under [[CUSTOM:]] must return
	// InjectionProject and a nil error. No canonical name may trigger ErrMarkerMismatch.
	for _, name := range docformat.CanonicalDeployed {
		class, err := docformat.ClassifyRegion(docformat.NodeCustom, name)
		if err != nil {
			t.Errorf("ClassifyRegion(NodeCustom, %q): want nil error (canonical deployed names are still project class under [[CUSTOM:]]), got: %v", name, err)
			continue
		}
		if class != domain.InjectionProject {
			t.Errorf("ClassifyRegion(NodeCustom, %q): want InjectionProject, got %q", name, class)
		}
	}
}

func TestClassifyRegion_Custom_NeverReturnsError_ForAnyName(t *testing.T) {
	// ClassifyRegion(NodeCustom, ...) must never return an error for any name.
	// The custom name set is fully open: unknown names are preserved, not rejected.
	names := []string{
		"ProjectNotes",
		"CustomConstraints",
		"Workflow:quick-fix",
		"my-custom-region",
		"SomeArbitraryUnseenName",
		// Canonical deployed names to confirm no mismatch error under NodeCustom:
		"CommunicationProtocol",
		"HarnessConstraints",
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	for _, name := range names {
		_, err := docformat.ClassifyRegion(docformat.NodeCustom, name)
		if err != nil {
			t.Errorf("ClassifyRegion(NodeCustom, %q): must never return an error (custom names are open), got: %v", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// NodeKind.UserOwned predicate
// ---------------------------------------------------------------------------

func TestNodeKind_UserOwned_Custom_IsUserOwned(t *testing.T) {
	// NodeCustom carries project-invented content that the tool must never discard.
	if !docformat.NodeCustom.UserOwned() {
		t.Error("NodeCustom.UserOwned() must return true — custom regions are project-owned content that the tool must preserve")
	}
}

func TestNodeKind_UserOwned_Injection_IsUserOwned(t *testing.T) {
	// NodeInjection has always been user-owned; UserOwned must confirm this.
	if !docformat.NodeInjection.UserOwned() {
		t.Error("NodeInjection.UserOwned() must return true — injection regions are project-owned content")
	}
}

func TestNodeKind_UserOwned_Deployed_IsNotUserOwned(t *testing.T) {
	// NodeDeployed regions are tool-managed; the tool generates and owns their content.
	if docformat.NodeDeployed.UserOwned() {
		t.Error("NodeDeployed.UserOwned() must return false — deployed regions are tool-managed, not project-owned")
	}
}

func TestNodeKind_UserOwned_Section_IsNotUserOwned(t *testing.T) {
	// NodeSection defines document structure; it does not carry user-owned content in the
	// sense that the transform must preserve.
	if docformat.NodeSection.UserOwned() {
		t.Error("NodeSection.UserOwned() must return false — sections define document structure, not user-owned content")
	}
}
