package runner_test

// Tests for setup's catalogue-path provisioning (T7.1, T7.2, T7.6, T7.7).
//
// A catalogue-path test is one where the subject names a catalogue agent key
// and no stub_agents are listed. Setup must issue exactly one Deploy call
// for this path, never any per-agent Render calls.
//
// Coverage:
//   - Catalogue-path setup calls Deploy once and no Render (T7.1)
//   - Stub-agents-path setup calls Render, not Deploy (T7.1)
//   - Deploy's ledger is appended before subsequent steps run (T7.2)
//   - Deploy receives a tier-model map covering both the subject and stub
//     tiers, keyed by the domain tier constants (T7.6)
//   - An absent StubModel falls back to the subject's Model (T7.6)
//   - Subject version is threaded from the Deploy result when the agent
//     declares a version (T7.7)
//   - Subject version is empty when the agent declares no version (T7.7)
//   - Subject definition path is derived from the matching DeployedAgent
//     entry, not reconstructed (T7.7)
//
// All these tests are in the TDD RED phase: they compile against the current
// implementation and fail until I7.1–I7.7 provide the real branching logic.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

// catalogueRequest builds a runner.Request whose subject uses the catalogue
// path: CatalogAgentKey is set and StubAgents is empty. DefinitionPath is
// deliberately left empty because it must come from the Deploy result, not
// from an authored value.
func catalogueRequest(testID string) runner.Request {
	req := newRequest(testID)
	req.Test.Definition.Subject.CatalogAgentKey = "orchestrator"
	req.Test.Definition.Subject.DefinitionPath = ""
	req.Test.Definition.Subject.Model = "claude-opus-4-5"
	// StubAgents deliberately absent — catalogue path
	return req
}

// --- T7.1: path selection ---

// TestSetup_CataloguePathIssuesOneDeployCallAndNoRenderCalls asserts that when
// the test definition takes the catalogue path — catalogue agent key declared
// and no stub_agents — setup calls Deploy exactly once and never calls Render.
// The subject and every referenced stub are provisioned in that single Deploy
// call; per-agent Render is the stub-agents-path behavior and must not run.
func TestSetup_CataloguePathIssuesOneDeployCallAndNoRenderCalls(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequest("catalogue-path-deploy-only")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	renderCalls := h.Deployer.allCalls()

	if len(deployCalls) != 1 {
		t.Errorf("Deploy called %d time(s), want exactly 1; "+
			"a catalogue-path test must provision via a single Deploy call, "+
			"not per-agent Render loops.\nDeploy calls: %+v",
			len(deployCalls), deployCalls)
	}
	if len(renderCalls) != 0 {
		t.Errorf("Render called %d time(s), want 0; "+
			"a catalogue-path test must not issue any per-agent Render calls — "+
			"Deploy already covers the subject and every referenced stub.\nRender calls: %+v",
			len(renderCalls), renderCalls)
	}
}

// TestSetup_CataloguePathDeployCallCarriesSubjectWorkflows asserts that the
// single Deploy call for a catalogue-path test carries the subject's declared
// workflow set, preserving the nil/empty distinction.
func TestSetup_CataloguePathDeployCallCarriesSubjectWorkflows(t *testing.T) {
	t.Run("declared_workflows_passed_through", func(t *testing.T) {
		h := newHarness(t)
		req := catalogueRequest("catalogue-workflows-declared")
		req.Test.Definition.Subject.Workflows = []string{"brownfield-tdd"}

		if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}

		deployCalls := h.Deployer.allDeployCalls()
		if len(deployCalls) == 0 {
			t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
		}
		got := deployCalls[0].Workflows
		if len(got) != 1 || got[0] != "brownfield-tdd" {
			t.Errorf("Deploy Workflows = %v, want [brownfield-tdd]; "+
				"the subject's declared workflow set must be passed through to Deploy verbatim",
				got)
		}
	})

	t.Run("nil_workflow_set_stays_nil", func(t *testing.T) {
		h := newHarness(t)
		req := catalogueRequest("catalogue-workflows-nil")
		req.Test.Definition.Subject.Workflows = nil

		if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}

		deployCalls := h.Deployer.allDeployCalls()
		if len(deployCalls) == 0 {
			t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
		}
		// nil means "not specified" — it must reach Deploy as nil, not empty.
		// A nil WorkflowIDs reaching Deploy means the implementation fills in the
		// actual workflow list; an explicit empty means "deploy with no workflows".
		if deployCalls[0].Workflows != nil {
			t.Errorf("Deploy Workflows = %v, want nil; "+
				"a nil subject workflow set must reach Deploy as nil, not as an empty slice",
				deployCalls[0].Workflows)
		}
	})
}

