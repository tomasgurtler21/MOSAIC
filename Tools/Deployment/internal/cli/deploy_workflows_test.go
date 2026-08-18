package cli_test

// deploy_workflows_test.go covers the CLI surface for the "deploy" subcommand's
// --workflows flag: nil/empty/populated semantics, and the interaction between
// --workflows and --selections when both are present.
//
// All tests that pass --workflows to the deploy subcommand are RED-phase TDD tests.
// The flag is not yet registered on deployCmd (I1.1 is undelivered), so any invocation
// that supplies --workflows produces ExitUsage (unknown flag) rather than reaching
// DeployNew. These tests will become GREEN once I1.1 and I1.2 are delivered.
//
// Verified behaviours:
//
// Three-way nil/empty/populated distinction (T1.1):
//   - Absent --workflows flag leaves DeployRequest.WorkflowIDs nil ("not specified").
//   - --workflows="" yields a non-nil empty slice ("explicitly none").
//   - --workflows=a,b,c yields the parsed, trimmed list ["a","b","c"].
//   - Whitespace around comma-separated IDs is trimmed.
//   - Absent vs explicitly empty are distinguishable in the request.
//
// Precedence between --workflows and --selections (T1.2):
//   - When --workflows is explicitly set, it wins over the selections file's
//     workflows: key; the file's other keys (tier_models, utility_agents, etc.)
//     are applied regardless.
//   - A selections file whose workflows: key is absent does not overwrite an
//     explicitly set --workflows value with nil.
//   - When --workflows is absent, a selections file's workflows: key is applied
//     unchanged (existing behavior preserved — AC1.5).

import (
	"bytes"
	"context"
	"testing"

	"mosaic-deploy/internal/cli"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// minDeployArgs returns the minimal valid "deploy" args that call DeployNew without
// hanging on interactive prompts: --harness, --workspace, --auto-confirm.
func minDeployArgs(harness, workspace string) []string {
	return []string{"deploy", "--harness", harness, "--workspace", workspace, "--auto-confirm"}
}

// ---------------------------------------------------------------------------
// T1.1: Three-way nil / empty / populated distinction
// ---------------------------------------------------------------------------

// TestDeployCmd_WorkflowsFlagAbsent_WorkflowIDsIsNil verifies that when --workflows is
// not supplied, DeployRequest.WorkflowIDs is nil. A nil slice is the "not specified"
// state that causes DeployNew to ask the user for a workflow selection interactively.
// An implementation that assigns a non-nil empty slice when the flag is absent would
// silently bypass the interactive prompt, deploying the orchestrator with no workflows.
func TestDeployCmd_WorkflowsFlagAbsent_WorkflowIDsIsNil(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act — no --workflows flag and no --selections
	code := cli.Run(context.Background(),
		minDeployArgs("stub-harness", workspace),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); absent --workflows with no --selections must succeed",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.WorkflowIDs != nil {
		t.Errorf("WorkflowIDs = %v, want nil; absent --workflows must produce nil (not specified), "+
			"not an empty slice, so DeployNew can distinguish 'not specified' from 'explicitly empty'",
			svc.deployReq.WorkflowIDs)
	}
}

// TestDeployCmd_WorkflowsFlagExplicitlyEmpty_WorkflowIDsIsNonNilEmpty verifies that
// --workflows="" sets DeployRequest.WorkflowIDs to a non-nil empty slice. A non-nil
// empty slice is the "explicitly none" state that tells DeployNew to skip the workflow
// selection prompt and deploy the orchestrator alone. A bare StringVar (without the
// PreRunE-captured bool) collapses this state into nil, silently breaking the guard.
func TestDeployCmd_WorkflowsFlagExplicitlyEmpty_WorkflowIDsIsNonNilEmpty(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace), "--workflows="),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); --workflows='' must be a valid flag",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.WorkflowIDs == nil {
		t.Error("WorkflowIDs = nil; --workflows='' must produce a non-nil empty slice so " +
			"DeployNew can distinguish 'explicitly none' from 'not specified' — " +
			"a nil value would cause the interactive prompt to fire again")
	}
	if len(svc.deployReq.WorkflowIDs) != 0 {
		t.Errorf("WorkflowIDs = %v, want non-nil empty slice; --workflows='' must yield zero ids",
			svc.deployReq.WorkflowIDs)
	}
}

