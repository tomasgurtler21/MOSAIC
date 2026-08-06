package docformat

import (
	"errors"
	"fmt"

	"mosaic-common/mosaic"
)

// CanonicalOrder lists the seven canonical document slots in required order.
// Entry at index 1 is "CommunicationProtocol", satisfied by a top-level
// [[DEPLOYED:CommunicationProtocol]] boundary; every other entry is a section name.
// This is the list the document-order check walks.
//
// Populated in vocabulary.go init().
var CanonicalOrder []string

// CanonicalDeployed lists the tool-managed boundary names, a closed set of eleven.
// A name in this list must be declared with [[DEPLOYED:]] in any document that uses it.
//
// Populated in vocabulary.go init().
var CanonicalDeployed []string

// DeployedParent maps each tool-managed boundary name to its required parent section.
// An entry whose value is "" means the boundary must appear at body top level
// (for example, CommunicationProtocol).
//
// Populated in vocabulary.go init().
var DeployedParent map[string]string

// ErrMarkerMismatch reports a tool-managed name declared with the wrong marker kind.
// A tool-managed name found under [[INJECTION:]] wraps this error.
var ErrMarkerMismatch = errors.New("boundary name declared with the wrong marker")

// ErrUnknownDeployedName reports a [[DEPLOYED:]] region whose name is not in the
// canonical tool-managed registry. An unrecognised tool-managed name has no generator
// and cannot be filled.
var ErrUnknownDeployedName = errors.New("unrecognised tool-managed boundary name")

// ExpectedMarker returns the marker kind a canonical name must be declared with,
// and whether the name is in the tool-managed registry.
//
//   - A name in CanonicalDeployed returns (NodeDeployed, true).
//   - Every other name returns ("", false). Injection names are open: there is no
//     user-owned registry to consult.
func ExpectedMarker(name string) (kind NodeKind, known bool) {
	for _, n := range CanonicalDeployed {
		if n == name {
			return NodeDeployed, true
		}
	}
	return "", false
}

// ClassifyRegion returns the InjectionClass for a region declared with the given
// marker kind and name.
//
//   - A tool-managed name under NodeDeployed returns its class and a nil error.
//   - A tool-managed name under NodeInjection returns an error wrapping ErrMarkerMismatch.
//   - A name not in CanonicalDeployed under NodeDeployed returns an error wrapping
//     ErrUnknownDeployedName — an unrecognised tool-managed name has no generator.
//   - Any name under NodeInjection that is not tool-managed returns InjectionProject and a
//     nil error. Unknown injection names are preserved, never rejected.
func ClassifyRegion(kind NodeKind, name string) (mosaic.InjectionClass, error) {
	isDeployed := isCanonicalDeployed(name)

	if kind == NodeDeployed {
		if !isDeployed {
			// Unrecognised tool-managed name — no generator exists.
			return "", fmt.Errorf("name %q is not a recognised tool-managed boundary name: %w", name, ErrUnknownDeployedName)
		}
		// Correctly declared tool-managed name — determine class by name.
		return classifyDeployedName(name), nil
	}

	// kind == NodeInjection (or NodeSection, but ClassifyRegion is not called for sections).
	if isDeployed {
		// Tool-managed name declared under the wrong marker.
		return "", fmt.Errorf("name %q requires [[DEPLOYED:]] but was declared with [[INJECTION:]]: %w", name, ErrMarkerMismatch)
	}
	// Injection names are open: any name not in CanonicalDeployed returns InjectionProject.
	// Unknown injection names are preserved, never rejected.
	return mosaic.InjectionProject, nil
}

// classifyDeployedName returns the InjectionClass for a name that is in CanonicalDeployed.
// The bundle branch is an explicit case list: a new tool-managed name falls to InjectionHarness
// and must be classified deliberately to avoid silently inheriting bundle behaviour.
func classifyDeployedName(name string) mosaic.InjectionClass {
	switch name {
	case "CommunicationProtocol":
		return mosaic.InjectionProtocol
	case "AvailableWorkflows":
		return mosaic.InjectionWorkflow
	case "InfrastructureAgents":
		return mosaic.InjectionInfrastructure
	case "AuthorityHierarchy", "ClosingProcedure", "ProtocolConstraints",
		"ErrorHandlingCommon", "ExecutionPhilosophyCommon":
		return mosaic.InjectionBundle
	default:
		// LanguagePatterns, HarnessConstraints, CustomConstraints, and any future
		// tool-managed harness names.
		return mosaic.InjectionHarness
	}
}

// isCanonicalDeployed reports whether name appears in CanonicalDeployed.
func isCanonicalDeployed(name string) bool {
	for _, n := range CanonicalDeployed {
		if n == name {
			return true
		}
	}
	return false
}
