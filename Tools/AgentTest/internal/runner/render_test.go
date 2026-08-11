package runner_test

// Tests for setup rendering the subject and each declared stub collaborator
// through the deployment port, recording every reported path in the
// provisioning ledger, deriving the subject's spawn-time definition path from
// the port's report, and handling render failures cleanly.
//
// These tests cover:
//   - setup calls Deploy.Render for the subject and each declared stub
//   - the subject is rendered before any stub
//   - the subject's declared workflow set is passed through to the port
//   - the subject's DefinitionPath after setup comes from the port's reported
//     DestinationPath, never from an authored value
//   - every path and directory the port reports is appended to the provisioning
//     ledger before the adapter's Provision is called
//   - the adapter's returned Provisioning is merged into the ledger, not
//     assigned over it, so rendered paths survive the adapter step
//   - a render failure for the subject or any stub aborts setup with a
//     diagnosable error naming what failed
//   - a partial render failure still passes the ledger (containing what was
//     recorded before the failure) to Teardown, so teardown is exact
//   - adapter.Provision is still called after rendering, confirming the adapter
//     remains the sole author of the composed sandbox configuration
//   - the adapter's provisioning failure (e.g. the single-rewriter check)
//     still aborts setup even when the deploy port ran successfully

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
)

// renderingRequest builds a runner.Request whose subject has CatalogAgentKey
// set, so the runner knows it must render the subject through the deploy port.
// DefinitionPath is deliberately left empty: it must come from the port's
// report, not from an authored value.
func renderingRequest(testID string) runner.Request {
	req := newRequest(testID)
	req.Test.Definition.Subject.CatalogAgentKey = "orchestrator"
	req.Test.Definition.Subject.DefinitionPath = ""
	return req
}

// --- subject and stub rendering ---

// TestSetup_RendersSubjectThroughDeployPort asserts that setup calls
// Deploy.Render exactly once for the subject, passing the subject's
// CatalogAgentKey, the adapter's harness ID, and the sandbox subject
// directory as the workspace root.
func TestSetup_RendersSubjectThroughDeployPort(t *testing.T) {
	h := newHarness(t)
	req := renderingRequest("render-subject")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	calls := h.Deployer.allCalls()
	subjectCalls := renderCallsByKey(calls, "orchestrator")
	if len(subjectCalls) != 1 {
		t.Fatalf("Deploy.Render called for subject %d time(s), want exactly 1; all calls: %+v",
			len(subjectCalls), calls)
	}
	got := subjectCalls[0]
	if got.CatalogAgentKey != "orchestrator" {
		t.Errorf("Deploy.Render: CatalogAgentKey = %q, want %q", got.CatalogAgentKey, "orchestrator")
	}
	if got.HarnessID != h.Adapter.ID() {
		t.Errorf("Deploy.Render: HarnessID = %q, want adapter ID %q", got.HarnessID, h.Adapter.ID())
	}
	wantRoot := h.Adapter.lastProvisionReq.Sandbox.SubjectDir
	if got.WorkspaceRoot != wantRoot {
		t.Errorf("Deploy.Render: WorkspaceRoot = %q, want sandbox SubjectDir %q; "+
			"setup must target the sandbox subject directory, not some other sandbox path",
			got.WorkspaceRoot, wantRoot)
	}
}

// TestSetup_PassesSubjectWorkflowsThroughToDeployPort asserts that the
// subject's declared workflow set is passed to Deploy.Render verbatim,
// preserving the nil/empty distinction that governs how the deploy tool
// renders the orchestrator's workflow region.
func TestSetup_PassesSubjectWorkflowsThroughToDeployPort(t *testing.T) {
	t.Run("declared_workflows_passed_through", func(t *testing.T) {
		h := newHarness(t)
		req := renderingRequest("render-workflows-declared")
		req.Test.Definition.Subject.Workflows = []string{"tdd-soft", "tdd-hard"}

		if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}

		calls := h.Deployer.allCalls()
		subjectCalls := renderCallsByKey(calls, "orchestrator")
		if len(subjectCalls) == 0 {
			t.Fatal("Deploy.Render was never called for the subject")
		}
		got := subjectCalls[0].Workflows
		if len(got) != 2 || got[0] != "tdd-soft" || got[1] != "tdd-hard" {
			t.Errorf("Deploy.Render: Workflows = %v, want [tdd-soft tdd-hard]", got)
		}
	})

	t.Run("nil_workflow_set_stays_nil", func(t *testing.T) {
		h := newHarness(t)
		req := renderingRequest("render-workflows-nil")
		req.Test.Definition.Subject.Workflows = nil

		if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
			t.Fatalf("Run returned unexpected error: %v", err)
		}

		calls := h.Deployer.allCalls()
		subjectCalls := renderCallsByKey(calls, "orchestrator")
		if len(subjectCalls) == 0 {
			t.Fatal("Deploy.Render was never called for the subject")
		}
		if subjectCalls[0].Workflows != nil {
			t.Errorf("Deploy.Render: Workflows = %v, want nil (unspecified, not empty)", subjectCalls[0].Workflows)
		}
	})
}