// TestSetup_StubAgentsPathIssuesRenderCallsAndNoDeployCall asserts that the
// existing stub-agents-path behavior is unchanged: when the definition
// declares stub_agents (and no catalogue agent key), setup calls Render once
// per stub and never calls Deploy.
func TestSetup_StubAgentsPathIssuesRenderCallsAndNoDeployCall(t *testing.T) {
	h := newHarness(t)
	req := newRequest("stub-agents-path-render-only")
	// No CatalogAgentKey — stub-agents path
	req.Test.Definition.Subject.CatalogAgentKey = ""
	req.Test.Definition.Subject.DefinitionPath = "researcher.md"
	req.Test.Definition.StubAgents = []domain.StubAgent{
		{
			Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "codebase-research"},
			SourcePath: "/workspace/agents/codebase-research.md",
		},
	}

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) != 0 {
		t.Errorf("Deploy called %d time(s), want 0; "+
			"a stub-agents-path test must not call Deploy — "+
			"it uses the per-file Render loop, unchanged from before Stage 7.\nDeploy calls: %+v",
			len(deployCalls), deployCalls)
	}
}

// --- T7.2: ledger discipline on the deploy path ---

// TestSetup_CataloguePathLedger_AgentsRecordedBeforeSubsequentSteps asserts
// that the destination paths the Deploy call reports are appended to the
// provisioning ledger before the adapter's Provision is called. A failure
// during Provision must leave the ledger describing exactly what exists so
// teardown removes it.
func TestSetup_CataloguePathLedger_AgentsRecordedBeforeSubsequentSteps(t *testing.T) {
	h := newHarness(t)

	var deployedPath string
	h.Deployer.deployFn = func(req domain.DeployRequest) (domain.DeployResult, error) {
		deployedPath = filepath.Join(req.WorkspaceRoot, ".claude", "agents", "orchestrator.md")
		return domain.DeployResult{
			Agents: []domain.DeployedAgent{
				{Key: "orchestrator", DestinationPath: deployedPath},
			},
		}, nil
	}

	req := catalogueRequest("catalogue-ledger-agents")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if deployedPath == "" {
		t.Fatal("Deploy was never called; test setup error — want Deploy called for the catalogue path")
	}

	deprovisioned := h.Adapter.lastDeprovisionedProv
	if !containsPath(deprovisioned.Files, deployedPath) {
		t.Errorf("deployed agent path %q not found in Provisioning.Files passed to Deprovision; "+
			"setup must record every path the Deploy call reports in the ledger before "+
			"the adapter's Provision step, so a Provision failure still leaves the ledger "+
			"describing exactly what was created.\ngot Files: %v",
			deployedPath, deprovisioned.Files)
	}
}

// TestSetup_CataloguePathLedger_CreatedDirectoriesRecorded asserts that
// directories the Deploy call reports it created are recorded in the
// provisioning ledger alongside the agent files, so Deprovision can remove
// them too.
func TestSetup_CataloguePathLedger_CreatedDirectoriesRecorded(t *testing.T) {
	h := newHarness(t)

	var deployedDir string
	h.Deployer.deployFn = func(req domain.DeployRequest) (domain.DeployResult, error) {
		deployedDir = filepath.Join(req.WorkspaceRoot, ".claude", "agents")
		return domain.DeployResult{
			Agents: []domain.DeployedAgent{
				{
					Key:             "orchestrator",
					DestinationPath: filepath.Join(deployedDir, "orchestrator.md"),
				},
			},
			CreatedDirectories: []string{deployedDir},
		}, nil
	}

	req := catalogueRequest("catalogue-ledger-dirs")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if deployedDir == "" {
		t.Fatal("Deploy was never called; test setup error")
	}

	deprovisioned := h.Adapter.lastDeprovisionedProv
	if !containsPath(deprovisioned.Dirs, deployedDir) {
		t.Errorf("deployed directory %q not found in Provisioning.Dirs passed to Deprovision; "+
			"setup must record directories the Deploy call created so teardown can remove them.\n"+
			"got Dirs: %v", deployedDir, deprovisioned.Dirs)
	}
}

