package docformat

func init() {
	// CanonicalSections lists the six canonical MOSAIC section names in their required
	// document order, mirroring boundary_constants.py. CommunicationProtocol is NOT a
	// member; it is a tool-managed boundary name declared with [[DEPLOYED:]] and occupies
	// a top-level slot in CanonicalOrder.
	CanonicalSections = []string{
		"Identity",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}

	// CanonicalOrder lists the seven canonical document slots in required order,
	// mirroring boundary_constants.py. The entry at index 1 is "CommunicationProtocol",
	// satisfied by a top-level [[DEPLOYED:CommunicationProtocol]] boundary; every other
	// entry is a section name. ArtifactProvenance is removed.
	CanonicalOrder = []string{
		"Identity",
		"CommunicationProtocol",
		"Capabilities",
		"Constraints",
		"ErrorHandling",
		"OutputFormat",
		"ExecutionPhilosophy",
	}

	// CanonicalDeployed lists the eleven tool-managed boundary names, a closed set.
	// Every name here must be declared with [[DEPLOYED:]] in any document that uses it.
	// ArtifactProvenance is removed; AuthorityHierarchy, ClosingProcedure,
	// ProtocolConstraints, ErrorHandlingCommon, and ExecutionPhilosophyCommon are added.
	CanonicalDeployed = []string{
		"CommunicationProtocol",
		"AuthorityHierarchy",
		"ClosingProcedure",
		"AvailableWorkflows",
		"InfrastructureAgents",
		"LanguagePatterns",
		"ProtocolConstraints",
		"HarnessConstraints",
		"CustomConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}

	// DeployedParent maps each tool-managed boundary name to its required parent section.
	// An entry whose value is "" means the boundary must appear at body top level
	// (for example, CommunicationProtocol). Eleven entries, mirroring boundary_constants.py.
	DeployedParent = map[string]string{
		"CommunicationProtocol":     "", // top level — must not be nested inside any section
		"AuthorityHierarchy":        "Identity",
		"ClosingProcedure":          "Identity",
		"AvailableWorkflows":        "Identity",
		"InfrastructureAgents":      "Identity",
		"LanguagePatterns":          "Capabilities",
		"ProtocolConstraints":       "Constraints",
		"HarnessConstraints":        "Constraints",
		"CustomConstraints":         "Constraints",
		"ErrorHandlingCommon":       "ErrorHandling",
		"ExecutionPhilosophyCommon": "ExecutionPhilosophy",
	}

	// InjectionParent maps each advisory injection name to its usual parent section,
	// mirroring boundary_constants.py. This is an advisory table only: injection names
	// are open, and an unlisted name is preserved like any other and is never flagged.
	// An entry whose value is "" means the injection usually appears at body top level.
	// ArtifactProvenanceExtension is removed; ProtocolExtension is added.
	InjectionParent = map[string]string{
		"ProtocolExtension":      "", // top level
		"IdentityExtension":      "Identity",
		"CodebaseContext":        "Capabilities",
		"OutputArtifactTemplate": "Capabilities",
		"SeverityThresholds":     "Capabilities",
		"SeverityDefinitions":    "Capabilities",
		"ErrorHandlingExtension": "ErrorHandling",
		"ContextLimits":          "ExecutionPhilosophy",
	}
}
