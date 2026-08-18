package docformat

import (
	"errors"
	"fmt"

	"mosaic-common/mosaic"
)

// CanonicalOrder lists the seven canonical document slots in required order.
// Entry at index 1 is "CommunicationProtocol", satisfied by a top-level
// <CommunicationProtocol type="managed"> boundary; every other entry is a section name.
// This is the list the document-order check walks.
//
// Populated in vocabulary.go init().
var CanonicalOrder []string

// CanonicalDeployed lists the tool-managed boundary names, a closed set of nine.
// A name in this list must be declared with type="managed" in any document that uses it.
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
// A tool-managed name found under type="project" (injection) instead of type="managed" wraps this error.
var ErrMarkerMismatch = errors.New("boundary name declared with the wrong marker")

// ErrUnknownDeployedName reports a managed region (type="managed") whose name is not in
// the canonical tool-managed registry. An unrecognised tool-managed name has no generator
// and cannot be filled.
var ErrUnknownDeployedName = errors.New("unrecognised tool-managed boundary name")

// ErrUnclassifiedDeployedName reports a canonical tool-managed name for which the
// classifier holds no explicit case. Such a name has no registered generator and
// must never be given a class by default.
var ErrUnclassifiedDeployedName = errors.New("tool-managed boundary name has no registered generator")

// ExpectedMarker returns the marker kind a canonical name must be declared with,
// and whether the name is in the tool-managed registry.
//
//   - A name in CanonicalDeployed returns (NodeDeployed, true).
//   - A managed block name (bare or compound form) returns (NodeDeployed, true).
//   - Every other name returns ("", false). Injection names are open: there is no
//     user-owned registry to consult.
func ExpectedMarker(name string) (kind NodeKind, known bool) {
	for _, n := range CanonicalDeployed {
		if n == name {
			return NodeDeployed, true
		}
	}
	if IsManagedBlockName(name) {
		return NodeDeployed, true
	}
	return "", false
}

// ClassifyRegion returns the InjectionClass for a region declared with the given
// marker kind and name.
//
//   - A tool-managed name under NodeDeployed returns its class and a nil error.
//   - A tool-managed name under NodeDeployed with no registered classifier case returns
//     an error wrapping ErrUnclassifiedDeployedName.
//   - A tool-managed name under NodeInjection returns an error wrapping ErrMarkerMismatch.
//   - A name not in CanonicalDeployed under NodeDeployed returns an error wrapping
//     ErrUnknownDeployedName — an unrecognised tool-managed name has no generator.
//   - Any name under NodeInjection that is not tool-managed returns InjectionProject and a
//     nil error. Unknown injection names are preserved, never rejected.
func ClassifyRegion(kind NodeKind, name string) (mosaic.InjectionClass, error) {
	// NodeCustom: always project class for every name. The custom name set is fully open;
	// a name that also appears in CanonicalDeployed is still project class, because a custom
	// region can never be tool-managed. This branch must be evaluated before isCanonicalDeployed
	// so that a canonical managed name under type="custom" never triggers ErrMarkerMismatch.
	if kind == NodeCustom {
		return mosaic.InjectionProject, nil
	}

	isDeployed := isCanonicalDeployed(name)

	if kind == NodeDeployed {
		// Managed block names are nested tool-emitted blocks (e.g. Workflow inside
		// AvailableWorkflows). They are not in CanonicalDeployed but are still valid
		// managed regions; classify by tag-name prefix via classifyManagedBlockName.
		if IsManagedBlockName(name) {
			return classifyManagedBlockName(name)
		}
		if !isDeployed {
			// Unrecognised tool-managed name — no generator exists.
			return "", fmt.Errorf("name %q is not a recognised tool-managed boundary name: %w", name, ErrUnknownDeployedName)
		}
		// Correctly declared tool-managed name — determine class by name.
		return classifyDeployedName(name)
	}

	// kind == NodeInjection (or NodeSection, but ClassifyRegion is not called for sections).
	if isDeployed {
		// Tool-managed name declared under the wrong marker.
		return "", fmt.Errorf("name %q requires type=\"managed\" but was declared with type=\"project\": %w", name, ErrMarkerMismatch)
	}
	// Injection names are open: any name not in CanonicalDeployed returns InjectionProject.
	// Unknown injection names are preserved, never rejected.
	return mosaic.InjectionProject, nil
}

// classifyDeployedName returns the InjectionClass for a name that is in CanonicalDeployed.
// Every case is explicit: a canonical name with no registered case returns an error
// wrapping ErrUnclassifiedDeployedName rather than silently inheriting a default class.
func classifyDeployedName(name string) (mosaic.InjectionClass, error) {
	switch name {
	case "CommunicationProtocol":
		return mosaic.InjectionProtocol, nil
	case "AvailableWorkflows":
		return mosaic.InjectionWorkflow, nil
	case "InfrastructureAgents":
		return mosaic.InjectionInfrastructure, nil
	case "AuthorityHierarchy", "ClosingProcedure", "ProtocolConstraints",
		"ErrorHandlingCommon", "ExecutionPhilosophyCommon":
		return mosaic.InjectionBundle, nil
	case "HarnessConstraints":
		return mosaic.InjectionHarness, nil
	default:
		// A canonical tool-managed name with no registered generator. This must never
		// silently answer a class by default.
		return "", fmt.Errorf("name %q has no registered classifier case: %w", name, ErrUnclassifiedDeployedName)
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

// classifyManagedBlockName returns the InjectionClass for a managed block name (a name in
// CanonicalManagedBlocks, matched on tag-name prefix). Every prefix has an explicit case;
// a prefix with no registered case returns an error wrapping ErrUnclassifiedDeployedName.
func classifyManagedBlockName(name string) (mosaic.InjectionClass, error) {
	prefix := TagNamePrefix(name)
	switch prefix {
	case "Workflow":
		return mosaic.InjectionWorkflow, nil
	default:
		return "", fmt.Errorf("managed block name %q (prefix %q) has no registered classifier case: %w", name, prefix, ErrUnclassifiedDeployedName)
	}
}