// TestDeployCmd_WorkflowsFlagPopulated_WorkflowIDsIsParsedList verifies that
// --workflows=a,b,c sets DeployRequest.WorkflowIDs to ["a","b","c"]. This is
// the primary flag use case: the caller names the workflow set on the command line
// instead of through a --selections file.
func TestDeployCmd_WorkflowsFlagPopulated_WorkflowIDsIsParsedList(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace), "--workflows=wf-alpha,wf-beta,wf-gamma"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); --workflows=wf-alpha,wf-beta,wf-gamma must succeed",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"wf-alpha", "wf-beta", "wf-gamma"}
	got := svc.deployReq.WorkflowIDs
	if len(got) != len(want) {
		t.Fatalf("WorkflowIDs = %v, want %v; --workflows must parse the comma-separated list", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("WorkflowIDs[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestDeployCmd_WorkflowsFlagPopulated_TrimsWhitespace verifies that --workflows trims
// whitespace around each comma-separated id. A user supplying "wf-a, wf-b" (with a space
// after the comma) must get ["wf-a","wf-b"], matching the render and workflows subcommand
// convention.
func TestDeployCmd_WorkflowsFlagPopulated_TrimsWhitespace(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace), "--workflows=wf-a, wf-b , wf-c"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"wf-a", "wf-b", "wf-c"}
	got := svc.deployReq.WorkflowIDs
	if len(got) != len(want) {
		t.Fatalf("WorkflowIDs = %v, want %v; whitespace around ids must be trimmed", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("WorkflowIDs[%d] = %q, want %q; whitespace trimming failed", i, got[i], w)
		}
	}
}

// TestDeployCmd_WorkflowsFlagAbsentVsEmpty_DistinguishableInRequest verifies the critical
// nil/non-nil distinction at the request level: an absent flag and an explicitly empty flag
// must reach DeployRequest.WorkflowIDs as different values so DeployNew can honour
// 'not specified' vs 'explicitly none'. A bare StringVar collapses both into nil.
func TestDeployCmd_WorkflowsFlagAbsentVsEmpty_DistinguishableInRequest(t *testing.T) {
	// Arrange — absent
	workspaceAbsent := t.TempDir()
	svcAbsent := &spyService{deployResp: successSummary(workspaceAbsent)}
	cli.Run(context.Background(),
		minDeployArgs("stub-harness", workspaceAbsent),
		svcAbsent, &bytes.Buffer{}, &bytes.Buffer{})

	// Arrange — explicitly empty
	workspaceEmpty := t.TempDir()
	svcEmpty := &spyService{deployResp: successSummary(workspaceEmpty)}
	cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspaceEmpty), "--workflows="),
		svcEmpty, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if svcAbsent.deployReq == nil || svcEmpty.deployReq == nil {
		t.Fatal("DeployNew was not called in one or both cases")
	}
	if svcAbsent.deployReq.WorkflowIDs != nil {
		t.Errorf("absent flag: WorkflowIDs = %v, want nil", svcAbsent.deployReq.WorkflowIDs)
	}
	if svcEmpty.deployReq.WorkflowIDs == nil {
		t.Error("empty flag: WorkflowIDs = nil, want non-nil empty slice; " +
			"the PreRunE-captured bool is the only mechanism that preserves the distinction — " +
			"a bare StringVar always produces nil for an absent flag")
	}
	if !( svcAbsent.deployReq.WorkflowIDs == nil && svcEmpty.deployReq.WorkflowIDs != nil ) {
		t.Errorf("absent/empty distinction not preserved: absent.WorkflowIDs=%v emptyWorkflowIDs=%v",
			svcAbsent.deployReq.WorkflowIDs, svcEmpty.deployReq.WorkflowIDs)
	}
}

// ---------------------------------------------------------------------------
// T1.2: --workflows vs --selections precedence
// ---------------------------------------------------------------------------

// TestDeployCmd_WorkflowsFlagAndSelections_FlagWins verifies that when both --workflows
// and --selections supply workflow ids, the explicit --workflows value wins. This is the
// shape of every real AgentTest Deploy call: the selections file carries tier_models and
// nothing else, while workflow ids travel on --workflows.
func TestDeployCmd_WorkflowsFlagAndSelections_FlagWins(t *testing.T) {
	// Arrange: selections file declares workflows that must be overridden.
	selectionsPath := writeTempYAML(t, `workflows:
  - file-wf-1
  - file-wf-2
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act: --workflows explicitly names different ids.
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--workflows=flag-wf",
			"--selections", selectionsPath,
		),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); --workflows + --selections must succeed",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"flag-wf"}
	got := svc.deployReq.WorkflowIDs
	if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
		t.Errorf("WorkflowIDs = %v, want %v; "+
			"--workflows must win over the selections file's workflows: key when both are supplied. "+
			"The current code assigns sf.Workflows unconditionally in the selections block; "+
			"the flag assignment must land last, or the selections block must skip Workflows when the flag was set",
			got, want)
	}
}

// TestDeployCmd_SelectionsFileNoWorkflows_ExplicitFlagPreserved verifies that a selections
// file whose workflows: key is absent does not overwrite an explicitly set --workflows value
// with nil. This is the invariant that makes the AgentTest Deploy call safe: the file
// carries only tier_models, and its absent workflows: key must not clobber the flag.
func TestDeployCmd_SelectionsFileNoWorkflows_ExplicitFlagPreserved(t *testing.T) {
	// Arrange: selections file has no workflows: key — only tier_models, the real use case.
	selectionsPath := writeTempYAML(t, `tier_models:
  TEST-SUBJECT: claude-opus-4
  TEST-STUB: claude-haiku-4-5
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act: --workflows explicitly names the workflow set.
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--workflows=my-workflow",
			"--selections", selectionsPath,
		),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"my-workflow"}
	got := svc.deployReq.WorkflowIDs
	if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
		t.Errorf("WorkflowIDs = %v, want %v; "+
			"a selections file with no workflows: key must not overwrite the explicitly set --workflows value with nil. "+
			"The current code assigns req.WorkflowIDs = sf.Workflows unconditionally, setting it to nil "+
			"when the file has no workflows: key — defeating the flag",
			got, want)
	}
}

