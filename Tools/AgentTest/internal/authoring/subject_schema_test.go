package authoring_test

// Tests for the Stage 5 declaration-schema changes:
//   - A harness-neutral subject declaration (subject.agent) parses into
//     SubjectUnderTest.CatalogAgentKey.
//   - An optional workflow set (subject.workflows) parses into
//     SubjectUnderTest.Workflows with the nil/empty distinction preserved.
//   - Stub collaborator definitions (stub_agents) parse into
//     TestDefinition.StubAgents.
//   - The previous harness-specific subject.definition key is rejected with
//     a specific "removed-key-subject-definition" diagnostic naming its
//     replacement, rather than falling through to the generic unknown-field
//     error.

import (
	"strings"
	"testing"

	"mosaic-agent-test/internal/authoring"
)

// --- subject.agent (CatalogAgentKey) ---

// TestParseTestDefinition_SubjectAgent_ParsesIntoCatalogAgentKey verifies
// that subject.agent in the YAML is parsed into
// SubjectUnderTest.CatalogAgentKey. This is the harness-neutral replacement
// for subject.definition.
func TestParseTestDefinition_SubjectAgent_ParsesIntoCatalogAgentKey(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  opening_message: "Build the thing."
  invocation_kind: orchestrator
stub_registry: stubs/d.stubs.json
`),
	}

	def, _ := authoring.ParseTestDefinition(src)

	if def.Subject.CatalogAgentKey != "orchestrator" {
		t.Errorf("Subject.CatalogAgentKey = %q, want %q; subject.agent in YAML must parse into CatalogAgentKey",
			def.Subject.CatalogAgentKey, "orchestrator")
	}
}

// TestParseTestDefinition_SubjectAgentWithWorkflows_DefinitionPathNotAuthored
// verifies that when the new subject.agent form is used, DefinitionPath is
// NOT authored into the definition: the sandbox-relative path is resolved at
// render time from the deployment port's own result, and is never a field a
// test author writes.
func TestParseTestDefinition_SubjectAgentWithWorkflows_DefinitionPathNotAuthored(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  workflows: [tdd-soft]
stub_registry: stubs/d.stubs.json
`),
	}

	def, _ := authoring.ParseTestDefinition(src)

	// DefinitionPath is no longer authored. With the new schema it must stay
	// empty after parsing; it is populated only during setup from the
	// deployment port's report.
	if def.Subject.DefinitionPath != "" {
		t.Errorf("Subject.DefinitionPath = %q, want empty string; DefinitionPath must not be authored and must not be derived from subject.agent",
			def.Subject.DefinitionPath)
	}
}

// --- subject.workflows (Workflows) ---

// TestParseTestDefinition_SubjectWorkflowsPinned_ParsesIntoSlice verifies
// that a declared workflow list is parsed into SubjectUnderTest.Workflows.
func TestParseTestDefinition_SubjectWorkflowsPinned_ParsesIntoSlice(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  workflows: [tdd-soft, tdd-hard]
stub_registry: stubs/d.stubs.json
`),
	}

	def, _ := authoring.ParseTestDefinition(src)

	if len(def.Subject.Workflows) != 2 {
		t.Fatalf("len(Subject.Workflows) = %d, want 2; subject.workflows must parse into the Workflows slice",
			len(def.Subject.Workflows))
	}
	if def.Subject.Workflows[0] != "tdd-soft" {
		t.Errorf("Subject.Workflows[0] = %q, want %q", def.Subject.Workflows[0], "tdd-soft")
	}
	if def.Subject.Workflows[1] != "tdd-hard" {
		t.Errorf("Subject.Workflows[1] = %q, want %q", def.Subject.Workflows[1], "tdd-hard")
	}
}

// TestParseTestDefinition_SubjectWorkflowsAbsent_IsNil verifies that when
// subject.workflows is absent from the declaration, SubjectUnderTest.Workflows
// is nil — "not specified" — distinct from a non-nil empty slice meaning
// "explicitly none".
func TestParseTestDefinition_SubjectWorkflowsAbsent_IsNil(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: stubs/d.stubs.json
`),
	}

	def, _ := authoring.ParseTestDefinition(src)

	if def.Subject.Workflows != nil {
		t.Errorf("Subject.Workflows = %v, want nil when workflows is absent from the declaration; absent means unspecified, which is distinct from explicitly-empty",
			def.Subject.Workflows)
	}
}

