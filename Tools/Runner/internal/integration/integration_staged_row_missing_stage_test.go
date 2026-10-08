package integration_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/workflow"
)

const stagedTwoGroupsContent = `## Staged Two Groups Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | COMPLETE | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
`

// TestIntegration_ResumeArtifactWithStagedRowRecordedWithoutStage_RefusesNamingRowAndRepair
// parses an artifact whose second log row ran a staged EXECUTION row but
// records Stage "-", and resumes it. The refusal names the row and the repair
// instead of failing inside the stage set.
func TestIntegration_ResumeArtifactWithStagedRowRecordedWithoutStage_RefusesNamingRowAndRepair(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "artifact", "staged-row-missing-stage.md"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	state, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("artifact.Parse: %v", err)
	}
	table, err := workflow.Parse([]byte(stagedTwoGroupsContent), domain.WorkflowInfo{
		ID: "staged-two-groups", Version: "1.0",
	})
	if err != nil {
		t.Fatalf("workflow.Parse: %v", err)
	}
	aw, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Fatalf("compat.Admit: %v", err)
	}
	stages := &domain.StageSet{Entries: []domain.StageEntry{{Number: 1, Approach: "TDD"}}}

	_, err = engine.ResumePoint(aw, stages, state, domain.NewInfraAgentSet(nil))

	var perr *domain.PositionUnresolvedError
	if !errors.As(err, &perr) {
		t.Fatalf("want *PositionUnresolvedError, got %v", err)
	}
	if perr.Cause != domain.CauseStagedRowWithoutStage {
		t.Errorf("position cause: want CauseStagedRowWithoutStage, got %d (%v)", perr.Cause, perr)
	}
	if perr.RecordedRow != domain.WorkflowRow(2) {
		t.Errorf("refusal must carry recorded row 2, got %d", perr.RecordedRow)
	}
	if !strings.Contains(err.Error(), "2") || !strings.Contains(err.Error(), "Test.") {
		t.Errorf("refusal must name row 2 and the Test.N repair, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "stage 0 has no entry") {
		t.Errorf("refusal must not be the opaque stage-set failure, got %q", err.Error())
	}
}
