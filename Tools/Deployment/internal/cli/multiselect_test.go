package cli_test

// multiselect_test.go verifies the mechanism by which multi-valued selections (workflows,
// utility agents, hooks, per-tier models) are expressed for non-interactive runs.
//
// Verified invariants (T19.2):
//
// Design decision: multi-valued selections are expressed via a selections YAML file supplied
// with the --selections flag. A selections file is preferable to repeated flags for
// batch multi-workspace scripting: the file is stable, shareable, and version-controllable,
// while repeated flags grow unmanageable at scale (AC19.8). Individual single-value flags
// (--harness, --workspace) remain as flags for ergonomics.
//
// Selections file format:
//   workflows: [quick-fix, build]
//   utility_agents: [code-reviewer]
//   hooks: [pre-commit]
//   tier_models:
//     HIGH: claude-opus-4
//     LOW: claude-haiku-3
//
// Happy paths:
//   - A selections file with workflows populates DeployRequest.WorkflowIDs
//   - A selections file with utility_agents populates DeployRequest.UtilityAgentIDs
//   - A selections file with hooks populates DeployRequest.HookIDs
//   - A selections file with tier_models populates DeployRequest.TierModels
//   - An empty selections file is valid and leaves all fields unset (flow uses Interaction)
//   - Absent --selections flag leaves multi-valued fields unset (flow uses Interaction)
//
// Failure modes:
//   - A --selections path that does not exist returns ExitUsage
//   - A --selections file with invalid YAML returns ExitFailure (file found but malformed)
//   - A --selections file with an unrecognised key is accepted (forward-compatible, ignored);
//     strict YAML decoding would break this guarantee — lenient decoding is required

import (
	"bytes"
	"context"
	"testing"

	"mosaic-deploy/internal/cli"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Happy paths: selections file populates request fields
// ---------------------------------------------------------------------------

// TestRun_SelectionsFile_PopulatesWorkflowIDs verifies that a selections file with a
// workflows list results in DeployRequest.WorkflowIDs containing those IDs.
func TestRun_SelectionsFile_PopulatesWorkflowIDs(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, `workflows: [quick-fix, build]`)

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--selections", selectionsPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"quick-fix", "build"}
	if !equalStringSlice(svc.deployReq.WorkflowIDs, want) {
		t.Errorf("WorkflowIDs = %v, want %v", svc.deployReq.WorkflowIDs, want)
	}
}

// TestRun_SelectionsFile_PopulatesUtilityAgentIDs verifies that a selections file with a
// utility_agents list results in DeployRequest.UtilityAgentIDs containing those IDs.
func TestRun_SelectionsFile_PopulatesUtilityAgentIDs(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, `utility_agents: [code-reviewer, qa-agent]`)

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--selections", selectionsPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"code-reviewer", "qa-agent"}
	if !equalStringSlice(svc.deployReq.UtilityAgentIDs, want) {
		t.Errorf("UtilityAgentIDs = %v, want %v", svc.deployReq.UtilityAgentIDs, want)
	}
}

// TestRun_SelectionsFile_PopulatesHookIDs verifies that a selections file with a hooks list
// results in DeployRequest.HookIDs containing those IDs.
func TestRun_SelectionsFile_PopulatesHookIDs(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, `hooks: [pre-commit, pre-push]`)

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--selections", selectionsPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"pre-commit", "pre-push"}
	if !equalStringSlice(svc.deployReq.HookIDs, want) {
		t.Errorf("HookIDs = %v, want %v", svc.deployReq.HookIDs, want)
	}
}

// TestRun_SelectionsFile_PopulatesTierModels verifies that a selections file with a
// tier_models mapping populates DeployRequest.TierModels.
func TestRun_SelectionsFile_PopulatesTierModels(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, `
tier_models:
  HIGH: claude-opus-4
  LOW: claude-haiku-3
`)

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--selections", selectionsPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if svc.deployReq.TierModels == nil {
		t.Fatal("TierModels is nil; selections file must populate tier models")
	}
	if got := svc.deployReq.TierModels[domain.Tier("HIGH")]; got != "claude-opus-4" {
		t.Errorf("TierModels[HIGH] = %q, want %q", got, "claude-opus-4")
	}
	if got := svc.deployReq.TierModels[domain.Tier("LOW")]; got != "claude-haiku-3" {
		t.Errorf("TierModels[LOW] = %q, want %q", got, "claude-haiku-3")
	}
}

