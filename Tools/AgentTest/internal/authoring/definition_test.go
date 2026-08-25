package authoring_test

// Tests for ParseTestDefinition: parsing and schema-validating a test
// definition document (`*.test.yaml`) into domain.TestDefinition.
//
// Coverage follows T4.1's list of declared concepts a test definition
// carries: layer, the negative inversion flag, repetitions with a declared
// pass rate, timeout and turn limit, parallel-group declarations, seeded
// pre-run files, and every assertion class the evaluator supports (AC4.3).

import (
	"fmt"
	"testing"
	"time"

	"mosaic-agent-test/internal/authoring"
	"mosaic-agent-test/internal/domain"
)

const wellFormedDefinition = `
schema_version: 1
id: happy-path
description: Full workflow routes research before planning
layer: orchestrator
negative: false

subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
  opening_message: |
    Build the thing described in Requirements.md.
  invocation_kind: orchestrator
  allowed_tools: [Task, Read, Write, Edit]

stub_registry: stubs/happy-path.stubs.json
timeout: 15m
turn_limit: 60
stop_after_invocations: 0

assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
`

func TestParseTestDefinition_WellFormed_PopulatesCoreFields(t *testing.T) {
	src := authoring.Source{Path: "happy-path.test.yaml", Data: []byte(wellFormedDefinition)}

	def, report := authoring.ParseTestDefinition(src)

	if report.HasErrors() {
		t.Fatalf("expected a well-formed definition to parse without errors, got: %v", report.Diagnostics)
	}
	if def.ID != "happy-path" {
		t.Errorf("ID = %q, want %q", def.ID, "happy-path")
	}
	if def.Layer != domain.LayerOrchestrator {
		t.Errorf("Layer = %q, want %q", def.Layer, domain.LayerOrchestrator)
	}
	if def.Negative {
		t.Error("Negative = true, want false")
	}
	if def.Subject.Identity != "orchestrator" {
		t.Errorf("Subject.Identity = %q, want %q", def.Subject.Identity, "orchestrator")
	}
	if def.Subject.CatalogAgentKey != "orchestrator" {
		t.Errorf("Subject.CatalogAgentKey = %q, want %q", def.Subject.CatalogAgentKey, "orchestrator")
	}
	if def.StubRegistryPath != "stubs/happy-path.stubs.json" {
		t.Errorf("StubRegistryPath = %q, want %q", def.StubRegistryPath, "stubs/happy-path.stubs.json")
	}
}

func TestParseTestDefinition_UnknownField_ReportsDiagnostic(t *testing.T) {
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
not_a_real_field: true
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if !hasDiagnosticCode(report, "unknown-field") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "unknown-field", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected an unknown field to be reported as a diagnostic")
	}
}

func TestParseTestDefinition_MissingRequiredID_ReportsDiagnostic(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: stubs/d.stubs.json
`),
	}

	_, report := authoring.ParseTestDefinition(src)

	if !hasDiagnosticCode(report, "missing-required-field") {
		t.Errorf("expected a diagnostic with code %q, got: %v", "missing-required-field", report.Diagnostics)
	}
	if !report.HasErrors() {
		t.Fatal("expected a missing required 'id' field to be reported as a diagnostic")
	}
}

func TestParseTestDefinition_NegativeFlagParses(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: subagent
negative: true
subject:
  identity: researcher
  agent: researcher
  infrastructure_agents: []
stub_registry: stubs/d.stubs.json
`),
	}

	def, report := authoring.ParseTestDefinition(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if def.Layer != domain.LayerSubagent {
		t.Errorf("Layer = %q, want %q", def.Layer, domain.LayerSubagent)
	}
	if !def.Negative {
		t.Error("Negative = false, want true")
	}
}

