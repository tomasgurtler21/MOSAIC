package docformat

import (
	"errors"
	"fmt"

	"mosaic-common/mosaic"
)

// CanonicalOrder lists the eight canonical document slots in required order.
// Entry at index 1 is "CommunicationProtocol", satisfied by a top-level
// [[DEPLOYED:CommunicationProtocol]] boundary; every other entry is a section name.
// This is the list the document-order check walks.
//
// Populated in vocabulary.go init().
var CanonicalOrder []string

// CanonicalDeployed lists the tool-managed boundary names. A name in this list must
// be declared with [[DEPLOYED:]] in any document that uses it.
//
// Populated in vocabulary.go init().
var CanonicalDeployed []string

// DeployedParent maps each tool-managed boundary name to its required parent section.
// An entry whose value is "" means the boundary must appear at body top level
// (for example, CommunicationProtocol).
//
// Populated in vocabulary.go init().
var DeployedParent map[string]string

// ErrMarkerMismatch reports a canonical name declared with the wrong marker kind.
// A tool-managed name found under [[INJECTION:]] or a user-owned name found under
// [[DEPLOYED:]] both wrap this error.
var ErrMarkerMismatch = errors.New("boundary name declared with the wrong marker")

// ErrUnknownDeployedName reports a [[DEPLOYED:]] region whose name is not in the
// canonical tool-managed registry. An unrecognised tool-managed name has no generator
// and cannot be filled.
var ErrUnknownDeployedName = errors.New("unrecognised tool-managed boundary name")

// ExpectedMarker returns the marker kind a canonical name must be declared with,
// and whether the name is in any canonical registry.
//
//   - For a tool-managed name (CanonicalDeployed), returns (NodeDeployed, true).
//   - For a user-owned name (CanonicalInjections), returns (NodeInjection, true).
//   - For a name in neither registry, returns ("", false).
func ExpectedMarker(name string) (kind NodeKind, known bool) {
	for _, n := range CanonicalDeployed {
		if n == name {
			return NodeDeployed, true
		}
	}
	for _, n := range CanonicalInjections {
		if n == name {
			return NodeInjection, true
		}
	}
	return "", false
}

// ClassifyRegion returns the InjectionClass for a region declared with the given
// marker kind and name.
//
//   - A tool-managed name under NodeDeployed returns its class and a nil error.
//   - A user-owned name under NodeInjection returns InjectionProject and a nil error.
//   - A canonical name under the wrong marker returns an error wrapping ErrMarkerMismatch.
//   - A name in neither registry under NodeDeployed returns an error wrapping
//     ErrUnknownDeployedName — an unrecognised tool-managed name has no generator.
//   - A name in neither registry under NodeInjection returns InjectionProject and
//     a nil error, preserving the existing "unknown injections default to project"
//     behaviour that AllowUnknownInjections governs at validation time.
func ClassifyRegion(kind NodeKind, name string) (mosaic.InjectionClass, error) {
	isDeployed := isCanonicalDeployed(name)
	isInjection := isCanonicalInjection(name)

	if kind == NodeDeployed {
		if isInjection {
			// User-owned name declared under the wrong marker.
			return "", fmt.Errorf("name %q requires [[INJECTION:]] but was declared with [[DEPLOYED:]]: %w", name, ErrMarkerMismatch)
		}
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
	// Both canonical user-owned names and unknown names return InjectionProject
	// under NodeInjection. AllowUnknownInjections governs validation-time behaviour.
	return mosaic.InjectionProject, nil
}

// classifyDeployedName returns the InjectionClass for a name that is in CanonicalDeployed.
func classifyDeployedName(name string) mosaic.InjectionClass {
	switch name {
	case "CommunicationProtocol":
		return mosaic.InjectionProtocol
	case "AvailableWorkflows":
		return mosaic.InjectionWorkflow
	case "InfrastructureAgents":
		return mosaic.InjectionInfrastructure
	default:
		// HarnessConstraints, CustomConstraints, LanguagePatterns, and any future
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
