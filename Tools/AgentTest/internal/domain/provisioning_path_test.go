package domain_test

// Tests for TestDefinition.ProvisioningPath(), the single authority on which
// provisioning path a test definition selects.
//
// The three-way predicate is a pure function on a TestDefinition value: no
// filesystem, no port, no sandbox. All three outcomes are tested in isolation,
// and all three must derive from the same function so the runner and preflight
// cannot drift apart.
//
// These tests are in the TDD RED phase: they compile against the stub
// implementation in provisioning_path.go and will fail until I7.1 provides
// the real predicate logic.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
)

// TestProvisioningPath_CatalogAgentKey_NoStubAgents_ReturnsCatalog asserts
// that a definition naming a catalogue agent key and declaring no stub_agents
// is routed to the catalogue path. This is the definition shape for a test
// that deploys via the configured test catalogue in one call.
func TestProvisioningPath_CatalogAgentKey_NoStubAgents_ReturnsCatalog(t *testing.T) {
	def := domain.TestDefinition{
		Subject: domain.SubjectUnderTest{
			CatalogAgentKey: "orchestrator",
		},
		// StubAgents deliberately absent
	}

	got := def.ProvisioningPath()
	if got != domain.ProvisioningCatalog {
		t.Errorf("ProvisioningPath() = %q, want %q; "+
			"a definition with a catalogue agent key and no stub_agents must "+
			"select the catalogue path so the runner issues one Deploy call",
			got, domain.ProvisioningCatalog)
	}
}

// TestProvisioningPath_StubAgentsDeclared_ReturnsStubAgentsPath asserts that
// a definition with at least one stub_agents entry selects the stub-agents
// path, regardless of whether a catalogue agent key is also set. The runner
// must use the per-file Render loop for this path.
func TestProvisioningPath_StubAgentsDeclared_ReturnsStubAgentsPath(t *testing.T) {
	def := domain.TestDefinition{
		StubAgents: []domain.StubAgent{
			{
				Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "researcher"},
				SourcePath: "/agents/researcher.md",
			},
		},
		// CatalogAgentKey absent — pure stub-agents path
	}

	got := def.ProvisioningPath()
	if got != domain.ProvisioningStubAgents {
		t.Errorf("ProvisioningPath() = %q, want %q; "+
			"a definition declaring stub_agents must select the stub-agents path "+
			"so the runner uses the per-file Render loop",
			got, domain.ProvisioningStubAgents)
	}
}

// TestProvisioningPath_BothCatalogKeyAndStubAgents_ReturnsConflicted asserts
// that a definition naming both a catalogue agent key and stub_agents is
// reported as conflicted. Preflight must reject this before any sandbox is
// created; the runner must never see it.
//
// TDD RED phase note: in RED phase this test passes coincidentally because the
// stub implementation returns ProvisioningConflicted unconditionally for all
// inputs. It provides no protective signal in RED phase; the real protection
// comes from the other two path cases failing as expected. Post-implementation,
// when the predicate selects by field values rather than returning a constant,
// this test correctly validates the conflicted branch.
func TestProvisioningPath_BothCatalogKeyAndStubAgents_ReturnsConflicted(t *testing.T) {
	def := domain.TestDefinition{
		Subject: domain.SubjectUnderTest{
			CatalogAgentKey: "orchestrator",
		},
		StubAgents: []domain.StubAgent{
			{
				Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "researcher"},
				SourcePath: "/agents/researcher.md",
			},
		},
	}

	got := def.ProvisioningPath()
	if got != domain.ProvisioningConflicted {
		t.Errorf("ProvisioningPath() = %q, want %q; "+
			"a definition declaring both a catalogue agent key and stub_agents is "+
			"conflicted and must be rejected by preflight",
			got, domain.ProvisioningConflicted)
	}
}

// TestProvisioningPath_IsAMethod_OnTestDefinition asserts that
// ProvisioningPath is a method on the TestDefinition value type, not a
// package-level function. This is the structural guarantee that runner and
// preflight both call the same predicate through the same receiver, preventing
// drift.
func TestProvisioningPath_IsAMethod_OnTestDefinition(t *testing.T) {
	// Compile-time: if ProvisioningPath() is not a method on TestDefinition,
	// this will not compile. The runtime assertion is unreachable if the
	// compile passes, which is the point.
	var def domain.TestDefinition
	result := def.ProvisioningPath()

	// The result must be one of the three known constants.
	switch result {
	case domain.ProvisioningCatalog, domain.ProvisioningStubAgents, domain.ProvisioningConflicted:
		// Expected — one of the defined values
	default:
		t.Errorf("ProvisioningPath() returned %q, want one of the defined ProvisioningPath constants; "+
			"an undefined return value indicates the predicate is returning outside its contract",
			result)
	}
}

// TestProvisioningPath_CataloguePathPredicateIsExclusive asserts that a
// definition whose ProvisioningPath() returns ProvisioningCatalog does NOT
// also return ProvisioningStubAgents or ProvisioningConflicted — the three
// values are mutually exclusive.
func TestProvisioningPath_CataloguePathPredicateIsExclusive(t *testing.T) {
	catalogueDef := domain.TestDefinition{
		Subject: domain.SubjectUnderTest{CatalogAgentKey: "orchestrator"},
	}
	stubAgentsDef := domain.TestDefinition{
		StubAgents: []domain.StubAgent{
			{Identity: domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "researcher"}},
		},
	}
	conflictedDef := domain.TestDefinition{
		Subject:    domain.SubjectUnderTest{CatalogAgentKey: "orchestrator"},
		StubAgents: []domain.StubAgent{{Identity: domain.CollaboratorIdentity{ToolName: "dispatch"}}},
	}

	cases := []struct {
		name string
		def  domain.TestDefinition
		want domain.ProvisioningPath
	}{
		{"catalogue_key_only", catalogueDef, domain.ProvisioningCatalog},
		{"stub_agents_only", stubAgentsDef, domain.ProvisioningStubAgents},
		{"both_declared", conflictedDef, domain.ProvisioningConflicted},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.def.ProvisioningPath()
			if got != tc.want {
				t.Errorf("ProvisioningPath() = %q, want %q", got, tc.want)
			}
		})
	}
}