// TestParseTestDefinition_SubjectWorkflowsExplicitlyEmpty_IsNonNilSlice
// verifies that workflows: [] yields a non-nil empty slice — "explicitly
// none" — distinct from a nil meaning "not specified". This distinction
// matters because the deployment tool treats nil and empty workflows
// differently.
func TestParseTestDefinition_SubjectWorkflowsExplicitlyEmpty_IsNonNilSlice(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  workflows: []
stub_registry: stubs/d.stubs.json
`),
	}

	def, _ := authoring.ParseTestDefinition(src)

	if def.Subject.Workflows == nil {
		t.Error("Subject.Workflows = nil, want non-nil empty slice; workflows: [] means 'explicitly none', not 'not specified'")
	}
	if len(def.Subject.Workflows) != 0 {
		t.Errorf("len(Subject.Workflows) = %d, want 0 for an explicit empty declaration", len(def.Subject.Workflows))
	}
}

// --- stub_agents ---

// TestParseTestDefinition_StubAgents_ParseIntoDefinition verifies that the
// stub_agents top-level key is parsed into TestDefinition.StubAgents, each
// entry carrying the collaborator identity and its source path.
func TestParseTestDefinition_StubAgents_ParseIntoDefinition(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: stubs/d.stubs.json
stub_agents:
  - identity:
      tool: dispatch
      agent: codebase-research
    source: ../agents/codebase-research.md
  - identity:
      tool: dispatch
      agent: planner
    source: ../agents/planner.md
`),
	}

	def, _ := authoring.ParseTestDefinition(src)

	if len(def.StubAgents) != 2 {
		t.Fatalf("len(StubAgents) = %d, want 2; stub_agents in YAML must parse into TestDefinition.StubAgents",
			len(def.StubAgents))
	}

	first := def.StubAgents[0]
	if first.Identity.ToolName != "dispatch" {
		t.Errorf("StubAgents[0].Identity.ToolName = %q, want %q", first.Identity.ToolName, "dispatch")
	}
	if first.Identity.AgentIdentity != "codebase-research" {
		t.Errorf("StubAgents[0].Identity.AgentIdentity = %q, want %q", first.Identity.AgentIdentity, "codebase-research")
	}
	if first.SourcePath != "../agents/codebase-research.md" {
		t.Errorf("StubAgents[0].SourcePath = %q, want %q", first.SourcePath, "../agents/codebase-research.md")
	}

	second := def.StubAgents[1]
	if second.Identity.AgentIdentity != "planner" {
		t.Errorf("StubAgents[1].Identity.AgentIdentity = %q, want %q", second.Identity.AgentIdentity, "planner")
	}
}

// TestParseTestDefinition_StubAgentEmptyAgentIdentity_ParsesAsPlainToolCall
// verifies that a stub_agents entry with an empty "agent" in its identity (a
// tool-only stub, where "agent" is omitted from the identity block) is parsed
// correctly as a plain tool call — a non-zero identity with a tool name but
// no agent name. This mirrors the stub-registry precedent established by
// TestParseStubRegistry_EmptyAgentIdentity_ParsesAsPlainToolCall: the schema
// must accept tool-only identities in both declaration surfaces consistently.
func TestParseTestDefinition_StubAgentEmptyAgentIdentity_ParsesAsPlainToolCall(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: stubs/d.stubs.json
stub_agents:
  - identity:
      tool: Read
    source: agents/read-stub.md
`),
	}

	def, _ := authoring.ParseTestDefinition(src)

	if len(def.StubAgents) != 1 {
		t.Fatalf("len(StubAgents) = %d, want 1; a tool-only stub_agents entry must parse successfully",
			len(def.StubAgents))
	}

	identity := def.StubAgents[0].Identity
	if identity.ToolName != "Read" {
		t.Errorf("StubAgents[0].Identity.ToolName = %q, want %q; tool name must be preserved",
			identity.ToolName, "Read")
	}
	if identity.AgentIdentity != "" {
		t.Errorf("StubAgents[0].Identity.AgentIdentity = %q, want empty string; a tool-only stub has no agent identity",
			identity.AgentIdentity)
	}
	if identity.IsZero() {
		t.Error("StubAgents[0].Identity.IsZero() = true, want false; a tool name alone is a valid non-absent identity")
	}
	if def.StubAgents[0].SourcePath != "agents/read-stub.md" {
		t.Errorf("StubAgents[0].SourcePath = %q, want %q", def.StubAgents[0].SourcePath, "agents/read-stub.md")
	}
}

// TestParseTestDefinition_StubAgentsAbsent_IsNil verifies that when no
// stub_agents key is declared, TestDefinition.StubAgents is nil.
func TestParseTestDefinition_StubAgentsAbsent_IsNil(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: stubs/d.stubs.json
`),
	}

	def, _ := authoring.ParseTestDefinition(src)

	if def.StubAgents != nil {
		t.Errorf("StubAgents = %v, want nil when no stub_agents key is declared", def.StubAgents)
	}
}

