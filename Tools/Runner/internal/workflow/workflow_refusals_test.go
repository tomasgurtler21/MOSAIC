package workflow_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

func TestParse_NoTable_ReturnsError(t *testing.T) {
	content := []byte("No routing table here.\n")
	info := domain.WorkflowInfo{ID: "no-table", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when no routing table is present")
	}
}

func TestParse_NoTable_ReturnsRefusalError(t *testing.T) {
	content := []byte("No routing table here.\n")
	info := domain.WorkflowInfo{ID: "no-table", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_Phase_ReturnsRefusalError(t *testing.T) {
	content := []byte(`| Subagent | HITL | Input | Output |
|----------|:----:|-------|--------|
| planner | TRUE | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-phase", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when Phase column is missing")
	}
	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_Subagent_ReturnsRefusalError(t *testing.T) {
	content := []byte(`| Phase | HITL | Input | Output |
|-------|:----:|-------|--------|
| PLANNING | TRUE | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-subagent", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when Subagent column is missing")
	}
	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_HITL_ReturnsRefusalError(t *testing.T) {
	content := []byte(`| Phase | Subagent | Input | Output |
|-------|----------|-------|--------|
| PLANNING | planner | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-hitl", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when HITL column is missing")
	}
	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_Input_ReturnsRefusalError(t *testing.T) {
	content := []byte(`| Phase | Subagent | HITL | Output |
|-------|----------|:----:|--------|
| PLANNING | planner | TRUE | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-input", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when Input column is missing")
	}
	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_Output_ReturnsRefusalError(t *testing.T) {
	content := []byte(`| Phase | Subagent | HITL | Input |
|-------|----------|:----:|-------|
| PLANNING | planner | TRUE | - |
`)
	info := domain.WorkflowInfo{ID: "missing-output", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when Output column is missing")
	}
	asRefusalError(t, err)
}

// Missing-column refusal: Component and Resource contract
//
// The five missing-required-column cases must all populate RefusalError with
// Component == "workflow" and a non-empty Resource naming the workflow, so that
// callers and error messages can attribute and locate the problem.

func TestParse_MissingRequiredColumn_Phase_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`| Subagent | HITL | Input | Output |
|----------|:----:|-------|--------|
| planner | TRUE | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-phase", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MissingRequiredColumn_Subagent_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`| Phase | HITL | Input | Output |
|-------|:----:|-------|--------|
| PLANNING | TRUE | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-subagent", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MissingRequiredColumn_HITL_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`| Phase | Subagent | Input | Output |
|-------|----------|-------|--------|
| PLANNING | planner | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-hitl", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MissingRequiredColumn_Input_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`| Phase | Subagent | HITL | Output |
|-------|----------|:----:|--------|
| PLANNING | planner | TRUE | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-input", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MissingRequiredColumn_Output_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`| Phase | Subagent | HITL | Input |
|-------|----------|:----:|-------|
| PLANNING | planner | TRUE | - |
`)
	info := domain.WorkflowInfo{ID: "missing-output", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_EmptyTable_ZeroDataRows_ReturnsRefusalError(t *testing.T) {
	// A table with a valid header and separator but no data rows must be refused.
	info := domain.WorkflowInfo{ID: "empty-table", Version: "1.0"}

	_, err := workflow.Parse([]byte(emptyTableContent), info)

	if err == nil {
		t.Fatal("Parse must return an error for a table with zero data rows")
	}
	asRefusalError(t, err)
}

func TestParse_RefusalError_ComponentIsWorkflow(t *testing.T) {
	// Every refusal from workflow.Parse must name "workflow" as the component.
	content := []byte("No table.\n")
	info := domain.WorkflowInfo{ID: "no-table", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
}

func TestParse_RefusalError_ResourceNamesWorkflow(t *testing.T) {
	// The RefusalError.Resource must identify the workflow so the user can
	// find the problematic file.
	content := []byte("No table.\n")
	info := domain.WorkflowInfo{ID: "my-special-workflow", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}
