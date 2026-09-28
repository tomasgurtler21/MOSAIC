package testrun_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"mosaic-run/internal/testcatalog"
	"mosaic-run/internal/testrun"
)

// =============================================================================
// Deploy failure
// =============================================================================

// TestRun_DeployFailure_NoRunsExecuted verifies that when the deployer returns
// an error, no run invocations are made.
func TestRun_DeployFailure_NoRunsExecuted(t *testing.T) {
	entries := smokeEntries()
	cat := &MockCatalog{
		smokeSet:    entries,
		workflowIDs: []string{"smoke-single", "smoke-double", "findings-loop"},
	}
	deployErr := errors.New("mosaic-deploy: deploy failed: some error")
	dep := &MockDeployer{err: deployErr}
	inv := &MockRunInvoker{}
	chk := &MockChecker{}
	rep := &MockReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error (want nil err, deploy error in summary): %v", err)
	}
	if len(inv.calls) != 0 {
		t.Errorf("RunInvoker.Invoke called %d times after deploy failure, want 0", len(inv.calls))
	}
	if summary == nil {
		t.Fatal("Run returned nil summary on deploy failure")
	}
}

// TestRun_DeployFailure_SummaryDeployErrorSet verifies that TestSummary.DeployError
// is set to the deployment error.
func TestRun_DeployFailure_SummaryDeployErrorSet(t *testing.T) {
	cat := &MockCatalog{
		smokeSet:    smokeEntries(),
		workflowIDs: []string{"smoke-single"},
	}
	deployErr := errors.New("deploy: some failure")
	dep := &MockDeployer{err: deployErr}
	inv := &MockRunInvoker{}
	chk := &MockChecker{}
	rep := &MockReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, _ := o.Run(context.Background(), cfg)
	if summary.DeployError == nil {
		t.Fatal("TestSummary.DeployError is nil, want deploy error")
	}
	if !errors.Is(summary.DeployError, deployErr) {
		t.Errorf("TestSummary.DeployError = %v, want to wrap %v", summary.DeployError, deployErr)
	}
}

// TestRun_DeployFailure_AllPassIsFalse verifies that AllPass is false when
// deployment fails.
func TestRun_DeployFailure_AllPassIsFalse(t *testing.T) {
	cat := &MockCatalog{
		smokeSet:    smokeEntries(),
		workflowIDs: []string{"smoke-single"},
	}
	dep := &MockDeployer{err: errors.New("deploy failed")}
	inv := &MockRunInvoker{}
	chk := &MockChecker{}
	rep := &MockReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:     testrun.ScopeSmoke,
		Harnesses: []string{"auto"},
	}

	summary, _ := o.Run(context.Background(), cfg)
	if summary.AllPass {
		t.Errorf("AllPass = true, want false when deployment fails")
	}
}

// =============================================================================
// Deploy argument correctness
// =============================================================================

// TestRun_DeployArguments_CorrectValuesPassedToDeployer verifies that the
// orchestrator passes the correct arguments to Deployer.Deploy: harnesses from
// cfg.Harnesses, workflows from CatalogPort.WorkflowIDs() (the critical guard
// that prevents silent empty deployments), and catalogFolder joined from
// cfg.MosaicRoot and "Tools/Runner/TestCatalog".
func TestRun_DeployArguments_CorrectValuesPassedToDeployer(t *testing.T) {
	const mosaicRoot = "/mosaic"
	const workspace = "/workspace"
	wantCatalogFolder := filepath.Join(mosaicRoot, "Tools/Runner/TestCatalog")
	wantHarnesses := []string{"auto", "ghcp-cli"}
	wantWorkflows := []string{"smoke-single", "smoke-double", "findings-loop"}

	cat := &MockCatalog{
		smokeSet:    smokeEntries(),
		workflowIDs: wantWorkflows,
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{
			defaultPassingInvokeResult(0),
			defaultPassingInvokeResult(1),
			defaultPassingInvokeResult(2),
			defaultPassingInvokeResult(3),
			defaultPassingInvokeResult(4),
			defaultPassingInvokeResult(5),
			defaultPassingInvokeResult(6),
			defaultPassingInvokeResult(7),
		},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSmoke,
		Harnesses:  wantHarnesses,
		MosaicRoot: mosaicRoot,
		Workspace:  workspace,
	}

	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if dep.callCount != 1 {
		t.Fatalf("Deploy called %d times, want 1", dep.callCount)
	}

	args := dep.capturedArgs[0]

	// catalogFolder must be MosaicRoot joined with the known sub-path.
	if args.catalogFolder != wantCatalogFolder {
		t.Errorf("Deploy catalogFolder = %q, want %q", args.catalogFolder, wantCatalogFolder)
	}

	// harnesses must match cfg.Harnesses exactly.
	if len(args.harnesses) != len(wantHarnesses) {
		t.Errorf("Deploy harnesses = %v, want %v", args.harnesses, wantHarnesses)
	} else {
		for i, h := range wantHarnesses {
			if args.harnesses[i] != h {
				t.Errorf("Deploy harnesses[%d] = %q, want %q", i, args.harnesses[i], h)
			}
		}
	}

	// workflows must be sourced from CatalogPort.WorkflowIDs() -- this is the
	// critical guard that prevents the deploy tool from silently deploying nothing.
	if len(args.workflows) != len(wantWorkflows) {
		t.Errorf("Deploy workflows = %v, want %v (from CatalogPort.WorkflowIDs())", args.workflows, wantWorkflows)
	} else {
		for i, wf := range wantWorkflows {
			if args.workflows[i] != wf {
				t.Errorf("Deploy workflows[%d] = %q, want %q", i, args.workflows[i], wf)
			}
		}
	}
}

