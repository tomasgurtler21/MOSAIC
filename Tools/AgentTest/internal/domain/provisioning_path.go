package domain

// ProvisioningPath names how a test's agent definitions reach the sandbox.
// The two concrete paths are mutually exclusive; declaring both is a
// validation error caught by preflight before any sandbox is created.
type ProvisioningPath string

const (
	// ProvisioningCatalog deploys the subject and every agent its workflows
	// reference in one call against the configured catalogue. It is selected
	// when the test definition names a catalogue agent key and declares no
	// stub_agents.
	ProvisioningCatalog ProvisioningPath = "catalog"

	// ProvisioningStubAgents renders the subject and each explicitly declared
	// stub agent source file, one call per file. It is selected when the test
	// definition declares at least one stub_agents entry.
	ProvisioningStubAgents ProvisioningPath = "stub-agents"

	// ProvisioningConflicted means the declaration selects both paths at once.
	// It is not runnable; preflight rejects it with a hard error before any
	// sandbox is created.
	ProvisioningConflicted ProvisioningPath = "conflicted"
)

// ProvisioningPath reports how this definition's agent definitions reach the
// sandbox. It is the single authority on the question: the runner and
// preflight both consult it rather than each re-deriving the condition
// independently.
//
//	subject agent key + no stub_agents  -> ProvisioningCatalog
//	stub_agents declared                -> ProvisioningStubAgents
//	subject agent key + stub_agents     -> ProvisioningConflicted
//
// A definition declaring neither a subject agent key nor stub_agents is not
// this predicate's problem: the existing missing-subject-agent check owns it.
//
// ProvisioningPath reports which path this definition's agent definitions
// reach the sandbox through, based solely on field values. The precedence is:
//  1. Both catalogue key and stub_agents declared → ProvisioningConflicted
//  2. Only catalogue key → ProvisioningCatalog
//  3. Otherwise (stub_agents only, or neither) → ProvisioningStubAgents
func (d TestDefinition) ProvisioningPath() ProvisioningPath {
	hasCatalogKey := d.Subject.CatalogAgentKey != ""
	hasStubs := len(d.StubAgents) > 0

	switch {
	case hasCatalogKey && hasStubs:
		return ProvisioningConflicted
	case hasCatalogKey:
		return ProvisioningCatalog
	default:
		return ProvisioningStubAgents
	}
}