// --- subject.definition removed-key diagnostic ---

// TestParseTestDefinition_SubjectDefinitionKey_RejectedAsRemovedKey verifies
// that a test definition still using subject.definition is rejected with an
// error, not silently accepted. The subject's sandbox-relative definition path
// is now resolved by the deployment port during setup and is never authored.
func TestParseTestDefinition_SubjectDefinitionKey_RejectedAsRemovedKey(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  definition: .claude/agents/orchestrator.md
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if !report.HasErrors() {
		t.Fatal("expected a test definition using subject.definition to be rejected with an error; " +
			"subject.definition is a removed key — the sandbox-relative path is now resolved " +
			"from the deployment port's own result, and must not be authored")
	}
	if !hasDiagnosticCode(report, "removed-key-subject-definition") {
		t.Errorf("expected a diagnostic with code %q, got: %v; the diagnostic must specifically "+
			"identify the removed key rather than falling through to a generic unknown-field error",
			"removed-key-subject-definition", report.Diagnostics)
	}
}

// TestParseTestDefinition_SubjectDefinitionKey_DiagnosticHasCorrectPointer
// verifies that the removed-key-subject-definition diagnostic's Pointer field
// identifies the exact location of the offending key.
func TestParseTestDefinition_SubjectDefinitionKey_DiagnosticHasCorrectPointer(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  definition: .claude/agents/orchestrator.md
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	var found *authoring.Diagnostic
	for i := range report.Diagnostics {
		if report.Diagnostics[i].Code == "removed-key-subject-definition" {
			d := report.Diagnostics[i]
			found = &d
			break
		}
	}
	if found == nil {
		t.Fatalf("expected diagnostic with code %q, none found in: %v",
			"removed-key-subject-definition", report.Diagnostics)
	}
	if found.Pointer != "subject.definition" {
		t.Errorf("diagnostic Pointer = %q, want %q; the pointer must identify the exact removed key",
			found.Pointer, "subject.definition")
	}
}

// TestParseTestDefinition_SubjectDefinitionKey_DiagnosticNamesReplacement
// verifies that the removed-key-subject-definition diagnostic's message names
// subject.agent as the replacement and conveys that the path is now resolved
// by the deployment port rather than authored.
func TestParseTestDefinition_SubjectDefinitionKey_DiagnosticNamesReplacement(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  definition: .claude/agents/orchestrator.md
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	var found *authoring.Diagnostic
	for i := range report.Diagnostics {
		if report.Diagnostics[i].Code == "removed-key-subject-definition" {
			d := report.Diagnostics[i]
			found = &d
			break
		}
	}
	if found == nil {
		t.Fatalf("expected diagnostic with code %q, none found in: %v",
			"removed-key-subject-definition", report.Diagnostics)
	}
	if !strings.Contains(found.Message, "subject.agent") {
		t.Errorf("diagnostic Message = %q, want it to name %q as the replacement field",
			found.Message, "subject.agent")
	}
}