func TestParseTestDefinition_RepetitionsAndPassRateParse(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: stubs/d.stubs.json
repetitions: 3
pass_rate: 0.67
`),
	}

	def, report := authoring.ParseTestDefinition(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if def.Settings.Repetitions == nil || *def.Settings.Repetitions != 3 {
		t.Errorf("Settings.Repetitions = %v, want pointer to 3", def.Settings.Repetitions)
	}
	if def.Settings.PassRate == nil || *def.Settings.PassRate != 0.67 {
		t.Errorf("Settings.PassRate = %v, want pointer to 0.67", def.Settings.PassRate)
	}
}

func TestParseTestDefinition_TimeoutAndTurnLimitParse(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: stubs/d.stubs.json
timeout: 15m
turn_limit: 60
`),
	}

	def, report := authoring.ParseTestDefinition(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if def.Settings.Timeout == nil || *def.Settings.Timeout != 15*time.Minute {
		t.Errorf("Settings.Timeout = %v, want pointer to 15m", def.Settings.Timeout)
	}
	if def.Settings.TurnLimit == nil || *def.Settings.TurnLimit != 60 {
		t.Errorf("Settings.TurnLimit = %v, want pointer to 60", def.Settings.TurnLimit)
	}
}

func TestParseTestDefinition_ParallelGroupsParse(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: stubs/d.stubs.json
parallel_groups:
  - name: research-fanout
    members:
      - { tool: Task, agent: researcher }
      - { tool: Task, agent: library-researcher }
`),
	}

	def, report := authoring.ParseTestDefinition(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if len(def.ParallelGroups) != 1 {
		t.Fatalf("len(ParallelGroups) = %d, want 1", len(def.ParallelGroups))
	}
	group := def.ParallelGroups[0]
	if group.Name != "research-fanout" {
		t.Errorf("ParallelGroups[0].Name = %q, want %q", group.Name, "research-fanout")
	}
	want := []domain.CollaboratorIdentity{
		{ToolName: "Task", AgentIdentity: "researcher"},
		{ToolName: "Task", AgentIdentity: "library-researcher"},
	}
	if len(group.Members) != len(want) {
		t.Fatalf("len(Members) = %d, want %d", len(group.Members), len(want))
	}
	for i, m := range want {
		if group.Members[i] != m {
			t.Errorf("Members[%d] = %+v, want %+v", i, group.Members[i], m)
		}
	}
}

func TestParseTestDefinition_SeedFilesParse_ForSingleDecisionTest(t *testing.T) {
	// A single-decision test starts mid-workflow: it seeds a pre-run file
	// into the subject directory instead of letting the subject build it.
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: stubs/d.stubs.json
seed_files:
  - path: Orchestration-{run_id}/Orchestration.md
    ref: fixtures/orchestration-mid-workflow.md
`),
	}

	def, report := authoring.ParseTestDefinition(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}
	if len(def.SeedFiles) != 1 {
		t.Fatalf("len(SeedFiles) = %d, want 1", len(def.SeedFiles))
	}
	seed := def.SeedFiles[0]
	if seed.Path != "Orchestration-{run_id}/Orchestration.md" {
		t.Errorf("SeedFiles[0].Path = %q, want %q", seed.Path, "Orchestration-{run_id}/Orchestration.md")
	}
	if seed.Ref != "fixtures/orchestration-mid-workflow.md" {
		t.Errorf("SeedFiles[0].Ref = %q, want %q", seed.Ref, "fixtures/orchestration-mid-workflow.md")
	}
	if seed.Content != "" {
		t.Errorf("SeedFiles[0].Content = %q, want empty (Ref and Content are mutually exclusive)", seed.Content)
	}
}