// =============================================================================
// CatalogPort.UnionInfrastructureAgentKeys forwarding to DeployerPort
// =============================================================================

// TestRun_UnionInfrastructureAgentKeys_ForwardedToDeployer verifies that
// Orchestrator.Run reads UnionInfrastructureAgentKeys() from the catalog and
// passes the result as the infrastructureKeys argument to Deployer.Deploy.
//
// This test is in the TDD RED phase. It compiles and runs, but fails because
// Orchestrator.Run currently calls InfrastructureAgentKeys() (the old method),
// not UnionInfrastructureAgentKeys(). The fake returns different values from
// each method so the mismatch is observable. It will pass once I3.4 updates
// the deploy call site.
func TestRun_UnionInfrastructureAgentKeys_ForwardedToDeployer(t *testing.T) {
	// Arrange: catalog's union keys differ from the old InfrastructureAgentKeys
	// value so the test can detect which method Orchestrator.Run calls.
	unionKeys := []string{"mosaictest-checkpoint", "mosaictest-review"}
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:    []string{"auto"},
		InSmokeSet:  true,
	}
	cat := &MockCatalog{
		smokeSet:       []testcatalog.CatalogEntry{entry},
		workflowIDs:    []string{"smoke-single"},
		infraKeys:      nil,       // old method: returns nil (the wrong value)
		unionInfraKeys: unionKeys, // new method: returns the expected value
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSmoke,
		Harnesses:  []string{"auto"},
		MosaicRoot: "/mosaic",
		Workspace:  "/workspace",
	}

	// Act
	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Assert: the deployer must have been called with the union keys.
	if dep.callCount == 0 {
		t.Fatal("Deploy was not called")
	}
	got := dep.capturedArgs[0].infrastructureKeys
	if len(got) != len(unionKeys) {
		t.Fatalf("Deploy infrastructureKeys = %v, want %v; "+
			"Orchestrator.Run must forward catalog.UnionInfrastructureAgentKeys() to "+
			"Deployer.Deploy (not the deprecated InfrastructureAgentKeys())",
			got, unionKeys)
	}
	for i, want := range unionKeys {
		if got[i] != want {
			t.Errorf("Deploy infrastructureKeys[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// TestRun_UnionInfrastructureAgentKeys_NoWorkflowAgents_NonNilEmptyForwardedToDeployer
// verifies that when no workflow declares any infrastructure agents,
// Orchestrator.Run forwards a non-nil empty slice (not nil) to Deployer.Deploy.
// A non-nil empty slice causes buildDeployArgs to emit --infrastructure ""
// (deploying zero agents), whereas nil would omit the flag entirely (deploying
// the default set).
//
// This test is in the TDD RED phase. It will fail because Orchestrator.Run
// currently calls InfrastructureAgentKeys() which returns nil, but the test
// expects non-nil empty. It will pass once I3.4 updates the call site to use
// UnionInfrastructureAgentKeys(), which the fake returns as []string{}.
func TestRun_UnionInfrastructureAgentKeys_NoWorkflowAgents_NonNilEmptyForwardedToDeployer(t *testing.T) {
	// Arrange: no workflows declare infrastructure agents.
	// The fake's UnionInfrastructureAgentKeys() returns []string{} (non-nil empty)
	// when unionInfraKeys is nil (the zero value), matching the real implementation.
	entry := testcatalog.CatalogEntry{
		WorkflowID:  "smoke-single",
		Mode:        "auto",
		FixturePath: "/catalog/Workflows/MosaicTest/Fixtures/smoke-single",
		AllModes:    []string{"auto"},
		InSmokeSet:  true,
	}
	cat := &MockCatalog{
		smokeSet:       []testcatalog.CatalogEntry{entry},
		workflowIDs:    []string{"smoke-single"},
		infraKeys:      nil, // old method returns nil
		unionInfraKeys: nil, // zero value: UnionInfrastructureAgentKeys() returns []string{}
	}
	dep := &MockDeployer{}
	inv := &MockRunInvoker{
		results: []invokeResult{defaultPassingInvokeResult(0)},
	}
	chk := &MockChecker{}
	rep := &MockReporter{}

	o := newOrchestrator(cat, dep, inv, chk, rep)
	cfg := testrun.TestConfig{
		Scope:      testrun.ScopeSmoke,
		Harnesses:  []string{"auto"},
		MosaicRoot: "/mosaic",
		Workspace:  "/workspace",
	}

	// Act
	_, err := o.Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Assert: the deployer must receive a non-nil empty slice, not nil.
	if dep.callCount == 0 {
		t.Fatal("Deploy was not called")
	}
	got := dep.capturedArgs[0].infrastructureKeys
	if got == nil {
		t.Errorf("Deploy infrastructureKeys = nil, want non-nil empty slice; " +
			"Orchestrator.Run must forward catalog.UnionInfrastructureAgentKeys() " +
			"which returns []string{} (not nil) when no workflows declare agents. " +
			"nil would cause buildDeployArgs to omit --infrastructure entirely.")
	}
	if len(got) != 0 {
		t.Errorf("Deploy infrastructureKeys = %v (len %d), want empty slice; "+
			"no workflows declared any infrastructure agents", got, len(got))
	}
}