// TestDeployCmd_SelectionsFileWorkflows_WhenFlagAbsent_WorkflowIDsFromFile verifies that
// when --workflows is absent and --selections is provided with a workflows: key, the file's
// workflows list is applied. This is the pre-existing behavior (AC1.5) and must remain
// unchanged after the --workflows flag is added.
func TestDeployCmd_SelectionsFileWorkflows_WhenFlagAbsent_WorkflowIDsFromFile(t *testing.T) {
	// Arrange
	selectionsPath := writeTempYAML(t, `workflows:
  - selections-wf-1
  - selections-wf-2
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act: no --workflows flag; selections file has the workflow list.
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace), "--selections", selectionsPath),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"selections-wf-1", "selections-wf-2"}
	got := svc.deployReq.WorkflowIDs
	if len(got) != len(want) {
		t.Fatalf("WorkflowIDs = %v, want %v; "+
			"absent --workflows must leave the selections file's workflows: key in effect", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("WorkflowIDs[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestDeployCmd_WorkflowsFlagAndSelections_SelectionsOtherKeysAreApplied verifies that
// when --workflows is explicitly set, the selections file's other keys (utility_agents,
// tier_models, hooks) are still applied. Only the file's workflows: key is superseded.
func TestDeployCmd_WorkflowsFlagAndSelections_SelectionsOtherKeysAreApplied(t *testing.T) {
	// Arrange: selections file carries both workflows (to be overridden) and utility_agents
	// (must survive). Using utility_agents because it maps to a visible request field.
	selectionsPath := writeTempYAML(t, `workflows:
  - file-wf
utility_agents:
  - utility-agent-x
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--workflows=flag-wf",
			"--selections", selectionsPath,
		),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert: exit and DeployNew called
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	// WorkflowIDs must come from the flag.
	wantWorkflows := []string{"flag-wf"}
	if len(svc.deployReq.WorkflowIDs) != len(wantWorkflows) || svc.deployReq.WorkflowIDs[0] != wantWorkflows[0] {
		t.Errorf("WorkflowIDs = %v, want %v; --workflows must win", svc.deployReq.WorkflowIDs, wantWorkflows)
	}
	// UtilityAgentIDs must come from the file (not dropped because --workflows is set).
	wantUtility := []string{"utility-agent-x"}
	if len(svc.deployReq.UtilityAgentIDs) != len(wantUtility) || svc.deployReq.UtilityAgentIDs[0] != wantUtility[0] {
		t.Errorf("UtilityAgentIDs = %v, want %v; "+
			"the selections file's utility_agents: key must still be applied when --workflows is set",
			svc.deployReq.UtilityAgentIDs, wantUtility)
	}
}

// TestDeployCmd_WorkflowsFlagExplicitlyEmpty_AndSelections_EmptyWins verifies that
// --workflows="" (explicitly none) wins over a selections file that names workflows.
// A non-nil empty slice must survive the selections block; the file's workflows: key
// must not overwrite an "explicitly empty" flag value with a non-empty list.
func TestDeployCmd_WorkflowsFlagExplicitlyEmpty_AndSelections_EmptyWins(t *testing.T) {
	// Arrange: selections file has workflows; flag is explicitly empty.
	selectionsPath := writeTempYAML(t, `workflows:
  - file-wf-a
  - file-wf-b
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--workflows=",
			"--selections", selectionsPath,
		),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); --workflows='' + --selections must succeed",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.WorkflowIDs == nil {
		t.Error("WorkflowIDs = nil; --workflows='' (explicitly none) must produce a non-nil empty slice, " +
			"not nil; the selections file must not overwrite an explicit empty flag with its workflows list")
	}
	if len(svc.deployReq.WorkflowIDs) != 0 {
		t.Errorf("WorkflowIDs = %v, want non-nil empty slice; "+
			"--workflows='' must win over the selections file's workflows list",
			svc.deployReq.WorkflowIDs)
	}
}
