package docformat

func init() {
	// CanonicalSections lists the seven canonical MOSAIC section names in their required
	// document order, mirroring boundary_constants.py. CommunicationProtocol is NOT a
	// member; it is a tool-managed boundary name declared with [[DEPLOYED:]].
	CanonicalSections = []string{
		"Identity",
		"ArtifactProvenance",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}

	// CanonicalOrder lists the eight canonical document slots in required order,
	// mirroring boundary_constants.py. Entry at index 1 is "CommunicationProtocol",
	// satisfied by a top-level [[DEPLOYED:CommunicationProtocol]] boundary; every other
	// entry is a section name. This is the list the document-order check walks.
	CanonicalOrder = []string{
		"Identity",
		"CommunicationProtocol",
		"ArtifactProvenance",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}

	// CanonicalDeployed lists the tool-managed boundary names that must be declared
	// with [[DEPLOYED:]] in any document that uses them.
	CanonicalDeployed = []string{
		"CommunicationProtocol",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"LanguagePatterns",
		"HarnessConstraints",
		"CustomConstraints",
	}

	// DeployedParent maps each tool-managed boundary name to its required parent section.
	// An entry whose value is "" means the boundary must appear at body top level
	// (for example, CommunicationProtocol).
	DeployedParent = map[string]string{
		"CommunicationProtocol": "", // top level — must not be nested inside any section
		"AvailableWorkflows":    "Identity",
		"InfrastructureAgents": "Identity",
		"LanguagePatterns":     "Capabilities",
		"HarnessConstraints":   "Constraints",
		"CustomConstraints":    "Constraints",
	}

	// CanonicalInjections lists the eight user-owned injection names, mirroring
	// boundary_constants.py. Tool-managed names and ProtocolExtension are NOT members.
	CanonicalInjections = []string{
		"IdentityExtension",
		"ArtifactProvenanceExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"ErrorHandlingExtension",
		"ContextLimits",
		"SeverityThresholds",
		"SeverityDefinitions",
	}

	// InjectionParent maps each user-owned injection name to the name of the section in
	// which it is expected to appear, mirroring boundary_constants.py. Tool-managed names
	// and ProtocolExtension are NOT members.
	InjectionParent = map[string]string{
		"IdentityExtension":           "Identity",
		"ArtifactProvenanceExtension": "ArtifactProvenance",
		"CodebaseContext":             "Capabilities",
		"OutputArtifactTemplate":      "Capabilities",
		"SeverityThresholds":          "Capabilities",
		"SeverityDefinitions":         "Capabilities",
		"ErrorHandlingExtension":      "ErrorHandling",
		"ContextLimits":               "ExecutionPhilosophy",
	}
}