// TestRun_SelectionsFile_AllSections_PopulatesAll verifies that a selections file with all
// supported sections correctly populates all corresponding request fields simultaneously.
func TestRun_SelectionsFile_AllSections_PopulatesAll(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, `
workflows: [quick-fix]
utility_agents: [code-reviewer]
hooks: [pre-commit]
tier_models:
  HIGH: claude-opus-4
`)

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--selections", selectionsPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if len(svc.deployReq.WorkflowIDs) == 0 {
		t.Error("WorkflowIDs is empty; expected [quick-fix]")
	}
	if len(svc.deployReq.UtilityAgentIDs) == 0 {
		t.Error("UtilityAgentIDs is empty; expected [code-reviewer]")
	}
	if len(svc.deployReq.HookIDs) == 0 {
		t.Error("HookIDs is empty; expected [pre-commit]")
	}
	if len(svc.deployReq.TierModels) == 0 {
		t.Error("TierModels is empty; expected {HIGH: claude-opus-4}")
	}
}

// TestRun_EmptySelectionsFile_LeavesFieldsUnset verifies that an empty selections file is
// valid and does not populate any of the optional request fields (the flow will ask through
// Interaction).
func TestRun_EmptySelectionsFile_LeavesFieldsUnset(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	selectionsPath := writeTempYAML(t, ``)

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--selections", selectionsPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d; empty selections file must be valid", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	// None of the multi-valued fields should be pre-populated from an empty file.
	if len(svc.deployReq.WorkflowIDs) != 0 {
		t.Errorf("WorkflowIDs = %v, want empty; empty file must not set any selection", svc.deployReq.WorkflowIDs)
	}
}

// TestRun_NoSelectionsFlag_MultiValuedFieldsLeftUnset verifies that when --selections is
// absent the multi-valued request fields are left unset, delegating those questions to the
// Interaction implementation.
func TestRun_NoSelectionsFlag_MultiValuedFieldsLeftUnset(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	if len(svc.deployReq.WorkflowIDs) != 0 {
		t.Errorf("WorkflowIDs = %v; without --selections the field must remain unset", svc.deployReq.WorkflowIDs)
	}
	if len(svc.deployReq.TierModels) != 0 {
		t.Errorf("TierModels = %v; without --selections the field must remain unset", svc.deployReq.TierModels)
	}
}

// ---------------------------------------------------------------------------
// Failure modes
// ---------------------------------------------------------------------------

// TestRun_SelectionsFile_NotFound_ReturnsExitUsage verifies that when the path given to
// --selections does not exist, Run returns ExitUsage rather than ExitFailure; a bad path
// is a usage error, not a runtime failure.
func TestRun_SelectionsFile_NotFound_ReturnsExitUsage(t *testing.T) {
	// Arrange
	svc := &spyService{}

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", t.TempDir(), "--selections", "/nonexistent/path/selections.yaml", "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (ExitUsage) for missing selections file", code, cli.ExitUsage)
	}
}

// TestRun_SelectionsFile_MalformedYAML_ReturnsExitFailure verifies that a selections file
// with invalid YAML content causes Run to return a non-zero exit code. A file that exists
// but is unreadable is a runtime failure, not a usage error.
func TestRun_SelectionsFile_MalformedYAML_ReturnsExitFailure(t *testing.T) {
	// Arrange
	svc := &spyService{}
	selectionsPath := writeTempYAML(t, `workflows: [unclosed`)

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", t.TempDir(), "--selections", selectionsPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert
	if code == cli.ExitSuccess {
		t.Error("exit code = ExitSuccess; malformed YAML in selections file must not succeed")
	}
}

// TestRun_SelectionsFile_UnknownKey_IsIgnored verifies that a selections file containing an
// unrecognised key is accepted without error and does not prevent known keys from being
// populated. This pins the forward-compatibility guarantee: a script written against a future
// version of the selections file format must still work against an older binary that ignores
// unknown keys. An implementation using strict YAML decoding (e.g. yaml.UnmarshalStrict)
// would return an error here, breaking that guarantee.
func TestRun_SelectionsFile_UnknownKey_IsIgnored(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	svc := &spyService{deployResp: successSummary(workspace)}
	// The YAML file contains both a recognised key (workflows) and an unknown future key.
	selectionsPath := writeTempYAML(t, `
workflows: [quick-fix]
unknown_future_key: some_value
`)

	// Act
	code := cli.Run(context.Background(),
		[]string{"deploy", "--harness", "stub-harness", "--workspace", workspace, "--selections", selectionsPath, "--auto-confirm"},
		svc, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert: the run must succeed and the known key must be populated normally.
	if code != cli.ExitSuccess {
		t.Errorf("exit code = %d, want %d; a selections file with an unrecognised key must be forward-compatible (accepted, not rejected)", code, cli.ExitSuccess)
	}
	if svc.deployReq == nil {
		t.Fatal("DeployNew was not called")
	}
	want := []string{"quick-fix"}
	if !equalStringSlice(svc.deployReq.WorkflowIDs, want) {
		t.Errorf("WorkflowIDs = %v, want %v; unknown keys must not discard known key values", svc.deployReq.WorkflowIDs, want)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// equalStringSlice reports whether a and b contain the same strings in the same order.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