// TestSetup_RendersEachDeclaredStubThroughDeployPort asserts that setup calls
// Deploy.Render once per declared StubAgent, in declaration order, using
// each stub's SourcePath.
func TestSetup_RendersEachDeclaredStubThroughDeployPort(t *testing.T) {
	h := newHarness(t)
	req := renderingRequest("render-stubs")
	req.Test.Definition.StubAgents = []domain.StubAgent{
		{
			Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "researcher"},
			SourcePath: "/workspace/agents/researcher.md",
		},
		{
			Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "reviewer"},
			SourcePath: "/workspace/agents/reviewer.md",
		},
	}

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	calls := h.Deployer.allCalls()
	stubCalls := renderCallsBySourcePath(calls)
	if len(stubCalls) < 2 {
		t.Fatalf("Deploy.Render called for %d stub(s), want 2; all calls: %+v", len(stubCalls), calls)
	}
	if stubCalls[0].SourcePath != "/workspace/agents/researcher.md" {
		t.Errorf("first stub render SourcePath = %q, want researcher stub path", stubCalls[0].SourcePath)
	}
	if stubCalls[1].SourcePath != "/workspace/agents/reviewer.md" {
		t.Errorf("second stub render SourcePath = %q, want reviewer stub path", stubCalls[1].SourcePath)
	}
	wantRoot := h.Adapter.lastProvisionReq.Sandbox.SubjectDir
	for i, sc := range stubCalls {
		if sc.WorkspaceRoot != wantRoot {
			t.Errorf("stub render[%d]: WorkspaceRoot = %q, want sandbox SubjectDir %q; "+
				"stubs must be rendered into the same subject directory as the subject",
				i, sc.WorkspaceRoot, wantRoot)
		}
	}
}

// TestSetup_SubjectRenderedBeforeStubs asserts that setup renders the subject
// before rendering any declared stub. The subject's DefinitionPath must be
// resolved before the adapter can build a correct spawn plan.
func TestSetup_SubjectRenderedBeforeStubs(t *testing.T) {
	var order []string

	h := newHarness(t)
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		if req.CatalogAgentKey != "" {
			order = append(order, "subject:"+req.CatalogAgentKey)
		} else {
			order = append(order, "stub:"+filepath.Base(req.SourcePath))
		}
		return domain.RenderAgentResult{
			DestinationPath: filepath.Join(req.WorkspaceRoot, "agent.md"),
		}, nil
	}

	req := renderingRequest("render-order")
	req.Test.Definition.StubAgents = []domain.StubAgent{
		{
			Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "researcher"},
			SourcePath: "/agents/researcher.md",
		},
	}

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if len(order) < 2 {
		t.Fatalf("Deploy.Render called %d time(s), want at least 2 (subject + 1 stub)", len(order))
	}
	if !strings.HasPrefix(order[0], "subject:") {
		t.Errorf("render order: first call was %q, want the subject rendered before stubs (got: %v)", order[0], order)
	}
}