// TestSetup_CataloguePathLedger_RecordedBeforeProvisionFailure asserts that
// even when the adapter's Provision step fails after Deploy succeeds, the
// deployed paths are present in the Provisioning ledger. Teardown runs after
// a Provision failure and must find the ledger accurate.
func TestSetup_CataloguePathLedger_RecordedBeforeProvisionFailure(t *testing.T) {
	h := newHarness(t)

	var deployedPath string
	h.Deployer.deployFn = func(req domain.DeployRequest) (domain.DeployResult, error) {
		deployedPath = filepath.Join(req.WorkspaceRoot, ".claude", "agents", "orchestrator.md")
		return domain.DeployResult{
			Agents: []domain.DeployedAgent{
				{Key: "orchestrator", DestinationPath: deployedPath},
			},
		}, nil
	}

	// Make Provision fail so we can confirm the ledger was captured before it ran.
	h.Adapter.provisionErr = errSetupFailure{"provision failed after deploy succeeded"}

	req := catalogueRequest("catalogue-ledger-before-provision-fail")

	_, err := runner.Run(context.Background(), h.Deps, req, nil)
	if err == nil {
		t.Fatal("Run returned nil error when Provision failed; want non-nil error")
	}

	if deployedPath == "" {
		t.Fatal("Deploy was never called; test setup error — want Deploy before Provision")
	}

	deprovisioned := h.Adapter.lastDeprovisionedProv
	if !containsPath(deprovisioned.Files, deployedPath) {
		t.Errorf("deployed path %q not in Provisioning.Files passed to Deprovision after Provision failure; "+
			"setup must record Deploy results in the ledger BEFORE calling Provision so that "+
			"teardown after a Provision failure still removes everything that was created.\n"+
			"got Files: %v", deployedPath, deprovisioned.Files)
	}
}

// errSetupFailure is a local sentinel for test-injected failures, distinct
// from the errors the runner itself might produce.
type errSetupFailure struct{ msg string }

func (e errSetupFailure) Error() string { return "test-injected: " + e.msg }

// --- T7.6: tier-model map ---

// TestSetup_CataloguePathDeployCarriesTierModelMap asserts that the Deploy
// call carries a TierModels map with an entry for the subject tier
// (domain.TierTestSubject) set to the definition's Subject.Model, and an
// entry for the stub tier (domain.TierTestStub) set to the definition's
// Subject.StubModel. The domain tier constants must be used — no literal
// tier strings.
func TestSetup_CataloguePathDeployCarriesTierModelMap(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequest("catalogue-tier-model-map")
	req.Test.Definition.Subject.Model = "claude-opus-4-5"
	req.Test.Definition.Subject.StubModel = "claude-haiku-3-5"

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a map with entries for subject and stub tiers — " +
			"a nil map means no model pre-answering, which leaves agents with unresolved model placeholders")
	}

	subjectModel, hasSubject := tm[domain.TierTestSubject]
	if !hasSubject {
		t.Errorf("Deploy TierModels missing key %q (domain.TierTestSubject); "+
			"the subject tier must be answered so the catalogue orchestrator gets a usable model.\n"+
			"got TierModels: %v", domain.TierTestSubject, tm)
	} else if subjectModel != "claude-opus-4-5" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"the subject tier entry must equal the definition's Subject.Model",
			domain.TierTestSubject, subjectModel, "claude-opus-4-5")
	}

	stubModel, hasStub := tm[domain.TierTestStub]
	if !hasStub {
		t.Errorf("Deploy TierModels missing key %q (domain.TierTestStub); "+
			"the stub tier must always be answered — two entries, always.\n"+
			"got TierModels: %v", domain.TierTestStub, tm)
	} else if stubModel != "claude-haiku-3-5" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q; "+
			"the stub tier entry must equal the definition's Subject.StubModel when non-empty",
			domain.TierTestStub, stubModel, "claude-haiku-3-5")
	}
}

