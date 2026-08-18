package domain_test

// runmode_test.go covers the Stage-2 RunMode vocabulary rename and additions.
//
// Tests in this file assert:
//   - Each renamed RunMode constant carries its new string value.
//   - ModeTransformHarness is unchanged by the rename.
//   - The two new mode constants (ModeDeployAgents, ModeDeployHooks) carry their
//     final string values.
//   - QDeployAgents carries its final string value.
//   - PlanAction.ActionUpdate is unaffected: its value remains "update".
//
// All RunMode value assertions are TDD RED tests: the constants are declared as empty-string
// stubs so that tests compile without the implementation. Each test fails until
// implementation I2.1 and I2.2 replace the stubs with the correct values.
//
// PlanAction and ModeTransformHarness assertions are regression guards (already green)
// that protect unrelated constants from being inadvertently renamed.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Renamed RunMode constants — new string values
// ---------------------------------------------------------------------------

// TestRunMode_DeployWorkspace_HasCorrectValue verifies that ModeDeployWorkspace carries
// the string value "deploy-workspace". This mode replaces ModeDeployWorkspace; the new value
// is what every writer (todo.Meta.Mode, logging) must emit after the rename.
func TestRunMode_DeployWorkspace_HasCorrectValue(t *testing.T) {
	const want = "deploy-workspace"
	if got := string(domain.ModeDeployWorkspace); got != want {
		t.Errorf("ModeDeployWorkspace = %q, want %q", got, want)
	}
}

// TestRunMode_UpdateWorkspace_HasCorrectValue verifies that ModeUpdateWorkspace carries
// the string value "update-workspace". This mode replaces ModeUpdateWorkspace; the rename must
// not change ActionUpdate (PlanAction), which is a separate type also valued "update".
func TestRunMode_UpdateWorkspace_HasCorrectValue(t *testing.T) {
	const want = "update-workspace"
	if got := string(domain.ModeUpdateWorkspace); got != want {
		t.Errorf("ModeUpdateWorkspace = %q, want %q", got, want)
	}
}

// TestRunMode_UpdateWorkflows_HasCorrectValue verifies that ModeUpdateWorkflows carries
// the string value "update-workflows". This mode replaces ModeUpdateWorkflows.
func TestRunMode_UpdateWorkflows_HasCorrectValue(t *testing.T) {
	const want = "update-workflows"
	if got := string(domain.ModeUpdateWorkflows); got != want {
		t.Errorf("ModeUpdateWorkflows = %q, want %q", got, want)
	}
}

// TestRunMode_PromoteToGeneric_HasCorrectValue verifies that ModePromoteToGeneric carries
// the string value "promote-to-generic". This mode replaces ModePromoteToGeneric.
func TestRunMode_PromoteToGeneric_HasCorrectValue(t *testing.T) {
	const want = "promote-to-generic"
	if got := string(domain.ModePromoteToGeneric); got != want {
		t.Errorf("ModePromoteToGeneric = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// ModeTransformHarness — invariant guard (unchanged by rename)
// ---------------------------------------------------------------------------

// TestRunMode_TransformHarness_IsUnchanged verifies that ModeTransformHarness retains
// its existing value "transform-harness" after the Stage-2 rename pass. It must not be
// inadvertently changed when the other four constants are renamed.
func TestRunMode_TransformHarness_IsUnchanged(t *testing.T) {
	const want = "transform-harness"
	if got := string(domain.ModeTransformHarness); got != want {
		t.Errorf("ModeTransformHarness = %q, want %q; this constant must not change", got, want)
	}
}

// ---------------------------------------------------------------------------
// New mode constants
// ---------------------------------------------------------------------------

// TestRunMode_DeployAgents_HasCorrectValue verifies that the new ModeDeployAgents constant
// carries the string value "deploy-agents". The constant must be declared and carry the
// correct value; no flow is implemented in this stage.
func TestRunMode_DeployAgents_HasCorrectValue(t *testing.T) {
	const want = "deploy-agents"
	if got := string(domain.ModeDeployAgents); got != want {
		t.Errorf("ModeDeployAgents = %q, want %q", got, want)
	}
}

// TestRunMode_DeployHooks_HasCorrectValue verifies that the new ModeDeployHooks constant
// carries the string value "deploy-hooks". The constant must be declared and carry the
// correct value; no flow is implemented in this stage.
func TestRunMode_DeployHooks_HasCorrectValue(t *testing.T) {
	const want = "deploy-hooks"
	if got := string(domain.ModeDeployHooks); got != want {
		t.Errorf("ModeDeployHooks = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// QDeployAgents — new QuestionID
// ---------------------------------------------------------------------------

// TestQDeployAgents_HasCorrectValue verifies that the new QDeployAgents QuestionID
// carries the string value "deploy-agents". This question is used by ModeDeployAgents
// for the merged agent selection; no service method is implemented in this stage.
func TestQDeployAgents_HasCorrectValue(t *testing.T) {
	const want = "deploy-agents"
	if got := string(domain.QDeployAgents); got != want {
		t.Errorf("QDeployAgents = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// PlanAction non-interference
// ---------------------------------------------------------------------------

// TestPlanAction_ActionUpdate_IsUnaffectedByRunModeRename verifies that
// ActionUpdate (PlanAction = "update") retains its value after the RunMode rename.
// ModeUpdateWorkspace (RunMode) is renamed to ModeUpdateWorkspace; ActionUpdate (PlanAction) is
// a distinct type with the same bare string "update" and must not change.
func TestPlanAction_ActionUpdate_IsUnaffectedByRunModeRename(t *testing.T) {
	const want = "update"
	if got := string(domain.ActionUpdate); got != want {
		t.Errorf("ActionUpdate = %q, want %q; PlanAction must not be affected by the RunMode rename",
			got, want)
	}
}

// TestPlanAction_AllValues_TableDriven is a regression guard for all PlanAction constants.
// It pins every PlanAction value so that a bulk find-and-replace that touches the "update"
// string cannot inadvertently change ActionUpdate.
func TestPlanAction_AllValues_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		got  domain.PlanAction
		want string
	}{
		{"ActionCreate", domain.ActionCreate, "create"},
		{"ActionUpdate", domain.ActionUpdate, "update"},
		{"ActionUnchanged", domain.ActionUnchanged, "unchanged"},
		{"ActionConflict", domain.ActionConflict, "locally-modified"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Stage-2 RunMode table — all renamed and new constants
// ---------------------------------------------------------------------------

// TestRunMode_Stage2_TableDriven is a consolidated table test covering every RunMode
// constant that changes or is added in Stage 2. A future reader can see at a glance
// which constants belong to this stage and what their final values are.
func TestRunMode_Stage2_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		got  domain.RunMode
		want string
	}{
		// Renamed constants.
		{"ModeDeployWorkspace", domain.ModeDeployWorkspace, "deploy-workspace"},
		{"ModeUpdateWorkspace", domain.ModeUpdateWorkspace, "update-workspace"},
		{"ModeUpdateWorkflows", domain.ModeUpdateWorkflows, "update-workflows"},
		{"ModePromoteToGeneric", domain.ModePromoteToGeneric, "promote-to-generic"},
		// Unchanged constants — regression guards.
		{"ModeTransformHarness", domain.ModeTransformHarness, "transform-harness"},
		// New constants.
		{"ModeDeployAgents", domain.ModeDeployAgents, "deploy-agents"},
		{"ModeDeployHooks", domain.ModeDeployHooks, "deploy-hooks"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := string(tc.got); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