// TestSetup_SubjectDefinitionPathComesFromDeployPortReport asserts that after
// setup the subject's DefinitionPath in the ProvisionRequest is derived from
// the deploy port's reported DestinationPath — made relative to the sandbox
// subject dir — and is never an authored value.
func TestSetup_SubjectDefinitionPathComesFromDeployPortReport(t *testing.T) {
	h := newHarness(t)

	// Configure a deployer that returns a distinctive path so we can confirm
	// the runner derived DefinitionPath from it rather than from any field in
	// the test definition.
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		if req.CatalogAgentKey != "" {
			return domain.RenderAgentResult{
				DestinationPath: filepath.Join(req.WorkspaceRoot, ".claude", "agents", req.CatalogAgentKey+".md"),
				AgentKey:        req.CatalogAgentKey,
			}, nil
		}
		return domain.RenderAgentResult{
			DestinationPath: filepath.Join(req.WorkspaceRoot, "stub.md"),
		}, nil
	}

	req := renderingRequest("definition-path-from-port")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	subject := h.Adapter.lastProvisionReq.Subject
	if subject.DefinitionPath == "" {
		t.Fatalf("Subject.DefinitionPath is empty after setup; "+
			"want the path the deploy port reported, made relative to SubjectDir")
	}
	if filepath.IsAbs(subject.DefinitionPath) {
		t.Errorf("Subject.DefinitionPath = %q is absolute; "+
			"want a sandbox-relative path (relative to the subject dir)", subject.DefinitionPath)
	}
	// The port reported a path containing the agent key; the derived
	// DefinitionPath must encode that same key.
	if !strings.Contains(subject.DefinitionPath, "orchestrator") {
		t.Errorf("Subject.DefinitionPath = %q does not reflect the subject's agent key %q; "+
			"the path must be derived from the port's DestinationPath, not reconstructed", subject.DefinitionPath, "orchestrator")
	}
}

// --- ledger exactness ---

// TestSetup_RenderedSubjectPathRecordedInProvisioningLedger asserts that the
// destination path the deploy port reports for the subject render is present
// in the Provisioning passed to Deprovision. The path must be in the ledger
// before the adapter's Provision is called — if setup fails during Provision,
// the rendered file is still removed.
func TestSetup_RenderedSubjectPathRecordedInProvisioningLedger(t *testing.T) {
	h := newHarness(t)

	var reportedPath string
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		if req.CatalogAgentKey != "" {
			reportedPath = filepath.Join(req.WorkspaceRoot, "agents", req.CatalogAgentKey+".md")
			return domain.RenderAgentResult{DestinationPath: reportedPath}, nil
		}
		return domain.RenderAgentResult{DestinationPath: filepath.Join(req.WorkspaceRoot, "stub.md")}, nil
	}

	req := renderingRequest("ledger-subject-path")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if reportedPath == "" {
		t.Fatal("Deploy.Render was never called for the subject; " +
			"setup must render the subject through the deploy port")
	}

	deprovisioned := h.Adapter.lastDeprovisionedProv
	if !containsPath(deprovisioned.Files, reportedPath) {
		t.Errorf("rendered subject path %q not found in Provisioning.Files passed to Deprovision; "+
			"setup must record every path the deploy port reports in the ledger so teardown removes it.\n"+
			"got Files: %v", reportedPath, deprovisioned.Files)
	}
}

// TestSetup_RenderedCreatedDirectoriesRecordedInProvisioningLedger asserts
// that directories the deploy port reports it created during a render are
// appended to Provisioning.Dirs in the ledger, so Deprovision can remove
// them too.
func TestSetup_RenderedCreatedDirectoriesRecordedInProvisioningLedger(t *testing.T) {
	h := newHarness(t)

	var renderedDir string
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		if req.CatalogAgentKey != "" {
			renderedDir = filepath.Join(req.WorkspaceRoot, ".claude", "agents")
			return domain.RenderAgentResult{
				DestinationPath:    filepath.Join(renderedDir, req.CatalogAgentKey+".md"),
				CreatedDirectories: []string{renderedDir},
			}, nil
		}
		return domain.RenderAgentResult{DestinationPath: filepath.Join(req.WorkspaceRoot, "stub.md")}, nil
	}

	req := renderingRequest("ledger-created-dirs")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if renderedDir == "" {
		t.Fatal("Deploy.Render was never called for the subject; " +
			"setup must render the subject through the deploy port")
	}

	deprovisioned := h.Adapter.lastDeprovisionedProv
	if !containsPath(deprovisioned.Dirs, renderedDir) {
		t.Errorf("rendered created directory %q not found in Provisioning.Dirs passed to Deprovision; "+
			"setup must record directories the deploy port created so they can be removed.\n"+
			"got Dirs: %v", renderedDir, deprovisioned.Dirs)
	}
}