// TestSetup_CataloguePathDeploy_AbsentStubModelFallsBackToSubjectModel asserts
// that when Subject.StubModel is empty, the stub tier entry falls back to
// Subject.Model rather than being left unanswered. An absent StubModel is a
// deliberate "use the subject's model for stubs" — working but expensive, not
// broken.
func TestSetup_CataloguePathDeploy_AbsentStubModelFallsBackToSubjectModel(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequest("catalogue-stub-model-fallback")
	req.Test.Definition.Subject.Model = "claude-opus-4-5"
	req.Test.Definition.Subject.StubModel = "" // absent — fallback case

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if tm == nil {
		t.Fatal("Deploy TierModels is nil; want a two-entry map even when StubModel is absent")
	}

	stubModel, hasStub := tm[domain.TierTestStub]
	if !hasStub {
		t.Errorf("Deploy TierModels missing key %q when StubModel is absent; "+
			"the stub tier must still be answered — two entries, always.\n"+
			"got TierModels: %v", domain.TierTestStub, tm)
	} else if stubModel != "claude-opus-4-5" {
		t.Errorf("Deploy TierModels[%q] = %q, want %q (Subject.Model fallback); "+
			"an absent StubModel must fall back to Subject.Model so the run degrades "+
			"to expensive-but-working rather than broken-and-free",
			domain.TierTestStub, stubModel, "claude-opus-4-5")
	}
}

// TestSetup_CataloguePathDeploy_TierMapHasExactlyTwoEntries asserts that the
// TierModels map always carries exactly two entries — one per tier — never
// more, never fewer. A partial map is a silent failure: the unmapped tier
// deploys with an unresolved model placeholder on a run that still reports
// success.
func TestSetup_CataloguePathDeploy_TierMapHasExactlyTwoEntries(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequest("catalogue-tier-map-count")
	req.Test.Definition.Subject.Model = "claude-opus-4-5"
	req.Test.Definition.Subject.StubModel = "claude-haiku-3-5"

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) == 0 {
		t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
	}

	tm := deployCalls[0].TierModels
	if len(tm) != 2 {
		t.Errorf("Deploy TierModels has %d entries, want exactly 2 (one per tier); "+
			"the map must be total over the test catalogue's two tiers.\n"+
			"got TierModels: %v", len(tm), tm)
	}
}

// --- T7.7: subject version and definition path on the catalogue path ---

// TestSetup_CataloguePathRecordsSubjectVersionFromDeployReport asserts that
// when the Deploy result's matching agent carries a SourceVersion, the runner
// threads that value through to result.SubjectVersion. The runner must carry
// the reported version as-is; it must never reconstruct or fabricate a value.
//
// This test references DeployedAgent.SourceVersion, which is added in Stage 3
// (I3.1). It will fail to compile (TDD RED) until that field exists and the
// runner threads it through (I3.3).
func TestSetup_CataloguePathRecordsSubjectVersionFromDeployReport(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequest("catalogue-subject-version-reported")

	const wantVersion = "1.2.3"

	// Fake deployer returns a version for the deployed subject agent.
	h.Deployer.deployFn = func(dr domain.DeployRequest) (domain.DeployResult, error) {
		return domain.DeployResult{
			Agents: []domain.DeployedAgent{
				{
					Key:             "orchestrator",
					DestinationPath: filepath.Join(dr.WorkspaceRoot, ".claude", "agents", "orchestrator.md"),
					SourceVersion:   wantVersion,
				},
			},
		}, nil
	}

	result, err := runner.Run(context.Background(), h.Deps, req, nil)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Prerequisite guard: Deploy must have been called exactly once.
	if len(h.Deployer.allDeployCalls()) != 1 {
		t.Fatalf("Deploy called %d time(s), want exactly 1; "+
			"this test cannot validate catalogue-path subject-version threading unless "+
			"the branching implementation calls Deploy exactly once",
			len(h.Deployer.allDeployCalls()))
	}

	if result.SubjectVersion != wantVersion {
		t.Errorf("result.SubjectVersion = %q, want %q; "+
			"when the Deploy result's matching agent carries a SourceVersion, "+
			"the runner must thread that value to result.SubjectVersion — "+
			"it must not discard it or substitute an empty string.",
			result.SubjectVersion, wantVersion)
	}
}

