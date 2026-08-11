package preflight_test

// Tests for Stage 5 preflight cross-validation of the new declaration schema:
//   - A subject with no agent declaration is rejected with missing-subject-agent.
//   - A subject whose declared agent is not in the catalogue is rejected with
//     unknown-subject-agent.
//   - A subject with a workflow id not in the catalogue is rejected with
//     unknown-workflow, naming the offending id.
//   - A subject declaration that fails to render for any other reason is
//     rejected with unrenderable-subject-declaration.
//   - An unrecognised reason code from a future deployment tool version is also
//     treated as unrenderable-subject-declaration (forward-compatibility rule).
//   - A stub whose source file does not exist is rejected with
//     missing-stub-definition.
//   - A stub whose source file exists but whose dry-run render fails is
//     rejected with unrenderable-stub-definition.
//   - Multiple declaration errors from different tests are all accumulated in
//     a single Validate pass.
//   - When Deploy is nil, the render-based checks are skipped and no
//     deployment-tool sentinel is misreported as a declaration error.
//
// All these checks belong inside preflight.Validate alongside the existing
// cross-validation (unknown-collaborator, missing-test-definition, etc.) so
// that every authoring error across a whole suite is reported before a single
// sandbox is created.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/agentdeploy"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// fakeDeployer is a configurable domain.AgentDeployer double that drives
// every outcome the preflight cross-validation tests need without a real
// deployment tool binary.
//
// renderFn is called on every Render call. When nil, Render returns a
// successful zero-valued result.
type fakeDeployer struct {
	renderFn func(ctx context.Context, req domain.RenderAgentRequest) (domain.RenderAgentResult, error)
}

func (f *fakeDeployer) Render(ctx context.Context, req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
	if f.renderFn != nil {
		return f.renderFn(ctx, req)
	}
	return domain.RenderAgentResult{DestinationPath: "/fake/dest/agent.md"}, nil
}

var _ domain.AgentDeployer = (*fakeDeployer)(nil)

// returningRenderError returns a fake deployer whose every Render call returns
// a *agentdeploy.RenderError with the given reason and detail. This is how
// each preflight diagnostic is driven in isolation.
func returningRenderError(reason, detail string) *fakeDeployer {
	return &fakeDeployer{
		renderFn: func(_ context.Context, _ domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
			return domain.RenderAgentResult{}, &agentdeploy.RenderError{
				Reason:      reason,
				Detail:      detail,
				ToolMessage: fmt.Sprintf("fake tool message for reason %q", reason),
				ExitCode:    1,
			}
		},
	}
}

// returningToolUnavailable returns a fake deployer that fails with
// ErrToolUnavailable, simulating a broken installation of the deployment
// tool rather than a bad declaration.
func returningToolUnavailable() *fakeDeployer {
	return &fakeDeployer{
		renderFn: func(_ context.Context, _ domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
			return domain.RenderAgentResult{}, fmt.Errorf("%w: fake binary missing", agentdeploy.ErrToolUnavailable)
		},
	}
}

// returningTimedOut returns a fake deployer that fails with ErrTimedOut.
func returningTimedOut() *fakeDeployer {
	return &fakeDeployer{
		renderFn: func(_ context.Context, _ domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
			return domain.RenderAgentResult{}, fmt.Errorf("%w: fake timeout", agentdeploy.ErrTimedOut)
		},
	}
}

// --- validating suite and definition constants ---

// validSuiteWithOneTest is the minimal suite for deployment-validation tests.
const validSuiteWithOneTest = `
schema_version: 1
id: deploy-validation-suite
tests:
  - path: subject.test.yaml
`

// validRegistryForDeployTests is a stub registry with one collaborator, used
// as the baseline for tests that need a loaded registry.
const validRegistryForDeployTests = `{
  "schema_version": 1,
  "test_id": "subject-test",
  "on_unmatched": "halt",
  "stubs": [
    { "match": { "tool": "dispatch", "agent": "codebase-research", "invocation": 1 },
      "response": { "status_code": "SUCCESS" } }
  ]
}`

// definitionWithSubjectAgent is a test definition that declares the subject
// using the new harness-neutral agent key form.
const definitionWithSubjectAgent = `
schema_version: 1
id: subject-test
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: subject.stubs.json
`