// TestSetup_AdapterProvisioningMergedNotReplacedInLedger asserts that the
// adapter's returned Provisioning is merged into the ledger rather than
// replacing it: both the rendered paths (appended before Provision) and the
// adapter's own installed paths (from the Provision return) must be present
// in the Provisioning passed to Deprovision.
func TestSetup_AdapterProvisioningMergedNotReplacedInLedger(t *testing.T) {
	h := newHarness(t)

	var renderedPath string
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		renderedPath = filepath.Join(req.WorkspaceRoot, "rendered-agent.md")
		return domain.RenderAgentResult{DestinationPath: renderedPath}, nil
	}

	// The adapter's Provision returns its own distinct file, scope findings,
	// and a sensitive path in Provisioning, representing a merge that must
	// survive into the Deprovision call.
	adapterFile := "/fake-adapter-installed-hook.json"
	adapterSensitive := "/fake-adapter-credential.json"
	adapterFinding := domain.ScopeFinding{Scope: domain.ConfigScope{Name: "user", Path: "/some/config"}}
	h.Adapter.provisioning = domain.Provisioning{
		Files:         []string{adapterFile, adapterSensitive},
		ScopeFindings: []domain.ScopeFinding{adapterFinding},
		Sensitive:     []string{adapterSensitive},
	}

	req := renderingRequest("ledger-merge")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if renderedPath == "" {
		t.Fatal("Deploy.Render was never called; " +
			"setup must render agents through the deploy port")
	}

	deprovisioned := h.Adapter.lastDeprovisionedProv
	if !containsPath(deprovisioned.Files, renderedPath) {
		t.Errorf("rendered path %q not in Provisioning.Files passed to Deprovision; "+
			"adapter.Provision must merge its result into the ledger, not replace it — "+
			"rendered paths must survive the adapter step.\n"+
			"got Files: %v", renderedPath, deprovisioned.Files)
	}
	if !containsPath(deprovisioned.Files, adapterFile) {
		t.Errorf("adapter-installed file %q not in Provisioning.Files passed to Deprovision; "+
			"the adapter's Provision result must be merged into the ledger.\n"+
			"got Files: %v", adapterFile, deprovisioned.Files)
	}
	// The adapter's ScopeFindings and Sensitive fields must replace the ledger's
	// zero values — they are never set by rendering, so the merge must carry them
	// through rather than leaving them empty.
	if len(deprovisioned.ScopeFindings) == 0 {
		t.Errorf("Provisioning.ScopeFindings passed to Deprovision is empty; "+
			"the adapter's returned ScopeFindings must replace the ledger's zero value during merge.\n"+
			"adapter returned ScopeFindings: %+v", []domain.ScopeFinding{adapterFinding})
	}
	if !containsPath(deprovisioned.Sensitive, adapterSensitive) {
		t.Errorf("adapter-sensitive path %q not in Provisioning.Sensitive passed to Deprovision; "+
			"the adapter's returned Sensitive list must replace the ledger's zero value during merge.\n"+
			"got Sensitive: %v", adapterSensitive, deprovisioned.Sensitive)
	}
}

// --- render failure handling ---

// TestSetup_SubjectRenderFailureAbortsSetupWithDiagnosableError asserts that
// when the deploy port returns an error for the subject render, Run returns
// a non-nil error whose message names the subject so an operator can diagnose
// what went wrong.
func TestSetup_SubjectRenderFailureAbortsSetupWithDiagnosableError(t *testing.T) {
	h := newHarness(t)

	renderErr := errors.New("deploy: agent-not-in-catalog: orchestrator")
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		if req.CatalogAgentKey != "" {
			return domain.RenderAgentResult{}, renderErr
		}
		return domain.RenderAgentResult{DestinationPath: filepath.Join(req.WorkspaceRoot, "stub.md")}, nil
	}

	req := renderingRequest("subject-render-fail")

	_, err := runner.Run(context.Background(), h.Deps, req)
	if err == nil {
		t.Fatal("Run returned nil error when the subject render failed; want a non-nil error")
	}
	// The error must name the subject or the render operation so an operator
	// can diagnose what failed without re-running.
	errMsg := err.Error()
	if !strings.Contains(errMsg, "orchestrator") &&
		!strings.Contains(errMsg, "subject") &&
		!strings.Contains(errMsg, "render") {
		t.Errorf("error %q does not name the subject or render operation; "+
			"want a diagnosable message that identifies what failed", errMsg)
	}
}