// TestSetup_CataloguePathRecordsSubjectVersionAsEmptyWhenAgentDeclaresNone
// asserts that when the matching deployed agent declares no version (SourceVersion
// is empty), result.SubjectVersion remains empty. The report layer maps an empty
// SubjectVersion to "unknown"; the runner's job is to carry what Deploy reports,
// never to reconstruct or invent a value.
func TestSetup_CataloguePathRecordsSubjectVersionAsEmptyWhenAgentDeclaresNone(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequest("catalogue-subject-version-unversioned")

	// Fake deployer returns a result with no SourceVersion — the agent declares
	// no version, which is legal. The runner must pass this empty value through.
	h.Deployer.deployFn = func(dr domain.DeployRequest) (domain.DeployResult, error) {
		return domain.DeployResult{
			Agents: []domain.DeployedAgent{
				{
					Key:             "orchestrator",
					DestinationPath: filepath.Join(dr.WorkspaceRoot, ".claude", "agents", "orchestrator.md"),
					SourceVersion:   "", // no version declared — legal state
				},
			},
		}, nil
	}

	result, err := runner.Run(context.Background(), h.Deps, req, nil)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Prerequisite guard: Deploy must have been called exactly once for this
	// test to be meaningful. Without the branching implementation, Deploy is
	// never called and the assertion below may pass trivially. Fail immediately
	// so the RED phase is visible.
	if len(h.Deployer.allDeployCalls()) != 1 {
		t.Fatalf("Deploy called %d time(s), want exactly 1; "+
			"this test cannot validate unversioned catalogue-path behavior unless "+
			"the branching implementation calls Deploy exactly once",
			len(h.Deployer.allDeployCalls()))
	}

	if result.SubjectVersion != "" {
		t.Errorf("result.SubjectVersion = %q, want empty string; "+
			"when the deployed agent declares no version, SubjectVersion must remain empty — "+
			"the report layer renders empty as 'unknown'. "+
			"The runner must NOT reconstruct the version by parsing the deployed file.",
			result.SubjectVersion)
	}
}

// TestSetup_CataloguePathDerivesDefinitionPathFromDeployReport asserts that
// the subject's DefinitionPath after setup comes from the DeployedAgent entry
// whose Key matches the declared CatalogAgentKey, made relative to the sandbox
// subject directory. It must never be reconstructed from harness layout
// knowledge.
func TestSetup_CataloguePathDerivesDefinitionPathFromDeployReport(t *testing.T) {
	h := newHarness(t)

	// Configure a deployFn that returns a distinctive agent path, so we can
	// confirm the runner derived DefinitionPath from it rather than guessing.
	h.Deployer.deployFn = func(req domain.DeployRequest) (domain.DeployResult, error) {
		return domain.DeployResult{
			Agents: []domain.DeployedAgent{
				{
					Key: "orchestrator",
					DestinationPath: filepath.Join(
						req.WorkspaceRoot,
						".claude", "agents", "orchestrator.md",
					),
				},
			},
		}, nil
	}

	req := catalogueRequest("catalogue-definition-path")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Prerequisite guard: Deploy must have been called exactly once. Without
	// branching, the runner uses the Render path and the Render stub's default
	// destination satisfies all three assertions below coincidentally. Fail
	// immediately so the RED phase is visible rather than masked.
	if len(h.Deployer.allDeployCalls()) != 1 {
		t.Fatalf("Deploy called %d time(s), want exactly 1; "+
			"this test cannot validate that DefinitionPath is derived from the Deploy "+
			"report unless Deploy is called exactly once — "+
			"the prerequisite (I7.2 catalogue-path branching) is not yet implemented",
			len(h.Deployer.allDeployCalls()))
	}

	subject := h.Adapter.lastProvisionReq.Subject
	if subject.DefinitionPath == "" {
		t.Fatalf("Subject.DefinitionPath is empty after catalogue-path setup; "+
			"want the path derived from the DeployedAgent entry whose Key matches the subject key")
	}
	if filepath.IsAbs(subject.DefinitionPath) {
		t.Errorf("Subject.DefinitionPath = %q is absolute; "+
			"want a path relative to the sandbox subject directory",
			subject.DefinitionPath)
	}
	if !strings.Contains(subject.DefinitionPath, "orchestrator") {
		t.Errorf("Subject.DefinitionPath = %q does not contain the subject key %q; "+
			"the definition path must be derived from the DeployedAgent entry whose Key "+
			"matches the declared catalogue agent key",
			subject.DefinitionPath, "orchestrator")
	}
}

