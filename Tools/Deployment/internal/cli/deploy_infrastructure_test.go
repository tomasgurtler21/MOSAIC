package cli_test

// deploy_infrastructure_test.go covers the CLI surface for the "deploy" subcommand's
// --infrastructure flag: nil/empty/populated semantics, and the interaction between
// --infrastructure and --selections when both are present.
//
// All tests that pass --infrastructure to the deploy subcommand are RED-phase TDD tests.
// The flag is not yet registered on deployCmd (I1.1 is undelivered), so any invocation
// that supplies --infrastructure produces ExitUsage (unknown flag) rather than reaching
// DeployNew. These tests will become GREEN once I1.1 is delivered.
//
// Verified behaviours:
//
// Three-way nil/empty/populated distinction:
//   - Absent --infrastructure flag leaves DeployRequest.InfrastructureAgentIDs nil.
//   - --infrastructure="" yields a non-nil empty slice ("explicitly none").
//   - --infrastructure=a,b,c yields the parsed, trimmed list ["a","b","c"].
//   - Whitespace around comma-separated IDs is trimmed.
//   - Absent vs explicitly empty are distinguishable in the request.
//
// Precedence between --infrastructure and --selections:
//   - When --infrastructure is explicitly set, it wins over the selections file's
//     infrastructure_agents: key; the file's other keys are applied regardless.
//   - A selections file whose infrastructure_agents: key is absent does not
//     overwrite an explicitly set --infrastructure value with nil.
//   - When --infrastructure is absent, a selections file's infrastructure_agents: key
//     is applied unchanged (existing behavior preserved).
//   - --infrastructure="" wins over a selections file that names infrastructure agents.

import (
	"bytes"
	"context"
	"testing"

	"mosaic-deploy/internal/cli"
)

// ---------------------------------------------------------------------------
// Three-way nil / empty / populated distinction
// ---------------------------------------------------------------------------

// TestDeployCmd_InfrastructureFlagAbsent_InfrastructureAgentIDsIsNil verifies
// that when --infrastructure is not supplied, DeployRequest.InfrastructureAgentIDs
// is nil. A nil slice is the "not specified" state that causes the deploy tool
// to ask the user for an infrastructure agent selection interactively.
func TestDeployCmd_InfrastructureFlagAbsent_InfrastructureAgentIDsIsNil(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act -- no --infrastructure flag and no --selections
	code := cli.Run(context.Background(),
		minDeployArgs("stub-harness", workspace),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); absent --infrastructure with no --selections must succeed",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.InfrastructureAgentIDs != nil {
		t.Errorf("InfrastructureAgentIDs = %v, want nil; absent --infrastructure must produce nil "+
			"(not specified) so the deploy tool can ask interactively",
			svc.deployReq.InfrastructureAgentIDs)
	}
}

// TestDeployCmd_InfrastructureFlagExplicitlyEmpty_InfrastructureAgentIDsIsNonNilEmpty
// verifies that --infrastructure="" sets DeployRequest.InfrastructureAgentIDs to a
// non-nil empty slice. A non-nil empty slice is the "explicitly none" state that
// tells the deploy tool to skip the infrastructure agent selection prompt and
// deploy no infrastructure agents.
func TestDeployCmd_InfrastructureFlagExplicitlyEmpty_InfrastructureAgentIDsIsNonNilEmpty(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace), "--infrastructure="),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); --infrastructure='' must be a valid flag",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.InfrastructureAgentIDs == nil {
		t.Error("InfrastructureAgentIDs = nil; --infrastructure='' must produce a non-nil empty slice " +
			"so the deploy tool can distinguish 'explicitly none' from 'not specified'. " +
			"A nil value would cause the interactive prompt to fire again")
	}
	if len(svc.deployReq.InfrastructureAgentIDs) != 0 {
		t.Errorf("InfrastructureAgentIDs = %v, want non-nil empty slice; --infrastructure='' must yield zero ids",
			svc.deployReq.InfrastructureAgentIDs)
	}
}