// TestSetup_StubRenderFailureAbortsSetupWithDiagnosableError asserts that
// when the deploy port returns an error for a stub render, Run returns a
// non-nil error whose message names the stub so an operator can identify it.
func TestSetup_StubRenderFailureAbortsSetupWithDiagnosableError(t *testing.T) {
	h := newHarness(t)

	renderErr := errors.New("deploy: source-not-found: /agents/reviewer.md")
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		if req.SourcePath != "" {
			return domain.RenderAgentResult{}, renderErr
		}
		// Subject render succeeds
		return domain.RenderAgentResult{
			DestinationPath: filepath.Join(req.WorkspaceRoot, "agent.md"),
		}, nil
	}

	req := renderingRequest("stub-render-fail")
	req.Test.Definition.StubAgents = []domain.StubAgent{
		{
			Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "reviewer"},
			SourcePath: "/agents/reviewer.md",
		},
	}

	_, err := runner.Run(context.Background(), h.Deps, req)
	if err == nil {
		t.Fatal("Run returned nil error when a stub render failed; want a non-nil error")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "reviewer") &&
		!strings.Contains(errMsg, "stub") &&
		!strings.Contains(errMsg, "render") {
		t.Errorf("error %q does not name the failing stub or render operation; "+
			"want a diagnosable message that identifies what failed", errMsg)
	}
}

// TestSetup_RenderFailureTearsDownWhatWasRecordedBeforeFailure asserts that
// when a render fails partway through setup — after the subject was rendered
// but before a stub was rendered — the subject's path is still in the
// Provisioning passed to Deprovision. The ledger must capture what was
// recorded before the failure so teardown removes the partial sandbox
// rather than abandoning it.
func TestSetup_RenderFailureTearsDownWhatWasRecordedBeforeFailure(t *testing.T) {
	h := newHarness(t)

	var subjectRenderedPath string
	renderErr := errors.New("deploy: source-not-found: /agents/researcher.md")
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		if req.CatalogAgentKey != "" {
			// Subject render succeeds; record the path we reported
			subjectRenderedPath = filepath.Join(req.WorkspaceRoot, "agent.md")
			return domain.RenderAgentResult{DestinationPath: subjectRenderedPath}, nil
		}
		// Stub render fails
		return domain.RenderAgentResult{}, renderErr
	}

	req := renderingRequest("partial-render-teardown")
	req.Test.Definition.StubAgents = []domain.StubAgent{
		{
			Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "researcher"},
			SourcePath: "/agents/researcher.md",
		},
	}

	_, err := runner.Run(context.Background(), h.Deps, req)
	if err == nil {
		t.Fatal("Run returned nil error after a partial render failure; want non-nil")
	}

	if subjectRenderedPath == "" {
		t.Fatal("subject render was never called; test setup error — " +
			"want Deploy.Render called for the subject before the stub")
	}

	// The subject was rendered before the stub failed. Its path must appear in
	// the Provisioning Deprovision was called with, because it was appended to
	// the ledger before the failure.
	deprovisioned := h.Adapter.lastDeprovisionedProv
	if !containsPath(deprovisioned.Files, subjectRenderedPath) {
		t.Errorf("subject-rendered path %q not in Provisioning.Files passed to Deprovision; "+
			"the ledger must contain every path recorded before a failure so teardown is exact.\n"+
			"got Files: %v", subjectRenderedPath, deprovisioned.Files)
	}
}

// TestSetup_SecondStubRenderFailureTearsDownFirstStubAndSubject asserts that
// when two stubs are declared and the second fails to render, the subject's
// path and the first stub's path are both present in the Provisioning passed
// to Deprovision. This validates that setup appends each render result to the
// ledger individually before moving on, rather than batching or skipping the
// per-stub append.
func TestSetup_SecondStubRenderFailureTearsDownFirstStubAndSubject(t *testing.T) {
	h := newHarness(t)

	var subjectRenderedPath, firstStubRenderedPath string
	renderErr := errors.New("deploy: source-not-found: /agents/reviewer.md")
	renderCount := 0
	h.Deployer.renderFn = func(req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
		renderCount++
		switch {
		case req.CatalogAgentKey != "":
			// Subject render succeeds
			subjectRenderedPath = filepath.Join(req.WorkspaceRoot, "agent.md")
			return domain.RenderAgentResult{DestinationPath: subjectRenderedPath}, nil
		case renderCount == 2:
			// First stub (renderCount=2, after subject) succeeds
			firstStubRenderedPath = filepath.Join(req.WorkspaceRoot, "researcher.md")
			return domain.RenderAgentResult{DestinationPath: firstStubRenderedPath}, nil
		default:
			// Second stub fails
			return domain.RenderAgentResult{}, renderErr
		}
	}

	req := renderingRequest("multi-stub-partial-teardown")
	req.Test.Definition.StubAgents = []domain.StubAgent{
		{
			Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "researcher"},
			SourcePath: "/agents/researcher.md",
		},
		{
			Identity:   domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "reviewer"},
			SourcePath: "/agents/reviewer.md",
		},
	}

	_, err := runner.Run(context.Background(), h.Deps, req)
	if err == nil {
		t.Fatal("Run returned nil error after the second stub render failed; want non-nil")
	}

	if subjectRenderedPath == "" {
		t.Fatal("subject render was never called; test setup error")
	}
	if firstStubRenderedPath == "" {
		t.Fatal("first stub render was never called; test setup error — " +
			"want the first stub rendered before the second is attempted")
	}

	// Both the subject and the first stub were rendered before the second stub
	// failed. Both paths must appear in the Provisioning Deprovision was called
	// with, proving each stub's path is appended to the ledger before the next
	// stub is rendered (not after all stubs complete).
	deprovisioned := h.Adapter.lastDeprovisionedProv
	if !containsPath(deprovisioned.Files, subjectRenderedPath) {
		t.Errorf("subject-rendered path %q not in Provisioning.Files passed to Deprovision; "+
			"the ledger must record every path before the failure so teardown is exact.\n"+
			"got Files: %v", subjectRenderedPath, deprovisioned.Files)
	}
	if !containsPath(deprovisioned.Files, firstStubRenderedPath) {
		t.Errorf("first-stub-rendered path %q not in Provisioning.Files passed to Deprovision; "+
			"each stub's path must be appended to the ledger before iterating to the next stub.\n"+
			"got Files: %v", firstStubRenderedPath, deprovisioned.Files)
	}
}