// TestSetup_CataloguePathMatchesAgentByKey asserts that the runner identifies
// the subject among all deployed agents by matching the DeployedAgent.Key
// against the declared CatalogAgentKey — no positional assumption, no "first
// entry" rule. A deploy result whose first entry is a stub must still yield
// the correct definition path for the subject.
func TestSetup_CataloguePathMatchesAgentByKey(t *testing.T) {
	h := newHarness(t)

	// Return the subject agent as the second entry to prove key-based matching.
	h.Deployer.deployFn = func(req domain.DeployRequest) (domain.DeployResult, error) {
		return domain.DeployResult{
			Agents: []domain.DeployedAgent{
				{
					Key:             "researcher", // a stub — not the subject
					DestinationPath: filepath.Join(req.WorkspaceRoot, ".claude", "agents", "researcher.md"),
				},
				{
					Key:             "orchestrator", // the subject — second entry
					DestinationPath: filepath.Join(req.WorkspaceRoot, ".claude", "agents", "orchestrator.md"),
				},
			},
		}, nil
	}

	req := catalogueRequest("catalogue-key-based-match")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Prerequisite guard: Deploy must have been called exactly once. Without
	// branching, Deploy is never called and the Render stub returns
	// "orchestrator.md" which satisfies the "not containing researcher"
	// assertion trivially. Fail immediately to surface the RED phase.
	if len(h.Deployer.allDeployCalls()) != 1 {
		t.Fatalf("Deploy called %d time(s), want exactly 1; "+
			"this test cannot validate key-based subject matching unless Deploy is "+
			"called exactly once — "+
			"the prerequisite (I7.2 catalogue-path branching) is not yet implemented",
			len(h.Deployer.allDeployCalls()))
	}

	subject := h.Adapter.lastProvisionReq.Subject
	if subject.DefinitionPath == "" {
		t.Fatalf("Subject.DefinitionPath is empty; "+
			"want the path for the agent keyed %q, even when it is not the first Deploy result", "orchestrator")
	}
	if strings.Contains(subject.DefinitionPath, "researcher") {
		t.Errorf("Subject.DefinitionPath = %q refers to the researcher stub, not the subject; "+
			"want key-based matching on %q, not positional (first entry) matching",
			subject.DefinitionPath, "orchestrator")
	}
}

// --- Error and edge-case tests for the catalogue path ---

// TestSetup_CataloguePathDeployResult_SubjectKeyNotFound_ReturnsError asserts
// that when the Deploy result's Agents list contains no entry whose Key matches
// the declared CatalogAgentKey, setup returns an error rather than proceeding
// with an empty or incorrect definition path. A deploy that succeeds but
// deploys a differently-keyed agent is a silent misconfiguration; the runner
// must surface it so the test author knows to fix the catalogue declaration or
// the declared CatalogAgentKey, rather than discovering the problem mid-run.
func TestSetup_CataloguePathDeployResult_SubjectKeyNotFound_ReturnsError(t *testing.T) {
	h := newHarness(t)

	// Deploy succeeds, but the returned agent list contains no entry whose Key
	// is "orchestrator" (the declared CatalogAgentKey). This simulates a deploy
	// that worked but deployed a differently-named agent.
	h.Deployer.deployFn = func(req domain.DeployRequest) (domain.DeployResult, error) {
		return domain.DeployResult{
			Agents: []domain.DeployedAgent{
				{
					Key:             "researcher", // wrong key — not the subject
					DestinationPath: filepath.Join(req.WorkspaceRoot, ".claude", "agents", "researcher.md"),
				},
			},
		}, nil
	}

	req := catalogueRequest("catalogue-key-not-found")

	_, err := runner.Run(context.Background(), h.Deps, req, nil)
	if err == nil {
		t.Error("Run returned nil error when the Deploy result contains no entry matching the " +
			"declared CatalogAgentKey; want a non-nil error surfacing the misconfiguration — " +
			"proceeding with an empty or wrong definition path would silently corrupt the run " +
			"and the test author would have no indication of what went wrong")
	}
}