// TestParseTestDefinition_EveryAssertionClassParses is the direct coverage
// for AC4.3: every assertion class the evaluator supports must be
// expressible in the test-definition format. Each case declares exactly one
// assertion class and checks it parsed into the corresponding Assertions
// field, non-nil.
func TestParseTestDefinition_EveryAssertionClassParses(t *testing.T) {
	base := `
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: stubs/d.stubs.json
assertions:
%s
`
	cases := []struct {
		class     domain.AssertionClass
		yamlBlock string
		check     func(t *testing.T, a domain.Assertions)
	}{
		{
			class: domain.ClassInvocationSequence,
			yamlBlock: `  invocation_sequence:
    exact: true
    steps:
      - { tool: Task, agent: requirements-refinement }
      - group: research-fanout
        members:
          - { tool: Task, agent: researcher }
          - { tool: Task, agent: library-researcher }
      - { tool: Task, agent: planner }`,
			check: func(t *testing.T, a domain.Assertions) {
				if a.InvocationSequence == nil {
					t.Fatal("InvocationSequence = nil, want populated")
				}
				if !a.InvocationSequence.Exact {
					t.Error("InvocationSequence.Exact = false, want true")
				}
				if len(a.InvocationSequence.Steps) != 3 {
					t.Fatalf("len(Steps) = %d, want 3", len(a.InvocationSequence.Steps))
				}
				group := a.InvocationSequence.Steps[1]
				if group.Group != "research-fanout" || len(group.Members) != 2 {
					t.Errorf("Steps[1] = %+v, want group %q with 2 members", group, "research-fanout")
				}
			},
		},
		{
			class: domain.ClassExecutionLogAgentIDs,
			yamlBlock: `  execution_log:
    agent_ids: ["requirements-refinement#1", "researcher#2", "planner#3"]`,
			check: func(t *testing.T, a domain.Assertions) {
				if a.ExecutionLogAgentIDs == nil {
					t.Fatal("ExecutionLogAgentIDs = nil, want populated")
				}
				if len(*a.ExecutionLogAgentIDs) != 3 {
					t.Fatalf("len(*ExecutionLogAgentIDs) = %d, want 3", len(*a.ExecutionLogAgentIDs))
				}
			},
		},
		{
			class: domain.ClassExecutionLogAllStatus,
			yamlBlock: `  execution_log:
    all_status: SUCCESS`,
			check: func(t *testing.T, a domain.Assertions) {
				if a.ExecutionLogAllStatus == nil || *a.ExecutionLogAllStatus != "SUCCESS" {
					t.Errorf("ExecutionLogAllStatus = %v, want pointer to %q", a.ExecutionLogAllStatus, "SUCCESS")
				}
			},
		},
		{
			class: domain.ClassFinalPhase,
			yamlBlock: `  final_state:
    phase: COMPLETED
    last_status: SUCCESS`,
			check: func(t *testing.T, a domain.Assertions) {
				if a.FinalPhase == nil || *a.FinalPhase != "COMPLETED" {
					t.Errorf("FinalPhase = %v, want pointer to %q", a.FinalPhase, "COMPLETED")
				}
				if a.FinalStatus == nil || *a.FinalStatus != "SUCCESS" {
					t.Errorf("FinalStatus = %v, want pointer to %q", a.FinalStatus, "SUCCESS")
				}
			},
		},
		{
			class:     domain.ClassProtocolViolations,
			yamlBlock: `  protocol_violations: 0`,
			check: func(t *testing.T, a domain.Assertions) {
				if a.ProtocolViolations == nil || *a.ProtocolViolations != 0 {
					t.Errorf("ProtocolViolations = %v, want pointer to 0", a.ProtocolViolations)
				}
			},
		},
		{
			class: domain.ClassArtifactCreated,
			yamlBlock: `  artifact_created:
    - Orchestration-{run_id}/Research.md`,
			check: func(t *testing.T, a domain.Assertions) {
				want := []string{"Orchestration-{run_id}/Research.md"}
				if len(a.ArtifactCreated) != len(want) || a.ArtifactCreated[0] != want[0] {
					t.Errorf("ArtifactCreated = %v, want %v", a.ArtifactCreated, want)
				}
			},
		},
		{
			class: domain.ClassArtifactNotCreated,
			yamlBlock: `  artifact_not_created:
    - Orchestration-{run_id}/Design.md`,
			check: func(t *testing.T, a domain.Assertions) {
				want := []string{"Orchestration-{run_id}/Design.md"}
				if len(a.ArtifactNotCreated) != len(want) || a.ArtifactNotCreated[0] != want[0] {
					t.Errorf("ArtifactNotCreated = %v, want %v", a.ArtifactNotCreated, want)
				}
			},
		},
		{
			class: domain.ClassMinConcurrency,
			yamlBlock: `  min_concurrency:
    research-fanout: 2`,
			check: func(t *testing.T, a domain.Assertions) {
				if a.MinConcurrency["research-fanout"] != 2 {
					t.Errorf("MinConcurrency[%q] = %d, want 2", "research-fanout", a.MinConcurrency["research-fanout"])
				}
			},
		},
		{
			class: domain.ClassTaskMessage,
			yamlBlock: `  task_messages:
    - at: 3
      identity: { tool: Task, agent: planner }
      required_input_artifacts:
        - Orchestration-{run_id}/Research.md
      optional_input_artifacts:
        - Orchestration-{run_id}/LibraryResearch.md
      required_output_artifacts:
        - Orchestration-{run_id}/Plan.md
      human_in_the_loop: true
      task_description_contains: ["plan"]`,
			check: func(t *testing.T, a domain.Assertions) {
				if len(a.TaskMessages) != 1 {
					t.Fatalf("len(TaskMessages) = %d, want 1", len(a.TaskMessages))
				}
				tm := a.TaskMessages[0]
				if tm.At != 3 {
					t.Errorf("TaskMessages[0].At = %d, want 3", tm.At)
				}
				if tm.Identity == nil || *tm.Identity != (domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "planner"}) {
					t.Errorf("TaskMessages[0].Identity = %v, want {Task planner}", tm.Identity)
				}
				if tm.HumanInTheLoop == nil || !*tm.HumanInTheLoop {
					t.Errorf("TaskMessages[0].HumanInTheLoop = %v, want pointer to true", tm.HumanInTheLoop)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.class), func(t *testing.T) {
			src := authoring.Source{
				Path: "d.test.yaml",
				Data: []byte(fmt.Sprintf(base, tc.yamlBlock)),
			}

			def, report := authoring.ParseTestDefinition(src)
			if report.HasErrors() {
				t.Fatalf("unexpected errors: %v", report.Diagnostics)
			}
			tc.check(t, def.Assertions)
		})
	}
}

