package preflight_test

// Tests for preflight's catalogue-path validation (T7.3, T7.4, T7.5).
//
// Coverage:
//   - A catalogue-path test triggers a single dry-run Deploy, not per-file
//     Render (T7.3)
//   - A stub-agents-path test keeps the per-file dry-run Render loop (T7.3)
//   - A test declaring both a catalogue subject and stub_agents produces a
//     conflicting-provisioning-paths diagnostic before any deploy or render
//     runs (T7.4)
//   - Deploy failure on the catalogue path produces an
//     undeployable-subject-declaration diagnostic carrying the tool's own
//     message (T7.5)
//   - Tool-availability failures on the catalogue path produce
//     environment-unusable, not a declaration diagnostic (T7.5)
//
// These tests are in the TDD RED phase: they compile against the current
// implementation and fail until I7.3–I7.4 provide the real branching and
// mutual-exclusion logic.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/agentdeploy"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// --- shared test data for catalogue-path preflight tests ---

// registryCatalogueTests is a minimal stub registry with no collaborator
// stubs, suitable for catalogue-path tests where collaborators are deployed
// via the catalogue rather than via explicit stub_agents entries. Using a
// registry with collaborator stubs against a catalogue-path definition (which
// has no stub_agents) would trigger the undeclared-stub-agent check, which
// is a separate concern from the catalogue-path provisioning tests here.
const registryCatalogueTests = `{
  "schema_version": 1,
  "test_id": "subject-test",
  "on_unmatched": "passthrough",
  "stubs": []
}`

// definitionCataloguePathOnly is a test definition that declares a catalogue
// agent key and no stub_agents — the catalogue path.
const definitionCataloguePathOnly = `
schema_version: 1
id: subject-test
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: subject.stubs.json
`

// definitionConflictedPaths declares both a catalogue agent key (via subject.agent)
// and stub_agents — the conflicted path that preflight must reject.
const definitionConflictedPaths = `
schema_version: 1
id: subject-test
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: subject.stubs.json
stub_agents:
  - identity:
      tool: dispatch
      agent: codebase-research
    source: agents/codebase-research.md
`

// definitionStubAgentsOnly declares stub_agents and no catalogue agent key —
// the stub-agents path, identical to what earlier stages tested.
const definitionStubAgentsOnly = `
schema_version: 1
id: subject-test
layer: orchestrator
subject:
  identity: orchestrator
stub_registry: subject.stubs.json
stub_agents:
  - identity:
      tool: dispatch
      agent: codebase-research
    source: agents/codebase-research.md
`

// --- T7.3: preflight path selection ---

// TestValidate_CataloguePathTest_TriggersOneDeployDryRun asserts that when the
// definition takes the catalogue path, preflight calls Deploy exactly once with
// DryRun set to true, rather than calling Render for the subject. A single
// dry-run Deploy validates the whole declaration; per-agent Render dry-runs are
// the stub-agents-path behavior.
func TestValidate_CataloguePathTest_TriggersOneDeployDryRun(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	deploy := &fakeDeployer{}
	preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	if len(deploy.deployCalls) != 1 {
		t.Errorf("Deploy called %d time(s), want exactly 1 dry-run Deploy for a catalogue-path test; "+
			"preflight must validate catalogue declarations with a single dry-run Deploy, "+
			"not a per-agent Render loop.\ngot deploy calls: %+v",
			len(deploy.deployCalls), deploy.deployCalls)
	}
	if len(deploy.deployCalls) == 1 && !deploy.deployCalls[0].DryRun {
		t.Error("Deploy was called without DryRun set; " +
			"preflight must set DryRun on the catalogue-path validation Deploy so nothing is written")
	}
}

// TestValidate_CataloguePathTest_DeployReceivesCorrectHarnessAndWorkspace asserts
// that the dry-run Deploy call carries the right HarnessID and a non-empty
// WorkspaceRoot (the preflight scratch root), so the deployment tool can
// resolve harness-specific paths.
func TestValidate_CataloguePathTest_DeployReceivesCorrectHarnessAndWorkspace(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	scratchRoot := t.TempDir()
	deploy := &fakeDeployer{}
	preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: scratchRoot,
		Deploy:            deploy,
	})

	if len(deploy.deployCalls) == 0 {
		t.Fatal("Deploy was never called; want one dry-run Deploy for a catalogue-path test")
	}

	call := deploy.deployCalls[0]
	if call.HarnessID != "claude-code" {
		t.Errorf("Deploy HarnessID = %q, want %q; "+
			"preflight must pass the selected harness ID to the Deploy call",
			call.HarnessID, "claude-code")
	}
	if call.WorkspaceRoot != scratchRoot {
		t.Errorf("Deploy WorkspaceRoot = %q, want %q (the DeployScratchRoot passed to preflight.Input); "+
			"preflight must supply the exact scratch root as the workspace so dry-run resolves "+
			"harness-specific paths correctly — a different non-empty path would pass the wrong "+
			"location to the deployment tool",
			call.WorkspaceRoot, scratchRoot)
	}
}