// TestSetup_CataloguePath_DeployRequestCarriesSandboxDeployLogDir asserts that
// the runner sets DeployRequest.LogDir to the sandbox's per-run deploy log
// directory before calling the deployer. Without this wiring, every concurrent
// run's deploy invocation would write to the deployment tool's shared fixed
// location under the repository root, creating a write-contention window between
// concurrent runs — the precise risk this stage was introduced to close.
//
// The expected value is derived from the same sandbox object that setup creates
// in step 1 and passes to the deployer in step 2: sandbox.DeployLogDir() is
// pure path arithmetic over ControlDir, so the test needs no knowledge of the
// workspace layout.
func TestSetup_CataloguePath_DeployRequestCarriesSandboxDeployLogDir(t *testing.T) {
	h := newHarness(t)
	req := catalogueRequest("catalogue-deploy-log-dir")

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Prerequisite guard: Deploy must have been called exactly once. Without
	// the catalogue-path branching, Deploy is never called (Render is used
	// instead), so the assertion below would fire on a missing call rather
	// than a missing LogDir. Fail immediately to surface the RED phase.
	deployCalls := h.Deployer.allDeployCalls()
	if len(deployCalls) != 1 {
		t.Fatalf("Deploy called %d time(s), want exactly 1; "+
			"this test requires the catalogue-path branching to call Deploy exactly once — "+
			"without it this assertion cannot distinguish 'LogDir not set' from 'Deploy not called'",
			len(deployCalls))
	}

	// The sandbox passed to Provision (step 6 of setup) is the same sandbox
	// created in step 1 and used for the Deploy call in step 2. Both operate
	// on the same domain.Sandbox value, so its DeployLogDir() is the expected
	// LogDir the runner must have provided to the deployer.
	sb := h.Adapter.lastProvisionReq.Sandbox
	wantLogDir := sb.DeployLogDir()

	if wantLogDir == "" {
		// Guard against a misconfigured sandbox whose ControlDir is empty,
		// which would make the assertion below trivially pass (both sides
		// would be empty) without validating anything real.
		t.Fatal("sandbox.DeployLogDir() is empty; " +
			"the workspace manager must populate Sandbox.ControlDir so " +
			"DeployLogDir() produces a non-empty per-run path")
	}

	gotLogDir := deployCalls[0].LogDir
	if gotLogDir != wantLogDir {
		t.Errorf("Deploy LogDir = %q, want %q (sandbox.DeployLogDir()); "+
			"the runner must set DeployRequest.LogDir = sandbox.DeployLogDir() before "+
			"calling the deployer so each run's deploy logs are governed by the run's "+
			"retention policy and concurrent runs cannot contend on a shared log location",
			gotLogDir, wantLogDir)
	}
}

