package compat_test

// Tests for FR-18a admission refusals (Conditions 1-6) and the RefusalError contract.

import (
	"strings"
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
)

// ---- FR-18a refusal: Condition 2 — multiple staged phase blocks ----

func TestAdmit_MultipleStagedPhaseBlocks_ReturnsRefusalError(t *testing.T) {
	// A routing table where EXECUTION.[StageNumber] rows appear in two separate
	// blocks (split by a non-staged row) has more than one staged phase block.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "split-execution", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: false}, Agent: "planner"},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "REVIEW", PhaseParsed: domain.PhaseParsed{Name: "REVIEW", IsStaged: false}, Agent: "reviewer"},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer2", OutputArtifacts: []string{"out2.md"}},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with multiple staged phase blocks")
	}
	asRefusalError(t, err)
}

func TestAdmit_MultipleStagedPhaseBlocks_RefusalMessage_NonEmpty(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "split-execution", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: false}, Agent: "planner"},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "REVIEW", PhaseParsed: domain.PhaseParsed{Name: "REVIEW", IsStaged: false}, Agent: "reviewer"},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer2", OutputArtifacts: []string{"out2.md"}},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the multiple staged phase blocks condition")
	}
	if !strings.Contains(re.Resource, "split-execution") {
		t.Errorf("RefusalError.Resource must name the workflow ID %q, got %q", "split-execution", re.Resource)
	}
}

// ---- FR-18a refusal: Condition 3 — non-EXECUTION staged phase ----

func TestAdmit_NonExecutionStagedPhase_ReturnsRefusalError(t *testing.T) {
	// A row with IsStaged=true and Phase name other than "EXECUTION" is refused.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "staged-planning", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: true},
				Agent:           "planner",
				OutputArtifacts: []string{"Plan.md"},
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err == nil {
		t.Fatal("Admit must refuse a workflow where the staged phase is not EXECUTION")
	}
	asRefusalError(t, err)
}

func TestAdmit_NonExecutionStagedPhase_RefusalMessage_MentionsPhaseName(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "staged-planning", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: true},
				Agent:       "planner",
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the non-EXECUTION staged phase condition")
	}
}

// ---- FR-18a refusal: Condition 4 — dynamic or growing stage set ----

func TestAdmit_DynamicStageSet_ReturnsRefusalError(t *testing.T) {
	// A workflow where an EXECUTION-phase row outputs Stage-*/Plan.md is refused.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "dynamic-stages", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:           "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"},
			},
			{
				Index:           1,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "implementation-tdd",
				OutputArtifacts: []string{"Stage-{StageNumber}/PlanProgress.md", "Stage-*/Plan.md"},
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with a dynamic or growing stage set")
	}
	asRefusalError(t, err)
}

func TestAdmit_DynamicStageSet_RefusalMessage_NonEmpty(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "dynamic-stages", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:           "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"},
			},
			{
				Index:           1,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "implementation-tdd",
				OutputArtifacts: []string{"Stage-{StageNumber}/PlanProgress.md", "Stage-*/Plan.md"},
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the dynamic stage set condition, not be empty")
	}
}

// ---- FR-18a refusal: Condition 6 — agent-with-mode notation ----

func TestAdmit_AgentWithModeNotation_ReturnsRefusalError(t *testing.T) {
	// Agent identifiers of the form "agent-name(mode)" are not supported.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "agent-with-mode", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "test-writer-tdd(fix-mode)",
				OutputArtifacts: []string{"out.md"},
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with agent-with-mode notation")
	}
	asRefusalError(t, err)
}

func TestAdmit_AgentWithModeNotation_RefusalMessage_NamesAgent(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "agent-with-mode", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "EXECUTION.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:       "test-writer-tdd(fix-mode)",
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the agent-with-mode condition")
	}
}

// ---- FR-18a refusal: Condition 5 — parallel dispatch ----

func TestAdmit_ParallelDispatch_OnSuccessMultipleAgents_ReturnsRefusalError(t *testing.T) {
	// Parallel dispatch is indicated when OnSuccess contains a comma-separated value.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "parallel-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:       "planner",
				OnSuccess:   domain.OptionalHint{ColumnPresent: true, Value: "agent-a,agent-b"},
			},
			{
				Index:       1,
				Phase:       "EXECUTION.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:       "implementer",
				OnSuccess:   domain.OptionalHint{ColumnPresent: true, Value: "COMPLETE"},
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with parallel dispatch notation")
	}
	asRefusalError(t, err)
}

func TestAdmit_ParallelDispatch_RefusalMessage_NonEmpty(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "parallel-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:       "planner",
				OnSuccess:   domain.OptionalHint{ColumnPresent: true, Value: "agent-a,agent-b"},
			},
			{
				Index:       1,
				Phase:       "EXECUTION.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:       "implementer",
				OnSuccess:   domain.OptionalHint{ColumnPresent: true, Value: "COMPLETE"},
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the parallel dispatch condition")
	}
	if !strings.Contains(re.Resource, "parallel-workflow") {
		t.Errorf("RefusalError.Resource must name the workflow ID %q, got %q", "parallel-workflow", re.Resource)
	}
}

// ---- FR-18a refusal: Condition 1 — stage source other than plan artifact ----

func TestAdmit_NonPlanStageSource_ReturnsRefusalError(t *testing.T) {
	// A routing table where the pre-EXECUTION rows do not output Stage-*/Plan.md
	// is refused.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "non-plan-stages", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:           "custom-planner",
				OutputArtifacts: []string{"custom-stages.json"},
			},
			{
				Index:           1,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "implementer",
				OutputArtifacts: []string{"out.md"},
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err == nil {
		t.Fatal("Admit must refuse a workflow where stage source is not the plan artifact")
	}
	asRefusalError(t, err)
}

func TestAdmit_NonPlanStageSource_RefusalMessage_NonEmpty(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "non-plan-stages", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:           "custom-planner",
				OutputArtifacts: []string{"custom-stages.json"},
			},
			{
				Index:           1,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "implementer",
				OutputArtifacts: []string{"out.md"},
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the non-plan stage source condition")
	}
	if !strings.Contains(re.Resource, "non-plan-stages") {
		t.Errorf("RefusalError.Resource must name the workflow ID %q, got %q", "non-plan-stages", re.Resource)
	}
}

// ---- RefusalError contract ----

func TestAdmit_RefusalError_ComponentIsCompat(t *testing.T) {
	// Every refusal from compat.Admit must name "compat" as the component.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "staged-planning", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: true},
				Agent:       "planner",
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Component != "compat" {
		t.Errorf("RefusalError.Component: want %q, got %q", "compat", re.Component)
	}
}

func TestAdmit_RefusalError_ResourceNamesWorkflow(t *testing.T) {
	// RefusalError.Resource must identify the workflow so the user can locate
	// the problematic workflow definition.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "my-problem-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: true},
				Agent:       "planner",
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
	if !strings.Contains(re.Resource, "my-problem-workflow") {
		t.Errorf("RefusalError.Resource must contain the workflow ID %q, got %q", "my-problem-workflow", re.Resource)
	}
}

func TestAdmit_RefusalError_ReasonIsNonEmpty(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "mode-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "EXECUTION.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:       "writer(fix-mode)",
			},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must not be empty")
	}
}