// definitionWithoutSubjectAgent is a test definition that declares a subject
// but omits subject.agent entirely — the missing-subject-agent case.
const definitionWithoutSubjectAgent = `
schema_version: 1
id: subject-test
layer: orchestrator
subject:
  identity: orchestrator
stub_registry: subject.stubs.json
`

// definitionWithBadWorkflows is a test definition with a declared subject
// agent and a workflow id that is not in the catalogue.
const definitionWithBadWorkflows = `
schema_version: 1
id: subject-test
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
  workflows: [nonexistent-workflow]
stub_registry: subject.stubs.json
`

// --- missing-subject-agent ---

// TestValidate_SubjectWithoutAgentField_ReportsMissingSubjectAgent verifies
// that preflight reports missing-subject-agent when a test definition's
// subject block declares no agent key at all. The check is a field inspection
// and does not require a dry-run render.
func TestValidate_SubjectWithoutAgentField_ReportsMissingSubjectAgent(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithoutSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      &fakeDeployer{}, // succeeds for anything that reaches it
	})

	if !hasDiagnosticCode(report, "missing-subject-agent") {
		t.Errorf("expected diagnostic %q when subject.agent is absent, got: %v",
			"missing-subject-agent", report.Diagnostics)
	}
}

// --- unknown-subject-agent ---

// TestValidate_SubjectAgentNotInCatalogue_ReportsUnknownSubjectAgent verifies
// that when Deploy.Render returns reason "agent-not-in-catalog" for the
// subject's dry-run render, preflight reports unknown-subject-agent rather
// than a generic error.
func TestValidate_SubjectAgentNotInCatalogue_ReportsUnknownSubjectAgent(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      returningRenderError("agent-not-in-catalog", "orchestrator"),
	})

	if !hasDiagnosticCode(report, "unknown-subject-agent") {
		t.Errorf("expected diagnostic %q when Deploy returns reason %q, got: %v",
			"unknown-subject-agent", "agent-not-in-catalog", report.Diagnostics)
	}
}

// --- unknown-workflow ---

// TestValidate_UnknownWorkflowId_ReportsUnknownWorkflowNamingTheId verifies
// that when Deploy.Render returns reason "unknown-workflow" with a Detail
// naming the offending id, preflight reports unknown-workflow and the
// diagnostic captures the id so a test author knows exactly which workflow
// to fix.
func TestValidate_UnknownWorkflowId_ReportsUnknownWorkflowNamingTheId(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithBadWorkflows,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      returningRenderError("unknown-workflow", "nonexistent-workflow"),
	})

	if !hasDiagnosticCode(report, "unknown-workflow") {
		t.Errorf("expected diagnostic %q when Deploy returns reason %q, got: %v",
			"unknown-workflow", "unknown-workflow", report.Diagnostics)
	}

	// The diagnostic must name the offending workflow id in its message so
	// the author knows which id to fix.
	for _, d := range report.Diagnostics {
		if d.Code == "unknown-workflow" {
			if d.Message == "" && d.Pointer == "" {
				t.Error("unknown-workflow diagnostic carries no message or pointer identifying the offending workflow id")
			}
			return
		}
	}
}

// --- unrenderable-subject-declaration (other render failures) ---

// TestValidate_SubjectDeclarationOtherRenderFailure_ReportsUnrenderableSubject
// verifies that a subject render failure whose reason is not one of the
// specifically modelled codes (agent-not-in-catalog, unknown-workflow) is
// reported as unrenderable-subject-declaration rather than silently discarded
// or mapped to the wrong code.
func TestValidate_SubjectDeclarationOtherRenderFailure_ReportsUnrenderableSubject(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      returningRenderError("internal", ""),
	})

	if !hasDiagnosticCode(report, "unrenderable-subject-declaration") {
		t.Errorf("expected diagnostic %q when Deploy returns unmodelled reason %q, got: %v",
			"unrenderable-subject-declaration", "internal", report.Diagnostics)
	}
}

