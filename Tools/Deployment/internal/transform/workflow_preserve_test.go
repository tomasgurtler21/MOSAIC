package transform_test

// workflow_preserve_test.go covers the opt-in Request.PreserveDeployedWorkflows: lifting a
// deployed orchestrator's AvailableWorkflows region byte-for-byte, the clear fallback, the
// flag being ignored when workflows are passed, and unchanged output when the flag is false.

import (
	"bytes"
	"strings"
	"testing"

	"mosaic-deploy/internal/transform"
)

const workflowRegionName = "AvailableWorkflows"

// deployedWorkflowsInner holds two deployed workflow sections. The first carries a version
// no catalog would write, and unusual spacing that a rebuild would not reproduce.
const deployedWorkflowsInner = `<Workflow type="managed" name="quick-fix" version="0.0.1-local">
## Quick Fix Workflow (locally edited)

**Use when:**   tiny changes only.

| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner | TRUE |

</Workflow>
<Workflow type="managed" name="retired-flow" version="9.9">
## A workflow that is no longer in any catalog

</Workflow>
`

func TestApply_PreserveDeployedWorkflows_LiftsRegionByteForByte(t *testing.T) {
	deployed := deployedOrchestrator("", deployedWorkflowsInner)

	result := applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
		r.Deployed = deployed
		r.PreserveDeployedWorkflows = true
	})

	got := regionContent(t, result.Output, workflowRegionName)
	if want := regionContent(t, deployed, workflowRegionName); !bytes.Equal(got, want) {
		t.Errorf("region not preserved byte-for-byte:\ngot:  %q\nwant: %q", got, want)
	}
	if !strings.Contains(string(got), `version="0.0.1-local"`) {
		t.Error("the deployed workflow version was not kept")
	}
	if a := regionAction(t, result, workflowRegionName); a != transform.RegionPreservedWorkflows {
		t.Errorf("action = %q, want %q", a, transform.RegionPreservedWorkflows)
	}
}

func TestApply_PreserveDeployedWorkflows_ReportsPreservedWorkflowIDsInDocumentOrder(t *testing.T) {
	result := applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator("", deployedWorkflowsInner)
		r.PreserveDeployedWorkflows = true
	})

	if !sameStrings(result.Report.Workflows, []string{"quick-fix", "retired-flow"}) {
		t.Errorf("Report.Workflows = %v, want [quick-fix retired-flow]", result.Report.Workflows)
	}
}

func TestApply_PreserveDeployedWorkflows_NestedCustomRegion_KeptOnce(t *testing.T) {
	custom := "<WorkflowNotes type=\"custom\">\nTeam notes about workflows.\n</WorkflowNotes>\n"

	result := applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
		r.Deployed = deployedOrchestrator("", deployedWorkflowsInner+custom)
		r.PreserveDeployedWorkflows = true
	})

	got := string(regionContent(t, result.Output, workflowRegionName))
	if n := strings.Count(got, custom); n != 1 {
		t.Errorf("custom region appears %d times, want once byte-identical: %q", n, got)
	}
	if !strings.HasPrefix(got, deployedWorkflowsInner) {
		t.Errorf("workflow sections not preserved ahead of the custom region: %q", got)
	}
}

func TestApply_PreserveDeployedWorkflows_NoDeployedRegion_FallsBackToClear(t *testing.T) {
	noRegion := []byte(deployedFrontmatter + "\n<Identity type=\"core\">\nOld file without the region.\n</Identity>\n")
	cases := map[string][]byte{
		"nil deployed":     nil,
		"region is absent": noRegion,
		"region is empty":  deployedOrchestrator("", ""),
	}
	for name, deployed := range cases {
		t.Run(name, func(t *testing.T) {
			result := applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
				r.Deployed = deployed
				r.PreserveDeployedWorkflows = true
			})

			if got := regionContent(t, result.Output, workflowRegionName); len(strings.TrimSpace(string(got))) != 0 {
				t.Errorf("region not empty: %q", got)
			}
			if a := regionAction(t, result, workflowRegionName); a != transform.RegionEmptied {
				t.Errorf("action = %q, want %q", a, transform.RegionEmptied)
			}
			if len(result.Report.Workflows) != 0 {
				t.Errorf("Report.Workflows = %v, want none", result.Report.Workflows)
			}
		})
	}
}