// TestParseTestDefinition_OmittedAssertion_IsDistinctFromEmptyList verifies
// the documented distinction: omitting an assertion key means the class is
// not evaluated (nil), while declaring an empty list asserts the empty set
// (a non-nil, zero-length slice) — a distinct, trivially-passing statement.
func TestParseTestDefinition_OmittedAssertion_IsDistinctFromEmptyList(t *testing.T) {
	src := authoring.Source{
		Path: "d.test.yaml",
		Data: []byte(`
schema_version: 1
id: d
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  infrastructure_agents: []
stub_registry: stubs/d.stubs.json
assertions:
  artifact_created: []
`),
	}

	def, report := authoring.ParseTestDefinition(src)
	if report.HasErrors() {
		t.Fatalf("unexpected errors: %v", report.Diagnostics)
	}

	if def.Assertions.ArtifactCreated == nil {
		t.Error("ArtifactCreated = nil, want a non-nil empty slice (an explicit empty-set assertion)")
	}
	if len(def.Assertions.ArtifactCreated) != 0 {
		t.Errorf("len(ArtifactCreated) = %d, want 0", len(def.Assertions.ArtifactCreated))
	}
	if def.Assertions.ArtifactNotCreated != nil {
		t.Errorf("ArtifactNotCreated = %v, want nil (never declared, so not evaluated)", def.Assertions.ArtifactNotCreated)
	}
	if def.Assertions.FinalPhase != nil {
		t.Errorf("FinalPhase = %v, want nil (never declared, so not evaluated)", def.Assertions.FinalPhase)
	}
}