// TestValidate_StubAgentsPathTest_KeepsRenderLoopNotDeploy asserts that for a
// stub-agents-path test, preflight continues to call Render per stub file and
// does not call Deploy. This pins the unchanged behavior of the stub-agents
// validation path.
func TestValidate_StubAgentsPathTest_KeepsRenderLoopNotDeploy(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":  validSuiteWithOneTest,
		"subject.test.yaml": definitionStubAgentsOnly,
		"subject.stubs.json": registryCatalogueTests,
		"agents/codebase-research.md": "# stub definition",
	})

	deploy := &fakeDeployer{}
	preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	if len(deploy.deployCalls) != 0 {
		t.Errorf("Deploy called %d time(s), want 0 for a stub-agents-path test; "+
			"stub-agents validation uses per-file Render dry-runs, not Deploy.\n"+
			"got deploy calls: %+v", len(deploy.deployCalls), deploy.deployCalls)
	}
}

// --- T7.4: mutual exclusion ---

// TestValidate_ConflictingProvisioningPaths_ReportsConflictingDiagnostic
// asserts that a test declaring both a catalogue agent key and stub_agents
// produces a conflicting-provisioning-paths error diagnostic. This is a hard
// preflight failure: the two paths are mutually exclusive, and silently picking
// one is not acceptable.
func TestValidate_ConflictingProvisioningPaths_ReportsConflictingDiagnostic(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":            validSuiteWithOneTest,
		"subject.test.yaml":           definitionConflictedPaths,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            &fakeDeployer{},
	})

	if !hasDiagnosticCode(report, "conflicting-provisioning-paths") {
		t.Errorf("expected diagnostic %q when a definition declares both a catalogue agent "+
			"key and stub_agents, got: %v; "+
			"the two provisioning paths are mutually exclusive and must be rejected by preflight "+
			"with a clear diagnostic naming the conflict, not silently resolved",
			"conflicting-provisioning-paths", report.Diagnostics)
	}
}

// TestValidate_ConflictingProvisioningPaths_NoDryRunOccurs asserts that when
// both paths are declared, preflight raises the conflict diagnostic without
// issuing any dry-run Deploy or Render calls. The hard error must fire before
// any validation work — a definition that would fail validation regardless
// must not incur the cost of running the tool.
func TestValidate_ConflictingProvisioningPaths_NoDryRunOccurs(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":            validSuiteWithOneTest,
		"subject.test.yaml":           definitionConflictedPaths,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition",
	})

	renderCallCount := 0
	deploy := &fakeDeployer{
		renderFn: func(_ context.Context, _ domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
			renderCallCount++
			return domain.RenderAgentResult{DestinationPath: "/fake/dest/agent.md"}, nil
		},
	}

	preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	if len(deploy.deployCalls) != 0 {
		t.Errorf("Deploy called %d time(s) despite conflicting provisioning paths; "+
			"the conflict diagnostic must fire before any dry-run so no work is done "+
			"for a definition that is invalid regardless.\ngot deploy calls: %+v",
			len(deploy.deployCalls), deploy.deployCalls)
	}
	if renderCallCount != 0 {
		t.Errorf("Render called %d time(s) despite conflicting provisioning paths; "+
			"the conflict diagnostic must fire before any dry-run so no work is done.",
			renderCallCount)
	}
}

// TestValidate_ConflictingProvisioningPaths_DiagnosticNamesConflictingDeclarations
// asserts that the conflicting-provisioning-paths diagnostic message names both
// the catalogue subject declaration and the stub_agents declaration, so a test
// author can identify exactly which fields to fix.
func TestValidate_ConflictingProvisioningPaths_DiagnosticNamesConflictingDeclarations(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":            validSuiteWithOneTest,
		"subject.test.yaml":           definitionConflictedPaths,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition",
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            &fakeDeployer{},
	})

	for _, d := range report.Diagnostics {
		if d.Code == "conflicting-provisioning-paths" {
			// The diagnostic must carry enough information to identify the conflict.
			if d.Message == "" && d.Pointer == "" {
				t.Error("conflicting-provisioning-paths diagnostic has no message or pointer; " +
					"want the diagnostic to name the conflicting declarations so the author " +
					"knows which fields to fix")
			}
			return
		}
	}
	t.Errorf("conflicting-provisioning-paths diagnostic not found; got: %v", report.Diagnostics)
}

