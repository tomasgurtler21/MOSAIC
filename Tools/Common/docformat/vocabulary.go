package docformat

func init() {
	// CanonicalSections lists the six canonical MOSAIC section names in their required
	// document order, mirroring boundary_constants.py. CommunicationProtocol is NOT a
	// member; it is a tool-managed boundary name declared as type="managed" and occupies
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
	// satisfied by a top-level <CommunicationProtocol type="managed"> boundary; every other
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

	// CanonicalDeployed lists the nine tool-managed boundary names, a closed set.
	// Every name here must be declared with type="managed" in any document that uses it.
	// ArtifactProvenance, LanguagePatterns, and CustomConstraints are removed;
	// AuthorityHierarchy, ClosingProcedure, ProtocolConstraints, ErrorHandlingCommon,
	// and ExecutionPhilosophyCommon are added.
	CanonicalDeployed = []string{
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

	// DeployedParent maps each tool-managed boundary name to its required parent section.
	// An entry whose value is "" means the boundary must appear at body top level
	// (for example, CommunicationProtocol). Nine entries, mirroring boundary_constants.py.
	DeployedParent = map[string]string{
		"CommunicationProtocol":     "", // top level — must not be nested inside any section
		"AuthorityHierarchy":        "Identity",
		"ClosingProcedure":          "Identity",
		"AvailableWorkflows":        "Identity",
		"InfrastructureAgents":      "Identity",
		"ProtocolConstraints":       "Constraints",
		"HarnessConstraints":        "Constraints",
		"ErrorHandlingCommon":       "ErrorHandling",
		"ExecutionPhilosophyCommon": "ExecutionPhilosophy",
	}

	// CanonicalManagedBlocks lists the tool-managed block names — managed regions the tool
	// emits nested inside a managed parent region. Members have no generator of their own,
	// are not refresh-scope regions, and are not valid bundle targets.
	// Initial content: exactly one entry — "Workflow".
	CanonicalManagedBlocks = []string{"Workflow"}

	// ManagedBlockParent maps each managed block name to the managed region it must be nested
	// inside. "Workflow" must appear inside "AvailableWorkflows".
	ManagedBlockParent = map[string]string{"Workflow": "AvailableWorkflows"}

	// InjectionParent maps each advisory injection name to its usual parent section,
	// mirroring boundary_constants.py. This is an advisory table only: injection names
	// are open, and an unlisted name is preserved like any other and is never flagged.
	// An entry whose value is "" means the injection usually appears at body top level.
	// ArtifactProvenanceExtension, CustomConstraints, and ProtocolExtension are removed;
	// LanguagePatterns is added — moved here from CanonicalDeployed.
	// Projects needing to extend the protocol use a custom ProtocolExtension region instead:
	// MOSAIC defines injection (type="project") slots; projects invent custom (type="custom") ones.
	InjectionParent = map[string]string{
		"IdentityExtension":      "Identity",
		"CodebaseContext":        "Capabilities",
		"LanguagePatterns":       "Capabilities", // moved from CanonicalDeployed
		"OutputArtifactTemplate": "Capabilities",
		"SeverityThresholds":     "Capabilities",
		"SeverityDefinitions":    "Capabilities",
		"ErrorHandlingExtension": "ErrorHandling",
		"ContextLimits":          "ExecutionPhilosophy",
	}
}