// --- adapter remains the sole author of the composed configuration ---

// TestSetup_AdapterProvisionCalledAfterDeployRenders asserts that
// adapter.Provision is still called even when the deploy port renders the
// subject and stubs. The adapter is the sole author of the sandbox's composed
// configuration (hooks, settings files); the deploy port only writes agent
// definition files and must not be mistaken for a configuration author.
func TestSetup_AdapterProvisionCalledAfterDeployRenders(t *testing.T) {
	h := newHarness(t)
	req := renderingRequest("adapter-provision-after-render")

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	calls := h.Rec.all()
	provisionFound := false
	for _, c := range calls {
		if c == "adapter.Provision" {
			provisionFound = true
			break
		}
	}
	if !provisionFound {
		t.Error("adapter.Provision was not called after the deploy port rendered; " +
			"the adapter must still provision the sandbox's composed configuration " +
			"(hooks, settings) even when agent rendering runs first")
	}
}

// TestSetup_AdapterProvisionFailureStillAbortsSetup asserts that a failure
// from adapter.Provision — for example, the single-rewriter check refusing a
// composed configuration with competing input-rewrite hooks — aborts setup
// even when the deploy port rendered successfully. The deploy port rendering
// agents does not bypass or weaken the adapter's configuration checks.
func TestSetup_AdapterProvisionFailureStillAbortsSetup(t *testing.T) {
	h := newHarness(t)
	sentinelErr := errors.New("adapter: multiple hooks would rewrite the intercepted call's input")
	h.Adapter.provisionErr = sentinelErr

	req := renderingRequest("adapter-provision-fail")

	_, err := runner.Run(context.Background(), h.Deps, req)
	if err == nil {
		t.Fatal("Run returned nil error when adapter.Provision failed; " +
			"the adapter's configuration checks must still govern setup " +
			"even when the deploy port ran successfully")
	}
	if !errors.Is(err, sentinelErr) && !strings.Contains(err.Error(), "multiple hooks") {
		t.Errorf("error = %q, want it to surface the adapter's provisioning failure; "+
			"the single-rewriter check must not be bypassed by the deploy port running", err.Error())
	}
}

// --- helpers ---

// renderCallsByKey returns the subset of Deploy.Render calls where
// CatalogAgentKey matches key — the subject render calls.
func renderCallsByKey(calls []domain.RenderAgentRequest, key string) []domain.RenderAgentRequest {
	var out []domain.RenderAgentRequest
	for _, c := range calls {
		if c.CatalogAgentKey == key {
			out = append(out, c)
		}
	}
	return out
}

// renderCallsBySourcePath returns the subset of Deploy.Render calls where
// SourcePath is non-empty — the stub render calls.
func renderCallsBySourcePath(calls []domain.RenderAgentRequest) []domain.RenderAgentRequest {
	var out []domain.RenderAgentRequest
	for _, c := range calls {
		if c.SourcePath != "" {
			out = append(out, c)
		}
	}
	return out
}

// containsPath reports whether paths contains target.
func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}