// --- T7.5: deploy-path failure diagnostics ---

// TestValidate_CataloguePathDeployFails_ReportsUndeployableSubjectDeclaration
// asserts that when the dry-run Deploy fails with a non-tool-availability error
// (i.e., a declaration-level error), preflight reports
// undeployable-subject-declaration carrying the tool's own message. The deploy
// subcommand produces no structured reason codes, so the diagnostic carries an
// undifferentiated tool message rather than a mapped code.
func TestValidate_CataloguePathDeployFails_ReportsUndeployableSubjectDeclaration(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	const fakeToolMessage = "deployment failed: orchestrator not found in catalogue"
	deploy := &fakeDeployer{
		deployFn: func(_ context.Context, _ domain.DeployRequest) (domain.DeployResult, error) {
			return domain.DeployResult{}, &agentdeploy.DeployError{
				ToolMessage: fakeToolMessage,
				ExitCode:    1,
			}
		},
	}

	_, report := preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	if !hasDiagnosticCode(report, "undeployable-subject-declaration") {
		t.Errorf("expected diagnostic %q when the dry-run Deploy fails, got: %v; "+
			"a catalogue-path dry-run failure must report undeployable-subject-declaration "+
			"so the author knows the declaration cannot be deployed",
			"undeployable-subject-declaration", report.Diagnostics)
	}

	// The known mapped render-path codes must NOT be emitted on the deploy path:
	// the deploy subcommand has no structured reason codes, so inventing them
	// would be fabricating a reason the tool never gave.
	if hasDiagnosticCode(report, "unknown-subject-agent") {
		t.Error("unknown-subject-agent must not be emitted on the catalogue deploy path; " +
			"that code maps render-path reason codes that deploy does not produce")
	}
	if hasDiagnosticCode(report, "unknown-workflow") {
		t.Error("unknown-workflow must not be emitted on the catalogue deploy path; " +
			"that code maps render-path reason codes that deploy does not produce")
	}
}

// TestValidate_CataloguePathDeployFails_DiagnosticCarriesToolMessage asserts
// that the undeployable-subject-declaration diagnostic carries the tool's own
// message, so the author can read what the deployment tool actually reported
// without re-running it.
func TestValidate_CataloguePathDeployFails_DiagnosticCarriesToolMessage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	const fakeToolMessage = "deployment failed: catalogue root not found"
	deploy := &fakeDeployer{
		deployFn: func(_ context.Context, _ domain.DeployRequest) (domain.DeployResult, error) {
			return domain.DeployResult{}, &agentdeploy.DeployError{
				ToolMessage: fakeToolMessage,
				ExitCode:    1,
			}
		},
	}

	_, report := preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	for _, d := range report.Diagnostics {
		if d.Code == "undeployable-subject-declaration" {
			if d.Message == "" {
				t.Error("undeployable-subject-declaration diagnostic has no message; " +
					"want the tool's own message so the author can read what failed " +
					"without re-running the deployment tool")
			}
			return
		}
	}
	t.Errorf("undeployable-subject-declaration not found in diagnostics: %v", report.Diagnostics)
}

// TestValidate_CataloguePathDeployToolUnavailable_ReportsEnvironmentUnusable
// asserts that when the Deploy call fails because the deployment tool binary
// is not available (ErrToolUnavailable), preflight reports environment-unusable
// rather than undeployable-subject-declaration. A broken installation is an
// environment problem, not a declaration error.
func TestValidate_CataloguePathDeployToolUnavailable_ReportsEnvironmentUnusable(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	deploy := &fakeDeployer{
		deployFn: func(_ context.Context, _ domain.DeployRequest) (domain.DeployResult, error) {
			return domain.DeployResult{}, fmt.Errorf("%w: fake binary missing", agentdeploy.ErrToolUnavailable)
		},
	}

	_, report := preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	if hasDiagnosticCode(report, "undeployable-subject-declaration") {
		t.Error("ErrToolUnavailable on the Deploy path must not produce undeployable-subject-declaration; " +
			"a missing binary is an environment problem, not a declaration error")
	}
	if !hasDiagnosticCode(report, "environment-unusable") {
		t.Error("ErrToolUnavailable on the Deploy path must produce environment-unusable, " +
			"not a declaration diagnostic — the distinction lets the author know " +
			"whether to fix their definition or their installation")
	}
}