// TestSetup_CataloguePathDeployCallCarriesInfrastructureAgentIDs asserts that
// the single Deploy call for a catalogue-path test carries the subject's
// declared InfrastructureAgentIDs, preserving the nil/non-nil-empty/populated
// distinction. The runner must forward the field verbatim; it must never
// interpret, default, or transform it. This is the same direct passthrough
// contract as Workflows.
func TestSetup_CataloguePathDeployCallCarriesInfrastructureAgentIDs(t *testing.T) {
	t.Run("populated_infrastructure_agent_ids_passed_through", func(t *testing.T) {
		h := newHarness(t)
		req := catalogueRequest("catalogue-infra-agent-ids-populated")
		req.Test.Definition.Subject.InfrastructureAgentIDs = []string{"checkpoint-manager-git", "commit-manager-git"}

		if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}

		deployCalls := h.Deployer.allDeployCalls()
		if len(deployCalls) == 0 {
			t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
		}
		got := deployCalls[0].InfrastructureAgentIDs
		want := req.Test.Definition.Subject.InfrastructureAgentIDs
		if len(got) != len(want) {
			t.Errorf("Deploy InfrastructureAgentIDs = %v, want %v; "+
				"the subject's InfrastructureAgentIDs must be passed through to Deploy verbatim",
				got, want)
		} else {
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("Deploy InfrastructureAgentIDs[%d] = %q, want %q",
						i, got[i], want[i])
				}
			}
		}
	})

	t.Run("nil_infrastructure_agent_ids_stays_nil", func(t *testing.T) {
		h := newHarness(t)
		req := catalogueRequest("catalogue-infra-agent-ids-nil")
		req.Test.Definition.Subject.InfrastructureAgentIDs = nil

		if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}

		deployCalls := h.Deployer.allDeployCalls()
		if len(deployCalls) == 0 {
			t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
		}
		// nil means "not specified" — it must reach Deploy as nil, not empty.
		// An implementor who writes `InfrastructureAgentIDs: []string{}` instead of
		// copying the field verbatim would incorrectly trigger the selections file.
		if deployCalls[0].InfrastructureAgentIDs != nil {
			t.Errorf("Deploy InfrastructureAgentIDs = %v, want nil; "+
				"a nil subject InfrastructureAgentIDs must reach Deploy as nil, not as an "+
				"empty slice — nil means not-specified, empty means explicitly-none",
				deployCalls[0].InfrastructureAgentIDs)
		}
	})

	t.Run("non_nil_empty_infrastructure_agent_ids_stays_non_nil_empty", func(t *testing.T) {
		h := newHarness(t)
		req := catalogueRequest("catalogue-infra-agent-ids-non-nil-empty")
		req.Test.Definition.Subject.InfrastructureAgentIDs = []string{} // explicitly none

		if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}

		deployCalls := h.Deployer.allDeployCalls()
		if len(deployCalls) == 0 {
			t.Fatal("Deploy was never called; want exactly one Deploy call for a catalogue-path test")
		}
		got := deployCalls[0].InfrastructureAgentIDs
		// Non-nil empty is the "explicitly none" sentinel. It must reach Deploy as
		// non-nil so the deploy guard writes the selections file and the deploy tool
		// receives a pre-answer rather than firing an interactive prompt.
		if got == nil {
			t.Error("Deploy InfrastructureAgentIDs = nil, want non-nil empty; " +
				"non-nil empty (explicitly none) must reach Deploy as non-nil — " +
				"an implementor who drops the value to nil would suppress the selections file")
		} else if len(got) != 0 {
			t.Errorf("Deploy InfrastructureAgentIDs = %v (len %d), want empty slice; "+
				"the verbatim passthrough must not add elements", got, len(got))
		}
	})
}

// TestSetup_CataloguePathNilDeploy_SkipsDeployGracefully asserts that when
// Deps.Deploy is nil on a catalogue-path request, setup skips the Deploy step
// gracefully — neither panicking nor returning an unexpected error. This
// follows the "not supplied means not checked" convention every other optional
// port obeys: nil is a valid configuration that means the deploy step is
// omitted, consistent with how a nil Render port would behave on the
// stub-agents path.
func TestSetup_CataloguePathNilDeploy_SkipsDeployGracefully(t *testing.T) {
	h := newHarness(t)
	h.Deps.Deploy = nil // not supplied — Deploy step must be skipped

	req := catalogueRequest("catalogue-nil-deploy")

	_, err := runner.Run(context.Background(), h.Deps, req, nil)
	if err != nil {
		t.Errorf("Run returned unexpected error when Deps.Deploy is nil: %v; "+
			"nil Deploy must be treated as 'not supplied means not checked' — "+
			"the catalogue path skips the Deploy step gracefully rather than panicking "+
			"or returning an error, consistent with the convention every other optional port obeys",
			err)
	}
}