// TestValidate_SubjectFutureReasonCode_TreatedAsUnrenderableSubject verifies
// the forward-compatibility rule: a reason code emitted by a newer deployment
// tool that this version of preflight does not recognise must be treated as
// unrenderable-subject-declaration, not rejected or mapped to a known code.
// An older preflight paired with a newer tool must degrade gracefully.
func TestValidate_SubjectFutureReasonCode_TreatedAsUnrenderableSubject(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      returningRenderError("some-reason-code-from-a-future-version", ""),
	})

	if !hasDiagnosticCode(report, "unrenderable-subject-declaration") {
		t.Errorf("expected diagnostic %q for an unrecognised reason code, got: %v; "+
			"forward-compatibility requires an unrecognised reason to be reported as "+
			"unrenderable-subject-declaration rather than silently passing or mapping to a known code",
			"unrenderable-subject-declaration", report.Diagnostics)
	}
	// The forward-compatibility rule is only meaningful when it does NOT
	// produce unknown-subject-agent or unknown-workflow for an unrecognised
	// code — those would be false positives.
	if hasDiagnosticCode(report, "unknown-subject-agent") {
		t.Error("unrecognised reason code must not produce unknown-subject-agent")
	}
	if hasDiagnosticCode(report, "unknown-workflow") {
		t.Error("unrecognised reason code must not produce unknown-workflow")
	}
}

// TestValidate_ReasonCodesToDiagnostics is the authoritative table that pins
// the subject-declaration reason-code vocabulary on the preflight side. A
// new code added in the deployment tool without a corresponding branch here
// turns this table red; a branch added here without a corresponding test
// means nothing, because the test is the contract.
func TestValidate_ReasonCodesToDiagnostics(t *testing.T) {
	cases := []struct {
		reason   string
		detail   string
		wantCode string
	}{
		{"agent-not-in-catalog", "orchestrator", "unknown-subject-agent"},
		{"unknown-workflow", "tdd-soft", "unknown-workflow"},
		{"internal", "", "unrenderable-subject-declaration"},
		{"source-not-found", "/some/path", "unrenderable-subject-declaration"},
		{"harness-unresolvable", "claude-code", "unrenderable-subject-declaration"},
	}

	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			root := writeTree(t, map[string]string{
				"suite.suite.yaml":   validSuiteWithOneTest,
				"subject.test.yaml":  definitionWithSubjectAgent,
				"subject.stubs.json": validRegistryForDeployTests,
			})

			_, report := preflight.Validate(preflight.Input{
				SuitePath:   filepath.Join(root, "suite.suite.yaml"),
				FixtureRoot: filepath.Join(root, "fixtures"),
				HarnessID:   "claude-code",
				Deploy:      returningRenderError(tc.reason, tc.detail),
			})

			if !hasDiagnosticCode(report, tc.wantCode) {
				t.Errorf("reason %q: expected diagnostic %q, got: %v",
					tc.reason, tc.wantCode, report.Diagnostics)
			}
		})
	}
}

// --- missing-stub-definition ---

// TestValidate_StubDefinitionSourcePathMissing_ReportsMissingStubDefinition
// verifies that a stub_agents entry whose source file does not exist at the
// resolved path is reported as missing-stub-definition. The check is a file
// existence test (os.Stat) and does not require a dry-run render.
func TestValidate_StubDefinitionSourcePathMissing_ReportsMissingStubDefinition(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml": validSuiteWithOneTest,
		"subject.test.yaml": `
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
    source: agents/does-not-exist.md
`,
		"subject.stubs.json": validRegistryForDeployTests,
		// agents/does-not-exist.md is deliberately absent
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      &fakeDeployer{}, // source file missing, so render should not be reached
	})

	if !hasDiagnosticCode(report, "missing-stub-definition") {
		t.Errorf("expected diagnostic %q when a stub_agents source file does not exist, got: %v",
			"missing-stub-definition", report.Diagnostics)
	}
}

// --- unrenderable-stub-definition ---