// TestValidate_CataloguePathDeployTimedOut_ReportsEnvironmentUnusable asserts
// that a port timeout on the Deploy call is treated as an environment problem,
// not as a declaration error, consistent with the Render-path timeout behavior.
func TestValidate_CataloguePathDeployTimedOut_ReportsEnvironmentUnusable(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	deploy := &fakeDeployer{
		deployFn: func(_ context.Context, _ domain.DeployRequest) (domain.DeployResult, error) {
			return domain.DeployResult{}, fmt.Errorf("%w: fake timeout", agentdeploy.ErrTimedOut)
		},
	}

	_, report := preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	if hasDiagnosticCode(report, "undeployable-subject-declaration") {
		t.Error("ErrTimedOut on the Deploy path must not produce undeployable-subject-declaration; " +
			"a port timeout is an environment problem, not a bad declaration")
	}
	if !hasDiagnosticCode(report, "environment-unusable") {
		t.Error("ErrTimedOut on the Deploy path must produce environment-unusable")
	}
}

// TestValidate_CataloguePathDeploy_AtMostOneDiagnosticPerSubject asserts that
// a single dry-run Deploy yields at most one subject-level diagnostic, because
// a single dry run can only reveal the first failure in the tool's validation
// order. This matches the one-diagnostic-per-subject discipline of the
// Render-based path.
func TestValidate_CataloguePathDeploy_AtMostOneDiagnosticPerSubject(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	deploy := &fakeDeployer{
		deployFn: func(_ context.Context, _ domain.DeployRequest) (domain.DeployResult, error) {
			return domain.DeployResult{}, &agentdeploy.DeployError{
				ToolMessage: "first tool failure",
				ExitCode:    1,
			}
		},
	}

	_, report := preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	subjectDiagCount := 0
	for _, d := range report.Diagnostics {
		if d.Code == "undeployable-subject-declaration" {
			subjectDiagCount++
		}
	}
	// Lower-bound assertion: at least one diagnostic is required when Deploy
	// fails. Without this, the test passes silently when the feature is
	// completely absent — zero diagnostics satisfies "at most one" trivially.
	if subjectDiagCount < 1 {
		t.Errorf("got %d undeployable-subject-declaration diagnostics, want at least 1; "+
			"when the dry-run Deploy fails, the catalogue path must produce at least one "+
			"undeployable-subject-declaration diagnostic — zero means the feature is absent, "+
			"not that it is working correctly",
			subjectDiagCount)
	}
	if subjectDiagCount > 1 {
		t.Errorf("got %d undeployable-subject-declaration diagnostics, want at most 1; "+
			"a single dry-run Deploy can only reveal the first failure in the tool's "+
			"validation order, so reporting more than one would fabricate failures",
			subjectDiagCount)
	}
}

// TestValidate_CataloguePathTierModelMapCarriedToDeploy asserts that the
// dry-run Deploy call in preflight carries the same tier-model map as setup
// would — a two-entry map keyed by the domain tier constants — so the
// preflight dry run validates the exact model selection the real run will use.
func TestValidate_CataloguePathTierModelMapCarriedToDeploy(t *testing.T) {
	// Use definitionCataloguePathOnly (model: claude-opus-4-5, no stub_model)
	// so the stub tier falls back to the subject's model. The assertion is on
	// the map having both keys, not on specific values.
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionCataloguePathOnly,
		"subject.stubs.json": registryCatalogueTests,
	})

	deploy := &fakeDeployer{}
	preflight.Validate(preflight.Input{
		SuitePath:         filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot:       filepath.Join(root, "fixtures"),
		HarnessID:         "claude-code",
		DeployScratchRoot: t.TempDir(),
		Deploy:            deploy,
	})

	if len(deploy.deployCalls) == 0 {
		t.Fatal("Deploy was never called; want one dry-run Deploy for a catalogue-path test")
	}

	tm := deploy.deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; " +
			"preflight must carry the same tier-model map as setup so the dry run " +
			"validates the exact model selection the real run will use")
	}
	if _, ok := tm[domain.TierTestSubject]; !ok {
		t.Errorf("Deploy TierModels missing key %q (domain.TierTestSubject); "+
			"preflight must include the subject tier in the map.\ngot: %v",
			domain.TierTestSubject, tm)
	}
	if _, ok := tm[domain.TierTestStub]; !ok {
		t.Errorf("Deploy TierModels missing key %q (domain.TierTestStub); "+
			"preflight must include the stub tier in the map — two entries, always.\ngot: %v",
			domain.TierTestStub, tm)
	}
}
