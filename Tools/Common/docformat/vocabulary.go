package docformat

import "mosaic-common/mosaic"

func init() {
	// CanonicalSections lists the eight MOSAIC section names in their required document
	// order, mirroring boundary_constants.py.
	CanonicalSections = []string{
		"Identity",
		"CommunicationProtocol",
		"ArtifactProvenance",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}

	// CanonicalInjections lists the thirteen canonical MOSAIC injection names, mirroring
	// boundary_constants.py.
	CanonicalInjections = []string{
		"IdentityExtension",
		"ProtocolExtension",
		"ArtifactProvenanceExtension",
		"LanguagePatterns",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"HarnessConstraints",
		"CustomConstraints",
		"ErrorHandlingExtension",
		"ContextLimits",
		"SeverityThresholds",
		"SeverityDefinitions",
		"AvailableWorkflows",
	}

	// InjectionParent maps each canonical injection name to the name of the section in
	// which it is expected to appear, mirroring boundary_constants.py.
	InjectionParent = map[string]string{
		"IdentityExtension":           "Identity",
		"ProtocolExtension":           "CommunicationProtocol",
		"ArtifactProvenanceExtension": "ArtifactProvenance",
		"LanguagePatterns":            "Capabilities",
		"CodebaseContext":        "Capabilities",
		"OutputArtifactTemplate": "Capabilities",
		"SeverityThresholds":     "Capabilities",
		"SeverityDefinitions":    "Capabilities",
		"HarnessConstraints":     "Constraints",
		"CustomConstraints":      "Constraints",
		"ErrorHandlingExtension": "ErrorHandling",
		"ContextLimits":          "ExecutionPhilosophy",
		"AvailableWorkflows":     "Identity",
	}
}

// ClassifyInjection returns the InjectionClass for a named injection.
//
// Classification rules (mirroring boundary_constants.py):
//   - "AvailableWorkflows" → InjectionWorkflow (assembled from workflow selections)
//   - "HarnessConstraints", "LanguagePatterns", "CustomConstraints" → InjectionHarness (filled from descriptor)
//   - all other names (canonical or not) → InjectionProject (project-specific, always empty on create)
//
// "CustomConstraints" is InjectionHarness so that VS Code GHCP can fill it with the parallel
// tool calls instruction at the harness level. Other harnesses return ok=false for
// Injection("CustomConstraints"), so applyHarnessInjection leaves it empty for them —
// consistent with project-level semantics everywhere except VS Code GHCP.
func ClassifyInjection(name string) mosaic.InjectionClass {
	switch name {
	case "AvailableWorkflows":
		return mosaic.InjectionWorkflow
	case "HarnessConstraints", "LanguagePatterns", "CustomConstraints":
		return mosaic.InjectionHarness
	default:
		return mosaic.InjectionProject
	}
}