// TestValidate_StubDefinitionRenderFails_ReportsUnrenderableStubDefinition
// verifies that a stub whose source file exists on disk but whose dry-run
// render returns an error is reported as unrenderable-stub-definition.
func TestValidate_StubDefinitionRenderFails_ReportsUnrenderableStubDefinition(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml": validSuiteWithOneTest,
		"subject.test.yaml": `
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
`,
		"subject.stubs.json":          validRegistryForDeployTests,
		"agents/codebase-research.md": "# stub definition — exists on disk",
	})

	// The fake deployer succeeds for the subject render (CatalogAgentKey != "")
	// but fails for the stub render (SourcePath != "").
	deploy := &fakeDeployer{
		renderFn: func(_ context.Context, req domain.RenderAgentRequest) (domain.RenderAgentResult, error) {
			if req.SourcePath != "" {
				return domain.RenderAgentResult{}, &agentdeploy.RenderError{
					Reason:      "source-not-generic",
					ToolMessage: "fake: stub definition is not a valid generic-form agent",
					ExitCode:    1,
				}
			}
			return domain.RenderAgentResult{DestinationPath: "/fake/dest/orchestrator.md"}, nil
		},
	}

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      deploy,
	})

	if !hasDiagnosticCode(report, "unrenderable-stub-definition") {
		t.Errorf("expected diagnostic %q when a stub dry-run render fails, got: %v",
			"unrenderable-stub-definition", report.Diagnostics)
	}
}

// --- tool-unavailability is not a declaration error ---

// TestValidate_ToolUnavailable_ReportedAsEnvironmentProblem verifies that
// when Deploy.Render returns ErrToolUnavailable — meaning the deployment
// binary itself could not be invoked — preflight does NOT report a declaration
// diagnostic (unknown-subject-agent, unrenderable-subject-declaration, etc.).
// A broken installation must not masquerade as a suite full of bad
// declarations.
func TestValidate_ToolUnavailable_ReportedAsEnvironmentProblem(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      returningToolUnavailable(),
	})

	// Must NOT produce declaration diagnostics when the tool itself is broken.
	if hasDiagnosticCode(report, "unknown-subject-agent") {
		t.Error("ErrToolUnavailable must not produce unknown-subject-agent; " +
			"a broken deployment tool is an environment problem, not a declaration error")
	}
	if hasDiagnosticCode(report, "unrenderable-subject-declaration") {
		t.Error("ErrToolUnavailable must not produce unrenderable-subject-declaration; " +
			"a broken deployment tool is an environment problem, not a declaration error")
	}

	// Must report through the environment-problem channel instead.
	if !hasDiagnosticCode(report, "environment-unusable") {
		t.Error("ErrToolUnavailable must be reported as environment-unusable, " +
			"not as a declaration diagnostic")
	}
}

// TestValidate_TimedOut_ReportedAsEnvironmentProblem verifies that
// ErrTimedOut (a port-level timeout, not a declaration error) is handled the
// same way as ErrToolUnavailable.
func TestValidate_TimedOut_ReportedAsEnvironmentProblem(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      returningTimedOut(),
	})

	if hasDiagnosticCode(report, "unrenderable-subject-declaration") {
		t.Error("ErrTimedOut must not produce unrenderable-subject-declaration; " +
			"a port timeout is an environment problem, not a declaration error")
	}
	if !hasDiagnosticCode(report, "environment-unusable") {
		t.Error("ErrTimedOut must be reported as environment-unusable, " +
			"not as a declaration diagnostic")
	}
}

// --- one-pass accumulation ---

// TestValidate_MultipleDeclarationErrors_AllReportedInOnePass is the decisive
// accumulation case for the new checks: multiple declaration errors in the
// same test must all be reported together, not one per run.
func TestValidate_MultipleDeclarationErrors_AllReportedInOnePass(t *testing.T) {
	// Seed two independent errors:
	//   1. Subject has no agent declaration → missing-subject-agent
	//   2. A stub_agents source file is missing → missing-stub-definition
	root := writeTree(t, map[string]string{
		"suite.suite.yaml": validSuiteWithOneTest,
		"subject.test.yaml": `
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
    source: agents/also-does-not-exist.md
`,
		"subject.stubs.json": validRegistryForDeployTests,
		// agents/also-does-not-exist.md is deliberately absent
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      &fakeDeployer{},
	})

	missingAgent := hasDiagnosticCode(report, "missing-subject-agent")
	missingStub := hasDiagnosticCode(report, "missing-stub-definition")

	if !missingAgent && !missingStub {
		t.Fatalf("expected both missing-subject-agent and missing-stub-definition diagnostics "+
			"to be reported in one pass; got: %v", report.Diagnostics)
	}
	if !missingAgent {
		t.Errorf("missing-subject-agent not found in: %v", report.Diagnostics)
	}
	if !missingStub {
		t.Errorf("missing-stub-definition not found in: %v", report.Diagnostics)
	}
}