func TestApply_PreserveDeployedWorkflows_IgnoredWhenWorkflowsPassed(t *testing.T) {
	blocks := []transform.WorkflowBlock{{ID: "greenfield-tdd", Block: []byte(greenfieldTDDBlock)}}
	deployed := deployedOrchestrator("", deployedWorkflowsInner)
	build := func(flag bool) transform.Result {
		return applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
			r.Deployed = deployed
			r.Workflows = blocks
			r.PreserveDeployedWorkflows = flag
		})
	}

	withFlag, withoutFlag := build(true), build(false)

	if !bytes.Equal(withFlag.Output, withoutFlag.Output) {
		t.Error("flag changed the output although workflows were passed")
	}
	if strings.Contains(string(regionContent(t, withFlag.Output, workflowRegionName)), "retired-flow") {
		t.Error("deployed workflows leaked into an assembled region")
	}
	if a := regionAction(t, withFlag, workflowRegionName); a != transform.RegionAssembled {
		t.Errorf("action = %q, want %q", a, transform.RegionAssembled)
	}
	if !sameStrings(withFlag.Report.Workflows, []string{"greenfield-tdd"}) {
		t.Errorf("Report.Workflows = %v, want [greenfield-tdd]", withFlag.Report.Workflows)
	}
}

// ---------------------------------------------------------------------------
// Regression: with the flag false, the request shapes of deploy, render, update and
// update-workflows behave exactly as before.
// ---------------------------------------------------------------------------

func TestApply_PreserveDeployedWorkflowsFalse_RequestShapesKeepExistingBehaviour(t *testing.T) {
	quickFix := []transform.WorkflowBlock{{ID: "quick-fix", Block: []byte(quickFixBlock)}}
	deployed := deployedOrchestrator("", deployedWorkflowsInner)

	shapes := []struct {
		name       string
		deployed   []byte
		workflows  []transform.WorkflowBlock
		wantAction transform.RegionAction
		wantIDs    []string
		wantRegion string // expected region text; "" means empty
	}{
		{"deploy: new file with workflows", nil, quickFix, transform.RegionAssembled, []string{"quick-fix"}, assembledQuickFix()},
		{"render: no deployed file, no workflows", nil, nil, transform.RegionEmptied, nil, ""},
		{"update-workflows: deployed file, workflows rebuilt", deployed, quickFix, transform.RegionAssembled, []string{"quick-fix"}, assembledQuickFix()},
		{"update: deployed file, no workflows passed", deployed, nil, transform.RegionEmptied, nil, ""},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			result := applyOrchestrator(t, orchestratorWithWorkflows, func(r *transform.Request) {
				r.Deployed = s.deployed
				r.Workflows = s.workflows
				r.PreserveDeployedWorkflows = false
			})

			got := string(regionContent(t, result.Output, workflowRegionName))
			if strings.TrimSpace(got) != strings.TrimSpace(s.wantRegion) {
				t.Errorf("region mismatch:\ngot:  %q\nwant: %q", got, s.wantRegion)
			}
			if a := regionAction(t, result, workflowRegionName); a != s.wantAction {
				t.Errorf("action = %q, want %q", a, s.wantAction)
			}
			if !sameStrings(result.Report.Workflows, s.wantIDs) {
				t.Errorf("Report.Workflows = %v, want %v", result.Report.Workflows, s.wantIDs)
			}
		})
	}
}

// assembledQuickFix is the quick-fix block as written into a deployed region: retyped to managed.
func assembledQuickFix() string {
	block := strings.Replace(quickFixBlock, `<Workflow type="core"`, `<Workflow type="managed"`, 1)
	block = strings.Replace(block, `| Phase | Subagent | HITL |
|-------|----------|:----:|
| PLANNING | planner | TRUE |`,
		`| Row | Phase | Subagent | HITL |
|-----|-------|----------|:----:|
| 1 | PLANNING | planner | TRUE |`, 1)
	return block
}