// TestDeployCmd_InfrastructureFlagPopulated_InfrastructureAgentIDsIsParsedList verifies
// that --infrastructure=a,b,c sets DeployRequest.InfrastructureAgentIDs to
// ["a","b","c"]. This is the primary flag use case: the caller names the
// infrastructure agent set on the command line.
func TestDeployCmd_InfrastructureFlagPopulated_InfrastructureAgentIDsIsParsedList(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--infrastructure=mosaictest-checkpoint,mosaictest-commit,mosaictest-review"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); --infrastructure with values must succeed",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"mosaictest-checkpoint", "mosaictest-commit", "mosaictest-review"}
	got := svc.deployReq.InfrastructureAgentIDs
	if len(got) != len(want) {
		t.Fatalf("InfrastructureAgentIDs = %v, want %v; --infrastructure must parse the comma-separated list",
			got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("InfrastructureAgentIDs[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestDeployCmd_InfrastructureFlagPopulated_TrimsWhitespace verifies that
// --infrastructure trims whitespace around each comma-separated id, matching
// the --workflows flag convention.
func TestDeployCmd_InfrastructureFlagPopulated_TrimsWhitespace(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--infrastructure=agent-a, agent-b , agent-c"),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"agent-a", "agent-b", "agent-c"}
	got := svc.deployReq.InfrastructureAgentIDs
	if len(got) != len(want) {
		t.Fatalf("InfrastructureAgentIDs = %v, want %v; whitespace must be trimmed", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("InfrastructureAgentIDs[%d] = %q, want %q; whitespace trimming failed", i, got[i], w)
		}
	}
}

// TestDeployCmd_InfrastructureFlagAbsentVsEmpty_DistinguishableInRequest verifies
// that an absent --infrastructure flag and an explicitly empty one reach
// DeployRequest.InfrastructureAgentIDs as different values (nil vs non-nil empty).
func TestDeployCmd_InfrastructureFlagAbsentVsEmpty_DistinguishableInRequest(t *testing.T) {
	// Arrange -- absent
	workspaceAbsent := t.TempDir()
	svcAbsent := &spyService{deployResp: successSummary(workspaceAbsent)}
	cli.Run(context.Background(),
		minDeployArgs("stub-harness", workspaceAbsent),
		svcAbsent, &bytes.Buffer{}, &bytes.Buffer{})

	// Arrange -- explicitly empty
	workspaceEmpty := t.TempDir()
	svcEmpty := &spyService{deployResp: successSummary(workspaceEmpty)}
	cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspaceEmpty), "--infrastructure="),
		svcEmpty, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if svcAbsent.deployReq == nil || svcEmpty.deployReq == nil {
		t.Fatal("DeployNew was not called in one or both cases")
	}
	if svcAbsent.deployReq.InfrastructureAgentIDs != nil {
		t.Errorf("absent flag: InfrastructureAgentIDs = %v, want nil",
			svcAbsent.deployReq.InfrastructureAgentIDs)
	}
	if svcEmpty.deployReq.InfrastructureAgentIDs == nil {
		t.Error("empty flag: InfrastructureAgentIDs = nil, want non-nil empty slice; " +
			"the PreRunE-captured Changed() check is the only mechanism that preserves " +
			"the nil-vs-empty distinction")
	}
}

// ---------------------------------------------------------------------------
// --infrastructure vs --selections precedence
// ---------------------------------------------------------------------------

// TestDeployCmd_InfrastructureFlagAndSelections_FlagWins verifies that when
// both --infrastructure and --selections supply infrastructure agent ids, the
// explicit --infrastructure value wins, matching --workflows precedence.
func TestDeployCmd_InfrastructureFlagAndSelections_FlagWins(t *testing.T) {
	// Arrange: selections file declares infrastructure_agents that must be overridden.
	selectionsPath := writeTempYAML(t, `infrastructure_agents:
  - file-agent-1
  - file-agent-2
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act: --infrastructure explicitly names a different set.
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--infrastructure=flag-agent",
			"--selections", selectionsPath,
		),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); --infrastructure + --selections must succeed",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"flag-agent"}
	got := svc.deployReq.InfrastructureAgentIDs
	if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
		t.Errorf("InfrastructureAgentIDs = %v, want %v; "+
			"--infrastructure must win over the selections file's infrastructure_agents: key "+
			"when both are supplied",
			got, want)
	}
}

// TestDeployCmd_SelectionsFileNoInfrastructure_ExplicitFlagPreserved verifies
// that a selections file whose infrastructure_agents: key is absent does not
// overwrite an explicitly set --infrastructure value with nil.
func TestDeployCmd_SelectionsFileNoInfrastructure_ExplicitFlagPreserved(t *testing.T) {
	// Arrange: selections file has no infrastructure_agents: key.
	selectionsPath := writeTempYAML(t, `tier_models:
  TEST-SUBJECT: claude-opus-4
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act: --infrastructure explicitly names an agent.
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--infrastructure=my-infra-agent",
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
	want := []string{"my-infra-agent"}
	got := svc.deployReq.InfrastructureAgentIDs
	if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
		t.Errorf("InfrastructureAgentIDs = %v, want %v; "+
			"a selections file without infrastructure_agents must not overwrite "+
			"the explicitly set --infrastructure value with nil",
			got, want)
	}
}

// TestDeployCmd_SelectionsFileInfrastructure_WhenFlagAbsent_IDsFromFile verifies
// that when --infrastructure is absent and --selections has infrastructure_agents:,
// the file's list is applied. This preserves the selections-file path (FR-2) and
// must remain unchanged after the --infrastructure flag is added.
func TestDeployCmd_SelectionsFileInfrastructure_WhenFlagAbsent_IDsFromFile(t *testing.T) {
	// Arrange
	selectionsPath := writeTempYAML(t, `infrastructure_agents:
  - file-infra-1
  - file-infra-2
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act: no --infrastructure flag; selections file has the agent list.
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
	want := []string{"file-infra-1", "file-infra-2"}
	got := svc.deployReq.InfrastructureAgentIDs
	if len(got) != len(want) {
		t.Fatalf("InfrastructureAgentIDs = %v, want %v; "+
			"absent --infrastructure must leave the selections file's infrastructure_agents: "+
			"value in effect",
			got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("InfrastructureAgentIDs[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestDeployCmd_InfrastructureFlagExplicitlyEmpty_AndSelections_EmptyWins verifies
// that --infrastructure="" (explicitly none) wins over a selections file that names
// infrastructure agents, producing a non-nil empty slice.
func TestDeployCmd_InfrastructureFlagExplicitlyEmpty_AndSelections_EmptyWins(t *testing.T) {
	// Arrange: selections file has infrastructure agents; flag is explicitly empty.
	selectionsPath := writeTempYAML(t, `infrastructure_agents:
  - file-agent-a
  - file-agent-b
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--infrastructure=",
			"--selections", selectionsPath,
		),
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d (ExitSuccess); --infrastructure='' + --selections must succeed",
			code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.InfrastructureAgentIDs == nil {
		t.Error("InfrastructureAgentIDs = nil; --infrastructure='' (explicitly none) must produce " +
			"a non-nil empty slice so the selections file cannot overwrite the explicit empty flag " +
			"with its infrastructure_agents list")
	}
	if len(svc.deployReq.InfrastructureAgentIDs) != 0 {
		t.Errorf("InfrastructureAgentIDs = %v, want non-nil empty slice; "+
			"--infrastructure='' must win over the selections file's infrastructure_agents list",
			svc.deployReq.InfrastructureAgentIDs)
	}
}

// TestDeployCmd_InfrastructureFlagAndSelections_OtherKeysAreApplied verifies
// that when --infrastructure is explicitly set, the selections file's other keys
// (utility_agents, tier_models, workflows) are still applied. Only the file's
// infrastructure_agents: key is superseded by the flag.
func TestDeployCmd_InfrastructureFlagAndSelections_OtherKeysAreApplied(t *testing.T) {
	// Arrange: selections file carries both infrastructure_agents (to be overridden)
	// and utility_agents (must survive).
	selectionsPath := writeTempYAML(t, `infrastructure_agents:
  - file-infra
utility_agents:
  - utility-agent-x
`)
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		append(minDeployArgs("stub-harness", workspace),
			"--infrastructure=flag-infra",
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
	// InfrastructureAgentIDs must come from the flag.
	wantInfra := []string{"flag-infra"}
	if len(svc.deployReq.InfrastructureAgentIDs) != len(wantInfra) ||
		svc.deployReq.InfrastructureAgentIDs[0] != wantInfra[0] {
		t.Errorf("InfrastructureAgentIDs = %v, want %v; --infrastructure must win",
			svc.deployReq.InfrastructureAgentIDs, wantInfra)
	}
	// UtilityAgentIDs must come from the file (not dropped because --infrastructure is set).
	wantUtility := []string{"utility-agent-x"}
	if len(svc.deployReq.UtilityAgentIDs) != len(wantUtility) ||
		svc.deployReq.UtilityAgentIDs[0] != wantUtility[0] {
		t.Errorf("UtilityAgentIDs = %v, want %v; "+
			"the selections file's utility_agents: key must still be applied "+
			"when --infrastructure is set",
			svc.deployReq.UtilityAgentIDs, wantUtility)
	}
}