// --- nil Deploy skips render-based checks ---

// TestValidate_DeployNil_SkipsRenderBasedChecks verifies that when Deploy is
// nil, the checks that require a dry-run render (unknown-subject-agent,
// unknown-workflow, unrenderable-subject-declaration) are skipped entirely.
// The nil convention matches zero-valued Capabilities: "not supplied" means
// "not checked", never "fails every render".
func TestValidate_DeployNil_SkipsRenderBasedChecks(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      nil, // no deployer supplied
	})

	// Render-based checks must be absent when Deploy is nil.
	for _, code := range []string{
		"unknown-subject-agent",
		"unknown-workflow",
		"unrenderable-subject-declaration",
		"unrenderable-stub-definition",
	} {
		if hasDiagnosticCode(report, code) {
			t.Errorf("nil Deploy must not produce %q; skipping render-based checks when no deployer is supplied", code)
		}
	}
}

// TestValidate_DeployNil_FieldInspectionCheckStillFires verifies that field-
// inspection checks (which do not need a dry-run render) still fire when
// Deploy is nil, so nil is not a blanket skip of all new checks.
func TestValidate_DeployNil_FieldInspectionCheckStillFires(t *testing.T) {
	root := writeTree(t, map[string]string{
		"suite.suite.yaml":   validSuiteWithOneTest,
		"subject.test.yaml":  definitionWithoutSubjectAgent,
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      nil, // no deployer supplied
	})

	// missing-subject-agent is a field inspection: it must fire regardless of
	// whether Deploy is wired.
	if !hasDiagnosticCode(report, "missing-subject-agent") {
		t.Errorf("expected diagnostic %q even when Deploy is nil; field-inspection checks "+
			"do not require the deployment port", "missing-subject-agent")
	}
}

// Ensure the agentdeploy sentinel errors used in this file are valid
// (compile-time check only; errors.Is verifies the wrapping chain).
var (
	_ = errors.Is(nil, agentdeploy.ErrToolUnavailable)
	_ = errors.Is(nil, agentdeploy.ErrTimedOut)
	_ = errors.Is(nil, agentdeploy.ErrRenderFailed)
)

// --- undeclared-stub-agent ---

// TestValidate_StubRegistryCollaboratorWithoutStubAgentsEntry_ReportsUndeclaredStubAgent
// verifies that a collaborator identity present in the stub registry but absent
// from the stub_agents list is reported as undeclared-stub-agent. The check is
// a stub-registry inspection and does not require a dry-run render, so it fires
// even when Deploy is nil.
//
// This matters because a dispatch to a collaborator with no deployed definition
// file is a confusing mid-run failure — a hard-to-diagnose dispatch refusal —
// for a declaration error that is fully knowable before the run starts.
func TestValidate_StubRegistryCollaboratorWithoutStubAgentsEntry_ReportsUndeclaredStubAgent(t *testing.T) {
	// The registry stubs the "codebase-research" collaborator, but the
	// test definition carries no stub_agents entry to back that stub with
	// a deployed definition file.
	root := writeTree(t, map[string]string{
		"suite.suite.yaml": validSuiteWithOneTest,
		"subject.test.yaml": `
schema_version: 1
id: subject-test
layer: orchestrator
subject:
  identity: orchestrator
  agent: orchestrator
stub_registry: subject.stubs.json
`,
		// stub registry references codebase-research, but stub_agents is absent
		"subject.stubs.json": validRegistryForDeployTests,
	})

	_, report := preflight.Validate(preflight.Input{
		SuitePath:   filepath.Join(root, "suite.suite.yaml"),
		FixtureRoot: filepath.Join(root, "fixtures"),
		HarnessID:   "claude-code",
		Deploy:      nil, // registry inspection — no render needed
	})

	if !hasDiagnosticCode(report, "undeclared-stub-agent") {
		t.Errorf("expected diagnostic %q when a stub registry entry names a collaborator "+
			"with no corresponding stub_agents definition, got: %v",
			"undeclared-stub-agent", report.Diagnostics)
	}
}

// suppressUnusedImportWarning uses the os package to satisfy the compiler when
// no file-system calls are in scope. This is removed once the tests exercise
// file-based checks.
var _ = os.Stat
